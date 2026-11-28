package thumbnail_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheCleanupPreservesRegisteredOriginalRecentAndWorkerFiles(t *testing.T) {
	f := newFixture(t, "")
	cache := filepath.Join(f.root, "cache")
	must(t, os.MkdirAll(cache, 0700))
	paths := []string{}
	old := time.Now().Add(-48 * time.Hour)
	for i := 0; i < 4; i++ {
		path := filepath.Join(cache, fmt.Sprintf("%064d.jpg", i))
		writeImage(t, path)
		must(t, os.Chtimes(path, old, old))
		paths = append(paths, path)
	}
	seed(t, f.db, filepath.Join(f.root, "original.jpg"), "image", paths[0])
	seed(t, f.db, paths[1], "image", nil)
	must(t, os.Chtimes(paths[2], time.Now(), time.Now()))
	worker := filepath.Join(cache, "thumbnail-worker.jpg")
	writeImage(t, worker)
	usage := ok(t, f.service.CacheUsage(t.Context())).(map[string]any)
	if usage["files"] != int64(4) || usage["unusedFiles"] != int64(1) {
		t.Fatal(usage)
	}
	cleaned := ok(t, f.service.ClearUnusedCache(t.Context())).(map[string]any)
	if cleaned["removedFiles"] != int64(1) {
		t.Fatal(cleaned)
	}
	if _, err := os.Stat(paths[3]); !os.IsNotExist(err) {
		t.Fatal("orphan was not removed", err)
	}
	for _, path := range append(paths[:3], worker) {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("protected file removed", path, err)
		}
	}
}
