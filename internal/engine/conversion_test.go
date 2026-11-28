package engine

import (
	"context"
	"math"
	"strings"
	"testing"
)

func TestNormalizeFormat(t *testing.T) {
	for _, f := range outputFormats {
		for _, video := range append([]string{""}, f.VideoCodecs...) {
			for _, audio := range f.AudioCodecs {
				o, err := NormalizeFormat(FormatOptions{Type: f.Type, Extension: f.Extension, VideoCodec: video, AudioCodec: audio})
				if err != nil || o.Extension != f.Extension {
					t.Fatal(o, err)
				}
			}
		}
	}
	for _, o := range []FormatOptions{
		{Type: "video", Extension: "webm", VideoCodec: "h264"}, {Type: "audio", Extension: "wav", AudioCodec: "mp3"},
		{Type: "audio", Extension: "mp4"}, {Type: "video", Extension: "mp3"}, {Type: "audio", Extension: "m4a", VideoCodec: "copy"},
		{Type: "video", Extension: "mkv", VideoCodec: "-i malicious"},
	} {
		if _, err := NormalizeFormat(o); err == nil {
			t.Fatal("accepted incompatible format", o)
		}
	}
	args, err := conversionArguments("source.mp4", "result.webm", FormatOptions{Type: "video", Extension: "webm"})
	if err != nil || !strings.Contains(strings.Join(args, " "), "-c:v libvpx-vp9") || !strings.Contains(strings.Join(args, " "), "-c:a libopus") {
		t.Fatal(args, err)
	}
}

func TestQuickWebPRejectsInvalidBoundsBeforeExecution(t *testing.T) {
	for _, duration := range []float64{0, -1, 30.001, math.NaN(), math.Inf(1)} {
		err := (VideoEditor{}).QuickWebP(context.Background(), "source", "output", 0, duration, 640, 15, 0, AnimationQuality{}, nil)
		if err == nil || !strings.Contains(err.Error(), "최대 30초") {
			t.Fatal(duration, err)
		}
	}
}
