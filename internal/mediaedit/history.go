package mediaedit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const maxEditJobs = 20
const historyCleanupPending = "history_cleanup_pending"

// The service mutex must be held. Active/recoverable records count towards the
// limit, but are never evicted; an all-protected history may temporarily exceed it.
func (s *Service) pruneHistory(ctx context.Context, keepDraft string) error {
	// Retain the five newest batch groups as complete units; never evict active
	// jobs or unresolved publication proofs from older groups.
	if _, err := s.store.db.ExecContext(ctx, `DELETE FROM media_edit_job AS old WHERE json_extract(request_json,'$.batch.id') IN (
	 SELECT json_extract(request_json,'$.batch.id') FROM media_edit_job WHERE json_type(request_json,'$.batch')='object'
	 GROUP BY json_extract(request_json,'$.batch.id') ORDER BY max(created_at) DESC LIMIT -1 OFFSET 5
	 ) AND state IN ('completed','failed','cancelled','interrupted') AND (state='completed' OR proof_json IS NULL) AND id<>?
 AND NOT EXISTS(SELECT 1 FROM media_edit_job sibling WHERE json_extract(sibling.request_json,'$.batch.id')=json_extract(old.request_json,'$.batch.id') AND (sibling.state IN ('pending','running','verifying','publishing','stop_unconfirmed') OR sibling.state<>'completed' AND sibling.proof_json IS NOT NULL))`, s.activeID); err != nil {
		return err
	}
	var count int
	if err := s.store.db.QueryRowContext(ctx, "SELECT count(*) FROM media_edit_job WHERE json_type(request_json,'$.batch') IS NULL").Scan(&count); err != nil {
		return err
	}
	ids, err := s.store.ids(ctx, "media_edit_job", `WHERE state IN ('completed','failed','cancelled','interrupted')
        AND json_type(request_json,'$.batch') IS NULL AND id<>? AND (state='completed' OR preview=1 OR proof_json IS NULL)
        ORDER BY created_at ASC,rowid ASC LIMIT ?`, s.activeID, max(0, count-maxEditJobs))
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !validID(id) {
			return fmt.Errorf("작업 내역 ID가 잘못되었습니다")
		}
		job, err := s.store.job(ctx, id)
		if err != nil {
			return err
		}
		if job.Preview {
			ext := ".gif"
			if job.Options.Kind == "webp" {
				ext = ".webp"
			}
			root, err := os.OpenRoot(s.cache)
			if err != nil {
				return err
			}
			// Only disposable previews under the cache root are removed. Published
			// media and result_path/output_root are never filesystem deletion targets.
			err = root.Remove(filepath.Join("previews", id+ext))
			root.Close()
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if _, err := s.store.db.ExecContext(ctx, "DELETE FROM media_edit_job WHERE id=?", id); err != nil {
			return err
		}
	}
	if err := s.store.db.QueryRowContext(ctx, "SELECT count(*) FROM media_edit_session WHERE state<>'released'").Scan(&count); err != nil {
		return err
	}
	ids, err = s.store.ids(ctx, "media_edit_session", `s WHERE s.state IN ('ready','failed','cancelled','interrupted')
        AND s.id<>? AND s.id<>?
        AND NOT EXISTS(SELECT 1 FROM media_edit_job j WHERE j.session_id=s.id AND
            (j.state IN ('pending','running','verifying','publishing','stop_unconfirmed') OR j.id=? OR
             (j.preview=0 AND j.state<>'completed' AND j.proof_json IS NOT NULL)))
        AND NOT EXISTS(SELECT 1 FROM media_edit_page p WHERE p.session_id=s.id AND p.state IN ('pending','running'))
        ORDER BY s.created_at ASC,s.rowid ASC LIMIT ?`, s.activeID, keepDraft, s.activeID, max(0, count-maxEditSessions))
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !validID(id) {
			return fmt.Errorf("편집 초안 ID가 잘못되었습니다")
		}
		// Retain a lightweight released session for request IDs and recent jobs'
		// foreign keys. The marker retries cache cleanup after interruption/failure.
		if _, err := s.store.db.ExecContext(ctx, "UPDATE media_edit_session SET state='released',cancel_requested=0,error_message='',diagnostic=?,updated_at=? WHERE id=?", historyCleanupPending, now(), id); err != nil {
			return err
		}
	}
	ids, err = s.store.ids(ctx, "media_edit_session", "WHERE state='released' AND diagnostic=?", historyCleanupPending)
	if err != nil || len(ids) == 0 {
		return err
	}
	root, err := os.OpenRoot(s.cache)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, id := range ids {
		// A canonical UUID is a single relative component; os.Root confines the
		// recursive deletion to this service's cache, including symlink handling.
		if !validID(id) {
			return fmt.Errorf("편집 캐시 ID가 잘못되었습니다")
		}
		if err := root.RemoveAll(id); err != nil {
			return err
		}
		tx, err := s.store.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, statement := range []string{"DELETE FROM media_edit_page WHERE session_id=?", "DELETE FROM media_edit_selection WHERE session_id=?", "UPDATE media_edit_session SET diagnostic='' WHERE id=?"} {
			if _, err = tx.ExecContext(ctx, statement, id); err != nil {
				break
			}
		}
		if err == nil {
			err = tx.Commit()
		}
		tx.Rollback()
		if err != nil {
			return err
		}
	}
	return nil
}
