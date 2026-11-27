package mediaedit

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

// Selection accepts bounded, idempotent chunks, so the HTTP body cap never
// limits the number of frames or deletion ranges in an entire edit.
func (s *Service) Selection(ctx context.Context, req SelectionRequest) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !validID(req.ID) || !validID(req.SessionID) || req.Offset < 0 || len(req.Ranges) > 256 {
		return invalid("삭제 선택 요청을 확인하세요")
	}
	v, e := s.store.session(ctx, req.SessionID)
	if e != nil {
		return fail(e)
	}
	if v.State != "ready" {
		return invalid("편집 자료가 준비되지 않았습니다")
	}
	tx, e := s.store.db.BeginTx(ctx, nil)
	if e != nil {
		return fail(e)
	}
	defer tx.Rollback()
	var session string
	var count int
	var finished bool
	e = tx.QueryRowContext(ctx, "SELECT session_id,item_count,finished FROM media_edit_selection WHERE id=?", req.ID).Scan(&session, &count, &finished)
	if e == sql.ErrNoRows {
		session = req.SessionID
		if req.Offset != 0 {
			return invalid("삭제 선택 순서를 확인하세요")
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO media_edit_selection(id,session_id,created_at)VALUES(?,?,?)", req.ID, req.SessionID, now())
	}
	if e != nil {
		return fail(e)
	}
	if session != req.SessionID || req.Offset > count {
		return invalid("삭제 선택 순서가 다릅니다")
	}
	if req.Offset < count || finished {
		if req.Offset+len(req.Ranges) > count {
			return invalid("완료된 삭제 선택은 변경할 수 없습니다")
		}
		for n, r := range req.Ranges {
			var first, last int
			if e = tx.QueryRowContext(ctx, "SELECT first_frame,last_frame FROM media_edit_range WHERE selection_id=? AND position=?", req.ID, req.Offset+n).Scan(&first, &last); e != nil {
				return fail(e)
			}
			if first != r.First || last != r.Last {
				return invalid("같은 요청의 삭제 내용이 다릅니다")
			}
		}
		if req.Final && req.Offset+len(req.Ranges) != count {
			return invalid("삭제 선택 완료 위치를 확인하세요")
		}
		if req.Final && !finished {
			if _, e = tx.ExecContext(ctx, "UPDATE media_edit_selection SET finished=1 WHERE id=?", req.ID); e != nil {
				return fail(e)
			}
		}
		if e = tx.Commit(); e != nil {
			return fail(e)
		}
		return api.Success(req.ID)
	}
	last := -1
	if count > 0 {
		if e = tx.QueryRowContext(ctx, "SELECT last_frame FROM media_edit_range WHERE selection_id=? AND position=?", req.ID, count-1).Scan(&last); e != nil {
			return fail(e)
		}
	}
	for n, r := range req.Ranges {
		if r.First < 0 || r.Last < r.First || r.Last >= v.FrameCount || r.First <= last {
			return invalid("삭제 프레임 범위를 확인하세요")
		}
		last = r.Last
		if _, e = tx.ExecContext(ctx, "INSERT INTO media_edit_range(selection_id,position,first_frame,last_frame)VALUES(?,?,?,?)", req.ID, count+n, r.First, r.Last); e != nil {
			return fail(e)
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE media_edit_selection SET item_count=?,finished=? WHERE id=?", count+len(req.Ranges), req.Final, req.ID); e != nil {
		return fail(e)
	}
	if e = tx.Commit(); e != nil {
		return fail(e)
	}
	return api.Success(req.ID)
}

// rangeReader closes every result set before returning; long encodes never
// monopolize SQLite's single connection or load the entire edit into memory.
type rangeReader struct {
	store     Store
	id        string
	position  int
	batch     []engine.FrameRange
	current   *engine.FrameRange
	ended     bool
	legacy    []int
	legacyPos int
}

func (r *rangeReader) next() (*engine.FrameRange, error) {
	if r.id == "" {
		if r.legacyPos >= len(r.legacy) {
			return nil, nil
		}
		n := r.legacy[r.legacyPos]
		r.legacyPos++
		return &engine.FrameRange{First: n, Last: n}, nil
	}
	if len(r.batch) == 0 && !r.ended {
		rows, e := r.store.db.QueryContext(context.Background(), "SELECT first_frame,last_frame FROM media_edit_range WHERE selection_id=? AND position>=? ORDER BY position LIMIT 256", r.id, r.position)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var v engine.FrameRange
			if e = rows.Scan(&v.First, &v.Last); e != nil {
				rows.Close()
				return nil, e
			}
			r.batch = append(r.batch, v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		if len(r.batch) < 256 {
			r.ended = true
		}
	}
	if len(r.batch) == 0 {
		return nil, nil
	}
	v := r.batch[0]
	r.batch = r.batch[1:]
	r.position++
	return &v, nil
}
func (r *rangeReader) skip(index int) (bool, error) {
	for r.current == nil || index > r.current.Last {
		v, e := r.next()
		if e != nil {
			return false, e
		}
		if v == nil {
			r.current = nil
			return false, nil
		}
		r.current = v
	}
	return index >= r.current.First, nil
}
func (s *Service) framePlan(v Session, req CreateRequest) (int, int, int, func() func(int) (bool, error), error) {
	start, end := req.RangeStart, req.RangeEnd
	if end == 0 {
		end = v.FrameCount
	}
	if start < 0 || end > v.FrameCount || end <= start {
		return 0, 0, 0, nil, fmt.Errorf("남길 구간을 확인하세요")
	}
	if req.Preview {
		start = max(start, req.PreviewStart)
	}
	factory := func() func(int) (bool, error) {
		reader := &rangeReader{store: s.store, id: req.SelectionID, legacy: req.Deleted}
		return reader.skip
	}
	removed := 0
	if req.SelectionID != "" {
		if err := s.store.db.QueryRow("SELECT coalesce(sum(max(0,min(last_frame,?)-max(first_frame,?)+1)),0) FROM media_edit_range WHERE selection_id=?", end-1, start, req.SelectionID).Scan(&removed); err != nil {
			return 0, 0, 0, nil, err
		}
	} else {
		for _, n := range req.Deleted {
			if n >= start && n < end {
				removed++
			}
		}
	}
	count := end - start - removed
	if req.Preview {
		reader := &rangeReader{store: s.store, id: req.SelectionID, legacy: req.Deleted}
		n, taken := start, 0
		if v.Options.NativeGIF || v.Options.NativeVideo || v.Options.NativeWebP {
			g, err := s.timing(v)
			if err != nil {
				return 0, 0, 0, nil, err
			}
			ticks := 0
			for n < end && ticks < 2*g.Rate() {
				drop, err := reader.skip(n)
				if err != nil {
					return 0, 0, 0, nil, err
				}
				if drop {
					n = reader.current.Last + 1
					continue
				}
				if n >= len(g.Delays) {
					return 0, 0, 0, nil, fmt.Errorf("GIF 재생 시간 정보 오류")
				}
				if v.Options.NativeVideo && req.Kind == "gif" && taken > 0 && ticks+g.Delays[n] > 2*g.Rate() {
					break // Keep complete source frames within the two-second GIF preview.
				}
				taken++
				ticks += g.Delays[n]
				n++
			}
		} else {
			wanted := min(v.Options.FPS, end/2) * 2
			if v.Options.FPS > end/2 {
				wanted = end
			}
			for n < end && taken < wanted {
				drop, err := reader.skip(n)
				if err != nil {
					return 0, 0, 0, nil, err
				}
				if drop {
					n = reader.current.Last + 1
					continue
				}
				next := end
				if reader.current != nil {
					next = min(end, reader.current.First)
				}
				step := min(next-n, wanted-taken)
				if step <= 0 {
					return 0, 0, 0, nil, fmt.Errorf("삭제 범위 오류")
				}
				taken += step
				n += step
			}
		}
		end, count = min(n, end), taken
	}
	if count < 1 {
		return 0, 0, 0, nil, fmt.Errorf("선택 구간에 최소 한 프레임을 남겨야 합니다")
	}
	return start, end, count, factory, nil
}
