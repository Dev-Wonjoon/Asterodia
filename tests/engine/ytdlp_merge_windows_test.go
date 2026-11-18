package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

func TestYtdlpDownloadMerge(t *testing.T) {
	ytdlpPath := os.Getenv("STORAGEBUCKET_TEST_YTDLP_EXE")
	ffprobePath := os.Getenv("STORAGEBUCKET_TEST_FFPROBE_EXE")
	if ytdlpPath == "" && ffprobePath == "" {
		t.Skip("yt-dlp와 FFmpeg 실행 경로를 지정하세요")
	}
	if !filepath.IsAbs(ytdlpPath) || !filepath.IsAbs(ffprobePath) {
		t.Fatal("yt-dlp.exe와 ffprobe.exe의 절대 경로를 지정하세요")
	}

	root := ytdlpTestRoot(t)
	sourceDir, err := os.MkdirTemp(root, "merge-source-")
	if err != nil {
		t.Fatal(err)
	}

	ffmpegDir := filepath.Dir(ffprobePath)
	ffmpegPath := filepath.Join(ffmpegDir, "ffmpeg.exe")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	var runner engine.ProcessRunner
	run := func(t *testing.T, executable string, args ...string) string {
		t.Helper()
		result, err := runner.Run(ctx, engine.ProcessRequest{
			Executable: executable,
			Directory:  sourceDir,
			Timeout:    30 * time.Second,
			Arguments:  args,
		})
		if err != nil {
			t.Fatalf("%s 실행 실패: %v\n%s",
				filepath.Base(executable), err, result.Stderr)
		}
		return result.Stdout
	}

	for _, video := range []struct {
		size string
		file string
	}{
		{size: "854x480", file: "video-480.mp4"},
		{size: "1280x720", file: "video-720.mp4"},
	} {
		run(t, ffmpegPath,
			"-nostdin", "-loglevel", "error",
			"-f", "lavfi", "-i", "color=c=blue:s="+video.size+":d=1",
			"-an", "-c:v", "libx264", "-pix_fmt", "yuv420p", video.file,
		)
	}
	run(t, ffmpegPath,
		"-nostdin", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=1000:duration=1",
		"-vn", "-c:a", "aac", "audio.m4a",
	)

	server := httptest.NewServer(http.FileServer(http.Dir(sourceDir)))
	defer server.Close()

	originalURL := server.URL + "/watch?id=merge"
	raw := []byte(fmt.Sprintf(`{
	"id": "merge",
	"title": "Merge fixture",
	"extractor": "generic",
	"extractor_key": "Generic",
	"webpage_url": %q,
	"formats": [
		{"format_id":"a1","ext":"m4a","vcodec":"none","acodec":"aac","url":%q},
		{"format_id":"v480","ext":"mp4","height":480,"vcodec":"h264","acodec":"none","url":%q},
		{"format_id":"v720","ext":"mp4","height":720,"vcodec":"h264","acodec":"none","url":%q}
	]
}`, originalURL, server.URL+"/audio.m4a",
		server.URL+"/video-480.mp4", server.URL+"/video-720.mp4"))

	targets, err := engine.ParseYtdlpTargets(raw, originalURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("대상 수: %d, 기대값: 1", len(targets))
	}

	ytdlp := engine.Ytdlp{Executable: ytdlpPath, FFmpegLocation: ffmpegDir}
	for _, tc := range []struct {
		quality    string
		wantHeight int
	}{
		{quality: "best", wantHeight: 720},
		{quality: "480", wantHeight: 480},
	} {
		t.Run(tc.quality, func(t *testing.T) {
			stageDir, err := os.MkdirTemp(root, "merge-stage-")
			if err != nil {
				t.Fatal(err)
			}
			path, err := ytdlp.DownloadSingle(ctx, targets[0], stageDir,
				engine.Options{Quality: tc.quality})
			if err != nil {
				t.Fatalf("병합 실패: %v", err)
			}

			output := run(t, ffprobePath,
				"-v", "error", "-show_streams",
				"-show_entries", "stream=codec_type,height",
				"-of", "json", "-i", path,
			)
			var media struct {
				Streams []struct {
					Type   string `json:"codec_type"`
					Height int    `json:"height"`
				} `json:"streams"`
			}
			if err := json.Unmarshal([]byte(output), &media); err != nil {
				t.Fatal(err)
			}

			var height, audioTracks int
			for _, stream := range media.Streams {
				switch stream.Type {
				case "video":
					height = stream.Height
				case "audio":
					audioTracks++
				}
			}
			if len(media.Streams) != 2 || height != tc.wantHeight || audioTracks != 1 {
				t.Fatalf("스트림 불일치: 총 %d개, 영상 높이 %d (기대 %d), 오디오 %d개",
					len(media.Streams), height, tc.wantHeight, audioTracks)
			}
			t.Logf("%dp 병합 확인: %s", height, path)
		})
	}
}
