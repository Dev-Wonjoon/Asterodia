package database_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

func TestSearchMigrationV8(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[rollback], func(t *testing.T) {
			root := migrationRoot(t)
			current := latestDB(t, filepath.Join(root, "current.db"))
			rows, err := current.Query(`SELECT name,tbl_name,sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 ELSE 2 END,name`)
			if err != nil {
				t.Fatal(err)
			}
			var statements []string
			for rows.Next() {
				var name, table, ddl string
				if err = rows.Scan(&name, &table, &ddl); err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(name, "search_") || strings.HasPrefix(name, "idx_search_") || strings.HasPrefix(table, "search_") {
					continue
				}
				statements = append(statements, ddl+";")
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			current.Close()
			path := filepath.Join(root, "v8.db")
			old := rawDB(t, path)
			execDB(t, old, strings.Join(statements, "\n"))
			execDB(t, old, "PRAGMA user_version=8;INSERT INTO media(id,source_kind,source_title,custom_title,created_at)VALUES(7,'legacy','원본 여행','사용자 제목',1)")
			old.Close()
			opts := database.MigrationOptions{BackupDirectory: filepath.Join(root, "backups")}
			if rollback {
				opts.Checkpoint = func(step string) error {
					if step == "before_commit" {
						return errors.New("fixture")
					}
					return nil
				}
				if _, err = database.OpenWithOptions(t.Context(), path, opts); err == nil {
					t.Fatal("실패 주입 누락")
				}
				snapshot := rawDB(t, path)
				if countDB(t, snapshot, "PRAGMA user_version") != 8 || countDB(t, snapshot, "SELECT count(*) FROM sqlite_master WHERE name='search_term'") != 0 {
					t.Fatal("부분 마이그레이션")
				}
				snapshot.Close()
				opts.Checkpoint = nil
			}
			db, err := database.OpenWithOptions(t.Context(), path, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if countDB(t, db, "PRAGMA user_version") != 10 || textDB(t, db, "SELECT custom_title FROM media WHERE id=7") != "사용자 제목" || countDB(t, db, "SELECT count(*) FROM search_dirty") != 1 {
				t.Fatal("검색 마이그레이션에서 자료/큐 누락")
			}
			catalog := media.NewService(db)
			result := catalog.SearchSuggest(t.Context(), media.SearchSuggestRequest{Text: "행"})
			if !result.OK || len(result.Data.(media.Object)["data"].([]media.Object)) != 1 {
				t.Fatal(result)
			}
			for {
				work, err := catalog.RefreshSearchIndex(t.Context(), 128)
				if err != nil {
					t.Fatal(err)
				}
				if !work {
					break
				}
			}
			assertHealthyDB(t, db)
			backup := rawDB(t, textDB(t, db, "SELECT backup_path FROM migration_run WHERE target_version=10"))
			defer backup.Close()
			if countDB(t, backup, "PRAGMA user_version") != 8 || textDB(t, backup, "SELECT custom_title FROM media WHERE id=7") != "사용자 제목" {
				t.Fatal("백업 변경")
			}
		})
	}
}

func TestSearchSchemaValidation(t *testing.T) {
	root := migrationRoot(t)
	path := filepath.Join(root, "unknown-search.db")
	db := latestDB(t, path)
	execDB(t, db, "CREATE TABLE search_fts_extra(id integer)")
	db.Close()
	if opened, err := database.Open(t.Context(), path); err == nil {
		opened.Close()
		t.Fatal("알 수 없는 검색 테이블을 허용했습니다")
	}
}
