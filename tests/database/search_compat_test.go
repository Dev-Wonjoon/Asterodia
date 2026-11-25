package database_test

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

// These trigger statements independently capture the first shipped v9 shape.
func originalV9Triggers() map[string]string {
	out := map[string]string{}
	for _, spec := range []struct{ name, event, table, ids string }{
		{"media_insert", "AFTER INSERT", "media", "SELECT new.id"},
		{"media_update", "AFTER UPDATE OF custom_title,source_title", "media", "SELECT new.id"},
		{"media_delete", "AFTER DELETE", "media", "SELECT old.id"},
		{"file_insert", "AFTER INSERT", "media_file", "SELECT new.media_id"},
		{"file_update", "AFTER UPDATE OF media_id,title,profile_id,platform_id", "media_file", "SELECT old.media_id UNION SELECT new.media_id"},
		{"file_delete", "AFTER DELETE", "media_file", "SELECT old.media_id"},
		{"tag_link_insert", "AFTER INSERT", "media_tag", "SELECT new.media_id"},
		{"tag_link_delete", "AFTER DELETE", "media_tag", "SELECT old.media_id"},
		{"tag_update", "AFTER UPDATE OF name", "tag", "SELECT media_id FROM media_tag WHERE tag_id=new.id"},
		{"tag_delete", "BEFORE DELETE", "tag", "SELECT media_id FROM media_tag WHERE tag_id=old.id"},
		{"profile_update", "AFTER UPDATE OF owner_name,owner_id", "profiles", "SELECT media_id FROM media_file WHERE profile_id=new.id"},
		{"profile_delete", "BEFORE DELETE", "profiles", "SELECT media_id FROM media_file WHERE profile_id=old.id"},
		{"platform_update", "AFTER UPDATE OF name", "platform", "SELECT media_id FROM media_file WHERE platform_id=new.id"},
		{"platform_delete", "BEFORE DELETE", "platform", "SELECT media_id FROM media_file WHERE platform_id=old.id"},
	} {
		name := "search_" + spec.name
		out[name] = fmt.Sprintf("CREATE TRIGGER %s %s ON %s BEGIN INSERT OR IGNORE INTO search_dirty(media_id) %s; UPDATE search_state SET revision=revision+1 WHERE id=1; END", name, spec.event, spec.table, spec.ids)
	}
	out["search_document_delete"] = "CREATE TRIGGER search_document_delete AFTER DELETE ON search_document BEGIN INSERT OR IGNORE INTO search_gc VALUES(old.term_id); END"
	return out
}

func searchV9Fixture(t *testing.T, path, variant string) *sql.DB {
	t.Helper()
	current := latestDB(t, filepath.Join(filepath.Dir(path), "current.db"))
	rows, err := current.Query(`SELECT name,sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 ELSE 2 END,name`)
	if err != nil {
		t.Fatal(err)
	}
	var statements []string
	original := originalV9Triggers()
	for rows.Next() {
		var name, ddl string
		if err = rows.Scan(&name, &ddl); err != nil {
			t.Fatal(err)
		}
		if old, ok := beforeAlbumSchema(name, name, ddl); !ok {
			continue
		} else {
			ddl = old
		}
		if strings.HasPrefix(name, "search_fts_") {
			continue
		}
		if variant == "original" || variant == "original-index" {
			if old, ok := original[name]; ok {
				ddl = old
			}
			if variant == "original-index" && name == "idx_search_term_normalized" {
				ddl = "CREATE INDEX idx_search_term_normalized ON search_term(normalized,kind,id)"
			}
		}
		if variant == "guarded" {
			ddl = strings.ReplaceAll(ddl, "SELECT DISTINCT candidate.media_id", "SELECT candidate.media_id")
			ddl = strings.Replace(ddl, "AFTER UPDATE OF custom_title,source_title,created_at", "AFTER UPDATE OF custom_title,source_title", 1)
		}
		statements = append(statements, ddl+";")
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	current.Close()
	db := rawDB(t, path)
	execDB(t, db, strings.Join(statements, "\n"))
	execDB(t, db, "INSERT INTO search_state(id)VALUES(1);PRAGMA user_version=9")
	return db
}

func TestSearchV9CompatibilityMigration(t *testing.T) {
	for _, variant := range []string{"original", "original-index", "guarded", "canonical"} {
		for _, rollback := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/rollback-%t", variant, rollback), func(t *testing.T) {
				root := migrationRoot(t)
				path := filepath.Join(root, "v9.db")
				old := searchV9Fixture(t, path, variant)
				addModernMedia(t, old, 7, "https://example.test/search-preserve", "원본 제목", filepath.Join(root, "not-a-real-file.png"), "original")
				execDB(t, old, `UPDATE media SET custom_title='사용자 제목',revision=12 WHERE id=7;INSERT INTO tag(id,name)VALUES(71,'여행');INSERT INTO media_tag VALUES(7,71);INSERT INTO favorite(media_id,created_at)VALUES(7,1);INSERT INTO migration_run(id,target_version,source_fingerprint,schema_hash,completed_at)VALUES('old-v9',9,'old-fingerprint','old-schema',1)`)
				catalog := media.NewService(old)
				for {
					work, err := catalog.RefreshSearchIndex(t.Context(), 128)
					if err != nil {
						t.Fatal(err)
					}
					if !work {
						break
					}
				}
				uuid := textDB(t, old, "SELECT file_uuid FROM media_file WHERE media_id=7")
				triggerBefore := textDB(t, old, "SELECT sql FROM sqlite_master WHERE name='search_profile_update'")
				terms := countDB(t, old, "SELECT count(*) FROM search_term")
				old.Close()
				opts := database.MigrationOptions{BackupDirectory: filepath.Join(root, "backups")}
				if rollback {
					opts.Checkpoint = func(step string) error {
						if step == "after_search_trigger_repair" {
							return errors.New("fixture")
						}
						return nil
					}
					if _, err := database.OpenWithOptions(t.Context(), path, opts); err == nil {
						t.Fatal("전환 실패 주입 누락")
					}
					snapshot := rawDB(t, path)
					if countDB(t, snapshot, "PRAGMA user_version") != 9 || textDB(t, snapshot, "SELECT sql FROM sqlite_master WHERE name='search_profile_update'") != triggerBefore || countDB(t, snapshot, "SELECT count(*) FROM search_dirty") != 0 {
						t.Fatal("실패한 전환이 원본을 변경했습니다")
					}
					snapshot.Close()
					opts.Checkpoint = nil
				}
				db, err := database.OpenWithOptions(t.Context(), path, opts)
				if err != nil {
					t.Fatal(err)
				}
				if countDB(t, db, "PRAGMA user_version") != 13 || textDB(t, db, "SELECT custom_title FROM media WHERE id=7") != "사용자 제목" || countDB(t, db, "SELECT revision FROM media WHERE id=7") != 12 || textDB(t, db, "SELECT file_uuid FROM media_file WHERE media_id=7") != uuid || countDB(t, db, "SELECT count(*) FROM favorite WHERE media_id=7") != 1 || countDB(t, db, "SELECT count(*) FROM media_tag WHERE media_id=7") != 1 || countDB(t, db, "SELECT count(*) FROM migration_run WHERE id='old-v9'") != 1 || countDB(t, db, "SELECT count(*) FROM search_term") != terms || countDB(t, db, "SELECT count(*) FROM search_dirty WHERE media_id=7") != 1 {
					t.Fatal("v9 자료 또는 검색 색인을 보존하지 못했습니다")
				}
				if !strings.Contains(textDB(t, db, "SELECT sql FROM sqlite_master WHERE name='search_profile_update'"), "SELECT DISTINCT candidate.media_id") {
					t.Fatal("트리거 전환 누락")
				}
				backupPath := textDB(t, db, "SELECT backup_path FROM migration_run WHERE target_version=13")
				backup := rawDB(t, backupPath)
				if countDB(t, backup, "PRAGMA user_version") != 9 || textDB(t, backup, "SELECT sql FROM sqlite_master WHERE name='search_profile_update'") != triggerBefore || textDB(t, backup, "SELECT custom_title FROM media WHERE id=7") != "사용자 제목" {
					t.Fatal("v9 백업 원문 유실")
				}
				backup.Close()
				assertHealthyDB(t, db)
				db.Close()
				reopened, err := database.OpenWithOptions(t.Context(), path, opts)
				if err != nil {
					t.Fatal(err)
				}
				defer reopened.Close()
				if countDB(t, reopened, "SELECT count(*) FROM migration_run WHERE target_version=13") != 1 {
					t.Fatal("중복 전환")
				}
			})
		}
	}
}

func TestSearchV9UnknownSchemaIsPreserved(t *testing.T) {
	for _, change := range []string{"trigger", "table", "index"} {
		t.Run(change, func(t *testing.T) {
			root := migrationRoot(t)
			path := filepath.Join(root, "unknown-v9.db")
			db := searchV9Fixture(t, path, "original")
			switch change {
			case "trigger":
				execDB(t, db, "DROP TRIGGER search_profile_update;CREATE TRIGGER search_profile_update AFTER UPDATE ON profiles BEGIN SELECT 1;END")
			case "table":
				execDB(t, db, "ALTER TABLE search_term ADD COLUMN unexpected TEXT")
			case "index":
				execDB(t, db, "DROP INDEX idx_search_term_normalized;CREATE INDEX idx_search_term_normalized ON search_term(value)")
			}
			db.Close()
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			backups := filepath.Join(root, "backups")
			if opened, err := database.OpenWithOptions(t.Context(), path, database.MigrationOptions{BackupDirectory: backups}); err == nil {
				opened.Close()
				t.Fatal("알 수 없는 구조를 임의 복구했습니다")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Fatal("알 수 없는 DB를 변경했습니다", err)
			}
			if _, err = os.Stat(backups); !os.IsNotExist(err) {
				t.Fatal("검증 전에 백업/전환을 시작했습니다", err)
			}
		})
	}
}
