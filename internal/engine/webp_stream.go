package engine

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// Encode GIF frames individually in one FFmpeg process. The streaming muxer
// retains their exact timing and full-canvas alpha, including transparent GIFs
// whose frames update only a small rectangle. No full-frame image cache is made.
func (e VideoEditor) gifWebP(ctx context.Context, req AnimationRequest, destination string, q AnimationQuality, progress func(int)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	done := make(chan error, 1)
	go func() {
		err := muxGIFWebP(ctx, reader, destination, req)
		reader.CloseWithError(err)
		if err != nil {
			cancel()
		}
		done <- err
	}()
	req.Output = writer
	lossless, pixel := "0", "yuva420p"
	if q.Lossless {
		lossless, pixel = "1", "bgra"
	}
	level := 4
	if q.Effort == "fast" {
		level = 1
	} else if q.Effort == "precise" {
		level = 6
	}
	args := []string{"-c:v", "libwebp", "-quality", strconv.Itoa(q.Quality), "-lossless", lossless, "-compression_level", strconv.Itoa(level), "-pix_fmt", pixel, "-frames:v", strconv.Itoa(req.ExpectedFrames), "-f", "image2pipe", "pipe:1"}
	err := e.animationPass(ctx, req, filepath.Dir(destination), args, progress)
	writer.CloseWithError(err)
	return errors.Join(err, <-done)
}

func muxGIFWebP(ctx context.Context, input io.Reader, path string, req AnimationRequest) error {
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	var header [44]byte
	copy(header[:], "RIFF")
	copy(header[8:], "WEBPVP8X")
	binary.LittleEndian.PutUint32(header[16:20], 10)
	header[20] = 0x12
	put24(header[24:27], req.Crop.Width-1)
	put24(header[27:30], req.Crop.Height-1)
	copy(header[30:], "ANIM")
	binary.LittleEndian.PutUint32(header[34:38], 6)
	if _, err = output.Write(header[:]); err != nil {
		return err
	}
	index := req.FirstFrame
	skip := func(int) (bool, error) { return false, nil }
	if req.NewSkip != nil {
		skip = req.NewSkip()
	}
	for frame := 0; frame < req.ExpectedFrames; frame++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		for index < req.FrameCount {
			drop, e := skip(index)
			if e != nil {
				return e
			}
			if !drop {
				break
			}
			index++
		}
		if index >= req.FrameCount || index >= len(req.Delays) {
			return fmt.Errorf("WebP 출력 프레임 정보 오류")
		}
		delay := req.Delays[index] * 10
		index++
		var riff [12]byte
		if _, err = io.ReadFull(input, riff[:]); err != nil {
			return err
		}
		if string(riff[:4]) != "RIFF" || string(riff[8:]) != "WEBP" {
			return fmt.Errorf("WebP 프레임 스트림 형식 오류")
		}
		remaining := int64(binary.LittleEndian.Uint32(riff[4:8])) - 4
		var chunk [8]byte
		for {
			if remaining < 8 {
				return fmt.Errorf("WebP 프레임이 잘렸습니다")
			}
			if _, err = io.ReadFull(input, chunk[:]); err != nil {
				return err
			}
			remaining -= 8
			tag := string(chunk[:4])
			size := int64(binary.LittleEndian.Uint32(chunk[4:]))
			padded := size + (size & 1)
			if padded > remaining {
				return fmt.Errorf("WebP 프레임 청크 크기 오류")
			}
			if tag == "VP8X" {
				if size != 10 {
					return fmt.Errorf("WebP 프레임 캔버스 오류")
				}
				if _, err = io.CopyN(io.Discard, input, padded); err != nil {
					return err
				}
				remaining -= padded
				continue
			}
			if tag != "VP8 " && tag != "VP8L" && tag != "ALPH" {
				return fmt.Errorf("지원하지 않는 WebP 프레임 청크")
			}
			break
		}
		if remaining > int64(^uint32(0))-24 || delay < 1 || delay > 0xffffff {
			return fmt.Errorf("WebP 컨테이너 표현 범위를 초과했습니다")
		}
		var anmf [24]byte
		copy(anmf[:], "ANMF")
		binary.LittleEndian.PutUint32(anmf[4:8], uint32(16+8+remaining))
		put24(anmf[14:17], req.Crop.Width-1)
		put24(anmf[17:20], req.Crop.Height-1)
		put24(anmf[20:23], delay)
		anmf[23] = 2 // Replace the complete canvas, including alpha.
		if _, err = output.Write(anmf[:]); err != nil {
			return err
		}
		if _, err = output.Write(chunk[:]); err != nil {
			return err
		}
		imageSeen := false
		for {
			tag := string(chunk[:4])
			size := int64(binary.LittleEndian.Uint32(chunk[4:]))
			padded := size + (size & 1)
			if padded > remaining || size == 0 {
				return fmt.Errorf("WebP 프레임 데이터 크기 오류")
			}
			if tag == "VP8 " || tag == "VP8L" {
				if imageSeen {
					return fmt.Errorf("WebP 이미지 청크 중복")
				}
				imageSeen = true
			} else if tag != "ALPH" || imageSeen {
				return fmt.Errorf("WebP 알파 청크 순서 오류")
			}
			if _, err = io.CopyN(output, input, padded); err != nil {
				return err
			}
			remaining -= padded
			if remaining == 0 {
				break
			}
			if remaining < 8 {
				return fmt.Errorf("WebP 프레임 후행 청크 오류")
			}
			if _, err = io.ReadFull(input, chunk[:]); err != nil {
				return err
			}
			remaining -= 8
			if _, err = output.Write(chunk[:]); err != nil {
				return err
			}
		}
		if !imageSeen {
			return fmt.Errorf("WebP 프레임 이미지가 없습니다")
		}
	}
	var extra [1]byte
	if n, e := input.Read(extra[:]); n != 0 || e != io.EOF {
		if e != nil {
			return e
		}
		return fmt.Errorf("WebP 프레임 수가 다릅니다")
	}
	size, err := output.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if size < 8 || size-8 > int64(^uint32(0)) {
		return fmt.Errorf("WebP RIFF 표현 범위를 초과했습니다")
	}
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], uint32(size-8))
	if _, err = output.WriteAt(length[:], 4); err != nil {
		return err
	}
	return output.Sync()
}
