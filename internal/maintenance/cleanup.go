package maintenance

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/download"
)

type Cleaner struct {
	db     *sql.DB
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
}

func NewCleaner(db *sql.DB) *Cleaner {
	ctx, cancel := context.WithCancel(context.Background())
	return &Cleaner{db: db, ctx: ctx, cancel: cancel}
}
func (s *Cleaner) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		timer := time.NewTicker(15 * time.Second)
		defer timer.Stop()
		for {
			_ = s.RunOnce(s.ctx)
			select {
			case <-s.ctx.Done():
				return
			case <-timer.C:
			}
		}
	}()
}
func (s *Cleaner) Close() { s.cancel(); s.wg.Wait() }
func (s *Cleaner) RunOnce(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT id,payload_json FROM maintenance_task WHERE kind IN ('deletion_cleanup','obsolete_file_cleanup') AND (state IN ('pending','running') OR (state='failed' AND try_count<3)) AND NOT EXISTS(SELECT 1 FROM legacy_record WHERE table_name='migration-v2:maintenance_task' AND row_key=maintenance_task.id) ORDER BY created_at LIMIT 100`)
	if err != nil {
		return err
	}
	type task struct{ id, payload string }
	tasks := []task{}
	for rows.Next() {
		var item task
		if err = rows.Scan(&item.id, &item.payload); err != nil {
			break
		}
		tasks = append(tasks, item)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range tasks {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err = s.db.ExecContext(ctx, "UPDATE maintenance_task SET state='running',try_count=try_count+1,updated_at=? WHERE id=?", time.Now().UnixMilli(), item.id); err != nil {
			return err
		}
		var payload struct {
			Paths []string `json:"paths"`
		}
		e := json.Unmarshal([]byte(item.payload), &payload)
		if e == nil && len(payload.Paths) == 0 {
			e = fmt.Errorf("파일 정리 목록이 없습니다")
		}
		if e == nil {
			for _, path := range payload.Paths {
				if e = s.removeOwnedFile(ctx, path); e != nil {
					break
				}
			}
		}
		state := "completed"
		var lastError any
		if e != nil {
			state = "failed"
			lastError = e.Error()
		}
		if _, err = s.db.ExecContext(ctx, "UPDATE maintenance_task SET state=?,last_error=?,updated_at=? WHERE id=?", state, lastError, time.Now().UnixMilli(), item.id); err != nil {
			return err
		}
	}
	return nil
}

var ownerPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var itemPattern = regexp.MustCompile(`^item-[0-9]{4,}$`)

func (s *Cleaner) removeOwnedFile(ctx context.Context, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("정리 대상에 절대 경로가 필요합니다")
	}
	path = filepath.Clean(path)
	var registered bool
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM media_file WHERE filepath=? OR thumbnail_path=? OR preview_path=?)", path, path, path).Scan(&registered); err != nil {
		return err
	}
	if registered {
		return fmt.Errorf("등록된 파일은 삭제할 수 없습니다")
	}
	// Human-readable paths are authorized by the exact publication record, not
	// by their name. Never infer ownership from site/user/title alone.
	publications, err := s.db.QueryContext(ctx, `SELECT q.id,a.id,st.storage_root FROM download_target t JOIN download_attempt a ON a.id=t.attempt_id JOIN download_queue q ON q.id=a.job_id JOIN download_job_storage st ON st.job_id=q.id WHERE t.final_path=? AND a.committed_at IS NOT NULL ORDER BY a.started_at DESC`, path)
	if err != nil {
		return err
	}
	type owner struct{ job, attempt, root string }
	owners := []owner{}
	for publications.Next() {
		var item owner
		if err = publications.Scan(&item.job, &item.attempt, &item.root); err != nil {
			break
		}
		owners = append(owners, item)
	}
	if err == nil {
		err = publications.Err()
	}
	publications.Close()
	if err != nil {
		return err
	}
	for _, item := range owners {
		rel, e := filepath.Rel(item.root, path)
		if e == nil && filepath.IsLocal(rel) && len(strings.Split(rel, string(filepath.Separator))) == 3 {
			if e = (download.Storage{Root: item.root}).RemovePublishedFile(item.job, item.attempt, path); e == nil {
				return nil
			}
		}
	}
	// Only roots recorded for real jobs and this runtime's generated layout qualify.
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT storage_root FROM download_job_storage")
	if err != nil {
		return err
	}
	roots := []string{}
	for rows.Next() {
		var root string
		if err = rows.Scan(&root); err != nil {
			break
		}
		roots = append(roots, root)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, base := range roots {
		relative, e := filepath.Rel(base, path)
		if e != nil {
			continue
		}
		parts := strings.Split(relative, string(filepath.Separator))
		if len(parts) != 5 || parts[0] != ".asterodia" || parts[1] != "files" || !ownerPattern.MatchString(parts[2]) || !itemPattern.MatchString(parts[3]) || !strings.HasPrefix(parts[4], "media.") {
			continue
		}
		root, e := os.OpenRoot(base)
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		defer root.Close()
		for i := 1; i < 5; i++ {
			info, e := root.Lstat(filepath.Join(parts[:i]...))
			if os.IsNotExist(e) {
				return nil
			}
			if e != nil {
				return e
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("정리 폴더 경로가 변경되었습니다")
			}
		}
		info, e := root.Lstat(relative)
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("일반 파일만 정리할 수 있습니다")
		}
		if e = root.Remove(relative); e != nil {
			return e
		}
		_ = root.Remove(filepath.Dir(relative))
		_ = root.Remove(filepath.Dir(filepath.Dir(relative)))
		return nil
	}
	return fmt.Errorf("앱 관리 경로를 확인할 수 없어 원본 파일을 보존했습니다")
}
