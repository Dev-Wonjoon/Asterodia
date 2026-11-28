package database

import (
	"context"
	"path/filepath"
	"testing"
)

func TestV15MigrationRetainsClassificationsAndBacksUpBeforeDateCache(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v15.db")
	db, err := connectFile(path, "rwc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, schemaV15SQL+";PRAGMA user_version=15;INSERT INTO platform(name) VALUES('Manual platform');"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, count int
	db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	db.QueryRowContext(ctx, "SELECT count(*) FROM platform WHERE name='Manual platform'").Scan(&count)
	if version != 16 || count != 1 {
		t.Fatal(version, count)
	}
	var backupPath string
	if err = db.QueryRowContext(ctx, "SELECT backup_path FROM migration_run WHERE target_version=16").Scan(&backupPath); err != nil {
		t.Fatal(err)
	}
	backup, err := connectFile(backupPath, "ro")
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	if err = backup.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 15 {
		t.Fatal("backup was not v15", version, err)
	}
}
