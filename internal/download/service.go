package download

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrRevisionConflict = errors.New("묶음 정보가 변경되었습니다. 새로고침 후 다시 확인하세요")

const maxConcurrentDownloads = 3

type ImageInspector interface {
	Inspect(context.Context, string, string) (engine.LocalMetadata, error)
}

type Service struct {
	UpdateGate     *api.UpdateGate
	runSlot        chan struct{}
	verifyMedia    func(context.Context, string, string) error
	imageInspector ImageInspector
	db             *sql.DB
	factory        func(string, engine.Options) Runner
	downloadPath   func() string
	afterCommit    func(int64)
	previewCleanup func(context.Context, string)
	previewLookup  func(context.Context, string) (*string, error)
	mu             sync.Mutex
	active         map[string]context.CancelFunc
	closed         bool
	wg             sync.WaitGroup
	logWarnings    sync.Map
	transfers      sync.Map
	executing      sync.Map // attempt IDs that hold a download slot
}

//wails:ignore
func (s *Service) ConfigureVerification(verify func(context.Context, string, string) error) {
	s.verifyMedia = verify
}

//wails:ignore
func (s *Service) ConfigureImages(inspector ImageInspector) {
	s.imageInspector = inspector
}

//wails:ignore
func (s *Service) ConfigurePreviewCleanup(cleanup func(context.Context, string)) {
	s.previewCleanup = cleanup
}

//wails:ignore
func (s *Service) ConfigurePreviewLookup(lookup func(context.Context, string) (*string, error)) {
	s.previewLookup = lookup
}

func (s *Service) attachPreview(ctx context.Context, job *Job) {
	job.Waiting = !job.HistoryOnly && !job.CleanupRequired && job.State == "pending"
	if !job.HistoryOnly && !job.CleanupRequired && job.State == "running" && job.Phase != nil && *job.Phase == "discovering" {
		var executing bool
		if job.CurrentAttemptID != nil {
			_, executing = s.executing.Load(*job.CurrentAttemptID)
		}
		job.Waiting = !executing
	}
	s.attachTransfer(job)
	if s.previewLookup != nil {
		job.ThumbnailPath, _ = s.previewLookup(ctx, job.URL)
	}
}

func NewService(db *sql.DB, path func() string, factory func(string, engine.Options) Runner, afterCommit func(int64)) *Service {
	return &Service{db: db, downloadPath: path, factory: factory, afterCommit: afterCommit, active: map[string]context.CancelFunc{}, runSlot: make(chan struct{}, maxConcurrentDownloads)}
}

type Job struct {
	HistoryOnly      bool              `json:"historyOnly"`
	ID               string            `json:"id"`
	URL              string            `json:"url"`
	ThumbnailPath    *string           `json:"thumbnailPath"`
	Title            *string           `json:"title"`
	State            string            `json:"state"`
	Phase            *string           `json:"phase"`
	Progress         int               `json:"progress"`
	Waiting          bool              `json:"waiting"`
	Transfer         *TransferProgress `json:"transfer,omitempty"`
	Options          *engine.Options   `json:"options"`
	CleanupRequired  bool              `json:"cleanupRequired"`
	CurrentAttemptID *string           `json:"currentAttemptId"`
	ErrorCode        *string           `json:"errorCode"`
	ErrorMessage     *string           `json:"errorMessage"`
}
type JobRequest struct {
	JobID string `json:"jobId"`
}
type RunRequest struct {
	JobID   string `json:"jobId"`
	Trigger string `json:"trigger"`
}
type ListRequest struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}
type EnqueueRequest struct {
	URL              string         `json:"url"`
	Options          engine.Options `json:"options"`
	Mode             string         `json:"mode"`
	MediaID          *int64         `json:"mediaId"`
	ExpectedRevision *int64         `json:"expectedRevision"`
}

func (s *Service) PreviewEngine(url string, options engine.Options) (engine.Kind, error) {
	return engine.Select(cleanInstagramURL(url), options)
}
func fail(err error) api.Result {
	code := "DATABASE_ERROR"
	switch {
	case errors.Is(err, sql.ErrNoRows):
		code = "NOT_FOUND"
	case errors.Is(err, ErrAlreadyProcessing):
		code = "ALREADY_PROCESSING"
	case errors.Is(err, ErrCleanupRequired):
		code = "CLEANUP_REQUIRED"
	case errors.Is(err, ErrRedownloadRequired):
		code = "REDOWNLOAD_CONFIRMATION_REQUIRED"
	case errors.Is(err, ErrRevisionConflict):
		code = "REVISION_CONFLICT"
	case errors.Is(err, engine.ErrInvalidURL):
		code = "INVALID_ARGUMENT"
	case errors.Is(err, ErrResourceLimit):
		code = "RESOURCE_LIMIT"
	case errors.Is(err, engine.ErrInstagramURL):
		code = "UNSUPPORTED_INSTAGRAM_URL"
	case errors.Is(err, engine.ErrInstagramOptions):
		code = "UNSUPPORTED_INSTAGRAM_OPTIONS"
	case errors.Is(err, ErrUnsupportedTargetDiscovery):
		code = "UNSUPPORTED_TARGET_DISCOVERY"
	case errors.Is(err, engine.ErrUnsupportedMediaFormat):
		code = "UNSUPPORTED_MEDIA_FORMAT"
	case errors.Is(err, engine.ErrStopUnconfirmed):
		code = "ENGINE_STOP_UNCONFIRMED"
	}
	return api.Failure(code, err.Error())
}

const jobSelect = `SELECT q.id,q.original_url,q.title,q.state,a.phase,q.progress,q.options_json,q.cleanup_required,q.current_attempt_id,q.error_code,q.error_message FROM download_queue q LEFT JOIN download_attempt a ON a.id=q.current_attempt_id`

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	var opts string
	err := row.Scan(&j.ID, &j.URL, &j.Title, &j.State, &j.Phase, &j.Progress, &opts, &j.CleanupRequired, &j.CurrentAttemptID, &j.ErrorCode, &j.ErrorMessage)
	var options engine.Options
	var flags struct {
		Imported bool `json:"_importedHistory"`
	}
	_ = json.Unmarshal([]byte(opts), &flags)
	j.HistoryOnly = flags.Imported
	if !flags.Imported && json.Unmarshal([]byte(opts), &options) == nil {
		if _, e := initialOptionsJSON(options); e == nil {
			j.Options = &options
		}
	}
	return j, err
}
func (s *Service) Enqueue(ctx context.Context, req EnqueueRequest) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	if req.Mode != "create" && req.Mode != "replace" {
		return api.Failure("INVALID_ARGUMENT", "다운로드 방식이 올바르지 않습니다")
	}
	req.URL = cleanInstagramURL(req.URL)
	if _, err := engine.Select(req.URL, req.Options); err != nil {
		return api.Failure("INVALID_ARGUMENT", err.Error())
	}
	if _, err := initialOptionsJSON(req.Options); err != nil {
		return api.Failure("INVALID_ARGUMENT", err.Error())
	}
	if req.Mode == "replace" && (req.MediaID == nil || req.ExpectedRevision == nil || *req.MediaID <= 0 || *req.ExpectedRevision < 0) {
		return api.Failure("INVALID_ARGUMENT", "교체할 묶음과 버전이 필요합니다")
	}
	if req.Mode == "create" && (req.MediaID != nil || req.ExpectedRevision != nil) {
		return api.Failure("INVALID_ARGUMENT", "새 다운로드에 교체 대상을 지정할 수 없습니다")
	}
	root := s.downloadPath()
	if err := os.MkdirAll(root, 0700); err != nil {
		return fail(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return api.Failure("NOT_READY", "앱을 종료하고 있습니다")
	}
	full, queueErr := s.downloadQueueFull(ctx)
	if queueErr != nil {
		return fail(queueErr)
	}
	if full {
		return api.Failure("QUEUE_FULL", "대기 다운로드는 최대 20건입니다")
	}
	if err := checkDownloadResources(root, ""); err != nil {
		return fail(err)
	}
	record, err := NewStore(s.db).CreateInitialAttempt(ctx, InitialAttemptInput{OriginalURL: req.URL, StorageRoot: root, Options: req.Options, ReplaceMediaID: req.MediaID, ExpectedRevision: req.ExpectedRevision})
	if err != nil {
		result := fail(err)
		if errors.Is(err, ErrRedownloadRequired) {
			normalized, e := normalizeRequestURL(req.URL)
			if e == nil {
				var id, revision int64
				if s.db.QueryRowContext(ctx, `SELECT m.id,m.revision FROM media m JOIN download_urls u ON u.id=m.request_url_id WHERE u.normalized_url=? AND u.normalization_version=1`, normalized).Scan(&id, &revision) == nil {
					result.Error.Context = map[string]int64{"mediaId": id, "expectedRevision": revision}
				}
			}
		}
		return result
	}
	return s.Get(ctx, JobRequest{record.JobID})
}
func (s *Service) Get(ctx context.Context, req JobRequest) api.Result {
	if req.JobID == "" {
		return api.Failure("INVALID_ARGUMENT", "작업 ID가 필요합니다")
	}
	j, err := scanJob(s.db.QueryRowContext(ctx, jobSelect+" WHERE q.id=?", req.JobID))
	if err != nil {
		return fail(err)
	}
	s.attachPreview(ctx, &j)
	return api.Success(j)
}
func (s *Service) List(ctx context.Context, req ListRequest) api.Result {
	if req.Limit == 0 {
		req.Limit = 50
	}
	if req.Limit < 1 || req.Limit > 200 || req.Offset < 0 {
		return api.Failure("INVALID_ARGUMENT", "목록 범위가 올바르지 않습니다")
	}
	jobs, err := s.listJobs(ctx, jobSelect+" ORDER BY q.created_at DESC,q.id DESC LIMIT ? OFFSET ?", req.Limit+1, req.Offset)
	if err != nil {
		return fail(err)
	}
	hasNext := len(jobs) > req.Limit
	if hasNext {
		jobs = jobs[:req.Limit]
	}
	// Gallery activity is independent of the history page. Never create provisional media rows.
	active, err := s.listJobs(ctx, jobSelect+` WHERE q.state IN ('pending','running') AND q.cleanup_required=0
		AND coalesce(a.phase,'')<>'rolling_back' AND coalesce(json_extract(q.options_json,'$._importedHistory'),0)=0
		ORDER BY q.created_at DESC,q.id DESC`)
	if err != nil {
		return fail(err)
	}
	return api.Success(map[string]any{"data": jobs, "activeJobs": active, "limit": req.Limit, "offset": req.Offset, "hasNextPage": hasNext})
}

func (s *Service) listJobs(ctx context.Context, query string, args ...any) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		j, e := scanJob(rows)
		if e != nil {
			return nil, e
		}
		jobs = append(jobs, j)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// Release the cursor before the local lookup uses the same database pool.
	if err = rows.Close(); err != nil {
		return nil, err
	}
	for i := range jobs {
		s.attachPreview(ctx, &jobs[i])
	}
	return jobs, nil
}
func (s *Service) Run(ctx context.Context, req RunRequest) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	if req.JobID == "" || (req.Trigger != "manual_initial" && req.Trigger != "manual_retry" && req.Trigger != "manual_restart") {
		return api.Failure("INVALID_ARGUMENT", "실행 요청이 올바르지 않습니다")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return api.Failure("NOT_READY", "앱을 종료하고 있습니다")
	}
	if _, ok := s.active[req.JobID]; ok {
		s.mu.Unlock()
		return fail(ErrAlreadyProcessing)
	}
	var imported bool
	if err := s.db.QueryRowContext(ctx, "SELECT coalesce(json_extract(options_json,'$._importedHistory'),0) FROM download_queue WHERE id=?", req.JobID).Scan(&imported); err != nil {
		s.mu.Unlock()
		return fail(err)
	}
	if imported {
		s.mu.Unlock()
		return api.Failure("IMPORTED_HISTORY", "가져오거나 이관한 작업은 보존된 이력입니다. 다시 다운로드하려면 URL을 새로 등록하세요")
	}
	if len(s.active) >= MaxQueuedDownloads+maxConcurrentDownloads {
		s.mu.Unlock()
		return api.Failure("QUEUE_FULL", "대기 다운로드는 최대 20건입니다")
	}
	if req.Trigger != "manual_initial" {
		full, queueErr := s.downloadQueueFull(ctx)
		if queueErr != nil {
			s.mu.Unlock()
			return fail(queueErr)
		}
		if full {
			s.mu.Unlock()
			return api.Failure("QUEUE_FULL", "대기 다운로드는 최대 20건입니다")
		}
		if err := s.retry(ctx, req.JobID, req.Trigger); err != nil {
			s.mu.Unlock()
			return fail(err)
		}
	}
	var attempt, root, url, options, phase, state string
	var cleanup bool
	err := s.db.QueryRowContext(ctx, `SELECT a.id,st.storage_root,q.original_url,q.options_json,a.phase,q.state,q.cleanup_required FROM download_queue q JOIN download_attempt a ON a.id=q.current_attempt_id JOIN download_job_storage st ON st.job_id=q.id WHERE q.id=? AND a.outcome IS NULL`, req.JobID).Scan(&attempt, &root, &url, &options, &phase, &state, &cleanup)
	if err != nil {
		s.mu.Unlock()
		return fail(err)
	}
	if phase != "discovering" || state != "running" || cleanup {
		s.mu.Unlock()
		return fail(ErrAlreadyProcessing)
	}
	var opts engine.Options
	if err = json.Unmarshal([]byte(options), &opts); err != nil {
		s.mu.Unlock()
		return api.Failure("INVALID_ARGUMENT", "저장된 옵션이 올바르지 않습니다")
	}
	workCtx, cancel := context.WithCancel(ctx)
	s.active[req.JobID] = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.active, req.JobID); s.mu.Unlock(); s.wg.Done() }()
	journal := newLogRecorder(NewStore(s.db), req.JobID, attempt, func(e error) { s.logWarning(req.JobID, e) })
	defer journal.close()
	transfer := &transferTracker{attempt: attempt}
	s.transfers.Store(req.JobID, transfer)
	defer s.transfers.Delete(req.JobID)
	workCtx = context.WithValue(workCtx, transferKey{}, transfer)
	journal.note("실행 시도 시작 · 엔진 실행 순서를 기다립니다")
	workCtx = engine.WithOutputObserver(workCtx, func(output engine.Output) {
		if !transfer.output(output) {
			journal.write(output)
		}
	})
	select {
	case s.runSlot <- struct{}{}:
		defer func() { <-s.runSlot }()
		s.executing.Store(attempt, struct{}{})
		defer s.executing.Delete(attempt)
		if workCtx.Err() != nil {
			err = workCtx.Err()
		} else {
			journal.note("다운로드 대상 조회 시작")
			runner := s.factory(root, opts)
			stopWatch := watchDownloadResources(workCtx, cancel, root, filepath.Join(root, ".storagebucket", "staging", stageOwner(req.JobID, attempt)))
			closeNetwork := func() {}
			if runner.SecureNetwork {
				workCtx, closeNetwork, err = engine.SecureContext(workCtx)
			}
			var discovery AttemptDiscovery
			if err == nil {
				discovery, err = runner.DiscoverAttempt(workCtx, req.JobID, attempt, url, opts)
			}
			if err == nil {
				journal.note("대상 확정 · 파일 다운로드 시작")
				_, err = runner.FreezeAndStageAttempt(workCtx, NewStore(s.db), discovery)
			}
			closeNetwork()
			if limitErr := stopWatch(); limitErr != nil {
				err = limitErr
			}
			if err == nil {
				err = checkDownloadResources(root, filepath.Join(root, ".storagebucket", "staging", stageOwner(req.JobID, attempt)))
			}
		}
	case <-workCtx.Done():
		err = workCtx.Err()
	}
	var mediaID int64
	if err == nil {
		// Cancel shares this lock so publication has a single cancellation boundary.
		s.mu.Lock()
		if workCtx.Err() != nil {
			err = workCtx.Err()
		} else {
			err = s.beginCommit(workCtx, req.JobID, attempt)
		}
		s.mu.Unlock()
		if err == nil {
			journal.note("다운로드 결과 검증 및 저장 시작")
			mediaID, err = s.commit(engine.WithOutputObserver(context.Background(), journal.write), req.JobID, attempt, root)
		}
	}
	if err != nil {
		journal.note("실행 실패: " + err.Error())
		code := "DOWNLOAD_ENGINE_FAILED"
		outcome := "failed"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			outcome = "cancelled"
			code = "CANCELLED"
		}
		if errors.Is(err, ErrUnsupportedTargetDiscovery) || errors.Is(err, engine.ErrUnsupportedURL) {
			code = "UNSUPPORTED_TARGET_DISCOVERY"
		}
		if errors.Is(err, ErrRevisionConflict) {
			code = "REVISION_CONFLICT"
		}
		if errors.Is(err, ErrResourceLimit) {
			code = "RESOURCE_LIMIT"
			outcome = "failed"
		}
		if errors.Is(err, engine.ErrStopUnconfirmed) {
			code = "ENGINE_STOP_UNCONFIRMED"
		}
		switch {
		case errors.Is(err, engine.ErrUnsupportedMediaFormat):
			code = "UNSUPPORTED_MEDIA_FORMAT"
		case errors.Is(err, engine.ErrInstagramPublicExtraction):
			code = "PUBLIC_MEDIA_EXTRACTION_FAILED"
		case errors.Is(err, engine.ErrAuthenticationRequired):
			code = "AUTHENTICATION_REQUIRED"
		case errors.Is(err, engine.ErrAccessDenied):
			code = "ACCESS_DENIED"
		case errors.Is(err, engine.ErrMediaUnavailable):
			code = "MEDIA_UNAVAILABLE"
		case errors.Is(err, engine.ErrRateLimited):
			code = "RATE_LIMITED"
		}
		if e := s.abort(req.JobID, attempt, root, outcome, code, err.Error(), errors.Is(err, engine.ErrStopUnconfirmed)); e != nil {
			return api.Failure("CLEANUP_REQUIRED", e.Error())
		}
		return api.Failure(code, err.Error())
	}
	if s.afterCommit != nil {
		s.afterCommit(mediaID)
	}
	journal.note(fmt.Sprintf("다운로드 및 DB 등록 완료 (미디어 %d)", mediaID))
	return api.Success(map[string]any{"status": "committed", "jobId": req.JobID, "attemptId": attempt, "mediaIdSnapshot": mediaID})
}
func (s *Service) Cancel(ctx context.Context, req JobRequest) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	s.mu.Lock()
	defer s.mu.Unlock()
	var phase, state string
	var attempt, root string
	err := s.db.QueryRowContext(ctx, `SELECT a.phase,q.state,a.id,st.storage_root FROM download_queue q JOIN download_attempt a ON a.id=q.current_attempt_id JOIN download_job_storage st ON st.job_id=q.id WHERE q.id=?`, req.JobID).Scan(&phase, &state, &attempt, &root)
	if err != nil {
		return fail(err)
	}
	if phase == "committing" {
		return api.Failure("CANNOT_CANCEL_COMMITTING", "저장 중인 작업은 취소할 수 없습니다")
	}
	if state != "running" && state != "pending" {
		return api.Failure("INVALID_ARGUMENT", "진행 중인 작업만 취소할 수 있습니다")
	}
	if _, err = s.db.ExecContext(ctx, "UPDATE download_attempt SET cancel_requested_at=? WHERE id=? AND outcome IS NULL", time.Now().UnixMilli(), attempt); err != nil {
		return fail(err)
	}
	if cancel, ok := s.active[req.JobID]; ok {
		cancel()
	} else if err = s.abort(req.JobID, attempt, root, "cancelled", "CANCELLED", "사용자가 취소했습니다", false); err != nil {
		return fail(err)
	}
	return api.Success(map[string]bool{"accepted": true})
}

func (s *Service) Cleanup(ctx context.Context, req JobRequest) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, active := s.active[req.JobID]; active {
		return fail(ErrAlreadyProcessing)
	}
	var attempt, root, state string
	var code *string
	var required bool
	err := s.db.QueryRowContext(ctx, `SELECT a.id,st.storage_root,q.state,q.error_code,q.cleanup_required FROM download_queue q JOIN download_attempt a ON a.id=q.current_attempt_id JOIN download_job_storage st ON st.job_id=q.id WHERE q.id=?`, req.JobID).Scan(&attempt, &root, &state, &code, &required)
	if err != nil {
		return fail(err)
	}
	if !required {
		return api.Success(map[string]bool{"accepted": true})
	}
	if code != nil && *code == "ENGINE_STOP_UNCONFIRMED" {
		return api.Failure("ENGINE_STOP_UNCONFIRMED", "앱을 다시 시작한 뒤 정리를 시도하세요")
	}
	if state == "pending" || state == "running" {
		return fail(ErrAlreadyProcessing)
	}
	value := "CLEANUP_REQUIRED"
	if code != nil {
		value = *code
	}
	if err = s.abort(req.JobID, attempt, root, state, value, "임시 파일 정리를 완료했습니다", false); err != nil {
		return api.Failure("CLEANUP_REQUIRED", err.Error())
	}
	return api.Success(map[string]bool{"accepted": true})
}

//wails:ignore
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	for _, cancel := range s.active {
		cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}
func (s *Service) retry(ctx context.Context, jobID, trigger string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	var cleanup bool
	var urlID int64
	var target *int64
	if err = tx.QueryRowContext(ctx, "SELECT state,cleanup_required,request_url_id,target_media_id FROM download_queue WHERE id=?", jobID).Scan(&state, &cleanup, &urlID, &target); err != nil {
		return err
	}
	if cleanup {
		return ErrCleanupRequired
	}
	if state == "running" || state == "pending" {
		return ErrAlreadyProcessing
	}
	if state == "completed" {
		return ErrRedownloadRequired
	}
	var active int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM download_queue WHERE request_url_id=? AND id<>? AND (state IN ('running','pending') OR cleanup_required=1)", urlID, jobID).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return ErrAlreadyProcessing
	}
	var mediaCount int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM media WHERE request_url_id=?", urlID).Scan(&mediaCount); err != nil {
		return err
	}
	if mediaCount > 0 && target == nil {
		return ErrRedownloadRequired
	}
	var attempt string
	if err = tx.QueryRowContext(ctx, "SELECT lower(hex(randomblob(16)))").Scan(&attempt); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	_, err = tx.ExecContext(ctx, `INSERT INTO download_attempt(id,job_id,attempt_no,trigger,phase,started_at,target_media_id,target_media_id_snapshot) SELECT ?,id,(SELECT coalesce(max(attempt_no),0)+1 FROM download_attempt WHERE job_id=q.id),?,'discovering',?,target_media_id,target_media_id_snapshot FROM download_queue q WHERE id=?`, attempt, trigger, now, jobID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE download_queue SET state='running',current_attempt_id=?,progress=0,error_code=NULL,error_message=NULL,finished_at=NULL,started_at=?,updated_at=? WHERE id=?", attempt, now, now, jobID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func requireOne(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("작업 상태가 변경되었습니다")
	}
	return nil
}
