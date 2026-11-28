package thumbnail

import (
	"context"
	"database/sql"
	"github.com/Dev-Wonjoon/Asterodia/internal/api"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var cacheFilename = regexp.MustCompile(`^[0-9a-f]{64}\.jpg$`)

func (s *Service) CacheUsage(ctx context.Context) api.Result { return s.scanCache(ctx, false) }
func (s *Service) ClearUnusedCache(ctx context.Context) api.Result {
	leave, ok := s.UpdateGate.Enter()
	if !ok {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leave()
	return s.scanCache(ctx, true)
}

// Only old, unregistered, generated JPEGs are eligible. Originals, registered
// thumbnails, previews, temporary worker files and recent files are never removed.
func (s *Service) scanCache(ctx context.Context, clean bool) api.Result {
	if !s.cacheMu.TryLock() {
		return api.Failure("NOT_READY", "캐시 조회 또는 정리가 진행 중입니다.")
	}
	defer s.cacheMu.Unlock()
	registered := map[string]bool{}
	key := func(path string) string {
		path = filepath.Clean(path)
		if runtime.GOOS == "windows" {
			path = strings.ToLower(path)
		}
		return path
	}
	rows, err := s.db.QueryContext(ctx, `SELECT thumbnail_path FROM media_file WHERE thumbnail_path IS NOT NULL UNION ALL SELECT preview_path FROM media_file WHERE preview_path IS NOT NULL UNION ALL SELECT filepath FROM media_file`)
	if err != nil {
		return api.Failure("INTERNAL_ERROR", "캐시 참조를 읽지 못했습니다.")
	}
	for rows.Next() {
		var path string
		if err = rows.Scan(&path); err != nil {
			rows.Close()
			return api.Failure("INTERNAL_ERROR", "캐시 참조를 읽지 못했습니다.")
		}
		if rel, e := filepath.Rel(s.cache, path); e == nil && filepath.Base(rel) == rel {
			registered[key(path)] = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return api.Failure("INTERNAL_ERROR", "캐시 참조를 읽지 못했습니다.")
	}
	dir, err := os.Open(s.cache)
	if os.IsNotExist(err) {
		return api.Success(map[string]any{"files": 0, "bytes": 0, "unusedFiles": 0, "unusedBytes": 0, "removedFiles": 0, "removedBytes": 0})
	}
	if err != nil {
		return api.Failure("INTERNAL_ERROR", "캐시 폴더를 읽지 못했습니다.")
	}
	defer dir.Close()
	var count, total, unused, unusedBytes, removed, removedBytes int64
	cutoff := time.Now().Add(-24 * time.Hour)
	for {
		entries, e := dir.ReadDir(128)
		if e != nil && e != io.EOF {
			return api.Failure("INTERNAL_ERROR", "캐시 폴더를 읽지 못했습니다.")
		}
		for _, entry := range entries {
			if ctx.Err() != nil || s.ctx.Err() != nil {
				return api.Failure("NOT_READY", "캐시 작업이 취소됐습니다.")
			}
			if !cacheFilename.MatchString(entry.Name()) || !entry.Type().IsRegular() {
				continue
			}
			info, e := entry.Info()
			if e != nil {
				continue
			}
			count++
			total += info.Size()
			path := filepath.Join(s.cache, entry.Name())
			if registered[key(path)] || !info.ModTime().Before(cutoff) {
				continue
			}
			unused++
			unusedBytes += info.Size()
			if !clean {
				continue
			}
			// The generation worker publishes while holding mu. Hold the DB transaction
			// until removal as well, so no concurrent DB writer can acquire a reference.
			s.mu.Lock()
			tx, e := s.db.BeginTx(ctx, nil)
			if e != nil {
				s.mu.Unlock()
				return api.Failure("INTERNAL_ERROR", "캐시 참조를 확인하지 못했습니다.")
			}
			var exists bool
			e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM media_file WHERE filepath=? OR thumbnail_path=? OR preview_path=?)`, path, path, path).Scan(&exists)
			latest, statErr := os.Lstat(path)
			if e == nil && !exists && statErr == nil && latest.Mode().IsRegular() && latest.Size() == info.Size() && latest.ModTime() == info.ModTime() {
				if os.Remove(path) == nil {
					removed++
					removedBytes += info.Size()
				}
			}
			tx.Rollback()
			s.mu.Unlock()
			if e != nil && e != sql.ErrNoRows {
				return api.Failure("INTERNAL_ERROR", "캐시 참조를 확인하지 못했습니다.")
			}
		}
		if e == io.EOF {
			break
		}
	}
	return api.Success(map[string]any{"files": count - removed, "bytes": total - removedBytes, "unusedFiles": unused - removed, "unusedBytes": unusedBytes - removedBytes, "removedFiles": removed, "removedBytes": removedBytes})
}
