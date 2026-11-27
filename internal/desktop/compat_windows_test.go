package desktop

import (
	"path/filepath"
	"testing"
)

func TestLibraryLockExcludesLegacyAndNewExecutables(t *testing.T) {
	directory := t.TempDir()
	unlock, err := AcquireLibraryLock(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"storagebucket.lock", "asterodia.lock"} {
		if release, err := AcquireLock(filepath.Join(directory, name)); err == nil {
			release()
			t.Fatalf("concurrent executable allowed: %s", name)
		}
	}
	unlock()
	legacyUnlock, err := AcquireLock(filepath.Join(directory, "storagebucket.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if release, err := AcquireLibraryLock(directory); err == nil {
		release()
		t.Fatal("legacy executable did not block migration")
	}
	legacyUnlock()
	release, err := AcquireLibraryLock(directory)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestDataDirectoryEnvironmentMigration(t *testing.T) {
	t.Setenv("STORAGEBUCKET_DATA_DIR", "legacy-data")
	t.Setenv("ASTERODIA_DATA_DIR", "")
	if DataDirectoryOverride() != "legacy-data" {
		t.Fatal("legacy data directory was not retained")
	}
	t.Setenv("ASTERODIA_DATA_DIR", "new-data")
	if DataDirectoryOverride() != "new-data" {
		t.Fatal("new data directory must take priority")
	}
}
