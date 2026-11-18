package localimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/google/uuid"
)

type Inspector interface {
	Inspect(context.Context, string, string) (engine.LocalMetadata, error)
}
type Request struct {
	JobID     string `json:"jobId"`
	Offset    int    `json:"offset"`
	Limit     int    `json:"limit"`
	FolderID  string `json:"folderId"`
	TargetID  string `json:"targetId"`
	FileState string `json:"fileState"`
	AllFailed bool   `json:"allFailed"`
}
type Counts struct {
	Discovered       int   `json:"discovered"`
	Excluded         int   `json:"excluded"`
	ScanExcluded     int   `json:"scanExcluded"`
	UserExcluded     int   `json:"userExcluded"`
	ScanErrors       int   `json:"scanErrors"`
	Added            int   `json:"added"`
	Updated          int   `json:"updated"`
	Unchanged        int   `json:"unchanged"`
	Failed           int   `json:"failed"`
	Processed        int   `json:"processed"`
	TotalBytes       int64 `json:"totalBytes"`
	ProcessedBytes   int64 `json:"processedBytes"`
	CompletedFolders int   `json:"completedFolders"`
	FailedFolders    int   `json:"failedFolders"`
}
type Job struct {
	ID              string  `json:"id"`
	Root            string  `json:"root"`
	State           string  `json:"state"`
	Counts          Counts  `json:"counts"`
	CancelRequested bool    `json:"cancelRequested"`
	PauseRequested  bool    `json:"pauseRequested"`
	ScanComplete    bool    `json:"scanComplete"`
	CanResume       bool    `json:"canResume"`
	CurrentPath     string  `json:"currentPath"`
	Error           *string `json:"error"`
	ElapsedMillis   int64   `json:"elapsedMillis"`
	BytesPerSecond  float64 `json:"bytesPerSecond"`
	FilesPerSecond  float64 `json:"filesPerSecond"`
	LargeFolders    int     `json:"largeFolders"`
	Revision        int64   `json:"revision"`
}
type Folder struct {
	ID        string  `json:"id"`
	Path      string  `json:"path"`
	State     string  `json:"state"`
	Error     *string `json:"error"`
	FileCount int     `json:"fileCount"`
}
type Page struct {
	Job         *Job     `json:"job"`
	Folders     []Folder `json:"folders"`
	Total       int      `json:"total"`
	HasNextPage bool     `json:"hasNextPage"`
}
type File struct {
	ID       string  `json:"id"`
	FolderID string  `json:"folderId"`
	Path     string  `json:"path"`
	State    string  `json:"state"`
	Error    *string `json:"error"`
}
type FilePage struct {
	Files       []File `json:"files"`
	Total       int    `json:"total"`
	HasNextPage bool   `json:"hasNextPage"`
}
type Service struct {
	db          *sql.DB
	inspector   Inspector
	choose      func() (string, error)
	notify      func(int64)
	mu          sync.Mutex
	active      string
	currentPath string
	generation  int64
	cancel      context.CancelFunc
	closed      bool
	wg          sync.WaitGroup
}

func NewService(db *sql.DB, inspector Inspector, choose func() (string, error), notify func(int64)) *Service {
	return &Service{db: db, inspector: inspector, choose: choose, notify: notify}
}
func fail(err error) api.Result { return api.Failure("LOCAL_IMPORT_FAILED", err.Error()) }
func bounds(req Request) (int, error) {
	if req.Offset < 0 || req.Limit < 0 || req.Limit > 200 {
		return 0, fmt.Errorf("조회 범위를 확인하세요")
	}
	if req.Limit == 0 {
		return 25, nil
	}
	return req.Limit, nil
}
func (s *Service) Choose(ctx context.Context) api.Result {
	if s.choose == nil {
		return fail(fmt.Errorf("폴더 선택 창을 사용할 수 없습니다"))
	}
	path, err := s.choose()
	if err != nil {
		return fail(err)
	}
	if path == "" {
		return api.Success(Page{Folders: []Folder{}})
	}
	return s.startScan(ctx, path)
}
func (s *Service) Rescan(ctx context.Context, req Request) api.Result {
	var root, state string
	if err := s.db.QueryRowContext(ctx, "SELECT root_path,state FROM local_import_job WHERE id=?", req.JobID).Scan(&root, &state); err != nil {
		return fail(err)
	}
	if state == "scanning" || state == "running" {
		return fail(fmt.Errorf("현재 작업을 중단한 뒤 다시 탐색하세요"))
	}
	return s.startScan(ctx, root)
}
func (s *Service) startScan(ctx context.Context, path string) api.Result {
	if !filepath.IsAbs(path) {
		return fail(fmt.Errorf("폴더 절대 경로가 필요합니다"))
	}
	root, key, err := canonical(path)
	if err != nil {
		return fail(err)
	}
	if info, e := os.Stat(root); e != nil || !info.IsDir() {
		return fail(fmt.Errorf("읽을 수 있는 폴더를 선택하세요"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != "" {
		return fail(fmt.Errorf("다른 폴더 가져오기가 진행 중입니다"))
	}
	id := uuid.NewString()
	now := time.Now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO local_import_job(id,root_path,root_key,state,counts_json,created_at,updated_at,run_started_at) VALUES(?,?,?,'scanning',?,?,?,?)", id, root, key, emptyCounts(), now, now, now); err != nil {
		return fail(err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO local_import_scan_directory(job_id,directory_path,directory_key,state) VALUES(?,?,?,'pending')", id, root, key); err != nil {
		return fail(err)
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	s.launch(id, true)
	return api.Success(Page{Job: &Job{ID: id, Root: root, State: "scanning", CanResume: true}, Folders: []Folder{}})
}

// Caller owns mu. A view's lifetime does not own background work.
func (s *Service) launch(id string, scan bool) {
	work, cancel := context.WithCancel(context.Background())
	s.active = id
	s.currentPath = ""
	s.cancel = cancel
	s.generation++
	generation := s.generation
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		defer func() {
			s.mu.Lock()
			if s.generation == generation {
				s.active = ""
				s.cancel = nil
			}
			s.mu.Unlock()
		}()
		if scan {
			s.scan(work, id)
		} else {
			s.run(work, id)
		}
	}()
}
func (s *Service) Get(ctx context.Context, req Request) api.Result {
	s.mu.Lock()
	active, path := s.active, s.currentPath
	s.mu.Unlock()
	limit, err := bounds(req)
	if err != nil {
		return fail(err)
	}
	p := Page{Folders: []Folder{}}
	var j Job
	var raw string
	var cancelled, paused, started *int64
	var version int
	query := "SELECT id,root_path,state,counts_json,cancel_requested_at,pause_requested_at,scan_complete,work_version,current_path,error_message,active_millis,run_started_at,revision FROM local_import_job WHERE id=?"
	args := []any{req.JobID}
	if req.JobID == "" {
		query = "SELECT id,root_path,state,counts_json,cancel_requested_at,pause_requested_at,scan_complete,work_version,current_path,error_message,active_millis,run_started_at,revision FROM local_import_job ORDER BY created_at DESC,rowid DESC LIMIT 1"
		args = nil
	}
	err = s.db.QueryRowContext(ctx, query, args...).Scan(&j.ID, &j.Root, &j.State, &raw, &cancelled, &paused, &j.ScanComplete, &version, &j.CurrentPath, &j.Error, &j.ElapsedMillis, &started, &j.Revision)
	if err == sql.ErrNoRows && req.JobID == "" {
		return api.Success(p)
	}
	if err != nil {
		return fail(err)
	}
	if err = json.Unmarshal([]byte(raw), &j.Counts); err != nil {
		return fail(err)
	}
	j.CancelRequested = cancelled != nil
	if active == j.ID && (j.State == "scanning" || j.State == "running") {
		j.CurrentPath = path
	}
	j.PauseRequested = paused != nil
	j.CanResume = version == 2
	if started != nil {
		j.ElapsedMillis += max(0, time.Now().UnixMilli()-*started)
	}
	if j.ElapsedMillis > 0 {
		j.BytesPerSecond = float64(j.Counts.ProcessedBytes) * 1000 / float64(j.ElapsedMillis)
		j.FilesPerSecond = float64(j.Counts.Processed) * 1000 / float64(j.ElapsedMillis)
	}
	if err = s.db.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(CASE WHEN (SELECT count(*) FROM local_import_target t WHERE t.attempt_id=f.current_attempt_id)>50000 THEN 1 ELSE 0 END),0) FROM local_import_folder f WHERE job_id=?`, j.ID).Scan(&p.Total, &j.LargeFolders); err != nil {
		return fail(err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT f.id,f.relative_path,f.state,f.error_code,(SELECT count(*) FROM local_import_target t WHERE t.attempt_id=f.current_attempt_id) FROM local_import_folder f WHERE job_id=? ORDER BY rowid LIMIT ? OFFSET ?`, j.ID, limit, req.Offset)
	if err != nil {
		return fail(err)
	}
	defer rows.Close()
	for rows.Next() {
		var f Folder
		if err = rows.Scan(&f.ID, &f.Path, &f.State, &f.Error, &f.FileCount); err != nil {
			return fail(err)
		}
		p.Folders = append(p.Folders, f)
	}
	if err = rows.Err(); err != nil {
		return fail(err)
	}
	p.Job = &j
	p.HasNextPage = req.Offset+len(p.Folders) < p.Total
	return api.Success(p)
}
func (s *Service) Files(ctx context.Context, req Request) api.Result {
	limit, err := bounds(req)
	if err != nil {
		return fail(err)
	}
	if req.FileState != "" && req.FileState != "failed" && req.FileState != "pending" && req.FileState != "excluded" && req.FileState != "registered" {
		return fail(fmt.Errorf("파일 상태를 확인하세요"))
	}
	where := "f.job_id=?"
	args := []any{req.JobID}
	if req.FolderID != "" {
		where += " AND f.id=?"
		args = append(args, req.FolderID)
	}
	if req.FileState != "" {
		where += " AND t.state=?"
		args = append(args, req.FileState)
	}
	p := FilePage{Files: []File{}}
	if err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM local_import_target t JOIN local_import_folder f ON f.current_attempt_id=t.attempt_id WHERE "+where, args...).Scan(&p.Total); err != nil {
		return fail(err)
	}
	args = append(args, limit, req.Offset)
	rows, err := s.db.QueryContext(ctx, "SELECT t.id,f.id,f.relative_path,t.relative_path,t.state,t.error_code FROM local_import_target t JOIN local_import_folder f ON f.current_attempt_id=t.attempt_id WHERE "+where+" ORDER BY f.rowid,t.sort_key,t.id LIMIT ? OFFSET ?", args...)
	if err != nil {
		return fail(err)
	}
	defer rows.Close()
	for rows.Next() {
		var f File
		var folder, name string
		if err = rows.Scan(&f.ID, &f.FolderID, &folder, &name, &f.State, &f.Error); err != nil {
			return fail(err)
		}
		f.Path = filepath.Join(folder, name)
		p.Files = append(p.Files, f)
	}
	if err = rows.Err(); err != nil {
		return fail(err)
	}
	p.HasNextPage = req.Offset+len(p.Files) < p.Total
	return api.Success(p)
}
func (s *Service) Run(ctx context.Context, req Request) api.Result    { return s.begin(ctx, req, false) }
func (s *Service) Resume(ctx context.Context, req Request) api.Result { return s.begin(ctx, req, true) }
func (s *Service) begin(ctx context.Context, req Request, resume bool) api.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != "" {
		return fail(fmt.Errorf("다른 폴더 가져오기가 진행 중입니다"))
	}
	var state string
	var scanned bool
	var version int
	if err := s.db.QueryRowContext(ctx, "SELECT state,scan_complete,work_version FROM local_import_job WHERE id=?", req.JobID).Scan(&state, &scanned, &version); err != nil {
		return fail(err)
	}
	if version != 2 {
		return fail(fmt.Errorf("이전 방식의 작업은 같은 폴더를 다시 탐색하세요"))
	}
	allowed := state == "ready"
	if resume {
		allowed = state == "paused" || state == "interrupted" || state == "cancelled" || state == "failed"
	}
	if !allowed {
		return fail(fmt.Errorf("시작하거나 이어갈 수 있는 작업 상태를 확인하세요"))
	}
	next := "running"
	if !scanned {
		next = "scanning"
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE local_import_job SET state=?,cancel_requested_at=NULL,pause_requested_at=NULL,error_message=NULL,run_started_at=?,revision=revision+1,updated_at=? WHERE id=?", next, time.Now().UnixMilli(), time.Now().UnixMilli(), req.JobID); err != nil {
		return fail(err)
	}
	s.launch(req.JobID, !scanned)
	return api.Success(true)
}
func (s *Service) Pause(ctx context.Context, req Request) api.Result  { return s.stop(ctx, req, false) }
func (s *Service) Cancel(ctx context.Context, req Request) api.Result { return s.stop(ctx, req, true) }
func (s *Service) stop(ctx context.Context, req Request, cancelled bool) api.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	column, next := "pause_requested_at", "paused"
	if cancelled {
		column, next = "cancel_requested_at", "cancelled"
	}
	result, err := s.db.ExecContext(ctx, "UPDATE local_import_job SET "+column+"=?,revision=revision+1,updated_at=? WHERE id=? AND state IN ('scanning','ready','running')", time.Now().UnixMilli(), time.Now().UnixMilli(), req.JobID)
	if err != nil {
		return fail(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fail(err)
	}
	if n == 0 {
		return fail(fmt.Errorf("진행 중인 작업을 확인하세요"))
	}
	if s.active == req.JobID && s.cancel != nil {
		s.cancel()
	} else {
		if err = s.endLocked(req.JobID, next, nil); err != nil {
			return fail(err)
		}
	}
	return api.Success(true)
}
func (s *Service) Retry(ctx context.Context, req Request) api.Result {
	return s.changeTargets(ctx, req, true)
}
func (s *Service) Exclude(ctx context.Context, req Request) api.Result {
	return s.changeTargets(ctx, req, false)
}
func (s *Service) changeTargets(ctx context.Context, req Request, retry bool) api.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != "" {
		return fail(fmt.Errorf("작업을 중단한 뒤 파일을 변경하세요"))
	}
	if req.TargetID == "" && !req.AllFailed {
		return fail(fmt.Errorf("대상 파일을 선택하세요"))
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	var state string
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT state,work_version FROM local_import_job WHERE id=?", req.JobID).Scan(&state, &version); err != nil {
		return fail(err)
	}
	if version != 2 || state == "running" || state == "scanning" {
		return fail(fmt.Errorf("중단되거나 완료된 작업을 확인하세요"))
	}
	filter := "attempt_id IN (SELECT current_attempt_id FROM local_import_folder WHERE job_id=?)"
	args := []any{req.JobID}
	if req.TargetID != "" {
		filter += " AND id=?"
		args = append(args, req.TargetID)
	}
	states := "('pending','failed')"
	update := "state='excluded',error_code=NULL"
	if retry {
		states = "('failed','excluded')"
		update = "state='pending',error_code=NULL,refresh_requested=1,inspection_json=NULL,inspection_hash=NULL,inspector_version=NULL,committed_at=NULL,media_file_id=NULL,file_uuid_snapshot=NULL,result_kind=NULL"
	}
	if req.AllFailed {
		states = "('failed')"
	}
	if _, err = tx.ExecContext(ctx, "UPDATE local_import_folder SET state='pending',error_code=NULL WHERE current_attempt_id IN (SELECT attempt_id FROM local_import_target WHERE "+filter+" AND state IN "+states+")", args...); err != nil {
		return fail(err)
	}
	result, err := tx.ExecContext(ctx, "UPDATE local_import_target SET "+update+" WHERE "+filter+" AND state IN "+states, args...)
	if err != nil {
		return fail(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fail(err)
	}
	if n == 0 {
		return fail(fmt.Errorf("변경할 파일이 없습니다"))
	}
	if _, err = tx.ExecContext(ctx, `UPDATE local_import_folder SET state='pending',error_code=NULL WHERE job_id=? AND EXISTS(SELECT 1 FROM local_import_target t WHERE t.attempt_id=current_attempt_id AND t.state='pending')`, req.JobID); err != nil {
		return fail(err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE local_import_attempt SET outcome=NULL,finished_at=NULL,committed_at=NULL,phase='inspecting' WHERE id IN (SELECT current_attempt_id FROM local_import_folder WHERE job_id=? AND state='pending')", req.JobID); err != nil {
		return fail(err)
	}
	if err = refreshCounts(ctx, tx, req.JobID); err != nil {
		return fail(err)
	}
	next := "paused"
	if state == "ready" {
		next = "ready"
	}
	if _, err = tx.ExecContext(ctx, "UPDATE local_import_job SET state=?,pause_requested_at=NULL,cancel_requested_at=NULL,revision=revision+1,updated_at=? WHERE id=?", next, time.Now().UnixMilli(), req.JobID); err != nil {
		return fail(err)
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	return api.Success(true)
}

//wails:ignore
func (s *Service) Recover(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != "" {
		return fmt.Errorf("실행 중에는 복구할 수 없습니다")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE local_import_folder SET state='interrupted' WHERE state IN ('pending','running') AND job_id IN (SELECT id FROM local_import_job WHERE state IN ('scanning','pending','running'))`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE local_import_job SET state='interrupted',active_millis=active_millis+CASE WHEN run_started_at IS NULL THEN 0 ELSE max(0,updated_at-run_started_at) END,run_started_at=NULL,current_path='',revision=revision+1 WHERE state IN ('scanning','pending','running')`); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM local_import_job WHERE state='interrupted'")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = refreshCounts(ctx, tx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

//wails:ignore
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}
