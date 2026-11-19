package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Keep the historical v3 DDL intact for migration validation.
//
//go:embed schema.sql
var schemaV3SQL string

var schemaV4SQL = strings.Replace(strings.Replace(schemaV3SQL, "kind = 'image' AND canonical_path_key", "kind IN ('image','video','audio') AND canonical_path_key", 1), "AND width IS NOT NULL AND height IS NOT NULL)),", "AND (kind = 'audio' OR (width IS NOT NULL AND height IS NOT NULL)) AND (kind = 'image' OR duration IS NOT NULL))),", 1)

var schemaV5SQL = schemaV5()
var schemaSQL = schemaV6()

func Open(ctx context.Context, filename string) (*sql.DB, error) {
	return OpenWithOptions(ctx, filename, MigrationOptions{BackupDirectory: filepath.Join(filepath.Dir(filename), "backups"), LegacyStorageRoot: filepath.Join(filepath.Dir(filename), "downloads")})
}

func OpenWithOptions(ctx context.Context, filename string, options MigrationOptions) (*sql.DB, error) {
	if !filepath.IsAbs(filename) || strings.ContainsRune(filename, 0) {
		return nil, fmt.Errorf("DB 절대 경로가 필요합니다")
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return nil, err
	}
	path := filepath.ToSlash(filename)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: url.Values{"mode": {"rwc"}, "_pragma": {"foreign_keys(1)", "busy_timeout(5000)", "temp_store(2)"}}.Encode()}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var version, tables int
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err == nil {
		err = db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables)
	}
	if err == nil {
		if version != schemaVersion && tables > 0 {
			_, err = Migrate(ctx, db, options)
		} else {
			err = Initialize(ctx, db)
		}
	}
	if err == nil {
		_, err = validateSchema(ctx, db, schemaVersion)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Initialize only initializes an empty database. Unknown existing schemas are
// never replaced or silently treated as a new library.
func Initialize(ctx context.Context, db *sql.DB) error {
	var version, tables int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version == schemaVersion {
		return nil
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
		return err
	}
	if version != 0 || tables != 0 {
		return fmt.Errorf("기존 DB 버전 %d은 빈 DB로 초기화할 수 없습니다", version)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("DB 초기화: %w", err)
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}
