package localimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type connection interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Service) setCurrentPath(id, path string) {
	s.mu.Lock()
	if s.active == id {
		s.currentPath = path
	}
	s.mu.Unlock()
}

func emptyCounts() string { raw, _ := json.Marshal(Counts{}); return string(raw) }
func changeCounts(ctx context.Context, q connection, id string, change func(*Counts)) error {
	var raw string
	if err := q.QueryRowContext(ctx, "SELECT counts_json FROM local_import_job WHERE id=?", id).Scan(&raw); err != nil {
		return err
	}
	var c Counts
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return err
	}
	change(&c)
	c.Excluded = c.ScanExcluded + c.UserExcluded
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, "UPDATE local_import_job SET counts_json=?,revision=revision+1,updated_at=? WHERE id=?", string(b), time.Now().UnixMilli(), id)
	return err
}

// A bounded SQL aggregate repairs summaries from durable per-file facts after
// a crash or a user retry. Normal per-file commits update counters atomically.
func refreshCounts(ctx context.Context, q connection, id string) error {
	c := Counts{}
	if err := q.QueryRowContext(ctx, `SELECT count(*),
 coalesce(sum(max(0,coalesce(json_extract(t.expected_stat_json,'$.size'),0))),0),
 coalesce(sum(CASE WHEN t.state='failed' THEN 1 ELSE 0 END),0),
 coalesce(sum(CASE WHEN t.state='excluded' THEN 1 ELSE 0 END),0),
 coalesce(sum(CASE WHEN t.committed_at IS NOT NULL OR t.state IN ('failed','excluded') OR (j.work_version=1 AND a.committed_at IS NOT NULL) THEN 1 ELSE 0 END),0),
 coalesce(sum(CASE WHEN t.committed_at IS NOT NULL OR t.state IN ('failed','excluded') THEN max(0,coalesce(json_extract(t.expected_stat_json,'$.size'),0)) ELSE 0 END),0),
 coalesce(sum(CASE WHEN t.result_kind='added' OR (j.work_version=1 AND a.committed_at IS NOT NULL AND t.state='inspected') THEN 1 ELSE 0 END),0),
 coalesce(sum(CASE WHEN t.result_kind='updated' OR (j.work_version=1 AND a.committed_at IS NOT NULL AND t.state='changed') THEN 1 ELSE 0 END),0),
 coalesce(sum(CASE WHEN t.result_kind='unchanged' OR (j.work_version=1 AND a.committed_at IS NOT NULL AND t.state='unchanged') THEN 1 ELSE 0 END),0)
 FROM local_import_target t JOIN local_import_attempt a ON a.id=t.attempt_id JOIN local_import_folder f ON f.id=a.import_folder_id JOIN local_import_job j ON j.id=f.job_id WHERE j.id=? AND a.id=f.current_attempt_id`, id).Scan(&c.Discovered, &c.TotalBytes, &c.Failed, &c.UserExcluded, &c.Processed, &c.ProcessedBytes, &c.Added, &c.Updated, &c.Unchanged); err != nil {
		return err
	}
	if err := q.QueryRowContext(ctx, "SELECT coalesce(sum(excluded_count),0),coalesce(sum(CASE WHEN state='failed' THEN 1 ELSE 0 END),0) FROM local_import_scan_directory WHERE job_id=?", id).Scan(&c.ScanExcluded, &c.ScanErrors); err != nil {
		return err
	}
	if err := q.QueryRowContext(ctx, "SELECT coalesce(sum(CASE WHEN state='completed' THEN 1 ELSE 0 END),0),coalesce(sum(CASE WHEN state IN ('failed','completed_with_errors') THEN 1 ELSE 0 END),0) FROM local_import_folder WHERE job_id=?", id).Scan(&c.CompletedFolders, &c.FailedFolders); err != nil {
		return err
	}
	return changeCounts(ctx, q, id, func(stored *Counts) { *stored = c })
}
func (s *Service) end(id, state string, problem error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.endLocked(id, state, problem)
}
func (s *Service) complete(ctx context.Context, id, state string) {
	s.mu.Lock()
	if ctx.Err() != nil {
		s.mu.Unlock()
		s.stopWork(ctx, id, ctx.Err())
		return
	}
	defer s.mu.Unlock()
	_ = s.endLocked(id, state, nil)
}
func (s *Service) endLocked(id, state string, problem error) error {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if state == "paused" || state == "cancelled" || state == "interrupted" || state == "failed" {
		if _, err = tx.ExecContext(ctx, "UPDATE local_import_folder SET state='interrupted' WHERE job_id=? AND state='running'", id); err != nil {
			return err
		}
	}
	if err = refreshCounts(ctx, tx, id); err != nil {
		return err
	}
	if state == "completed" {
		var failures int
		if err = tx.QueryRowContext(ctx, "SELECT coalesce(json_extract(counts_json,'$.failed'),0)+coalesce(json_extract(counts_json,'$.scanErrors'),0) FROM local_import_job WHERE id=?", id).Scan(&failures); err != nil {
			return err
		}
		if failures > 0 {
			state = "completed_with_errors"
		}
	}
	var message any
	if problem != nil {
		message = problem.Error()
	}
	now := time.Now().UnixMilli()
	if _, err = tx.ExecContext(ctx, `UPDATE local_import_job SET state=?,scan_complete=CASE WHEN ?='ready' THEN 1 ELSE scan_complete END,error_message=?,current_path='',active_millis=active_millis+CASE WHEN run_started_at IS NULL THEN 0 ELSE max(0,?-run_started_at) END,run_started_at=NULL,revision=revision+1,updated_at=? WHERE id=?`, state, state, message, now, now, id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if s.active == id {
		s.active = ""
		s.cancel = nil
	}
	return nil
}
func (s *Service) stopWork(ctx context.Context, id string, err error) {
	if ctx.Err() == nil {
		s.end(id, "failed", err)
		return
	}
	var cancel, pause *int64
	if e := s.db.QueryRow("SELECT cancel_requested_at,pause_requested_at FROM local_import_job WHERE id=?", id).Scan(&cancel, &pause); e != nil {
		s.end(id, "failed", e)
		return
	}
	state := "interrupted"
	if pause != nil {
		state = "paused"
	}
	if cancel != nil {
		state = "cancelled"
	}
	s.end(id, state, nil)
}
