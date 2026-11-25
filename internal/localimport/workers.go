package localimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

func importWorkerCount(cpus int) int { return max(1, cpus/2) }

type inspection struct {
	target  target
	name    string
	refresh bool
	problem error
	done    chan error
}

type importProgress struct {
	lastMedia                  int64
	lastNotify, lastPathUpdate time.Time
}

func (s *Service) pendingInspections(ctx context.Context, base target, limit int) ([]*inspection, error) {
	// Bootstrap a new group's first successful checkpoint before inspecting its
	// remaining files. All prepared targets must capture the same import media ID.
	var mediaID int64
	err := s.db.QueryRowContext(ctx, "SELECT coalesce(import_media_id,0) FROM media_directory WHERE directory_key=? AND key_version=1", base.key).Scan(&mediaID)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if mediaID == 0 {
		limit = 1
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,relative_path,expected_stat_json,refresh_requested FROM local_import_target WHERE attempt_id=? AND state='pending' ORDER BY sort_key,id LIMIT ?", base.attempt, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var batch []*inspection
	for rows.Next() {
		item := &inspection{target: base, done: make(chan error, 1)}
		var raw string
		if err = rows.Scan(&item.target.id, &item.name, &raw, &item.refresh); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &item.target.state); err != nil {
			return nil, err
		}
		item.target.expectedSize = item.target.state.Size
		batch = append(batch, item)
	}
	return batch, rows.Err()
}

func (s *Service) runFolder(ctx context.Context, job, root string, base target, workers int) error {
	progress := importProgress{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// At most two targets per worker exist in memory, including completed
		// results waiting for an earlier file. No unbounded reorder buffer.
		batch, err := s.pendingInspections(ctx, base, workers*2)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			break
		}
		for _, item := range batch {
			t := &item.target
			if filepath.Base(item.name) != item.name || item.name == "." || !filepath.IsLocal(item.name) || t.state.Path != filepath.Join(t.path, item.name) {
				item.problem = fileError(fmt.Errorf("파일의 탐색 경로가 일치하지 않습니다"))
			} else {
				item.problem = s.prepareTarget(ctx, root, t, item.refresh)
			}
			var failure *fileFailure
			if item.problem != nil && !errors.As(item.problem, &failure) {
				return item.problem
			}
		}
		if err = s.inspectBatch(ctx, job, root, batch, workers, &progress); err != nil {
			return err
		}
	}
	if progress.lastMedia > 0 && s.notify != nil {
		s.notify(progress.lastMedia)
	}
	return nil
}

func (s *Service) inspectBatch(ctx context.Context, job, root string, batch []*inspection, workers int, progress *importProgress) error {
	work, cancel := context.WithCancel(ctx)
	jobs := make(chan *inspection, len(batch))
	for _, item := range batch {
		jobs <- item
	}
	close(jobs)
	var wg sync.WaitGroup
	for range min(workers, len(batch)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				if work.Err() != nil {
					return
				}
				err := item.problem
				if err == nil {
					err = s.inspectTarget(work, root, &item.target)
				}
				item.done <- err
			}
		}()
	}
	// Cancellation/fatal errors join every worker before the job becomes
	// resumable or Close returns. Uncommitted targets stay pending on disk.
	defer func() { cancel(); wg.Wait() }()
	for _, item := range batch {
		// Only the coordinator accesses this target after receiving its result.
		path := filepath.Join(item.target.path, item.name)
		s.setCurrentPath(job, path)
		if time.Since(progress.lastPathUpdate) >= 200*time.Millisecond {
			if _, err := s.db.ExecContext(ctx, "UPDATE local_import_job SET current_path=?,updated_at=?,revision=revision+1 WHERE id=?", path, time.Now().UnixMilli(), job); err != nil {
				return err
			}
			progress.lastPathUpdate = time.Now()
		}
		var err error
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err = <-item.done:
		}
		t := &item.target
		if err == nil {
			err = s.commitTarget(ctx, job, root, t)
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var failure *fileFailure
			if !errors.As(err, &failure) {
				return err
			}
			if err = s.recordFailure(ctx, job, t.id, t.expectedSize, err); err != nil {
				return err
			}
		} else if t.mediaID > 0 && s.notify != nil {
			if progress.lastMedia != t.mediaID || time.Since(progress.lastNotify) >= 500*time.Millisecond {
				s.notify(t.mediaID)
				progress.lastNotify = time.Now()
			}
			progress.lastMedia = t.mediaID
		}
	}
	return nil
}
