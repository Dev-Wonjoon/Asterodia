package database_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
)

func TestV3MediaReferencesMigration(t *testing.T) {
	root := migrationRoot(t)
	path := filepath.Join(root, "v3.db")
	db := rawDB(t, path)
	ddl, err := os.ReadFile(repoPath(t, "internal", "database", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	execDB(t, db, string(ddl))
	execDB(t, db, "PRAGMA user_version=3")
	execDB(t, db, "INSERT INTO media(id,source_kind,source_title,created_at) VALUES(7,'local','보존할 폴더',1)")
	execDB(t, db, "INSERT INTO local_folder(id,media_id,source_title) VALUES(1,7,'보존할 폴더')")
	execDB(t, db, "INSERT INTO local_folder_location(id,local_folder_id,media_id,directory_path,directory_key,key_version,state) VALUES(1,1,7,?,'existing',1,'active')", root)
	execDB(t, db, `INSERT INTO media_file(id,media_id,file_key,storage_kind,filepath,canonical_path_key,relative_path,local_folder_location_id,filesize,mtime_ns,hash_algorithm,content_hash,kind,position,detected_format,width,height,created_at) VALUES(12,7,'original','external_reference',?,'original','original.png',1,100,'1','sha256',?,'image',0,'png',8,6,1)`, filepath.Join(root, "original.png"), string(makeHexHash()))
	execDB(t, db, "UPDATE media SET cover_mode='manual',cover_file_id=12,custom_title='사용자 제목' WHERE id=7")
	execDB(t, db, "UPDATE sqlite_sequence SET seq=1000 WHERE name='media_file'")
	db.Close()
	db, err = database.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if countDB(t, db, "PRAGMA user_version") != 14 || countDB(t, db, "SELECT count(*) FROM media WHERE id=7 AND custom_title='사용자 제목' AND cover_file_id=12") != 1 || countDB(t, db, "SELECT seq FROM sqlite_sequence WHERE name='media_file'") != 1000 {
		t.Fatal("기존 식별자/표지/시퀀스가 변경됐습니다")
	}
	for _, kind := range []string{"video", "audio"} {
		execDB(t, db, `INSERT INTO media_file(media_id,file_key,storage_kind,directory_id,filepath,canonical_path_key,relative_path,local_folder_location_id,filesize,mtime_ns,hash_algorithm,content_hash,kind,position,detected_format,width,height,duration,created_at) VALUES(7,?,'external_reference',(SELECT directory_id FROM media_file WHERE id=12),?,?,?,1,100,'1','sha256',?,?,?,'fixture',8,6,1,1)`, kind, filepath.Join(root, kind), kind, kind, string(makeHexHash()), kind, map[string]int{"video": 1, "audio": 2}[kind])
	}
	assertHealthyDB(t, db)
	var backupPath string
	if err = db.QueryRow("SELECT backup_path FROM migration_run WHERE target_version=14").Scan(&backupPath); err != nil {
		t.Fatal(err)
	}
	backup := rawDB(t, backupPath)
	if countDB(t, backup, "PRAGMA user_version") != 3 || countDB(t, backup, "SELECT count(*) FROM media_file") != 1 {
		t.Fatal("이관 전 백업이 올바르지 않습니다")
	}
}
func makeHexHash() []byte {
	out := make([]byte, 64)
	for i := range out {
		out[i] = 'a'
	}
	return out
}
