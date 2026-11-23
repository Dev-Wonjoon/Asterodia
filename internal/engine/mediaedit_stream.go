package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type AnimationQuality struct {
	Preset     string `json:"preset"`
	Quality    int    `json:"quality"`
	Effort     string `json:"effort"`
	Lossless   bool   `json:"lossless"`
	Dither     string `json:"dither,omitempty"`
	WebPPreset string `json:"webpPreset,omitempty"`
}
type FrameRange struct {
	First int `json:"first"`
	Last  int `json:"last"`
}
type AnimationRequest struct {
	Source          string
	Start, End      float64
	FPS, FrameCount int
	Crop            EditCrop
	Format          string
	Quality         AnimationQuality
	// NewSkip is recreated for each palette/encoding pass and advances in order.
	NewSkip         func() func(int) (bool, error)
	ExpectedFrames  int
	FirstFrame      int
	Output          io.Writer
	NativeGIF       bool
	NativeVideo     bool
	DelayRate       int
	Delays          []int
	Duration        float64
	PictureSequence bool
}

func frameFilter(fps, count int) string {
	// One fixed timeline is shared by lazy previews and both encoding passes.
	return fmt.Sprintf("fps=%d:start_time=0:round=up:eof_action=pass,tpad=stop_mode=clone:stop=-1,trim=end_frame=%d,setpts=N/(%d*TB)", fps, count, fps)
}

func (e VideoEditor) streamRun(ctx context.Context, dir string, args []string, input io.Reader, output io.Writer, duration float64, progress func(int)) error {
	base := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-xerror", "-n", "-threads", "2", "-filter_threads", "1", "-filter_complex_threads", "1"}
	if progress != nil {
		base = append(base, "-progress", "pipe:2", "-nostats")
	}
	base = append(base, args...)
	result, err := new(ProcessRunner).Run(ctx, ProcessRequest{Executable: e.FFmpeg, Directory: dir, Unbounded: true, Arguments: base, Input: input, Output: output, StreamOutput: true, OnStderrLine: func(line string) error {
		if progress != nil && strings.HasPrefix(line, "out_time_us=") && duration > 0 {
			n, _ := strconv.ParseFloat(strings.TrimPrefix(line, "out_time_us="), 64)
			progress(max(0, min(99, int(n/1e6/duration*100))))
		}
		return nil
	}})
	if err != nil {
		return fmt.Errorf("%w: %s", err, result.Stderr)
	}
	return nil
}

// FramePage emits only a small display-sized page, never full-size frame caches.
func (e VideoEditor) FramePage(ctx context.Context, source, dir string, start, end float64, fps, count, offset, limit int) error {
	if !finiteRange(start, end) || fps < 1 || count < 1 || offset < 0 || limit < 1 || offset >= count {
		return fmt.Errorf("프레임 범위를 확인하세요")
	}
	limit = min(limit, count-offset)
	filter := frameFilter(fps, count) + fmt.Sprintf(",select='between(n,%d,%d)',scale=480:480:force_original_aspect_ratio=decrease,setsar=1", offset, offset+limit-1)
	args := []string{"-ss", number(start), "-t", number(end - start), "-protocol_whitelist", "file,pipe", "-i", source, "-map", "0:V:0", "-an", "-sn", "-dn", "-vf", filter, "-fps_mode", "passthrough", "-frames:v", strconv.Itoa(limit), "-start_number", strconv.Itoa(offset), "-q:v", "3", "-threads", "2", filepath.Join(dir, "%012d.jpg")}
	return e.streamRun(ctx, dir, args, nil, nil, 0, nil)
}

// frameStream drops frames chunk-by-chunk. Even a 4K frame is never buffered in Go.
type frameStream struct {
	ctx        context.Context
	out        io.Writer
	frameBytes int64
	remaining  int64
	index      int
	skip       func(int) (bool, error)
	drop       bool
}

func (s *frameStream) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		if err := s.ctx.Err(); err != nil {
			return total, err
		}
		if s.remaining == 0 {
			s.remaining = s.frameBytes
			var err error
			s.drop, err = s.skip(s.index)
			if err != nil {
				return total, err
			}
		}
		n := int(min(int64(len(p)), s.remaining))
		if !s.drop {
			written, err := s.out.Write(p[:n])
			if err != nil {
				return total + written, err
			}
			if written != n {
				return total + written, io.ErrShortWrite
			}
		}
		p = p[n:]
		total += n
		s.remaining -= int64(n)
		if s.remaining == 0 {
			s.index++
		}
	}
	return total, nil
}

func (e VideoEditor) animationPass(ctx context.Context, req AnimationRequest, dir string, encoderArgs []string, progress func(int)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	skip := func(int) (bool, error) { return false, nil }
	if req.NewSkip != nil {
		skip = req.NewSkip()
	}
	pixelFormat := "rgb24"
	channels := int64(3)
	if req.NativeGIF || req.NativeVideo {
		pixelFormat = "rgba"
		channels = 4
	}
	stream := &frameStream{ctx: ctx, out: writer, index: req.FirstFrame, frameBytes: int64(req.Crop.Width) * int64(req.Crop.Height) * channels, skip: skip}
	if stream.frameBytes <= 0 {
		return fmt.Errorf("출력 프레임 크기를 확인하세요")
	}
	filter := frameFilter(req.FPS, req.FrameCount) + fmt.Sprintf(",trim=start_frame=%d,setpts=N/(%d*TB),crop=%d:%d:%d:%d:exact=1,setsar=1,format=rgb24", req.FirstFrame, req.FPS, req.Crop.Width, req.Crop.Height, req.Crop.X, req.Crop.Y)
	decode := []string{"-ss", number(req.Start), "-t", number(req.End - req.Start), "-protocol_whitelist", "file,pipe", "-i", req.Source, "-map", "0:V:0", "-an", "-sn", "-dn", "-vf", filter, "-fps_mode", "passthrough", "-frames:v", strconv.Itoa(req.FrameCount), "-threads", "2", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1"}
	if req.PictureSequence {
		decode = append([]string{"-framerate", strconv.Itoa(req.FPS), "-start_number", "0", "-protocol_whitelist", "file,pipe", "-i", req.Source}, decode[10:]...)
	}
	if req.NativeGIF {
		filter = fmt.Sprintf("trim=start_frame=%d:end_frame=%d,setpts=N/(100*TB),crop=%d:%d:%d:%d:exact=1,setsar=1,format=rgba", req.FirstFrame, req.FrameCount, req.Crop.Width, req.Crop.Height, req.Crop.X, req.Crop.Y)
		decode = []string{"-ignore_loop", "1", "-min_delay", "0", "-default_delay", "1", "-protocol_whitelist", "file,pipe", "-i", req.Source, "-an", "-vf", filter, "-fps_mode", "passthrough", "-frames:v", strconv.Itoa(req.FrameCount - req.FirstFrame), "-f", "rawvideo", "-pix_fmt", "rgba", "pipe:1"}
	}
	if req.NativeVideo {
		filter = fmt.Sprintf("trim=start_frame=%d:end_frame=%d,setpts=N/TB,crop=%d:%d:%d:%d:exact=1,setsar=1,format=rgba", req.FirstFrame, req.FrameCount, req.Crop.Width, req.Crop.Height, req.Crop.X, req.Crop.Y)
		decode = []string{"-ss", number(req.Start), "-t", number(req.End - req.Start), "-protocol_whitelist", "file,pipe", "-i", req.Source, "-map", "0:V:0", "-an", "-sn", "-dn", "-vf", filter, "-fps_mode", "passthrough", "-frames:v", strconv.Itoa(req.FrameCount - req.FirstFrame), "-f", "rawvideo", "-pix_fmt", "rgba", "pipe:1"}
	}
	inputFPS := req.FPS
	if req.NativeVideo {
		inputFPS = 100
	} // Exact delays are applied by the GIF/WebP container writer.
	input := []string{"-f", "rawvideo", "-pixel_format", pixelFormat, "-video_size", fmt.Sprintf("%dx%d", req.Crop.Width, req.Crop.Height), "-framerate", strconv.Itoa(inputFPS), "-i", "pipe:0"}
	var decodeErr error
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		decodeErr = e.streamRun(ctx, dir, decode, nil, stream, 0, nil)
		if decodeErr == nil && stream.remaining != 0 {
			decodeErr = io.ErrUnexpectedEOF
		}
		writer.CloseWithError(decodeErr)
	}()
	duration := float64(req.ExpectedFrames) / float64(req.FPS)
	if req.NativeGIF || req.NativeVideo {
		duration = req.Duration
	}
	err := e.streamRun(ctx, dir, append(input, encoderArgs...), reader, req.Output, duration, progress)
	reader.Close()
	cancel()
	wait.Wait()
	if err != nil {
		return errors.Join(err, decodeErr)
	}
	if decodeErr != nil {
		return decodeErr
	}
	if stream.index != req.FrameCount {
		return fmt.Errorf("원본 프레임 수가 변경됐습니다")
	}
	return nil
}

func NormalizeAnimationQuality(format string, q AnimationQuality) (AnimationQuality, error) {
	if q.Dither == "auto" {
		q.Dither = ""
	}
	if q.WebPPreset == "none" {
		q.WebPPreset = ""
	}
	switch q.Dither {
	case "", "sierra2_4a", "sierra3", "floyd_steinberg", "bayer", "atkinson", "none":
	default:
		return q, fmt.Errorf("GIF 디더링 방식을 확인하세요")
	}
	switch q.WebPPreset {
	case "", "photo", "drawing", "icon", "text":
	default:
		return q, fmt.Errorf("WebP 최적화 유형을 확인하세요")
	}
	if format != "gif" && q.Dither != "" || format != "webp" && q.WebPPreset != "" {
		return q, fmt.Errorf("출력 형식에 맞는 고급 설정을 선택하세요")
	}
	if q.Preset == "" {
		q.Preset = "balanced"
	}
	if q.Effort == "" {
		q.Effort = "normal"
	}
	if q.Effort != "fast" && q.Effort != "normal" && q.Effort != "precise" {
		return q, fmt.Errorf("처리 방식을 확인하세요")
	}
	switch q.Preset {
	case "small":
		q.Quality = 60
	case "balanced":
		q.Quality = 80
	case "high":
		q.Quality = 95
	case "max":
		q.Quality = 100
	case "lossless":
		q.Quality = 80
		q.Lossless = true
	case "custom":
	default:
		return q, fmt.Errorf("화질 프리셋을 확인하세요")
	}
	if q.Quality < 1 || q.Quality > 100 || q.Lossless && format != "webp" {
		return q, fmt.Errorf("화질 또는 무손실 옵션을 확인하세요")
	}
	if q.Lossless && q.WebPPreset != "" {
		return q, fmt.Errorf("WebP 무손실에서는 최적화 유형을 기본으로 선택하세요")
	}
	if q.WebPPreset != "" {
		q.Effort = "normal"
	}
	return q, nil
}

func (e VideoEditor) Animation(ctx context.Context, req AnimationRequest, destination string, progress func(int)) error {
	if !finiteRange(req.Start, req.End) || req.FPS < 1 || req.FrameCount < 1 || req.ExpectedFrames < 1 || req.Crop.Width < 1 || req.Crop.Height < 1 {
		return fmt.Errorf("애니메이션 옵션을 확인하세요")
	}
	q, err := NormalizeAnimationQuality(req.Format, req.Quality)
	if err != nil {
		return err
	}
	dir := filepath.Dir(destination)
	if req.NativeVideo && req.Format == "gif" {
		var err error
		req, err = videoGIFTiming(ctx, req)
		if err != nil {
			return err
		}
	}
	lastDelay := 0
	if req.NativeGIF || req.NativeVideo {
		req.Duration, lastDelay, err = gifTiming(ctx, req)
		if err != nil {
			return err
		}
	}
	if req.Format == "webp" {
		if req.NativeGIF || req.NativeVideo {
			return e.gifWebP(ctx, req, destination, q, progress)
		}
		level := 4
		if q.Effort == "fast" {
			level = 1
		} else if q.Effort == "precise" {
			level = 6
		}
		lossless := "0"
		pixelFormat := "yuv420p"
		if q.Lossless {
			lossless = "1"
		}
		if req.NativeGIF {
			pixelFormat = "yuva420p"
		}
		if q.Lossless {
			pixelFormat = "bgra"
		}
		args := []string{"-c:v", "libwebp_anim"}
		if q.WebPPreset != "" {
			args = append(args, "-preset", q.WebPPreset)
		}
		args = append(args, "-lossless", lossless, "-quality", strconv.Itoa(q.Quality), "-compression_level", strconv.Itoa(level), "-pix_fmt", pixelFormat, "-loop", "0", "-threads", "2", destination)
		if err = e.animationPass(ctx, req, dir, args, progress); err != nil {
			return err
		}
		return nil
	}
	if req.Format != "gif" {
		return fmt.Errorf("출력 형식을 확인하세요")
	}
	colors := 256
	if q.Quality < 90 {
		colors = max(16, 16+int(math.Round(float64(q.Quality-1)/89*240)))
	}
	palette := filepath.Join(dir, "palette.png")
	transparent := 0
	if req.NativeGIF {
		transparent = 1
	}
	first := []string{"-vf", fmt.Sprintf("palettegen=max_colors=%d:reserve_transparent=%d", colors, transparent), "-frames:v", "1", "-update", "1", "-threads", "2", palette}
	if err = e.animationPass(ctx, req, dir, first, func(p int) {
		if progress != nil {
			progress(p * 30 / 100)
		}
	}); err != nil {
		return err
	}
	dither := "sierra2_4a"
	if q.Effort == "fast" {
		dither = "bayer:bayer_scale=3"
	} else if q.Effort == "precise" {
		dither = "sierra3"
	}
	if q.Dither != "" {
		dither = q.Dither
	}
	if dither == "none" {
		dither = "0"
	}
	if q.Dither == "bayer" {
		dither = "bayer:bayer_scale=3"
	}
	second := []string{"-i", palette, "-filter_complex", "[0:v][1:v]paletteuse=dither=" + dither, "-loop", "0", "-gifflags", "-offsetting", "-threads", "2", destination}
	if req.NativeGIF || req.NativeVideo {
		second = []string{"-i", palette, "-filter_complex", "[0:v][1:v]paletteuse=dither=" + dither, "-fps_mode", "passthrough", "-enc_time_base", "1:100", "-frames:v", strconv.Itoa(req.ExpectedFrames), "-final_delay", strconv.Itoa(lastDelay), "-loop", "0", "-gifflags", "-offsetting", "-threads", "2", destination}
	}
	err = e.animationPass(ctx, req, dir, second, func(p int) {
		if progress != nil {
			progress(30 + p*70/100)
		}
	})
	if err != nil {
		return err
	}
	if req.NativeGIF || req.NativeVideo {
		return fixGIFDelays(ctx, destination, req)
	}
	return nil
}

// ProbeVideo reads display dimensions, including rotation, without full decoding.
func (e VideoEditor) ProbeVideo(ctx context.Context, path string) (LocalMetadata, error) {
	var data struct {
		Streams []struct {
			CodecType     string `json:"codec_type"`
			CodecName     string `json:"codec_name"`
			FrameCount    string `json:"nb_frames"`
			Width, Height int
			SideData      []struct{ Rotation int } `json:"side_data_list"`
		}
		Format struct{ Duration string }
	}
	result, err := new(ProcessRunner).Run(ctx, ProcessRequest{Executable: e.FFprobe, Directory: filepath.Dir(path), Timeout: 30 * time.Second, Arguments: []string{"-v", "error", "-protocol_whitelist", "file,pipe", "-show_entries", "stream=codec_type,codec_name,width,height,nb_frames:stream_side_data=rotation:format=duration", "-of", "json", path}})
	if err != nil {
		return LocalMetadata{}, err
	}
	if err = json.Unmarshal([]byte(result.Stdout), &data); err != nil {
		return LocalMetadata{}, err
	}
	for _, v := range data.Streams {
		if v.CodecType == "video" && v.Width > 0 && v.Height > 0 {
			for _, s := range v.SideData {
				if s.Rotation%180 != 0 {
					v.Width, v.Height = v.Height, v.Width
					break
				}
			}
			d, err := strconv.ParseFloat(data.Format.Duration, 64)
			if err != nil || !finiteRange(0, d) {
				return LocalMetadata{}, fmt.Errorf("재생 시간을 확인할 수 없습니다")
			}
			metadata := LocalMetadata{Kind: "video", Width: &v.Width, Height: &v.Height, Duration: &d, VideoCodec: v.CodecName}
			if count, err := strconv.Atoi(v.FrameCount); err == nil && count > 0 {
				metadata.FrameCount = &count
			}
			return metadata, nil
		}
	}
	return LocalMetadata{}, fmt.Errorf("영상 스트림을 찾지 못했습니다")
}
