package download_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
)

func TestGalleryActivityIndependentOfHistoryPage(t *testing.T) {
	db := openStoreTestDB(t)
	store := download.NewStore(db)
	service := download.NewService(db, nil, nil, nil)
	defer service.Close()
	for i, state := range []string{"pending", "running", "failed", "cancelled", "interrupted", "completed", "cleanup", "rollback", "history"} {
		job, err := store.CreateInitialAttempt(t.Context(), download.InitialAttemptInput{OriginalURL: fmt.Sprintf("https://example.invalid/%d", i), StorageRoot: filepath.Join(os.Getenv("STORAGEBUCKET_TEST_ROOT"), "downloads")})
		if err != nil {
			t.Fatal(err)
		}
		if state == "cleanup" {
			execStoreTestSQL(t, db, "UPDATE download_queue SET cleanup_required=1 WHERE id=?", job.JobID)
		} else if state == "rollback" {
			execStoreTestSQL(t, db, "UPDATE download_attempt SET phase='rolling_back' WHERE id=?", job.AttemptID)
		} else if state == "history" {
			execStoreTestSQL(t, db, `UPDATE download_queue SET options_json='{"_importedHistory":true}' WHERE id=?`, job.JobID)
		} else {
			execStoreTestSQL(t, db, "UPDATE download_queue SET state=?,created_at=? WHERE id=?", state, i, job.JobID)
		}
	}
	ctx := t.Context()
	for _, offset := range []int{0, 3, 20} {
		result := service.List(ctx, download.ListRequest{Limit: 1, Offset: offset})
		if !result.OK {
			t.Fatal(result.Error)
		}
		page := result.Data.(map[string]any)
		active := page["activeJobs"].([]download.Job)
		if len(active) != 2 {
			t.Fatalf("활성 작업 %d개: %+v", len(active), active)
		}
		for _, job := range active {
			if job.State != "pending" && job.State != "running" {
				t.Fatal(job)
			}
		}
	}
	var mediaCount, historyCount int
	if err := db.QueryRow("SELECT count(*) FROM media").Scan(&mediaCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM download_queue").Scan(&historyCount); err != nil {
		t.Fatal(err)
	}
	if mediaCount != 0 || historyCount != 9 {
		t.Fatal("조회가 미디어 또는 이력을 변경", mediaCount, historyCount)
	}
}
