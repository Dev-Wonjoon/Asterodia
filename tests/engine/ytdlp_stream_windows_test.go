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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

// Exercise discovery and the frozen extractor metadata with real segment downloaders.
func TestYtdlpDownloadStreams(t *testing.T) {
	ytdlpPath := os.Getenv("ASTERODIA_TEST_YTDLP_EXE")
	probePath := os.Getenv("ASTERODIA_TEST_FFPROBE_EXE")
	if ytdlpPath == "" && probePath == "" {
		t.Skip("yt-dlp·FFmpeg 실행 경로 필요")
	}
	if !filepath.IsAbs(ytdlpPath) || !filepath.IsAbs(probePath) {
		t.Fatal("엔진 절대 경로 필요")
	}
	root := ytdlpTestRoot(t)
	ffmpegDir := filepath.Dir(probePath)
	var runner engine.ProcessRunner
	for _, kind := range []string{"hls-ts", "hls-fmp4", "hls-m3u8-protocol", "dash"} {
		t.Run(kind, func(t *testing.T) {
			source, err := os.MkdirTemp(root, kind+"-source-")
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=10:duration=3", "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-g", "5", "-sc_threshold", "0", "-c:a", "aac", "-shortest"}
			manifest := "index.m3u8"
			if kind == "dash" {
				manifest = "index.mpd"
				args = append(args, "-f", "dash", "-seg_duration", "0.5", manifest)
			} else {
				args = append(args, "-f", "hls", "-hls_time", "0.5", "-hls_list_size", "0")
				if kind == "hls-fmp4" {
					args = append(args, "-hls_segment_type", "fmp4")
				}
				args = append(args, manifest)
			}
			generated, err := runner.Run(t.Context(), engine.ProcessRequest{Executable: filepath.Join(ffmpegDir, "ffmpeg.exe"), Directory: source, Arguments: args, Timeout: 30 * time.Second})
			if err != nil {
				t.Fatalf("소스 생성: %v\n%s", err, generated.Stderr)
			}
			var broken, blocking, requireHeader atomic.Bool
			var segments atomic.Int32
			var cancelDownload context.CancelFunc
			var cancelOnce sync.Once
			files := http.FileServer(http.Dir(source))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/"+manifest && r.URL.RawQuery != "signature=keep%2Bexact" {
					http.Error(w, "lost signature", 403)
					return
				}
				if requireHeader.Load() && r.Header.Get("X-Fixture") != "transport" {
					http.Error(w, "lost headers", 403)
					return
				}
				isSegment := strings.HasSuffix(r.URL.Path, ".ts") || strings.HasSuffix(r.URL.Path, ".m4s")
				if isSegment {
					segments.Add(1)
					if blocking.Load() {
						cancelOnce.Do(cancelDownload)
						<-r.Context().Done()
						return
					}
					if broken.Load() && (strings.Contains(r.URL.Path, "index1.") || strings.Contains(r.URL.Path, "00002.")) {
						http.NotFound(w, r)
						return
					}
				}
				files.ServeHTTP(w, r)
			}))
			defer server.Close()
			url := server.URL + "/" + manifest + "?signature=keep%2Bexact"
			y := engine.Ytdlp{Executable: ytdlpPath, FFmpegLocation: ffmpegDir}
			discoveryDir, err := os.MkdirTemp(root, "stream-discovery-")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := y.Discover(t.Context(), url, discoveryDir)
			if err != nil {
				t.Fatal(err)
			}
			// Extractor transport context must survive freezing along with fragment URLs/ranges.
			var metadata map[string]any
			if err = json.Unmarshal(raw, &metadata); err != nil {
				t.Fatal(err)
			}
			metadata["http_headers"] = map[string]string{"X-Fixture": "transport"}
			for _, f := range metadata["formats"].([]any) {
				f.(map[string]any)["http_headers"] = map[string]string{"X-Fixture": "transport"}
				if kind == "hls-m3u8-protocol" {
					f.(map[string]any)["protocol"] = "m3u8"
				}
			}
			raw, _ = json.Marshal(metadata)
			targets, err := engine.ParseYtdlpTargets(raw, url)
			if err != nil || len(targets) != 1 {
				t.Fatalf("대상: %v / %d", err, len(targets))
			}
			requireHeader.Store(true)
			for _, mode := range []string{"mp4", "mkv", "audio", "missing", "cancel"} {
				t.Run(mode, func(t *testing.T) {
					stage, err := os.MkdirTemp(root, kind+"-"+mode+"-")
					if err != nil {
						t.Fatal(err)
					}
					options := engine.Options{}
					if mode == "mkv" {
						options.Extension = "mkv"
					}
					if mode == "audio" {
						options.Type = "audio"
					}
					broken.Store(mode == "missing")
					ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
					defer cancel()
					cancelDownload = cancel
					blocking.Store(mode == "cancel")
					before := segments.Load()
					var progressFrames atomic.Int32
					ctx = engine.WithOutputObserver(ctx, func(output engine.Output) {
						const prefix = "__ASTERODIA_PROGRESS__:"
						if !strings.HasPrefix(output.Text, prefix) {
							return
						}
						var frame struct {
							Streams  int `json:"streams"`
							Download struct {
								Bytes int64 `json:"downloaded_bytes"`
							} `json:"download"`
						}
						if json.Unmarshal([]byte(strings.TrimPrefix(output.Text, prefix)), &frame) == nil && frame.Streams > 0 && frame.Download.Bytes > 0 {
							progressFrames.Add(1)
						}
					})
					path, err := y.DownloadSingle(ctx, targets[0], stage, options)
					if mode == "missing" || mode == "cancel" {
						if err == nil || path != "" {
							t.Fatal("불완전한 결과를 성공으로 반환", path, err)
						}
						if mode == "cancel" && ctx.Err() != context.Canceled {
							t.Fatal("취소 요청 누락", err)
						}
						if mode == "missing" && ctx.Err() != nil {
							t.Fatal("누락을 시간 제한으로만 검출", err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if segments.Load() <= before {
						t.Fatal("세그먼트 요청 누락")
					}
					if progressFrames.Load() == 0 {
						t.Fatal("실제 HLS·DASH 다운로드 진행 수치 누락")
					}
					probe, err := runner.Run(t.Context(), engine.ProcessRequest{Executable: probePath, Directory: stage, Timeout: 15 * time.Second, Arguments: []string{"-v", "error", "-show_entries", "stream=codec_type:format=duration,format_name", "-of", "json", path}})
					if err != nil {
						t.Fatal(err, probe.Stderr)
					}
					var result struct {
						Streams []struct {
							Type string `json:"codec_type"`
						}
						Format struct {
							Duration string `json:"duration"`
							Name     string `json:"format_name"`
						}
					}
					if err = json.Unmarshal([]byte(probe.Stdout), &result); err != nil {
						t.Fatal(err)
					}
					var video, audio bool
					for _, stream := range result.Streams {
						video = video || stream.Type == "video"
						audio = audio || stream.Type == "audio"
					}
					var duration float64
					fmt.Sscan(result.Format.Duration, &duration)
					if !audio || video != (mode != "audio") || duration < 2.8 {
						t.Fatalf("미디어 불일치: %s", probe.Stdout)
					}
					if mode == "mp4" && !strings.Contains(result.Format.Name, "mp4") {
						t.Fatalf("MP4 컨테이너가 아님: %s", probe.Stdout)
					}
				})
			}
		})
	}
}

func TestYtdlpDownloadManifestRouting(t *testing.T) {
	for _, url := range []string{"https://instagram.com/cdn/file.M3U8?signature=one%2Btwo", "https://x.com/video/index.mpd?token=123"} {
		kind, err := engine.Select(url, engine.Options{})
		if err != nil || kind != engine.KindYtdlp {
			t.Fatal(url, kind, err)
		}
	}
}
