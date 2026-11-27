package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

const legacyDatabaseName = "storagebucket.db"
const legacyDatabaseIdentity = "_storagebucket_database"

func isDatabaseIdentity(value string) bool {
	return value == "_asterodia_database" || value == legacyDatabaseIdentity
}

// ApplicationPath must run while both the current and legacy library locks are
// held. The old database is retained, and committed WAL content is copied through
// SQLite rather than copying or renaming the live database files individually.
func ApplicationPath(ctx context.Context, root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("DB 폴더의 절대 경로가 필요합니다")
	}
	target := filepath.Join(root, "asterodia.db")
	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("DB 경로가 일반 파일이 아닙니다: %s", target)
		}
		return target, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	legacy := filepath.Join(root, legacyDatabaseName)
	if info, err := os.Lstat(legacy); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("이전 DB 경로가 일반 파일이 아닙니다: %s", legacy)
		}
	} else if os.IsNotExist(err) {
		return target, nil
	} else {
		return "", err
	}
	source, err := connectFile(legacy, "ro")
	if err != nil {
		return "", err
	}
	defer source.Close()
	var version int
	if err := source.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return "", err
	}
	if err := supportedDatabaseVersion(version); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(root, ".asterodia-migration-*.db")
	if err != nil {
		return "", err
	}
	name := temp.Name()
	defer os.Remove(name) // Only this operation's temporary file is removed.
	if err := temp.Close(); err != nil {
		return "", err
	}
	if _, err := source.ExecContext(ctx, "VACUUM INTO ?", name); err != nil {
		return "", fmt.Errorf("이전 라이브러리 스냅샷 생성 실패: %w", err)
	}
	check, err := connectFile(name, "ro")
	if err != nil {
		return "", err
	}
	err = healthy(ctx, check)
	_ = check.Close()
	if err != nil {
		return "", fmt.Errorf("이전 라이브러리 스냅샷 검증 실패: %w", err)
	}
	copy, err := os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	err = copy.Sync()
	closeErr := copy.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	// Library locks serialize publication; an existing new database always wins.
	if _, err := os.Lstat(target); err == nil {
		return target, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Rename(name, target); err != nil {
		return "", err
	}
	return target, nil
}
