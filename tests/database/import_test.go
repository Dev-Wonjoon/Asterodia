package database_test

import (
	"crypto/sha256"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/database"
	"github.com/Dev-Wonjoon/Asterodia/internal/desktop"
	"github.com/Dev-Wonjoon/Asterodia/internal/download"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
)

func fileDigest(t *testing.T, path string) [32]byte {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return sha256.Sum256(b)
}
func addModernMedia(t *testing.T, db *sql.DB, id int64, url, title, path, key string) {
	t.Helper()
	execDB(t, db, "INSERT INTO download_urls(id,url,normalized_url,normalization_version,created_at) VALUES(?,?,?,1,1000)", id, url, url)
	execDB(t, db, "INSERT INTO media(id,source_kind,request_url_id,source_title,custom_title,revision,created_at,updated_at) VALUES(?,'download',?,?,?,5,1000,1000)", id, id, title, title)
	execDB(t, db, "INSERT INTO media_file(id,media_id,file_key,storage_kind,filepath,kind,position,created_at) VALUES(?,?,?,'app_managed',?,'video',0,1000)", id, id, key, path)
}
func importer(t *testing.T, root string) (*sql.DB, *database.Importer, string) {
	t.Helper()
	path := filepath.Join(root, "current.db")
	db := latestDB(t, path)
	return db, database.NewImporter(db, path, filepath.Join(root, "backups")), path
}

func TestImportAccumulatesWithoutOverwriting(t *testing.T) {
	root := migrationRoot(t)
	target, service, _ := importer(t, root)
	sourcePath := filepath.Join(root, "source.db")
	source := latestDB(t, sourcePath)
	addModernMedia(t, target, 1, "https://example.test/existing", "현재 제목", filepath.Join(root, "existing.mp4"), "video:existing")
	addModernMedia(t, source, 1, "https://example.test/new", "가져온 제목", filepath.Join(root, "new.mp4"), "video:new")
	execDB(t, target, "INSERT INTO platform(id,name) VALUES(1,'Current platform')")
	execDB(t, source, "INSERT INTO platform(id,name) VALUES(1,'Source platform')")
	execDB(t, source, "INSERT INTO profiles(id,owner_id,owner_name,platform_id,updated_at) VALUES(1,'source-owner','작성자',1,1000)")
	execDB(t, source, "UPDATE media_file SET platform_id=1,profile_id=1 WHERE id=1")
	execDB(t, target, "INSERT INTO tag(id,name) VALUES(1,'현재 태그')")
	execDB(t, source, "INSERT INTO tag(id,name) VALUES(1,'가져온 태그')")
	execDB(t, source, "INSERT INTO media_tag(media_id,tag_id) VALUES(1,1)")
	execDB(t, source, "INSERT INTO favorite(media_id,created_at) VALUES(1,1000)")
	source.Close()
	before := fileDigest(t, sourcePath)
	result, e := service.Import(t.Context(), sourcePath)
	if e != nil {
		t.Fatal(e)
	}
	if result.AddedMedia != 1 || result.AddedFiles != 1 || result.MissingFiles != 1 {
		t.Fatalf("가져오기 결과 %+v", result)
	}
	if countDB(t, target, "SELECT count(*) FROM media") != 2 || countDB(t, target, "SELECT count(*) FROM media WHERE id=1 AND custom_title='현재 제목' AND revision=5") != 1 {
		t.Fatal("기존 자료가 덮어써졌습니다")
	}
	if countDB(t, target, "SELECT count(*) FROM media_file f JOIN media m ON m.id=f.media_id JOIN profiles p ON p.id=f.profile_id JOIN platform pl ON pl.id=p.platform_id WHERE m.custom_title='가져온 제목' AND p.owner_name='작성자' AND pl.name='Source platform' AND f.storage_kind='unverified_legacy'") != 1 {
		t.Fatal("ID 충돌 관계 연결 실패")
	}
	if countDB(t, target, "SELECT count(*) FROM media_tag mt JOIN media m ON m.id=mt.media_id JOIN tag t ON t.id=mt.tag_id WHERE m.custom_title='가져온 제목' AND t.name='가져온 태그'") != 1 {
		t.Fatal("태그 연결 유실")
	}
	if before != fileDigest(t, sourcePath) {
		t.Fatal("선택한 원본 DB가 변경됐습니다")
	}
	backup := rawDB(t, result.BackupPath)
	if countDB(t, backup, "SELECT count(*) FROM media") != 1 {
		t.Fatal("현재 라이브러리 백업 실패")
	}
	result, e = service.Import(t.Context(), sourcePath)
	if e != nil {
		t.Fatal(e)
	}
	if result.AddedMedia != 0 || result.AddedFiles != 0 || result.SkippedFiles != 1 {
		t.Fatalf("재가져오기 중복 %+v", result)
	}
	source = rawDB(t, sourcePath)
	execDB(t, source, "INSERT INTO media_file(media_id,file_key,storage_kind,filepath,kind,position,created_at) VALUES(1,'video:extra','app_managed',?,'video',1,1001)", filepath.Join(root, "extra.mp4"))
	source.Close()
	result, e = service.Import(t.Context(), sourcePath)
	if e != nil {
		t.Fatal(e)
	}
	if result.AddedMedia != 0 || result.AddedFiles != 1 {
		t.Fatalf("추가 파일 누적 실패 %+v", result)
	}
	assertHealthyDB(t, target)
}

func TestImportKeepsExistingEditsAndCover(t *testing.T) {
	root := migrationRoot(t)
	target, service, _ := importer(t, root)
	sourcePath := filepath.Join(root, "source.db")
	source := latestDB(t, sourcePath)
	path := filepath.Join(root, "same.mp4")
	addModernMedia(t, target, 1, "https://example.test/same", "사용자 제목", path, "video:one")
	addModernMedia(t, source, 100, "https://example.test/same", "원본 제목", path, "video:one")
	execDB(t, target, "INSERT INTO tag(name) VALUES('내 태그')")
	execDB(t, target, "INSERT INTO media_tag(media_id,tag_id) VALUES(1,1)")
	execDB(t, target, "UPDATE media SET cover_mode='manual',cover_file_id=1 WHERE id=1")
	execDB(t, source, "INSERT INTO tag(name) VALUES('원본 태그')")
	execDB(t, source, "INSERT INTO media_tag(media_id,tag_id) VALUES(100,1)")
	execDB(t, source, "INSERT INTO favorite(media_id,created_at) VALUES(100,1000)")
	execDB(t, source, "INSERT INTO media_file(id,media_id,file_key,storage_kind,filepath,kind,position,created_at) VALUES(101,100,'video:two','app_managed',?,'video',1,1001)", filepath.Join(root, "second.mp4"))
	source.Close()
	result, e := service.Import(t.Context(), sourcePath)
	if e != nil {
		t.Fatal(e)
	}
	if result.AddedMedia != 0 || result.AddedFiles != 1 || result.SkippedFiles != 1 {
		t.Fatal(result)
	}
	if countDB(t, target, "SELECT count(*) FROM media WHERE id=1 AND custom_title='사용자 제목' AND cover_mode='manual' AND cover_file_id=1 AND revision=6") != 1 {
		t.Fatal("사용자 제목·표지·revision 보존 실패")
	}
	if countDB(t, target, "SELECT count(*) FROM media_tag mt JOIN tag t ON t.id=mt.tag_id WHERE mt.media_id=1 AND t.name='내 태그'") != 1 || countDB(t, target, "SELECT count(*) FROM media_tag WHERE media_id=1") != 1 || countDB(t, target, "SELECT count(*) FROM favorite WHERE media_id=1") != 0 {
		t.Fatal("기존 태그·즐겨찾기 선택을 변경했습니다")
	}
	assertHealthyDB(t, target)
}

func TestWailsImportAndReadOnlyHistory(t *testing.T) {
	root := migrationRoot(t)
	target, service, _ := importer(t, root)
	sourcePath := filepath.Join(root, "legacy.db")
	source := latestDB(t, sourcePath)
	addModernMedia(t, source, 7, "https://example.test/watch?v=one", "가져올 제목", filepath.Join(root, "original.mp4"), "video:fixture")
	execDB(t, source, "INSERT INTO download_queue(id,request_url_id,original_url,mode,state,options_json,progress,created_at,updated_at) VALUES('pending-job',7,'https://example.test/watch?v=one','create','pending','{}',0,1700000000000,1700000000000)")
	source.Close()
	before := fileDigest(t, sourcePath)
	result, e := service.Import(t.Context(), sourcePath)
	if e != nil {
		t.Fatal(e)
	}
	if result.SourceVersion != 14 || result.AddedMedia != 1 || result.AddedHistory != 1 {
		t.Fatal(result)
	}
	if before != fileDigest(t, sourcePath) {
		t.Fatal("원본 DB가 변경되었습니다")
	}
	var job string
	if e = target.QueryRow("SELECT id FROM download_queue").Scan(&job); e != nil {
		t.Fatal(e)
	}
	downloader := download.NewService(target, func() string { return root }, func(string, engine.Options) download.Runner {
		t.Fatal("가져온 이력이 실행됐습니다")
		return download.Runner{}
	}, nil)
	defer downloader.Close()
	r := downloader.Run(t.Context(), download.RunRequest{JobID: job, Trigger: "manual_restart"})
	if r.OK || r.Error.Code != "IMPORTED_HISTORY" {
		t.Fatalf("이력 실행 차단 %+v", r)
	}
	current := downloader.Get(t.Context(), download.JobRequest{JobID: job})
	if !current.OK || current.Data.(download.Job).Options != nil {
		t.Fatal("이력 실행 버튼이 활성화될 수 있습니다")
	}
	result, e = service.Import(t.Context(), sourcePath)
	if e != nil {
		t.Fatal(e)
	}
	if result.AddedMedia != 0 || result.AddedFiles != 0 || result.AddedHistory != 0 {
		t.Fatalf("Wails DB 반복 가져오기 중복 %+v", result)
	}
	copyPath := filepath.Join(root, "renamed-copy.db")
	b, e := os.ReadFile(sourcePath)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(copyPath, b, 0600); e != nil {
		t.Fatal(e)
	}
	result, e = service.Import(t.Context(), copyPath)
	if e != nil {
		t.Fatal(e)
	}
	if result.AddedMedia != 0 || result.AddedFiles != 0 || result.AddedHistory != 0 {
		t.Fatal("이름이 다른 원본 사본을 중복 등록했습니다")
	}
	assertHealthyDB(t, target)
}

func TestImportRollbackBusyAndInvalidSource(t *testing.T) {
	root := migrationRoot(t)
	target, service, current := importer(t, root)
	sourcePath := filepath.Join(root, "source.db")
	source := latestDB(t, sourcePath)
	addModernMedia(t, source, 1, "https://example.test/new", "가져올 자료", filepath.Join(root, "new.mp4"), "one")
	source.Close()
	before := fileDigest(t, sourcePath)
	execDB(t, target, "CREATE TRIGGER fixture_fail BEFORE INSERT ON media_file BEGIN SELECT RAISE(ABORT,'fixture'); END")
	if _, e := service.Import(t.Context(), sourcePath); e == nil {
		t.Fatal("DB 오류를 성공으로 처리했습니다")
	}
	if countDB(t, target, "SELECT count(*) FROM media") != 0 || countDB(t, target, "SELECT count(*) FROM download_urls") != 0 || countDB(t, target, "SELECT count(*) FROM legacy_record") != 0 {
		t.Fatal("부분 병합이 남았습니다")
	}
	execDB(t, target, "DROP TRIGGER fixture_fail")
	if _, e := service.Import(t.Context(), current); e == nil {
		t.Fatal("현재 DB 자체 가져오기를 허용했습니다")
	}
	execDB(t, target, "INSERT INTO download_urls(id,url,normalized_url,normalization_version,created_at) VALUES(9,'https://example.test/busy','https://example.test/busy',1,1)")
	execDB(t, target, "INSERT INTO download_queue(id,request_url_id,original_url,mode,state,options_json) VALUES('busy',9,'https://example.test/busy','create','pending','{}')")
	if _, e := service.Import(t.Context(), sourcePath); e == nil || !strings.Contains(e.Error(), "작업") {
		t.Fatalf("진행 작업 차단 실패 %v", e)
	}
	if countDB(t, target, "SELECT count(*) FROM media") != 0 {
		t.Fatal("진행 중 DB가 변경됐습니다")
	}
	if before != fileDigest(t, sourcePath) {
		t.Fatal("실패 시 원본 DB가 변경됐습니다")
	}
	assertHealthyDB(t, target)
}

func TestImportIncludesWALAndNeverRunsSourceMaintenance(t *testing.T) {
	root := migrationRoot(t)
	target, service, _ := importer(t, root)
	sourcePath := filepath.Join(root, "wal-source.db")
	source := latestDB(t, sourcePath)
	execDB(t, source, "PRAGMA journal_mode=WAL;PRAGMA wal_autocheckpoint=0")
	addModernMedia(t, source, 1, "https://example.test/wal", "WAL 자료", filepath.Join(root, "wal.mp4"), "wal")
	execDB(t, source, `INSERT INTO maintenance_task(id,kind,state,owner_kind,payload_json,created_at,updated_at) VALUES('source-delete','deletion_cleanup','pending','system','{"paths":["C:/original/user-file.mp4"]}',1,1)`)
	before := fileDigest(t, sourcePath)
	result, e := service.Import(t.Context(), sourcePath)
	if e != nil {
		t.Fatal(e)
	}
	if result.AddedMedia != 1 {
		t.Fatal("커밋된 WAL 자료가 누락되었습니다")
	}
	if countDB(t, target, "SELECT count(*) FROM maintenance_task") != 0 {
		t.Fatal("원본 DB의 정리 작업을 실행 대기열에 넣었습니다")
	}
	if countDB(t, target, "SELECT count(*) FROM legacy_record WHERE table_name LIKE 'import:%:maintenance_task'") != 1 {
		t.Fatal("원본 작업 이력 원문 유실")
	}
	if before != fileDigest(t, sourcePath) {
		t.Fatal("읽기 전용 원본이 변경됐습니다")
	}
	assertHealthyDB(t, target)
}

func TestDatabasePickerCancellation(t *testing.T) {
	root := migrationRoot(t)
	_, importer, _ := importer(t, root)
	calls := 0
	service := desktop.NewService(importer, func() (string, error) { calls++; return "", nil })
	result := service.ImportDatabase(t.Context())
	if !result.OK || !result.Data.(database.ImportResult).Cancelled || calls != 1 {
		t.Fatal("선택 취소 처리 실패")
	}
	entries, e := os.ReadDir(filepath.Join(root, "backups"))
	if e == nil && len(entries) > 0 {
		t.Fatal("선택 취소 시 DB를 처리했습니다")
	}
}

func TestImportFileConflictRollsBack(t *testing.T) {
	root := migrationRoot(t)
	target, service, _ := importer(t, root)
	sourcePath := filepath.Join(root, "conflict.db")
	source := latestDB(t, sourcePath)
	path := filepath.Join(root, "same.mp4")
	addModernMedia(t, target, 1, "https://example.test/same", "현재 자료", path, "one")
	addModernMedia(t, source, 10, "https://example.test/same", "다른 내용", path, "one")
	execDB(t, target, "UPDATE media_file SET hash_algorithm='sha256',content_hash=?", strings.Repeat("a", 64))
	execDB(t, source, "UPDATE media_file SET hash_algorithm='sha256',content_hash=?", strings.Repeat("b", 64))
	execDB(t, source, "INSERT INTO tag(name) VALUES('롤백할 태그')")
	source.Close()
	if _, e := service.Import(t.Context(), sourcePath); e == nil || !strings.Contains(e.Error(), "내용") {
		t.Fatalf("내용 충돌을 허용했습니다: %v", e)
	}
	if countDB(t, target, "SELECT count(*) FROM media") != 1 || countDB(t, target, "SELECT count(*) FROM tag") != 0 || countDB(t, target, "SELECT count(*) FROM legacy_record") != 0 {
		t.Fatal("충돌 시 부분 병합이 남았습니다")
	}
	if countDB(t, target, "SELECT count(*) FROM media_file WHERE content_hash=?", strings.Repeat("a", 64)) != 1 {
		t.Fatal("기존 파일이 덮어써졌습니다")
	}
}

func TestImportLocalFolderReferencesAndRepeatedImport(t *testing.T) {
	root := migrationRoot(t)
	target, service, _ := importer(t, root)
	sourcePath := filepath.Join(root, "local.db")
	source := latestDB(t, sourcePath)
	addModernMedia(t, target, 1, "https://example.test/current", "기존 자료", filepath.Join(root, "current.mp4"), "current")
	directory := filepath.Join(root, "photos")
	path := filepath.Join(directory, "photo.jpg")
	key := strings.ToLower(directory)
	execDB(t, source, "INSERT INTO media(id,source_kind,custom_title,created_at) VALUES(1,'local','사진 폴더',1000)")
	execDB(t, source, "INSERT INTO local_folder(id,media_id,source_title) VALUES(1,1,'photos')")
	directoryID, err := media.NewStore(source).RecordedDirectory(t.Context(), directory, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = media.NewStore(source).SetImportMedia(t.Context(), directoryID, 1); err != nil {
		t.Fatal(err)
	}
	execDB(t, source, "INSERT INTO local_folder_location(id,local_folder_id,media_id,directory_id,directory_path,directory_key,key_version,state) VALUES(1,1,1,?,?,?,1,'active')", directoryID, directory, key)
	execDB(t, source, "INSERT INTO media_file(id,media_id,file_key,storage_kind,directory_id,filepath,canonical_path_key,relative_path,local_folder_location_id,filesize,mtime_ns,hash_algorithm,content_hash,kind,position,detected_format,width,height,created_at) VALUES(1,1,'photo','external_reference',?,?,?,'photo.jpg',1,3,'1000','sha256',?,'image',0,'jpeg',10,10,1000)", directoryID, path, strings.ToLower(path), strings.Repeat("a", 64))
	execDB(t, source, "UPDATE media SET cover_mode='manual',cover_file_id=1 WHERE id=1")
	source.Close()
	result, e := service.Import(t.Context(), sourcePath)
	if e != nil {
		t.Fatal(e)
	}
	if result.AddedMedia != 1 || result.AddedFiles != 1 {
		t.Fatal(result)
	}
	if countDB(t, target, "SELECT count(*) FROM media m JOIN media_file f ON f.id=m.cover_file_id AND f.media_id=m.id JOIN local_folder_location l ON l.id=f.local_folder_location_id AND l.media_id=m.id JOIN local_folder d ON d.id=l.local_folder_id AND d.media_id=m.id WHERE m.id<>1 AND m.source_kind='local' AND f.storage_kind='external_reference' AND l.directory_key=?", key) != 1 {
		t.Fatal("로컬 폴더·파일·표지 참조 재배정 실패")
	}
	result, e = service.Import(t.Context(), sourcePath)
	if e != nil || result.AddedMedia != 0 || result.AddedFiles != 0 {
		t.Fatalf("로컬 반복 가져오기 실패: %+v %v", result, e)
	}
	assertHealthyDB(t, target)
}

func TestImportPreservesFileOrder(t *testing.T) {
	root := migrationRoot(t)
	target, service, _ := importer(t, root)
	sourcePath := filepath.Join(root, "ordered.db")
	source := latestDB(t, sourcePath)
	addModernMedia(t, source, 1, "https://example.test/ordered", "정렬된 자료", filepath.Join(root, "first.mp4"), "first")
	for _, item := range []struct {
		id, position int
		key          string
	}{{2, 2, "last"}, {10, 1, "middle"}} {
		execDB(t, source, "INSERT INTO media_file(id,media_id,file_key,storage_kind,filepath,kind,position,created_at) VALUES(?,1,?,'app_managed',?,'video',?,1000)", item.id, item.key, filepath.Join(root, item.key+".mp4"), item.position)
	}
	execDB(t, source, "UPDATE media SET cover_mode='manual',cover_file_id=10 WHERE id=1")
	source.Close()
	if _, e := service.Import(t.Context(), sourcePath); e != nil {
		t.Fatal(e)
	}
	var order string
	if e := target.QueryRow("SELECT group_concat(file_key,',') FROM (SELECT file_key FROM media_file ORDER BY position)").Scan(&order); e != nil {
		t.Fatal(e)
	}
	if order != "first,middle,last" {
		t.Fatalf("파일 순서 변경: %s", order)
	}
	if countDB(t, target, "SELECT count(*) FROM media m JOIN media_file f ON f.id=m.cover_file_id WHERE f.file_key='middle' AND m.cover_mode='manual'") != 1 {
		t.Fatal("표지 참조 유실")
	}
}
