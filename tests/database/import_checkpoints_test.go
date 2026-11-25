package database_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/localimport"
)

// Construct the historical v5 fixture by removing only the explicitly listed
// v6 additions. Production's strict v5 validator independently checks the DDL.
func historicalV5DB(t *testing.T, filename string) *sql.DB {
	t.Helper()
	root := migrationRoot(t)
	current := latestDB(t, filepath.Join(root, "catalog.db"))
	rows, err := current.Query(`SELECT name,tbl_name,sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 ELSE 2 END,name`)
	if err != nil {
		t.Fatal(err)
	}
	var statements []string
	for rows.Next() {
		var name, table, ddl string
		if err = rows.Scan(&name, &table, &ddl); err != nil {
			break
		}
		if old, ok := beforeAlbumSchema(name, table, ddl); !ok {
			continue
		} else {
			ddl = old
		}
		if strings.HasPrefix(name, "search_") || strings.HasPrefix(table, "search_") || strings.HasPrefix(table, "media_edit_") || strings.HasPrefix(name, "edit_") {
			continue
		}
		ddl = strings.ReplaceAll(ddl, "'legacy','edited'", "'legacy'")
		if table == "local_import_scan_directory" || name == "idx_import_target_work" || name == "idx_import_folder_work" || name == "idx_file_path_lookup" || name == "idx_file_canonical_lookup" {
			continue
		}
		if name == "local_import_job" {
			for _, column := range []string{"pause_requested_at", "scan_complete", "work_version", "current_path", "error_message", "active_millis", "run_started_at"} {
				lines := strings.Split(ddl, "\n")
				filtered := lines[:0]
				for _, line := range lines {
					if !strings.HasPrefix(strings.TrimSpace(line), `"`+column+`" `) {
						filtered = append(filtered, line)
					}
				}
				ddl = strings.Join(filtered, "\n")
			}
			ddl = strings.ReplaceAll(ddl, "'completed','completed_with_errors','paused'", "'completed'")
		}
		if name == "local_import_attempt" || name == "local_import_folder" {
			ddl = strings.ReplaceAll(ddl, "'completed','completed_with_errors'", "'completed'")
			ddl = strings.ReplaceAll(ddl, "outcome IN ('completed')", "outcome = 'completed'")
		}
		if name == "local_import_target" {
			lines := strings.Split(ddl, "\n")
			filtered := lines[:0]
			for _, line := range lines {
				v := strings.TrimSpace(line)
				if !strings.HasPrefix(v, `"sort_key" `) && !strings.HasPrefix(v, `"result_kind" `) && !strings.HasPrefix(v, `"refresh_requested" `) {
					filtered = append(filtered, line)
				}
			}
			ddl = strings.Join(filtered, "\n")
		}
		if name == "v2_media_delete" {
			ddl = strings.Replace(ddl, "      JOIN local_import_job j ON j.id = f.job_id\n", "", 1)
			ddl = strings.Replace(ddl, "(j.state = 'running' AND f.state IN ('pending','running','interrupted'))", "f.state IN ('pending','running')", 1)
		}
		statements = append(statements, ddl+";")
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	db := rawDB(t, filename)
	execDB(t, db, strings.Join(statements, "\n"))
	execDB(t, db, "PRAGMA user_version=5")
	return db
}
func TestV5CheckpointMigrationAndRollback(t *testing.T) {
	for _, fault := range []string{"", "after_import_checkpoint_upgrade", "before_commit"} {
		t.Run("checkpoint-"+fault, func(t *testing.T) {
			root := migrationRoot(t)
			filename := filepath.Join(root, "v5.db")
			db := historicalV5DB(t, filename)
			addModernMedia(t, db, 7, "https://example.test/retained", "게시글", filepath.Join(root, "original.mp4"), "keep")
			identity := textDB(t, db, "SELECT file_uuid FROM media_file WHERE media_id=7")
			token := textDB(t, db, "SELECT filename_token FROM media_file WHERE media_id=7")
			execDB(t, db, "INSERT INTO local_import_job(id,root_path,root_key,state,counts_json,created_at,updated_at) VALUES('old',?,'old-key','ready','{}',1,1)", root)
			execDB(t, db, "INSERT INTO local_import_folder(id,job_id,directory_path,directory_key,relative_path,state) VALUES('folder','old',?,'old-key','.','pending')", root)
			execDB(t, db, "INSERT INTO local_import_attempt(id,import_folder_id,attempt_no,phase,commit_token,started_at) VALUES('attempt','folder',1,'inspecting','old-token',1)")
			execDB(t, db, "UPDATE local_import_folder SET current_attempt_id='attempt' WHERE id='folder'")
			execDB(t, db, "INSERT INTO local_import_target(id,attempt_id,path_key,relative_path,expected_stat_json,state) VALUES('target','attempt','file','original.mp4','{}','pending')")
			execDB(t, db, "INSERT INTO migration_run(id,target_version,source_fingerprint,schema_hash,completed_at) VALUES('v5-history',5,'keep','keep',1)")
			db.Close()
			opts := database.MigrationOptions{BackupDirectory: filepath.Join(root, "backups")}
			if fault != "" {
				opts.Checkpoint = func(stage string) error {
					if stage == fault {
						return fmt.Errorf("injected %s", stage)
					}
					return nil
				}
			}
			opened, err := database.OpenWithOptions(t.Context(), filename, opts)
			if fault != "" {
				if err == nil {
					opened.Close()
					t.Fatal("이관 실패가 반영되지 않았습니다")
				}
				old := rawDB(t, filename)
				if countDB(t, old, "PRAGMA user_version") != 5 || countDB(t, old, "SELECT count(*) FROM sqlite_master WHERE name='local_import_scan_directory'") != 0 || textDB(t, old, "SELECT file_uuid FROM media_file WHERE media_id=7") != identity {
					t.Fatal("v5 롤백 실패")
				}
				assertHealthyDB(t, old)
				old.Close()
				opened, err = database.Open(t.Context(), filename)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Close()
			if countDB(t, opened, "PRAGMA user_version") != 13 || textDB(t, opened, "SELECT file_uuid FROM media_file WHERE media_id=7") != identity || textDB(t, opened, "SELECT filename_token FROM media_file WHERE media_id=7") != token || countDB(t, opened, "SELECT count(*) FROM migration_run WHERE id='v5-history' AND target_version=5") != 1 {
				t.Fatal("식별자 또는 이전 이관 이력 유실")
			}
			if countDB(t, opened, "SELECT count(*) FROM local_import_job WHERE id='old' AND work_version=1 AND scan_complete=1") != 1 || countDB(t, opened, "SELECT count(*) FROM local_import_target WHERE id='target'") != 1 {
				t.Fatal("이전 작업 기록 유실")
			}
			service := localimport.NewService(opened, nil, nil, nil)
			defer service.Close()
			if result := service.Resume(t.Context(), localimport.Request{JobID: "old"}); result.OK {
				t.Fatal("이전 폴더 단위 작업을 파일 단위 재개로 오인했습니다")
			}
			backup := rawDB(t, textDB(t, opened, "SELECT backup_path FROM migration_run WHERE target_version=13"))
			defer backup.Close()
			if countDB(t, backup, "PRAGMA user_version") != 5 {
				t.Fatal("v5 백업 유실")
			}
			assertHealthyDB(t, opened)
		})
	}
}
