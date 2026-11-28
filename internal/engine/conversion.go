package engine

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// FormatOptions describes the output, independently of the downloaded source.
type FormatOptions struct {
	Type       string `json:"type"`
	Extension  string `json:"extension"`
	VideoCodec string `json:"videoCodec,omitempty"`
	AudioCodec string `json:"audioCodec,omitempty"`
}

type OutputFormat struct {
	Type        string   `json:"type"`
	Extension   string   `json:"extension"`
	VideoCodecs []string `json:"videoCodecs"`
	AudioCodecs []string `json:"audioCodecs"`
}

var outputFormats = []OutputFormat{
	{"video", "mp4", []string{"copy", "h264", "hevc", "av1"}, []string{"copy", "aac", "mp3"}},
	{"video", "mkv", []string{"copy", "h264", "hevc", "vp9", "av1"}, []string{"copy", "aac", "mp3", "opus", "vorbis", "flac", "pcm"}},
	{"video", "webm", []string{"vp9", "av1"}, []string{"opus", "vorbis"}},
	{"video", "mov", []string{"h264", "hevc"}, []string{"aac", "pcm"}},
	{"audio", "m4a", nil, []string{"aac"}},
	{"audio", "mp3", nil, []string{"mp3"}},
	{"audio", "ogg", nil, []string{"opus", "vorbis", "flac"}},
	{"audio", "webm", nil, []string{"opus", "vorbis"}},
	{"audio", "flac", nil, []string{"flac"}},
	{"audio", "wav", nil, []string{"pcm"}},
}

var encoderNames = map[string]string{"h264": "libx264", "hevc": "libx265", "vp9": "libvpx-vp9", "av1": "libsvtav1", "aac": "aac", "mp3": "libmp3lame", "opus": "libopus", "vorbis": "libvorbis", "flac": "flac", "pcm": "pcm_s16le"}

func containsCodec(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func NormalizeFormat(o FormatOptions) (FormatOptions, error) {
	if o.Type == "" {
		o.Type = "video"
	}
	if o.Extension == "" {
		if o.Type == "audio" {
			o.Extension = "mp3"
		} else {
			o.Extension = "mp4"
		}
	}
	for _, f := range outputFormats {
		if f.Type != o.Type || f.Extension != o.Extension {
			continue
		}
		if o.Type == "audio" && o.VideoCodec != "" {
			return o, fmt.Errorf("오디오 출력에는 영상 코덱을 지정할 수 없습니다")
		}
		if o.Type == "video" && o.VideoCodec == "" {
			o.VideoCodec = f.VideoCodecs[0]
		}
		if o.AudioCodec == "" {
			o.AudioCodec = f.AudioCodecs[0]
		}
		if (o.Type == "video" && !containsCodec(f.VideoCodecs, o.VideoCodec)) || !containsCodec(f.AudioCodecs, o.AudioCodec) {
			return o, fmt.Errorf("선택한 컨테이너와 코덱이 호환되지 않습니다")
		}
		return o, nil
	}
	return o, fmt.Errorf("지원하지 않는 출력 형식입니다")
}

// AvailableFormats reports only encoders and muxers provided by the managed binary.
func (e VideoEditor) AvailableFormats(ctx context.Context) ([]OutputFormat, error) {
	if !filepath.IsAbs(e.FFmpeg) {
		return nil, fmt.Errorf("설정에서 FFmpeg를 설치하세요")
	}
	encoders, err := new(ProcessRunner).Run(ctx, ProcessRequest{Executable: e.FFmpeg, Directory: filepath.Dir(e.FFmpeg), Timeout: 15 * time.Second, Arguments: []string{"-hide_banner", "-encoders"}})
	if err != nil {
		return nil, err
	}
	muxers, err := new(ProcessRunner).Run(ctx, ProcessRequest{Executable: e.FFmpeg, Directory: filepath.Dir(e.FFmpeg), Timeout: 15 * time.Second, Arguments: []string{"-hide_banner", "-muxers"}})
	if err != nil {
		return nil, err
	}
	available := map[string]bool{}
	for _, line := range strings.Split(encoders.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			available[fields[1]] = true
		}
	}
	mux := map[string]bool{}
	for _, line := range strings.Split(muxers.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "E" {
			mux[fields[1]] = true
		}
	}
	result := []OutputFormat{}
	for _, f := range outputFormats {
		name := map[string]string{"mkv": "matroska", "m4a": "ipod"}[f.Extension]
		if name == "" {
			name = f.Extension
		}
		if !mux[name] {
			continue
		}
		v := OutputFormat{Type: f.Type, Extension: f.Extension, VideoCodecs: []string{}, AudioCodecs: []string{}}
		for _, codec := range f.VideoCodecs {
			if codec == "copy" || available[encoderNames[codec]] {
				v.VideoCodecs = append(v.VideoCodecs, codec)
			}
		}
		for _, codec := range f.AudioCodecs {
			if codec == "copy" || available[encoderNames[codec]] {
				v.AudioCodecs = append(v.AudioCodecs, codec)
			}
		}
		if len(v.AudioCodecs) > 0 && (v.Type == "audio" || len(v.VideoCodecs) > 0) {
			result = append(result, v)
		}
	}
	if available["libwebp_anim"] && mux["webp"] {
		result = append(result, OutputFormat{Type: "image", Extension: "webp", VideoCodecs: []string{"webp"}, AudioCodecs: []string{}})
	}
	return result, nil
}

func (e VideoEditor) CheckFormat(ctx context.Context, o FormatOptions) error {
	o, err := NormalizeFormat(o)
	if err != nil {
		return err
	}
	formats, err := e.AvailableFormats(ctx)
	if err != nil {
		return err
	}
	for _, f := range formats {
		if f.Type == o.Type && f.Extension == o.Extension && containsCodec(f.AudioCodecs, o.AudioCodec) && (o.Type == "audio" || containsCodec(f.VideoCodecs, o.VideoCodec)) {
			return nil
		}
	}
	return fmt.Errorf("설치된 FFmpeg가 선택한 코덱·컨테이너를 지원하지 않습니다")
}

func conversionArguments(source, destination string, o FormatOptions) ([]string, error) {
	o, err := NormalizeFormat(o)
	if err != nil {
		return nil, err
	}
	args := []string{"-protocol_whitelist", "file,pipe", "-i", source, "-map_metadata", "-1", "-map_chapters", "-1"}
	if o.Type == "audio" {
		args = append(args, "-map", "0:a:0", "-vn")
	} else {
		codec := encoderNames[o.VideoCodec]
		if o.VideoCodec == "copy" {
			codec = "copy"
		}
		args = append(args, "-map", "0:v:0", "-map", "0:a:0?", "-c:v", codec)
		if o.VideoCodec != "copy" {
			args = append(args, "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2", "-pix_fmt", "yuv420p")
		}
		switch o.VideoCodec {
		case "h264", "hevc":
			args = append(args, "-preset", "medium", "-crf", "23")
			if o.VideoCodec == "hevc" {
				args = append(args, "-x265-params", "pools=2:frame-threads=2:log-level=error")
			}
		case "vp9":
			args = append(args, "-crf", "32", "-b:v", "0", "-row-mt", "1")
		case "av1":
			args = append(args, "-preset", "8", "-crf", "32", "-svtav1-params", "lp=2")
		}
	}
	codec := encoderNames[o.AudioCodec]
	if o.AudioCodec == "copy" {
		codec = "copy"
	}
	args = append(args, "-c:a", codec)
	switch o.AudioCodec {
	case "aac", "mp3":
		args = append(args, "-b:a", "192k")
	case "opus":
		args = append(args, "-b:a", "128k")
	case "vorbis":
		args = append(args, "-q:a", "5")
	}
	if o.Extension == "mp4" || o.Extension == "mov" || o.Extension == "m4a" {
		args = append(args, "-movflags", "+faststart")
	}
	return append(args, "-threads", "2", "-n", destination), nil
}

func (e VideoEditor) Convert(ctx context.Context, source, destination string, o FormatOptions, duration float64, progress func(int)) error {
	if !filepath.IsAbs(source) || !filepath.IsAbs(destination) || source == destination {
		return fmt.Errorf("원본과 다른 절대 출력 경로가 필요합니다")
	}
	args, err := conversionArguments(source, destination, o)
	if err != nil {
		return err
	}
	return e.streamRun(ctx, filepath.Dir(destination), args, nil, nil, max(duration, 0.001), progress)
}

// QuickWebP encodes a bounded clip directly, without editor sessions or frame caches.
func (e VideoEditor) QuickWebP(ctx context.Context, source, destination string, start, duration float64, width, fps, loop int, quality AnimationQuality, progress func(int)) error {
	if !finiteRange(start, start+duration) || duration > 30 || width < 64 || width > 1920 || fps < 1 || fps > 30 || (loop != 0 && loop != 1) || math.IsNaN(duration) {
		return fmt.Errorf("WebP는 최대 30초, 64–1920px, 1–30FPS로 지정하세요")
	}
	q, err := NormalizeAnimationQuality("webp", quality)
	if err != nil {
		return err
	}
	compression := "4"
	if q.Effort == "fast" {
		compression = "1"
	}
	if q.Effort == "precise" {
		compression = "6"
	}
	lossless := "0"
	if q.Lossless {
		lossless = "1"
	}
	filter := fmt.Sprintf("fps=%d,scale=w='min(%d,iw)':h=-1:flags=lanczos,setsar=1", fps, width)
	args := []string{"-protocol_whitelist", "file,pipe", "-ss", number(start), "-i", source, "-t", number(duration), "-map", "0:v:0", "-an", "-vf", filter, "-c:v", "libwebp_anim", "-lossless", lossless, "-quality", strconv.Itoa(q.Quality), "-compression_level", compression, "-loop", strconv.Itoa(loop), "-threads", "2", "-n", destination}
	if q.WebPPreset != "" {
		args = append(args[:len(args)-1], "-preset", q.WebPPreset, destination)
	}
	return e.streamRun(ctx, filepath.Dir(destination), args, nil, nil, duration, progress)
}

// Legacy defaults preserve existing source selection and avoid unnecessary
// transcoding. Explicit output codecs or new containers allow more sources.
func downloadOutputFormat(options Options) (FormatOptions, bool, error) {
	kind := options.Type
	if options.Quality == "audio" {
		kind = "audio"
	}
	output, err := NormalizeFormat(FormatOptions{Type: kind, Extension: options.Extension, VideoCodec: options.VideoCodec, AudioCodec: options.AudioCodec})
	if err != nil {
		return output, false, err
	}
	advanced := options.VideoCodec != "" || options.AudioCodec != "" || (output.Type == "audio" && output.Extension != "mp3") || (output.Type == "video" && output.Extension != "mp4" && output.Extension != "mkv")
	return output, advanced, nil
}
