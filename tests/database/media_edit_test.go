package database_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/database"
)

func TestV6MediaEditMigrationPreservesHistoryAndRollsBack(t *testing.T) {
	for _, fault := range []bool{false, true} {
		t.Run(fmt.Sprint(fault), func(t *testing.T) {
			root := migrationRoot(t)
			current := latestDB(t, filepath.Join(root, "current.db"))
			rows, e := current.Query(`SELECT name,tbl_name,sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 ELSE 2 END,name`)
			if e != nil {
				t.Fatal(e)
			}
			var statements []string
			for rows.Next() {
				var name, table, ddl string
				if e = rows.Scan(&name, &table, &ddl); e != nil {
					t.Fatal(e)
				}
				if old, ok := beforeAlbumSchema(name, table, ddl); !ok {
					continue
				} else {
					ddl = old
				}
				if strings.HasPrefix(name, "search_") || strings.HasPrefix(table, "search_") || strings.HasPrefix(table, "media_edit_") || strings.HasPrefix(name, "edit_") {
					continue
				}
				statements = append(statements, strings.ReplaceAll(ddl, "'legacy','edited'", "'legacy'")+";")
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(root, "v6.db")
			old := rawDB(t, path)
			execDB(t, old, strings.Join(statements, "\n"))
			execDB(t, old, "PRAGMA user_version=6")
			addModernMedia(t, old, 7, "https://example.test/keep-v6", "제목", filepath.Join(root, "original.mp4"), "file")
			identity := textDB(t, old, "SELECT file_uuid FROM media_file WHERE media_id=7")
			execDB(t, old, "UPDATE sqlite_sequence SET seq=321 WHERE name='media'")
			execDB(t, old, "INSERT INTO migration_run(id,target_version,source_fingerprint,schema_hash,completed_at)VALUES('previous-v6',6,'keep','keep',1)")
			old.Close()
			opts := database.MigrationOptions{BackupDirectory: filepath.Join(root, "backups")}
			if fault {
				opts.Checkpoint = func(stage string) error {
					if stage == "before_commit" {
						return fmt.Errorf("injected")
					}
					return nil
				}
			}
			opened, e := database.OpenWithOptions(t.Context(), path, opts)
			if fault {
				if e == nil {
					opened.Close()
					t.Fatal("주입 실패가 확정됨")
				}
				snapshot := rawDB(t, path)
				if countDB(t, snapshot, "PRAGMA user_version") != 6 || textDB(t, snapshot, "SELECT file_uuid FROM media_file WHERE media_id=7") != identity {
					t.Fatal("롤백 중 원본 변경")
				}
				snapshot.Close()
				opts.Checkpoint = nil
				opened, e = database.OpenWithOptions(t.Context(), path, opts)
			}
			if e != nil {
				t.Fatal(e)
			}
			defer opened.Close()
			if countDB(t, opened, "PRAGMA user_version") != 16 || countDB(t, opened, "SELECT seq FROM sqlite_sequence WHERE name='media'") != 321 || textDB(t, opened, "SELECT file_uuid FROM media_file WHERE media_id=7") != identity || countDB(t, opened, "SELECT count(*) FROM migration_run WHERE id='previous-v6'") != 1 {
				t.Fatal("기존 파일·시퀀스·이력 유실")
			}
			assertHealthyDB(t, opened)
			backup := rawDB(t, textDB(t, opened, "SELECT backup_path FROM migration_run WHERE target_version=16"))
			if countDB(t, backup, "PRAGMA user_version") != 6 {
				t.Fatal("v6 백업 유실")
			}
		})
	}
}

func TestV7StreamingMigrationPreservesCachedSessions(t *testing.T) {
	for _, fault := range []bool{false, true} {
		t.Run(fmt.Sprint(fault), func(t *testing.T) {
			root := migrationRoot(t)
			current := latestDB(t, filepath.Join(root, "current.db"))
			rows, e := current.Query(`SELECT name,tbl_name,sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 ELSE 2 END,name`)
			if e != nil {
				t.Fatal(e)
			}
			var statements []string
			for rows.Next() {
				var name, table, ddl string
				if e = rows.Scan(&name, &table, &ddl); e != nil {
					t.Fatal(e)
				}
				if old, ok := beforeAlbumSchema(name, table, ddl); !ok {
					continue
				} else {
					ddl = old
				}
				if strings.HasPrefix(name, "search_") || strings.HasPrefix(table, "search_") || table == "media_edit_page" || table == "media_edit_selection" || table == "media_edit_range" {
					continue
				}
				ddl = strings.ReplaceAll(ddl, "frame_count>=0", "frame_count BETWEEN 0 AND 900")
				ddl = strings.ReplaceAll(ddl, "\n render_version integer NOT NULL DEFAULT 1 CHECK(render_version IN (1,2)),", "")
				ddl = strings.ReplaceAll(ddl, "\n preview integer NOT NULL DEFAULT 0 CHECK(preview IN (0,1)),", "")
				ddl = strings.ReplaceAll(ddl, "\n resources_json text NOT NULL DEFAULT '{}' CHECK(json_valid(resources_json)),", "")
				ddl = strings.ReplaceAll(ddl, "EXISTS(SELECT 1 FROM media_edit_page p WHERE p.session_id=s.id AND p.state IN ('pending','running')) OR ", "")
				statements = append(statements, ddl+";")
			}
			if e = rows.Err(); e != nil {
				t.Fatal(e)
			}
			rows.Close()
			path := filepath.Join(root, "v7.db")
			old := rawDB(t, path)
			execDB(t, old, strings.Join(statements, "\n"))
			execDB(t, old, "PRAGMA user_version=7")
			execDB(t, old, `INSERT INTO media_edit_session(id,source_uuid,source_path,source_title,request_json,state,width,height,frame_count,created_at,updated_at)VALUES('cached','source','missing.mp4','보관','{}','ready',160,120,10,1,1)`)
			execDB(t, old, `INSERT INTO media_edit_job(id,session_id,request_json,state,output_root,result_uuid,created_at,updated_at)VALUES('old-job','cached','{"deleted":[1,4]}','completed','output','result',1,1)`)
			old.Close()
			opts := database.MigrationOptions{BackupDirectory: filepath.Join(root, "backups")}
			if fault {
				opts.Checkpoint = func(stage string) error {
					if stage == "before_commit" {
						return fmt.Errorf("injected")
					}
					return nil
				}
			}
			migrated, e := database.OpenWithOptions(t.Context(), path, opts)
			if fault {
				if e == nil {
					migrated.Close()
					t.Fatal("fault committed")
				}
				snapshot := rawDB(t, path)
				if countDB(t, snapshot, "PRAGMA user_version") != 7 || textDB(t, snapshot, "SELECT request_json FROM media_edit_job WHERE id='old-job'") != `{"deleted":[1,4]}` {
					t.Fatal("rollback lost v7 edits")
				}
				snapshot.Close()
				opts.Checkpoint = nil
				migrated, e = database.OpenWithOptions(t.Context(), path, opts)
			}
			if e != nil {
				t.Fatal(e)
			}
			defer migrated.Close()
			assertHealthyDB(t, migrated)
			if countDB(t, migrated, "PRAGMA user_version") != 16 || countDB(t, migrated, "SELECT render_version FROM media_edit_session WHERE id='cached'") != 1 || textDB(t, migrated, "SELECT request_json FROM media_edit_job WHERE id='old-job'") != `{"deleted":[1,4]}` {
				t.Fatal("legacy cached sessions changed")
			}
			execDB(t, migrated, "UPDATE media_edit_session SET frame_count=100000 WHERE id='cached'")
		})
	}
}
