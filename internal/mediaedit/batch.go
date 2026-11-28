package mediaedit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"github.com/google/uuid"
)

const maxBatchFiles = 500

var errBatchStart = errors.New("시작 시간이 원본 재생 시간을 초과합니다. 시작 시간을 줄여 다시 등록하세요.")
var errBatchNoAudio = errors.New("원본에 오디오 트랙이 없습니다. 다른 파일이나 영상 출력을 선택하세요.")

type BatchSelection struct {
	MediaID  int64   `json:"mediaId"`
	Revision int64   `json:"revision"`
	FileIDs  []int64 `json:"fileIds,omitempty"`
}
type BatchRequest struct {
	RequestID       string                  `json:"requestId"`
	Selection       []BatchSelection        `json:"selection"`
	Mode            string                  `json:"mode"`
	Format          engine.FormatOptions    `json:"format"`
	Start           float64                 `json:"start"`
	Duration        float64                 `json:"duration"`
	Width           int                     `json:"width"`
	FPS             int                     `json:"fps"`
	Loop            int                     `json:"loop"`
	Quality         engine.AnimationQuality `json:"quality"`
	OutputDirectory string                  `json:"outputDirectory,omitempty"`
}
type BatchOptions struct {
	ID          string               `json:"id"`
	RequestHash string               `json:"requestHash"`
	MediaID     int64                `json:"mediaId"`
	FileID      int64                `json:"fileId"`
	Title       string               `json:"title"`
	Mode        string               `json:"mode"`
	Format      engine.FormatOptions `json:"format"`
	Start       float64              `json:"start"`
	Duration    float64              `json:"duration"`
	Width       int                  `json:"width"`
	FPS         int                  `json:"fps"`
	Loop        int                  `json:"loop"`
}
type BatchFailure struct {
	MediaID int64  `json:"mediaId"`
	FileID  int64  `json:"fileId"`
	Message string `json:"message"`
}
type BatchResult struct {
	Jobs     []Job          `json:"jobs"`
	Failures []BatchFailure `json:"failures"`
}

func (s *Service) Formats(ctx context.Context) api.Result {
	formats, err := s.editor.AvailableFormats(ctx)
	if err != nil {
		return api.Failure("NOT_READY", "설정에서 FFmpeg를 설치하거나 설치 상태를 확인하세요")
	}
	return api.Success(formats)
}

// Batch snapshots sources and queues durable jobs; encoding never runs in this request.
func (s *Service) Batch(ctx context.Context, req BatchRequest) api.Result {
	leave, allowed := s.UpdateGate.Enter()
	if !allowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leave()
	if !validID(req.RequestID) || len(req.Selection) == 0 || len(req.Selection) > maxBatchFiles || (req.Mode != "convert" && req.Mode != "webp") {
		return invalid("일괄 변환 대상을 최대 500개까지 선택하세요")
	}
	output := CreateRequest{OutputDirectory: req.OutputDirectory}
	if err := normalizeOutput(&output); err != nil {
		return invalid(err.Error())
	}
	req.OutputDirectory = output.OutputDirectory
	if req.Mode == "convert" {
		var err error
		req.Format, err = engine.NormalizeFormat(req.Format)
		if err != nil {
			return invalid(err.Error())
		}
		if err = s.editor.CheckFormat(ctx, req.Format); err != nil {
			return invalid(err.Error())
		}
	} else {
		if math.IsNaN(req.Start) || math.IsInf(req.Start, 0) || req.Start < 0 || math.IsNaN(req.Duration) || math.IsInf(req.Duration, 0) || req.Duration <= 0 || req.Duration > 30 || req.Width < 64 || req.Width > 1920 || req.FPS < 1 || req.FPS > 30 || (req.Loop != 0 && req.Loop != 1) {
			return invalid("WebP는 최대 30초, 64–1920px, 1–30FPS로 지정하세요")
		}
		var err error
		req.Quality, err = engine.NormalizeAnimationQuality("webp", req.Quality)
		if err != nil {
			return invalid(err.Error())
		}
		formats, err := s.editor.AvailableFormats(ctx)
		if err != nil {
			return fail(err)
		}
		supported := false
		for _, f := range formats {
			if f.Type == "image" && f.Extension == "webp" {
				supported = true
			}
		}
		if !supported {
			return invalid("설치된 FFmpeg가 애니메이션 WebP를 지원하지 않습니다")
		}
	}
	merged := map[int64]BatchSelection{}
	for _, selected := range req.Selection {
		if selected.MediaID <= 0 || selected.Revision < 0 || len(selected.FileIDs) > maxBatchFiles {
			return invalid("선택한 파일과 변경 정보를 확인하세요")
		}
		for _, id := range selected.FileIDs {
			if id <= 0 {
				return invalid("선택한 파일과 변경 정보를 확인하세요")
			}
		}
		previous, exists := merged[selected.MediaID]
		if exists && previous.Revision != selected.Revision {
			return invalid("선택한 항목의 변경 정보가 다릅니다. 새로고침 후 다시 선택하세요")
		}
		if exists {
			if len(previous.FileIDs) == 0 || len(selected.FileIDs) == 0 {
				previous.FileIDs = nil
			} else {
				previous.FileIDs = append(previous.FileIDs, selected.FileIDs...)
			}
			selected = previous
		} else {
			selected.FileIDs = append([]int64(nil), selected.FileIDs...)
		}
		merged[selected.MediaID] = selected
	}
	req.Selection = nil
	for _, selected := range merged {
		ids := map[int64]bool{}
		for _, id := range selected.FileIDs {
			ids[id] = true
		}
		selected.FileIDs = nil
		for id := range ids {
			selected.FileIDs = append(selected.FileIDs, id)
		}
		sort.Slice(selected.FileIDs, func(a, b int) bool { return selected.FileIDs[a] < selected.FileIDs[b] })
		req.Selection = append(req.Selection, selected)
	}
	sort.Slice(req.Selection, func(a, b int) bool { return req.Selection[a].MediaID < req.Selection[b].MediaID })
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(raw(req))))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.halted || s.done == nil {
		return api.Failure("NOT_READY", "변환 서비스를 사용할 준비가 되지 않았습니다")
	}
	result := BatchResult{Jobs: []Job{}, Failures: []BatchFailure{}}
	ids, err := s.store.ids(ctx, "media_edit_job", "WHERE json_extract(request_json,'$.batch.id')=? ORDER BY created_at,rowid", req.RequestID)
	if err != nil {
		return fail(err)
	}
	for _, id := range ids {
		job, e := s.store.job(ctx, id)
		if e != nil {
			return fail(e)
		}
		if job.Options.Batch.RequestHash != fingerprint {
			return invalid("같은 요청 식별자로 다른 변환을 등록할 수 없습니다")
		}
		result.Jobs = append(result.Jobs, job)
	}
	if len(ids) > 0 {
		return api.Success(result)
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	var queued int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM media_edit_job WHERE state IN ('pending','running','verifying','publishing','stop_unconfirmed')").Scan(&queued); err != nil {
		return fail(err)
	}
	root := req.OutputDirectory
	if root == "" {
		root = s.output()
	}
	if !filepath.IsAbs(root) {
		return invalid("PC의 저장 폴더를 확인하세요")
	}
	seen := map[int64]bool{}
	checked := map[int64]error{}
	for _, selected := range req.Selection {
		checked[selected.MediaID] = media.CheckWrite(ctx, tx, selected.MediaID, selected.Revision)
	}
	for _, selected := range req.Selection {
		if checked[selected.MediaID] != nil {
			result.Failures = append(result.Failures, BatchFailure{MediaID: selected.MediaID, Message: "원본이 변경되었거나 다른 작업에서 사용 중입니다. 새로고침 후 다시 선택하세요."})
			continue
		}
		query := `SELECT DISTINCT f.id,f.filepath,f.file_uuid,f.kind,m.id,m.revision,coalesce(nullif(f.title,''),nullif(m.custom_title,''),nullif(m.source_title,''),'미디어') FROM media_file f JOIN media m ON m.id=f.media_id WHERE (f.media_id=? OR EXISTS(SELECT 1 FROM user_album_file a WHERE a.album_id=? AND a.file_id=f.id))`
		args := []any{selected.MediaID, selected.MediaID}
		if len(selected.FileIDs) > 0 {
			placeholders := make([]string, len(selected.FileIDs))
			for i, id := range selected.FileIDs {
				placeholders[i] = "?"
				args = append(args, id)
			}
			query += " AND f.id IN (" + strings.Join(placeholders, ",") + ")"
		}
		query += " ORDER BY f.id LIMIT 501"
		rows, e := tx.QueryContext(ctx, query, args...)
		if e != nil {
			return fail(e)
		}
		type source struct {
			id, owner, revision     int64
			path, uuid, kind, title string
		}
		sources := []source{}
		for rows.Next() {
			var f source
			if e = rows.Scan(&f.id, &f.path, &f.uuid, &f.kind, &f.owner, &f.revision, &f.title); e != nil {
				rows.Close()
				return fail(e)
			}
			sources = append(sources, f)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return fail(e)
		}
		wanted := map[int64]bool{}
		for _, id := range selected.FileIDs {
			wanted[id] = true
		}
		for _, f := range sources {
			if len(selected.FileIDs) > 0 && !wanted[f.id] {
				continue
			}
			delete(wanted, f.id)
			if seen[f.id] {
				continue
			}
			seen[f.id] = true
			if len(seen) > maxBatchFiles || queued >= 1000 {
				return api.Failure("LIMIT_EXCEEDED", "한 번에 최대 500개, 대기 작업 최대 1000개까지 등록할 수 있습니다")
			}
			failure := func(message string) {
				result.Failures = append(result.Failures, BatchFailure{MediaID: selected.MediaID, FileID: f.id, Message: message})
			}
			if _, ok := checked[f.owner]; !ok {
				checked[f.owner] = media.CheckWrite(ctx, tx, f.owner, f.revision)
			}
			if checked[f.owner] != nil {
				failure("원본이 변경되었거나 다른 작업에서 사용 중입니다. 새로고침 후 다시 선택하세요.")
				continue
			}
			if (req.Mode == "webp" && f.kind != "video") || (req.Mode == "convert" && (f.kind != "video" && f.kind != "audio" || req.Format.Type == "video" && f.kind != "video")) {
				failure("선택한 출력으로 변환할 수 없는 파일 종류입니다")
				continue
			}
			if !filepath.IsAbs(f.path) {
				failure("원본 파일 경로를 확인하세요")
				continue
			}
			pin, e := openBatchSource(f.path)
			if e != nil {
				failure("원본 파일을 열 수 없습니다")
				continue
			}
			identity, e := media.FileIdentity(pin)
			info, se := pin.Stat()
			pin.Close()
			if e != nil || se != nil {
				failure("원본 파일 정보를 확인할 수 없습니다")
				continue
			}
			sessionID, jobID := uuid.NewString(), uuid.NewString()
			prepare := PrepareRequest{Batch: true, RequestID: sessionID, FileID: f.id, Kind: "trim", QuickVideo: true, FullSource: true, FPS: 1000}
			batch := &BatchOptions{ID: req.RequestID, RequestHash: fingerprint, MediaID: selected.MediaID, FileID: f.id, Title: f.title, Mode: req.Mode, Format: req.Format, Start: req.Start, Duration: req.Duration, Width: req.Width, FPS: req.FPS, Loop: req.Loop}
			create := CreateRequest{RequestID: jobID, SessionID: sessionID, Kind: "convert", OutputDirectory: req.OutputDirectory, Batch: batch, Quality: req.Quality}
			if req.Mode == "webp" {
				create.Kind = "webp"
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO media_edit_session(id,source_file_id,source_uuid,source_path,source_title,source_identity,source_size,source_mtime,request_json,state,progress,created_at,updated_at,render_version) VALUES(?,?,?,?,?,?,?,?,?,'ready',100,?,?,2)`, sessionID, f.id, f.uuid, f.path, f.title, identity, info.Size(), fmt.Sprint(info.ModTime().UnixNano()), raw(prepare), now(), now()); e != nil {
				return fail(e)
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO media_edit_job(id,session_id,request_json,state,output_root,result_uuid,created_at,updated_at) VALUES(?,?,?,'pending',?,?,?,?)`, jobID, sessionID, raw(create), root, uuid.NewString(), now(), now()); e != nil {
				return fail(e)
			}
			result.Jobs = append(result.Jobs, Job{ID: jobID, SessionID: sessionID, Options: create, State: "pending", CreatedAt: now()})
			queued++
		}
		for id := range wanted {
			result.Failures = append(result.Failures, BatchFailure{MediaID: selected.MediaID, FileID: id, Message: "선택한 파일이 이동되었거나 삭제되었습니다"})
		}
		if len(sources) == 0 {
			result.Failures = append(result.Failures, BatchFailure{MediaID: selected.MediaID, Message: "변환할 파일이 없습니다"})
		}
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	s.signal()
	return api.Success(result)
}

func (s *Service) RetryBatch(ctx context.Context, req Request) api.Result {
	leave, allowed := s.UpdateGate.Enter()
	if !allowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leave()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validID(req.ID) {
		return invalid("작업 ID를 확인하세요")
	}
	if s.closed || s.halted || s.done == nil {
		return api.Failure("NOT_READY", "변환 서비스를 사용할 준비가 되지 않았습니다")
	}
	j, err := s.store.job(ctx, req.ID)
	if err != nil {
		return fail(err)
	}
	if j.Options.Batch == nil || (j.State != "failed" && j.State != "cancelled" && j.State != "interrupted") {
		return invalid("중단되거나 실패한 일괄 변환 작업을 선택하세요")
	}
	var proof sql.NullString
	var count int
	if err = s.store.db.QueryRowContext(ctx, "SELECT proof_json FROM media_edit_job WHERE id=?", j.ID).Scan(&proof); err != nil {
		return fail(err)
	}
	if proof.Valid {
		return invalid("저장 확정 중단 작업은 앱을 다시 시작해 결과를 확인하세요")
	}
	if err = s.store.db.QueryRowContext(ctx, "SELECT count(*) FROM media_edit_job WHERE state IN ('pending','running','verifying','publishing','stop_unconfirmed')").Scan(&count); err != nil {
		return fail(err)
	}
	if count >= 1000 {
		return api.Failure("LIMIT_EXCEEDED", "대기 작업이 가득 찼습니다")
	}
	var active int
	if err = s.store.db.QueryRowContext(ctx, `SELECT count(*) FROM media_edit_job WHERE json_extract(request_json,'$.batch.id')=? AND json_extract(request_json,'$.batch.fileId')=? AND state IN ('pending','running','verifying','publishing','stop_unconfirmed')`, j.Options.Batch.ID, j.Options.Batch.FileID).Scan(&active); err != nil {
		return fail(err)
	}
	if active > 0 {
		return invalid("이미 재시도 중인 파일입니다")
	}
	pin, _, err := s.source(ctx, j.SessionID)
	if err != nil {
		return invalid("원본이 변경되었거나 없어졌습니다. 새로 선택해 변환하세요")
	}
	pin.Close()
	var root string
	if err = s.store.db.QueryRowContext(ctx, "SELECT output_root FROM media_edit_job WHERE id=?", j.ID).Scan(&root); err != nil {
		return fail(err)
	}
	j.Options.RequestID = uuid.NewString()
	_, err = s.store.db.ExecContext(ctx, `INSERT INTO media_edit_job(id,session_id,request_json,state,output_root,result_uuid,created_at,updated_at) VALUES(?,?,?,'pending',?,?,?,?)`, j.Options.RequestID, j.SessionID, raw(j.Options), root, uuid.NewString(), now(), now())
	if err != nil {
		return fail(err)
	}
	s.signal()
	result, err := s.store.job(ctx, j.Options.RequestID)
	if err != nil {
		return fail(err)
	}
	return api.Success(result)
}

func (s *Service) CancelBatch(ctx context.Context, req Request) api.Result {
	leave, allowed := s.UpdateGate.Enter()
	if !allowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leave()
	if !validID(req.ID) {
		return invalid("일괄 작업 ID를 확인하세요")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.store.db.ExecContext(ctx, `UPDATE media_edit_job SET state='cancelled',updated_at=? WHERE json_extract(request_json,'$.batch.id')=? AND state='pending'`, now(), req.ID)
	if err != nil {
		return fail(err)
	}
	if s.activeID != "" {
		j, e := s.store.job(ctx, s.activeID)
		if e == nil && j.Options.Batch != nil && j.Options.Batch.ID == req.ID && (j.State == "running" || j.State == "verifying") {
			_, err = s.store.db.ExecContext(ctx, "UPDATE media_edit_job SET cancel_requested=1 WHERE id=?", j.ID)
			if err != nil {
				return fail(err)
			}
			s.activeCancel()
		}
	}
	return api.Success(nil)
}

func (s *Service) renderBatch(ctx context.Context, j Job, v Session, destination string) (float64, error) {
	b := j.Options.Batch
	pin, path, err := s.source(ctx, v.ID)
	if err != nil {
		return 0, err
	}
	defer pin.Close()
	progress := s.progress("media_edit_job", j.ID, 0, 88)
	if b.Mode == "webp" {
		meta, err := s.editor.ProbeVideo(ctx, path)
		if err != nil {
			return 0, err
		}
		if meta.Duration == nil || b.Start >= *meta.Duration {
			return 0, errBatchStart
		}
		duration := min(b.Duration, *meta.Duration-b.Start)
		return duration, s.editor.QuickWebP(ctx, path, destination, b.Start, duration, b.Width, b.FPS, b.Loop, j.Options.Quality, progress)
	}
	var kind string
	if err = s.store.db.QueryRowContext(ctx, "SELECT kind FROM media_file WHERE id=?", b.FileID).Scan(&kind); err != nil {
		return 0, err
	}
	if kind != "audio" && kind != "video" {
		return 0, fmt.Errorf("지원하지 않는 원본 형식입니다")
	}
	meta, err := (engine.LocalInspector{FFprobe: s.editor.FFprobe, FFmpeg: s.editor.FFmpeg}).Inspect(ctx, path, kind)
	if err != nil {
		return 0, err
	}
	duration := 0.0
	if meta.Duration != nil {
		duration = *meta.Duration
	}
	err = s.editor.Convert(ctx, path, destination, b.Format, duration, progress)
	if err != nil && b.Format.Type == "audio" && strings.Contains(err.Error(), "matches no streams") {
		return 0, errBatchNoAudio
	}
	return duration, err
}

func (s *Service) verifyOutput(ctx context.Context, j Job, path string) (engine.LocalMetadata, int, error) {
	if j.Options.Batch != nil && j.Options.Batch.Mode == "convert" {
		meta, err := (engine.LocalInspector{FFprobe: s.editor.FFprobe, FFmpeg: s.editor.FFmpeg}).Inspect(ctx, path, j.Options.Batch.Format.Type)
		meta.Format = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		return meta, 0, err
	}
	return s.editor.Verify(ctx, path)
}

// Open the source with the same sharing policy as editor sessions.
func openBatchSource(path string) (*os.File, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return media.OpenReadFile(root, filepath.Base(path))
}

func outputFPS(j Job, v Session) int {
	if j.Options.Batch != nil {
		return j.Options.Batch.FPS
	}
	return v.Options.FPS
}
