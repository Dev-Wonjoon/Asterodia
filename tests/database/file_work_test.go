package database_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/database"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
)

func mustRejectSQL(t *testing.T, db *sql.DB, statement string, args ...any) {
	t.Helper()
	if _, err := db.Exec(statement, args...); err == nil {
		t.Fatalf("거부해야 하는 변경이 성공했습니다: %s", statement)
	}
}
func moveFixture(t *testing.T) (*sql.DB, string, int64) {
	t.Helper()
	root := migrationRoot(t)
	file := filepath.Join(root, "work.db")
	db := latestDB(t, file)
	for i := int64(1); i <= 2; i++ {
		addModernMedia(t, db, i, "https://example.test/post/"+string(rune('0'+i)), "게시글", filepath.Join(root, string(rune('a'+i))+".mp4"), "file-"+string(rune('a'+i)))
	}
	directory, err := media.NewStore(db).RecordedDirectory(t.Context(), filepath.Join(root, "B"), true)
	if err != nil {
		t.Fatal(err)
	}
	execDB(t, db, "INSERT INTO file_move_job(id,target_directory_id,target_directory_snapshot,state,created_at,updated_at) VALUES('job',?,?,'pending',1,1)", directory, filepath.Join(root, "B"))
	execDB(t, db, `INSERT INTO file_move_item(id,job_id,media_id,file_id,media_id_snapshot,file_uuid_snapshot,source_path,source_key,target_path,target_key,expected_stat_json,state,commit_token) SELECT 'item','job',media_id,id,media_id,file_uuid,filepath,filepath,?,'reserved-target','{}','pending','token' FROM media_file WHERE id=1`, filepath.Join(root, "B", "target.mp4"))
	return db, file, directory
}
func TestFileMoveCheckpointGuardsAndSourcePreservation(t *testing.T) {
	db, filename, directory := moveFixture(t)
	identity := textDB(t, db, "SELECT file_uuid FROM media_file WHERE id=1")
	mustRejectSQL(t, db, "UPDATE file_move_item SET state='unknown' WHERE id='item'")
	mustRejectSQL(t, db, "UPDATE file_move_item SET source_path='changed' WHERE id='item'")
	mustRejectSQL(t, db, "UPDATE file_move_item SET media_id=2,file_id=2 WHERE id='item'")
	mustRejectSQL(t, db, "UPDATE file_move_item SET media_id=NULL,file_id=NULL WHERE id='item'")
	mustRejectSQL(t, db, "UPDATE file_move_job SET target_directory_snapshot='changed' WHERE id='job'")
	mustRejectSQL(t, db, "DELETE FROM file_move_job WHERE id='job'")
	mustRejectSQL(t, db, "DELETE FROM media_file WHERE id=1")
	mustRejectSQL(t, db, `INSERT INTO file_move_item(id,job_id,media_id,file_id,media_id_snapshot,file_uuid_snapshot,source_path,source_key,expected_stat_json,state,commit_token) SELECT 'wrong','job',media_id,id,media_id,'wrong',filepath,filepath,'{}','pending','other-token' FROM media_file WHERE id=2`)
	mustRejectSQL(t, db, `INSERT INTO file_move_item(id,job_id,media_id,file_id,media_id_snapshot,file_uuid_snapshot,source_path,source_key,expected_stat_json,state,commit_token) SELECT 'duplicate','job',media_id,id,media_id,file_uuid,filepath,filepath,'{}','pending','other-token' FROM media_file WHERE id=1`)
	mustRejectSQL(t, db, `INSERT INTO file_move_item(id,job_id,media_id,file_id,media_id_snapshot,file_uuid_snapshot,source_path,source_key,target_path,target_key,expected_stat_json,state,commit_token) SELECT 'target-conflict','job',media_id,id,media_id,file_uuid,filepath,filepath,'other','reserved-target','{}','pending','other-token' FROM media_file WHERE id=2`)
	service := media.NewService(db)
	result := service.Edit(t.Context(), media.EditRequest{MediaID: 1, ExpectedRevision: 5})
	if result.OK || result.Error.Code != "JOB_ACTIVE" {
		t.Fatalf("진행 중인 묶음 편집: %+v", result)
	}
	mustRejectSQL(t, db, "UPDATE file_move_item SET state='committed',committed_at=2 WHERE id='item'")
	// Simulate the durable transaction boundary; this test never moves a file.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(`UPDATE media_file SET directory_id=?,filepath=?,storage_kind='external_reference',canonical_path_key='new-key',relative_path='target.mp4',filesize=1,mtime_ns='1',hash_algorithm='sha256',content_hash=?,detected_format='mov',width=8,height=6,duration=1 WHERE id=1`, directory, filepath.Join(filepath.Dir(filename), "B", "target.mp4"), strings.Repeat("a", 64))
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if _, err = tx.Exec("UPDATE file_move_item SET state='cleanup_pending',verification_json='{}',committed_at=2 WHERE id='item'"); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = database.Open(t.Context(), filename)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mustRejectSQL(t, db, "UPDATE file_move_item SET target_path='changed' WHERE id='item'")
	mustRejectSQL(t, db, "UPDATE file_move_item SET verification_json='{\"changed\":true}' WHERE id='item'")
	mustRejectSQL(t, db, "UPDATE file_move_item SET committed_at=NULL,state='pending' WHERE id='item'")
	if textDB(t, db, "SELECT state FROM file_move_item WHERE id='item'") != "cleanup_pending" || textDB(t, db, "SELECT file_uuid FROM media_file WHERE id=1") != identity {
		t.Fatal("재시작 시 작업 단계 또는 파일 식별자 유실")
	}
	if countDB(t, db, "SELECT count(*) FROM media WHERE id=1 AND source_kind='download' AND request_url_id=1") != 1 {
		t.Fatal("외부 폴더 저장이 다운로드 출처를 변경했습니다")
	}
	execDB(t, db, "UPDATE file_move_item SET state='completed',finished_at=3 WHERE id='item'; UPDATE file_move_job SET state='completed',updated_at=3 WHERE id='job'")
	service = media.NewService(db)
	result = service.UnregisterMedia(t.Context(), media.UnregisterRequest{MediaID: 1, ExpectedRevision: 5})
	if !result.OK {
		t.Fatal(result.Error)
	}
	if countDB(t, db, "SELECT count(*) FROM maintenance_task WHERE kind='deletion_cleanup'") != 0 {
		t.Fatal("외부 참조 파일의 삭제 작업을 만들었습니다")
	}
	if countDB(t, db, "SELECT count(*) FROM file_move_item WHERE id='item' AND file_id IS NULL AND media_id IS NULL AND media_id_snapshot=1 AND file_uuid_snapshot=?", identity) != 1 {
		t.Fatal("등록 해제가 이동 이력을 지웠습니다")
	}
	assertHealthyDB(t, db)
}

func TestRegisteredFileCheckpointPersistsAndRejectsMismatch(t *testing.T) {
	db, filename, directory := moveFixture(t)
	execDB(t, db, "UPDATE file_move_item SET state='excluded' WHERE id='item'; UPDATE file_move_job SET state='cancelled' WHERE id='job'")
	execDB(t, db, "INSERT INTO local_import_job(id,root_path,root_key,state,counts_json,created_at,updated_at) VALUES('import',?,'root','running','{}',1,1)", filepath.Dir(filename))
	execDB(t, db, "INSERT INTO local_import_folder(id,job_id,directory_path,directory_key,relative_path,state) VALUES('folder','import',?,'dir','.','running')", filepath.Dir(filename))
	execDB(t, db, "INSERT INTO local_import_attempt(id,import_folder_id,attempt_no,phase,commit_token,started_at) VALUES('attempt','folder',1,'inspecting','import-token',1)")
	execDB(t, db, "UPDATE local_import_folder SET current_attempt_id='attempt' WHERE id='folder'")
	insert := `INSERT INTO local_import_target(id,attempt_id,path_key,relative_path,expected_stat_json,state,media_file_id,file_uuid_snapshot,inspection_hash,inspector_version,committed_at) VALUES(?,'attempt',?,'file.mp4','{}',?,?,?,?,?,?)`
	mustRejectSQL(t, db, insert, "bad", "bad-key", "registered", 1, "wrong", strings.Repeat("a", 64), "fixture-v1", 2)
	mustRejectSQL(t, db, insert, "incomplete", "incomplete-key", "registered", nil, nil, nil, nil, nil)
	identity := textDB(t, db, "SELECT file_uuid FROM media_file WHERE id=1")
	execDB(t, db, insert, "verified", "key", "registered", 1, identity, strings.Repeat("a", 64), "fixture-v1", 2)
	execDB(t, db, "UPDATE media_file SET directory_id=? WHERE id=1", directory)
	db.Close()
	db, err := database.Open(t.Context(), filename)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if countDB(t, db, "SELECT count(*) FROM local_import_target WHERE id='verified' AND media_file_id=1 AND inspector_version='fixture-v1' AND committed_at=2 AND state='registered'") != 1 {
		t.Fatal("파일별 등록 체크포인트 유실")
	}
	execDB(t, db, "DELETE FROM media_file WHERE id=1")
	if countDB(t, db, "SELECT count(*) FROM local_import_target WHERE id='verified' AND media_file_id IS NULL AND file_uuid_snapshot=? AND state='registered'", identity) != 1 {
		t.Fatal("삭제된 파일의 등록 이력 유실")
	}
	assertHealthyDB(t, db)
}
