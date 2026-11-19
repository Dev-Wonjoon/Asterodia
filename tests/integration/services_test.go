package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/desktop"
	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/maintenance"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"github.com/Dev-Wonjoon/StorageBucket/internal/settings"
	"github.com/Dev-Wonjoon/StorageBucket/internal/thumbnail"
)

const postURL = "https://www.instagram.com/p/Fixture/"

type discoverFunc func(context.Context, string, string) (engine.GalleryDLDiscovery, error)

func (f discoverFunc) Discover(ctx context.Context, url, dir string) (engine.GalleryDLDiscovery, error) {
	return f(ctx, url, dir)
}

type fixture struct {
	db      *sql.DB
	root    string
	service *download.Service
	catalog *media.Service
	gallery discoverFunc
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base := os.Getenv("STORAGEBUCKET_TEST_ROOT")
	if !filepath.IsAbs(base) {
		t.Fatal("STORAGEBUCKET_TEST_ROOT 절대 경로가 필요합니다")
	}
	base, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repo, err = filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(filepath.Join(repo, ".test-data", "integration"), base)
	if err != nil || relative == "." || !filepath.IsLocal(relative) {
		t.Fatal("실행별 테스트 폴더는 저장소의 .test-data/integration 아래에 있어야 합니다")
	}
	root, err := os.MkdirTemp(base, "services-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	db, err := database.Open(t.Context(), filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := &fixture{db: db, root: root}
	f.catalog = media.NewService(db)
	var data bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err = png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(data.Bytes())
	}))
	t.Cleanup(server.Close)
	f.gallery = func(context.Context, string, string) (engine.GalleryDLDiscovery, error) {
		metadata := map[string]any{"category": "instagram", "subcategory": "post", "post_shortcode": "Fixture", "typename": "GraphImage", "post_id": "123456", "media_id": "123456", "count": 1, "extension": "png"}
		b, _ := json.Marshal([]any{[]any{2, metadata}, []any{3, server.URL + "/image.png", metadata}})
		return engine.ParseGalleryDLDiscovery(b)
	}
	f.service = download.NewService(db, func() string { return filepath.Join(root, "downloads") }, func(root string, options engine.Options) download.Runner {
		return download.Runner{Storage: download.Storage{Root: root}, GalleryDL: discoverFunc(func(ctx context.Context, url, dir string) (engine.GalleryDLDiscovery, error) {
			return f.gallery(ctx, url, dir)
		})}
	}, nil)
	t.Cleanup(f.service.Close)
	return f
}
func success(t *testing.T, result api.Result) any {
	t.Helper()
	if !result.OK {
		t.Fatalf("서비스 실패: %+v", result.Error)
	}
	return result.Data
}
func errorCode(t *testing.T, result api.Result, code string) {
	t.Helper()
	if result.OK || result.Error == nil || result.Error.Code != code {
		t.Fatalf("기대 오류 %s, 결과 %+v", code, result)
	}
}
func (f *fixture) enqueue(t *testing.T) download.Job {
	t.Helper()
	return success(t, f.service.Enqueue(t.Context(), download.EnqueueRequest{URL: postURL, Mode: "create"})).(download.Job)
}
func (f *fixture) run(t *testing.T, j download.Job) int64 {
	t.Helper()
	return success(t, f.service.Run(t.Context(), download.RunRequest{JobID: j.ID, Trigger: "manual_initial"})).(map[string]any)["mediaIdSnapshot"].(int64)
}
func queryInt(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func assertFK(t *testing.T, db *sql.DB) {
	t.Helper()
	if n := queryInt(t, db, "SELECT count(*) FROM pragma_foreign_key_check()"); n != 0 {
		t.Fatalf("외래 키 위반 %d", n)
	}
}

func TestInstagramMultipleMediaPublication(t *testing.T) {
	assets := newFixture(t)
	var pngData bytes.Buffer
	if e := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 5, 3))); e != nil {
		t.Fatal(e)
	}
	var video []byte
	ffmpeg := ""
	if probe := os.Getenv("STORAGEBUCKET_TEST_FFPROBE_EXE"); probe != "" {
		ffmpeg = filepath.Join(filepath.Dir(probe), "ffmpeg.exe")
		path := filepath.Join(assets.root, "source.mp4")
		_, e := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: ffmpeg, Directory: assets.root, Timeout: 30 * time.Second, Arguments: []string{"-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=blue:s=32x24:d=0.2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", path}})
		if e != nil {
			t.Fatal(e)
		}
		video, e = os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
	}
	for _, mode := range []string{"photos", "reel", "mixed", "background-music", "second-fails", "cancel-second", "replacement-fails", "corrupt-video"} {
		t.Run(mode, func(t *testing.T) {
			if (mode == "mixed" || mode == "reel" || mode == "corrupt-video") && len(video) == 0 {
				t.Skip("실제 영상 검증에는 ffmpeg가 필요합니다")
			}
			f := newFixture(t)
			var oldID, oldRevision int64
			var oldPath string
			var oldContent []byte
			if mode == "replacement-fails" {
				oldID = f.run(t, f.enqueue(t))
				oldRevision = queryInt(t, f.db, "SELECT revision FROM media WHERE id=?", oldID)
				if e := f.db.QueryRow("SELECT filepath FROM media_file WHERE media_id=?", oldID).Scan(&oldPath); e != nil {
					t.Fatal(e)
				}
				var e error
				oldContent, e = os.ReadFile(oldPath)
				if e != nil {
					t.Fatal(e)
				}
			}
			secondStarted := make(chan struct{}, 1)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Header.Get("User-Agent") != "fixture-user-agent" {
					t.Error("엔진의 미디어 요청 헤더 누락")
				}
				if r.URL.Path == "/second.png" {
					if mode == "cancel-second" {
						secondStarted <- struct{}{}
						<-r.Context().Done()
						return
					}
					if mode == "second-fails" || mode == "replacement-fails" {
						http.NotFound(w, r)
						return
					}
				}
				if r.URL.Path == "/video.mp4" {
					w.Header().Set("Content-Type", "video/mp4")
					if mode == "corrupt-video" {
						w.Write([]byte("\x00\x00\x00\x18ftypisomtruncated"))
						return
					}
					w.Write(video)
					return
				}
				w.Header().Set("Content-Type", "image/png")
				w.Write(pngData.Bytes())
			}))
			defer server.Close()
			paths := []string{"/first.png", "/second.png"}
			if mode == "mixed" {
				paths = []string{"/first.png", "/video.mp4", "/second.png"}
			}
			if mode == "reel" || mode == "corrupt-video" {
				paths = []string{"/video.mp4"}
			}
			f.gallery = func(context.Context, string, string) (engine.GalleryDLDiscovery, error) {
				subcategory, kind := "post", "post"
				if mode == "reel" {
					subcategory, kind = "reel", "reel"
				}
				base := map[string]any{"category": "instagram", "subcategory": subcategory, "type": kind, "post_shortcode": "Fixture", "post_id": "123456", "count": len(paths), "owner_id": "9007199254740999", "username": "instagram-owner", "description": "사진과 영상 게시물"}
				if mode == "background-music" {
					base["count"] = len(paths) + 1
				}
				messages := [][]any{{2, base}}
				for i, path := range paths {
					row := make(map[string]any)
					for k, v := range base {
						row[k] = v
					}
					row["media_id"], row["num"], row["extension"], row["_http_headers"] = fmt.Sprint(123456+i), i+1, strings.TrimPrefix(filepath.Ext(path), "."), map[string]string{"User-Agent": "fixture-user-agent"}
					messages = append(messages, []any{3, server.URL + path + "?signed=fixture", row})
					if mode == "background-music" && i == 0 {
						music := make(map[string]any)
						for k, v := range base {
							music[k] = v
						}
						music["media_id"], music["num"], music["extension"], music["audio_url"] = "987654", 1, "m4a", server.URL+"/music.m4a"
						messages = append(messages, []any{3, music["audio_url"], music})
					}
				}
				raw, _ := json.Marshal(messages)
				return engine.ParseGalleryDLDiscovery(raw)
			}
			if ffmpeg != "" {
				f.service.ConfigureVerification(func(ctx context.Context, path, kind string) error { return engine.VerifyMedia(ctx, ffmpeg, path, kind) })
			}
			request := download.EnqueueRequest{URL: postURL, Mode: "create"}
			if mode == "reel" {
				request.URL = "https://www.instagram.com/reel/Fixture/"
			}
			if oldID != 0 {
				request.Mode = "replace"
				request.MediaID = &oldID
				request.ExpectedRevision = &oldRevision
			}
			job := success(t, f.service.Enqueue(t.Context(), request)).(download.Job)
			run := func() api.Result {
				return f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"})
			}
			var result api.Result
			if mode == "cancel-second" {
				completed := make(chan api.Result, 1)
				go func() { completed <- run() }()
				select {
				case <-secondStarted:
				case <-time.After(10 * time.Second):
					t.Fatal("두 번째 파일 요청 시작 실패")
				}
				success(t, f.service.Cancel(t.Context(), download.JobRequest{JobID: job.ID}))
				select {
				case result = <-completed:
				case <-time.After(10 * time.Second):
					t.Fatal("다운로드 취소 실패")
				}
			} else {
				result = run()
			}
			if mode == "second-fails" || mode == "replacement-fails" || mode == "cancel-second" || mode == "corrupt-video" {
				expected := "MEDIA_UNAVAILABLE"
				if mode == "cancel-second" {
					expected = "CANCELLED"
				}
				if mode == "corrupt-video" {
					expected = "DOWNLOAD_ENGINE_FAILED"
				}
				errorCode(t, result, expected)
				want := int64(0)
				if oldID != 0 {
					want = 1
				}
				if queryInt(t, f.db, "SELECT count(*) FROM media") != want || queryInt(t, f.db, "SELECT count(*) FROM media_file") != want {
					t.Fatal("부분 등록이 남았습니다")
				}
				if oldID != 0 {
					content, e := os.ReadFile(oldPath)
					if e != nil || !bytes.Equal(content, oldContent) || queryInt(t, f.db, "SELECT revision FROM media WHERE id=?", oldID) != oldRevision {
						t.Fatal("실패한 교체가 기존 자료를 변경했습니다")
					}
				}
				if queryInt(t, f.db, "SELECT cleanup_required FROM download_queue WHERE id=?", job.ID) != 0 {
					t.Fatal("작업 파일 정리가 완료되지 않았습니다")
				}
			} else {
				id := success(t, result).(map[string]any)["mediaIdSnapshot"].(int64)
				if queryInt(t, f.db, "SELECT count(*) FROM media_file WHERE media_id=? AND length(content_hash)=64", id) != int64(len(paths)) {
					t.Fatal("여러 파일 저장 누락")
				}
				var order string
				if e := f.db.QueryRow("SELECT group_concat(kind,',') FROM (SELECT kind FROM media_file WHERE media_id=? ORDER BY position)", id).Scan(&order); e != nil {
					t.Fatal(e)
				}
				want := "image,image"
				if mode == "mixed" {
					want = "image,video,image"
				}
				if mode == "reel" {
					want = "video"
				}
				if order != want || requests.Load() != int32(len(paths)) {
					t.Fatalf("파일 순서·종류·요청 수 오류: %s %d", order, requests.Load())
				}
				if queryInt(t, f.db, "SELECT count(*) FROM media_file f JOIN profiles p ON p.id=f.profile_id JOIN platform pl ON pl.id=f.platform_id WHERE f.media_id=? AND p.owner_id='9007199254740999' AND pl.name='Instagram'", id) != int64(len(paths)) {
					t.Fatal("작성자 식별자 유실")
				}
			}
			assertFK(t, f.db)
		})
	}
}

func TestDownloadLibraryLifecycle(t *testing.T) {
	f := newFixture(t)
	job := f.enqueue(t)
	errorCode(t, f.service.Enqueue(t.Context(), download.EnqueueRequest{URL: postURL, Mode: "create"}), "ALREADY_PROCESSING")
	mediaID := f.run(t, job)
	done := success(t, f.service.Get(t.Context(), download.JobRequest{JobID: job.ID})).(download.Job)
	if done.State != "completed" || done.Progress != 100 || done.Phase == nil || *done.Phase != "finished" {
		t.Fatalf("완료 상태 %+v", done)
	}
	if queryInt(t, f.db, "SELECT count(*) FROM media_file WHERE media_id=? AND length(content_hash)=64 AND storage_kind='app_managed' AND width=8 AND height=6", mediaID) != 1 {
		t.Fatal("미디어 파일 검증 정보 누락")
	}
	detail := success(t, f.catalog.Get(t.Context(), media.GetRequest{MediaID: mediaID})).(media.Object)
	if detail["fileCount"] != int64(1) || detail["title"] != "Instagram Fixture" {
		t.Fatalf("묶음 상세 %+v", detail)
	}
	revision := detail["revision"].(int64)
	favorite := true
	edited := success(t, f.catalog.Edit(t.Context(), media.EditRequest{MediaID: mediaID, ExpectedRevision: revision, CustomTitle: json.RawMessage(`"내 이미지"`), IsFavorite: &favorite})).(media.Object)
	errorCode(t, f.catalog.Edit(t.Context(), media.EditRequest{MediaID: mediaID, ExpectedRevision: revision}), "REVISION_CONFLICT")
	revision = edited["revision"].(int64)
	var request media.EditRequest
	if err := json.Unmarshal([]byte(`{"tagChange":{"addNames":["여행","사진"]}}`), &request); err != nil {
		t.Fatal(err)
	}
	request.MediaID = mediaID
	request.ExpectedRevision = revision
	edited = success(t, f.catalog.Edit(t.Context(), request)).(media.Object)
	list := success(t, f.catalog.List(t.Context(), media.ListRequest{FavoritesOnly: true, Filter: media.Filter{Keyword: "내 이미지", Tags: []string{"여행", "사진"}, TagMode: "and"}})).(media.Object)
	if list["total"] != int64(1) {
		t.Fatalf("검색 누락 %+v", list)
	}
	names := success(t, f.catalog.Suggest(t.Context(), media.SuggestRequest{Kind: "tag", Text: "여"})).([]string)
	if len(names) != 1 || names[0] != "여행" {
		t.Fatalf("태그 제안 %+v", names)
	}
	errorCode(t, f.service.Enqueue(t.Context(), download.EnqueueRequest{URL: postURL, Mode: "create"}), "REDOWNLOAD_CONFIRMATION_REQUIRED")
	// Preview serving accepts registered paths and rejects arbitrary local files.
	var path string
	if err := f.db.QueryRow("SELECT filepath FROM media_file WHERE media_id=?", mediaID).Scan(&path); err != nil {
		t.Fatal(err)
	}
	middleware := desktop.MediaMiddleware(f.db, filepath.Join(f.root, "cache", "previews"))(http.NotFoundHandler())
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, httptest.NewRequest("GET", "/media?path="+url.QueryEscape(path), nil))
	if rec.Code != 200 || !bytes.HasPrefix(rec.Body.Bytes(), []byte("\x89PNG")) {
		t.Fatalf("미디어 응답 %d", rec.Code)
	}
	secret := filepath.Join(f.root, "secret.txt")
	os.WriteFile(secret, []byte("private"), 0600)
	rec = httptest.NewRecorder()
	middleware.ServeHTTP(rec, httptest.NewRequest("GET", "/media?path="+url.QueryEscape(secret), nil))
	if rec.Code != 404 {
		t.Fatalf("미등록 파일 노출 %d", rec.Code)
	}
	removed := success(t, f.catalog.UnregisterMedia(t.Context(), media.UnregisterRequest{MediaID: mediaID, ExpectedRevision: edited["revision"].(int64)})).(media.Object)
	if removed["parentRemoved"] != true || queryInt(t, f.db, "SELECT count(*) FROM media") != 0 {
		t.Fatal("묶음 해제 실패")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("DB 등록 해제 시 즉시 파일을 삭제하면 안 됩니다")
	}
	if queryInt(t, f.db, "SELECT count(*) FROM maintenance_task WHERE kind='deletion_cleanup'") != 1 {
		t.Fatal("정리 작업 누락")
	}
	assertFK(t, f.db)
	cleaner := maintenance.NewCleaner(f.db)
	defer cleaner.Close()
	if err := cleaner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("앱 관리 파일 정리 실패: %v", err)
	}
}

func TestFailureCancellationRetryAndRecovery(t *testing.T) {
	t.Run("failure-retry", func(t *testing.T) {
		f := newFixture(t)
		original := f.gallery
		f.gallery = func(context.Context, string, string) (engine.GalleryDLDiscovery, error) {
			return engine.GalleryDLDiscovery{}, errors.New("fixture failure")
		}
		job := f.enqueue(t)
		errorCode(t, f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"}), "DOWNLOAD_ENGINE_FAILED")
		failed := success(t, f.service.Get(t.Context(), download.JobRequest{JobID: job.ID})).(download.Job)
		if failed.State != "failed" || failed.CleanupRequired {
			t.Fatalf("실패 정리 %+v", failed)
		}
		f.gallery = original
		success(t, f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_retry"}))
		if queryInt(t, f.db, "SELECT count(*) FROM download_attempt WHERE job_id=?", job.ID) != 2 {
			t.Fatal("시도 이력 유실")
		}
		assertFK(t, f.db)
	})
	t.Run("cancel-before-run", func(t *testing.T) {
		f := newFixture(t)
		job := f.enqueue(t)
		success(t, f.service.Cancel(t.Context(), download.JobRequest{JobID: job.ID}))
		got := success(t, f.service.Get(t.Context(), download.JobRequest{JobID: job.ID})).(download.Job)
		if got.State != "cancelled" {
			t.Fatal(got)
		}
		assertFK(t, f.db)
	})
	t.Run("cancel-running", func(t *testing.T) {
		f := newFixture(t)
		started := make(chan struct{})
		f.gallery = func(ctx context.Context, _ string, _ string) (engine.GalleryDLDiscovery, error) {
			close(started)
			<-ctx.Done()
			return engine.GalleryDLDiscovery{}, ctx.Err()
		}
		job := f.enqueue(t)
		result := make(chan api.Result, 1)
		go func() {
			result <- f.service.Run(context.Background(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"})
		}()
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("엔진 시작 대기 시간 초과")
		}
		errorCode(t, f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"}), "ALREADY_PROCESSING")
		success(t, f.service.Cancel(t.Context(), download.JobRequest{JobID: job.ID}))
		select {
		case r := <-result:
			errorCode(t, r, "CANCELLED")
		case <-time.After(5 * time.Second):
			t.Fatal("취소 시간 초과")
		}
		assertFK(t, f.db)
	})
	t.Run("recover-published-uncommitted", func(t *testing.T) {
		f := newFixture(t)
		job := f.enqueue(t)
		attempt := *job.CurrentAttemptID
		root := filepath.Join(f.root, "downloads")
		runner := download.Runner{Storage: download.Storage{Root: root}, GalleryDL: f.gallery}
		discovery, err := runner.DiscoverAttempt(t.Context(), job.ID, attempt, postURL, engine.Options{})
		if err != nil {
			t.Fatal(err)
		}
		files, err := runner.FreezeAndStageAttempt(t.Context(), download.NewStore(f.db), discovery)
		if err != nil {
			t.Fatal(err)
		}
		finalDir := filepath.Join(root, ".storagebucket", "files", filepath.Base(discovery.StageDir()))
		os.MkdirAll(filepath.Dir(finalDir), 0700)
		rel, _ := filepath.Rel(discovery.StageDir(), files[0].Path)
		final := filepath.Join(finalDir, rel)
		if _, err = f.db.Exec("UPDATE download_attempt SET phase='committing' WHERE id=?", attempt); err != nil {
			t.Fatal(err)
		}
		if _, err = f.db.Exec("UPDATE download_target SET final_path=? WHERE attempt_id=?", final, attempt); err != nil {
			t.Fatal(err)
		}
		if err = os.Rename(discovery.StageDir(), finalDir); err != nil {
			t.Fatal(err)
		}
		if err = f.service.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err = os.Stat(finalDir); !os.IsNotExist(err) {
			t.Fatalf("미커밋 최종 파일 정리 실패 %v", err)
		}
		got := success(t, f.service.Get(t.Context(), download.JobRequest{JobID: job.ID})).(download.Job)
		if got.State != "interrupted" || got.CleanupRequired {
			t.Fatal(got)
		}
		success(t, f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_restart"}))
		if err = f.service.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
		if queryInt(t, f.db, "SELECT count(*) FROM media") != 1 {
			t.Fatal("완료한 묶음을 복구 시 손상")
		}
		assertFK(t, f.db)
	})
}

func TestReplacePreservesUserMetadata(t *testing.T) {
	f := newFixture(t)
	id := f.run(t, f.enqueue(t))
	var oldPath string
	f.db.QueryRow("SELECT filepath FROM media_file WHERE media_id=?", id).Scan(&oldPath)
	rev := queryInt(t, f.db, "SELECT revision FROM media WHERE id=?", id)
	favorite := true
	edited := success(t, f.catalog.Edit(t.Context(), media.EditRequest{MediaID: id, ExpectedRevision: rev, CustomTitle: json.RawMessage(`"사용자 제목"`), IsFavorite: &favorite})).(media.Object)
	rev = edited["revision"].(int64)
	wrong := rev - 1
	errorCode(t, f.service.Enqueue(t.Context(), download.EnqueueRequest{URL: postURL, Mode: "replace", MediaID: &id, ExpectedRevision: &wrong}), "REVISION_CONFLICT")
	job := success(t, f.service.Enqueue(t.Context(), download.EnqueueRequest{URL: postURL, Mode: "replace", MediaID: &id, ExpectedRevision: &rev})).(download.Job)
	errorCode(t, f.catalog.Edit(t.Context(), media.EditRequest{MediaID: id, ExpectedRevision: rev}), "JOB_ACTIVE")
	if f.run(t, job) != id {
		t.Fatal("교체가 새 묶음을 생성했습니다")
	}
	got := success(t, f.catalog.Get(t.Context(), media.GetRequest{MediaID: id})).(media.Object)
	if got["title"] != "사용자 제목" || got["isFavorite"] != true {
		t.Fatalf("사용자 메타데이터 유실 %+v", got)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatal("기존 파일을 등록 커밋 전에 삭제했습니다")
	}
	assertFK(t, f.db)
}

func TestThumbnailSettingsAndDataLock(t *testing.T) {
	f := newFixture(t)
	id := f.run(t, f.enqueue(t))
	fileID := queryInt(t, f.db, "SELECT id FROM media_file WHERE media_id=?", id)
	var updates atomic.Int32
	thumbs := thumbnail.NewService(f.db, filepath.Join(f.root, "cache", "thumbnails"), "", func(int64) { updates.Add(1) })
	if err := thumbs.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer thumbs.Close()
	success(t, thumbs.Retry(t.Context(), thumbnail.Request{MediaID: id, FileID: fileID}))
	deadline := time.Now().Add(5 * time.Second)
	for updates.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	result := success(t, thumbs.Get(t.Context(), thumbnail.Request{MediaID: id, FileID: fileID})).(thumbnail.Status)
	if result.State != "registered" {
		t.Fatalf("썸네일 상태 %+v", result)
	}
	var thumb string
	f.db.QueryRow("SELECT thumbnail_path FROM media_file WHERE id=?", fileID).Scan(&thumb)
	if !strings.HasSuffix(thumb, ".jpg") {
		t.Fatal(thumb)
	}
	// Existing unknown settings must survive an atomic update and reopen.
	configFile := filepath.Join(f.root, "config.json")
	os.WriteFile(configFile, []byte(`{"unknown":{"keep":true},"cookieBrowser":"Edge"}`), 0600)
	config, err := settings.New(configFile, filepath.Join(f.root, "downloads"), settings.Dialogs{})
	if err != nil {
		t.Fatal(err)
	}
	if config.GetCookieBrowser() != "edge" {
		t.Fatal("브라우저 정규화 실패")
	}
	if ok, err := config.SetCookieBrowser("firefox"); !ok || err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(configFile)
	if !bytes.Contains(b, []byte(`"keep": true`)) {
		t.Fatal("기존 설정 유실")
	}
	if _, err = config.SetCookieBrowser("invalid"); err == nil {
		t.Fatal("미지원 브라우저 허용")
	}
	lock := filepath.Join(f.root, "library.lock")
	release, err := desktop.AcquireLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	if other, e := desktop.AcquireLock(lock); e == nil {
		other()
		t.Fatal("동일 DB 중복 실행 허용")
	}
	release()
	release, err = desktop.AcquireLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	release()
	assertFK(t, f.db)
}
