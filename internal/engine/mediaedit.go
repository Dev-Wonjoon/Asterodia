package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// VideoEditor owns FFmpeg invocation, never database or publication decisions.
type VideoEditor struct{ FFmpeg, FFprobe string }
type EditCrop struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func (e VideoEditor) runEdit(ctx context.Context, dir string, args []string, duration float64, progress func(int)) error {
	if !filepath.IsAbs(e.FFmpeg) {
		return fmt.Errorf("설정에서 FFmpeg를 설치하세요")
	}
	base := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-xerror", "-n", "-threads", "2", "-filter_threads", "1", "-filter_complex_threads", "1", "-progress", "pipe:1", "-nostats"}
	if len(args) == 0 {
		return fmt.Errorf("편집 출력 인자가 필요합니다")
	}
	base = append(base, args[:len(args)-1]...)
	base = append(base, "-threads", "2", args[len(args)-1])
	result, err := new(ProcessRunner).Run(ctx, ProcessRequest{Executable: e.FFmpeg, Directory: dir, Timeout: 20 * time.Minute, Arguments: base, StreamOutput: true, OnStdoutLine: func(line string) error {
		if strings.HasPrefix(line, "out_time_us=") && duration > 0 && progress != nil {
			n, _ := strconv.ParseFloat(strings.TrimPrefix(line, "out_time_us="), 64)
			progress(max(0, min(99, int(n/1e6/duration*100))))
		}
		return nil
	}})
	if err != nil {
		return fmt.Errorf("%w: %s", err, result.Stderr)
	}
	return err
}

func (e VideoEditor) Prepare(ctx context.Context, source, dir string, start, end float64, fps, width int, progress func(int)) error {
	if !filepath.IsAbs(source) || !filepath.IsAbs(dir) || !finiteRange(start, end) || end-start > 30 || fps < 1 || fps > 30 || width < 64 || width > 960 || math.Ceil((end-start)*float64(fps)) > 900 {
		return fmt.Errorf("GIF 프레임 준비 범위를 확인하세요")
	}
	// Square bounds preserve aspect ratio, including portrait videos. Frame IDs
	// refer to these immutable PNGs for both preview and final encoding.
	filter := fmt.Sprintf("fps=%d,scale=%d:%d:force_original_aspect_ratio=decrease,setsar=1", fps, width, width)
	args := []string{"-ss", number(start), "-protocol_whitelist", "file,pipe", "-i", source, "-t", number(end - start), "-map", "0:V:0", "-an", "-sn", "-dn", "-vf", filter, "-frames:v", "900", "-start_number", "0", filepath.Join(dir, "%04d.png")}
	return e.runEdit(ctx, dir, args, end-start, progress)
}

func (e VideoEditor) GIF(ctx context.Context, dir, destination string, fps, count int, crop EditCrop, progress func(int)) error {
	if fps < 1 || fps > 30 || count < 1 || count > 900 || crop.X < 0 || crop.Y < 0 || crop.Width < 1 || crop.Height < 1 || crop.Width > 960 || crop.Height > 960 {
		return fmt.Errorf("GIF 출력 옵션을 확인하세요")
	}
	cropFilter := fmt.Sprintf("crop=%d:%d:%d:%d:exact=1", crop.Width, crop.Height, crop.X, crop.Y)
	input := []string{"-framerate", strconv.Itoa(fps), "-start_number", "0", "-i", filepath.Join(dir, "%04d.png")}
	palette := filepath.Join(dir, "palette.png")
	// A split/palettegen/paletteuse graph buffers the complete uncompressed
	// animation. Two passes keep frame memory bounded even at the 900-frame cap.
	first := append(append([]string{}, input...), "-vf", cropFilter+",palettegen=reserve_transparent=0", "-frames:v", "1", "-update", "1", palette)
	if err := e.runEdit(ctx, dir, first, float64(count)/float64(fps), func(p int) {
		if progress != nil {
			progress(p * 30 / 100)
		}
	}); err != nil {
		return err
	}
	args := append(append([]string{}, input...), "-i", palette, "-filter_complex", "[0:v]"+cropFilter+"[v];[v][1:v]paletteuse=dither=sierra2_4a", "-frames:v", strconv.Itoa(count), "-loop", "0", "-gifflags", "-offsetting", destination)
	return e.runEdit(ctx, dir, args, float64(count)/float64(fps), func(p int) {
		if progress != nil {
			progress(30 + p*70/100)
		}
	})
}

func (e VideoEditor) Trim(ctx context.Context, source, destination string, start, end float64, mute bool, progress func(int)) error {
	if !finiteRange(start, end) {
		return fmt.Errorf("영상 구간을 확인하세요")
	}
	metadata, err := e.ProbeVideo(ctx, source)
	if err != nil {
		return err
	}
	pixelFormat := "yuv420p"
	if *metadata.Width%2 != 0 || *metadata.Height%2 != 0 {
		pixelFormat = "yuv444p"
	}
	args := []string{"-ss", number(start), "-protocol_whitelist", "file,pipe", "-i", source, "-t", number(end - start), "-map", "0:V:0", "-sn", "-dn"}
	if mute {
		args = append(args, "-an")
	} else {
		args = append(args, "-map", "0:a:0?", "-c:a", "aac", "-b:a", "192k")
	}
	args = append(args, "-vf", "setsar=1", "-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-pix_fmt", pixelFormat, "-threads:v", "2", "-map_metadata", "-1", "-movflags", "+faststart", destination)
	return e.streamRun(ctx, filepath.Dir(destination), args, nil, nil, end-start, progress)
}

func finiteRange(start, end float64) bool {
	return !math.IsNaN(start) && !math.IsNaN(end) && !math.IsInf(start, 0) && !math.IsInf(end, 0) && start >= 0 && end > start
}
func number(n float64) string { return strconv.FormatFloat(n, 'f', 6, 64) }

func (e VideoEditor) Verify(ctx context.Context, path string) (LocalMetadata, int, error) {
	if strings.EqualFold(filepath.Ext(path), ".gif") {
		return e.verifyGIF(ctx, path)
	}
	if strings.EqualFold(filepath.Ext(path), ".webp") {
		return e.VerifyWebP(ctx, path)
	}
	var probe struct {
		Streams []struct {
			CodecType     string `json:"codec_type"`
			Width, Height int
			Count         string `json:"nb_read_frames"`
		}
		Format struct{ Duration string }
	}
	result, err := new(ProcessRunner).Run(ctx, ProcessRequest{Executable: e.FFprobe, Directory: filepath.Dir(path), Unbounded: true, Arguments: []string{"-v", "error", "-protocol_whitelist", "file,pipe", "-count_frames", "-show_entries", "stream=codec_type,width,height,nb_read_frames:format=duration", "-of", "json", path}})
	if err != nil {
		return LocalMetadata{}, 0, err
	}
	if err = json.Unmarshal([]byte(result.Stdout), &probe); err != nil {
		return LocalMetadata{}, 0, err
	}
	for _, v := range probe.Streams {
		if v.CodecType == "video" && v.Width > 0 && v.Height > 0 {
			duration, _ := strconv.ParseFloat(probe.Format.Duration, 64)
			count, _ := strconv.Atoi(v.Count)
			if count < 1 || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 {
				return LocalMetadata{}, 0, fmt.Errorf("결과 프레임과 재생 시간을 확인할 수 없습니다")
			}
			kind, format := "video", "mp4"
			if strings.HasSuffix(path, ".gif") {
				kind, format = "image", "gif"
			}
			return LocalMetadata{Kind: kind, Format: format, Width: &v.Width, Height: &v.Height, Duration: &duration}, count, nil
		}
	}
	return LocalMetadata{}, 0, fmt.Errorf("결과 영상 스트림을 확인할 수 없습니다")
}
