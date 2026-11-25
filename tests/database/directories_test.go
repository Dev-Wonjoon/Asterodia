package database_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"github.com/google/uuid"
)

func historicalV4(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(repoPath(t, "internal", "database", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	// Frozen v4 changes over the retained v3 fixture, not the current schema.
	s := strings.Replace(string(raw), "kind = 'image' AND canonical_path_key", "kind IN ('image','video','audio') AND canonical_path_key", 1)
	return strings.Replace(s, "AND width IS NOT NULL AND height IS NOT NULL)),", "AND (kind = 'audio' OR (width IS NOT NULL AND height IS NOT NULL)) AND (kind = 'image' OR duration IS NOT NULL))),", 1)
}
func textDB(t *testing.T, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, stmt string, args ...any) string {
	t.Helper()
	var value string
	if err := q.QueryRowContext(t.Context(), stmt, args...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestV4DirectoryMigrationAndRollback(t *testing.T) {
	for _, fault := range []string{"", "after_directory_backfill", "before_commit"} {
		t.Run("checkpoint-"+fault, func(t *testing.T) {
			root := migrationRoot(t)
			filename := filepath.Join(root, "v4.db")
			db := rawDB(t, filename)
			execDB(t, db, historicalV4(t))
			execDB(t, db, "PRAGMA user_version=4")
			folder := filepath.Join(root, "missing-originals")
			execDB(t, db, "INSERT INTO media(id,source_kind,source_title,custom_title,created_at,revision) VALUES(9,'local','source','사용자 제목',1,7)")
			execDB(t, db, "INSERT INTO local_folder(id,media_id,source_title,revision) VALUES(4,9,'source',3)")
			execDB(t, db, "INSERT INTO local_folder_location(id,local_folder_id,media_id,directory_path,directory_key,key_version,state) VALUES(6,4,9,?,'old-key',1,'active')", folder)
			execDB(t, db, `INSERT INTO media_file(id,media_id,file_key,storage_kind,filepath,canonical_path_key,relative_path,local_folder_location_id,filesize,mtime_ns,hash_algorithm,content_hash,kind,position,detected_format,width,height,created_at) VALUES(11,9,'old-photo','external_reference',?,'old-file-key','photo.png',6,10,'77','sha256',?,'image',0,'png',8,6,1)`, filepath.Join(folder, "photo.png"), strings.Repeat("a", 64))
			execDB(t, db, "UPDATE media SET cover_mode='manual',cover_file_id=11 WHERE id=9")
			execDB(t, db, "INSERT INTO tag(id,name) VALUES(3,'태그'); INSERT INTO media_tag(media_id,tag_id) VALUES(9,3); INSERT INTO favorite(media_id,created_at) VALUES(9,1); UPDATE sqlite_sequence SET seq=500 WHERE name='media_file'")
			execDB(t, db, "INSERT INTO migration_run(id,target_version,source_fingerprint,schema_hash,completed_at) VALUES('previous',4,'keep','keep',1)")
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
					t.Fatal("이관 오류가 반영되지 않았습니다")
				}
				original := rawDB(t, filename)
				if countDB(t, original, "PRAGMA user_version") != 4 || countDB(t, original, "SELECT count(*) FROM sqlite_master WHERE name='media_directory'") != 0 {
					t.Fatal("실패한 이관의 DDL이 남았습니다")
				}
				if countDB(t, original, "SELECT count(*) FROM media_file WHERE id=11 AND local_folder_location_id=6") != 1 {
					t.Fatal("실패가 기존 파일 연결을 변경했습니다")
				}
				assertHealthyDB(t, original)
				original.Close()
				opened, err = database.OpenWithOptions(t.Context(), filename, database.MigrationOptions{BackupDirectory: opts.BackupDirectory})
			}
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Close()
			if countDB(t, opened, "PRAGMA user_version") != 13 || countDB(t, opened, "SELECT count(*) FROM migration_run WHERE id='previous' AND target_version=4") != 1 {
				t.Fatal("버전 또는 기존 이관 이력 유실")
			}
			if countDB(t, opened, "SELECT count(*) FROM media WHERE id=9 AND custom_title='사용자 제목' AND cover_file_id=11 AND revision=7") != 1 || countDB(t, opened, "SELECT count(*) FROM media_tag WHERE media_id=9 AND tag_id=3") != 1 || countDB(t, opened, "SELECT count(*) FROM favorite WHERE media_id=9") != 1 {
				t.Fatal("편집·표지·태그·즐겨찾기 유실")
			}
			if countDB(t, opened, "SELECT seq FROM sqlite_sequence WHERE name='media_file'") != 500 {
				t.Fatal("시퀀스 유실")
			}
			if countDB(t, opened, "SELECT count(*) FROM media_file f JOIN media_directory d ON d.id=f.directory_id JOIN local_folder_location l ON l.directory_id=d.id WHERE f.id=11 AND l.id=6 AND d.registered=1 AND d.key_version=0 AND d.import_media_id=9") != 1 {
				t.Fatal("기록된 폴더 연결 유실")
			}
			if _, err = os.Stat(folder); !os.IsNotExist(err) {
				t.Fatal("이관이 원본 폴더를 만들었습니다")
			}
			identity := textDB(t, opened, "SELECT file_uuid FROM media_file WHERE id=11")
			parsed, err := uuid.Parse(identity)
			if err != nil || parsed.Version() != 4 {
				t.Fatal("안정 파일 식별자 생성 실패")
			}
			token := textDB(t, opened, "SELECT filename_token FROM media_file WHERE id=11")
			if len(token) != 8 {
				t.Fatal(token)
			}
			backup := textDB(t, opened, "SELECT backup_path FROM migration_run WHERE target_version=13")
			snapshot := rawDB(t, backup)
			if countDB(t, snapshot, "PRAGMA user_version") != 4 {
				t.Fatal("v4 백업 유실")
			}
			assertHealthyDB(t, opened)
			opened.Close()
			reopened, err := database.Open(t.Context(), filename)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if textDB(t, reopened, "SELECT file_uuid FROM media_file WHERE id=11") != identity || textDB(t, reopened, "SELECT filename_token FROM media_file WHERE id=11") != token || countDB(t, reopened, "SELECT count(*) FROM migration_run WHERE target_version=13") != 1 {
				t.Fatal("재시작이 식별자·이관 이력을 변경했습니다")
			}
		})
	}
}

func addLocalV5(t *testing.T, db *sql.DB, id, directory int64, path, title string) {
	t.Helper()
	execDB(t, db, "INSERT INTO media(id,source_kind,source_title,custom_title,created_at,revision) VALUES(?,'local',?,?,1,2)", id, title, title)
	execDB(t, db, "INSERT INTO local_folder(media_id,source_title) VALUES(?,?)", id, title)
	execDB(t, db, `INSERT INTO media_file(media_id,file_key,storage_kind,directory_id,filepath,canonical_path_key,relative_path,filesize,mtime_ns,hash_algorithm,content_hash,kind,position,detected_format,width,height,created_at) VALUES(?,?,'external_reference',?,?,?,?,1,'1','sha256',?,'image',0,'png',8,6,1)`, id, fmt.Sprint("local-", id), directory, path, path, filepath.Base(path), strings.Repeat("a", 64))
}

func TestV5SharedDirectoryAndStableFileIdentity(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "shared.db"))
	folder := filepath.Join(root, "B")
	directory, err := media.NewStore(db).RecordedDirectory(t.Context(), folder, true)
	if err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 2; i++ {
		addModernMedia(t, db, i, fmt.Sprintf("https://example.test/post/%d", i), fmt.Sprint("게시글 ", i), filepath.Join(folder, fmt.Sprintf("photo%d.jpg", i)), fmt.Sprint("post-file-", i))
	}
	addLocalV5(t, db, 3, directory, filepath.Join(folder, "loose.png"), "기본 폴더 묶음")
	if err = media.NewStore(db).SetImportMedia(t.Context(), directory, 3); err != nil {
		t.Fatal(err)
	}
	identity := textDB(t, db, "SELECT file_uuid FROM media_file WHERE id=1")
	execDB(t, db, "UPDATE media_file SET directory_id=?,storage_kind='external_reference',canonical_path_key=filepath,relative_path='photo.jpg',filesize=1,mtime_ns='1',hash_algorithm='sha256',content_hash=?,duration=1,detected_format='fixture',width=8,height=6 WHERE id IN (1,2)", directory, strings.Repeat("a", 64))
	service := media.NewService(db)
	for i := int64(1); i <= 3; i++ {
		result := service.Get(t.Context(), media.GetRequest{MediaID: i})
		if !result.OK {
			t.Fatal(result.Error)
		}
		obj := result.Data.(media.Object)
		locations := obj["locations"].([]media.Object)
		if len(locations) != 1 || locations[0]["id"] != directory {
			t.Fatalf("묶음별 실제 위치: %+v", locations)
		}
	}
	if countDB(t, db, "SELECT count(*) FROM media WHERE source_kind='download'") != 2 || textDB(t, db, "SELECT file_uuid FROM media_file WHERE id=1") != identity {
		t.Fatal("보관 방식 변경이 출처 또는 식별자를 변경했습니다")
	}
	result := service.Directories(t.Context(), media.DirectoryListRequest{Limit: 1, Keyword: "B"})
	if !result.OK {
		t.Fatal(result.Error)
	}
	page := result.Data.(media.Object)
	dirs := page["data"].([]media.Object)
	if page["total"] != int64(1) || dirs[0]["mediaCount"] != int64(3) || dirs[0]["fileCount"] != int64(3) {
		t.Fatal(page)
	}
	if _, err = db.Exec("UPDATE media_file SET file_uuid=? WHERE id=1", uuid.NewString()); err == nil {
		t.Fatal("안정 식별자를 변경했습니다")
	}
	// Eight digits are a filename token, not a globally unique database ID.
	execDB(t, db, "UPDATE media_file SET filename_token='550e8400' WHERE id IN (1,2)")
	assertHealthyDB(t, db)
}

func TestImportV5SharedDirectoryKeepsGroupsSeparate(t *testing.T) {
	root := migrationRoot(t)
	target, importer, _ := importer(t, root)
	folder := filepath.Join(root, "B")
	dir, err := media.NewStore(target).RecordedDirectory(t.Context(), folder, true)
	if err != nil {
		t.Fatal(err)
	}
	addLocalV5(t, target, 50, dir, filepath.Join(folder, "current.png"), "현재 묶음")
	if err = media.NewStore(target).SetImportMedia(t.Context(), dir, 50); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(root, "source-v5.db")
	source := latestDB(t, sourcePath)
	sourceDir, err := media.NewStore(source).RecordedDirectory(t.Context(), folder, true)
	if err != nil {
		t.Fatal(err)
	}
	addLocalV5(t, source, 1, sourceDir, filepath.Join(folder, "postA.png"), "게시글 A")
	addLocalV5(t, source, 2, sourceDir, filepath.Join(folder, "postB.png"), "게시글 B")
	if err = media.NewStore(source).SetImportMedia(t.Context(), sourceDir, 1); err != nil {
		t.Fatal(err)
	}
	source.Close()
	for i := 0; i < 2; i++ {
		result, err := importer.Import(t.Context(), sourcePath)
		if err != nil {
			t.Fatal(err)
		}
		if (i == 0 && result.AddedMedia != 2) || (i == 1 && (result.AddedMedia != 0 || result.AddedFiles != 0)) {
			t.Fatalf("가져오기 %d: %+v", i, result)
		}
	}
	if countDB(t, target, "SELECT count(*) FROM media") != 3 || countDB(t, target, "SELECT count(*) FROM media_directory") != 1 || countDB(t, target, "SELECT import_media_id FROM media_directory WHERE id=?", dir) != 50 {
		t.Fatal("물리 폴더 공유가 논리 묶음을 합쳤습니다")
	}
	for _, title := range []string{"현재 묶음", "게시글 A", "게시글 B"} {
		if countDB(t, target, "SELECT count(*) FROM media WHERE custom_title=?", title) != 1 {
			t.Fatal("묶음 메타데이터 유실")
		}
	}
	assertHealthyDB(t, target)
}

func TestV4ImportMapRemainsIdempotentAfterV5Upgrade(t *testing.T) {
	root := migrationRoot(t)
	target, service, _ := importer(t, root)
	filename := filepath.Join(root, "source-v4.db")
	source := rawDB(t, filename)
	execDB(t, source, historicalV4(t))
	execDB(t, source, "PRAGMA user_version=4")
	directory := filepath.Join(root, "photos")
	execDB(t, source, "INSERT INTO media(id,source_kind,source_title,created_at) VALUES(9,'local','photos',1); INSERT INTO local_folder(id,media_id,source_title) VALUES(4,9,'photos')")
	execDB(t, source, "INSERT INTO local_folder_location(id,local_folder_id,media_id,directory_path,directory_key,key_version,state) VALUES(6,4,9,?,'old-dir',1,'active')", directory)
	execDB(t, source, `INSERT INTO media_file(id,media_id,file_key,storage_kind,filepath,canonical_path_key,relative_path,local_folder_location_id,filesize,mtime_ns,hash_algorithm,content_hash,kind,position,detected_format,width,height,created_at) VALUES(11,9,'photo','external_reference',?,'old-key','a.png',6,1,'1','sha256',?,'image',0,'png',8,6,1)`, filepath.Join(directory, "a.png"), strings.Repeat("a", 64))
	source.Close()
	first, err := service.Import(t.Context(), filename)
	if err != nil || first.AddedMedia != 1 {
		t.Fatalf("첫 가져오기: %+v %v", first, err)
	}
	// Emulate the actual import-map format retained by an upgraded v4 target.
	execDB(t, target, "UPDATE legacy_record SET payload_json=json_set(payload_json,'$.identity',?) WHERE table_name LIKE 'import-map:%:media' AND row_key='9'", strings.ToLower(filepath.Clean(directory)))
	if countDB(t, target, "SELECT count(*) FROM legacy_record WHERE table_name LIKE 'import-map:%:media' AND row_key='9'") != 1 {
		t.Fatal("이전 이관 연결 fixture 누락")
	}
	second, err := service.Import(t.Context(), filename)
	if err != nil || second.AddedMedia != 0 || second.AddedFiles != 0 || countDB(t, target, "SELECT count(*) FROM media") != 1 {
		t.Fatalf("이전 병합 기록 재사용: %+v %v", second, err)
	}
	assertHealthyDB(t, target)
}
