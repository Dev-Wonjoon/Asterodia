package engine_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

// Sites can expose only WebM/Opus audio, which must still be convertible to MP3.
func TestYtdlpDownloadAudioSources(t *testing.T) {
	ytdlpPath := os.Getenv("ASTERODIA_TEST_YTDLP_EXE")
	probePath := os.Getenv("ASTERODIA_TEST_FFPROBE_EXE")
	if ytdlpPath == "" && probePath == "" {
		t.Skip("yt-dlp·FFmpeg 실행 경로 필요")
	}
	if !filepath.IsAbs(ytdlpPath) || !filepath.IsAbs(probePath) {
		t.Fatal("엔진 절대 경로 필요")
	}
	root := ytdlpTestRoot(t)
	source, err := os.MkdirTemp(root, "audio-source-")
	if err != nil {
		t.Fatal(err)
	}
	ffmpegDir := filepath.Dir(probePath)
	var runner engine.ProcessRunner
	server := httptest.NewServer(http.FileServer(http.Dir(source)))
	defer server.Close()
	y := engine.Ytdlp{Executable: ytdlpPath, FFmpegLocation: ffmpegDir}
	for _, tc := range []struct{ ext, codec string }{{"webm", "libopus"}, {"opus", "libopus"}, {"mp3", "libmp3lame"}, {"m4a", "aac"}} {
		t.Run(tc.ext, func(t *testing.T) {
			filename := "audio." + tc.ext
			generated, err := runner.Run(t.Context(), engine.ProcessRequest{Executable: filepath.Join(ffmpegDir, "ffmpeg.exe"), Directory: source, Timeout: 30 * time.Second, Arguments: []string{"-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.3", "-vn", "-c:a", tc.codec, filename}})
			if err != nil {
				t.Fatalf("소스 생성: %v\n%s", err, generated.Stderr)
			}
			address := server.URL + "/" + filename
			raw, _ := json.Marshal(map[string]any{"id": "audio", "title": "audio", "extractor_key": "Generic", "webpage_url": address, "formats": []map[string]any{{"format_id": "audio", "ext": tc.ext, "vcodec": "none", "acodec": tc.codec, "url": address}}})
			targets, err := engine.ParseYtdlpTargets(raw, address)
			if err != nil || len(targets) != 1 {
				t.Fatalf("대상: %v / %d", err, len(targets))
			}
			options := engine.Options{Type: "audio", Quality: "audio", Extension: "mp3"}
			if err = targets[0].ValidateDirectDownload(options); err != nil {
				t.Fatalf("MP3 변환 가능한 오디오가 제외됨: %v", err)
			}
			stage, err := os.MkdirTemp(root, "audio-stage-")
			if err != nil {
				t.Fatal(err)
			}
			path, err := y.DownloadSingle(t.Context(), targets[0], stage, options)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Ext(path) != ".mp3" {
				t.Fatalf("MP3 확장자가 아님: %s", path)
			}
			probe, err := runner.Run(t.Context(), engine.ProcessRequest{Executable: probePath, Directory: stage, Timeout: 30 * time.Second, Arguments: []string{"-v", "error", "-show_entries", "stream=codec_type,codec_name", "-of", "json", path}})
			if err != nil {
				t.Fatal(err)
			}
			var media struct {
				Streams []struct {
					Kind  string `json:"codec_type"`
					Codec string `json:"codec_name"`
				} `json:"streams"`
			}
			if err = json.Unmarshal([]byte(probe.Stdout), &media); err != nil {
				t.Fatal(err)
			}
			if len(media.Streams) != 1 || media.Streams[0].Kind != "audio" || media.Streams[0].Codec != "mp3" {
				t.Fatalf("MP3 오디오 전용 파일이 아님: %s", probe.Stdout)
			}
			entries, err := os.ReadDir(stage)
			if err != nil || len(entries) != 1 || entries[0].Name() != "media.mp3" {
				t.Fatalf("변환 후 임시 파일 잔존: %v / %v", entries, err)
			}
		})
	}
}
