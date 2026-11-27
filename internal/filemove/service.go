package filemove

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"github.com/google/uuid"
)

type Inspector interface {
	Inspect(context.Context, string, string) (engine.LocalMetadata, error)
}
type Service struct {
	UpdateGate *api.UpdateGate
	inspector  Inspector
	db         *sql.DB
	mu         sync.Mutex
	cancel     context.CancelFunc
	done       chan struct{}
	active     string
	copied     int64
	current    string
	closed     bool
	changed    func(int64)
}

func NewService(db *sql.DB, changed func(int64)) *Service { return &Service{db: db, changed: changed} }

//wails:ignore
func (s *Service) ConfigureInspector(inspector Inspector) { s.inspector = inspector }

type Selection struct {
	MediaID  int64   `json:"mediaId"`
	Revision int64   `json:"revision"`
	FileIDs  []int64 `json:"fileIds"`
}
type CreateRequest struct {
	Selection   []Selection `json:"selection"`
	Operation   string      `json:"operation"`
	DirectoryID int64       `json:"directoryId"`
	MergeTitle  *string     `json:"mergeTitle"`
}
type Request struct {
	JobID  string `json:"jobId"`
	Offset int    `json:"offset"`
}
type Item struct {
	ID      string `json:"id"`
	MediaID int64  `json:"mediaId"`
	FileID  int64  `json:"fileId"`
	Source  string `json:"source"`
	Target  string `json:"target"`
	State   string `json:"state"`
	Error   string `json:"error"`
}
type Status struct {
	JobID          string `json:"jobId"`
	AlbumID        int64  `json:"albumId"`
	Operation      string `json:"operation"`
	Directory      string `json:"directory"`
	State          string `json:"state"`
	Total          int64  `json:"total"`
	Completed      int64  `json:"completed"`
	Failed         int64  `json:"failed"`
	CleanupPending int64  `json:"cleanupPending"`
	TotalBytes     int64  `json:"totalBytes"`
	ProcessedBytes int64  `json:"processedBytes"`
	Current        string `json:"current"`
	Items          []Item `json:"items"`
	Offset         int    `json:"offset"`
	HasMore        bool   `json:"hasMore"`
}
type expected struct {
	Version           int    `json:"version"`
	AlbumID           int64  `json:"albumId,omitempty"`
	KeepName          bool   `json:"keepName,omitempty"`
	Identity          string `json:"identity"`
	Size              int64  `json:"size"`
	Mtime             string `json:"mtime"`
	Token             string `json:"token"`
	DirectoryIdentity string `json:"directoryIdentity"`
	Hash              string `json:"hash,omitempty"`
}
type proof struct {
	Destination media.FileProof `json:"destination"`
	Source      media.FileProof `json:"source"`
	Token       string          `json:"token"`
	Ready       bool            `json:"ready"`
}

func failure(err error) api.Result { return api.Failure("FILE_MOVE_FAILED", err.Error()) }
func (s *Service) Create(ctx context.Context, req CreateRequest) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != "" {
		return api.Failure("JOB_ACTIVE", "파일 이동이 실행 중입니다")
	}
	if len(req.Selection) == 0 || len(req.Selection) > 500 || req.DirectoryID <= 0 || (req.Operation != "" && req.Operation != "copy" && req.Operation != "move") {
		return api.Failure("INVALID_ARGUMENT", "게시글 1~500개와 대상 폴더를 선택하세요")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return failure(e)
	}
	defer tx.Rollback()
	var dir string
	if e = tx.QueryRowContext(ctx, "SELECT directory_path FROM media_directory WHERE id=? AND registered=1", req.DirectoryID).Scan(&dir); e != nil {
		return api.Failure("INVALID_LOCATION", "가져오기로 등록한 폴더를 선택하세요")
	}
	actual, key, e := media.CanonicalPath(dir)
	if e != nil {
		return failure(e)
	}
	dirHandle, e := os.Open(actual)
	if e != nil {
		return failure(e)
	}
	dirID, e := media.FileIdentity(dirHandle)
	dirInfo, se := dirHandle.Stat()
	dirHandle.Close()
	if e != nil {
		return failure(e)
	}
	if se != nil || !dirInfo.IsDir() {
		return api.Failure("INVALID_LOCATION", "대상 폴더를 확인하세요")
	}
	if _, e = media.NewStore(tx).EnsureDirectory(ctx, actual, key, 1, true); e != nil {
		return failure(e)
	}
	seen := map[int64][]int64{}
	selected := map[int64]bool{}
	ordered := []int64{}
	for _, selection := range req.Selection {
		if e = media.CheckWrite(ctx, tx, selection.MediaID, selection.Revision); e != nil {
			return api.Failure("REVISION_CONFLICT", "선택한 앨범이 변경됐거나 다른 작업에서 사용 중입니다. 새로고침 후 확인하세요.")
		}
		if len(selection.FileIDs) > 20000 {
			return api.Failure("INVALID_ARGUMENT", "한 번에 최대 20,000개 파일을 모을 수 있습니다")
		}
		raw, _ := json.Marshal(selection.FileIDs)
		rows, err := tx.QueryContext(ctx, `SELECT id,media_id FROM media_file WHERE (media_id=? OR id IN (SELECT file_id FROM user_album_file WHERE album_id=?)) AND (?=0 OR id IN (SELECT value FROM json_each(?))) ORDER BY coalesce((SELECT position FROM user_album_file WHERE album_id=? AND file_id=media_file.id),position),id LIMIT 20001`, selection.MediaID, selection.MediaID, len(selection.FileIDs), string(raw), selection.MediaID)
		if err != nil {
			return failure(err)
		}
		found := map[int64]bool{}
		for rows.Next() {
			var file, owner int64
			if e = rows.Scan(&file, &owner); e != nil {
				break
			}
			found[file] = true
			if !selected[file] {
				selected[file] = true
				ordered = append(ordered, file)
				seen[owner] = append(seen[owner], file)
			}
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return failure(e)
		}
		for _, file := range selection.FileIDs {
			if !found[file] {
				return api.Failure("INVALID_ARGUMENT", "선택한 파일이 앨범에 없습니다")
			}
		}
		if len(selected) > 20000 {
			return api.Failure("INVALID_ARGUMENT", "한 번에 최대 20,000개 파일을 모을 수 있습니다")
		}
	}
	// The actual owning groups must be idle even when selection came from a virtual album.
	for owner := range seen {
		var revision int64
		if e = tx.QueryRowContext(ctx, "SELECT revision FROM media WHERE id=?", owner).Scan(&revision); e != nil {
			return failure(e)
		}
		if e = media.CheckWrite(ctx, tx, owner, revision); e != nil {
			return api.Failure("JOB_ACTIVE", "원본 파일의 다른 작업이 진행 중입니다")
		}
	}
	operation := req.Operation
	var albumID int64
	if req.MergeTitle != nil {
		albumID, e = media.CreateMergedAlbum(ctx, tx, *req.MergeTitle, ordered)
		if e != nil {
			return failure(e)
		}
	}
	if operation == "" {
		operation = "move"
	}
	job := uuid.NewString()
	now := time.Now().UnixMilli()
	if _, e = tx.ExecContext(ctx, "INSERT INTO file_move_job(id,target_directory_id,target_directory_snapshot,state,created_at,updated_at,operation)VALUES(?,?,?,'pending',?,?,?)", job, req.DirectoryID, actual, now, now, operation); e != nil {
		return failure(e)
	}
	count := 0
	for id, ids := range seen {
		rawIDs, _ := json.Marshal(ids)
		rows, err := tx.QueryContext(ctx, "SELECT id,file_uuid,filepath,filename_token,coalesce(content_hash,'') FROM media_file WHERE media_id=? AND id IN (SELECT value FROM json_each(?)) ORDER BY position,id", id, string(rawIDs))
		if err != nil {
			return failure(err)
		}
		type record struct {
			id                      int64
			uuid, path, token, hash string
		}
		files := []record{}
		for rows.Next() {
			var f record
			if e = rows.Scan(&f.id, &f.uuid, &f.path, &f.token, &f.hash); e != nil {
				break
			}
			files = append(files, f)
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return failure(e)
		}
		for _, f := range files {
			count++
			if count > 20000 {
				return api.Failure("INVALID_ARGUMENT", "한 번에 최대 20,000개 파일을 이동할 수 있습니다")
			}
			state, message := "pending", ""
			stat := expected{Version: 1, AlbumID: albumID, KeepName: req.Operation != "", Token: f.token, DirectoryIdentity: dirID, Hash: f.hash}
			path, sourceKey, err := media.CanonicalPath(f.path)
			if err == nil {
				// Do not follow a last-component link, or accept a directory as a file.
				info, err := os.Lstat(f.path)
				if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
					message = "정상 원본 파일을 확인할 수 없습니다"
				} else {
					h, err := os.Open(path)
					if err != nil {
						message = err.Error()
					} else {
						stat.Identity, err = media.FileIdentity(h)
						h.Close()
						if err != nil {
							message = err.Error()
						}
						stat.Size = info.Size()
						stat.Mtime = fmt.Sprint(info.ModTime().UnixNano())
					}
				}
			} else {
				message = "원본 파일을 찾을 수 없습니다"
				path = f.path
				sourceKey = f.path
			}
			if message != "" {
				state = "failed"
			}
			raw, _ := json.Marshal(stat)
			if _, e = tx.ExecContext(ctx, `INSERT INTO file_move_item(id,job_id,media_id,file_id,media_id_snapshot,file_uuid_snapshot,source_path,source_key,expected_stat_json,state,commit_token,error_message)VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, uuid.NewString(), job, id, f.id, id, f.uuid, f.path, sourceKey, string(raw), state, uuid.NewString(), message); e != nil {
				return failure(e)
			}
		}
	}
	if count == 0 {
		return api.Failure("INVALID_ARGUMENT", "선택한 게시글에 이동할 파일이 없습니다")
	}
	if e = tx.Commit(); e != nil {
		return failure(e)
	}
	response := map[string]string{"jobId": job}
	if albumID != 0 {
		response["albumId"] = fmt.Sprint(albumID)
	}
	return api.Success(response)
}
func (s *Service) Get(ctx context.Context, req Request) api.Result {
	if req.Offset < 0 {
		return api.Failure("INVALID_ARGUMENT", "페이지를 확인하세요")
	}
	var v Status
	v.Items = []Item{}
	v.Offset = req.Offset
	query := "SELECT id,target_directory_snapshot,state,operation FROM file_move_job WHERE id=?"
	args := []any{req.JobID}
	if req.JobID == "" {
		query = "SELECT id,target_directory_snapshot,state,operation FROM file_move_job WHERE EXISTS(SELECT 1 FROM file_move_item WHERE job_id=file_move_job.id AND json_extract(expected_stat_json,'$.version')=1) ORDER BY created_at DESC,rowid DESC LIMIT 1"
		args = nil
	}
	if e := s.db.QueryRowContext(ctx, query, args...).Scan(&v.JobID, &v.Directory, &v.State, &v.Operation); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return api.Success(nil)
		}
		return failure(e)
	}
	e := s.db.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(state IN ('completed','excluded')),0),coalesce(sum(state='failed'),0),coalesce(sum(state IN ('committed','cleanup_pending')),0),coalesce(sum(json_extract(expected_stat_json,'$.size')),0),coalesce(sum(CASE WHEN state IN ('completed','excluded','committed','cleanup_pending') THEN json_extract(expected_stat_json,'$.size') ELSE 0 END),0) FROM file_move_item WHERE job_id=?`, v.JobID).Scan(&v.Total, &v.Completed, &v.Failed, &v.CleanupPending, &v.TotalBytes, &v.ProcessedBytes)
	if e != nil {
		return failure(e)
	}
	if e = s.db.QueryRowContext(ctx, "SELECT coalesce(max(json_extract(expected_stat_json,'$.albumId')),0) FROM file_move_item WHERE job_id=?", v.JobID).Scan(&v.AlbumID); e != nil {
		return failure(e)
	}
	rows, e := s.db.QueryContext(ctx, "SELECT id,media_id_snapshot,coalesce(file_id,0),source_path,coalesce(target_path,''),state,coalesce(error_message,'') FROM file_move_item WHERE job_id=? ORDER BY rowid LIMIT 100 OFFSET ?", v.JobID, req.Offset)
	if e != nil {
		return failure(e)
	}
	for rows.Next() {
		var item Item
		if e = rows.Scan(&item.ID, &item.MediaID, &item.FileID, &item.Source, &item.Target, &item.State, &item.Error); e != nil {
			break
		}
		v.Items = append(v.Items, item)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return failure(e)
	}
	v.HasMore = int64(req.Offset+len(v.Items)) < v.Total
	s.mu.Lock()
	if s.active == v.JobID {
		v.Current = s.current
		v.ProcessedBytes += s.copied
		if v.ProcessedBytes > v.TotalBytes {
			v.ProcessedBytes = v.TotalBytes
		}
	} else if v.State == "running" || v.State == "pausing" {
		// The worker can finish between the first DB read and this lock.
		// Re-read its final state before reporting an interrupted job.
		if err := s.db.QueryRowContext(ctx, "SELECT state FROM file_move_job WHERE id=?", v.JobID).Scan(&v.State); err != nil {
			s.mu.Unlock()
			return failure(err)
		}
		if v.State == "running" || v.State == "pausing" {
			v.State = "interrupted"
		}
	}
	s.mu.Unlock()
	return api.Success(v)
}
func (s *Service) Run(ctx context.Context, req Request) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != "" {
		return api.Failure("JOB_ACTIVE", "파일 이동이 실행 중입니다")
	}
	var state string
	if e := s.db.QueryRowContext(ctx, "SELECT state FROM file_move_job WHERE id=? AND EXISTS(SELECT 1 FROM file_move_item WHERE job_id=file_move_job.id AND json_extract(expected_stat_json,'$.version')=1)", req.JobID).Scan(&state); e != nil {
		return failure(e)
	}
	if state == "completed" {
		return api.Failure("INVALID_ARGUMENT", "이미 완료된 작업입니다")
	}
	work, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	s.active = req.JobID
	if _, e := s.db.ExecContext(ctx, "UPDATE file_move_job SET state='running',pause_requested_at=NULL,updated_at=? WHERE id=?", time.Now().UnixMilli(), req.JobID); e != nil {
		cancel()
		s.active = ""
		return failure(e)
	}
	go s.run(work, req.JobID, s.done)
	return api.Success(map[string]string{"jobId": req.JobID})
}
func (s *Service) Pause(ctx context.Context, req Request) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != req.JobID {
		return api.Failure("INVALID_ARGUMENT", "진행 중인 작업을 선택하세요")
	}
	if _, e := s.db.ExecContext(ctx, "UPDATE file_move_job SET state='pausing',pause_requested_at=?,updated_at=? WHERE id=?", time.Now().UnixMilli(), time.Now().UnixMilli(), req.JobID); e != nil {
		return failure(e)
	}
	s.cancel()
	return api.Success(map[string]string{"jobId": req.JobID})
}
func (s *Service) Retry(ctx context.Context, req Request) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	s.mu.Lock()
	if s.closed || s.active != "" {
		s.mu.Unlock()
		return api.Failure("JOB_ACTIVE", "중단 후 재시도하세요")
	}
	var supported int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM file_move_item WHERE job_id=? AND json_extract(expected_stat_json,'$.version')=1", req.JobID).Scan(&supported); err != nil || supported == 0 {
		s.mu.Unlock()
		return api.Failure("INVALID_ARGUMENT", "이전 작업 이력은 재시도할 수 없습니다")
	}
	// Recover partial outputs before a failed item can be reserved again.
	if _, e := s.db.ExecContext(ctx, "UPDATE file_move_item SET state='pending',error_code=NULL,error_message=NULL WHERE job_id=? AND state='failed'", req.JobID); e != nil {
		s.mu.Unlock()
		return failure(e)
	}
	s.mu.Unlock()
	return s.Run(ctx, req)
}

//wails:ignore
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	cancel, done := s.cancel, s.done
	if s.active != "" && cancel != nil {
		cancel()
	} else {
		done = nil
	}
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}

// Recover restores only jobs created by this executor. Imported history is inert.
//
//wails:ignore
func (s *Service) Recover(ctx context.Context) error {
	_, e := s.db.ExecContext(ctx, `UPDATE file_move_job SET state='interrupted',updated_at=? WHERE state IN ('running','pausing') AND EXISTS(SELECT 1 FROM file_move_item WHERE job_id=file_move_job.id AND json_extract(expected_stat_json,'$.version')=1)`, time.Now().UnixMilli())
	return e
}
