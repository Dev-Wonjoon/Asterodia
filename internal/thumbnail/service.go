package thumbnail

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

type Request struct {
	MediaID int64 `json:"mediaId"`
	FileID  int64 `json:"fileId"`
}
type Status struct {
	State    string  `json:"state"`
	TaskID   *string `json:"taskId"`
	CanRetry bool    `json:"canRetry"`
}
type Service struct {
	preview       func(context.Context, string, string) (json.RawMessage, error)
	previewMu     sync.Mutex
	db            *sql.DB
	cache, ffmpeg string
	notify        func(int64)
	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	wake          chan struct{}
	wg            sync.WaitGroup
	closed        bool
}

func NewService(db *sql.DB, cache, ffmpeg string, notify func(int64)) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{db: db, cache: cache, ffmpeg: ffmpeg, notify: notify, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1)}
	return s
}

//wails:ignore
func (s *Service) Start(ctx context.Context) error {
	// Interrupted generation never reuses a partial image.
	if _, err := s.db.ExecContext(ctx, "UPDATE maintenance_task SET state='pending' WHERE kind='preview_generation' AND state='running'"); err != nil {
		return err
	}
	s.wg.Add(1)
	go s.worker()
	s.signal()
	return nil
}

//wails:ignore
func (s *Service) Close() { s.mu.Lock(); s.closed = true; s.cancel(); s.mu.Unlock(); s.wg.Wait() }
func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Service) Get(ctx context.Context, req Request) api.Result {
	if req.MediaID <= 0 || req.FileID <= 0 {
		return api.Failure("INVALID_ARGUMENT", "묶음과 파일 ID가 필요합니다")
	}
	var kind, path string
	var thumb *string
	if err := s.db.QueryRowContext(ctx, "SELECT kind,filepath,thumbnail_path FROM media_file WHERE id=? AND media_id=?", req.FileID, req.MediaID).Scan(&kind, &path, &thumb); err != nil {
		if err == sql.ErrNoRows {
			return api.Failure("NOT_FOUND", "파일 등록을 찾을 수 없습니다")
		}
		return api.Failure("INTERNAL_ERROR", "썸네일 상태를 읽지 못했습니다")
	}
	if thumb != nil {
		if info, err := os.Stat(*thumb); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return api.Success(Status{State: "registered"})
		}
	}
	if kind != "image" && kind != "video" {
		return api.Success(Status{State: "unsupported"})
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return api.Success(Status{State: "missing"})
	}
	var id, state string
	err := s.db.QueryRowContext(ctx, `SELECT id,state FROM maintenance_task WHERE kind='preview_generation' AND json_extract(payload_json,'$.fileId')=? AND json_extract(payload_json,'$.mediaId')=? ORDER BY created_at DESC,rowid DESC LIMIT 1`, req.FileID, req.MediaID).Scan(&id, &state)
	if err != nil && err != sql.ErrNoRows {
		return api.Failure("INTERNAL_ERROR", "썸네일 작업을 읽지 못했습니다")
	}
	if err == nil && (state == "pending" || state == "running") {
		return api.Success(Status{State: state, TaskID: &id})
	}
	if err == nil && state == "failed" {
		return api.Success(Status{State: "failed", TaskID: &id, CanRetry: true})
	}
	return api.Success(Status{State: "missing", CanRetry: true})
}
func (s *Service) Retry(ctx context.Context, req Request) api.Result {
	status := s.Get(ctx, req)
	if !status.OK {
		return status
	}
	current := status.Data.(Status)
	if !current.CanRetry {
		if current.State == "pending" || current.State == "running" || current.State == "registered" {
			return status
		}
		return api.Failure("NOT_RETRYABLE", "이 파일은 썸네일을 생성할 수 없습니다")
	}
	if err := s.schedule(ctx, req); err != nil {
		return api.Failure("INTERNAL_ERROR", err.Error())
	}
	return s.Get(ctx, req)
}
func (s *Service) schedule(ctx context.Context, req Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("앱을 종료하고 있습니다")
	}
	payload, _ := json.Marshal(req)
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT INTO maintenance_task(id,kind,state,owner_kind,payload_json,created_at,updated_at) SELECT lower(hex(randomblob(16))),'preview_generation','pending','system',?,?,? WHERE NOT EXISTS(SELECT 1 FROM maintenance_task WHERE kind='preview_generation' AND state IN ('pending','running') AND json_extract(payload_json,'$.fileId')=? AND json_extract(payload_json,'$.mediaId')=?)`, string(payload), now, now, req.FileID, req.MediaID)
	if err == nil {
		s.signal()
	}
	return err
}

//wails:ignore
func (s *Service) ScheduleMedia(mediaID int64) {
	rows, err := s.db.Query("SELECT id FROM media_file WHERE media_id=? AND kind IN ('image','video') AND thumbnail_path IS NULL", mediaID)
	if err != nil {
		return
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		_ = s.schedule(s.ctx, Request{mediaID, id})
	}
}
func (s *Service) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		}
		for s.ctx.Err() == nil {
			var id, payload string
			err := s.db.QueryRowContext(s.ctx, "SELECT id,payload_json FROM maintenance_task WHERE kind='preview_generation' AND state='pending' ORDER BY created_at,rowid LIMIT 1").Scan(&id, &payload)
			if err != nil {
				break
			}
			if _, err = s.db.ExecContext(s.ctx, "UPDATE maintenance_task SET state='running',try_count=try_count+1,updated_at=? WHERE id=?", time.Now().UnixMilli(), id); err != nil {
				break
			}
			var req Request
			err = json.Unmarshal([]byte(payload), &req)
			if err == nil {
				err = s.generate(s.ctx, req)
			}
			state := "completed"
			var message any
			if err != nil {
				state = "failed"
				message = err.Error()
			}
			_, _ = s.db.ExecContext(context.Background(), "UPDATE maintenance_task SET state=?,last_error=?,updated_at=? WHERE id=?", state, message, time.Now().UnixMilli(), id)
			if s.notify != nil {
				s.notify(req.MediaID)
			}
		}
	}
}
func sourceHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
func (s *Service) generate(ctx context.Context, req Request) error {
	var path, kind string
	var hash *string
	if err := s.db.QueryRowContext(ctx, "SELECT filepath,kind,content_hash FROM media_file WHERE id=? AND media_id=?", req.FileID, req.MediaID).Scan(&path, &kind, &hash); err != nil {
		return err
	}
	before, err := os.Stat(path)
	if err != nil {
		return err
	}
	fingerprint, err := sourceHash(path)
	if err != nil {
		return err
	}
	if hash != nil && fingerprint != *hash {
		return fmt.Errorf("원본 파일이 변경되었습니다")
	}
	if err = os.MkdirAll(s.cache, 0700); err != nil {
		return err
	}
	key := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:jpeg-v1", req.FileID, fingerprint)))
	dest := filepath.Join(s.cache, fmt.Sprintf("%x.jpg", key))
	temp, err := os.CreateTemp(s.cache, "thumbnail-*.jpg")
	if err != nil {
		return err
	}
	tmp := temp.Name()
	temp.Close()
	defer os.Remove(tmp)
	switch kind {
	case "image":
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		cfg, _, e := image.DecodeConfig(f)
		if e != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
			f.Close()
			return fmt.Errorf("썸네일 이미지의 크기 또는 형식을 지원하지 않습니다")
		}
		if _, e = f.Seek(0, 0); e != nil {
			f.Close()
			return e
		}
		img, _, e := image.Decode(f)
		f.Close()
		if e != nil {
			return e
		}
		width, height := cfg.Width, cfg.Height
		if width > 480 || height > 480 {
			if width >= height {
				height = height * 480 / width
				width = 480
			} else {
				width = width * 480 / height
				height = 480
			}
		}
		if width < 1 {
			width = 1
		}
		if height < 1 {
			height = 1
		}
		small := image.NewRGBA(image.Rect(0, 0, width, height))
		bounds := img.Bounds()
		for y := 0; y < height; y++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			for x := 0; x < width; x++ {
				small.Set(x, y, img.At(bounds.Min.X+x*cfg.Width/width, bounds.Min.Y+y*cfg.Height/height))
			}
		}
		out, e := os.OpenFile(tmp, os.O_WRONLY|os.O_TRUNC, 0600)
		if e != nil {
			return e
		}
		e = jpeg.Encode(out, small, &jpeg.Options{Quality: 85})
		if e == nil {
			e = out.Sync()
		}
		closeErr := out.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	case "video":
		_, err = new(engine.ProcessRunner).Run(ctx, engine.ProcessRequest{Executable: s.ffmpeg, Directory: s.cache, Timeout: time.Minute, Arguments: []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", path, "-frames:v", "1", "-vf", "scale=480:480:force_original_aspect_ratio=decrease", "-update", "1", tmp}})
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("썸네일을 지원하지 않는 파일입니다")
	}
	after, err := os.Stat(path)
	if err != nil {
		return err
	}
	current, err := sourceHash(path)
	if err != nil {
		return err
	}
	if before.Size() != after.Size() || before.ModTime() != after.ModTime() || fingerprint != current {
		return fmt.Errorf("썸네일 생성 중 원본이 변경되었습니다")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err = os.Rename(tmp, dest); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, "UPDATE media_file SET thumbnail_path=?,thumbnail_origin='app_generated' WHERE id=? AND media_id=? AND filepath=? AND content_hash IS ?", dest, req.FileID, req.MediaID, path, hash)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("등록된 원본 정보가 변경되었습니다")
	}
	return nil
}
