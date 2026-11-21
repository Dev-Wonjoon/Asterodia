package engine

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type webpFrame struct {
	offset, length int64
	width, height  int
}

func uint24(p []byte) int   { return int(p[0]) | int(p[1])<<8 | int(p[2])<<16 }
func put24(p []byte, n int) { p[0] = byte(n); p[1] = byte(n >> 8); p[2] = byte(n >> 16) }

// Stream the container and decode boundary frames. FFprobe builds without
// animated WebP decoding cannot supply its duration or frame count reliably.
func (e VideoEditor) VerifyWebP(ctx context.Context, path string) (LocalMetadata, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return LocalMetadata{}, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return LocalMetadata{}, 0, err
	}
	var header [12]byte
	if _, err = io.ReadFull(f, header[:]); err != nil {
		return LocalMetadata{}, 0, err
	}
	if string(header[:4]) != "RIFF" || string(header[8:]) != "WEBP" || int64(binary.LittleEndian.Uint32(header[4:8]))+8 != info.Size() {
		return LocalMetadata{}, 0, fmt.Errorf("WebP RIFF 크기가 일치하지 않습니다")
	}
	w, h, count, millis, loop := 0, 0, 0, int64(0), -1
	var first, last, static webpFrame
	alphaStart := int64(-1)
	for pos := int64(12); pos < info.Size(); {
		if err = ctx.Err(); err != nil {
			return LocalMetadata{}, 0, err
		}
		var chunk [8]byte
		if _, err = f.ReadAt(chunk[:], pos); err != nil {
			return LocalMetadata{}, 0, err
		}
		length := int64(binary.LittleEndian.Uint32(chunk[4:]))
		start := pos + 8
		next := start + length + (length & 1)
		if next > info.Size() || next <= pos {
			return LocalMetadata{}, 0, fmt.Errorf("WebP 청크가 잘렸습니다")
		}
		switch string(chunk[:4]) {
		case "VP8X":
			var data [10]byte
			if length != 10 {
				return LocalMetadata{}, 0, fmt.Errorf("WebP 캔버스 정보가 올바르지 않습니다")
			}
			if _, err = f.ReadAt(data[:], start); err != nil {
				return LocalMetadata{}, 0, err
			}
			w, h = uint24(data[4:7])+1, uint24(data[7:10])+1
		case "ANIM":
			var data [6]byte
			if length != 6 {
				return LocalMetadata{}, 0, fmt.Errorf("WebP 반복 정보가 올바르지 않습니다")
			}
			if _, err = f.ReadAt(data[:], start); err != nil {
				return LocalMetadata{}, 0, err
			}
			loop = int(binary.LittleEndian.Uint16(data[4:]))
		case "ANMF":
			var data [16]byte
			if length < 24 {
				return LocalMetadata{}, 0, fmt.Errorf("WebP 프레임이 잘렸습니다")
			}
			if _, err = f.ReadAt(data[:], start); err != nil {
				return LocalMetadata{}, 0, err
			}
			x, y, fw, fh := uint24(data[:3])*2, uint24(data[3:6])*2, uint24(data[6:9])+1, uint24(data[9:12])+1
			if w < 1 || h < 1 || x > w-fw || y > h-fh {
				return LocalMetadata{}, 0, fmt.Errorf("WebP 프레임이 캔버스 밖에 있습니다")
			}
			if err = validateWebPPayload(f, start+16, length-16); err != nil {
				return LocalMetadata{}, 0, err
			}
			frame := webpFrame{offset: start + 16, length: length - 16, width: fw, height: fh}
			if count == 0 {
				first = frame
			}
			last = frame
			count++
			millis += int64(uint24(data[12:15]))
		case "ALPH":
			alphaStart = pos
		case "VP8 ", "VP8L":
			var data [10]byte
			n := min(length, int64(len(data)))
			if _, err = f.ReadAt(data[:n], start); err != nil {
				return LocalMetadata{}, 0, err
			}
			if w == 0 {
				if string(chunk[:4]) == "VP8L" && length >= 5 && data[0] == 0x2f {
					w = 1 + int(data[1]) + int(data[2]&63)*256
					h = 1 + int(data[2]>>6) + int(data[3])*4 + int(data[4]&15)*1024
				} else if length >= 10 && string(data[3:6]) == "\x9d\x01\x2a" {
					w = int(binary.LittleEndian.Uint16(data[6:8]) & 0x3fff)
					h = int(binary.LittleEndian.Uint16(data[8:10]) & 0x3fff)
				}
			}
			static = webpFrame{offset: pos, length: 8 + length + (length & 1), width: w, height: h}
			if alphaStart >= 0 {
				static.offset = alphaStart
				static.length = next - alphaStart
			}
		}
		pos = next
	}
	if w < 1 || h < 1 {
		return LocalMetadata{}, 0, fmt.Errorf("WebP 크기를 확인할 수 없습니다")
	}
	if count == 0 {
		if static.length == 0 {
			return LocalMetadata{}, 0, fmt.Errorf("WebP 이미지가 없습니다")
		}
		count = 1
		first, last = static, static
	} else if loop != 0 || millis <= 0 {
		return LocalMetadata{}, 0, fmt.Errorf("WebP 반복 또는 재생 시간이 올바르지 않습니다")
	}
	for n, frame := range []webpFrame{first, last} {
		if n == 1 && frame.offset == first.offset {
			continue
		}
		if err = e.verifyWebPFrame(ctx, f, path, frame); err != nil {
			return LocalMetadata{}, 0, err
		}
	}
	duration := float64(millis) / 1000
	return LocalMetadata{Kind: "image", Format: "webp", Width: &w, Height: &h, Duration: &duration}, count, nil
}

func validateWebPPayload(f *os.File, start, length int64) error {
	image := false
	for pos := int64(0); pos < length; {
		var h [8]byte
		if pos+8 > length {
			return fmt.Errorf("WebP 프레임 청크가 잘렸습니다")
		}
		if _, err := f.ReadAt(h[:], start+pos); err != nil {
			return err
		}
		size := int64(binary.LittleEndian.Uint32(h[4:]))
		next := pos + 8 + size + (size & 1)
		if size == 0 || next > length {
			return fmt.Errorf("WebP 프레임 데이터가 잘렸습니다")
		}
		switch string(h[:4]) {
		case "VP8 ", "VP8L":
			if image {
				return fmt.Errorf("WebP 프레임 이미지가 중복됐습니다")
			}
			image = true
		case "ALPH":
		default:
			return fmt.Errorf("지원하지 않는 WebP 프레임 청크입니다")
		}
		pos = next
	}
	if !image {
		return fmt.Errorf("WebP 프레임 이미지가 없습니다")
	}
	return nil
}
func extractWebPFrame(source *os.File, path string, frame webpFrame) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".webp-check-*.webp")
	if err != nil {
		return "", err
	}
	name := f.Name()
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(name)
		}
	}()
	var header [30]byte
	copy(header[:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(22+frame.length))
	copy(header[8:], "WEBPVP8X")
	binary.LittleEndian.PutUint32(header[16:20], 10)
	header[20] = 0x10
	put24(header[24:27], frame.width-1)
	put24(header[27:30], frame.height-1)
	if _, err = f.Write(header[:]); err == nil {
		_, err = io.CopyN(f, io.NewSectionReader(source, frame.offset, frame.length), frame.length)
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	complete = true
	return name, nil
}
func (e VideoEditor) verifyWebPFrame(ctx context.Context, source *os.File, path string, frame webpFrame) error {
	name, err := extractWebPFrame(source, path, frame)
	if err != nil {
		return err
	}
	defer os.Remove(name)
	_, err = new(ProcessRunner).Run(ctx, ProcessRequest{Executable: e.FFmpeg, Directory: filepath.Dir(path), Unbounded: true, Arguments: []string{"-nostdin", "-v", "error", "-xerror", "-protocol_whitelist", "file,pipe", "-i", name, "-map", "0:v:0", "-f", "null", "-"}})
	return err
}

// WebPThumbnail supports FFmpeg versions that cannot demux animated WebP.
// Only the first compressed frame is copied, then FFmpeg scales its decode.
func (e VideoEditor) WebPThumbnail(ctx context.Context, path, destination string) error {
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	var header [12]byte
	if _, err = io.ReadFull(source, header[:]); err != nil {
		return err
	}
	if string(header[:4]) != "RIFF" || string(header[8:]) != "WEBP" || int64(binary.LittleEndian.Uint32(header[4:8]))+8 != info.Size() {
		return fmt.Errorf("WebP 파일 형식이 올바르지 않습니다")
	}
	input := path
	filter := "scale=480:480:force_original_aspect_ratio=decrease"
	w, h := 0, 0
	for pos := int64(12); pos < info.Size(); {
		if err = ctx.Err(); err != nil {
			return err
		}
		var chunk [8]byte
		if _, err = source.ReadAt(chunk[:], pos); err != nil {
			return err
		}
		length := int64(binary.LittleEndian.Uint32(chunk[4:]))
		start := pos + 8
		next := start + length + (length & 1)
		if next > info.Size() || next <= pos {
			return fmt.Errorf("WebP 청크가 잘렸습니다")
		}
		if string(chunk[:4]) == "VP8X" {
			var data [10]byte
			if length != 10 {
				return fmt.Errorf("WebP 크기 정보 오류")
			}
			if _, err = source.ReadAt(data[:], start); err != nil {
				return err
			}
			w, h = uint24(data[4:7])+1, uint24(data[7:10])+1
		}
		if string(chunk[:4]) == "ANMF" {
			var data [16]byte
			if length < 24 {
				return fmt.Errorf("WebP 프레임 오류")
			}
			if _, err = source.ReadAt(data[:], start); err != nil {
				return err
			}
			x, y, fw, fh := uint24(data[:3])*2, uint24(data[3:6])*2, uint24(data[6:9])+1, uint24(data[9:12])+1
			if w < 1 || h < 1 || x > w-fw || y > h-fh {
				return fmt.Errorf("WebP 캔버스 오류")
			}
			if err = validateWebPPayload(source, start+16, length-16); err != nil {
				return err
			}
			input, err = extractWebPFrame(source, destination, webpFrame{offset: start + 16, length: length - 16, width: fw, height: fh})
			if err != nil {
				return err
			}
			defer os.Remove(input)
			filter = fmt.Sprintf("format=rgba,pad=%d:%d:%d:%d:color=black,", w, h, x, y) + filter
			break
		}
		pos = next
	}
	_, err = new(ProcessRunner).Run(ctx, ProcessRequest{Executable: e.FFmpeg, Directory: filepath.Dir(destination), Unbounded: true, Arguments: []string{"-nostdin", "-v", "error", "-xerror", "-y", "-protocol_whitelist", "file,pipe", "-i", input, "-frames:v", "1", "-vf", filter, "-update", "1", destination}})
	return err
}
