package integration_test

import (
	"bytes"
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

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

type localVideo struct {
	engine *engine.Ytdlp
	info   json.RawMessage
}

type observedGeneric struct {
	real          *engine.Ytdlp
	afterDiscover func()
}

func (v observedGeneric) Discover(ctx context.Context, url, dir string) (json.RawMessage, error) {
	raw, e := v.real.Discover(ctx, url, dir)
	if e == nil {
		v.afterDiscover()
	}
	return raw, e
}
func (v observedGeneric) DownloadSingle(ctx context.Context, target engine.YtdlpTarget, dir string, options engine.Options) (string, error) {
	return v.real.DownloadSingle(ctx, target, dir, options)
}

func TestGenericWebpagePublication(t *testing.T) {
	ytdlp, probe := os.Getenv("STORAGEBUCKET_TEST_YTDLP_EXE"), os.Getenv("STORAGEBUCKET_TEST_FFPROBE_EXE")
	if !filepath.IsAbs(ytdlp) || !filepath.IsAbs(probe) {
		t.Skip("실제 Generic 검증에는 yt-dlp와 ffprobe 경로가 필요합니다")
	}
	ffmpeg := filepath.Join(filepath.Dir(probe), "ffmpeg.exe")
	assets := newFixture(t)
	videoPath, audioPath := filepath.Join(assets.root, "source.mp4"), filepath.Join(assets.root, "source.m4a")
	for _, args := range [][]string{
		{"-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=blue:s=64x48:d=0.3", "-f", "lavfi", "-i", "sine=frequency=800:duration=0.3", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", "-movflags", "+faststart", videoPath},
		{"-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=800:duration=0.3", "-c:a", "aac", audioPath},
	} {
		if _, e := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: ffmpeg, Directory: assets.root, Timeout: 30 * time.Second, Arguments: args}); e != nil {
			t.Fatal(e)
		}
	}
	video, e := os.ReadFile(videoPath)
	if e != nil {
		t.Fatal(e)
	}
	audio, e := os.ReadFile(audioPath)
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"single", "multiple", "direct", "mp3", "m4a-to-mp3", "mkv", "unsupported-hls", "second-fails", "retry", "cancel", "replacement-fails"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			cookie := filepath.Join(f.root, "original-cookies.txt")
			originalCookies := []byte("# Netscape HTTP Cookie File\n127.0.0.1\tFALSE\t/\tFALSE\t2147483647\tsession\tfixture\n")
			if e := os.WriteFile(cookie, originalCookies, 0600); e != nil {
				t.Fatal(e)
			}
			var discovered, fail atomic.Bool
			fail.Store(mode == "second-fails" || mode == "retry")
			var generations atomic.Int32
			secondStarted := make(chan struct{}, 1)
			var page string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, e := r.Cookie("session")
				if e != nil || c.Value != "fixture" {
					http.Error(w, "cookie required", http.StatusUnauthorized)
					return
				}
				if r.URL.Path == "/page.html" {
					generation := generations.Add(1)
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					fmt.Fprintf(w, `<html><head><title>Generic local fixture</title></head><body><video controls><source src="/one.mp4?token=%d" type="video/mp4"></video>`, generation)
					if mode == "multiple" || mode == "second-fails" || mode == "retry" || mode == "cancel" || mode == "replacement-fails" {
						fmt.Fprintf(w, `<video controls><source src="/two.mp4?token=%d" type="video/mp4"></video>`, generation)
					}
					fmt.Fprint(w, "</body></html>")
					return
				}
				if r.URL.Path == "/fixture.m3u8" {
					w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
					fmt.Fprint(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:0.3,\n/unsupported.ts\n#EXT-X-ENDLIST\n")
					return
				}
				if r.URL.Path == "/one.mp4" || r.URL.Path == "/two.mp4" {
					if r.Header.Get("Referer") != page {
						http.Error(w, "referer required", http.StatusForbidden)
						return
					}
					if discovered.Load() && r.URL.Path == "/two.mp4" {
						if mode == "cancel" {
							select {
							case secondStarted <- struct{}{}:
							default:
							}
							<-r.Context().Done()
							return
						}
						if fail.Load() {
							http.NotFound(w, r)
							return
						}
					}
				}
				if strings.HasSuffix(r.URL.Path, ".m4a") {
					w.Header().Set("Content-Type", "audio/mp4")
					http.ServeContent(w, r, "source.m4a", time.Time{}, bytes.NewReader(audio))
					return
				}
				if strings.HasSuffix(r.URL.Path, ".mp4") {
					w.Header().Set("Content-Type", "video/mp4")
					http.ServeContent(w, r, "source.mp4", time.Time{}, bytes.NewReader(video))
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			page = server.URL + "/page.html?fixture=" + mode
			url := page
			options := engine.Options{}
			playlist := true
			options.Playlist = &playlist
			switch mode {
			case "direct":
				url = server.URL + "/direct.mp4"
			case "mp3":
				options.Type = "audio"
				options.Extension = "mp3"
			case "m4a-to-mp3":
				url = server.URL + "/direct.m4a"
				options.Type = "audio"
				options.Extension = "mp3"
			case "mkv":
				options.Extension = "mkv"
			case "unsupported-hls":
				url = server.URL + "/fixture.m3u8"
			}
			svc := download.NewService(f.db, func() string { return filepath.Join(f.root, "downloads") }, func(root string, options engine.Options) download.Runner {
				discovered.Store(false)
				return download.Runner{Storage: download.Storage{Root: root}, Ytdlp: observedGeneric{real: &engine.Ytdlp{Executable: ytdlp, FFmpegLocation: ffmpeg, CookieFile: cookie, NoPlaylist: options.Playlist == nil || !*options.Playlist}, afterDiscover: func() { discovered.Store(true) }}}
			}, nil)
			defer svc.Close()
			svc.ConfigureVerification(func(ctx context.Context, path, kind string) error { return engine.VerifyMedia(ctx, ffmpeg, path, kind) })
			enqueue := func(replace bool) download.Job {
				req := download.EnqueueRequest{URL: url, Mode: "create", Options: options}
				if replace {
					var id, revision int64
					if e := f.db.QueryRow("SELECT id,revision FROM media LIMIT 1").Scan(&id, &revision); e != nil {
						t.Fatal(e)
					}
					req.Mode = "replace"
					req.MediaID = &id
					req.ExpectedRevision = &revision
				}
				return success(t, svc.Enqueue(t.Context(), req)).(download.Job)
			}
			job := enqueue(false)
			run := func(trigger string) api.Result {
				return svc.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: trigger})
			}
			var result api.Result
			if mode == "cancel" {
				finished := make(chan api.Result, 1)
				go func() { finished <- run("manual_initial") }()
				select {
				case <-secondStarted:
				case <-time.After(30 * time.Second):
					t.Fatal("두 번째 영상 요청 시작 실패")
				}
				success(t, svc.Cancel(t.Context(), download.JobRequest{JobID: job.ID}))
				select {
				case result = <-finished:
				case <-time.After(30 * time.Second):
					t.Fatal("Generic 다운로드 취소 실패")
				}
			} else {
				result = run("manual_initial")
			}
			if mode == "second-fails" || mode == "retry" || mode == "cancel" || mode == "unsupported-hls" {
				code := "DOWNLOAD_ENGINE_FAILED"
				if mode == "cancel" {
					code = "CANCELLED"
				}
				if mode == "unsupported-hls" {
					code = "UNSUPPORTED_MEDIA_FORMAT"
				}
				if result.OK || result.Error == nil || result.Error.Code != code {
					t.Fatalf("기대 오류 %s, 실제 오류 %+v", code, result.Error)
				}
				if queryInt(t, f.db, "SELECT count(*) FROM media") != 0 || queryInt(t, f.db, "SELECT count(*) FROM media_file") != 0 || queryInt(t, f.db, "SELECT cleanup_required FROM download_queue WHERE id=?", job.ID) != 0 {
					t.Fatal("실패한 Generic 다운로드의 부분 자료가 남았습니다")
				}
				if mode == "unsupported-hls" && queryInt(t, f.db, "SELECT count(*) FROM download_target") != 0 {
					t.Fatal("미지원 형식을 저장 대상으로 확정했습니다")
				}
				if mode == "retry" {
					fail.Store(false)
					result = run("manual_retry")
				} else {
					assertFK(t, f.db)
					return
				}
			}
			id := success(t, result).(map[string]any)["mediaIdSnapshot"].(int64)
			want := int64(1)
			if mode == "multiple" || mode == "retry" || mode == "replacement-fails" {
				want = 2
			}
			platform := "HTML5MediaEmbed"
			if mode == "direct" || mode == "m4a-to-mp3" {
				platform = "Generic"
			}
			if queryInt(t, f.db, "SELECT count(*) FROM media_file f JOIN platform p ON p.id=f.platform_id WHERE f.media_id=? AND p.name=? AND length(f.content_hash)=64", id, platform) != want {
				var platforms string
				_ = f.db.QueryRow("SELECT group_concat(name,',') FROM platform").Scan(&platforms)
				t.Fatalf("Generic 파일·플랫폼 등록 누락: 플랫폼=%s 파일 수=%d", platforms, queryInt(t, f.db, "SELECT count(*) FROM media_file WHERE media_id=?", id))
			}
			if mode == "retry" && queryInt(t, f.db, "SELECT count(DISTINCT target_key) FROM download_target") != 2 {
				t.Fatal("서명 URL 변경으로 대상 키가 달라졌습니다")
			}
			kind, ext := "video", ".mp4"
			if options.Type == "audio" {
				kind, ext = "audio", ".mp3"
			}
			if options.Extension == "mkv" {
				ext = ".mkv"
			}
			if queryInt(t, f.db, "SELECT count(*) FROM media_file WHERE media_id=? AND kind=? AND filepath LIKE ?", id, kind, "%"+ext) != want {
				t.Fatal("출력 형식·종류 불일치")
			}
			if mode == "replacement-fails" {
				var path string
				if e := f.db.QueryRow("SELECT filepath FROM media_file WHERE media_id=? ORDER BY position LIMIT 1", id).Scan(&path); e != nil {
					t.Fatal(e)
				}
				before, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				revision := queryInt(t, f.db, "SELECT revision FROM media WHERE id=?", id)
				fail.Store(true)
				job = enqueue(true)
				errorCode(t, run("manual_initial"), "DOWNLOAD_ENGINE_FAILED")
				after, e := os.ReadFile(path)
				if e != nil || !bytes.Equal(before, after) || queryInt(t, f.db, "SELECT revision FROM media WHERE id=?", id) != revision || queryInt(t, f.db, "SELECT count(*) FROM media_file WHERE media_id=?", id) != want {
					t.Fatal("실패한 Generic 교체가 기존 자료를 변경했습니다")
				}
			}
			if content, e := os.ReadFile(cookie); e != nil || !bytes.Equal(content, originalCookies) {
				t.Fatal("원본 쿠키가 변경됐습니다")
			}
			assertFK(t, f.db)
		})
	}
}

func (v localVideo) Discover(context.Context, string, string) (json.RawMessage, error) {
	return v.info, nil
}
func (v localVideo) DownloadSingle(ctx context.Context, target engine.YtdlpTarget, dir string, options engine.Options) (string, error) {
	return v.engine.DownloadSingle(ctx, target, dir, options)
}

func TestRealVideoAudioAndMKVPublication(t *testing.T) {
	ytdlp := os.Getenv("STORAGEBUCKET_TEST_YTDLP_EXE")
	probe := os.Getenv("STORAGEBUCKET_TEST_FFPROBE_EXE")
	if ytdlp == "" || probe == "" {
		t.Skip("로컬 엔진 검증에는 yt-dlp와 ffprobe 경로가 필요합니다")
	}
	ffmpeg := filepath.Join(filepath.Dir(probe), "ffmpeg.exe")
	f := newFixture(t)
	assets := filepath.Join(f.root, "video-fixture")
	if err := os.Mkdir(assets, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(assets, "combined.mp4")
	_, err := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: ffmpeg, Directory: assets, Timeout: 30 * time.Second, Arguments: []string{"-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=blue:s=640x360:d=1", "-f", "lavfi", "-i", "sine=frequency=800:duration=1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", source}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(assets)))
	defer server.Close()
	cookie := filepath.Join(f.root, "original-cookies.txt")
	originalCookies := []byte("# Netscape HTTP Cookie File\n127.0.0.1\tFALSE\t/\tFALSE\t2147483647\tsession\tfixture\n")
	if err = os.WriteFile(cookie, originalCookies, 0600); err != nil {
		t.Fatal(err)
	}
	svc := download.NewService(f.db, func() string { return filepath.Join(f.root, "downloads") }, func(root string, opts engine.Options) download.Runner {
		info := json.RawMessage(fmt.Sprintf(`{"id":"BaW_jenozKc","title":"Local video fixture","extractor":"youtube","extractor_key":"Youtube","webpage_url":"https://www.youtube.com/watch?v=BaW_jenozKc","uploader_id":"fixture-owner","uploader":"Fixture Author","duration":1,"formats":[{"format_id":"http","ext":"mp4","height":360,"vcodec":"h264","acodec":"aac","url":%q}]}`, server.URL+"/combined.mp4"))
		return download.Runner{Storage: download.Storage{Root: root}, Ytdlp: localVideo{engine: &engine.Ytdlp{Executable: ytdlp, FFmpegLocation: ffmpeg, CookieFile: cookie}, info: info}}
	}, nil)
	defer svc.Close()
	svc.ConfigureVerification(func(ctx context.Context, path, kind string) error { return engine.VerifyMedia(ctx, ffmpeg, path, kind) })
	for _, tc := range []struct{ name, kind, extension string }{{"video", "video", "mp4"}, {"audio", "audio", "mp3"}, {"mkv", "video", "mkv"}} {
		t.Run(tc.name, func(t *testing.T) {
			request := download.EnqueueRequest{Mode: "create", URL: "https://www.youtube.com/watch?v=BaW_jenozKc&fixture=" + tc.name, Options: engine.Options{Type: tc.kind, Extension: tc.extension, Quality: "best"}}
			job := success(t, svc.Enqueue(t.Context(), request)).(download.Job)
			result := success(t, svc.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"})).(map[string]any)
			id := result["mediaIdSnapshot"].(int64)
			var path, kind string
			if err = f.db.QueryRow("SELECT filepath,kind FROM media_file WHERE media_id=?", id).Scan(&path, &kind); err != nil {
				t.Fatal(err)
			}
			if filepath.Ext(path) != "."+tc.extension || kind != tc.kind {
				t.Fatalf("형식/종류 불일치 %s %s", path, kind)
			}
			if err = engine.VerifyMedia(t.Context(), ffmpeg, path, kind); err != nil {
				t.Fatal(err)
			}
			if queryInt(t, f.db, "SELECT count(*) FROM media_file f JOIN profiles p ON p.id=f.profile_id JOIN platform pl ON pl.id=f.platform_id WHERE f.media_id=? AND p.owner_name='Fixture Author' AND pl.name='Youtube'", id) != 1 {
				t.Fatal("플랫폼 및 작성자 등록 누락")
			}
			entries, e := os.ReadDir(filepath.Dir(path))
			if e != nil || int64(len(entries)) != queryInt(t, f.db, "SELECT count(*) FROM media_file") {
				t.Fatal("쿠키 작업 사본이나 임시 파일이 최종 미디어에 남았습니다")
			}
			if path != filepath.Join(f.root, "downloads", "youtube", "fixture-owner", "Local video fixture."+tc.extension) {
				t.Fatalf("다운로드 파일명 규칙 불일치: %s", path)
			}
			assertFK(t, f.db)
		})
	}
	actual, err := os.ReadFile(cookie)
	if err != nil || string(actual) != string(originalCookies) {
		t.Fatal("사용자 쿠키 원본이 변경되었습니다")
	}
}
