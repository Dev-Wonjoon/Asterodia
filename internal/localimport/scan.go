package localimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/google/uuid"
)

const scanBatchSize = 128

type scanEntry struct {
	name, path, key     string
	directory, excluded bool
	state               fileState
	problem             string
}

func ensureFolder(ctx context.Context, q connection, job, root, path, key, problem string) (string, string, error) {
	var id, attempt string
	err := q.QueryRowContext(ctx, "SELECT id,current_attempt_id FROM local_import_folder WHERE job_id=? AND directory_key=?", job, key).Scan(&id, &attempt)
	if err == nil {
		if problem != "" {
			_, err = q.ExecContext(ctx, "UPDATE local_import_folder SET error_code=? WHERE id=?", problem, id)
		}
		return id, attempt, err
	}
	if err != sql.ErrNoRows {
		return "", "", err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", "", err
	}
	id, attempt = uuid.NewString(), uuid.NewString()
	state := "pending"
	var reason any
	if problem != "" {
		state = "failed"
		reason = problem
	}
	if _, err = q.ExecContext(ctx, "INSERT INTO local_import_folder(id,job_id,directory_path,directory_key,relative_path,state,error_code) VALUES(?,?,?,?,?,?,?)", id, job, path, key, rel, state, reason); err != nil {
		return "", "", err
	}
	if _, err = q.ExecContext(ctx, "INSERT INTO local_import_attempt(id,import_folder_id,attempt_no,phase,commit_token,started_at) VALUES(?,?,1,'inspecting',?,?)", attempt, id, uuid.NewString(), time.Now().UnixMilli()); err != nil {
		return "", "", err
	}
	if _, err = q.ExecContext(ctx, "UPDATE local_import_folder SET current_attempt_id=? WHERE id=?", attempt, id); err != nil {
		return "", "", err
	}
	return id, attempt, nil
}
func (s *Service) scan(ctx context.Context, job string) {
	var root, key string
	if err := s.db.QueryRowContext(ctx, "SELECT root_path,root_key FROM local_import_job WHERE id=?", job).Scan(&root, &key); err != nil {
		s.stopWork(ctx, job, err)
		return
	}
	if err := checkDirectory(root, root, key); err != nil {
		s.stopWork(ctx, job, err)
		return
	}
	for {
		if err := ctx.Err(); err != nil {
			s.stopWork(ctx, job, err)
			return
		}
		var id int64
		var path, dirKey string
		err := s.db.QueryRowContext(ctx, "SELECT id,directory_path,directory_key FROM local_import_scan_directory WHERE job_id=? AND state='pending' ORDER BY id LIMIT 1", job).Scan(&id, &path, &dirKey)
		if err == sql.ErrNoRows {
			s.complete(ctx, job, "ready")
			return
		}
		if err != nil {
			s.stopWork(ctx, job, err)
			return
		}
		s.setCurrentPath(job, path)
		if _, err = s.db.ExecContext(ctx, "UPDATE local_import_job SET current_path=?,updated_at=?,revision=revision+1 WHERE id=?", path, time.Now().UnixMilli(), job); err != nil {
			s.stopWork(ctx, job, err)
			return
		}
		if err = s.scanDirectory(ctx, job, root, id, path, dirKey); err != nil {
			s.stopWork(ctx, job, err)
			return
		}
	}
}
func (s *Service) scanDirectory(ctx context.Context, job, root string, id int64, path, key string) error {
	// No OS enumeration offset is assumed stable across restarts. Only an
	// unfinished directory is reread; unique target/child keys retain saved work.
	var excluded int
	if err := s.db.QueryRowContext(ctx, "SELECT excluded_count FROM local_import_scan_directory WHERE id=?", id).Scan(&excluded); err != nil {
		return err
	}
	if excluded != 0 {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, "UPDATE local_import_scan_directory SET excluded_count=0 WHERE id=?", id); err != nil {
			return err
		}
		if err = changeCounts(ctx, tx, job, func(c *Counts) { c.ScanExcluded -= excluded }); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	if err := checkDirectory(root, path, key); err != nil {
		return s.scanProblem(ctx, job, root, id, path, key, err)
	}
	dir, err := os.Open(path)
	if err != nil {
		return s.scanProblem(ctx, job, root, id, path, key, err)
	}
	defer dir.Close()
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		entries, readErr := dir.ReadDir(scanBatchSize)
		batch := make([]scanEntry, 0, scanBatchSize)
		last := time.Now()
		for _, entry := range entries {
			if err = ctx.Err(); err != nil {
				return err
			}
			item := scanEntry{name: entry.Name(), path: filepath.Join(path, entry.Name()), directory: entry.IsDir()}
			info, e := entry.Info()
			if e != nil {
				item.problem = e.Error()
			} else {
				skip, e := hiddenOrLink(item.path, info)
				if e != nil {
					item.problem = e.Error()
				} else {
					item.excluded = skip
				}
				if info.Size() > 0 {
					item.state.Size = info.Size()
				}
			}
			if item.directory && (strings.EqualFold(item.name, ".git") || strings.EqualFold(item.name, ".asterodia")) {
				item.excluded = true
			}
			if !item.directory && engine.LocalKind(item.path) == "" {
				item.excluded = true
			}
			if !item.excluded {
				if item.directory {
					item.path, item.key, e = canonical(item.path)
					if e != nil {
						item.path = filepath.Join(path, item.name)
						item.key = "unreadable:" + item.path
						item.problem = e.Error()
					}
				} else {
					size := item.state.Size
					item.state, e = snapshot(root, item.path)
					if e != nil {
						item.state = fileState{Path: item.path, Size: size}
						item.problem = e.Error()
					}
				}
			}
			batch = append(batch, item)
			if len(batch) == scanBatchSize || time.Since(last) > 200*time.Millisecond {
				if err = s.saveScanBatch(ctx, job, root, id, path, key, batch); err != nil {
					return err
				}
				batch = batch[:0]
				last = time.Now()
			}
		}
		if len(batch) > 0 {
			if err = s.saveScanBatch(ctx, job, root, id, path, key, batch); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			_, err = s.db.ExecContext(ctx, "UPDATE local_import_scan_directory SET state='completed' WHERE id=?", id)
			return err
		}
		if readErr != nil {
			return s.scanProblem(ctx, job, root, id, path, key, readErr)
		}
	}
}
func (s *Service) saveScanBatch(ctx context.Context, job, root string, scanID int64, path, key string, entries []scanEntry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	delta := Counts{}
	var folder, attempt string
	for _, entry := range entries {
		if entry.excluded {
			delta.ScanExcluded++
			continue
		}
		if entry.directory {
			state := "pending"
			var problem any
			if entry.problem != "" {
				state = "failed"
				problem = entry.problem
			}
			r, e := tx.ExecContext(ctx, "INSERT INTO local_import_scan_directory(job_id,directory_path,directory_key,state,error_message) VALUES(?,?,?,?,?) ON CONFLICT(job_id,directory_key) DO NOTHING", job, entry.path, entry.key, state, problem)
			if e != nil {
				return e
			}
			n, e := r.RowsAffected()
			if e != nil {
				return e
			}
			if state == "failed" && n > 0 {
				delta.ScanErrors++
				if _, _, e = ensureFolder(ctx, tx, job, root, entry.path, entry.key, entry.problem); e != nil {
					return e
				}
			}
			continue
		}
		if folder == "" {
			folder, attempt, err = ensureFolder(ctx, tx, job, root, path, key, "")
			if err != nil {
				return err
			}
		}
		raw, e := json.Marshal(entry.state)
		if e != nil {
			return e
		}
		state := "pending"
		var problem any
		if entry.problem != "" {
			state = "failed"
			problem = entry.problem
		}
		r, e := tx.ExecContext(ctx, `INSERT INTO local_import_target(id,attempt_id,path_key,relative_path,expected_stat_json,state,error_code,sort_key) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(attempt_id,path_key) DO NOTHING`, uuid.NewString(), attempt, "entry:"+key+"/"+entry.name, entry.name, string(raw), state, problem, naturalKey(entry.name))
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n > 0 {
			delta.Discovered++
			delta.TotalBytes += entry.state.Size
			if state == "failed" {
				delta.Failed++
				delta.Processed++
				delta.ProcessedBytes += entry.state.Size
			}
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE local_import_scan_directory SET excluded_count=excluded_count+? WHERE id=?", delta.ScanExcluded, scanID); err != nil {
		return err
	}
	if err = changeCounts(ctx, tx, job, func(c *Counts) {
		c.Discovered += delta.Discovered
		c.TotalBytes += delta.TotalBytes
		c.ScanExcluded += delta.ScanExcluded
		c.ScanErrors += delta.ScanErrors
		c.Failed += delta.Failed
		c.Processed += delta.Processed
		c.ProcessedBytes += delta.ProcessedBytes
	}); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) scanProblem(ctx context.Context, job, root string, id int64, path, key string, problem error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, _, err = ensureFolder(ctx, tx, job, root, path, key, problem.Error()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE local_import_scan_directory SET state='failed',error_message=? WHERE id=?", problem.Error(), id); err != nil {
		return err
	}
	if err = changeCounts(ctx, tx, job, func(c *Counts) { c.ScanErrors++ }); err != nil {
		return err
	}
	return tx.Commit()
}

// Order numeric runs without loading a folder's complete file list into Go.
func naturalKey(name string) string {
	lower := strings.ToLower(name)
	var out strings.Builder
	for i := 0; i < len(lower); {
		if lower[i] >= '0' && lower[i] <= '9' {
			j := i
			for j < len(lower) && lower[j] >= '0' && lower[j] <= '9' {
				j++
			}
			n := strings.TrimLeft(lower[i:j], "0")
			fmt.Fprintf(&out, "0%08x%s%08x", len(n), n, j-i)
			i = j
		} else {
			out.WriteByte(lower[i])
			i++
		}
	}
	out.WriteByte(0)
	out.WriteString(name)
	return out.String()
}
