package integration_test

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

	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

type localVideo struct {
	engine *engine.Ytdlp
	info   json.RawMessage
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
			if e != nil || len(entries) != 1 {
				t.Fatal("쿠키 작업 사본이나 임시 파일이 최종 미디어에 남았습니다")
			}
			assertFK(t, f.db)
		})
	}
	actual, err := os.ReadFile(cookie)
	if err != nil || string(actual) != string(originalCookies) {
		t.Fatal("사용자 쿠키 원본이 변경되었습니다")
	}
}
