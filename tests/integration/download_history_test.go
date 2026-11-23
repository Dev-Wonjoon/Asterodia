package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/thumbnail"
)

func TestDownloadJobsIncludeLocalPreviewWithoutRemoteLookup(t *testing.T) {
	f := newFixture(t)
	job := f.enqueue(t)
	mediaID := f.run(t, job)
	thumbs := thumbnail.NewService(f.db, filepath.Join(f.root, "cache", "thumbnails"), "", nil)
	defer thumbs.Close()
	thumbs.ConfigurePreview(func(context.Context, string, string) (json.RawMessage, error) {
		t.Error("목록/상세 조회에서 웹 미리보기 호출")
		return nil, nil
	})
	f.service.ConfigurePreviewLookup(thumbs.CachedDownloadPreview)
	var source string
	if err := f.db.QueryRow("SELECT filepath FROM media_file WHERE media_id=?", mediaID).Scan(&source); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	cover := filepath.Join(f.root, "cover.png")
	if err = os.WriteFile(cover, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec("UPDATE media_file SET thumbnail_path=?,thumbnail_origin='app_generated' WHERE media_id=?", cover, mediaID); err != nil {
		t.Fatal(err)
	}
	cachedJob := success(t, f.service.Enqueue(t.Context(), download.EnqueueRequest{Mode: "create", URL: "https://www.instagram.com/p/PreviewFixture/?preview=cache"})).(download.Job)
	cache := filepath.Join(f.root, "cache", "previews", fmt.Sprintf("%x.jpg", sha256.Sum256([]byte(cachedJob.URL))))
	if err = os.MkdirAll(filepath.Dir(cache), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cache, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	// A list cursor must be closed before a callback queries this same pool.
	f.db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	data := success(t, f.service.List(ctx, download.ListRequest{})).(map[string]any)
	jobs := data["data"].([]download.Job)
	if len(jobs) != 2 {
		t.Fatal(jobs)
	}
	expected := map[string]string{job.ID: cover, cachedJob.ID: cache}
	for _, row := range jobs {
		if row.ThumbnailPath == nil || *row.ThumbnailPath != expected[row.ID] {
			t.Fatal("초기 목록 경로 누락", row)
		}
		detail := success(t, f.service.Get(ctx, download.JobRequest{JobID: row.ID})).(download.Job)
		if detail.ThumbnailPath == nil || *detail.ThumbnailPath != expected[row.ID] {
			t.Fatal("상세 경로 누락", detail)
		}
	}
	if err = os.Remove(cover); err != nil {
		t.Fatal(err)
	}
	missing := success(t, f.service.Get(ctx, download.JobRequest{JobID: job.ID})).(download.Job)
	if missing.ThumbnailPath != nil {
		t.Fatal("누락 파일 경로 반환", missing)
	}
	if err = os.WriteFile(cover, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	restored := success(t, f.service.Get(ctx, download.JobRequest{JobID: job.ID})).(download.Job)
	if restored.ThumbnailPath == nil || *restored.ThumbnailPath != cover {
		t.Fatal("복구 경로 미반영", restored)
	}
}

func TestDeleteDownloadHistoryReleasesOnlyUnusedPreview(t *testing.T) {
	f := newFixture(t)
	thumbs := thumbnail.NewService(f.db, filepath.Join(f.root, "cache", "thumbnails"), "", nil)
	defer thumbs.Close()
	calls := 0
	f.service.ConfigurePreviewCleanup(func(ctx context.Context, url string) {
		calls++
		if err := thumbs.ReleaseDownloadPreview(ctx, url); err != nil {
			t.Error(err)
		}
	})
	first := f.enqueue(t)
	cache := filepath.Join(f.root, "cache", "previews", fmt.Sprintf("%x.jpg", sha256.Sum256([]byte(first.URL))))
	if err := os.MkdirAll(filepath.Dir(cache), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, []byte("fixture preview"), 0600); err != nil {
		t.Fatal(err)
	}
	errorCode(t, f.service.Delete(t.Context(), download.JobRequest{JobID: first.ID}), "CANNOT_DELETE_REQUEST")
	if calls != 0 {
		t.Fatal("차단된 삭제에서 캐시 정리")
	}
	success(t, f.service.Cancel(t.Context(), download.JobRequest{JobID: first.ID}))
	second := f.enqueue(t)
	success(t, f.service.Cancel(t.Context(), download.JobRequest{JobID: second.ID}))
	if _, err := f.db.Exec(`CREATE TRIGGER reject_preview_history_delete BEFORE DELETE ON download_queue BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
		t.Fatal(err)
	}
	errorCode(t, f.service.Delete(t.Context(), download.JobRequest{JobID: first.ID}), "DATABASE_ERROR")
	if calls != 0 {
		t.Fatal("롤백된 삭제에서 캐시 정리")
	}
	if _, err := f.db.Exec(`DROP TRIGGER reject_preview_history_delete`); err != nil {
		t.Fatal(err)
	}
	success(t, f.service.Delete(t.Context(), download.JobRequest{JobID: first.ID}))
	if _, err := os.Stat(cache); err != nil {
		t.Fatal("다른 요청의 캐시 삭제", err)
	}
	success(t, f.service.Delete(t.Context(), download.JobRequest{JobID: second.ID}))
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatal("미사용 캐시 잔존", err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
	assertFK(t, f.db)
}

func TestDeleteDownloadHistoryPreservesMedia(t *testing.T) {
	for _, state := range []string{"failed", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			f := newFixture(t)
			original := f.enqueue(t)
			mediaID := f.run(t, original)
			var path string
			if err := f.db.QueryRow("SELECT filepath FROM media_file WHERE media_id=?", mediaID).Scan(&path); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			revision := queryInt(t, f.db, "SELECT revision FROM media WHERE id=?", mediaID)
			job := success(t, f.service.Enqueue(t.Context(), download.EnqueueRequest{URL: postURL, Mode: "replace", MediaID: &mediaID, ExpectedRevision: &revision})).(download.Job)
			if state == "cancelled" {
				success(t, f.service.Cancel(t.Context(), download.JobRequest{JobID: job.ID}))
			} else {
				f.gallery = func(context.Context, string, string) (engine.GalleryDLDiscovery, error) {
					return engine.GalleryDLDiscovery{}, errors.New("fixture failure")
				}
				for _, trigger := range []string{"manual_initial", "manual_retry"} {
					errorCode(t, f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: trigger}), "DOWNLOAD_ENGINE_FAILED")
				}
			}
			if _, err = f.db.Exec(`INSERT INTO download_target(id,attempt_id,target_key,source_url,metadata_json,position,state) SELECT 'target-'||id,id,'fixture','https://example.invalid/file','{}',1,'failed' FROM download_attempt WHERE job_id=?`, job.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec(`INSERT INTO maintenance_task(id,kind,state,owner_kind,download_attempt_id,payload_json,created_at,updated_at) SELECT 'task-'||id,'rollback_cleanup','completed','download',id,'{}',0,0 FROM download_attempt WHERE job_id=?`, job.ID); err != nil {
				t.Fatal(err)
			}
			success(t, f.service.Delete(t.Context(), download.JobRequest{JobID: job.ID}))
			errorCode(t, f.service.Get(t.Context(), download.JobRequest{JobID: job.ID}), "NOT_FOUND")
			for _, table := range []string{"download_attempt", "download_job_storage"} {
				if n := queryInt(t, f.db, "SELECT count(*) FROM "+table+" WHERE job_id=?", job.ID); n != 0 {
					t.Fatalf("%s: %d", table, n)
				}
			}
			if n := queryInt(t, f.db, "SELECT count(*) FROM download_target"); n != 1 {
				t.Fatalf("다른 요청 대상 보존 실패: %d", n)
			}
			if n := queryInt(t, f.db, "SELECT count(*) FROM maintenance_task WHERE owner_kind='download'"); n != 0 {
				t.Fatal("정리된 요청 이력 잔존")
			}
			if n := queryInt(t, f.db, "SELECT count(*) FROM media WHERE id=? AND revision=?", mediaID, revision); n != 1 {
				t.Fatal("갤러리 변경")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Fatal("미디어 파일 변경", err)
			}
			success(t, f.service.Get(t.Context(), download.JobRequest{JobID: original.ID}))
			assertFK(t, f.db)
		})
	}
}

func TestDeleteDownloadHistoryGuardsAndRollback(t *testing.T) {
	f := newFixture(t)
	errorCode(t, f.service.Delete(t.Context(), download.JobRequest{}), "INVALID_ARGUMENT")
	errorCode(t, f.service.Delete(t.Context(), download.JobRequest{JobID: "missing"}), "NOT_FOUND")
	job := f.enqueue(t)
	for _, state := range []string{"pending", "running", "interrupted"} {
		if _, err := f.db.Exec("UPDATE download_queue SET state=? WHERE id=?", state, job.ID); err != nil {
			t.Fatal(err)
		}
		errorCode(t, f.service.Delete(t.Context(), download.JobRequest{JobID: job.ID}), "CANNOT_DELETE_REQUEST")
	}
	if _, err := f.db.Exec("UPDATE download_queue SET state='failed' WHERE id=?", job.ID); err != nil {
		t.Fatal(err)
	}
	errorCode(t, f.service.Delete(t.Context(), download.JobRequest{JobID: job.ID}), "CANNOT_DELETE_REQUEST")
	if _, err := f.db.Exec("UPDATE download_queue SET state='pending' WHERE id=?", job.ID); err != nil {
		t.Fatal(err)
	}
	success(t, f.service.Cancel(t.Context(), download.JobRequest{JobID: job.ID}))
	if _, err := f.db.Exec("UPDATE download_queue SET cleanup_required=1 WHERE id=?", job.ID); err != nil {
		t.Fatal(err)
	}
	errorCode(t, f.service.Delete(t.Context(), download.JobRequest{JobID: job.ID}), "CLEANUP_REQUIRED")
	if _, err := f.db.Exec("UPDATE download_queue SET cleanup_required=0 WHERE id=?", job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO maintenance_task(id,kind,state,owner_kind,download_attempt_id,payload_json,created_at,updated_at) VALUES('cleanup','rollback_cleanup','pending','download',?,'{}',0,0)`, *job.CurrentAttemptID); err != nil {
		t.Fatal(err)
	}
	errorCode(t, f.service.Delete(t.Context(), download.JobRequest{JobID: job.ID}), "CANNOT_DELETE_REQUEST")
	if _, err := f.db.Exec("UPDATE maintenance_task SET state='completed' WHERE id='cleanup'"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`CREATE TRIGGER reject_history_delete BEFORE DELETE ON download_attempt BEGIN SELECT RAISE(ABORT,'fixture delete failure'); END`); err != nil {
		t.Fatal(err)
	}
	errorCode(t, f.service.Delete(t.Context(), download.JobRequest{JobID: job.ID}), "DATABASE_ERROR")
	remaining := success(t, f.service.Get(t.Context(), download.JobRequest{JobID: job.ID})).(download.Job)
	if remaining.CurrentAttemptID == nil || *remaining.CurrentAttemptID != *job.CurrentAttemptID || queryInt(t, f.db, "SELECT count(*) FROM maintenance_task WHERE id='cleanup'") != 1 {
		t.Fatal("삭제 롤백 실패")
	}
	assertFK(t, f.db)
	if _, err := f.db.Exec("DROP TRIGGER reject_history_delete"); err != nil {
		t.Fatal(err)
	}
	success(t, f.service.Delete(t.Context(), download.JobRequest{JobID: job.ID}))
	assertFK(t, f.db)
}

func TestDeleteImportedDownloadHistoryWithoutAttempts(t *testing.T) {
	f := newFixture(t)
	job := f.enqueue(t)
	if _, err := f.db.Exec(`UPDATE download_queue SET state='failed',current_attempt_id=NULL,options_json='{"_importedHistory":true}',log='legacy log' WHERE id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("DELETE FROM download_attempt WHERE job_id=?", job.ID); err != nil {
		t.Fatal(err)
	}
	success(t, f.service.Delete(t.Context(), download.JobRequest{JobID: job.ID}))
	errorCode(t, f.service.Logs(t.Context(), download.LogRequest{JobID: job.ID}), "NOT_FOUND")
	assertFK(t, f.db)
}

func TestDeleteDownloadHistoryProtectsCommittedAndActive(t *testing.T) {
	f := newFixture(t)
	job := f.enqueue(t)
	f.run(t, job)
	errorCode(t, f.service.Delete(t.Context(), download.JobRequest{JobID: job.ID}), "CANNOT_DELETE_REQUEST")
	if _, err := f.db.Exec("UPDATE download_queue SET state='failed' WHERE id=?", job.ID); err != nil {
		t.Fatal(err)
	}
	errorCode(t, f.service.Delete(t.Context(), download.JobRequest{JobID: job.ID}), "CANNOT_DELETE_REQUEST")
	assertFK(t, f.db)

	f = newFixture(t)
	job = f.enqueue(t)
	started := make(chan struct{})
	f.gallery = func(ctx context.Context, _, _ string) (engine.GalleryDLDiscovery, error) {
		close(started)
		<-ctx.Done()
		return engine.GalleryDLDiscovery{}, ctx.Err()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"})
	}()
	<-started
	errorCode(t, f.service.Delete(t.Context(), download.JobRequest{JobID: job.ID}), "ALREADY_PROCESSING")
	success(t, f.service.Cancel(t.Context(), download.JobRequest{JobID: job.ID}))
	<-done
	success(t, f.service.Delete(t.Context(), download.JobRequest{JobID: job.ID}))
	f.service.Close()
	errorCode(t, f.service.Delete(t.Context(), download.JobRequest{JobID: job.ID}), "NOT_READY")
}
