package mediaedit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"github.com/google/uuid"
)

var errGIFFrameTiming = errors.New("GIF로 유지할 수 없는 프레임 간격입니다. WebP로 저장하세요.")

const maxEditSessions = 20

type Service struct {
	UpdateGate   *api.UpdateGate
	store        Store
	cache        string
	output       func() string
	editor       engine.VideoEditor
	changed      func(int64)
	mu           sync.Mutex
	activeID     string
	activeCancel context.CancelFunc
	closed       bool
	halted       bool
	wake         chan struct{}
	done         chan struct{}
	stop         context.CancelFunc
	usageMu      sync.Mutex
	usage        map[string]*usageTracker
	activePage   bool
	timelineMu   sync.Mutex
	timelines    map[string]engine.GIFTimeline
	overviewCtx  context.Context
	overviewWG   sync.WaitGroup
	overviewSlot chan struct{}
	overviews    map[string]*overviewCache
}

func NewService(db *sql.DB, cache string, editor engine.VideoEditor, output func() string, changed func(int64)) *Service {
	return &Service{store: Store{db}, cache: cache, editor: editor, output: output, changed: changed, wake: make(chan struct{}, 1), overviewSlot: make(chan struct{}, 1), overviews: make(map[string]*overviewCache), timelines: make(map[string]engine.GIFTimeline)}
}
func validID(id string) bool {
	parsed, e := uuid.Parse(id)
	return e == nil && parsed.Version() == 4 && parsed.String() == id
}
func fail(err error) api.Result {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, os.ErrNotExist) {
		return api.Failure("NOT_FOUND", "편집 자료를 찾지 못했습니다")
	}
	return api.Failure("EDIT_FAILED", "편집 요청을 처리하지 못했습니다. 작업 상태와 PC의 저장 공간을 확인하세요.")
}
func invalid(message string) api.Result { return api.Failure("INVALID_ARGUMENT", message) }
func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) Prepare(ctx context.Context, req PrepareRequest) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	req.NativeGIF = false
	req.NativeWebP = false
	req.NativeVideo = false
	if req.QuickVideo && (!req.FullSource || req.OriginalFrames) || req.OriginalFrames && req.Kind == "trim" {
		return invalid("영상 편집 방식을 확인하세요")
	}
	if req.FullSource {
		req.Start, req.End, req.FPS = 0, 0, 1000
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.halted || s.done == nil {
		return api.Failure("NOT_READY", "편집 서비스를 사용할 준비가 되지 않았습니다")
	}
	if req.Kind == "" {
		req.Kind = "gif"
	}
	if req.Kind == "webp" || req.Kind == "animation" {
		req.Kind = "gif"
	}
	if !validID(req.RequestID) || req.FileID <= 0 || (req.Kind != "gif" && req.Kind != "trim") || math.IsNaN(req.Start) || math.IsNaN(req.End) || math.IsInf(req.Start, 0) || math.IsInf(req.End, 0) || req.Start < 0 || (!req.FullSource && req.End <= req.Start) || req.FPS < 1 || math.Ceil((req.End-req.Start)*float64(req.FPS)) > 9007199254740991 {
		return invalid("구간과 FPS를 확인하세요. 원본 재생 시간 안에서 선택해야 합니다.")
	}
	if existing, e := s.store.session(ctx, req.RequestID); e == nil {
		compared := req
		if existing.RenderVersion >= 2 {
			compared.Width = 0
			if compared.OriginalFrames {
				compared.FPS, compared.NativeVideo = existing.Options.FPS, existing.Options.NativeVideo
			}
			if compared.FullSource {
				compared.End = existing.Options.End
				compared.FPS = existing.Options.FPS
				compared.NativeVideo = existing.Options.NativeVideo
			}
			if existing.Options.NativeGIF {
				compared.NativeGIF = true
				compared.FPS = 100
				compared.Start = 0
				compared.End = existing.Options.End
			}
			if existing.Options.NativeWebP {
				compared.NativeWebP = true
				compared.FPS, compared.Start, compared.End = 1000, 0, existing.Options.End
			}
		}
		if raw(existing.Options) != raw(compared) {
			return invalid("같은 요청 식별자로 다른 편집을 만들 수 없습니다")
		}
		return api.Success(existing)
	} else if !errors.Is(e, sql.ErrNoRows) {
		return fail(e)
	}
	// Preview display size never controls final output resolution.
	req.Width = 0
	var reused bool
	if e := s.store.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM media_edit_job WHERE id=?)", req.RequestID).Scan(&reused); e != nil {
		return fail(e)
	}
	if reused {
		return invalid("이미 출력 작업에 사용한 요청 ID입니다")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	var path, title, sourceUUID, kind string
	var mediaID, revision int64
	err = tx.QueryRowContext(ctx, `SELECT f.filepath,f.file_uuid,f.kind,m.id,m.revision,coalesce(nullif(f.title,''),nullif(m.custom_title,''),nullif(m.source_title,''),'영상') FROM media_file f JOIN media m ON m.id=f.media_id WHERE f.id=?`, req.FileID).Scan(&path, &sourceUUID, &kind, &mediaID, &revision, &title)
	if err != nil {
		return fail(err)
	}
	if kind != "video" && (kind != "image" || req.Kind == "trim") {
		return invalid("등록된 영상 또는 GIF·WebP 파일을 선택하세요")
	}
	if err = media.CheckWrite(ctx, tx, mediaID, revision); err != nil {
		return api.Failure("JOB_ACTIVE", "원본을 사용하는 다른 작업이 있습니다")
	}
	if !filepath.IsAbs(path) {
		return invalid("원본 파일 경로를 확인하세요")
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return fail(err)
	}
	defer root.Close()
	f, err := media.OpenReadFile(root, filepath.Base(path))
	if err != nil {
		return fail(err)
	}
	defer f.Close()
	if kind == "image" {
		if req.QuickVideo || req.OriginalFrames {
			return invalid("영상 파일에서 사용할 수 있는 편집 방식입니다")
		}
		var signature [12]byte
		if _, err = f.ReadAt(signature[:], 0); err != nil {
			return fail(err)
		}
		switch {
		case string(signature[:6]) == "GIF87a" || string(signature[:6]) == "GIF89a":
			req.NativeGIF, req.FPS = true, 100
		case string(signature[:4]) == "RIFF" && string(signature[8:]) == "WEBP":
			req.NativeWebP, req.FPS = true, 1000
		default:
			return invalid("GIF 또는 WebP 파일을 선택하세요")
		}
		req.Start = 0
	}
	identity, err := media.FileIdentity(f)
	if err != nil {
		return fail(err)
	}
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO media_edit_session(id,source_file_id,source_uuid,source_path,source_title,source_identity,source_size,source_mtime,request_json,state,created_at,updated_at,render_version) VALUES(?,?,?,?,?,?,?,?,?,'pending',?,?,2)`, req.RequestID, req.FileID, sourceUUID, path, title, identity, info.Size(), fmt.Sprint(info.ModTime().UnixNano()), raw(req), now(), now())
	if err != nil {
		return fail(err)
	}
	if req.QuickVideo {
		// Basic metadata does not wait behind queued encodes or index frames.
		metadata, probeErr := s.editor.ProbeVideo(ctx, path)
		if probeErr != nil {
			if errors.Is(probeErr, engine.ErrStopUnconfirmed) {
				s.halted = true
			}
			return fail(probeErr)
		}
		req.Start, req.End = 0, *metadata.Duration
		if _, err = tx.ExecContext(ctx, "UPDATE media_edit_session SET state='ready',width=?,height=?,progress=100,request_json=? WHERE id=?", *metadata.Width, *metadata.Height, raw(req), req.RequestID); err != nil {
			return fail(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	_ = s.pruneHistory(ctx, req.RequestID)
	s.signal()
	v, err := s.store.session(ctx, req.RequestID)
	if err != nil {
		return fail(err)
	}
	return api.Success(v)
}

func (s *Service) GetSession(ctx context.Context, req Request) api.Result {
	if !validID(req.ID) {
		return invalid("편집 자료 ID를 확인하세요")
	}
	v, e := s.store.session(ctx, req.ID)
	if e != nil {
		return fail(e)
	}
	return api.Success(v)
}
func (s *Service) GetJob(ctx context.Context, req Request) api.Result {
	if !validID(req.ID) {
		return invalid("작업 ID를 확인하세요")
	}
	v, e := s.store.job(ctx, req.ID)
	if e != nil {
		return fail(e)
	}
	v.Resources = s.liveUsage(v.ID, v.Resources)
	return api.Success(v)
}

// ResultPath is available through Wails only, never through the mobile routes.
func (s *Service) ResultPath(ctx context.Context, req Request) api.Result {
	if !validID(req.ID) {
		return invalid("작업 ID를 확인하세요")
	}
	var path string
	err := s.store.db.QueryRowContext(ctx, `SELECT f.filepath FROM media_edit_job j JOIN media_file f ON f.id=j.result_file_id AND f.file_uuid=j.result_uuid WHERE j.id=? AND j.state='completed'`, req.ID).Scan(&path)
	if err != nil {
		return fail(err)
	}
	return api.Success(path)
}
func (s *Service) List(ctx context.Context) api.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessions := []Session{}
	jobs := []Job{}
	ids, e := s.store.ids(ctx, "media_edit_session", "WHERE state<>'released' ORDER BY created_at DESC,rowid DESC")
	if e != nil {
		return fail(e)
	}
	for _, id := range ids {
		v, e := s.store.session(ctx, id)
		if e != nil {
			return fail(e)
		}
		sessions = append(sessions, v)
	}
	ids, e = s.store.ids(ctx, "media_edit_job", "ORDER BY created_at DESC,rowid DESC")
	if e != nil {
		return fail(e)
	}
	for _, id := range ids {
		v, e := s.store.job(ctx, id)
		if e != nil {
			return fail(e)
		}
		v.Resources = s.liveUsage(v.ID, v.Resources)
		jobs = append(jobs, v)
	}
	return api.Success(map[string]any{"sessions": sessions, "jobs": jobs})
}
func (s *Service) Create(ctx context.Context, req CreateRequest) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.halted || s.done == nil {
		return api.Failure("NOT_READY", "편집 서비스를 사용할 준비가 되지 않았습니다")
	}
	if !validID(req.RequestID) || !validID(req.SessionID) || (req.Kind != "gif" && req.Kind != "webp" && req.Kind != "trim") {
		return invalid("편집 요청을 확인하세요")
	}
	if err := normalizeOutput(&req); err != nil {
		return invalid(err.Error())
	}
	// Canonical ordering makes retries independent of selection order.
	req.Deleted = append([]int{}, req.Deleted...)
	sort.Ints(req.Deleted)
	if req.Kind != "trim" {
		var err error
		req.Quality, err = engine.NormalizeAnimationQuality(req.Kind, req.Quality)
		if err != nil {
			return invalid(err.Error())
		}
	}
	if old, e := s.store.job(ctx, req.RequestID); e == nil {
		compared := old.Options
		// An unspecified destination on retry keeps the original frozen folder.
		if req.OutputDirectory == "" {
			compared.OutputDirectory = ""
		}
		if compared.Kind != "trim" {
			compared.Quality, e = engine.NormalizeAnimationQuality(compared.Kind, compared.Quality)
			if e != nil {
				return fail(e)
			}
		}
		if raw(compared) != raw(req) {
			return invalid("같은 요청 식별자로 다른 작업을 만들 수 없습니다")
		}
		return api.Success(old)
	} else if !errors.Is(e, sql.ErrNoRows) {
		return fail(e)
	}
	var reused bool
	if e := s.store.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM media_edit_session WHERE id=?)", req.RequestID).Scan(&reused); e != nil {
		return fail(e)
	}
	if reused {
		return invalid("이미 편집 자료에 사용한 요청 ID입니다")
	}
	v, e := s.store.session(ctx, req.SessionID)
	if e != nil {
		return fail(e)
	}
	if v.State != "ready" {
		return invalid("프레임 또는 영상 준비가 완료된 뒤 저장하세요")
	}
	if !v.Options.QuickVideo && (req.Kind == "trim") != (v.Options.Kind == "trim") {
		return invalid("준비한 편집 종류와 출력 종류가 다릅니다")
	}
	if req.Kind == "trim" || v.Options.QuickVideo {
		if _, _, e = trimBounds(v, req); e != nil {
			return invalid(e.Error())
		}
		if v.Options.QuickVideo && (len(req.Deleted) > 0 || req.SelectionID != "") {
			return invalid("프레임 삭제는 프레임 정밀 편집에서 지정하세요")
		}
	} else if req.TrimStart != nil || req.TrimEnd != nil {
		return invalid("영상 자르기 시간은 영상 출력에서만 지정하세요")
	}
	if req.Kind == "gif" && !v.Options.NativeVideo && !v.Options.NativeWebP && !v.Options.QuickVideo && v.Options.FPS > 100 {
		return invalid("GIF의 시간 단위로는 100FPS 초과를 표현할 수 없습니다. WebP를 선택하거나 새 자료의 FPS를 조정하세요.")
	}
	if req.Kind == "webp" && v.Options.FPS > 1000 {
		return invalid("WebP의 시간 단위로는 1000FPS 초과를 표현할 수 없습니다. 새 자료의 FPS를 조정하세요.")
	}
	if req.Kind != "trim" {
		rangeEnd := req.RangeEnd
		if rangeEnd == 0 {
			rangeEnd = v.FrameCount
		}
		if !v.Options.QuickVideo && (req.RangeStart < 0 || rangeEnd > v.FrameCount || rangeEnd <= req.RangeStart) {
			return invalid("남길 프레임 구간을 확인하세요")
		}
		c := req.Crop
		if c.X < 0 || c.Y < 0 || c.Width < 1 || c.Height < 1 || c.Width > v.Width || c.Height > v.Height || c.X > v.Width-c.Width || c.Y > v.Height-c.Height || !v.Options.QuickVideo && len(req.Deleted) >= v.FrameCount {
			return invalid("크롭 영역과 남길 프레임을 확인하세요")
		}
		for i, n := range req.Deleted {
			if n < 0 || n >= v.FrameCount || (i > 0 && n == req.Deleted[i-1]) {
				return invalid("삭제 프레임 목록을 확인하세요")
			}
		}
		if req.SelectionID != "" {
			if !validID(req.SelectionID) || len(req.Deleted) > 0 {
				return invalid("삭제 선택을 확인하세요")
			}
			var ready bool
			var removed int
			if e = s.store.db.QueryRowContext(ctx, `SELECT finished,(SELECT coalesce(sum(last_frame-first_frame+1),0) FROM media_edit_range WHERE selection_id=s.id) FROM media_edit_selection s WHERE id=? AND session_id=?`, req.SelectionID, v.ID).Scan(&ready, &removed); e != nil {
				return fail(e)
			}
			if !ready || removed >= v.FrameCount {
				return invalid("최소 한 프레임을 남겨야 합니다")
			}
		}
	}
	if req.Kind != "trim" && !v.Options.QuickVideo {
		first, end, _, factory, planErr := s.framePlan(v, req)
		if planErr != nil {
			return invalid(planErr.Error())
		}
		if req.Kind == "gif" && (v.Options.NativeVideo || v.Options.NativeWebP) {
			timeline, err := s.timing(v)
			if err != nil {
				return fail(err)
			}
			skip := factory()
			for n := first; n < end; n++ {
				drop, err := skip(n)
				if err != nil {
					return fail(err)
				}
				if !drop && (n >= len(timeline.Delays) || timeline.Delays[n] < 10 || timeline.Delays[n] > 655350) {
					return invalid("GIF로 유지할 수 없는 프레임 간격입니다. WebP로 저장하세요.")
				}
			}
		}
		if e = ctx.Err(); e != nil {
			return fail(e)
		}
	}
	if req.Preview && (req.Kind == "trim" || req.PreviewStart < 0 || !v.Options.QuickVideo && req.PreviewStart >= v.FrameCount) {
		return invalid("화질 미리보기 위치를 확인하세요")
	}
	var count int
	if e = s.store.db.QueryRowContext(ctx, "SELECT count(*) FROM media_edit_job WHERE state IN ('pending','running','verifying','publishing','stop_unconfirmed')").Scan(&count); e != nil {
		return fail(e)
	}
	if count >= 10 {
		return api.Failure("LIMIT_EXCEEDED", "동시에 대기할 수 있는 작업은 최대 10개입니다")
	}
	if e = s.validateOutputAlbums(ctx, req.AlbumIDs); e != nil {
		return invalid(e.Error())
	}
	root := req.OutputDirectory
	if root == "" {
		root = s.output()
	}
	if !filepath.IsAbs(root) {
		return invalid("PC의 저장 폴더를 확인하세요")
	}
	if req.Preview {
		root = s.cache
	} else {
		req.OutputDirectory = filepath.Clean(root)
	}
	_, e = s.store.db.ExecContext(ctx, `INSERT INTO media_edit_job(id,session_id,request_json,state,output_root,result_uuid,created_at,updated_at,preview) VALUES(?,?,?,'pending',?,?,?,?,?)`, req.RequestID, req.SessionID, raw(req), root, uuid.NewString(), now(), now(), req.Preview)
	if e != nil {
		return fail(e)
	}
	_ = s.pruneHistory(ctx, req.SessionID)
	s.signal()
	job, e := s.store.job(ctx, req.RequestID)
	if e != nil {
		return fail(e)
	}
	return api.Success(job)
}

func (s *Service) Cancel(ctx context.Context, req Request) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validID(req.ID) {
		return invalid("작업 ID를 확인하세요")
	}
	table := "media_edit_job"
	job, e := s.store.job(ctx, req.ID)
	state := job.State
	if errors.Is(e, sql.ErrNoRows) {
		table = "media_edit_session"
		v, err := s.store.session(ctx, req.ID)
		e = err
		state = v.State
	}
	if e != nil {
		return fail(e)
	}
	if table == "media_edit_session" && state == "ready" {
		if s.activePage && s.activeID == req.ID {
			s.activeCancel()
		}
		_, e = s.store.db.ExecContext(ctx, "UPDATE media_edit_page SET state='failed',error_message='프레임 읽기를 취소했습니다' WHERE session_id=? AND state='pending'", req.ID)
		if e != nil {
			return fail(e)
		}
		return api.Success(nil)
	}
	if state == "publishing" {
		return api.Failure("FINALIZING", "결과를 저장하고 있습니다. 완료 후 결과를 확인하세요.")
	}
	if state != "pending" && state != "preparing" && state != "running" && state != "verifying" {
		return api.Success(nil)
	}
	if s.activeID == req.ID {
		if _, e = s.store.db.ExecContext(ctx, "UPDATE "+table+" SET cancel_requested=1,updated_at=? WHERE id=?", now(), req.ID); e != nil {
			return fail(e)
		}
		s.activeCancel()
	} else {
		if e = s.store.state(table, req.ID, "cancelled", ""); e != nil {
			return fail(e)
		}
	}
	_ = s.pruneHistory(ctx, "")
	return api.Success(nil)
}
func (s *Service) Release(ctx context.Context, req Request) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validID(req.ID) {
		return invalid("편집 자료 ID를 확인하세요")
	}
	v, e := s.store.session(ctx, req.ID)
	if e != nil {
		return fail(e)
	}
	var active int
	if e = s.store.db.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM media_edit_job WHERE session_id=? AND state IN ('pending','running','verifying','publishing','stop_unconfirmed'))+(SELECT count(*) FROM media_edit_page WHERE session_id=? AND state IN ('pending','running'))", req.ID, req.ID).Scan(&active); e != nil {
		return fail(e)
	}
	if active > 0 || s.activeID == req.ID || v.State == "pending" || v.State == "preparing" || v.State == "stop_unconfirmed" {
		return api.Failure("JOB_ACTIVE", "작업을 마치거나 취소한 뒤 편집 자료를 정리하세요")
	}
	if e = s.store.state("media_edit_session", req.ID, "released", ""); e != nil {
		return fail(e)
	}
	root, e := os.OpenRoot(s.cache)
	if e != nil {
		return fail(e)
	}
	defer root.Close()
	if e = root.RemoveAll(req.ID); e != nil {
		return fail(e)
	}
	return api.Success(nil)
}

//wails:ignore
func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("편집 서비스가 종료됐습니다")
	}
	if s.done != nil {
		return nil
	}
	if !filepath.IsAbs(s.cache) {
		return fmt.Errorf("편집 캐시 절대 경로가 필요합니다")
	}
	if e := os.MkdirAll(s.cache, 0700); e != nil {
		return e
	}
	if e := s.recover(); e != nil {
		return e
	}
	_ = s.pruneHistory(context.Background(), "")
	ctx, cancel := context.WithCancel(context.Background())
	s.overviewCtx = ctx
	s.stop = cancel
	s.done = make(chan struct{})
	go s.work(ctx)
	s.signal()
	return nil
}

//wails:ignore
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	if s.stop != nil {
		s.stop()
	}
	done := s.done
	s.mu.Unlock()
	if done != nil {
		<-done
	}
	s.overviewWG.Wait()
}

func trimBounds(v Session, req CreateRequest) (float64, float64, error) {
	start, end := v.Options.Start, v.Options.End
	if req.RangeStart != 0 || req.RangeEnd != 0 {
		return 0, 0, fmt.Errorf("영상 구간을 시간으로 지정하세요")
	}
	if (req.TrimStart == nil) != (req.TrimEnd == nil) {
		return 0, 0, fmt.Errorf("영상 시작·종료 시간을 함께 지정하세요")
	}
	if req.TrimStart != nil {
		start, end = *req.TrimStart, *req.TrimEnd
	}
	if math.IsNaN(start) || math.IsNaN(end) || math.IsInf(start, 0) || math.IsInf(end, 0) || start < v.Options.Start || end > v.Options.End+1e-6 || end <= start {
		return 0, 0, fmt.Errorf("준비한 영상 구간 안에서 시작·종료 시간을 지정하세요")
	}
	return start, min(end, v.Options.End), nil
}
