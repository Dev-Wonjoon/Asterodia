package download

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type verifiedFile struct {
	id, key, source, staged, final, hash, format, token string
	position                                            int
	size, mtime                                         int64
	width, height                                       *int
	metadata                                            TargetMetadata
}

func workPaths(job, attempt string) (string, string) {
	owner := stageOwner(job, attempt)
	return filepath.Join(".storagebucket", "staging", owner), filepath.Join(".storagebucket", "files", owner)
}
func (s *Service) beginCommit(ctx context.Context, job, attempt string) error {
	return requireOne(s.db.ExecContext(ctx, `UPDATE download_attempt SET phase='committing' WHERE id=? AND job_id=? AND phase='verifying' AND outcome IS NULL AND cancel_requested_at IS NULL AND EXISTS (SELECT 1 FROM download_queue WHERE id=? AND current_attempt_id=? AND state='running' AND cleanup_required=0)`, attempt, job, job, attempt))
}
func verifyFile(path string, metadata TargetMetadata) (verifiedFile, error) {
	var v verifiedFile
	v.metadata = metadata
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return v, err
	}
	if !strings.EqualFold(filepath.Clean(resolved), filepath.Clean(path)) {
		return v, fmt.Errorf("파일 실제 경로가 변경되었습니다")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return v, err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 {
		return v, fmt.Errorf("일반 미디어 파일이 필요합니다")
	}
	f, err := os.Open(path)
	if err != nil {
		return v, err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil {
		return v, err
	}
	after, err := f.Stat()
	if err != nil {
		return v, err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		return v, fmt.Errorf("검증 중 파일이 변경되었습니다")
	}
	v.hash = fmt.Sprintf("%x", hash.Sum(nil))
	v.size = after.Size()
	v.mtime = after.ModTime().UnixNano()
	v.format = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if metadata.Kind == "image" {
		if _, err = f.Seek(0, 0); err != nil {
			return v, err
		}
		config, format, e := image.DecodeConfig(f)
		if e != nil || config.Width <= 0 || config.Height <= 0 {
			return v, fmt.Errorf("이미지 형식 검증 실패: %v", e)
		}
		v.width = &config.Width
		v.height = &config.Height
		v.format = format
	}
	return v, nil
}

// Verified work stays private until publication. Each friendly destination is
// recorded durably before its exclusive move and before the DB commit.
func (s *Service) commit(ctx context.Context, job, attempt, storage string) (int64, error) {
	stageRel, finalRel := workPaths(job, attempt)
	base, err := filepath.EvalSymlinks(storage)
	if err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	if err = root.MkdirAll(filepath.Join(".storagebucket", "files"), 0700); err != nil {
		return 0, err
	}
	parentInfo, err := root.Lstat(filepath.Dir(finalRel))
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return 0, fmt.Errorf("최종 파일 폴더가 올바르지 않습니다")
	}
	if _, err = root.Lstat(finalRel); !os.IsNotExist(err) {
		return 0, fmt.Errorf("최종 폴더가 이미 존재하거나 확인하지 못했습니다")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,target_key,source_url,metadata_json,position,staged_path FROM download_target WHERE attempt_id=? AND state='downloaded' ORDER BY position`, attempt)
	if err != nil {
		return 0, err
	}
	files := []verifiedFile{}
	for rows.Next() {
		var v verifiedFile
		var raw string
		if err = rows.Scan(&v.id, &v.key, &v.source, &raw, &v.position, &v.staged); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(raw), &v.metadata); err != nil {
			break
		}
		stageDir := filepath.Join(base, stageRel)
		if err = verifyStagedFile(stageDir, v.position, v.staged); err != nil {
			break
		}
		proof, e := verifyFile(v.staged, v.metadata)
		if e != nil {
			err = e
			break
		}
		v.size = proof.size
		v.mtime = proof.mtime
		v.hash = proof.hash
		v.format = proof.format
		v.width = proof.width
		v.height = proof.height
		rel, e := filepath.Rel(stageDir, v.staged)
		if e != nil {
			err = e
			break
		}
		v.final = filepath.Join(base, finalRel, rel)
		files = append(files, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, fmt.Errorf("검증할 파일이 없습니다")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	for _, v := range files {
		if err = requireOne(tx.ExecContext(ctx, "UPDATE download_target SET final_path=?,state='verified' WHERE id=? AND attempt_id=? AND staged_path=? AND state='downloaded'", v.final, v.id, attempt, v.staged)); err != nil {
			tx.Rollback()
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	// Do not publish the private cookie work copy alongside media.
	if err = root.Remove(filepath.Join(stageRel, "cookies.txt")); err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	if err = root.Rename(stageRel, finalRel); err != nil {
		return 0, err
	}
	for _, v := range files {
		proof, e := verifyFile(v.final, v.metadata)
		if e != nil {
			return 0, e
		}
		if proof.hash != v.hash || proof.size != v.size {
			return 0, fmt.Errorf("최종 파일 내용이 검증 결과와 다릅니다")
		}
		if s.verifyMedia != nil && (v.metadata.Kind == "video" || v.metadata.Kind == "audio") {
			if e = s.verifyMedia(ctx, v.final, v.metadata.Kind); e != nil {
				return 0, fmt.Errorf("미디어 재생 검증 실패: %w", e)
			}
		}
	}
	previousPaths := make([]string, len(files))
	for i := range files {
		previousPaths[i] = files[i].final
	}
	files, err = (Storage{Root: base}).publish(root, job, attempt, files)
	if err != nil {
		return 0, err
	}
	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	for i, v := range files {
		if err = requireOne(tx.ExecContext(ctx, "UPDATE download_target SET final_path=? WHERE id=? AND attempt_id=? AND state='verified' AND final_path=?", v.final, v.id, attempt, previousPaths[i])); err != nil {
			tx.Rollback()
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	release, err := holdPublished(root, job, attempt, files)
	if err != nil {
		return 0, err
	}
	defer release()
	return s.register(ctx, job, attempt, files)
}
func (s *Service) register(ctx context.Context, job, attempt string, files []verifiedFile) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var mode, options string
	var requestID int64
	var target *int64
	if err = tx.QueryRowContext(ctx, `SELECT q.mode,q.request_url_id,q.target_media_id,q.options_json FROM download_queue q JOIN download_attempt a ON a.id=q.current_attempt_id WHERE q.id=? AND a.id=? AND q.state='running' AND a.phase='committing' AND a.outcome IS NULL`, job, attempt).Scan(&mode, &requestID, &target, &options); err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	var mediaID int64
	if mode == "create" {
		result, e := tx.ExecContext(ctx, "INSERT INTO media(source_kind,request_url_id,source_title,created_at,updated_at) VALUES('download',?,?,?,?)", requestID, files[0].metadata.Title, now, now)
		if e != nil {
			return 0, e
		}
		mediaID, err = result.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else {
		if target == nil {
			return 0, ErrRevisionConflict
		}
		mediaID = *target
		var expected struct {
			Revision *int64 `json:"_expectedRevision"`
		}
		if json.Unmarshal([]byte(options), &expected) != nil || expected.Revision == nil {
			return 0, ErrRevisionConflict
		}
		var revision int64
		if err = tx.QueryRowContext(ctx, "SELECT revision FROM media WHERE id=? AND request_url_id=?", mediaID, requestID).Scan(&revision); err != nil || revision != *expected.Revision {
			return 0, ErrRevisionConflict
		}
		if _, err = tx.ExecContext(ctx, "UPDATE media SET cover_mode='auto',cover_file_id=NULL WHERE id=?", mediaID); err != nil {
			return 0, err
		}
		// Old files stay on disk. Their DB registration is replaced atomically; the
		// maintenance record allows safe, retryable reclamation after commit.
		if err = s.queueObsolete(ctx, tx, mediaID, now); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM media_file WHERE media_id=?", mediaID); err != nil {
			return 0, err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE download_attempt SET target_media_id=?,target_media_id_snapshot=? WHERE id=?", mediaID, mediaID, attempt); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE download_queue SET target_media_id=?,target_media_id_snapshot=? WHERE id=?", mediaID, mediaID, job); err != nil {
		return 0, err
	}
	for _, v := range files {
		var platformID any
		directoryPath, directoryKey, e := media.CanonicalPath(filepath.Dir(v.final))
		if e != nil {
			return 0, e
		}
		directoryID, e := media.NewStore(tx).EnsureDirectory(ctx, directoryPath, directoryKey, 1, false)
		if e != nil {
			return 0, e
		}
		if v.metadata.PlatformName != "" {
			if _, err = tx.ExecContext(ctx, "INSERT INTO platform(name) VALUES(?) ON CONFLICT(name) DO NOTHING", v.metadata.PlatformName); err != nil {
				return 0, err
			}
			var id int64
			if err = tx.QueryRowContext(ctx, "SELECT id FROM platform WHERE name=?", v.metadata.PlatformName).Scan(&id); err != nil {
				return 0, err
			}
			platformID = id
		}
		var profileID any
		if platformID != nil && v.metadata.OwnerID != "" {
			if _, err = tx.ExecContext(ctx, "INSERT INTO profiles(owner_id,owner_name,platform_id,updated_at) VALUES(?,?,?,?) ON CONFLICT(owner_id,platform_id) DO UPDATE SET owner_name=excluded.owner_name,updated_at=excluded.updated_at", v.metadata.OwnerID, v.metadata.OwnerName, platformID, now); err != nil {
				return 0, err
			}
			var id int64
			if err = tx.QueryRowContext(ctx, "SELECT id FROM profiles WHERE owner_id=? AND platform_id=?", v.metadata.OwnerID, platformID).Scan(&id); err != nil {
				return 0, err
			}
			profileID = id
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO media_file(media_id,file_key,filename_token,storage_kind,directory_id,source_url,external_id,platform_id,profile_id,title,filepath,filesize,mtime_ns,hash_algorithm,content_hash,kind,position,duration,detected_format,width,height,created_at) VALUES(?,?,?,'app_managed',?,?,?,?,?,?,?,?,?, 'sha256',?,?,?,?,?,?,?,?)`, mediaID, v.key, v.token, directoryID, v.source, v.metadata.ExternalID, platformID, profileID, v.metadata.Title, v.final, v.size, fmt.Sprint(v.mtime), v.hash, v.metadata.Kind, v.position, v.metadata.Duration, v.format, v.width, v.height, now); err != nil {
			return 0, err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE media SET source_title=?,last_download_commit_attempt_id=?,revision=revision+1,updated_at=? WHERE id=?", files[0].metadata.Title, attempt, now, mediaID); err != nil {
		return 0, err
	}
	if err = requireOne(tx.ExecContext(ctx, "UPDATE download_attempt SET phase='finished',outcome='completed',finished_at=?,committed_at=? WHERE id=? AND outcome IS NULL", now, now, attempt)); err != nil {
		return 0, err
	}
	if err = requireOne(tx.ExecContext(ctx, "UPDATE download_queue SET state='completed',progress=100,title=?,error_code=NULL,error_message=NULL,finished_at=?,updated_at=? WHERE id=? AND current_attempt_id=?", files[0].metadata.Title, now, now, job, attempt)); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return mediaID, nil
}
func (s *Service) queueObsolete(ctx context.Context, tx *sql.Tx, mediaID, now int64) error {
	rows, err := tx.QueryContext(ctx, "SELECT filepath FROM media_file WHERE media_id=? AND storage_kind='app_managed'", mediaID)
	if err != nil {
		return err
	}
	paths := []string{}
	for rows.Next() {
		var p string
		if err = rows.Scan(&p); err != nil {
			break
		}
		paths = append(paths, p)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	b, _ := json.Marshal(map[string]any{"paths": paths})
	_, err = tx.ExecContext(ctx, `INSERT INTO maintenance_task(id,kind,state,owner_kind,payload_json,created_at,updated_at) VALUES(lower(hex(randomblob(16))),'obsolete_file_cleanup','pending','system',?,?,?)`, string(b), now, now)
	return err
}
func (s *Service) abort(job, attempt, storage, outcome, code, message string, blocked bool) error {
	ctx := context.Background()
	var committed *int64
	if err := s.db.QueryRowContext(ctx, "SELECT committed_at FROM download_attempt WHERE id=?", attempt).Scan(&committed); err != nil {
		return err
	}
	if committed != nil {
		return nil
	}
	// Persist cleanup ownership before touching files, including on a crash.
	if _, err := s.db.ExecContext(ctx, "UPDATE download_queue SET cleanup_required=1 WHERE id=?", job); err != nil {
		return err
	}
	var cleanupErr error
	if blocked {
		cleanupErr = errors.New("엔진 종료를 확인하지 못해 파일 정리를 보류했습니다")
	} else {
		cleanupErr = removeWorkspace(storage, job, attempt)
	}
	now := time.Now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE download_attempt SET phase='finished',outcome=?,finished_at=? WHERE id=? AND committed_at IS NULL", outcome, now, attempt); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE download_queue SET state=?,cleanup_required=?,error_code=?,error_message=?,finished_at=?,updated_at=? WHERE id=? AND current_attempt_id=?", outcome, cleanupErr != nil, code, message, now, now, job, attempt); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return cleanupErr
}
func removeWorkspace(storage, job, attempt string) error {
	if !filepath.IsAbs(storage) {
		return fmt.Errorf("저장소 절대 경로가 필요합니다")
	}
	root, err := os.OpenRoot(storage)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	if err = (Storage{Root: storage}).removePublished(root, job, attempt, ""); err != nil {
		return err
	}
	stage, final := workPaths(job, attempt)
	for _, rel := range []string{stage, final} {
		// Reject changed parent paths instead of following a reparse point.
		for _, parent := range []string{".storagebucket", filepath.Dir(rel)} {
			info, e := root.Lstat(parent)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return e
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("작업 폴더 경로가 변경되었습니다")
			}
		}
		if err = root.RemoveAll(rel); err != nil {
			return err
		}
	}
	return nil
}

// Recover rolls back unfinished attempts. Committed files are never removed.
//
//wails:ignore
func (s *Service) Recover(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT q.id,a.id,st.storage_root FROM download_queue q JOIN download_attempt a ON a.id=q.current_attempt_id JOIN download_job_storage st ON st.job_id=q.id WHERE a.committed_at IS NULL AND (a.outcome IS NULL OR q.cleanup_required=1)`)
	if err != nil {
		return err
	}
	type pending struct{ job, attempt, root string }
	pendingRows := []pending{}
	for rows.Next() {
		var p pending
		if err = rows.Scan(&p.job, &p.attempt, &p.root); err != nil {
			break
		}
		pendingRows = append(pendingRows, p)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range pendingRows {
		if e := s.abort(p.job, p.attempt, p.root, "interrupted", "INTERRUPTED", "앱 종료로 중단된 작업입니다. 다시 시도할 수 있습니다", false); e != nil {
			// A single locked workspace must not prevent the user opening their library.
			if _, err = s.db.ExecContext(ctx, "UPDATE download_queue SET error_message=? WHERE id=?", e.Error(), p.job); err != nil {
				return err
			}
		}
	}
	return nil
}
