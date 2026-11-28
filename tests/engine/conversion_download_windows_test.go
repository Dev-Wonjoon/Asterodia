package engine_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

// Supply deterministic yt-dlp output while exercising the actual FFmpeg
// conversion, path validation, staging cleanup, and output verification.
func runConversionDownloadFixture() bool {
	if os.Getenv("ASTERODIA_CONVERSION_DOWNLOAD_FIXTURE") != "1" || len(os.Args) < 2 || os.Args[1] != "--ignore-config" {
		return false
	}
	var info struct {
		ID      string
		Formats []struct {
			ID  string `json:"format_id"`
			Ext string
		}
	}
	if json.NewDecoder(os.Stdin).Decode(&info) != nil || len(info.Formats) != 1 {
		os.Exit(2)
	}
	for _, arg := range os.Args {
		if arg == "--extract-audio" {
			fmt.Fprintln(os.Stderr, "unexpected lossy intermediate extraction")
			os.Exit(2)
		}
	}
	path, _ := filepath.Abs("media." + info.Formats[0].Ext)
	source, err := os.Open(os.Getenv("ASTERODIA_CONVERSION_DOWNLOAD_SOURCE"))
	if err != nil {
		os.Exit(2)
	}
	defer source.Close()
	output, err := os.Create(path)
	if err != nil {
		os.Exit(2)
	}
	_, err = io.Copy(output, source)
	output.Close()
	if err != nil {
		os.Exit(2)
	}
	result, _ := json.Marshal(map[string]string{"id": info.ID, "format_id": info.Formats[0].ID, "filepath": path})
	fmt.Println("__ASTERODIA_RESULT__:" + string(result))
	os.Exit(0)
	return true
}

func TestYtdlpDownloadSelectedCodecsAndContainers(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg.exe")
	if err != nil {
		t.Fatal(err)
	}
	probe, err := exec.LookPath("ffprobe.exe")
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := ytdlpTestRoot(t)
	t.Setenv("ASTERODIA_CONVERSION_DOWNLOAD_FIXTURE", "1")
	for _, tc := range []struct {
		name, sourceExt, sourceVideo, sourceAudio string
		options                                   engine.Options
		video, audio                              string
	}{
		{"video-webm", "mp4", "h264", "aac", engine.Options{Type: "video", Extension: "webm", VideoCodec: "vp9", AudioCodec: "opus"}, "vp9", "opus"},
		{"webm-source-mp4", "webm", "vp9", "opus", engine.Options{Type: "video", Extension: "mp4", VideoCodec: "h264", AudioCodec: "aac"}, "h264", "aac"},
		{"audio-flac", "m4a", "none", "aac", engine.Options{Type: "audio", Extension: "flac", AudioCodec: "flac"}, "", "flac"},
		{"audio-webm", "m4a", "none", "aac", engine.Options{Type: "audio", Extension: "webm", AudioCodec: "opus"}, "", "opus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := os.MkdirTemp(root, "conversion-source-")
			if err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(dir, "source."+tc.sourceExt)
			args := []string{"-nostdin", "-v", "error"}
			if tc.sourceVideo != "none" {
				args = append(args, "-f", "lavfi", "-i", "testsrc2=size=64x48:rate=10:duration=0.5")
			}
			args = append(args, "-f", "lavfi", "-i", "sine=frequency=400:duration=0.5")
			if tc.sourceVideo != "none" {
				args = append(args, "-c:v", map[string]string{"h264": "libx264", "vp9": "libvpx-vp9"}[tc.sourceVideo], "-pix_fmt", "yuv420p")
			}
			args = append(args, "-c:a", map[string]string{"aac": "aac", "opus": "libopus"}[tc.sourceAudio], source)
			if _, err = new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: ffmpeg, Directory: dir, Timeout: time.Minute, Arguments: args}); err != nil {
				t.Fatal(err)
			}
			t.Setenv("ASTERODIA_CONVERSION_DOWNLOAD_SOURCE", source)
			raw, _ := json.Marshal(map[string]any{"id": "fixture", "title": "fixture", "extractor_key": "Generic", "webpage_url": "https://example.invalid/video", "formats": []map[string]any{{"format_id": "source", "ext": tc.sourceExt, "vcodec": tc.sourceVideo, "acodec": tc.sourceAudio, "url": "https://example.invalid/media"}}})
			targets, err := engine.ParseYtdlpTargets(raw, "https://example.invalid/video")
			if err != nil || len(targets) != 1 {
				t.Fatal(targets, err)
			}
			if err = targets[0].ValidateDirectDownload(tc.options); err != nil {
				t.Fatal(err)
			}
			stage, err := os.MkdirTemp(root, "conversion-stage-")
			if err != nil {
				t.Fatal(err)
			}
			y := engine.Ytdlp{Executable: executable, FFmpegLocation: ffmpeg}
			path, err := y.DownloadSingle(t.Context(), targets[0], stage, tc.options)
			if err != nil {
				t.Fatal(err)
			}
			if path != filepath.Join(stage, "media."+tc.options.Extension) {
				t.Fatal(path)
			}
			if err = engine.VerifyMedia(t.Context(), ffmpeg, path, tc.options.Type); err != nil {
				t.Fatal(err)
			}
			result, err := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: probe, Directory: stage, Timeout: time.Minute, Arguments: []string{"-v", "error", "-show_entries", "stream=codec_type,codec_name", "-of", "json", path}})
			if err != nil {
				t.Fatal(err)
			}
			var media struct {
				Streams []struct {
					Type  string `json:"codec_type"`
					Codec string `json:"codec_name"`
				}
			}
			if err = json.Unmarshal([]byte(result.Stdout), &media); err != nil {
				t.Fatal(err)
			}
			codecs := map[string]string{}
			for _, s := range media.Streams {
				codecs[s.Type] = s.Codec
			}
			if codecs["video"] != tc.video || codecs["audio"] != tc.audio {
				t.Fatal(codecs)
			}
			entries, err := os.ReadDir(stage)
			if err != nil || len(entries) != 1 {
				t.Fatal(entries, err)
			}
		})
	}
}
