package database_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/maintenance"
)

func migrationRoot(t *testing.T) string {
	t.Helper()
	configured := os.Getenv("STORAGEBUCKET_TEST_ROOT")
	if !filepath.IsAbs(configured) {
		t.Fatal("STORAGEBUCKET_TEST_ROOT 절대 경로가 필요합니다")
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
func legacyDB(t *testing.T, path string, migrations int) *sql.DB {
	t.Helper()
	db := rawDB(t, path)
	execDB(t, db, "CREATE TABLE __drizzle_migrations(id SERIAL PRIMARY KEY,hash text NOT NULL,created_at numeric)")
	b, e := os.ReadFile(repoPath(t, "legacy", "electron", "drizzle", "meta", "_journal.json"))
	if e != nil {
		t.Fatal(e)
	}
	var journal struct {
		Entries []struct {
			Tag  string `json:"tag"`
			When int64  `json:"when"`
		} `json:"entries"`
	}
	if e = json.Unmarshal(b, &journal); e != nil {
		t.Fatal(e)
	}
	for _, entry := range journal.Entries[:migrations] {
		b, e = os.ReadFile(repoPath(t, "legacy", "electron", "drizzle", entry.Tag+".sql"))
		if e != nil {
			t.Fatal(e)
		}
		execDB(t, db, string(b))
		execDB(t, db, "INSERT INTO __drizzle_migrations(hash,created_at) VALUES(?,?)", fmt.Sprintf("%x", sha256.Sum256(b)), entry.When)
	}
	return db
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
func addLegacyMedia(t *testing.T, db *sql.DB, path string) {
	t.Helper()
	execDB(t, db, "INSERT INTO platform(id,name) VALUES(1,'Youtube')")
	execDB(t, db, "INSERT INTO profiles(id,owner_id,owner_name,platform_id,updated_at) VALUES(1,'owner','작성자',1,1700000000)")
	execDB(t, db, "INSERT INTO download_urls(id,url,video_id,created_at) VALUES(3,'https://example.test/watch?v=one','one',1700000000)")
	execDB(t, db, "INSERT INTO media(id,title,filepath,filesize,url_id,platform_id,profile_id,created_at,updated_at) VALUES(7,'기존 제목',?,12,3,1,1,1700000000,1700000001)", path)
	execDB(t, db, "INSERT INTO tag(id,name,updated_at) VALUES(2,'여행',1700000000)")
	execDB(t, db, "INSERT INTO media_tag(media_id,tag_id) VALUES(7,2)")
	execDB(t, db, "INSERT INTO favorite(id,media_id,created_at) VALUES(4,7,1700000000)")
}

func TestAutomaticLegacyMigration(t *testing.T) {
	root := migrationRoot(t)
	path := filepath.Join(root, "legacy.sqlite")
	db := legacyDB(t, path, 7)
	addLegacyMedia(t, db, filepath.Join(root, "old.mp4"))
	execDB(t, db, "UPDATE sqlite_sequence SET seq=100 WHERE name='media'")
	execDB(t, db, "UPDATE sqlite_sequence SET seq=200 WHERE name='download_urls'")
	execDB(t, db, "INSERT INTO download_queue(id,url,status,options,progress,created_at,updated_at) VALUES('old-job','https://example.test/other','downloading',?,50,1700000000,1700000001)", `"{\"quality\":\"720\"}"`)
	db.Close()
	migrated, e := database.Open(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	defer migrated.Close()
	if countDB(t, migrated, "PRAGMA user_version") != 6 {
		t.Fatal("버전 갱신 실패")
	}
	if countDB(t, migrated, "SELECT count(*) FROM media WHERE id=7 AND custom_title='기존 제목' AND created_at=1700000000000") != 1 {
		t.Fatal("미디어 값·시각 이관 실패")
	}
	if countDB(t, migrated, "SELECT count(*) FROM media_file WHERE id=7 AND media_id=7 AND storage_kind='unverified_legacy'") != 1 {
		t.Fatal("파일 관계 이관 실패")
	}
	if countDB(t, migrated, "SELECT count(*) FROM media_tag WHERE media_id=7 AND tag_id=2") != 1 || countDB(t, migrated, "SELECT count(*) FROM favorite WHERE media_id=7") != 1 {
		t.Fatal("태그·즐겨찾기 유실")
	}
	if countDB(t, migrated, "SELECT count(*) FROM download_queue WHERE id='old-job' AND state='interrupted' AND auto_history_unknown=1 AND auto_run_count IS NULL") != 1 {
		t.Fatal("기존 작업 이력 유실")
	}
	if countDB(t, migrated, "SELECT id FROM download_urls WHERE url='https://example.test/other'") <= 200 {
		t.Fatal("이전 URL 최고 ID 재사용")
	}
	if countDB(t, migrated, "SELECT seq FROM sqlite_sequence WHERE name='media_file'") < 100 {
		t.Fatal("이전 파일 최고 ID 재사용")
	}
	if countDB(t, migrated, "SELECT count(*) FROM legacy_record WHERE table_name='media' AND row_key='7'") != 1 {
		t.Fatal("원문 기록 누락")
	}
	backups, e := filepath.Glob(filepath.Join(root, "backups", "*.db"))
	if e != nil || len(backups) != 1 {
		t.Fatalf("백업 %v %v", backups, e)
	}
	backup := rawDB(t, backups[0])
	if countDB(t, backup, "PRAGMA user_version") != 0 || countDB(t, backup, "SELECT count(*) FROM media WHERE id=7") != 1 {
		t.Fatal("이관 전 백업 실패")
	}
	assertHealthyDB(t, migrated)
	migrated.Close()
	again, e := database.Open(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	again.Close()
	backups, _ = filepath.Glob(filepath.Join(root, "backups", "*.db"))
	if len(backups) != 1 {
		t.Fatal("최신 DB를 다시 이관했습니다")
	}
}
func TestPartialLegacyMigrationPreservesURL(t *testing.T) {
	root := migrationRoot(t)
	path := filepath.Join(root, "partial.sqlite")
	db := legacyDB(t, path, 1)
	execDB(t, db, "INSERT INTO media(id,title,filepath,url,created_at) VALUES(1,'partial',?,'https://example.test/original',1700000000)", filepath.Join(root, "old.jpg"))
	db.Close()
	migrated, e := database.Open(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	defer migrated.Close()
	if countDB(t, migrated, "SELECT count(*) FROM media_file WHERE source_url='https://example.test/original'") != 1 {
		t.Fatal("과거 URL 컬럼 유실")
	}
	assertHealthyDB(t, migrated)
}

func TestLegacyQueueTimestamps(t *testing.T) {
	cases := []struct {
		name                      string
		started, finished         any
		wantStarted, wantFinished any
	}{
		{"null", nil, nil, nil, nil},
		{"finished-only", nil, int64(1700000010), nil, int64(1700000010000)},
		{"seconds", int64(1700000000), int64(1700000010), int64(1700000000000), int64(1700000010000)},
		{"epoch", int64(0), int64(1), int64(0), int64(1000)},
		{"before-epoch", int64(-2), int64(-1), int64(-2000), int64(-1000)},
		{"mixed-text-and-number", "2023-11-14 22:13:20", int64(1700000010), int64(1700000000000), int64(1700000010000)},
	}
	for _, mode := range []string{"automatic", "manual-import"} {
		for _, tc := range cases {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				root := migrationRoot(t)
				path := filepath.Join(root, "legacy-times.db")
				source := legacyDB(t, path, 7)
				execDB(t, source, "INSERT INTO download_queue(id,url,status,options,created_at,updated_at,started_at,finished_at) VALUES('finished-job','https://example.test/times','completed','{}',1700000000,1700000010,?,?)", tc.started, tc.finished)
				source.Close()
				before := fileDigest(t, path)
				var db *sql.DB
				if mode == "automatic" {
					var e error
					db, e = database.Open(t.Context(), path)
					if e != nil {
						t.Fatal(e)
					}
					defer db.Close()
					if countDB(t, db, "SELECT count(*) FROM legacy_record WHERE table_name='download_queue' AND row_key='finished-job' AND json_extract(payload_json,'$.finished_at') IS ?", tc.finished) != 1 {
						t.Fatal("이관 전 시각 원문 유실")
					}
				} else {
					var service *database.Importer
					db, service, _ = importer(t, root)
					result, e := service.Import(t.Context(), path)
					if e != nil {
						t.Fatal(e)
					}
					if result.AddedHistory != 1 {
						t.Fatalf("이력 누락: %+v", result)
					}
					if before != fileDigest(t, path) {
						t.Fatal("선택한 원본 DB가 변경됐습니다")
					}
				}
				if countDB(t, db, "SELECT count(*) FROM download_queue WHERE state='completed' AND started_at IS ? AND finished_at IS ? AND created_at=1700000000000 AND updated_at=1700000010000", tc.wantStarted, tc.wantFinished) != 1 {
					t.Fatal("구버전 초 단위 시각 변환 실패")
				}
				assertHealthyDB(t, db)
			})
		}
	}
}

func TestMigrationRollbackAndVersionChecks(t *testing.T) {
	t.Run("normalization-collision", func(t *testing.T) {
		root := migrationRoot(t)
		path := filepath.Join(root, "collision.db")
		db := legacyDB(t, path, 7)
		addLegacyMedia(t, db, filepath.Join(root, "old.mp4"))
		execDB(t, db, "INSERT INTO download_urls(url,created_at) VALUES('https://EXAMPLE.test/watch?v=one',1700000000)")
		db.Close()
		if opened, e := database.Open(t.Context(), path); e == nil {
			opened.Close()
			t.Fatal("정규화 충돌을 허용했습니다")
		}
		db = rawDB(t, path)
		if countDB(t, db, "PRAGMA user_version") != 0 || countDB(t, db, "SELECT count(*) FROM media WHERE title='기존 제목'") != 1 {
			t.Fatal("실패 시 원본 DB가 변경됐습니다")
		}
		assertHealthyDB(t, db)
	})
	t.Run("future-and-unknown", func(t *testing.T) {
		root := migrationRoot(t)
		for _, version := range []int{0, 1, 99} {
			path := filepath.Join(root, fmt.Sprintf("unknown-%d.db", version))
			db := rawDB(t, path)
			execDB(t, db, "CREATE TABLE user_data(id INTEGER PRIMARY KEY,value TEXT)")
			execDB(t, db, "INSERT INTO user_data VALUES(1,'keep')")
			execDB(t, db, fmt.Sprintf("PRAGMA user_version=%d", version))
			db.Close()
			if opened, e := database.Open(t.Context(), path); e == nil {
				opened.Close()
				t.Fatal("미확인 DB를 변경했습니다")
			}
			db = rawDB(t, path)
			if countDB(t, db, "SELECT count(*) FROM user_data WHERE value='keep'") != 1 {
				t.Fatal("사용자 DB 유실")
			}
		}
	})
	t.Run("v2-to-v3", func(t *testing.T) {
		root := migrationRoot(t)
		path := filepath.Join(root, "v2.db")
		b, e := os.ReadFile(repoPath(t, "internal", "database", "schema.sql"))
		if e != nil {
			t.Fatal(e)
		}
		ddl, _, ok := strings.Cut(string(b), "CREATE TABLE download_job_storage")
		if !ok {
			t.Fatal("v2 경계 누락")
		}
		db := rawDB(t, path)
		execDB(t, db, ddl)
		execDB(t, db, "PRAGMA user_version=2")
		db.Close()
		db, e = database.Open(t.Context(), path)
		if e != nil {
			t.Fatal(e)
		}
		defer db.Close()
		if countDB(t, db, "SELECT count(*) FROM sqlite_master WHERE name='download_job_storage'") != 1 {
			t.Fatal("저장소 테이블 누락")
		}
		assertHealthyDB(t, db)
	})
}

func TestV2MigrationSuspendsOldWorkAndInfersStorage(t *testing.T) {
	root := migrationRoot(t)
	path := filepath.Join(root, "v2-work.db")
	b, e := os.ReadFile(repoPath(t, "internal", "database", "schema.sql"))
	if e != nil {
		t.Fatal(e)
	}
	ddl, _, _ := strings.Cut(string(b), "CREATE TABLE download_job_storage")
	db := rawDB(t, path)
	execDB(t, db, ddl)
	execDB(t, db, "PRAGMA user_version=2")
	oldRoot := filepath.Join(root, "old-downloads")
	mediaPath := filepath.Join(oldRoot, ".storagebucket", "files", strings.Repeat("a", 64), "item-0001", "media.mp4")
	if e = os.MkdirAll(filepath.Dir(mediaPath), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(mediaPath, []byte("must keep"), 0600); e != nil {
		t.Fatal(e)
	}
	execDB(t, db, "INSERT INTO download_urls(id,url,normalized_url,normalization_version,created_at) VALUES(1,'https://example.test/old','https://example.test/old',1,1000)")
	execDB(t, db, "INSERT INTO download_queue(id,request_url_id,original_url,mode,state,options_json,cleanup_required,created_at) VALUES('old',1,'https://example.test/old','create','running','{}',1,1000)")
	execDB(t, db, `INSERT INTO download_attempt(id,job_id,attempt_no,"trigger",phase,manifest_path,started_at) VALUES('attempt','old',1,'manual_initial','downloading',?,1000)`, filepath.Join(oldRoot, ".storagebucket", "staging", "attempt", "manifest.json"))
	execDB(t, db, "UPDATE download_queue SET current_attempt_id='attempt' WHERE id='old'")
	payload, _ := json.Marshal(map[string]any{"paths": []string{mediaPath}})
	execDB(t, db, "INSERT INTO maintenance_task(id,kind,state,owner_kind,payload_json,created_at,updated_at) VALUES('cleanup','deletion_cleanup','pending','system',?,1000,1000)", string(payload))
	execDB(t, db, "INSERT INTO local_import_job(id,root_path,root_key,state,counts_json,created_at,updated_at) VALUES('local',?,'local','running','{}',1000,1000)", root)
	db.Close()
	db, e = database.OpenWithOptions(t.Context(), path, database.MigrationOptions{BackupDirectory: filepath.Join(root, "backups"), LegacyStorageRoot: filepath.Join(root, "different-default")})
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if countDB(t, db, "SELECT count(*) FROM download_job_storage WHERE job_id='old' AND storage_root=?", oldRoot) != 1 {
		t.Fatal("실제 저장 루트 유실")
	}
	if countDB(t, db, "SELECT count(*) FROM download_queue WHERE state='interrupted' AND cleanup_required=0 AND json_extract(options_json,'$._importedHistory')=1") != 1 {
		t.Fatal("구버전 작업이 실행 가능 상태입니다")
	}
	if countDB(t, db, "SELECT count(*) FROM download_attempt WHERE outcome='interrupted' AND phase='finished'") != 1 || countDB(t, db, "SELECT count(*) FROM local_import_job WHERE state='interrupted'") != 1 {
		t.Fatal("미완료 작업 이관 실패")
	}
	if countDB(t, db, "SELECT count(*) FROM legacy_record WHERE table_name='migration-v2:download_queue' AND json_extract(payload_json,'$.state')='running'") != 1 {
		t.Fatal("원래 작업 상태 유실")
	}
	service := download.NewService(db, func() string { return oldRoot }, nil, nil)
	defer service.Close()
	if r := service.Run(t.Context(), download.RunRequest{JobID: "old", Trigger: "manual_retry"}); r.OK || r.Error.Code != "IMPORTED_HISTORY" {
		t.Fatalf("이관한 작업 실행 허용: %+v", r)
	}
	cleaner := maintenance.NewCleaner(db)
	defer cleaner.Close()
	if e = cleaner.RunOnce(t.Context()); e != nil {
		t.Fatal(e)
	}
	if data, e := os.ReadFile(mediaPath); e != nil || string(data) != "must keep" {
		t.Fatal("구버전 정리 예약이 파일을 삭제했습니다")
	}
	if countDB(t, db, "SELECT try_count FROM maintenance_task WHERE id='cleanup'") != 0 {
		t.Fatal("보존된 정리 예약을 실행했습니다")
	}
	assertHealthyDB(t, db)
}

func TestMigrationProcessCrash(t *testing.T) {
	if os.Getenv("STORAGEBUCKET_MIGRATION_CRASH") == "1" {
		path := os.Getenv("STORAGEBUCKET_MIGRATION_FIXTURE")
		configured := os.Getenv("STORAGEBUCKET_TEST_ROOT")
		relative, e := filepath.Rel(configured, path)
		if e != nil || !filepath.IsAbs(configured) || filepath.IsAbs(relative) || strings.HasPrefix(relative, "..") {
			t.Fatal("테스트 전용 DB가 필요합니다")
		}
		db := rawDB(t, path)
		_, e = database.Migrate(t.Context(), db, database.MigrationOptions{BackupDirectory: filepath.Join(filepath.Dir(path), "backups"), LegacyStorageRoot: filepath.Join(filepath.Dir(path), "downloads"), Checkpoint: func(stage string) error {
			if stage == "before_commit" {
				os.Exit(73)
			}
			return nil
		}})
		t.Fatalf("비정상 종료 지점에 도달하지 못했습니다: %v", e)
	}
	root := migrationRoot(t)
	path := filepath.Join(root, "crash.db")
	db := legacyDB(t, path, 7)
	addLegacyMedia(t, db, filepath.Join(root, "original.mp4"))
	db.Close()
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMigrationProcessCrash$", "-test.count=1")
	command.Env = append(os.Environ(), "STORAGEBUCKET_MIGRATION_CRASH=1", "STORAGEBUCKET_MIGRATION_FIXTURE="+path)
	output, err := command.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 73 {
		t.Fatalf("예상하지 않은 종료: %v %s", err, output)
	}
	db = rawDB(t, path)
	if countDB(t, db, "PRAGMA user_version") != 0 || countDB(t, db, "SELECT count(*) FROM media WHERE id=7 AND title='기존 제목'") != 1 {
		t.Fatal("비정상 종료 후 기존 데이터 유실")
	}
	assertHealthyDB(t, db)
	db.Close()
	db, err = database.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if countDB(t, db, "PRAGMA user_version") != 6 || countDB(t, db, "SELECT count(*) FROM media_file WHERE media_id=7") != 1 {
		t.Fatal("재시작 이관 실패")
	}
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
	command.Env = append(os.Environ(), "STORAGEBUCKET_MIGRATION_CRASH=1", "STORAGEBUCKET_MIGRATION_FIXTURE="+path)
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
	if countDB(t, db, "PRAGMA user_version") != 6 || countDB(t, db, "SELECT count(*) FROM media_file WHERE media_id=7 AND directory_id IS NOT NULL AND length(file_uuid)=36 AND length(filename_token)=8") != 1 {
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
