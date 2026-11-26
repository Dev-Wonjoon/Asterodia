package database_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

func albumParent(id int64) *int64 { return &id }

func TestAlbumExplorerHierarchySearchAndPagination(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "explorer.db"))
	catalog := media.NewService(db)
	for i := int64(1); i <= 3; i++ {
		addModernMedia(t, db, i, fmt.Sprintf("https://example.test/%d", i), fmt.Sprintf("source %d", i), filepath.Join(root, fmt.Sprintf("%d.mp4", i)), fmt.Sprint(i))
	}
	execDB(t, db, `INSERT INTO platform(id,name)VALUES(1,'YouTube'),(2,'Other');INSERT INTO profiles(id,platform_id,owner_id,owner_name,updated_at)VALUES(1,1,'author','작가',0);UPDATE media_file SET platform_id=1,profile_id=1,filesize=300,title='제주 영상' WHERE id=1;UPDATE media_file SET platform_id=2,filesize=100,title='부산 영상' WHERE id=2;UPDATE media_file SET filesize=200,title='제주 사진' WHERE id=3;INSERT INTO tag(id,name)VALUES(1,'여행'),(2,'풍경');INSERT INTO media_tag(media_id,tag_id)VALUES(1,1),(1,2),(2,1)`)
	parent := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("여행 앨범"), AddFileIDs: []int64{1, 2}}))
	parentID := parent["id"].(int64)
	child := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("제주도"), ParentID: &parentID, AddFileIDs: []int64{1, 3}}))
	childID := child["id"].(int64)
	grand := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("하위 앨범"), ParentID: &childID}))
	grandID := grand["id"].(int64)
	read := func(req media.ExploreAlbumRequest) media.Object {
		t.Helper()
		req.MediaID = parentID
		return albumObject(t, catalog.ExploreAlbum(t.Context(), req))
	}
	page := read(media.ExploreAlbumRequest{Limit: 1})
	if page["total"] != int64(3) || !page["hasNextPage"].(bool) || page["data"].([]media.Object)[0]["kind"] != "album" {
		t.Fatal(page)
	}
	seen := map[string]bool{}
	for offset := 0; offset < 3; offset++ {
		p := read(media.ExploreAlbumRequest{Limit: 1, Offset: offset})
		entries := p["data"].([]media.Object)
		if len(entries) != 1 {
			t.Fatal(p)
		}
		key := fmt.Sprint(entries[0])
		if seen[key] {
			t.Fatal("duplicate page", entries)
		}
		seen[key] = true
	}
	page = read(media.ExploreAlbumRequest{Recursive: true})
	if page["total"] != int64(5) {
		t.Fatal("recursive search must deduplicate shared files", page)
	}
	for _, test := range []struct {
		filter    media.Filter
		recursive bool
		total     int64
	}{
		{media.Filter{Keyword: "제주"}, false, 2},
		{media.Filter{Tags: []string{"여행", "풍경"}, TagMode: "and"}, false, 2},
		{media.Filter{Tags: []string{"여행", "풍경"}, TagMode: "or"}, false, 3},
		{media.Filter{Platforms: []string{"YouTube"}, Authors: []string{"작가"}}, false, 2},
		{media.Filter{Keyword: "제주 사진"}, false, 1}, // The child album matches its referenced file title.
		{media.Filter{Keyword: "제주 사진"}, true, 2},
	} {
		p := read(media.ExploreAlbumRequest{Filter: test.filter, Recursive: test.recursive})
		if p["total"] != test.total {
			t.Fatalf("%+v: %v", test, p)
		}
	}
	page = read(media.ExploreAlbumRequest{Sort: "size", Descending: true})
	entries := page["data"].([]media.Object)
	if entries[1]["file"].(media.Object)["id"] != int64(1) {
		t.Fatal("file sort", page)
	}
	deep := albumObject(t, catalog.ExploreAlbum(t.Context(), media.ExploreAlbumRequest{MediaID: grandID}))
	crumbs := deep["breadcrumbs"].([]media.Object)
	if len(crumbs) != 3 || crumbs[0]["id"] != parentID || crumbs[2]["id"] != grandID {
		t.Fatal(crumbs)
	}
	before := read(media.ExploreAlbumRequest{Recursive: true})
	grand = albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{AlbumID: grandID, ExpectedRevision: grand["revision"].(int64), Title: albumTitle("renamed grandchild")}))
	after := read(media.ExploreAlbumRequest{Recursive: true})
	if before["revisionToken"] == after["revisionToken"] || before["album"].(media.Object)["revision"] != after["album"].(media.Object)["revision"] {
		t.Fatal("descendant edits did not invalidate the paging snapshot")
	}
	parent = albumObject(t, catalog.Get(t.Context(), media.GetRequest{MediaID: parentID}))
	cycle := catalog.SaveAlbum(t.Context(), media.AlbumRequest{AlbumID: parentID, ExpectedRevision: parent["revision"].(int64), ParentID: &grandID})
	if cycle.OK || cycle.Error.Code != "INVALID_ARGUMENT" {
		t.Fatal("cycle accepted", cycle)
	}
	if _, err := db.Exec("INSERT INTO album_parent(album_id,parent_id)VALUES(?,?)", parentID, grandID); err == nil {
		t.Fatal("SQL cycle accepted")
	}
	child = albumObject(t, catalog.Get(t.Context(), media.GetRequest{MediaID: childID}))
	albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{AlbumID: childID, ExpectedRevision: child["revision"].(int64), ParentID: albumParent(0)}))
	if countDB(t, db, "SELECT count(*) FROM album_parent WHERE album_id=?", childID) != 0 {
		t.Fatal("move to root failed")
	}
	stale := catalog.SaveAlbum(t.Context(), media.AlbumRequest{AlbumID: childID, ExpectedRevision: child["revision"].(int64), Title: albumTitle("stale")})
	if stale.OK || stale.Error.Code != "REVISION_CONFLICT" {
		t.Fatal(stale)
	}
	// A source group remains reachable as a folder even after all its files have
	// been collected into its own child album.
	albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("source child"), ParentID: albumParent(1), AddFileIDs: []int64{1}}))
	showFiles := false
	gallery := albumObject(t, catalog.List(t.Context(), media.ListRequest{Gallery: true, ShowAlbumFiles: &showFiles, Limit: 100}))
	found := false
	for _, row := range gallery["data"].([]media.Object) {
		if row["id"] == int64(1) && row["galleryFile"] == nil {
			found = true
		}
	}
	if !found {
		t.Fatal("source parent disappeared after creating its subalbum", gallery)
	}
	catalog.ConfigureContentFilters(func() media.ContentFilters { return media.ContentFilters{HiddenPlatforms: []string{"YouTube"}} })
	if result := catalog.ExploreAlbum(t.Context(), media.ExploreAlbumRequest{MediaID: parentID}); result.OK || result.Error.Code != "NOT_FOUND" {
		t.Fatal("hidden album exposed", result)
	}
	read(media.ExploreAlbumRequest{Filter: media.Filter{ShowHidden: true}})
	child = albumObject(t, catalog.Get(t.Context(), media.GetRequest{MediaID: childID}))
	albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{AlbumID: childID, ExpectedRevision: child["revision"].(int64), Delete: true}))
	if countDB(t, db, "SELECT count(*) FROM media WHERE id=?", grandID) != 1 || countDB(t, db, "SELECT count(*) FROM album_parent WHERE album_id=?", grandID) != 0 || countDB(t, db, "SELECT count(*) FROM media_file") != 3 {
		t.Fatal("removing parent removed children or files")
	}
	assertHealthyDB(t, db)
}

func TestV12AlbumHierarchyMigrationRollbackBackupAndImport(t *testing.T) {
	root := migrationRoot(t)
	path := filepath.Join(root, "v12.db")
	db := latestDB(t, path)
	addModernMedia(t, db, 1, "https://example.test/v12", "keep", filepath.Join(root, "keep.mp4"), "keep")
	albumObject(t, media.NewService(db).SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("kept album"), AddFileIDs: []int64{1}}))
	execDB(t, db, "DROP TABLE instagram_profile_source;DROP TABLE instagram_profile_album;DROP TABLE album_parent;PRAGMA user_version=12")
	db.Close()
	opts := database.MigrationOptions{BackupDirectory: filepath.Join(root, "backups"), Checkpoint: func(step string) error {
		if step == "before_commit" {
			return errors.New("injected")
		}
		return nil
	}}
	if opened, err := database.OpenWithOptions(t.Context(), path, opts); err == nil {
		opened.Close()
		t.Fatal("rollback failed")
	}
	old := rawDB(t, path)
	if countDB(t, old, "PRAGMA user_version") != 12 || countDB(t, old, "SELECT count(*) FROM sqlite_master WHERE name='album_parent'") != 0 {
		t.Fatal("failed migration changed source")
	}
	old.Close()
	opts.Checkpoint = nil
	upgraded, err := database.OpenWithOptions(t.Context(), path, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	if countDB(t, upgraded, "PRAGMA user_version") != 14 || countDB(t, upgraded, "SELECT count(*) FROM user_album_file") != 1 {
		t.Fatal("migration lost membership")
	}
	backup := rawDB(t, textDB(t, upgraded, "SELECT backup_path FROM migration_run WHERE target_version=14"))
	defer backup.Close()
	if countDB(t, backup, "PRAGMA user_version") != 12 {
		t.Fatal("backup version")
	}
	catalog := media.NewService(upgraded)
	parent := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("parent")}))
	albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("child"), ParentID: albumParent(parent["id"].(int64))}))
	target, importer, _ := importer(t, filepath.Join(root, "target"))
	if _, err = importer.Import(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	if countDB(t, target, "SELECT count(*) FROM album_parent ap JOIN media m ON m.id=ap.album_id JOIN media p ON p.id=ap.parent_id WHERE m.custom_title='child' AND p.custom_title='parent'") != 1 {
		t.Fatal("import lost remapped hierarchy")
	}
	execDB(t, target, "DELETE FROM album_parent")
	if _, err = importer.Import(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	if countDB(t, target, "SELECT count(*) FROM album_parent") != 0 {
		t.Fatal("reimport overwrote local placement")
	}
	assertHealthyDB(t, target)
}
