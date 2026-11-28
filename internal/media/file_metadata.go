package media

import (
	"context"
	"time"
)

// StartFileMetadata reads small batches off the UI path. Persistent pending rows
// cover existing libraries, new downloads/imports and paths changed by a move.
//
//wails:ignore
func (s *Service) StartFileMetadata(notify func()) {
	s.searchMu.Lock()
	defer s.searchMu.Unlock()
	if s.metadataCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.metadataCancel = cancel
	s.searchWG.Add(1)
	go func() {
		defer s.searchWG.Done()
		idle := true
		for {
			count, err := s.RefreshFileMetadata(ctx, 32)
			if (count > 0 && idle || count == 0 && !idle) && notify != nil {
				notify()
			}
			idle = count == 0
			delay := 100 * time.Millisecond
			if count == 0 || err != nil {
				delay = 2 * time.Second
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

//wails:ignore
func (s *Service) RefreshFileMetadata(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 128 {
		return 0, domainError("INVALID_ARGUMENT")
	}
	leave, allowed := s.UpdateGate.Enter()
	if !allowed {
		return 0, nil
	}
	defer leave()
	rows, err := objects(ctx, s.db, "SELECT file_id,filepath,generation FROM file_metadata WHERE checked_at=0 ORDER BY file_id DESC LIMIT ?", limit)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		path := row["filepath"].(string)
		birth := fileBirthTime(path)
		result, err := s.db.ExecContext(ctx, "UPDATE file_metadata SET birth_ms=?,checked_at=? WHERE file_id=? AND filepath=? AND generation=? AND checked_at=0", birth, time.Now().UnixMilli(), row["file_id"], path, row["generation"])
		if err != nil {
			return count, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return count, err
		}
		count += int(affected)
	}
	return count, nil
}
