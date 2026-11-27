package download

import (
	"database/sql"
	"errors"
	"github.com/Dev-Wonjoon/Asterodia/internal/database"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestDownloadResourceThresholds(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "media")
	if err := os.WriteFile(path, make([]byte, 17), 0600); err != nil {
		t.Fatal(err)
	}
	free := func(string) (uint64, error) { return 100, nil }
	if !errors.Is(checkDownloadUsage(root, root, 16, 5, free), ErrResourceLimit) {
		t.Fatal("job byte limit was not enforced")
	}
	if !errors.Is(checkDownloadUsage(root, root, 100, 101, free), ErrResourceLimit) {
		t.Fatal("disk reserve was not enforced")
	}
	if err := checkDownloadUsage(root, root, 17, 100, free); err != nil {
		t.Fatal("exact thresholds must be allowed", err)
	}
}

func TestDownloadQueueHasAdmissionLimit(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err = database.Initialize(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	s := NewService(db, func() string { return root }, nil, nil)
	defer s.Close()
	firstID := ""
	for i := 0; i < MaxQueuedDownloads; i++ {
		result := s.Enqueue(t.Context(), EnqueueRequest{Mode: "create", URL: "https://example.com/" + strconv.Itoa(i), Options: engine.Options{}})
		if result.OK && i == 0 {
			firstID = result.Data.(Job).ID
		}
		if !result.OK {
			t.Fatalf("admission %d failed: %+v", i, result.Error)
		}
	}
	result := s.Enqueue(t.Context(), EnqueueRequest{Mode: "create", URL: "https://example.com/overflow"})
	if result.OK || result.Error.Code != "QUEUE_FULL" {
		t.Fatalf("queue admission was unbounded: %+v", result)
	}
	retry := s.Run(t.Context(), RunRequest{JobID: firstID, Trigger: "manual_retry"})
	if retry.OK || retry.Error.Code != "QUEUE_FULL" {
		t.Fatalf("queue limit was bypassed by retry: %+v", retry)
	}
	// A rejected retry must not hold the service lock needed for cancellation/close.
	if !s.mu.TryLock() {
		s.mu.Unlock()
		t.Fatal("rejected retry retained the service lock")
	}
	s.mu.Unlock()

}
