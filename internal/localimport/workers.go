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
	ready   bool
}

// Folder queues and database access belong to the coordinator. Workers own a
// target until its result arrives; each folder commits in natural file order.
type importFolder struct {
	base                    target
	afterKey, afterID       string
	bootstrapped, exhausted bool
	pending                 []*inspection
	lastMedia               int64
	lastNotify              time.Time
}

type inspectionResult struct {
	item *inspection
	err  error
}

func (s *Service) nextImportFolder(ctx context.Context, job string, after int64) (*importFolder, int64, error) {
	f := &importFolder{}
	var row, mediaID int64
	err := s.db.QueryRowContext(ctx, `SELECT f.rowid,f.id,f.current_attempt_id,f.directory_path,f.directory_key,coalesce(d.import_media_id,0)
 FROM local_import_folder f LEFT JOIN media_directory d ON d.directory_key=f.directory_key AND d.key_version=1
 WHERE f.job_id=? AND f.state IN ('pending','running','interrupted') AND f.rowid>?
 ORDER BY f.rowid LIMIT 1`, job, after).Scan(&row, &f.base.folder, &f.base.attempt, &f.base.path, &f.base.key, &mediaID)
	if err == sql.ErrNoRows {
		return nil, after, nil
	}
	if err != nil {
		return nil, after, err
	}
	f.bootstrapped = mediaID != 0
	_, err = s.db.ExecContext(ctx, "UPDATE local_import_folder SET state='running' WHERE id=?", f.base.folder)
	return f, row, err
}

func (s *Service) nextInspection(ctx context.Context, root string, f *importFolder) (*inspection, error) {
	item := &inspection{target: f.base}
	var raw, key string
	err := s.db.QueryRowContext(ctx, `SELECT id,relative_path,expected_stat_json,refresh_requested,sort_key
 FROM local_import_target WHERE attempt_id=? AND state='pending' AND (sort_key,id)>(?,?)
 ORDER BY sort_key,id LIMIT 1`, f.base.attempt, f.afterKey, f.afterID).Scan(&item.target.id, &item.name, &raw, &item.refresh, &key)
	if err == sql.ErrNoRows {
		f.exhausted = true
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(raw), &item.target.state); err != nil {
		return nil, err
	}
	f.afterKey, f.afterID = key, item.target.id
	t := &item.target
	t.expectedSize = t.state.Size
	if filepath.Base(item.name) != item.name || item.name == "." || !filepath.IsLocal(item.name) || t.state.Path != filepath.Join(t.path, item.name) {
		item.problem = fileError(fmt.Errorf("파일의 탐색 경로가 일치하지 않습니다"))
	} else {
		item.problem = s.prepareTarget(ctx, root, t, item.refresh)
	}
	var failure *fileFailure
	if item.problem != nil && !errors.As(item.problem, &failure) {
		return nil, item.problem
	}
	return item, nil
}

func (s *Service) commitInspection(ctx context.Context, job, root string, f *importFolder, item *inspection) error {
	t := &item.target
	err := item.problem
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
		return s.recordFailure(ctx, job, t.id, t.expectedSize, err)
	}
	// A new group's next file must see the first successful checkpoint's group
	// ID. Failed/foreign files do not bootstrap a group.
	if t.mediaID > 0 {
		f.bootstrapped = true
		if s.notify != nil && (f.lastMedia != t.mediaID || time.Since(f.lastNotify) >= 500*time.Millisecond) {
			s.notify(t.mediaID)
			f.lastNotify = time.Now()
		}
		f.lastMedia = t.mediaID
	}
	return nil
}

func (s *Service) runInspections(ctx context.Context, job, root string, workers int) error {
	work, cancel := context.WithCancel(ctx)
	window := workers * 2
	jobs := make(chan *inspection, window)
	results := make(chan inspectionResult, window)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-work.Done():
					return
				case item := <-jobs:
					if work.Err() != nil {
						return
					}
					err := item.problem
					if err == nil {
						err = s.inspectTarget(work, root, &item.target)
					}
					select {
					case results <- inspectionResult{item, err}:
					case <-work.Done():
						return
					}
				}
			}
		}()
	}
	// Join all decoders before publishing a terminal state. Pending database
	// checkpoints remain resumable after pause, cancellation and Close.
	defer func() { cancel(); wg.Wait() }()

	// Bound both folder contexts and queued/running/completed targets. Fair
	// per-folder windows keep a slow ordered result from monopolizing the queue.
	folders := make([]*importFolder, workers)
	var afterFolder int64
	var allFolders bool
	var outstanding, dispatchNext, commitNext int
	var lastPathUpdate time.Time
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		for i, f := range folders {
			if f != nil && f.exhausted && len(f.pending) == 0 {
				if err := s.completeFolder(ctx, job, f.base.folder, f.base.attempt); err != nil {
					return err
				}
				if f.lastMedia > 0 && s.notify != nil {
					s.notify(f.lastMedia)
				}
				folders[i] = nil
			}
			if folders[i] == nil && !allFolders {
				next, row, err := s.nextImportFolder(ctx, job, afterFolder)
				if err != nil {
					return err
				}
				folders[i], afterFolder = next, row
				allFolders = next == nil
			}
		}
		activeFolders := 0
		for _, f := range folders {
			if f != nil {
				activeFolders++
			}
		}
		if allFolders && activeFolders == 0 {
			return nil
		}
		perFolder := max(2, window/max(1, activeFolders))
		finishedFolder := false
		for outstanding < window {
			advanced := false
			for n := range len(folders) {
				i := (dispatchNext + n) % len(folders)
				f := folders[i]
				if f == nil || f.exhausted || len(f.pending) >= perFolder || (!f.bootstrapped && len(f.pending) != 0) {
					continue
				}
				item, err := s.nextInspection(ctx, root, f)
				if err != nil {
					return err
				}
				advanced = true
				dispatchNext = (i + 1) % len(folders)
				if item == nil {
					finishedFolder = len(f.pending) == 0 || finishedFolder
					break
				}
				f.pending = append(f.pending, item)
				outstanding++
				path := filepath.Join(item.target.path, item.name)
				s.setCurrentPath(job, path)
				if time.Since(lastPathUpdate) >= 200*time.Millisecond {
					if _, err = s.db.ExecContext(ctx, "UPDATE local_import_job SET current_path=?,updated_at=?,revision=revision+1 WHERE id=?", path, time.Now().UnixMilli(), job); err != nil {
						return err
					}
					lastPathUpdate = time.Now()
				}
				select {
				case jobs <- item:
				case <-ctx.Done():
					return ctx.Err()
				}
				break
			}
			if !advanced {
				break
			}
		}

		// Commit one ready ordered result, then refill its slot before the next
		// database write. Unrelated folders need not wait for a slow result.
	collect:
		for {
			select {
			case result := <-results:
				result.item.problem, result.item.ready = result.err, true
			default:
				break collect
			}
		}
		committed := false
		for n := range len(folders) {
			i := (commitNext + n) % len(folders)
			f := folders[i]
			if f == nil || len(f.pending) == 0 || !f.pending[0].ready {
				continue
			}
			if err := s.commitInspection(ctx, job, root, f, f.pending[0]); err != nil {
				return err
			}
			f.pending[0] = nil
			f.pending = f.pending[1:]
			outstanding--
			commitNext = (i + 1) % len(folders)
			committed = true
			break
		}
		if committed || finishedFolder || outstanding == 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result := <-results:
			result.item.problem, result.item.ready = result.err, true
		}
	}
}
