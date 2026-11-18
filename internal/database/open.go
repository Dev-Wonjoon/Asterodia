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

// schemaSQL preserves the Electron v3 schema, including its integrity guards.
//
//go:embed schema.sql
var schemaSQL string

func Open(ctx context.Context, filename string) (*sql.DB, error) {
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
	uri := url.URL{Scheme: "file", Path: path, RawQuery: url.Values{"mode": {"rwc"}, "_pragma": {"foreign_keys(1)", "busy_timeout(5000)"}}.Encode()}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = Initialize(ctx, db); err != nil {
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
		return fmt.Errorf("기존 DB 버전 %d은 자동 변경하지 않습니다. Electron에서 v3 이관 후 열어 주세요", version)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("DB 초기화: %w", err)
	}
	if _, err = tx.ExecContext(ctx, "PRAGMA user_version = 3"); err != nil {
		return err
	}
	return tx.Commit()
}
