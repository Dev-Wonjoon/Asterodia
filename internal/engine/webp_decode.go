package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

type webpSourceFrame struct {
	webpFrame
	x, y, delay      int
	dispose, replace bool
}
type webpSource struct {
	width, height int
	background    color.NRGBA
	frames        []webpSourceFrame
}

// Read container timing without relying on FFmpeg's animated WebP demuxer.
func readWebPSource(ctx context.Context, f *os.File) (webpSource, error) {
	var result webpSource
	info, err := f.Stat()
	if err != nil {
		return result, err
	}
	var header [12]byte
	if _, err = f.ReadAt(header[:], 0); err != nil {
		return result, err
	}
	if string(header[:4]) != "RIFF" || string(header[8:]) != "WEBP" || int64(binary.LittleEndian.Uint32(header[4:8]))+8 != info.Size() {
		return result, fmt.Errorf("WebP RIFF 크기가 일치하지 않습니다")
	}
	animated, animationHeader := false, false
	for pos := int64(12); pos < info.Size(); {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		var chunk [8]byte
		if _, err = f.ReadAt(chunk[:], pos); err != nil {
			return result, err
		}
		length := int64(binary.LittleEndian.Uint32(chunk[4:]))
		start := pos + 8
		next := start + length + (length & 1)
		if next > info.Size() || next <= pos {
			return result, fmt.Errorf("WebP 청크가 잘렸습니다")
		}
		switch string(chunk[:4]) {
		case "VP8X":
			var data [10]byte
			if length != 10 {
				return result, fmt.Errorf("WebP 캔버스 정보가 올바르지 않습니다")
			}
			if _, err = f.ReadAt(data[:], start); err != nil {
				return result, err
			}
			result.width, result.height = uint24(data[4:7])+1, uint24(data[7:10])+1
			animated = data[0]&2 != 0
		case "ANIM":
			var data [6]byte
			if length != 6 {
				return result, fmt.Errorf("WebP 반복 정보가 올바르지 않습니다")
			}
			if _, err = f.ReadAt(data[:], start); err != nil {
				return result, err
			}
			result.background = color.NRGBA{R: data[2], G: data[1], B: data[0], A: data[3]}
			animationHeader = true
		case "ANMF":
			var data [16]byte
			if !animated || !animationHeader || length < 24 || len(result.frames) >= 1000000 {
				return result, fmt.Errorf("WebP 프레임 정보가 올바르지 않습니다")
			}
			if _, err = f.ReadAt(data[:], start); err != nil {
				return result, err
			}
			frame := webpSourceFrame{webpFrame: webpFrame{offset: start + 16, length: length - 16, width: uint24(data[6:9]) + 1, height: uint24(data[9:12]) + 1}, x: uint24(data[:3]) * 2, y: uint24(data[3:6]) * 2, delay: max(1, uint24(data[12:15])), dispose: data[15]&1 != 0, replace: data[15]&2 != 0}
			if frame.x > result.width-frame.width || frame.y > result.height-frame.height || data[15]&0xfc != 0 {
				return result, fmt.Errorf("WebP 프레임이 캔버스 밖에 있습니다")
			}
			if err = validateWebPPayload(f, frame.offset, frame.length); err != nil {
				return result, err
			}
			result.frames = append(result.frames, frame)
		}
		pos = next
	}
	if !animated {
		config, err := webp.DecodeConfig(io.NewSectionReader(f, 0, info.Size()))
		if err != nil {
			return result, err
		}
		result.width, result.height = config.Width, config.Height
		// A static image is editable as one frame, with a default 100ms duration.
		result.frames = []webpSourceFrame{{webpFrame: webpFrame{offset: 0, length: info.Size(), width: config.Width, height: config.Height}, delay: 100, replace: true}}
	}
	if result.width < 1 || result.height < 1 || int64(result.width)*int64(result.height) > 64*1024*1024 || len(result.frames) == 0 {
		return result, fmt.Errorf("WebP 크기와 프레임 수를 확인하세요")
	}
	return result, nil
}

func ReadWebPTimeline(ctx context.Context, path string) (GIFTimeline, error) {
	f, err := os.Open(path)
	if err != nil {
		return GIFTimeline{}, err
	}
	defer f.Close()
	source, err := readWebPSource(ctx, f)
	if err != nil {
		return GIFTimeline{}, err
	}
	timeline := GIFTimeline{Width: source.width, Height: source.height, TickRate: 1000}
	for _, frame := range source.frames {
		timeline.Delays = append(timeline.Delays, frame.delay)
	}
	return timeline, nil
}

func webpSourceReader(f *os.File, frame webpSourceFrame) io.Reader {
	if frame.offset == 0 {
		return io.NewSectionReader(f, 0, frame.length)
	}
	var header [30]byte
	copy(header[:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(22+frame.length))
	copy(header[8:], "WEBPVP8X")
	binary.LittleEndian.PutUint32(header[16:20], 10)
	header[20] = 0x10
	put24(header[24:27], frame.width-1)
	put24(header[27:30], frame.height-1)
	return io.MultiReader(bytes.NewReader(header[:]), io.NewSectionReader(f, frame.offset, frame.length))
}

// Retain only the canvas and the current decoded subframe. Earlier frames must
// be composited even when a requested page or crop starts later in the source.
func walkWebP(ctx context.Context, path string, first, end int, emit func(int, *image.NRGBA) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	source, err := readWebPSource(ctx, f)
	if err != nil {
		return err
	}
	if first < 0 || end > len(source.frames) || first >= end {
		return fmt.Errorf("WebP 프레임 범위를 확인하세요")
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, source.width, source.height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(source.background), image.Point{}, draw.Src)
	for n, frame := range source.frames[:end] {
		if err = ctx.Err(); err != nil {
			return err
		}
		config, err := webp.DecodeConfig(webpSourceReader(f, frame))
		if err != nil {
			return err
		}
		if config.Width != frame.width || config.Height != frame.height {
			return fmt.Errorf("WebP 프레임 크기가 일치하지 않습니다")
		}
		decoded, err := webp.Decode(webpSourceReader(f, frame))
		if err != nil {
			return err
		}
		rect := image.Rect(frame.x, frame.y, frame.x+frame.width, frame.y+frame.height)
		op := draw.Over
		if frame.replace {
			op = draw.Src
		}
		draw.Draw(canvas, rect, decoded, decoded.Bounds().Min, op)
		if n >= first {
			if err = emit(n, canvas); err != nil {
				return err
			}
		}
		if frame.dispose {
			draw.Draw(canvas, rect, image.NewUniform(source.background), image.Point{}, draw.Src)
		}
	}
	return nil
}

func (e VideoEditor) WebPFramePage(ctx context.Context, path, dir string, count, offset, limit int) error {
	if offset < 0 || offset >= count || limit < 1 {
		return fmt.Errorf("WebP 프레임 범위를 확인하세요")
	}
	return walkWebP(ctx, path, offset, min(count, offset+limit), func(n int, canvas *image.NRGBA) error {
		w, h := canvas.Bounds().Dx(), canvas.Bounds().Dy()
		if w > 480 || h > 480 {
			if w >= h {
				h = max(1, h*480/w)
				w = 480
			} else {
				w = max(1, w*480/h)
				h = 480
			}
		}
		thumb := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(thumb, thumb.Bounds(), image.Black, image.Point{}, draw.Src)
		xdraw.CatmullRom.Scale(thumb, thumb.Bounds(), canvas, canvas.Bounds(), draw.Over, nil)
		out, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("%012d.jpg", n)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		err = jpeg.Encode(out, thumb, &jpeg.Options{Quality: 85})
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}

func decodeWebPRaw(ctx context.Context, req AnimationRequest, output io.Writer) error {
	return walkWebP(ctx, req.Source, req.FirstFrame, req.FrameCount, func(_ int, canvas *image.NRGBA) error {
		rect := image.Rect(req.Crop.X, req.Crop.Y, req.Crop.X+req.Crop.Width, req.Crop.Y+req.Crop.Height)
		if !rect.In(canvas.Bounds()) {
			return fmt.Errorf("크롭 영역을 확인하세요")
		}
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			start := canvas.PixOffset(rect.Min.X, y)
			if _, err := output.Write(canvas.Pix[start : start+rect.Dx()*4]); err != nil {
				return err
			}
		}
		return nil
	})
}
