package media

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type storeConnection interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Store owns shared directory registration, regardless of the media's origin.
// Callers supply path identity evidence; this object never opens media files.
type Store struct{ q storeConnection }

func NewStore(q storeConnection) *Store { return &Store{q: q} }

func (s *Store) EnsureDirectory(ctx context.Context, path, key string, version int, registered bool) (int64, error) {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) || key == "" || (version != 0 && version != 1) {
		return 0, fmt.Errorf("디렉터리 경로와 식별 정보를 확인하세요")
	}
	path = filepath.Clean(path)
	if version == 0 && key != "recorded-v1:"+path {
		return 0, fmt.Errorf("기록된 디렉터리 경로와 식별자가 다릅니다")
	}
	if version == 1 && (!strings.HasPrefix(key, "winpath-v1:") || strings.ToLower(filepath.Clean(strings.TrimPrefix(key, "winpath-v1:"))) != strings.ToLower(path)) {
		return 0, fmt.Errorf("디렉터리 경로와 정규화 식별자가 다릅니다")
	}
	var id int64
	var storedPath string
	err := s.q.QueryRowContext(ctx, "SELECT id,directory_path FROM media_directory WHERE directory_key=? AND key_version=?", key, version).Scan(&id, &storedPath)
	if err == nil && strings.ToLower(filepath.Clean(storedPath)) != strings.ToLower(path) {
		return 0, fmt.Errorf("같은 디렉터리 식별자에 다른 경로가 기록되어 있습니다")
	}
	if err == sql.ErrNoRows {
		var oldKey string
		var oldVersion int
		err = s.q.QueryRowContext(ctx, "SELECT id,directory_key,key_version FROM media_directory WHERE directory_path=?", path).Scan(&id, &oldKey, &oldVersion)
		if err == nil && oldKey != key && version == 1 {
			if oldVersion != 0 {
				return 0, fmt.Errorf("디렉터리 식별 정보가 변경되었습니다")
			}
			_, err = s.q.ExecContext(ctx, "UPDATE media_directory SET directory_key=?,key_version=1 WHERE id=? AND key_version=0", key, id)
		}
	}
	if err == sql.ErrNoRows {
		result, e := s.q.ExecContext(ctx, "INSERT INTO media_directory(directory_path,directory_key,key_version,registered,created_at) VALUES(?,?,?,?,?)", path, key, version, registered, time.Now().UnixMilli())
		if e != nil {
			return 0, e
		}
		id, err = result.LastInsertId()
	}
	if err != nil {
		return 0, err
	}
	if registered {
		_, err = s.q.ExecContext(ctx, "UPDATE media_directory SET registered=1 WHERE id=?", id)
	}
	return id, err
}

// RecordedDirectory deliberately does not resolve paths from imported databases.
// Version 0 denotes recorded spelling, not verified filesystem identity.
func (s *Store) RecordedDirectory(ctx context.Context, path string, registered bool) (int64, error) {
	return s.EnsureDirectory(ctx, path, "recorded-v1:"+filepath.Clean(path), 0, registered)
}
func (s *Store) SetImportMedia(ctx context.Context, directory, mediaID int64) error {
	result, err := s.q.ExecContext(ctx, "UPDATE media_directory SET import_media_id=? WHERE id=? AND (import_media_id IS NULL OR import_media_id=?)", mediaID, directory, mediaID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("디렉터리의 기본 가져오기 묶음이 변경되었습니다")
	}
	return nil
}
