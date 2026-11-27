package database_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/database"
)

func TestBrandMigrationSnapshotsCommittedWALWithoutOverwriting(t *testing.T) {
	root := migrationRoot(t)
	legacy := filepath.Join(root, "storagebucket.db")
	source := latestDB(t, legacy)
	execDB(t, source, "PRAGMA journal_mode=WAL")
	execDB(t, source, "PRAGMA wal_autocheckpoint=0")
	execDB(t, source, "INSERT INTO tag(id,name) VALUES(1,'committed in WAL')")
	if info, err := os.Stat(legacy + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("fixture must have a nonempty WAL: %v", err)
	}
	before := fileDigest(t, legacy)
	target, err := database.ApplicationPath(t.Context(), root)
	if err != nil || target != filepath.Join(root, "asterodia.db") {
		t.Fatalf("migration path: %q, %v", target, err)
	}
	if fileDigest(t, legacy) != before {
		t.Fatal("legacy main database changed")
	}
	copy := latestDB(t, target)
	var value string
	if err := copy.QueryRow("SELECT name FROM tag WHERE id=1").Scan(&value); err != nil || value != "committed in WAL" {
		t.Fatalf("committed WAL data lost: %q, %v", value, err)
	}
	execDB(t, copy, "INSERT INTO tag(id,name) VALUES(3,'new library edit')")
	execDB(t, source, "INSERT INTO tag(id,name) VALUES(2,'legacy edit after migration')")
	if _, err := database.ApplicationPath(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := copy.QueryRow("SELECT count(*) FROM tag WHERE id=3").Scan(&count); err != nil || count != 1 {
		t.Fatal("an existing new library was overwritten")
	}
	if err := copy.QueryRow("SELECT count(*) FROM tag WHERE id=2").Scan(&count); err != nil || count != 0 {
		t.Fatal("migration unexpectedly repeated")
	}
}

func TestBrandMigrationFailureDoesNotPublishPartialDatabase(t *testing.T) {
	for _, mode := range []string{"corrupt", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			root := migrationRoot(t)
			legacy := filepath.Join(root, "storagebucket.db")
			ctx := t.Context()
			if mode == "corrupt" {
				if err := os.WriteFile(legacy, []byte("invalid database"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				latestDB(t, legacy)
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := database.ApplicationPath(ctx, root); err == nil {
				t.Fatal("invalid or cancelled migration accepted")
			}
			if _, err := os.Stat(filepath.Join(root, "asterodia.db")); !os.IsNotExist(err) {
				t.Fatalf("partial database published: %v", err)
			}
			if _, err := os.Stat(legacy); err != nil {
				t.Fatal("original database removed")
			}
		})
	}
}

func TestBrandMigrationFreshAndExistingPaths(t *testing.T) {
	root := migrationRoot(t)
	want := filepath.Join(root, "asterodia.db")
	if path, err := database.ApplicationPath(t.Context(), root); err != nil || path != want {
		t.Fatalf("fresh path: %q, %v", path, err)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatal("path resolution should not create an empty database")
	}
	if err := os.WriteFile(want, []byte("existing new database"), 0600); err != nil {
		t.Fatal(err)
	}
	before := fileDigest(t, want)
	if _, err := database.ApplicationPath(t.Context(), root); err != nil || fileDigest(t, want) != before {
		t.Fatal("existing new data changed")
	}
}
