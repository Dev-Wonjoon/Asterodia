package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

	var requireRange atomic.Bool
	var rangedRequests atomic.Int32
	var videoAvailable, audioAvailable atomic.Int64
	var earlyRequests, mediaRequests atomic.Int32
	files := http.FileServer(http.Dir(sourceDir))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaRequests.Add(1)
		available := videoAvailable.Load()
		if r.URL.Path == "/audio.m4a" {
			available = audioAvailable.Load()
		}
		if time.Now().Unix() < available {
			earlyRequests.Add(1)
			http.Error(w, "media is not available yet", http.StatusForbidden)
			return
		}
		if requireRange.Load() && r.Method == http.MethodGet {
			var start, end int64
			if n, _ := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); n != 2 || start < 0 || end < start || end-start+1 > 1024 {
				http.Error(w, "bounded Range request required", http.StatusBadRequest)
				return
			}
			rangedRequests.Add(1)
		}
		files.ServeHTTP(w, r)
	}))
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

	ytdlp := engine.Ytdlp{Executable: ytdlpPath, FFmpegLocation: ffmpegDir}
	for _, tc := range []struct {
		name              string
		quality           string
		wantHeight        int
		chunked           bool
		downloaderOptions string
		availability      string
		audioOnly         bool
		omitAvailability  bool
	}{
		{name: "best", quality: "best", wantHeight: 720},
		{name: "480", quality: "480", wantHeight: 480},
		{name: "chunked", quality: "best", wantHeight: 720, chunked: true, downloaderOptions: `{"http_chunk_size":1024}`},
		{name: "zero-chunk", quality: "best", wantHeight: 720, downloaderOptions: `{"http_chunk_size":0}`},
		{name: "negative-chunk", quality: "best", wantHeight: 720, downloaderOptions: `{"http_chunk_size":-1}`},
		{name: "null-options", quality: "best", wantHeight: 720, downloaderOptions: `null`},
		{name: "available-video", quality: "best", wantHeight: 720, availability: "video"},
		{name: "available-audio-merge", quality: "best", wantHeight: 720, availability: "audio"},
		{name: "available-audio-only", quality: "best", availability: "audio", audioOnly: true},
		{name: "available-past", quality: "best", wantHeight: 720, availability: "past"},
		{name: "available-null", quality: "best", wantHeight: 720, availability: "null"},
		{name: "missing-availability-reproduces-403", quality: "best", availability: "video", omitAvailability: true},
		{name: "cancel-availability-wait", quality: "best", availability: "cancel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireRange.Store(tc.chunked)
			rangedRequests.Store(0)
			earlyRequests.Store(0)
			mediaRequests.Store(0)
			videoAvailable.Store(0)
			audioAvailable.Store(0)
			now := time.Now().Unix()
			switch tc.availability {
			case "video":
				videoAvailable.Store(now + 4)
			case "audio":
				audioAvailable.Store(now + 4)
			case "cancel":
				videoAvailable.Store(now + 60)
				audioAvailable.Store(now + 60)
			}
			input := raw
			if tc.downloaderOptions != "" || tc.availability != "" {
				var info map[string]any
				if err := json.Unmarshal(raw, &info); err != nil {
					t.Fatal(err)
				}
				for _, row := range info["formats"].([]any) {
					format := row.(map[string]any)
					if tc.downloaderOptions != "" {
						format["downloader_options"] = json.RawMessage(tc.downloaderOptions)
					}
					if tc.availability != "" && !tc.omitAvailability {
						available := now - 60
						if format["ext"] == "m4a" && audioAvailable.Load() > 0 {
							available = audioAvailable.Load()
						} else if format["ext"] == "mp4" && videoAvailable.Load() > 0 {
							available = videoAvailable.Load()
						}
						format["available_at"] = available
						if tc.availability == "null" {
							format["available_at"] = nil
						}
					}
				}
				input, err = json.Marshal(info)
				if err != nil {
					t.Fatal(err)
				}
			}
			targets, err := engine.ParseYtdlpTargets(input, originalURL)
			if err != nil {
				t.Fatal(err)
			}
			if len(targets) != 1 {
				t.Fatalf("대상 수: %d, 기대값: 1", len(targets))
			}
			stageDir, err := os.MkdirTemp(root, "merge-stage-")
			if err != nil {
				t.Fatal(err)
			}
			downloadCtx, stop := context.WithCancel(ctx)
			defer stop()
			var cancelledDuringSleep atomic.Bool
			if tc.availability == "cancel" {
				downloadCtx = engine.WithOutputObserver(downloadCtx, func(output engine.Output) {
					if strings.Contains(output.Text, "Sleeping") {
						cancelledDuringSleep.Store(true)
						stop()
					}
				})
			}
			options := engine.Options{Quality: tc.quality}
			if tc.audioOnly {
				options.Type = "audio"
			}
			started := time.Now()
			path, err := ytdlp.DownloadSingle(downloadCtx, targets[0], stageDir, options)
			if tc.availability == "cancel" {
				if err == nil || !cancelledDuringSleep.Load() || mediaRequests.Load() != 0 || time.Since(started) > 8*time.Second {
					t.Fatalf("대기 중 취소 실패: err=%v, sleep=%t, requests=%d, elapsed=%v", err, cancelledDuringSleep.Load(), mediaRequests.Load(), time.Since(started))
				}
				entries, readErr := os.ReadDir(stageDir)
				if readErr != nil || len(entries) != 0 {
					t.Fatalf("대기 중 취소 후 파일 잔존: %v, %v", entries, readErr)
				}
				return
			}
			if tc.omitAvailability {
				if err == nil || !strings.Contains(err.Error(), "403") || earlyRequests.Load() == 0 {
					t.Fatalf("필드 누락 시 403 미재현: err=%v, early=%d", err, earlyRequests.Load())
				}
				return
			}
			if err != nil {
				t.Fatalf("병합 실패: %v", err)
			}
			if earlyRequests.Load() != 0 {
				t.Fatalf("다운로드 가능 시각 전 요청: %d회", earlyRequests.Load())
			}
			if tc.chunked && rangedRequests.Load() < 4 {
				t.Fatalf("영상·오디오 분할 요청 누락: %d회", rangedRequests.Load())
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
			wantStreams := 2
			if tc.audioOnly {
				wantStreams = 1
			}
			if len(media.Streams) != wantStreams || height != tc.wantHeight || audioTracks != 1 {
				t.Fatalf("스트림 불일치: 총 %d개, 영상 높이 %d (기대 %d), 오디오 %d개",
					len(media.Streams), height, tc.wantHeight, audioTracks)
			}
			t.Logf("%dp 병합 확인: %s", height, path)
		})
	}
}
