package database_test

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

func instagramPost(t *testing.T, db *sql.DB, root string, id int64, owner, name string) {
	t.Helper()
	addModernMedia(t, db, id, fmt.Sprintf("https://www.instagram.com/p/post%d/", id), "caption", filepath.Join(root, fmt.Sprintf("%d.mp4", id)), fmt.Sprintf("instagram:%d", id))
	execDB(t, db, "INSERT INTO platform(name) VALUES('Instagram') ON CONFLICT DO NOTHING")
	platform := countDB(t, db, "SELECT id FROM platform WHERE name='Instagram'")
	if owner == "" {
		execDB(t, db, "UPDATE media_file SET platform_id=? WHERE id=?", platform, id)
		return
	}
	execDB(t, db, "INSERT INTO profiles(owner_id,owner_name,platform_id,updated_at) VALUES(?,?,?,1) ON CONFLICT(owner_id,platform_id) DO UPDATE SET owner_name=excluded.owner_name", owner, name, platform)
	profile := countDB(t, db, "SELECT id FROM profiles WHERE owner_id=? AND platform_id=?", owner, platform)
	execDB(t, db, "UPDATE media_file SET platform_id=?,profile_id=?,external_id=? WHERE id=?", platform, profile, fmt.Sprint(id), id)
}
func syncInstagram(t *testing.T, db *sql.DB, id int64) {
	t.Helper()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = media.SyncInstagramProfileAlbums(t.Context(), tx, id); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestInstagramProfileAlbumsAppendAndPreserveSources(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "profiles.db"))
	instagramPost(t, db, root, 1, "101", "account")
	instagramPost(t, db, root, 2, "101", "account")
	instagramPost(t, db, root, 3, "102", "another")
	instagramPost(t, db, root, 4, "", "unknown")
	instagramPost(t, db, root, 5, "username-only", "untrusted")
	execDB(t, db, `INSERT INTO media_file(media_id,file_key,storage_kind,filepath,kind,position,profile_id,platform_id,created_at)
 SELECT 1,'instagram:second','app_managed',?,'video',1,profile_id,platform_id,1000 FROM media_file WHERE id=1`, filepath.Join(root, "second.mp4"))
	second := countDB(t, db, "SELECT id FROM media_file WHERE file_key='instagram:second'")
	execDB(t, db, "INSERT INTO tag(name) VALUES('keep');INSERT INTO media_tag(media_id,tag_id) VALUES(1,1);INSERT INTO favorite(media_id,created_at) VALUES(1,10)")
	original := textDB(t, db, "SELECT file_uuid FROM media_file WHERE id=1")
	user := albumObject(t, media.NewService(db).SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("my selection"), AddFileIDs: []int64{1, second}}))
	syncInstagram(t, db, 0)
	album := countDB(t, db, "SELECT media_id FROM instagram_profile_album WHERE owner_id='101'")
	if countDB(t, db, "SELECT count(*) FROM instagram_profile_album") != 2 || countDB(t, db, "SELECT count(*) FROM instagram_profile_source") != 3 {
		t.Fatal("unknown accounts were grouped")
	}
	if countDB(t, db, "SELECT count(*) FROM user_album_file WHERE album_id=?", album) != 3 {
		t.Fatal("carousel/post files missing")
	}
	if textDB(t, db, "SELECT group_concat(file_id,',') FROM (SELECT file_id FROM user_album_file WHERE album_id=? ORDER BY position,file_id)", album) != fmt.Sprintf("1,%d,2", second) {
		t.Fatal("post order changed")
	}
	if textDB(t, db, "SELECT file_uuid FROM media_file WHERE id=1") != original || countDB(t, db, "SELECT count(*) FROM user_album_file WHERE album_id=?", user["id"]) != 2 {
		t.Fatal("original identities or user references changed")
	}
	if countDB(t, db, "SELECT count(*) FROM favorite WHERE media_id=?", album) != 1 || countDB(t, db, "SELECT count(*) FROM media_tag WHERE media_id=?", album) != 1 {
		t.Fatal("metadata was not inherited")
	}
	execDB(t, db, "DELETE FROM favorite WHERE media_id=?", album)
	execDB(t, db, "DELETE FROM media_tag WHERE media_id=?", album)
	syncInstagram(t, db, 0)
	if countDB(t, db, "SELECT count(*) FROM favorite WHERE media_id=?", album) != 0 || countDB(t, db, "SELECT count(*) FROM media_tag WHERE media_id=?", album) != 0 {
		t.Fatal("reconciliation overwrote album choices")
	}
	if countDB(t, db, "SELECT count(*) FROM user_album_file WHERE album_id=?", album) != 3 {
		t.Fatal("reconciliation duplicated files")
	}
	execDB(t, db, "UPDATE media SET custom_title='my profile' WHERE id=?", album)
	instagramPost(t, db, root, 100, "101", "renamed")
	syncInstagram(t, db, 100)
	if textDB(t, db, "SELECT custom_title FROM media WHERE id=?", album) != "my profile" || textDB(t, db, "SELECT source_title FROM media WHERE id=?", album) != "Instagram @renamed" {
		t.Fatal("rename changed identity/title")
	}
	if countDB(t, db, "SELECT position FROM user_album_file WHERE album_id=? AND file_id=100", album) != 3 {
		t.Fatal("new post was not appended")
	}
	instagramPost(t, db, root, 101, "101", "renamed")
	execDB(t, db, "UPDATE media_file SET external_id='100' WHERE id=101")
	syncInstagram(t, db, 101)
	if countDB(t, db, "SELECT count(*) FROM user_album_file WHERE album_id=?", album) != 4 {
		t.Fatal("alternate post URL duplicated the same media")
	}
	catalog := media.NewService(db)
	page := albumObject(t, catalog.List(t.Context(), media.ListRequest{Gallery: true, AlbumCards: true, Limit: 100}))
	for _, row := range page["data"].([]media.Object) {
		if row["id"] == int64(1) || row["id"] == int64(2) || row["id"] == int64(3) || row["id"] == int64(100) || row["id"] == int64(101) {
			t.Fatal("profile and source cards duplicated", row)
		}
	}
	files := albumObject(t, catalog.List(t.Context(), media.ListRequest{Gallery: true, GalleryView: "files", Limit: 100}))
	found := 0
	for _, row := range files["data"].([]media.Object) {
		if row["id"] == album {
			found++
		}
	}
	if found != 4 {
		t.Fatal("file-only view lost profile files", files)
	}
	showCollected := false
	filtered := albumObject(t, catalog.List(t.Context(), media.ListRequest{Gallery: true, GalleryView: "files", ShowAlbumFiles: &showCollected, Limit: 100}))
	visible := []int64{}
	for _, row := range filtered["data"].([]media.Object) {
		if row["id"] == album {
			visible = append(visible, row["galleryFile"].(media.Object)["id"].(int64))
		}
	}
	if fmt.Sprint(visible) != "[2 100]" {
		t.Fatal("file view ignored collected-file visibility", visible)
	}
	collapsed := albumObject(t, catalog.List(t.Context(), media.ListRequest{Gallery: true, ShowAlbumFiles: &showCollected, Limit: 100}))
	for _, row := range collapsed["data"].([]media.Object) {
		if row["id"] == album && row["galleryFile"] != nil {
			t.Fatal("profile album expanded automatically")
		}
	}
	assertHealthyDB(t, db)
}

func TestV13InstagramMigrationBackupRollbackAndIdempotence(t *testing.T) {
	root := migrationRoot(t)
	filename := filepath.Join(root, "old.db")
	db := latestDB(t, filename)
	instagramPost(t, db, root, 1, "201", "kept")
	instagramPost(t, db, root, 2, "201", "kept")
	if err := os.WriteFile(filepath.Join(root, "1.mp4"), []byte("unchanged source bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	before := fileDigest(t, filepath.Join(root, "1.mp4"))
	identity := textDB(t, db, "SELECT file_uuid FROM media_file WHERE id=1")
	execDB(t, db, "DROP TABLE instagram_profile_source;DROP TABLE instagram_profile_album;PRAGMA user_version=13")
	db.Close()
	options := database.MigrationOptions{BackupDirectory: filepath.Join(root, "backups"), Checkpoint: func(step string) error {
		if step == "before_commit" {
			return errors.New("injected")
		}
		return nil
	}}
	if opened, err := database.OpenWithOptions(t.Context(), filename, options); err == nil {
		opened.Close()
		t.Fatal("migration did not roll back")
	}
	old := rawDB(t, filename)
	if countDB(t, old, "PRAGMA user_version") != 13 || countDB(t, old, "SELECT count(*) FROM sqlite_master WHERE name='instagram_profile_album'") != 0 {
		t.Fatal("rollback changed database")
	}
	old.Close()
	options.Checkpoint = nil
	upgraded, err := database.OpenWithOptions(t.Context(), filename, options)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	if countDB(t, upgraded, "PRAGMA user_version") != 14 || countDB(t, upgraded, "SELECT count(*) FROM instagram_profile_album") != 1 || countDB(t, upgraded, "SELECT count(*) FROM instagram_profile_source") != 2 {
		t.Fatal("old posts not grouped")
	}
	if textDB(t, upgraded, "SELECT file_uuid FROM media_file WHERE id=1") != identity || fileDigest(t, filepath.Join(root, "1.mp4")) != before {
		t.Fatal("migration changed original files")
	}
	backup := rawDB(t, textDB(t, upgraded, "SELECT backup_path FROM migration_run WHERE target_version=14"))
	if countDB(t, backup, "PRAGMA user_version") != 13 || countDB(t, backup, "SELECT count(*) FROM media") != 2 {
		t.Fatal("backup was not the old database")
	}
	result, err := database.Migrate(t.Context(), upgraded, options)
	if err != nil || result.Migrated || countDB(t, upgraded, "SELECT count(*) FROM media") != 3 {
		t.Fatal("repeated upgrade changed data", result, err)
	}
	assertHealthyDB(t, upgraded)
}

func TestInstagramProfileAlbumImportReusesAccount(t *testing.T) {
	root := migrationRoot(t)
	target, service, _ := importer(t, root)
	instagramPost(t, target, root, 1, "301", "target")
	syncInstagram(t, target, 0)
	album := countDB(t, target, "SELECT media_id FROM instagram_profile_album WHERE owner_id='301'")
	execDB(t, target, "UPDATE media SET custom_title='keep local title' WHERE id=?", album)
	sourcePath := filepath.Join(root, "source.db")
	source := latestDB(t, sourcePath)
	instagramPost(t, source, root, 10, "301", "source")
	syncInstagram(t, source, 0)
	source.Close()
	before := fileDigest(t, sourcePath)
	for i := 0; i < 2; i++ {
		if _, err := service.Import(t.Context(), sourcePath); err != nil {
			t.Fatal(err)
		}
		if countDB(t, target, "SELECT count(*) FROM instagram_profile_album") != 1 || countDB(t, target, "SELECT count(*) FROM user_album_file WHERE album_id=?", album) != 2 {
			t.Fatal("import duplicated/lost profile", i)
		}
		if textDB(t, target, "SELECT custom_title FROM media WHERE id=?", album) != "keep local title" {
			t.Fatal("import overwrote local title")
		}
	}
	if fileDigest(t, sourcePath) != before {
		t.Fatal("source database changed")
	}
	assertHealthyDB(t, target)
}
