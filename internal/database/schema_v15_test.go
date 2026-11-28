package database

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestV14MigrationPreservesSourcesAndIndexesCachedPaths(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	filename := filepath.Join(root, "library.db")
	db, err := connectFile(filename, "rwc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, schemaV14SQL+";PRAGMA user_version=14;INSERT INTO platform(name) VALUES('Original source');"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(ctx, filename)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, retained int
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM platform WHERE name='Original source'").Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if version != 16 || retained != 1 {
		t.Fatal(version, retained)
	}
	var backupPath string
	if err = db.QueryRowContext(ctx, "SELECT backup_path FROM migration_run WHERE target_version=16").Scan(&backupPath); err != nil {
		t.Fatal(err)
	}
	backup, err := connectFile(backupPath, "ro")
	if err != nil {
		t.Fatal(err)
	}
	if err = backup.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	backup.Close()
	if version != 14 {
		t.Fatal("pre-migration backup was not preserved", version)
	}
	rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN SELECT EXISTS(SELECT 1 FROM media_file WHERE thumbnail_path=? OR preview_path=?)", "cached.jpg", "cached.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	plan := ""
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail + "\n"
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan, "SCAN media_file") || !strings.Contains(plan, "idx_file_thumbnail_path") || !strings.Contains(plan, "idx_file_preview_path") {
		t.Fatal("cached image lookup scans the library", plan)
	}
}
