package mediaedit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

const framePageSize = 48

func (s *Service) Frames(ctx context.Context, req FramesRequest) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.halted || !validID(req.ID) || req.Offset < 0 {
		return invalid("프레임 위치를 확인하세요")
	}
	v, e := s.store.session(ctx, req.ID)
	if e != nil {
		return fail(e)
	}
	if v.State != "ready" || req.Offset >= v.FrameCount {
		return invalid("프레임 위치 또는 자료 상태를 확인하세요")
	}
	offset := req.Offset / framePageSize * framePageSize
	result := FramePage{Offset: offset, Count: min(framePageSize, v.FrameCount-offset), State: "ready"}
	if v.RenderVersion == 1 {
		return api.Success(result)
	}
	var queued int
	if e = s.store.db.QueryRowContext(ctx, "SELECT count(*) FROM media_edit_page WHERE state IN('pending','running')").Scan(&queued); e != nil {
		return fail(e)
	}
	if queued >= 16 {
		return invalid("프레임 준비가 대기 중입니다. 잠시 후 다시 시도하세요")
	}
	if req.Retry {
		_, e = s.store.db.ExecContext(ctx, "UPDATE media_edit_page SET state='pending',error_message='' WHERE session_id=? AND page_offset=? AND state='failed'", v.ID, offset)
		if e != nil {
			return fail(e)
		}
	}
	if _, e = s.store.db.ExecContext(ctx, "INSERT INTO media_edit_page(session_id,page_offset,state,accessed_at)VALUES(?,?,'pending',?) ON CONFLICT(session_id,page_offset) DO UPDATE SET accessed_at=excluded.accessed_at", v.ID, offset, now()); e != nil {
		return fail(e)
	}
	if e = s.store.db.QueryRowContext(ctx, "SELECT state,error_message FROM media_edit_page WHERE session_id=? AND page_offset=?", v.ID, offset).Scan(&result.State, &result.Error); e != nil {
		return fail(e)
	}
	if result.State == "pending" {
		s.signal()
	}
	s.pageTiming(v, &result)
	return api.Success(result)
}

func (s *Service) FrameStatus(ctx context.Context, req FramesRequest) api.Result {
	if !validID(req.ID) || req.Offset < 0 {
		return invalid("프레임 위치를 확인하세요")
	}
	v, e := s.store.session(ctx, req.ID)
	if e != nil {
		return fail(e)
	}
	if v.State != "ready" || req.Offset >= v.FrameCount {
		return invalid("프레임 위치를 확인하세요")
	}
	offset := req.Offset / framePageSize * framePageSize
	p := FramePage{Offset: offset, Count: min(framePageSize, v.FrameCount-offset), State: "ready"}
	if v.RenderVersion == 1 {
		return api.Success(p)
	}
	e = s.store.db.QueryRowContext(ctx, "SELECT state,error_message FROM media_edit_page WHERE session_id=? AND page_offset=?", v.ID, offset).Scan(&p.State, &p.Error)
	if e == sql.ErrNoRows {
		p.State = "missing"
	} else if e != nil {
		return fail(e)
	}
	s.pageTiming(v, &p)
	return api.Success(p)
}

func (s *Service) prepareTimeline(ctx context.Context, id string) error {
	v, e := s.store.session(ctx, id)
	if e != nil {
		return e
	}
	source, path, e := s.source(ctx, id)
	if e != nil {
		return e
	}
	defer source.Close()
	var metadata engine.LocalMetadata
	var timeline *engine.GIFTimeline
	count := 0
	if v.Options.NativeGIF {
		native, err := engine.ReadGIFTimeline(ctx, path)
		if err != nil {
			return err
		}
		timeline = &native
		duration := native.Duration()
		metadata = engine.LocalMetadata{Width: &native.Width, Height: &native.Height, Duration: &duration}
		count = len(native.Delays)
		v.Options.End = duration
	} else {
		metadata, e = s.editor.ProbeVideo(ctx, path)
		if e != nil {
			return e
		}
		if v.Options.FullSource {
			v.Options.Start, v.Options.End = 0, *metadata.Duration
		}
		if (v.Options.FullSource || v.Options.OriginalFrames) && !v.Options.QuickVideo {
			if v.Options.Kind != "trim" {
				var native engine.GIFTimeline
				var err error
				if v.Options.OriginalFrames {
					native, err = s.editor.ReadVideoClipTimeline(ctx, path, metadata, v.Options.Start, v.Options.End)
				} else {
					native, err = s.editor.ReadVideoTimeline(ctx, path, metadata)
				}
				if err != nil {
					return err
				}
				timeline = &native
				v.Options.NativeVideo = true
				if !v.Options.OriginalFrames {
					v.Options.End = native.Duration()
				}
				v.Options.FPS = 1000
				count = len(native.Delays)
			}
		}
		if !v.Options.FullSource && v.Options.End > *metadata.Duration+0.001 {
			return fmt.Errorf("선택 구간이 원본 재생 시간을 초과합니다")
		}
		if v.Options.Kind != "trim" && !v.Options.NativeVideo && !v.Options.QuickVideo {
			count = int(math.Ceil((v.Options.End-v.Options.Start)*float64(v.Options.FPS) - 1e-9))
			if count < 1 {
				return fmt.Errorf("선택 구간에 프레임이 없습니다")
			}
		}
	}
	cache, e := os.OpenRoot(s.cache)
	if e != nil {
		return e
	}
	defer cache.Close()
	if e = cache.Mkdir(id, 0700); e != nil {
		return e
	}
	if timeline != nil {
		file, err := cache.OpenFile(filepath.Join(id, timelineName(v)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		e = json.NewEncoder(file).Encode(timeline)
		if e == nil {
			e = file.Sync()
		}
		ce := file.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			return e
		}
	}
	if _, e = s.store.db.ExecContext(ctx, "UPDATE media_edit_session SET width=?,height=?,frame_count=?,progress=15,request_json=? WHERE id=?", *metadata.Width, *metadata.Height, count, raw(v.Options), id); e != nil {
		return e
	}
	if v.Options.Kind != "trim" && !v.Options.QuickVideo {
		if e = s.buildPage(ctx, id, 0); e != nil {
			return e
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e = ctx.Err(); e != nil {
		return e
	}
	_, e = s.store.db.ExecContext(ctx, "UPDATE media_edit_session SET state='ready',progress=100,updated_at=? WHERE id=?", now(), id)
	return e
}

func pageName(offset int) string { return filepath.Join("pages", fmt.Sprintf("%012d", offset)) }
func (s *Service) buildPage(ctx context.Context, id string, offset int) error {
	v, e := s.store.session(ctx, id)
	if e != nil {
		return e
	}
	source, path, e := s.source(ctx, id)
	if e != nil {
		return e
	}
	defer source.Close()
	cache, e := os.OpenRoot(s.cache)
	if e != nil {
		return e
	}
	defer cache.Close()
	rel := filepath.Join(id, pageName(offset))
	if e = cache.MkdirAll(filepath.Dir(rel), 0700); e != nil {
		return e
	}
	if e = cache.RemoveAll(rel); e != nil {
		return e
	}
	if e = cache.Mkdir(rel, 0700); e != nil {
		return e
	}
	lock, e := media.LockDirectory(filepath.Join(s.cache, rel))
	if e != nil {
		return e
	}
	defer lock.Close()
	_, e = s.store.db.ExecContext(ctx, "INSERT INTO media_edit_page(session_id,page_offset,state,accessed_at)VALUES(?,?,'running',?) ON CONFLICT(session_id,page_offset) DO UPDATE SET state='running',error_message='',accessed_at=excluded.accessed_at", id, offset, now())
	if e != nil {
		return e
	}
	if v.Options.NativeGIF {
		e = s.editor.GIFFramePage(ctx, path, filepath.Join(s.cache, rel), v.FrameCount, offset, framePageSize)
	} else if v.Options.NativeVideo {
		e = s.editor.VideoClipFramePage(ctx, path, filepath.Join(s.cache, rel), v.FrameCount, offset, framePageSize, v.Options.Start, v.Options.End)
	} else {
		e = s.editor.FramePage(ctx, path, filepath.Join(s.cache, rel), v.Options.Start, v.Options.End, v.Options.FPS, v.FrameCount, offset, framePageSize)
	}
	if e != nil {
		_, _ = s.store.db.Exec("UPDATE media_edit_page SET state='failed',error_message='프레임을 읽지 못했습니다. 원본과 PC 연결을 확인하세요.' WHERE session_id=? AND page_offset=?", id, offset)
		return e
	}
	pageRoot, openErr := cache.OpenRoot(rel)
	if openErr != nil {
		return openErr
	}
	for n := offset; n < min(v.FrameCount, offset+framePageSize); n++ {
		f, err := pageRoot.Open(fmt.Sprintf("%012d.jpg", n))
		if err != nil {
			pageRoot.Close()
			return err
		}
		img, err := jpeg.Decode(f)
		f.Close()
		if err != nil {
			pageRoot.Close()
			return err
		}
		if err = frameThumbnail(pageRoot, n, img); err != nil {
			pageRoot.Close()
			return err
		}
		if err = ctx.Err(); err != nil {
			pageRoot.Close()
			return err
		}
	}
	pageRoot.Close()
	if _, e = s.store.db.ExecContext(ctx, "UPDATE media_edit_page SET state='ready',accessed_at=? WHERE session_id=? AND page_offset=?", now(), id, offset); e != nil {
		return e
	}
	// A small LRU bounds preview disk use without bounding media dimensions or length.
	rows, e := s.store.db.QueryContext(ctx, "SELECT session_id,page_offset FROM media_edit_page WHERE state='ready' ORDER BY accessed_at DESC LIMIT -1 OFFSET 8")
	if e != nil {
		return e
	}
	type oldPage struct {
		id     string
		offset int
	}
	old := []oldPage{}
	for rows.Next() {
		var p oldPage
		if e = rows.Scan(&p.id, &p.offset); e != nil {
			break
		}
		old = append(old, p)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return e
	}
	for _, p := range old {
		if p.id == id && p.offset == offset {
			continue
		}
		if e = cache.RemoveAll(filepath.Join(p.id, pageName(p.offset))); e == nil {
			_, _ = s.store.db.ExecContext(ctx, "DELETE FROM media_edit_page WHERE session_id=? AND page_offset=?", p.id, p.offset)
		}
	}
	return nil
}

func (s *Service) runNextPage(ctx context.Context) bool {
	s.mu.Lock()
	if s.halted || s.closed {
		s.mu.Unlock()
		return false
	}
	var id string
	var offset int
	if e := s.store.db.QueryRowContext(ctx, "SELECT session_id,page_offset FROM media_edit_page WHERE state='pending' ORDER BY accessed_at LIMIT 1").Scan(&id, &offset); e != nil {
		s.mu.Unlock()
		return false
	}
	run, cancel := context.WithCancel(ctx)
	run, finish := s.trackUsage(run, id, false)
	s.activeID = id
	s.activeCancel = cancel
	s.activePage = true
	s.mu.Unlock()
	e := s.buildPage(run, id, offset)
	cancel()
	finish()
	s.mu.Lock()
	defer s.mu.Unlock()
	if e != nil {
		_, _ = s.store.db.Exec("UPDATE media_edit_page SET state='failed',error_message='프레임 읽기가 중단됐습니다. 자료를 다시 준비하거나 목록을 갱신하세요.' WHERE session_id=? AND page_offset=?", id, offset)
	}
	if e != nil && isStopUnconfirmed(e) {
		s.halted = true
		_ = s.store.state("media_edit_session", id, "stop_unconfirmed", "엔진 종료를 확인하지 못했습니다. 앱을 다시 시작하세요.")
	}
	s.activeID = ""
	s.activeCancel = nil
	s.activePage = false
	return true
}

func isStopUnconfirmed(err error) bool { return errors.Is(err, engine.ErrStopUnconfirmed) }

func (s *Service) gifTimeline(id string) (engine.GIFTimeline, error) {
	return s.readTimeline(id, "gif-timeline.json")
}
func timelineName(v Session) string {
	if v.Options.NativeVideo {
		return "video-timeline.json"
	}
	return "gif-timeline.json"
}
func (s *Service) timing(v Session) (engine.GIFTimeline, error) {
	return s.readTimeline(v.ID, timelineName(v))
}
func (s *Service) readTimeline(id, name string) (engine.GIFTimeline, error) {
	var value engine.GIFTimeline
	root, err := os.OpenRoot(s.cache)
	if err != nil {
		return value, err
	}
	defer root.Close()
	f, err := root.Open(filepath.Join(id, name))
	if err != nil {
		return value, err
	}
	defer f.Close()
	err = json.NewDecoder(f).Decode(&value)
	return value, err
}
func (s *Service) pageTiming(v Session, p *FramePage) {
	if !v.Options.NativeGIF && !v.Options.NativeVideo {
		return
	}
	g, err := s.timing(v)
	if err != nil {
		return
	}
	ticks := int64(0)
	for n, d := range g.Delays {
		if n >= p.Offset && n < p.Offset+p.Count {
			p.Times = append(p.Times, g.StartTime+float64(ticks)/float64(g.Rate()))
			p.Durations = append(p.Durations, float64(d)/float64(g.Rate()))
		}
		ticks += int64(d)
		if n >= p.Offset+p.Count {
			break
		}
	}
}
