package download

import (
	"context"
	"errors"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
)

var errDeleteState = errors.New("정리가 끝난 실패·취소 요청 기록만 삭제할 수 있습니다")

func (s *Service) Delete(ctx context.Context, req JobRequest) api.Result {
	if req.JobID == "" {
		return api.Failure("INVALID_ARGUMENT", "작업 ID가 필요합니다")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return api.Failure("NOT_READY", "앱을 종료하고 있습니다")
	}
	if _, active := s.active[req.JobID]; active {
		s.mu.Unlock()
		return fail(ErrAlreadyProcessing)
	}
	var rawURL string
	if err := s.db.QueryRowContext(ctx, "SELECT original_url FROM download_queue WHERE id=?", req.JobID).Scan(&rawURL); err != nil {
		s.mu.Unlock()
		return fail(err)
	}
	if err := NewStore(s.db).deleteHistory(ctx, req.JobID); err != nil {
		s.mu.Unlock()
		if errors.Is(err, errDeleteState) {
			return api.Failure("CANNOT_DELETE_REQUEST", err.Error())
		}
		return fail(err)
	}
	s.logWarnings.Delete(req.JobID)
	cleanup := s.previewCleanup
	s.mu.Unlock()
	if cleanup != nil {
		cleanup(ctx, rawURL)
	}
	return api.Success(map[string]bool{"accepted": true})
}

func (s *Store) deleteHistory(ctx context.Context, job string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	var cleanup bool
	if err = tx.QueryRowContext(ctx, "SELECT state,cleanup_required FROM download_queue WHERE id=?", job).Scan(&state, &cleanup); err != nil {
		return err
	}
	if state != "failed" && state != "cancelled" {
		return errDeleteState
	}
	if cleanup {
		return ErrCleanupRequired
	}
	var protected bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM download_attempt WHERE job_id=? AND (outcome IS NULL OR committed_at IS NOT NULL)) OR EXISTS(SELECT 1 FROM maintenance_task m JOIN download_attempt a ON a.id=m.download_attempt_id WHERE a.job_id=? AND m.state<>'completed')`, job, job).Scan(&protected); err != nil {
		return err
	}
	if protected {
		return errDeleteState
	}
	// Break the queue/attempt reference cycle before deleting only owned history.
	for _, statement := range []string{
		"UPDATE download_queue SET current_attempt_id=NULL WHERE id=?",
		"DELETE FROM maintenance_task WHERE download_attempt_id IN (SELECT id FROM download_attempt WHERE job_id=?)",
		"DELETE FROM download_target WHERE attempt_id IN (SELECT id FROM download_attempt WHERE job_id=?)",
		"DELETE FROM download_attempt WHERE job_id=?",
		"DELETE FROM download_queue WHERE id=?",
	} {
		if _, err = tx.ExecContext(ctx, statement, job); err != nil {
			return err
		}
	}
	return tx.Commit()
}
