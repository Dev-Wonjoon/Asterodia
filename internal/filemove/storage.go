package filemove

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

type workItem struct {
	metadataMissing                                                                   bool
	row                                                                               int64
	id, source, sourceKey, target, state, raw, verification, uuid, title, token, kind string
	mediaID, fileID, directoryID                                                      int64
	directory                                                                         string
	operation                                                                         string
}

func (s *Service) run(ctx context.Context, job string, done chan struct{}) {
	defer func() {
		s.mu.Lock()
		s.active = ""
		s.current = ""
		s.copied = 0
		s.cancel = nil
		s.mu.Unlock()
		close(done)
	}()
	var after int64
	for ctx.Err() == nil {
		var w workItem
		e := s.db.QueryRowContext(ctx, `SELECT i.rowid,i.id,i.source_path,i.source_key,coalesce(i.target_path,''),i.state,i.expected_stat_json,coalesce(i.verification_json,''),i.file_uuid_snapshot,coalesce(f.title,''),coalesce(f.filename_token,''),coalesce(f.kind,''),(f.detected_format IS NULL OR f.kind NOT IN ('image','video','audio') OR (f.kind IN ('image','video') AND (f.width IS NULL OR f.height IS NULL)) OR (f.kind IN ('video','audio') AND f.duration IS NULL)),i.media_id_snapshot,coalesce(i.file_id,0),j.target_directory_id,j.target_directory_snapshot,j.operation FROM file_move_item i JOIN file_move_job j ON j.id=i.job_id LEFT JOIN media_file f ON f.id=i.file_id WHERE i.job_id=? AND i.rowid>? AND i.state IN ('pending','copying','verified','committed','cleanup_pending') ORDER BY i.rowid LIMIT 1`, job, after).Scan(&w.row, &w.id, &w.source, &w.sourceKey, &w.target, &w.state, &w.raw, &w.verification, &w.uuid, &w.title, &w.token, &w.kind, &w.metadataMissing, &w.mediaID, &w.fileID, &w.directoryID, &w.directory, &w.operation)
		if errors.Is(e, sql.ErrNoRows) {
			break
		}
		if e != nil {
			break
		}
		after = w.row
		s.mu.Lock()
		s.current = w.source
		s.copied = 0
		s.mu.Unlock()
		e = s.process(ctx, w)
		if e != nil {
			state := "failed"
			if w.state == "committed" || w.state == "cleanup_pending" {
				state = "cleanup_pending"
			}
			// A cancellation leaves a retryable checkpoint, preserving already committed files.
			if errors.Is(e, context.Canceled) && state == "failed" {
				state = "pending"
			}
			s.db.ExecContext(context.Background(), "UPDATE file_move_item SET state=?,error_code='FILE_MOVE_FAILED',error_message=? WHERE id=? AND committed_at IS NULL", state, e.Error(), w.id)
			s.db.ExecContext(context.Background(), "UPDATE file_move_item SET state='cleanup_pending',error_code='CLEANUP_PENDING',error_message=? WHERE id=? AND committed_at IS NOT NULL AND state<>'completed'", e.Error(), w.id)
		}
	}
	state := "completed"
	if ctx.Err() != nil {
		state = "paused"
	} else {
		var unresolved int
		if e := s.db.QueryRow("SELECT count(*) FROM file_move_item WHERE job_id=? AND state NOT IN ('completed','excluded')", job).Scan(&unresolved); e != nil || unresolved > 0 {
			state = "completed_with_errors"
		}
	}
	s.db.Exec("UPDATE file_move_job SET state=?,updated_at=?,revision=revision+1 WHERE id=?", state, time.Now().UnixMilli(), job)
}
func openRoot(path string, identity string) (*os.Root, func(), error) {
	lock, e := media.LockDirectory(path)
	if e != nil {
		return nil, nil, e
	}
	root, e := os.OpenRoot(path)
	if e != nil {
		lock.Close()
		return nil, nil, e
	}
	f, e := root.Open(".")
	if e != nil {
		root.Close()
		lock.Close()
		return nil, nil, e
	}
	actual, e := media.FileIdentity(f)
	f.Close()
	if e != nil || actual != identity {
		root.Close()
		lock.Close()
		return nil, nil, fmt.Errorf("대상 폴더의 실제 위치가 변경됐습니다")
	}
	return root, func() { root.Close(); lock.Close() }, nil
}
func (s *Service) process(ctx context.Context, w workItem) error {
	committed := false
	defer func() {
		if committed && s.changed != nil {
			s.changed(w.mediaID)
		}
	}()
	var stat expected
	if json.Unmarshal([]byte(w.raw), &stat) != nil || stat.Version != 1 {
		return fmt.Errorf("이전 작업 이력은 실행할 수 없습니다")
	}
	if w.state == "committed" || w.state == "cleanup_pending" {
		if w.operation == "copy" {
			return fmt.Errorf("복사 작업에서는 원본 정리를 실행할 수 없습니다")
		}
		return s.cleanup(ctx, w, stat)
	}
	if w.fileID == 0 {
		return fmt.Errorf("파일 등록이 해제됐습니다")
	}
	root, closeRoot, e := openRoot(w.directory, stat.DirectoryIdentity)
	if e != nil {
		return e
	}
	defer closeRoot()
	// A partial copy is never accepted as a media file. Only its saved identity can reclaim it.
	if w.target != "" {
		if e = s.discard(root, w); e != nil {
			return e
		}
		if _, e = s.db.ExecContext(ctx, "UPDATE file_move_item SET target_path=NULL,target_key=NULL,verification_json=NULL WHERE id=? AND committed_at IS NULL", w.id); e != nil {
			return e
		}
	}
	var inspected engine.LocalMetadata
	if w.metadataMissing {
		if s.inspector == nil {
			return fmt.Errorf("미디어 정보 검사를 위한 엔진이 필요합니다")
		}
		detected := engine.LocalKind(w.source)
		inspected, e = s.inspector.Inspect(ctx, w.source, detected)
		if e != nil {
			return fmt.Errorf("원본 미디어 검사 실패: %w", e)
		}
		if inspected.Kind != "image" && inspected.Kind != "video" && inspected.Kind != "audio" {
			return fmt.Errorf("검증된 이미지·영상·오디오만 이동할 수 있습니다")
		}
		w.kind = inspected.Kind
	}
	sourceRoot, e := os.OpenRoot(filepath.Dir(w.source))
	if e != nil {
		return e
	}
	defer sourceRoot.Close()
	source, e := media.OpenOwnedFile(sourceRoot, filepath.Base(w.source))
	if e != nil {
		return e
	}
	defer source.Close()
	identity, e := media.FileIdentity(source)
	if e != nil {
		return e
	}
	info, e := source.Stat()
	if e != nil {
		return e
	}
	if identity != stat.Identity || info.Size() != stat.Size || fmt.Sprint(info.ModTime().UnixNano()) != stat.Mtime {
		return fmt.Errorf("원본이 선택한 시점 이후 변경됐습니다. 원본을 보존했습니다")
	}
	if w.kind != "image" && w.kind != "video" && w.kind != "audio" {
		return fmt.Errorf("검증된 이미지·영상·오디오만 이동할 수 있습니다")
	}
	if strings.EqualFold(filepath.Clean(filepath.Dir(w.source)), filepath.Clean(w.directory)) {
		_, e = s.db.ExecContext(ctx, "UPDATE file_move_item SET state='excluded',error_message='이미 대상 폴더에 있습니다',finished_at=? WHERE id=?", time.Now().UnixMilli(), w.id)
		return e
	}
	var dest *os.File
	var name string
	token := w.token
	for trial := 0; trial < 100; trial++ {
		if trial > 0 || token == "" {
			token, e = media.NewFilenameToken()
			if e != nil {
				return e
			}
		}
		name, e = media.Filename(w.title, strings.TrimSuffix(filepath.Base(w.source), filepath.Ext(w.source)), filepath.Ext(w.source), token)
		if e != nil {
			return e
		}
		if stat.KeepName {
			name = filepath.Base(w.source)
			if trial > 0 {
				ext := filepath.Ext(name)
				name = strings.TrimSuffix(name, ext) + fmt.Sprintf(" (%d)", trial) + ext
			}
		}
		reserved := proof{Token: token}
		reservedRaw, _ := json.Marshal(reserved)
		if _, e = s.db.ExecContext(ctx, "UPDATE file_move_item SET target_path=?,target_key=?,verification_json=?,state='copying' WHERE id=? AND committed_at IS NULL", filepath.Join(w.directory, name), "move:"+strings.ToLower(filepath.Join(w.directory, name)), string(reservedRaw), w.id); e != nil {
			return e
		}
		dest, e = media.CreateOwnedFile(root, name)
		if errors.Is(e, os.ErrExist) {
			continue
		}
		if e != nil {
			return e
		}
		break
	}
	if dest == nil {
		return fmt.Errorf("파일명 충돌을 해결하지 못했습니다")
	}
	defer dest.Close()
	target := filepath.Join(w.directory, name)
	destID, e := media.FileIdentity(dest)
	if e != nil {
		media.DeleteOwnedFile(dest)
		return e
	}
	evidence := proof{Destination: media.FileProof{Path: name, Identity: destID}, Source: media.FileProof{Identity: identity, Size: stat.Size}, Token: token}
	raw, _ := json.Marshal(evidence)
	// Persist ownership before writing bytes. If saving fails, delete through our own handle.
	_, e = s.db.ExecContext(ctx, "UPDATE file_move_item SET target_path=?,target_key=?,verification_json=?,state='copying',try_count=try_count+1 WHERE id=? AND committed_at IS NULL", target, "move:"+strings.ToLower(target), string(raw), w.id)
	if e != nil {
		media.DeleteOwnedFile(dest)
		return e
	}
	defer func() {
		if !committed {
			media.DeleteOwnedFile(dest)
		}
	}()
	hash := sha256.New()
	buf := make([]byte, 1024*1024)
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		n, re := source.Read(buf)
		if n > 0 {
			if _, e = dest.Write(buf[:n]); e != nil {
				return e
			}
			hash.Write(buf[:n])
			s.mu.Lock()
			s.copied += int64(n)
			s.mu.Unlock()
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			return re
		}
	}
	evidence.Source.Hash = fmt.Sprintf("%x", hash.Sum(nil))
	evidence.Destination.Size = stat.Size
	evidence.Destination.Hash = evidence.Source.Hash
	if stat.Hash != "" && stat.Hash != evidence.Source.Hash {
		return fmt.Errorf("원본 내용이 등록된 해시와 다릅니다. 원본을 보존했습니다")
	}
	if e = dest.Sync(); e != nil {
		return e
	}
	if e = media.VerifyOwnedFileContext(ctx, dest, evidence.Destination); e != nil {
		return e
	}
	if e = media.VerifyOwnedFileContext(ctx, source, evidence.Source); e != nil {
		return e
	}
	evidence.Ready = true
	raw, _ = json.Marshal(evidence)
	if _, e = s.db.ExecContext(ctx, "UPDATE file_move_item SET state='verified',verification_json=? WHERE id=? AND state='copying'", string(raw), w.id); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	// Resolve the public path while both exact handles are still locked.
	actual, targetKey, e := media.CanonicalPath(target)
	if e != nil || !strings.EqualFold(actual, target) {
		return fmt.Errorf("대상 파일 경로가 변경됐습니다")
	}
	destInfo, e := dest.Stat()
	if e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if w.operation == "copy" {
		var current string
		if e = tx.QueryRowContext(ctx, "SELECT filepath FROM media_file WHERE id=? AND file_uuid=?", w.fileID, w.uuid).Scan(&current); e != nil {
			return e
		}
		if current != w.source {
			return fmt.Errorf("원본 DB 위치가 변경됐습니다")
		}
		result, err := tx.ExecContext(ctx, "UPDATE file_move_item SET state='completed',committed_at=?,finished_at=?,error_code=NULL,error_message=NULL WHERE id=? AND state='verified'", time.Now().UnixMilli(), time.Now().UnixMilli(), w.id)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil || n != 1 {
			return fmt.Errorf("복사 작업 상태가 변경됐습니다")
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		committed = true
		return nil
	}
	var refs int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM media_file WHERE id<>? AND (filepath=? COLLATE NOCASE OR canonical_path_key=?)", w.fileID, w.source, w.sourceKey).Scan(&refs); e != nil {
		return e
	}
	if refs > 0 {
		return fmt.Errorf("다른 등록에서도 참조하는 원본은 이동할 수 없습니다")
	}
	result, e := tx.ExecContext(ctx, `UPDATE media_file SET kind=?,detected_format=coalesce(nullif(?,''),detected_format),width=coalesce(?,width),height=coalesce(?,height),duration=coalesce(?,duration),filepath=?,canonical_path_key=?,directory_id=?,storage_kind='external_reference',relative_path=?,local_folder_location_id=NULL,filename_token=?,filesize=?,mtime_ns=?,hash_algorithm='sha256',content_hash=? WHERE id=? AND file_uuid=? AND filepath=?`, w.kind, inspected.Format, inspected.Width, inspected.Height, inspected.Duration, target, targetKey, w.directoryID, name, token, stat.Size, fmt.Sprint(destInfo.ModTime().UnixNano()), evidence.Source.Hash, w.fileID, w.uuid, w.source)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil || n != 1 {
		return fmt.Errorf("원본 DB 위치가 변경됐습니다")
	}
	// A successful move is independent of the original import checkpoint/location.
	if _, e = tx.ExecContext(ctx, "UPDATE file_move_item SET state='committed',committed_at=?,error_code=NULL,error_message=NULL WHERE id=? AND state='verified'", time.Now().UnixMilli(), w.id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE media SET revision=revision+1,updated_at=? WHERE id=?", time.Now().UnixMilli(), w.mediaID); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	committed = true
	s.mu.Lock()
	s.copied = 0
	s.mu.Unlock()
	// The verified source handle remains locked through commit and deletion.
	if e = media.DeleteOwnedFile(source); e != nil {
		return e
	}
	_, e = s.db.ExecContext(context.Background(), "UPDATE file_move_item SET state='completed',finished_at=?,error_code=NULL,error_message=NULL WHERE id=?", time.Now().UnixMilli(), w.id)
	return e
}
func (s *Service) discard(root *os.Root, w workItem) error {
	if filepath.Dir(w.target) != w.directory {
		return fmt.Errorf("작업의 대상 경로를 확인하세요")
	}
	f, e := media.OpenOwnedFile(root, filepath.Base(w.target))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	defer f.Close()
	var evidence proof
	if json.Unmarshal([]byte(w.verification), &evidence) != nil || evidence.Destination.Identity == "" {
		return fmt.Errorf("사본 소유권을 확인할 수 없어 보존했습니다: %s", w.target)
	}
	id, e := media.FileIdentity(f)
	if e != nil {
		return e
	}
	if id != evidence.Destination.Identity {
		return fmt.Errorf("다른 파일로 바뀐 사본을 보존했습니다: %s", w.target)
	}
	if evidence.Ready {
		if e = media.VerifyOwnedFile(f, evidence.Destination); e != nil {
			return e
		}
	}
	return media.DeleteOwnedFile(f)
}
func (s *Service) cleanup(ctx context.Context, w workItem, stat expected) error {
	var evidence proof
	if json.Unmarshal([]byte(w.verification), &evidence) != nil || !evidence.Ready {
		return fmt.Errorf("확정된 파일 검증 기록을 확인하세요")
	}
	root, closeRoot, e := openRoot(w.directory, stat.DirectoryIdentity)
	if e != nil {
		return e
	}
	defer closeRoot()
	dest, e := media.OpenOwnedFile(root, filepath.Base(w.target))
	if e != nil {
		return e
	}
	defer dest.Close()
	if e = media.VerifyOwnedFileContext(ctx, dest, evidence.Destination); e != nil {
		return e
	}
	var registered string
	if e = s.db.QueryRowContext(ctx, "SELECT filepath FROM media_file WHERE id=? AND file_uuid=?", w.fileID, w.uuid).Scan(&registered); e != nil {
		return e
	}
	if registered != w.target {
		return fmt.Errorf("확정 후 DB 경로가 변경되어 원본을 보존했습니다")
	}
	sourceRoot, e := os.OpenRoot(filepath.Dir(w.source))
	if e == nil {
		defer sourceRoot.Close()
		f, err := media.OpenOwnedFile(sourceRoot, filepath.Base(w.source))
		if err == nil {
			defer f.Close()
			if e = media.VerifyOwnedFileContext(ctx, f, evidence.Source); e != nil {
				return e
			}
			if e = media.DeleteOwnedFile(f); e != nil {
				return e
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	_, e = s.db.ExecContext(ctx, "UPDATE file_move_item SET state='completed',finished_at=?,error_code=NULL,error_message=NULL WHERE id=?", time.Now().UnixMilli(), w.id)
	return e
}
