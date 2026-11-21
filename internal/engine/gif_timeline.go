package engine

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// GIFTimeline reads container timing without allocating decoded source frames.
type GIFTimeline struct {
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	DelayOffsets []int64 `json:"-"`
	Delays       []int   `json:"delays"` // hundredths of a second
}

func ReadGIFTimeline(ctx context.Context, path string) (GIFTimeline, error) {
	var result GIFTimeline
	f, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var header [13]byte
	if _, err = io.ReadFull(r, header[:]); err != nil {
		return result, err
	}
	if string(header[:6]) != "GIF87a" && string(header[:6]) != "GIF89a" {
		return result, fmt.Errorf("GIF 형식이 아닙니다")
	}
	result.Width = int(binary.LittleEndian.Uint16(header[6:8]))
	result.Height = int(binary.LittleEndian.Uint16(header[8:10]))
	if result.Width < 1 || result.Height < 1 {
		return result, fmt.Errorf("GIF 크기를 확인하세요")
	}
	discard := func(n int) error { _, err := io.CopyN(io.Discard, r, int64(n)); return err }
	if header[10]&0x80 != 0 {
		if err = discard(3 * (1 << ((header[10] & 7) + 1))); err != nil {
			return result, err
		}
	}
	blocks := func() error {
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			n, err := r.ReadByte()
			if err != nil {
				return err
			}
			if n == 0 {
				return nil
			}
			if err = discard(int(n)); err != nil {
				return err
			}
		}
	}
	delay := 1
	delayOffset := int64(-1)
	for {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		tag, err := r.ReadByte()
		if err != nil {
			return result, err
		}
		switch tag {
		case 0x3b:
			if len(result.Delays) == 0 {
				return result, fmt.Errorf("GIF에 프레임이 없습니다")
			}
			return result, nil
		case 0x21:
			label, err := r.ReadByte()
			if err != nil {
				return result, err
			}
			if label == 0xf9 {
				position, err := f.Seek(0, io.SeekCurrent)
				if err != nil {
					return result, err
				}
				delayOffset = position - int64(r.Buffered()) + 2
				var gce [6]byte
				if _, err = io.ReadFull(r, gce[:]); err != nil {
					return result, err
				}
				if gce[0] != 4 || gce[5] != 0 {
					return result, fmt.Errorf("GIF 프레임 제어 정보 오류")
				}
				delay = max(1, int(binary.LittleEndian.Uint16(gce[2:4])))
			} else if err = blocks(); err != nil {
				return result, err
			}
		case 0x2c:
			var descriptor [9]byte
			if _, err = io.ReadFull(r, descriptor[:]); err != nil {
				return result, err
			}
			x, y, w, h := int(binary.LittleEndian.Uint16(descriptor[:2])), int(binary.LittleEndian.Uint16(descriptor[2:4])), int(binary.LittleEndian.Uint16(descriptor[4:6])), int(binary.LittleEndian.Uint16(descriptor[6:8]))
			if w < 1 || h < 1 || x > result.Width-w || y > result.Height-h {
				return result, fmt.Errorf("GIF 프레임 크기 오류")
			}
			if descriptor[8]&0x80 != 0 {
				if err = discard(3 * (1 << ((descriptor[8] & 7) + 1))); err != nil {
					return result, err
				}
			}
			if _, err = r.ReadByte(); err != nil {
				return result, err
			}
			if err = blocks(); err != nil {
				return result, err
			}
			result.Delays = append(result.Delays, delay)
			result.DelayOffsets = append(result.DelayOffsets, delayOffset)
			delayOffset = -1
			delay = 1
		default:
			return result, fmt.Errorf("GIF 데이터가 올바르지 않습니다")
		}
	}
}
func (g GIFTimeline) Duration() float64 {
	var ticks int64
	for _, d := range g.Delays {
		ticks += int64(d)
	}
	return float64(ticks) / 100
}

func (e VideoEditor) GIFFramePage(ctx context.Context, source, dir string, count, offset, limit int) error {
	if count < 1 || offset < 0 || offset >= count || limit < 1 {
		return fmt.Errorf("GIF 프레임 범위를 확인하세요")
	}
	filter := fmt.Sprintf("trim=start_frame=%d:end_frame=%d,setpts=N/(100*TB),scale=480:480:force_original_aspect_ratio=decrease,setsar=1", offset, min(count, offset+limit))
	return e.streamRun(ctx, dir, []string{"-ignore_loop", "1", "-min_delay", "0", "-default_delay", "1", "-protocol_whitelist", "file,pipe", "-i", source, "-an", "-vf", filter, "-fps_mode", "passthrough", "-frames:v", strconv.Itoa(min(limit, count-offset)), "-start_number", strconv.Itoa(offset), "-q:v", "3", filepath.Join(dir, "%012d.jpg")}, nil, nil, 0, nil)
}

func (e VideoEditor) verifyGIF(ctx context.Context, path string) (LocalMetadata, int, error) {
	timeline, err := ReadGIFTimeline(ctx, path)
	if err != nil {
		return LocalMetadata{}, 0, err
	}
	err = e.streamRun(ctx, filepath.Dir(path), []string{"-ignore_loop", "1", "-min_delay", "0", "-default_delay", "1", "-protocol_whitelist", "file,pipe", "-i", path, "-map", "0:v:0", "-f", "null", "-"}, nil, nil, 0, nil)
	if err != nil {
		return LocalMetadata{}, 0, err
	}
	duration := timeline.Duration()
	return LocalMetadata{Kind: "image", Format: "gif", Width: &timeline.Width, Height: &timeline.Height, Duration: &duration}, len(timeline.Delays), nil
}

// Timing stays metadata-only; all decoded pixels continue through bounded pipes.
func gifTiming(ctx context.Context, req AnimationRequest) (float64, int, error) {
	skip := func(int) (bool, error) { return false, nil }
	if req.NewSkip != nil {
		skip = req.NewSkip()
	}
	ticks, n, last := int64(0), 0, 1
	for index := req.FirstFrame; index < req.FrameCount; index++ {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		drop, err := skip(index)
		if err != nil {
			return 0, 0, err
		}
		if drop {
			continue
		}
		if index >= len(req.Delays) || req.Delays[index] < 1 || req.Delays[index] > 65535 {
			return 0, 0, fmt.Errorf("GIF 재생 시간 정보 오류")
		}
		last = req.Delays[index]
		ticks += int64(last)
		n++
	}
	if n != req.ExpectedFrames {
		return 0, 0, fmt.Errorf("GIF 출력 프레임 수가 다릅니다")
	}
	return float64(ticks) / 100, last, nil
}

func fixGIFDelays(ctx context.Context, path string, req AnimationRequest) error {
	timeline, err := ReadGIFTimeline(ctx, path)
	if err != nil {
		return err
	}
	if len(timeline.Delays) != req.ExpectedFrames {
		return fmt.Errorf("GIF 출력 프레임 수가 다릅니다")
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	skip := func(int) (bool, error) { return false, nil }
	if req.NewSkip != nil {
		skip = req.NewSkip()
	}
	n := 0
	for index := req.FirstFrame; index < req.FrameCount; index++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		drop, err := skip(index)
		if err != nil {
			return err
		}
		if drop {
			continue
		}
		if n >= len(timeline.DelayOffsets) || timeline.DelayOffsets[n] < 0 {
			return fmt.Errorf("GIF 프레임 시간 정보가 없습니다")
		}
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], uint16(req.Delays[index]))
		if _, err = f.WriteAt(b[:], timeline.DelayOffsets[n]); err != nil {
			return err
		}
		n++
	}
	return f.Sync()
}
