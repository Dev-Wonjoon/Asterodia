package localimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"github.com/google/uuid"
)

type target struct {
	id, folder, attempt, path, key       string
	state                                fileState
	metadata                             engine.LocalMetadata
	hash                                 string
	existing, mediaID, localID           int64
	fileUUID, storage, oldMtime, oldHash string
	oldSize                              int64
	unchanged, foreign                   bool
	expectedSize                         int64
}
type fileFailure struct{ err error }

func (e *fileFailure) Error() string { return e.err.Error() }
func fileError(err error) error      { return &fileFailure{err} }

func (s *Service) run(ctx context.Context, job string) {
	workers := importWorkerCount(runtime.NumCPU())
	var root string
	if err := s.db.QueryRowContext(ctx, "SELECT root_path FROM local_import_job WHERE id=?", job).Scan(&root); err != nil {
		s.stopWork(ctx, job, err)
		return
	}
	if err := s.runInspections(ctx, job, root, workers); err != nil {
		s.stopWork(ctx, job, err)
		return
	}
	s.complete(ctx, job, "completed")
}
func hashFile(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	buffer := make([]byte, 128*1024)
	for {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		n, e := f.Read(buffer)
		if n > 0 {
			hash.Write(buffer[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", e
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// Database lookups and group guards run only on the coordinator.
func (s *Service) prepareTarget(ctx context.Context, root string, t *target, refresh bool) error {
	if err := checkDirectory(root, t.path, t.key); err != nil {
		return fileError(err)
	}
	before, err := snapshot(root, t.state.Path)
	if err != nil {
		return fileError(err)
	}
	if !refresh && before != t.state {
		return fileError(fmt.Errorf("탐색 후 원본이 변경되었습니다. 파일을 확인한 뒤 재시도하세요"))
	}
	t.state = before
	err = s.db.QueryRowContext(ctx, "SELECT coalesce(import_media_id,0) FROM media_directory WHERE directory_key=? AND key_version=1", t.key).Scan(&t.mediaID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	var parent int64
	err = s.db.QueryRowContext(ctx, `SELECT id,media_id,file_uuid,storage_kind,coalesce(filesize,0),coalesce(mtime_ns,''),coalesce(content_hash,'') FROM media_file WHERE canonical_path_key=? OR filepath=? ORDER BY id LIMIT 1`, t.state.Key, t.state.Path).Scan(&t.existing, &parent, &t.fileUUID, &t.storage, &t.oldSize, &t.oldMtime, &t.oldHash)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if t.existing != 0 && (t.storage != "external_reference" || parent != t.mediaID) {
		t.foreign = true
		t.unchanged = true
		t.hash = t.oldHash
		t.mediaID = 0
		return nil
	}
	if t.mediaID != 0 {
		// Link before inspection so writes to this group are guarded while running.
		_, err = s.db.ExecContext(ctx, "UPDATE local_import_folder SET local_folder_id=(SELECT id FROM local_folder WHERE media_id=?) WHERE id=? AND local_folder_id IS NOT (SELECT id FROM local_folder WHERE media_id=?)", t.mediaID, t.folder, t.mediaID)
		if err != nil {
			return err
		}
	}
	if t.existing != 0 && t.oldSize == t.state.Size && t.oldMtime == t.state.Mtime {
		t.unchanged = true
		t.hash = t.oldHash
		return nil
	}
	return nil
}

// Workers own their target and perform no database reads or writes.
func (s *Service) inspectTarget(ctx context.Context, root string, t *target) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.foreign || t.unchanged {
		return nil
	}
	// Preparation can precede execution by a full queue window. Revalidate the
	// source immediately before opening it for hashing/decoding as well.
	if err := checkDirectory(root, t.path, t.key); err != nil {
		return fileError(err)
	}
	before, err := snapshot(root, t.state.Path)
	if err != nil || before != t.state {
		return fileError(fmt.Errorf("검사 대기 중 원본 파일이 변경되었습니다"))
	}
	t.hash, err = hashFile(ctx, t.state.Path)
	if err != nil {
		return fileError(err)
	}
	if t.existing != 0 && t.hash == t.oldHash {
		t.unchanged = true
	} else {
		if s.inspector == nil {
			return fileError(fmt.Errorf("미디어 검사 엔진이 연결되지 않았습니다"))
		}
		t.metadata, err = s.inspector.Inspect(ctx, t.state.Path, engine.LocalKind(t.state.Path))
		if err != nil {
			return fileError(err)
		}
	}
	after, err := snapshot(root, t.state.Path)
	if err != nil || after != t.state {
		return fileError(fmt.Errorf("검사 중 원본 파일이 변경되었습니다"))
	}
	return nil
}
func (s *Service) commitTarget(ctx context.Context, job, root string, t *target) error {
	// Slow filesystem checks stay outside the short cancellation/commit lock.
	before, err := snapshot(root, t.state.Path)
	if err != nil || before != t.state {
		return fileError(fmt.Errorf("저장 직전 원본 파일이 변경되었습니다"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	var currentUUID string
	err = tx.QueryRowContext(ctx, "SELECT id,file_uuid FROM media_file WHERE canonical_path_key=? OR filepath=? ORDER BY id LIMIT 1", t.state.Key, t.state.Path).Scan(&current, &currentUUID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if current != t.existing || (current != 0 && currentUUID != t.fileUUID) {
		return fileError(fmt.Errorf("검사 중 파일 등록 정보가 변경되었습니다"))
	}
	{
		var reserved int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM file_move_item WHERE state IN ('pending','copying','verified','committed','cleanup_pending') AND target_key=?", "move:"+strings.ToLower(t.state.Path)).Scan(&reserved); err != nil {
			return err
		}
		if reserved > 0 {
			return fileError(fmt.Errorf("파일 이동이 진행 중인 경로입니다"))
		}
	}
	if t.existing != 0 {
		var moving int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM file_move_item WHERE file_id=? AND state IN ('pending','copying','verified','committed','cleanup_pending')", t.existing).Scan(&moving); err != nil {
			return err
		}
		if moving > 0 {
			return fileError(fmt.Errorf("파일 이동이 진행 중입니다"))
		}
	}
	now := time.Now().UnixMilli()
	resultKind := "unchanged"
	targetState := "registered"
	if t.foreign {
		targetState = "unchanged"
	} else {
		directory, err := media.NewStore(tx).EnsureDirectory(ctx, t.path, t.key, 1, true)
		if err != nil {
			return err
		}
		var mediaID int64
		if err = tx.QueryRowContext(ctx, "SELECT coalesce(import_media_id,0) FROM media_directory WHERE id=?", directory).Scan(&mediaID); err != nil {
			return err
		}
		if mediaID != t.mediaID {
			return fileError(fmt.Errorf("검사 중 기본 가져오기 묶음이 변경되었습니다"))
		}
		var localID, location, revision int64
		if mediaID == 0 {
			r, e := tx.ExecContext(ctx, "INSERT INTO media(source_kind,source_title,created_at,updated_at) VALUES('local',?,?,?)", filepath.Base(t.path), now, now)
			if e != nil {
				return e
			}
			mediaID, e = r.LastInsertId()
			if e != nil {
				return e
			}
			r, e = tx.ExecContext(ctx, "INSERT INTO local_folder(media_id,source_title) VALUES(?,?)", mediaID, filepath.Base(t.path))
			if e != nil {
				return e
			}
			localID, e = r.LastInsertId()
			if e != nil {
				return e
			}
			if e = media.NewStore(tx).SetImportMedia(ctx, directory, mediaID); e != nil {
				return e
			}
		} else {
			if err = tx.QueryRowContext(ctx, "SELECT l.id,m.revision FROM local_folder l JOIN media m ON m.id=l.media_id WHERE l.media_id=?", mediaID).Scan(&localID, &revision); err != nil {
				return err
			}
		}
		err = tx.QueryRowContext(ctx, "SELECT id FROM local_folder_location WHERE media_id=? AND directory_id=? AND state='active' ORDER BY id LIMIT 1", mediaID, directory).Scan(&location)
		if err == sql.ErrNoRows {
			r, e := tx.ExecContext(ctx, "INSERT INTO local_folder_location(local_folder_id,media_id,directory_id,directory_path,directory_key,key_version,state) VALUES(?,?,?,?,?,1,'active')", localID, mediaID, directory, t.path, t.key)
			if e != nil {
				return e
			}
			location, err = r.LastInsertId()
		}
		if err != nil {
			return err
		}
		t.mediaID = mediaID
		if t.existing == 0 {
			var position int64
			if err = tx.QueryRowContext(ctx, "SELECT coalesce(max(position)+1,0) FROM media_file WHERE media_id=?", mediaID).Scan(&position); err != nil {
				return err
			}
			r, e := tx.ExecContext(ctx, `INSERT INTO media_file(media_id,file_key,storage_kind,directory_id,title,filepath,canonical_path_key,relative_path,local_folder_location_id,filesize,mtime_ns,hash_algorithm,content_hash,kind,position,detected_format,width,height,duration,created_at) VALUES(?,?,'external_reference',?,?,?,?,?,?,?,?,'sha256',?,?,?,?,?,?,?,?)`, mediaID, "local:"+uuid.NewString(), directory, filepath.Base(t.state.Path), t.state.Path, t.state.Key, filepath.Base(t.state.Path), location, t.state.Size, t.state.Mtime, t.hash, t.metadata.Kind, position, t.metadata.Format, t.metadata.Width, t.metadata.Height, t.metadata.Duration, now)
			if e != nil {
				return e
			}
			t.existing, e = r.LastInsertId()
			if e != nil {
				return e
			}
			resultKind = "added"
			if err = tx.QueryRowContext(ctx, "SELECT file_uuid FROM media_file WHERE id=?", t.existing).Scan(&t.fileUUID); err != nil {
				return err
			}
		} else if !t.unchanged {
			if _, err = tx.ExecContext(ctx, `UPDATE media_file SET filesize=?,mtime_ns=?,content_hash=?,kind=?,detected_format=?,width=?,height=?,duration=?,thumbnail_path=NULL,thumbnail_origin='none',preview_path=NULL,cache_key=NULL WHERE id=? AND media_id=?`, t.state.Size, t.state.Mtime, t.hash, t.metadata.Kind, t.metadata.Format, t.metadata.Width, t.metadata.Height, t.metadata.Duration, t.existing, mediaID); err != nil {
				return err
			}
			resultKind = "updated"
		} else {
			if _, err = tx.ExecContext(ctx, "UPDATE media_file SET filesize=?,mtime_ns=? WHERE id=? AND media_id=?", t.state.Size, t.state.Mtime, t.existing, mediaID); err != nil {
				return err
			}
		}
		if resultKind != "unchanged" {
			if _, err = tx.ExecContext(ctx, "UPDATE media SET revision=revision+1,updated_at=? WHERE id=?", now, mediaID); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, "UPDATE local_import_folder SET local_folder_id=? WHERE id=?", localID, t.folder); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE local_import_attempt SET expected_media_revision=? WHERE id=?", revision, t.attempt); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(t.state)
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(t.metadata)
	if err != nil {
		return err
	}
	var hash any
	if t.hash != "" {
		hash = t.hash
	}
	r, err := tx.ExecContext(ctx, `UPDATE local_import_target SET state=?,expected_stat_json=?,inspection_json=?,media_file_id=?,file_uuid_snapshot=?,inspection_hash=?,inspector_version='local-v2',committed_at=?,result_kind=?,refresh_requested=0,error_code=NULL WHERE id=? AND state='pending'`, targetState, string(raw), string(metadata), t.existing, t.fileUUID, hash, now, resultKind, t.id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("파일 체크포인트가 변경되었습니다")
	}
	if err = changeCounts(ctx, tx, job, func(c *Counts) {
		c.Processed++
		c.ProcessedBytes += t.state.Size
		c.TotalBytes += t.state.Size - t.expectedSize
		switch resultKind {
		case "added":
			c.Added++
		case "updated":
			c.Updated++
		default:
			c.Unchanged++
		}
	}); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) recordFailure(ctx context.Context, job, id string, size int64, problem error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.ExecContext(ctx, "UPDATE local_import_target SET state='failed',error_code=? WHERE id=? AND state='pending'", problem.Error(), id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		if err = changeCounts(ctx, tx, job, func(c *Counts) { c.Failed++; c.Processed++; c.ProcessedBytes += size }); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Service) completeFolder(ctx context.Context, job, folder, attempt string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var failures int
	if err = tx.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM local_import_target WHERE attempt_id=? AND state='failed')+(SELECT count(*) FROM local_import_scan_directory d JOIN local_import_folder f ON f.job_id=d.job_id AND f.directory_key=d.directory_key WHERE f.id=? AND d.state='failed')", attempt, folder).Scan(&failures); err != nil {
		return err
	}
	state := "completed"
	if failures > 0 {
		state = "completed_with_errors"
	}
	if _, err = tx.ExecContext(ctx, "UPDATE local_import_folder SET state=? WHERE id=?", state, folder); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	if _, err = tx.ExecContext(ctx, "UPDATE local_import_attempt SET phase='finished',outcome=?,finished_at=?,committed_at=? WHERE id=?", state, now, now, attempt); err != nil {
		return err
	}
	if err = changeCounts(ctx, tx, job, func(c *Counts) {
		if failures > 0 {
			c.FailedFolders++
		} else {
			c.CompletedFolders++
		}
	}); err != nil {
		return err
	}
	return tx.Commit()
}
