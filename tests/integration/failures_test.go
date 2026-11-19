package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/maintenance"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"github.com/Dev-Wonjoon/StorageBucket/internal/thumbnail"
)

func TestQueueSerializesEnginesAndCancelsWaitingJob(t *testing.T) {
	f := newFixture(t)
	started := make(chan struct{})
	var calls atomic.Int32
	f.gallery = func(ctx context.Context, _ string, _ string) (engine.GalleryDLDiscovery, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-ctx.Done()
		return engine.GalleryDLDiscovery{}, ctx.Err()
	}
	first := f.enqueue(t)
	second := success(t, f.service.Enqueue(t.Context(), download.EnqueueRequest{Mode: "create", URL: postURL + "?second=1"})).(download.Job)
	result1, result2 := make(chan struct{}, 1), make(chan struct{}, 1)
	go func() {
		f.service.Run(context.Background(), download.RunRequest{JobID: first.ID, Trigger: "manual_initial"})
		result1 <- struct{}{}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("첫 엔진 시작 실패")
	}
	go func() {
		f.service.Run(context.Background(), download.RunRequest{JobID: second.ID, Trigger: "manual_initial"})
		result2 <- struct{}{}
	}()
	// The second job may be cancelled either before or after it enters the slot wait.
	success(t, f.service.Cancel(t.Context(), download.JobRequest{JobID: second.ID}))
	select {
	case <-result2:
	case <-time.After(5 * time.Second):
		t.Fatal("대기 작업 취소 실패")
	}
	if calls.Load() != 1 {
		t.Fatal("여러 엔진이 동시에 실행됐습니다")
	}
	success(t, f.service.Cancel(t.Context(), download.JobRequest{JobID: first.ID}))
	select {
	case <-result1:
	case <-time.After(5 * time.Second):
		t.Fatal("실행 작업 취소 실패")
	}
	assertFK(t, f.db)
}

func TestReplacementCommitFailureKeepsOldLibrary(t *testing.T) {
	for _, mode := range []string{"db-insert-failure", "revision-conflict"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			id := f.run(t, f.enqueue(t))
			revision := queryInt(t, f.db, "SELECT revision FROM media WHERE id=?", id)
			var oldPath, hash string
			if err := f.db.QueryRow("SELECT filepath,content_hash FROM media_file WHERE media_id=?", id).Scan(&oldPath, &hash); err != nil {
				t.Fatal(err)
			}
			job := success(t, f.service.Enqueue(t.Context(), download.EnqueueRequest{URL: postURL, Mode: "replace", MediaID: &id, ExpectedRevision: &revision})).(download.Job)
			expected := "DOWNLOAD_ENGINE_FAILED"
			if mode == "db-insert-failure" {
				if _, err := f.db.Exec("CREATE TRIGGER fixture_failure BEFORE INSERT ON media_file BEGIN SELECT RAISE(ABORT,'fixture'); END"); err != nil {
					t.Fatal(err)
				}
			} else {
				expected = "REVISION_CONFLICT"
				if _, err := f.db.Exec("UPDATE media SET revision=revision+1 WHERE id=?", id); err != nil {
					t.Fatal(err)
				}
			}
			errorCode(t, f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"}), expected)
			var pathAfter, hashAfter string
			if err := f.db.QueryRow("SELECT filepath,content_hash FROM media_file WHERE media_id=?", id).Scan(&pathAfter, &hashAfter); err != nil {
				t.Fatal(err)
			}
			if pathAfter != oldPath || hashAfter != hash {
				t.Fatal("실패한 교체가 기존 등록을 변경했습니다")
			}
			if _, err := os.Stat(oldPath); err != nil {
				t.Fatal("실패한 교체가 기존 파일을 삭제했습니다")
			}
			publicFiles, err := os.ReadDir(filepath.Dir(oldPath))
			if err != nil || len(publicFiles) != 1 || publicFiles[0].Name() != filepath.Base(oldPath) {
				t.Fatalf("롤백된 새 다운로드 파일이 남았습니다: %v %v", publicFiles, err)
			}
			entries, err := os.ReadDir(filepath.Join(f.root, "downloads", ".storagebucket", "files"))
			if err != nil || len(entries) != 1 {
				t.Fatalf("실패 시 새 최종 폴더가 남음: %d %v", len(entries), err)
			}
			if queryInt(t, f.db, "SELECT count(*) FROM maintenance_task WHERE kind='obsolete_file_cleanup'") != 0 {
				t.Fatal("롤백된 교체의 정리 작업이 남았습니다")
			}
			assertFK(t, f.db)
		})
	}
}

func TestThumbnailSourceChangeAndMissingSource(t *testing.T) {
	f := newFixture(t)
	id := f.run(t, f.enqueue(t))
	fileID := queryInt(t, f.db, "SELECT id FROM media_file WHERE media_id=?", id)
	thumbs := thumbnail.NewService(f.db, filepath.Join(f.root, "cache", "thumbnails"), "", nil)
	defer thumbs.Close()
	var path string
	if err := f.db.QueryRow("SELECT filepath FROM media_file WHERE id=?", fileID).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	status := success(t, thumbs.Get(t.Context(), thumbnail.Request{MediaID: id, FileID: fileID})).(thumbnail.Status)
	if status.State != "missing" || status.CanRetry {
		t.Fatalf("원본 없음 상태 %+v", status)
	}
	errorCode(t, thumbs.Retry(t.Context(), thumbnail.Request{MediaID: id, FileID: fileID}), "NOT_RETRYABLE")
	errorCode(t, thumbs.Get(t.Context(), thumbnail.Request{MediaID: id, FileID: fileID + 100}), "NOT_FOUND")
}

func TestUnregisterSelectedCoverAndPreserveReferencedFile(t *testing.T) {
	f := newFixture(t)
	id := f.run(t, f.enqueue(t))
	fileID := queryInt(t, f.db, "SELECT id FROM media_file WHERE media_id=?", id)
	_, err := f.db.Exec(`INSERT INTO media_file(media_id,file_key,storage_kind,filepath,kind,position,created_at) SELECT media_id,'fixture:second','unverified_legacy',filepath,kind,1,created_at FROM media_file WHERE id=?`, fileID)
	if err != nil {
		t.Fatal(err)
	}
	revision := queryInt(t, f.db, "SELECT revision FROM media WHERE id=?", id)
	var req media.EditRequest
	encoded, _ := json.Marshal(map[string]any{"mediaId": id, "expectedRevision": revision, "cover": map[string]any{"mode": "manual", "fileId": fileID}})
	json.Unmarshal(encoded, &req)
	obj := success(t, f.catalog.Edit(t.Context(), req)).(media.Object)
	removed := success(t, f.catalog.UnregisterFiles(t.Context(), media.UnregisterRequest{MediaID: id, ExpectedRevision: obj["revision"].(int64), FileIDs: []int64{fileID}})).(media.Object)
	if removed["parentRemoved"] != false {
		t.Fatal("남은 파일이 있는 묶음을 삭제했습니다")
	}
	detail := success(t, f.catalog.Get(t.Context(), media.GetRequest{MediaID: id})).(media.Object)
	if detail["coverMode"] != "auto" || detail["fileCount"] != int64(1) {
		t.Fatal(detail)
	}
	cleaner := maintenance.NewCleaner(f.db)
	defer cleaner.Close()
	if err = cleaner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	var path string
	f.db.QueryRow("SELECT filepath FROM media_file WHERE media_id=?", id).Scan(&path)
	if _, err = os.Stat(path); err != nil {
		t.Fatal("다른 등록에서 참조하는 파일을 삭제했습니다")
	}
	assertFK(t, f.db)
}

func TestValidationAndUnknownSchemaPreserveData(t *testing.T) {
	f := newFixture(t)
	errorCode(t, f.service.Enqueue(t.Context(), download.EnqueueRequest{URL: "file:///private", Mode: "create"}), "INVALID_ARGUMENT")
	errorCode(t, f.service.Enqueue(t.Context(), download.EnqueueRequest{URL: postURL, Mode: "create", Options: engine.Options{Type: "audio", Extension: "mp4"}}), "INVALID_ARGUMENT")
	errorCode(t, f.catalog.List(t.Context(), media.ListRequest{Offset: -1}), "INVALID_ARGUMENT")
	errorCode(t, f.catalog.Suggest(t.Context(), media.SuggestRequest{Kind: "arbitrary"}), "INVALID_ARGUMENT")
	errorCode(t, f.service.Get(t.Context(), download.JobRequest{JobID: "unknown"}), "NOT_FOUND")
	// Initialize must refuse a nonempty database with an unknown version.
	if _, err := f.db.Exec("PRAGMA user_version=999"); err != nil {
		t.Fatal(err)
	}
	if err := database.Initialize(context.Background(), f.db); err == nil {
		t.Fatal("지원하지 않는 기존 스키마를 초기화했습니다")
	}
	if queryInt(t, f.db, "PRAGMA user_version") != 999 || queryInt(t, f.db, "SELECT count(*) FROM sqlite_master WHERE type='table'") == 0 {
		t.Fatal("기존 DB가 변경되었습니다")
	}
}
