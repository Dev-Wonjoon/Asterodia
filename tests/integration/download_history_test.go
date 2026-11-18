package integration_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

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
