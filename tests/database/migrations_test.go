package database_test

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/database"
)

func migrationRoot(t *testing.T) string {
	t.Helper()
	configured := os.Getenv("ASTERODIA_TEST_ROOT")
	if !filepath.IsAbs(configured) {
		t.Fatal("ASTERODIA_TEST_ROOT 절대 경로가 필요합니다")
	}
	_, source, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	resolved, e := filepath.EvalSymlinks(configured)
	if e != nil {
		t.Fatal(e)
	}
	allowed := filepath.Join(repo, ".test-data", "database-open")
	relative, e := filepath.Rel(allowed, resolved)
	if e != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("DB 테스트 전용 경로가 필요합니다")
	}
	root, e := os.MkdirTemp(resolved, "migration-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	return root
}
func repoPath(t *testing.T, parts ...string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(append([]string{filepath.Dir(file), "..", ".."}, parts...)...)
}
func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(1)
	if _, e = db.Exec("PRAGMA foreign_keys=ON;PRAGMA temp_store=MEMORY"); e != nil {
		db.Close()
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func execDB(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	// Historical fixtures start from latestDB, then remove newer schema features.
	// Keep additive v15 objects out of fixtures representing shipped older versions.
	if match := regexp.MustCompile(`(?i)PRAGMA\s+user_version\s*=\s*(\d+)`).FindStringSubmatch(query); len(match) > 0 {
		version, _ := strconv.Atoi(match[1])
		if version < 16 {
			if _, err := db.Exec(`DROP TRIGGER IF EXISTS file_metadata_insert;DROP TRIGGER IF EXISTS file_metadata_update;DROP VIEW IF EXISTS gallery_file_membership;DROP TABLE IF EXISTS file_metadata;DROP INDEX IF EXISTS idx_media_title_order;`); err != nil {
				t.Fatal(err)
			}
		}
		if version < 15 {
			stripV15Fixture(t, db)
		}
	}
	if _, e := db.Exec(query, args...); e != nil {
		t.Fatalf("SQL 실패: %v\n%s", e, query)
	}
}
func countDB(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if e := db.QueryRow(query, args...).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func assertHealthyDB(t *testing.T, db *sql.DB) {
	t.Helper()
	if n := countDB(t, db, "SELECT count(*) FROM pragma_foreign_key_check()"); n != 0 {
		t.Fatalf("외래 키 오류 %d", n)
	}
	var s string
	if e := db.QueryRow("PRAGMA integrity_check").Scan(&s); e != nil || s != "ok" {
		t.Fatalf("무결성 %q %v", s, e)
	}
}
func latestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, e := database.Open(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func TestMigrationProcessCrash(t *testing.T) {
	if os.Getenv("ASTERODIA_MIGRATION_CRASH") == "1" {
		path := os.Getenv("ASTERODIA_MIGRATION_FIXTURE")
		configured := os.Getenv("ASTERODIA_TEST_ROOT")
		relative, e := filepath.Rel(configured, path)
		if e != nil || !filepath.IsAbs(configured) || filepath.IsAbs(relative) || strings.HasPrefix(relative, "..") {
			t.Fatal("테스트 전용 DB가 필요합니다")
		}
		db := rawDB(t, path)
		_, e = database.Migrate(t.Context(), db, database.MigrationOptions{BackupDirectory: filepath.Join(filepath.Dir(path), "backups"), Checkpoint: func(stage string) error {
			if stage == "before_commit" {
				os.Exit(73)
			}
			return nil
		}})
		t.Fatalf("비정상 종료 지점에 도달하지 못했습니다: %v", e)
	}
	t.Skip("subprocess helper for migration crash verification")
}

func TestV4MigrationProcessCrash(t *testing.T) {
	root := migrationRoot(t)
	path := filepath.Join(root, "v4-crash.db")
	db := rawDB(t, path)
	execDB(t, db, historicalV4(t))
	execDB(t, db, "PRAGMA user_version=4")
	addModernMedia(t, db, 7, "https://example.test/post/crash", "게시글", filepath.Join(root, "original.mp4"), "original-file")
	db.Close()
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMigrationProcessCrash$", "-test.count=1")
	command.Env = append(os.Environ(), "ASTERODIA_MIGRATION_CRASH=1", "ASTERODIA_MIGRATION_FIXTURE="+path)
	output, err := command.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 73 {
		t.Fatalf("예상하지 않은 종료: %v %s", err, output)
	}
	db = rawDB(t, path)
	if countDB(t, db, "PRAGMA user_version") != 4 || countDB(t, db, "SELECT count(*) FROM sqlite_master WHERE name='media_directory'") != 0 || countDB(t, db, "SELECT count(*) FROM media_file WHERE media_id=7 AND file_key='original-file'") != 1 {
		t.Fatal("v4 이관 중 비정상 종료 후 기존 데이터 또는 스키마 유실")
	}
	assertHealthyDB(t, db)
	db.Close()
	db, err = database.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if countDB(t, db, "PRAGMA user_version") != 16 || countDB(t, db, "SELECT count(*) FROM media_file WHERE media_id=7 AND directory_id IS NOT NULL AND length(file_uuid)=36 AND length(filename_token)=8") != 1 {
		t.Fatal("v4 이관 재시작 실패")
	}
	assertHealthyDB(t, db)
}

func TestUnknownFTSPrefixTableIsRejected(t *testing.T) {
	root := migrationRoot(t)
	path := filepath.Join(root, "unknown-fts.db")
	db := latestDB(t, path)
	execDB(t, db, "CREATE TABLE media_fts_custom(value TEXT)")
	execDB(t, db, "INSERT INTO media_fts_custom VALUES('keep')")
	db.Close()
	if opened, e := database.Open(t.Context(), path); e == nil {
		opened.Close()
		t.Fatal("미확인 테이블을 무시했습니다")
	}
	db = rawDB(t, path)
	if countDB(t, db, "SELECT count(*) FROM media_fts_custom WHERE value='keep'") != 1 {
		t.Fatal("미확인 데이터 유실")
	}
}

func stripV15Fixture(t *testing.T, db *sql.DB) {
	t.Helper()
	var exists int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='media_platform' AND type='table'").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists == 0 {
		return
	}
	var view string
	if err := db.QueryRow("SELECT sql FROM sqlite_master WHERE name='search_source' AND type='view'").Scan(&view); err == nil {
		if at := strings.Index(view, "UNION SELECT mp.media_id,'platform',p.name"); at >= 0 {
			if _, err = db.Exec("DROP VIEW search_source;" + strings.TrimSpace(view[:at]) + ";"); err != nil {
				t.Fatal(err)
			}
		}
	} else if err != sql.ErrNoRows {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP VIEW IF EXISTS media_platform_sources;DROP TRIGGER IF EXISTS manual_platform_insert;DROP TRIGGER IF EXISTS manual_platform_delete;DROP TABLE media_platform;
 DROP INDEX IF EXISTS idx_file_thumbnail_path;DROP INDEX IF EXISTS idx_file_preview_path;DROP INDEX IF EXISTS idx_download_target_media;DROP INDEX IF EXISTS idx_media_tag_reverse;
 DROP INDEX IF EXISTS idx_edit_source_file;DROP INDEX IF EXISTS idx_move_media_state;DROP INDEX IF EXISTS idx_import_folder_state;`); err != nil {
		t.Fatal(err)
	}
}
