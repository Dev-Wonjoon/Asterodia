package database_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

// Existing historical fixtures are reconstructed from current DDL. Strip the
// complete v11 additions before applying their own older-version differences.
func beforeAlbumSchema(name, table, ddl string) (string, bool) {
	if strings.HasPrefix(name, "user_album") || strings.HasPrefix(table, "user_album") || strings.HasPrefix(name, "idx_album_") || strings.HasPrefix(name, "search_album_") || strings.HasPrefix(name, "v11_") {
		return "", false
	}
	ddl = strings.ReplaceAll(ddl, ", operation text NOT NULL DEFAULT 'move' CHECK(operation IN ('move','copy'))", "")
	if name == "search_source" {
		if start := strings.Index(ddl, "\n UNION SELECT a.album_id"); start >= 0 {
			ddl = ddl[:start]
		}
	}
	return ddl, true
}

func albumObject(t *testing.T, result api.Result) media.Object {
	t.Helper()
	if !result.OK {
		t.Fatalf("%+v", result.Error)
	}
	return result.Data.(map[string]any)
}
func albumTitle(s string) *string { return &s }

func TestUserAlbumSearchTracksMembershipAndFileChanges(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "search-albums.db"))
	catalog := media.NewService(db)
	addModernMedia(t, db, 1, "https://example.test/search-album", "original", filepath.Join(root, "file.mp4"), "file")
	execDB(t, db, "UPDATE media_file SET title='바다 사진' WHERE id=1")
	album := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("collection"), AddFileIDs: []int64{1}}))
	id := album["id"].(int64)
	refresh := func() {
		t.Helper()
		for {
			work, err := catalog.RefreshSearchIndex(t.Context(), 128)
			if err != nil {
				t.Fatal(err)
			}
			if !work {
				return
			}
		}
	}
	check := func(keyword string, total int64) {
		t.Helper()
		page := albumObject(t, catalog.List(t.Context(), media.ListRequest{Filter: media.Filter{Keyword: keyword}}))
		if page["total"] != total {
			t.Fatal(page)
		}
	}
	check("바다", 2)
	refresh()
	check("바다", 2)
	execDB(t, db, "UPDATE media_file SET title='산 사진' WHERE id=1")
	refresh()
	check("바다", 0)
	check("산", 2)
	rev := countDB(t, db, "SELECT revision FROM media WHERE id=?", id)
	albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{AlbumID: id, ExpectedRevision: rev, RemoveFileIDs: []int64{1}}))
	refresh()
	check("산", 1)
}

func TestUserAlbumsPreserveOwnershipAndOrder(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "albums.db"))
	catalog := media.NewService(db)
	for i := int64(1); i <= 3; i++ {
		addModernMedia(t, db, i, fmt.Sprintf("https://example.test/%d", i), fmt.Sprintf("original %d", i), filepath.Join(root, fmt.Sprintf("folder-%d", i), "file.mp4"), fmt.Sprint(i))
	}
	first := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("여행"), AddFileIDs: []int64{3, 1, 3}}))
	id := first["id"].(int64)
	rev := first["revision"].(int64)
	if first["isUserAlbum"] != true || first["fileCount"] != int64(2) || countDB(t, db, "SELECT count(*) FROM media_file") != 3 {
		t.Fatal(first)
	}
	seq := albumObject(t, catalog.Sequence(t.Context(), media.SequenceRequest{MediaID: id}))
	if fmt.Sprint(seq["ids"]) != "[3 1]" {
		t.Fatal(seq)
	}
	// Reorder with an optimistic revision and reject a stale edit.
	edited := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{AlbumID: id, ExpectedRevision: rev, MoveFileID: 1, BeforeFileID: 3}))
	stale := catalog.SaveAlbum(t.Context(), media.AlbumRequest{AlbumID: id, ExpectedRevision: rev, Title: albumTitle("stale")})
	if stale.OK || stale.Error.Code != "REVISION_CONFLICT" {
		t.Fatal(stale)
	}
	albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("공유"), AddMediaIDs: []int64{id}, AddFileIDs: []int64{2}}))
	detail := albumObject(t, catalog.Get(t.Context(), media.GetRequest{MediaID: id, FileLimit: 1, FileOffset: 1}))
	files := detail["files"].(media.Object)["data"].([]media.Object)
	if files[0]["id"] != int64(3) {
		t.Fatal(detail)
	}
	albumObject(t, catalog.Edit(t.Context(), media.EditRequest{MediaID: id, ExpectedRevision: edited["revision"].(int64), Cover: &struct {
		Mode   string `json:"mode"`
		FileID int64  `json:"fileId"`
	}{Mode: "manual", FileID: 3}}))
	rev = countDB(t, db, "SELECT revision FROM media WHERE id=?", id)
	albumObject(t, catalog.UnregisterFiles(t.Context(), media.UnregisterRequest{MediaID: id, ExpectedRevision: rev, FileIDs: []int64{3}}))
	if countDB(t, db, "SELECT count(*) FROM media_file WHERE id=3 AND media_id=3") != 1 || countDB(t, db, "SELECT count(*) FROM user_album_file WHERE file_id=3") != 1 {
		t.Fatal("membership removal affected original or another album")
	}
	rev = countDB(t, db, "SELECT revision FROM media WHERE id=?", id)
	albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{AlbumID: id, ExpectedRevision: rev, Delete: true}))
	if countDB(t, db, "SELECT count(*) FROM media_file") != 3 || countDB(t, db, "SELECT count(*) FROM maintenance_task") != 0 {
		t.Fatal("album deletion scheduled original cleanup")
	}
	if rows, err := db.Query("PRAGMA foreign_key_check"); err != nil {
		t.Fatal(err)
	} else {
		defer rows.Close()
		if rows.Next() {
			t.Fatal("broken references")
		}
	}
}

func TestUserAlbumImportRemapsMembersAndEmptyAlbums(t *testing.T) {
	root := migrationRoot(t)
	sourcePath := filepath.Join(root, "source.db")
	source := latestDB(t, sourcePath)
	addModernMedia(t, source, 7, "https://example.test/member", "original", filepath.Join(root, "file.mp4"), "file")
	catalog := media.NewService(source)
	albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("collected"), AddFileIDs: []int64{7}}))
	albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("empty")}))
	source.Close()
	target, importer, _ := importer(t, root)
	result, err := importer.Import(t.Context(), sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedMedia != 3 || result.AddedFiles != 1 || countDB(t, target, "SELECT count(*) FROM user_album") != 2 || countDB(t, target, "SELECT count(*) FROM user_album_file a JOIN media_file f ON f.id=a.file_id JOIN media m ON m.id=a.album_id WHERE m.custom_title='collected'") != 1 {
		t.Fatal(result)
	}
	result, err = importer.Import(t.Context(), sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedMedia != 0 || result.AddedFiles != 0 {
		t.Fatal(result)
	}
}

func TestV10AlbumMigrationPreservesFilesAndBackup(t *testing.T) {
	root := migrationRoot(t)
	current := latestDB(t, filepath.Join(root, "current.db"))
	rows, err := current.Query("SELECT name,tbl_name,sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 ELSE 2 END,name")
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{}
	for rows.Next() {
		var name, table, ddl string
		if err = rows.Scan(&name, &table, &ddl); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(name, "search_fts_") {
			continue
		}
		if old, ok := beforeAlbumSchema(name, table, ddl); ok {
			statements = append(statements, old+";")
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	current.Close()
	path := filepath.Join(root, "v10.db")
	old := rawDB(t, path)
	execDB(t, old, strings.Join(statements, "\n"))
	execDB(t, old, "INSERT INTO search_state(id)VALUES(1);PRAGMA user_version=10")
	addModernMedia(t, old, 7, "https://example.test/v10", "keep", filepath.Join(root, "keep.mp4"), "keep")
	identity := textDB(t, old, "SELECT file_uuid FROM media_file WHERE id=7")
	old.Close()
	upgraded, err := database.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	if countDB(t, upgraded, "PRAGMA user_version") != 11 || textDB(t, upgraded, "SELECT file_uuid FROM media_file WHERE id=7") != identity {
		t.Fatal("migration lost identity")
	}
	backup := rawDB(t, textDB(t, upgraded, "SELECT backup_path FROM migration_run WHERE target_version=11"))
	if countDB(t, backup, "PRAGMA user_version") != 10 {
		t.Fatal("backup not preserved")
	}
}
