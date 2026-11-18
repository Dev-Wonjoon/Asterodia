package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const schemaVersion = 3

func OpenExisting(ctx context.Context, filename string) (*sql.DB, error) {
	if !filepath.IsAbs(filename) || strings.ContainsRune(filename, '\x00') {
		return nil, fmt.Errorf("SQLite 파일의 올바른 절대 경로가 필요합니다")
	}

	path := filepath.ToSlash(filepath.Clean(filename))
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	params := url.Values{
		"mode":    {"rw"},
		"_pragma": {"foreign_keys(1)", "busy_timeout(5000)"},
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: params.Encode()}

	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, fmt.Errorf("SQLite 연결 구성 실패: %w", err)
	}
	db.SetMaxOpenConns(1)

	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("SQLite 파일 열기 또는 버전 확인 실패: %w", err)
	}
	if version != schemaVersion {
		_ = db.Close()
		return nil, fmt.Errorf("지원하지 않는 SQLite 스키마 버전 %d, 기대값 %d",
			version, schemaVersion)
	}
	return db, nil
}
