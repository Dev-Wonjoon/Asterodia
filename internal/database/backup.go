package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

func connectFile(filename, mode string) (*sql.DB, error) {
	if !filepath.IsAbs(filename) || strings.ContainsRune(filename, 0) {
		return nil, fmt.Errorf("DB 절대 경로가 필요합니다")
	}
	path := filepath.ToSlash(filepath.Clean(filename))
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: url.Values{"mode": {mode}, "_pragma": {"foreign_keys(1)", "busy_timeout(5000)", "temp_store(2)"}}.Encode()}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// Backup uses SQLite's snapshot operation so committed WAL data is included.
// It never copies just the main .db file of a running SQLite connection.
func Backup(ctx context.Context, db *sql.DB, directory, purpose string) (string, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return backup(ctx, conn, directory, purpose)
}
func backup(ctx context.Context, q connection, directory, purpose string) (string, error) {
	if purpose == "" || strings.ContainsAny(purpose, "/\\:\x00") {
		return "", fmt.Errorf("올바르지 않은 백업 종류입니다")
	}
	if !filepath.IsAbs(directory) {
		return "", fmt.Errorf("백업 폴더 절대 경로가 필요합니다")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(directory, "asterodia."+purpose+"."+time.Now().UTC().Format("20060102T150405.000")+"."+uuid.NewString()+".db")
	reserved, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	if err = reserved.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	// This operation owns the reserved empty file. SQLite can fill it atomically
	// as a snapshot without replacing any pre-existing backup.
	if _, err := q.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("DB 스냅샷 생성 실패: %w", err)
	}
	copy, err := connectFile(path, "ro")
	if err != nil {
		return "", err
	}
	defer copy.Close()
	if err = healthy(ctx, copy); err != nil {
		_ = copy.Close()
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
