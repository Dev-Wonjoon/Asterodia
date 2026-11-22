package database_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
)

func TestElectronDatabaseSupportEndsAt201(t *testing.T) {
	for _, version := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			root := migrationRoot(t)
			path := filepath.Join(root, "electron.db")
			db := rawDB(t, path)
			// Rejection depends on the unsupported format version, not archived DDL.
			execDB(t, db, "CREATE TABLE __drizzle_migrations(hash TEXT,created_at NUMERIC)")
			execDB(t, db, "CREATE TABLE media(id INTEGER PRIMARY KEY,title TEXT,filepath TEXT)")
			execDB(t, db, "INSERT INTO media VALUES(7,'keep','original.mp4')")
			execDB(t, db, fmt.Sprintf("PRAGMA user_version=%d", version))
			db.Close()
			before := fileDigest(t, path)
			checkError := func(err error) {
				t.Helper()
				if err == nil || !strings.Contains(err.Error(), "2.1.0") || !strings.Contains(err.Error(), "Electron") {
					t.Fatalf("expected explicit Electron support notice, got %v", err)
				}
			}
			opened, err := database.Open(t.Context(), path)
			if opened != nil {
				opened.Close()
			}
			checkError(err)
			opened, err = database.OpenExisting(t.Context(), path)
			if opened != nil {
				opened.Close()
			}
			checkError(err)
			db = rawDB(t, path)
			result, err := database.Migrate(t.Context(), db, database.MigrationOptions{BackupDirectory: filepath.Join(root, "backups")})
			checkError(err)
			if result.Migrated || result.BackupPath != "" {
				t.Fatal("unsupported DB was migrated or backed up", result)
			}
			checkError(database.Initialize(t.Context(), db))
			if countDB(t, db, "PRAGMA user_version") != int64(version) || countDB(t, db, "SELECT count(*) FROM media WHERE id=7 AND title='keep'") != 1 {
				t.Fatal("source changed")
			}
			db.Close()
			target, service, _ := importer(t, root)
			imported, err := service.Import(t.Context(), path)
			checkError(err)
			if imported.SourceSnapshot != "" || imported.BackupPath != "" || imported.AddedMedia != 0 {
				t.Fatal("unsupported DB import created side effects", imported)
			}
			if countDB(t, target, "SELECT count(*) FROM media") != 0 {
				t.Fatal("target library changed")
			}
			if before != fileDigest(t, path) {
				t.Fatal("source file changed")
			}
			backups, err := filepath.Glob(filepath.Join(root, "backups", "*.db"))
			if err != nil || len(backups) != 0 {
				t.Fatal("unsupported DB created backups", backups, err)
			}
		})
	}
}

func TestUnsupportedDatabaseVersionPreservesSource(t *testing.T) {
	for _, version := range []int{-1, 99} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			root := migrationRoot(t)
			path := filepath.Join(root, "unknown.db")
			db := rawDB(t, path)
			execDB(t, db, "CREATE TABLE user_data(value TEXT);INSERT INTO user_data VALUES('keep')")
			execDB(t, db, fmt.Sprintf("PRAGMA user_version=%d", version))
			db.Close()
			before := fileDigest(t, path)
			if opened, err := database.Open(t.Context(), path); err == nil {
				opened.Close()
				t.Fatal("unsupported DB was opened")
			}
			if before != fileDigest(t, path) {
				t.Fatal("source changed")
			}
		})
	}
}
