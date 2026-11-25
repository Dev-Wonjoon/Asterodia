package thumbnail_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/desktop"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/thumbnail"
)

func previewJob(t *testing.T, f *fixture, url, job string, mediaID int64) {
	t.Helper()
	execSQL(t, f.db, `INSERT OR IGNORE INTO download_urls(url,normalized_url,normalization_version,created_at) VALUES(?,?,1,0)`, url, url)
	if mediaID > 0 {
		execSQL(t, f.db, `UPDATE media SET source_kind='download',request_url_id=(SELECT id FROM download_urls WHERE normalized_url=?) WHERE id=?`, url, mediaID)
	}
	execSQL(t, f.db, `INSERT INTO download_queue(id,request_url_id,original_url,mode,state,options_json) SELECT ?,id,?,'create','cancelled','{}' FROM download_urls WHERE normalized_url=?`, job, url, url)
}

func cachedPreview(t *testing.T, f *fixture, url string) string {
	t.Helper()
	dir := filepath.Join(f.root, "previews")
	must(t, os.MkdirAll(dir, 0700))
	path := filepath.Join(dir, fmt.Sprintf("%x.jpg", sha256.Sum256([]byte(url))))
	writeImage(t, path)
	return path
}

func TestLocalDownloadPreviewsBypassBlockedWebDiscovery(t *testing.T) {
	f := newFixture(t, "")
	slowURL := "https://example.invalid/slow"
	previewJob(t, f, slowURL, "slow", 0)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
	f.service.ConfigurePreview(func(ctx context.Context, url, dir string) (json.RawMessage, error) {
		if url != slowURL {
			return nil, fmt.Errorf("로컬 요청에서 웹 조회: %s", url)
		}
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	})
	go func() { defer close(done); f.service.GetDownloadPreview(t.Context(), slowURL) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("웹 조회 미시작")
	}
	url := "https://example.invalid/cached"
	previewJob(t, f, url, "cache", 0)
	cache := cachedPreview(t, f, url)
	libraryURL := "https://example.invalid/library"
	cover := filepath.Join(f.root, "cover.png")
	writeImage(t, cover)
	req := seed(t, f.db, filepath.Join(f.root, "offline.png"), "image", cover)
	previewJob(t, f, libraryURL, "library", req.MediaID)
	for _, tc := range []struct{ url, path string }{{url, cache}, {libraryURL, cover}} {
		for _, lookup := range []func(context.Context, string) (*string, error){f.service.GetDownloadPreview, f.service.CachedDownloadPreview} {
			returned := make(chan struct{}, 1)
			var got *string
			var err error
			go func() { got, err = lookup(t.Context(), tc.url); returned <- struct{}{} }()
			select {
			case <-returned:
				must(t, err)
				if got == nil || *got != tc.path {
					t.Fatal("로컬 미리보기 누락", got)
				}
			case <-time.After(time.Second):
				t.Fatal("로컬 조회가 웹 잠금을 기다립니다")
			}
		}
	}
	if got, err := f.service.CachedDownloadPreview(t.Context(), slowURL); err != nil || got != nil {
		t.Fatal(got, err)
	}
	select {
	case <-done:
		t.Fatal("차단한 웹 조회가 먼저 종료됐습니다")
	default:
	}
}

func TestDownloadPreviewReusesOldCacheWithoutDiscovery(t *testing.T) {
	f := newFixture(t, "")
	url := "https://example.invalid/old-cache"
	previewJob(t, f, url, "job", 0)
	cache := cachedPreview(t, f, url)
	old := time.Now().Add(-365 * 24 * time.Hour)
	must(t, os.Chtimes(cache, old, old))
	f.service.ConfigurePreview(func(context.Context, string, string) (json.RawMessage, error) {
		t.Fatal("유효한 캐시가 있는데 웹 조회")
		return nil, nil
	})
	for range 2 {
		got, err := f.service.GetDownloadPreview(t.Context(), url)
		must(t, err)
		if got == nil || *got != cache {
			t.Fatal(got)
		}
	}
	info, err := os.Stat(cache)
	must(t, err)
	if !info.ModTime().Equal(old) {
		t.Fatal("캐시를 다시 썼습니다")
	}
}

func TestDownloadPreviewPrefersLibraryAndRegeneratesLocally(t *testing.T) {
	f := newFixture(t, "")
	url := "https://example.invalid/local"
	original := filepath.Join(f.root, "source.png")
	writeImage(t, original)
	req := seed(t, f.db, original, "unknown", nil)
	previewJob(t, f, url, "job", req.MediaID)
	cache := cachedPreview(t, f, url)
	f.service.ConfigurePreview(func(context.Context, string, string) (json.RawMessage, error) {
		t.Fatal("원본이 있는데 웹 조회")
		return nil, nil
	})
	got, err := f.service.GetDownloadPreview(t.Context(), url)
	must(t, err)
	if got == nil || *got != cache {
		t.Fatal("생성 중 캐시 미사용", got)
	}
	must(t, os.Remove(cache))
	got, err = f.service.GetDownloadPreview(t.Context(), url)
	must(t, err)
	if got != nil {
		t.Fatal(got)
	}
	if n := queryCount(t, f.db, "SELECT count(*) FROM maintenance_task"); n != 1 {
		t.Fatal("중복 예약", n)
	}
	must(t, f.service.Start(t.Context()))
	wait(t, f, req, "registered")
	generated := thumbnailPath(t, f, req)
	cache = cachedPreview(t, f, url)
	got, err = f.service.GetDownloadPreview(t.Context(), url)
	must(t, err)
	if got == nil || *got != generated || *got == cache {
		t.Fatal("로컬 썸네일 우선순위", got)
	}
	// A valid thumbnail remains usable even when the source is offline.
	must(t, os.Remove(original))
	got, err = f.service.GetDownloadPreview(t.Context(), url)
	must(t, err)
	if got == nil || *got != generated {
		t.Fatal(got)
	}
}

func queryCount(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var count int
	must(t, db.QueryRow(query).Scan(&count))
	return count
}

func TestDownloadPreviewRejectsInternalThumbnailWhenLocalFilesAreMissing(t *testing.T) {
	f := newFixture(t, "")
	imagePath := filepath.Join(f.root, "remote.png")
	writeImage(t, imagePath)
	payload, err := os.ReadFile(imagePath)
	must(t, err)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write(payload)
	}))
	defer server.Close()
	url := "https://example.invalid/missing"
	req := seed(t, f.db, filepath.Join(f.root, "absent.mp4"), "video", nil)
	previewJob(t, f, url, "job", req.MediaID)
	calls := 0
	f.service.ConfigurePreview(func(context.Context, string, string) (json.RawMessage, error) {
		calls++
		return json.Marshal(map[string]string{"thumbnail": server.URL})
	})
	for range 2 {
		got, err := f.service.GetDownloadPreview(t.Context(), url)
		must(t, err)
		if got != nil {
			t.Fatal("내부망 미리보기가 저장됐습니다", *got)
		}
	}
	if calls != 2 || hits.Load() != 0 {
		t.Fatal("내부망 HTTP 요청이 차단되지 않았습니다", calls, hits.Load())
	}
	for _, invalid := range []string{"file:///source", "https://user:pass@example.invalid/a", "https://example.invalid/no-history"} {
		got, err := f.service.GetDownloadPreview(t.Context(), invalid)
		must(t, err)
		if got != nil {
			t.Fatal(got)
		}
	}
	if calls != 2 {
		t.Fatal("삭제되거나 잘못된 요청의 웹 조회", calls)
	}
}

func TestDownloadPreviewCleanupPreservesSharedCacheAndLibrary(t *testing.T) {
	f := newFixture(t, "")
	url := "https://example.invalid/shared"
	previewJob(t, f, url, "one", 0)
	previewJob(t, f, url, "two", 0)
	cache := cachedPreview(t, f, url)
	execSQL(t, f.db, "DELETE FROM download_queue WHERE id='one'")
	must(t, f.service.ReleaseDownloadPreview(t.Context(), url))
	if _, err := os.Stat(cache); err != nil {
		t.Fatal("공유 캐시 삭제", err)
	}
	execSQL(t, f.db, "DELETE FROM download_queue WHERE id='two'")
	req := seed(t, f.db, filepath.Join(f.root, "source.png"), "image", cache)
	must(t, f.service.ReleaseDownloadPreview(t.Context(), url))
	if _, err := os.Stat(cache); err != nil {
		t.Fatal("갤러리 썸네일 삭제", err)
	}
	execSQL(t, f.db, "UPDATE media_file SET thumbnail_path=NULL,thumbnail_origin='none' WHERE id=?", req.FileID)
	must(t, f.service.ReleaseDownloadPreview(t.Context(), url))
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatal("미사용 캐시 잔존", err)
	}
	must(t, f.service.ReleaseDownloadPreview(t.Context(), url))
	got, err := f.service.GetDownloadPreview(t.Context(), url)
	must(t, err)
	if got != nil {
		t.Fatal("이력 삭제 후 캐시 재생성", got)
	}
}

func TestDownloadPreviewUsesLegacyAssociationAndManualCover(t *testing.T) {
	f := newFixture(t, "")
	url := "https://example.invalid/legacy"
	previewJob(t, f, url, "legacy", 0)
	execSQL(t, f.db, `UPDATE download_queue SET options_json='{"_importedHistory":true}' WHERE id='legacy'`)
	first := filepath.Join(f.root, "first.png")
	second := filepath.Join(f.root, "cover.png")
	writeImage(t, first)
	writeImage(t, second)
	req := seed(t, f.db, filepath.Join(f.root, "absent.png"), "unknown", first)
	execSQL(t, f.db, `UPDATE media_file SET legacy_url_id=(SELECT request_url_id FROM download_queue WHERE id='legacy') WHERE id=?`, req.FileID)
	execSQL(t, f.db, `INSERT INTO media_file(media_id,file_key,storage_kind,filepath,kind,thumbnail_path,thumbnail_origin,position,created_at,legacy_url_id)
		SELECT media_id,'cover','unverified_legacy',filepath,kind,?,'unverified_legacy',1,0,legacy_url_id FROM media_file WHERE id=?`, second, req.FileID)
	execSQL(t, f.db, `UPDATE media SET cover_mode='manual',cover_file_id=(SELECT max(id) FROM media_file WHERE media_id=?) WHERE id=?`, req.MediaID, req.MediaID)
	got, err := f.service.GetDownloadPreview(t.Context(), url)
	must(t, err)
	if got == nil || *got != second {
		t.Fatal("구버전 연결/수동 커버 무시", got)
	}
}

func TestMain(m *testing.M) {
	if marker := os.Getenv("STORAGEBUCKET_HOLD_THUMBNAIL_ENGINE"); marker != "" {
		rel, err := filepath.Rel(os.Getenv("STORAGEBUCKET_TEST_ROOT"), marker)
		if err != nil || !filepath.IsLocal(rel) {
			os.Exit(2)
		}
		if err = os.WriteFile(marker, []byte("started"), 0600); err != nil {
			os.Exit(2)
		}
		for {
			time.Sleep(time.Second)
		}
	}
	if os.Getenv("STORAGEBUCKET_EMPTY_THUMBNAIL_ENGINE") == "1" {
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestShutdownStopsGenerationAndRestartResumesPendingTask(t *testing.T) {
	ffmpeg := os.Getenv("STORAGEBUCKET_TEST_FFMPEG_EXE")
	if !filepath.IsAbs(ffmpeg) {
		t.Fatal("실제 FFmpeg 절대 경로가 필요합니다")
	}
	executable, err := os.Executable()
	must(t, err)
	f := newFixture(t, executable)
	path := filepath.Join(f.root, "video.mp4")
	_, err = new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: ffmpeg, Directory: f.root, Timeout: 30 * time.Second, Arguments: []string{"-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=blue:s=160x90:d=1", "-c:v", "libx264", "-pix_fmt", "yuv420p", path}})
	must(t, err)
	req := seed(t, f.db, path, "unknown", nil)
	marker := filepath.Join(f.root, "engine-started.txt")
	t.Setenv("STORAGEBUCKET_HOLD_THUMBNAIL_ENGINE", marker)
	must(t, f.service.Start(t.Context()))
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err = os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("생성 엔진 미시작")
		}
		time.Sleep(10 * time.Millisecond)
	}
	started := time.Now()
	f.service.Close()
	if time.Since(started) > 3*time.Second {
		t.Fatal("엔진 종료 지연")
	}
	var state string
	must(t, f.db.QueryRow("SELECT state FROM maintenance_task LIMIT 1").Scan(&state))
	if state != "pending" {
		t.Fatal(state)
	}
	t.Setenv("STORAGEBUCKET_HOLD_THUMBNAIL_ENGINE", "")
	restarted := thumbnail.NewService(f.db, filepath.Join(f.root, "cache"), ffmpeg, nil)
	defer restarted.Close()
	must(t, restarted.Start(t.Context()))
	f.service = restarted
	wait(t, f, req, "registered")
	thumbnailPath(t, f, req)
}

type fixture struct {
	root    string
	db      *sql.DB
	service *thumbnail.Service
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func ok(t *testing.T, result api.Result) any {
	t.Helper()
	if !result.OK {
		t.Fatalf("%+v", result.Error)
	}
	return result.Data
}
func execSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	must(t, err)
}

func newFixture(t *testing.T, ffmpeg string) *fixture {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	repo, err := filepath.EvalSymlinks(filepath.Join(filepath.Dir(source), "..", ".."))
	must(t, err)
	base, err := filepath.EvalSymlinks(os.Getenv("STORAGEBUCKET_TEST_ROOT"))
	must(t, err)
	rel, err := filepath.Rel(filepath.Join(repo, ".test-data", "thumbnail"), base)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		t.Fatal(".test-data/thumbnail 아래의 실행별 폴더가 필요합니다")
	}
	root, err := os.MkdirTemp(base, "thumb-")
	must(t, err)
	db, err := database.Open(t.Context(), filepath.Join(root, "library.db"))
	must(t, err)
	s := thumbnail.NewService(db, filepath.Join(root, "cache"), ffmpeg, nil)
	t.Cleanup(func() { s.Close(); db.Close(); os.RemoveAll(root) })
	return &fixture{root, db, s}
}

func writeImage(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	must(t, err)
	img := image.NewRGBA(image.Rect(0, 0, 32, 20))
	img.Set(0, 0, color.RGBA{255, 40, 80, 255})
	must(t, png.Encode(file, img))
	must(t, file.Close())
}
func seed(t *testing.T, db *sql.DB, path, kind string, thumb any) thumbnail.Request {
	t.Helper()
	row, err := db.Exec("INSERT INTO media(source_kind,source_title,custom_title,revision,created_at) VALUES('legacy','원본','사용자 제목',7,0)")
	must(t, err)
	mediaID, err := row.LastInsertId()
	must(t, err)
	origin := "none"
	if thumb != nil {
		origin = "unverified_legacy"
	}
	row, err = db.Exec("INSERT INTO media_file(media_id,file_key,storage_kind,filepath,kind,thumbnail_path,thumbnail_origin,position,created_at) VALUES(?,?,'unverified_legacy',?,?,?,?,0,0)", mediaID, fmt.Sprint(mediaID), path, kind, thumb, origin)
	must(t, err)
	fileID, err := row.LastInsertId()
	must(t, err)
	return thumbnail.Request{MediaID: mediaID, FileID: fileID}
}
func wait(t *testing.T, f *fixture, req thumbnail.Request, state string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		status := ok(t, f.service.Get(t.Context(), req)).(thumbnail.Status)
		if status.State == state {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	var last string
	f.db.QueryRow("SELECT coalesce(last_error,'') FROM maintenance_task ORDER BY rowid DESC LIMIT 1").Scan(&last)
	t.Fatalf("기대 상태 %s, 마지막 오류 %s", state, last)
}
func thumbnailPath(t *testing.T, f *fixture, req thumbnail.Request) string {
	t.Helper()
	var path string
	must(t, f.db.QueryRow("SELECT thumbnail_path FROM media_file WHERE id=?", req.FileID).Scan(&path))
	file, err := os.Open(path)
	must(t, err)
	defer file.Close()
	config, err := jpeg.DecodeConfig(file)
	must(t, err)
	if config.Width <= 0 || config.Height <= 0 || config.Width > 480 || config.Height > 480 {
		t.Fatal(config)
	}
	return path
}

func TestStartupBackfillsLegacyImagesAndDanglingThumbnail(t *testing.T) {
	f := newFixture(t, "")
	original := filepath.Join(f.root, "source.png")
	writeImage(t, original)
	before, err := os.ReadFile(original)
	must(t, err)
	missing := seed(t, f.db, original, "unknown", nil)
	dangling := seed(t, f.db, original, "unknown", filepath.Join(f.root, "old-cache.jpg"))
	empty := filepath.Join(f.root, "empty.jpg")
	must(t, os.WriteFile(empty, nil, 0600))
	zero := seed(t, f.db, original, "image", empty)
	var wg sync.WaitGroup
	must(t, f.service.Start(t.Context()))
	for range 20 {
		wg.Add(1)
		go func() { defer wg.Done(); f.service.ScheduleMissing(); f.service.ScheduleMedia(missing.MediaID) }()
	}
	wg.Wait()
	for _, req := range []thumbnail.Request{missing, dangling, zero} {
		wait(t, f, req, "registered")
		thumbnailPath(t, f, req)
	}
	// Finish a final scan before asserting that repeated requests did not regenerate healthy images.
	f.service.ScheduleMissing()
	f.service.Close()
	var count int
	must(t, f.db.QueryRow("SELECT count(*) FROM maintenance_task WHERE kind='preview_generation'").Scan(&count))
	if count != 3 {
		t.Fatalf("중복 생성 %d", count)
	}
	after, err := os.ReadFile(original)
	must(t, err)
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("원본 변경")
	}
	var kind, title string
	var revision int
	must(t, f.db.QueryRow("SELECT f.kind,m.custom_title,m.revision FROM media_file f JOIN media m ON m.id=f.media_id WHERE f.id=?", missing.FileID).Scan(&kind, &title, &revision))
	if kind != "image" || title != "사용자 제목" || revision != 7 {
		t.Fatal(kind, title, revision)
	}
}

func TestExistingThumbnailsAndUnsupportedFilesStayUnchanged(t *testing.T) {
	f := newFixture(t, "")
	path := filepath.Join(f.root, "healthy.png")
	writeImage(t, path)
	before, err := os.ReadFile(path)
	must(t, err)
	req := seed(t, f.db, filepath.Join(f.root, "missing-original.png"), "unknown", path)
	audio := seed(t, f.db, filepath.Join(f.root, "audio.mp3"), "unknown", nil)
	other := seed(t, f.db, filepath.Join(f.root, "document.bin"), "unknown", nil)
	must(t, f.service.Start(t.Context()))
	f.service.Close()
	if ok(t, f.service.Get(t.Context(), req)).(thumbnail.Status).State != "registered" {
		t.Fatal("기존 썸네일 미유지")
	}
	if ok(t, f.service.Get(t.Context(), audio)).(thumbnail.Status).State == "unsupported" {
		t.Fatal("오디오 표지 생성을 지원해야 합니다")
	}
	for _, item := range []thumbnail.Request{other} {
		if ok(t, f.service.Get(t.Context(), item)).(thumbnail.Status).State != "unsupported" {
			t.Fatal(item)
		}
	}
	after, err := os.ReadFile(path)
	must(t, err)
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("기존 썸네일 변경")
	}
}

func TestAudioArtwork(t *testing.T) {
	ffmpeg := os.Getenv("STORAGEBUCKET_TEST_FFMPEG_EXE")
	if !filepath.IsAbs(ffmpeg) {
		t.Fatal("실제 FFmpeg 절대 경로가 필요합니다")
	}
	for _, kind := range []string{"embedded", "cached", "default"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, ffmpeg)
			run := func(args ...string) {
				t.Helper()
				result, err := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: ffmpeg, Directory: f.root, Timeout: 30 * time.Second, Arguments: args})
				if err != nil {
					t.Fatalf("오디오 소스 생성: %v %s", err, result.Stderr)
				}
			}
			original := filepath.Join(f.root, "song.mp3")
			run("-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.3", "-c:a", "libmp3lame", original)
			if kind == "embedded" {
				cover := filepath.Join(f.root, "cover.png")
				writeImage(t, cover)
				withCover := filepath.Join(f.root, "with-cover.mp3")
				run("-nostdin", "-loglevel", "error", "-i", original, "-i", cover, "-map", "0:a", "-map", "1:v", "-c", "copy", "-id3v2_version", "3", "-disposition:v", "attached_pic", withCover)
				original = withCover
			}
			before, err := os.ReadFile(original)
			must(t, err)
			req := seed(t, f.db, original, "audio", nil)
			var oldCache string
			if kind == "cached" {
				url := "https://example.invalid/song"
				previewJob(t, f, url, "audio-job", req.MediaID)
				oldCache = cachedPreview(t, f, url)
			}
			f.service.ConfigurePreview(func(context.Context, string, string) (json.RawMessage, error) {
				t.Error("오디오 표지 생성 중 불필요한 웹 조회")
				return nil, nil
			})
			must(t, f.service.Start(t.Context()))
			wait(t, f, req, "registered")
			path := thumbnailPath(t, f, req)
			imageFile, err := os.Open(path)
			must(t, err)
			config, err := jpeg.DecodeConfig(imageFile)
			imageFile.Close()
			must(t, err)
			wantWidth, wantHeight := 32, 20
			if kind == "embedded" {
				wantWidth, wantHeight = 480, 300
			}
			if kind == "default" {
				wantWidth, wantHeight = 480, 480
			}
			if config.Width != wantWidth || config.Height != wantHeight {
				t.Fatalf("표지 크기: %+v", config)
			}
			if oldCache != "" {
				must(t, os.Remove(oldCache))
				if path == oldCache {
					t.Fatal("임시 다운로드 캐시를 그대로 참조했습니다")
				}
				thumbnailPath(t, f, req)
			}
			after, err := os.ReadFile(original)
			must(t, err)
			if sha256.Sum256(before) != sha256.Sum256(after) {
				t.Fatal("MP3 원본이 변경되었습니다")
			}
			var actualKind string
			must(t, f.db.QueryRow("SELECT kind FROM media_file WHERE id=?", req.FileID).Scan(&actualKind))
			if actualKind != "audio" {
				t.Fatal("오디오 파일 종류가 변경되었습니다", actualKind)
			}
		})
	}
}

func TestMissingAndCorruptSourcesCanBeRetriedAfterRestoration(t *testing.T) {
	f := newFixture(t, "")
	missingPath := filepath.Join(f.root, "missing.png")
	brokenPath := filepath.Join(f.root, "broken.png")
	must(t, os.WriteFile(brokenPath, []byte("not an image"), 0600))
	requests := []thumbnail.Request{seed(t, f.db, missingPath, "unknown", nil), seed(t, f.db, brokenPath, "unknown", nil)}
	must(t, f.service.Start(t.Context()))
	for _, req := range requests {
		wait(t, f, req, "failed")
		if !ok(t, f.service.Get(t.Context(), req)).(thumbnail.Status).CanRetry {
			t.Fatal("재시도 불가")
		}
	}
	writeImage(t, missingPath)
	writeImage(t, brokenPath)
	for _, req := range requests {
		ok(t, f.service.Retry(t.Context(), req))
		wait(t, f, req, "registered")
		thumbnailPath(t, f, req)
	}
}

func TestBackfillContinuesAcrossBatchesAndRecoversRunningTask(t *testing.T) {
	f := newFixture(t, "")
	path := filepath.Join(f.root, "source.png")
	writeImage(t, path)
	for range 270 {
		seed(t, f.db, path, "image", path)
	}
	last := seed(t, f.db, path, "unknown", nil)
	payload, err := json.Marshal(last)
	must(t, err)
	execSQL(t, f.db, "INSERT INTO maintenance_task(id,kind,state,owner_kind,payload_json,created_at,updated_at) VALUES('interrupted','preview_generation','running','system',?,0,0)", string(payload))
	must(t, f.service.Start(t.Context()))
	must(t, f.service.Start(t.Context()))
	wait(t, f, last, "registered")
	var count int
	must(t, f.db.QueryRow("SELECT count(*) FROM maintenance_task").Scan(&count))
	if count != 1 {
		t.Fatal(count)
	}
	// A later insertion must be found by a scan requested after the initial startup scan.
	later := seed(t, f.db, path, "unknown", nil)
	f.service.ScheduleMissing()
	wait(t, f, later, "registered")
	f.service.Close()
	if f.service.Start(context.Background()) == nil {
		t.Fatal("종료 후 재시작 허용")
	}
	f.service.ScheduleMissing()
}

func TestVideoThumbnailUsesAnActualFFmpegFrame(t *testing.T) {
	ffmpeg := os.Getenv("STORAGEBUCKET_TEST_FFMPEG_EXE")
	if !filepath.IsAbs(ffmpeg) {
		t.Fatal("실제 FFmpeg 절대 경로가 필요합니다")
	}
	f := newFixture(t, ffmpeg)
	path := filepath.Join(f.root, "source.mp4")
	_, err := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: ffmpeg, Directory: f.root, Timeout: 30 * time.Second, Arguments: []string{"-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=red:s=160x90:d=1", "-c:v", "libx264", "-pix_fmt", "yuv420p", path}})
	must(t, err)
	req := seed(t, f.db, path, "unknown", nil)
	must(t, f.service.Start(t.Context()))
	wait(t, f, req, "registered")
	file, err := os.Open(thumbnailPath(t, f, req))
	must(t, err)
	defer file.Close()
	img, err := jpeg.Decode(file)
	must(t, err)
	r, g, b, _ := img.At(10, 10).RGBA()
	if r <= g*2 || r <= b*2 {
		t.Fatal("원본 영상의 빨간 프레임이 아닙니다")
	}
}

func TestSuccessfulEngineWithoutFrameIsNotPublished(t *testing.T) {
	executable, err := os.Executable()
	must(t, err)
	f := newFixture(t, executable)
	path := filepath.Join(f.root, "source.mp4")
	must(t, os.WriteFile(path, []byte("fixture"), 0600))
	t.Setenv("STORAGEBUCKET_EMPTY_THUMBNAIL_ENGINE", "1")
	req := seed(t, f.db, path, "video", nil)
	must(t, f.service.Start(t.Context()))
	wait(t, f, req, "failed")
	var thumbnailPath *string
	must(t, f.db.QueryRow("SELECT thumbnail_path FROM media_file WHERE id=?", req.FileID).Scan(&thumbnailPath))
	if thumbnailPath != nil {
		t.Fatal("빈 출력 게시")
	}
}

func TestDatabaseImportSchedulesMissingThumbnailsAfterCommit(t *testing.T) {
	f := newFixture(t, "")
	original := filepath.Join(f.root, "imported.png")
	writeImage(t, original)
	sourcePath := filepath.Join(f.root, "old.db")
	source, err := database.Open(t.Context(), sourcePath)
	must(t, err)
	seed(t, source, original, "unknown", filepath.Join(f.root, "old-thumbnail.jpg"))
	must(t, source.Close())
	must(t, f.service.Start(t.Context()))
	callback := 0
	service := desktop.NewService(database.NewImporter(f.db, filepath.Join(f.root, "library.db"), filepath.Join(f.root, "backups")), func() (string, error) { return sourcePath, nil }, func() { callback++; f.service.ScheduleMissing() })
	ok(t, service.ImportDatabase(t.Context()))
	if callback != 1 {
		t.Fatal(callback)
	}
	var req thumbnail.Request
	must(t, f.db.QueryRow("SELECT media_id,id FROM media_file LIMIT 1").Scan(&req.MediaID, &req.FileID))
	wait(t, f, req, "registered")
	thumbnailPath(t, f, req)
	cancelled := desktop.NewService(nil, func() (string, error) { return "", nil }, func() { callback++ })
	ok(t, cancelled.ImportDatabase(t.Context()))
	if callback != 1 {
		t.Fatal("취소 후 콜백")
	}
	failed := desktop.NewService(database.NewImporter(f.db, "unused", f.root), func() (string, error) { return filepath.Join(f.root, "not-found.db"), nil }, func() { callback++ })
	if failed.ImportDatabase(t.Context()).OK || callback != 1 {
		t.Fatal("실패 후 콜백")
	}
}
