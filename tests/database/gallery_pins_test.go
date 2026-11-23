package database_test

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

func setGalleryPin(t *testing.T, db *sql.DB, mediaID, fileID int64, pinned bool) media.Object {
	t.Helper()
	revision := countDB(t, db, "SELECT revision FROM media WHERE id=?", mediaID)
	return albumObject(t, media.NewService(db).Edit(t.Context(), media.EditRequest{MediaID: mediaID, ExpectedRevision: revision, IsPinned: &pinned, PinFileID: fileID}))
}

func TestGalleryPinsSortAcrossPagesAndRestoreOriginalOrder(t *testing.T) {
	root := migrationRoot(t)
	path := filepath.Join(root, "pins.db")
	db := latestDB(t, path)
	for id := int64(1); id <= 4; id++ {
		addModernMedia(t, db, id, fmt.Sprintf("https://example.test/pin/%d", id), fmt.Sprintf("source %d", id), filepath.Join(root, fmt.Sprintf("%d.mp4", id)), fmt.Sprint(id))
		execDB(t, db, "UPDATE media SET created_at=? WHERE id=?", id*100, id)
	}
	catalog := media.NewService(db)
	album := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("collection"), AddFileIDs: []int64{1, 2}}))
	albumID := album["id"].(int64)
	setGalleryPin(t, db, 3, 0, true)
	setGalleryPin(t, db, albumID, 0, true)
	execDB(t, db, "UPDATE gallery_pin SET created_at=CASE media_id WHEN 3 THEN 100 ELSE 200 END")
	show := false
	read := func(offset int) media.Object {
		return albumObject(t, catalog.List(t.Context(), media.ListRequest{Gallery: true, GalleryView: "all", ShowAlbumFiles: &show, Limit: 1, Offset: offset}))
	}
	for i, want := range []int64{albumID, 3, 4} {
		page := read(i)
		row := page["data"].([]media.Object)[0]
		if row["id"] != want || page["total"] != int64(3) || page["hasNextPage"] != (i < 2) || row["isPinned"] != (i < 2) {
			t.Fatal("pin ordering or pagination", page)
		}
	}
	if countDB(t, db, "SELECT count(*) FROM favorite") != 0 {
		t.Fatal("pinning changed favorites")
	}
	db.Close()
	var err error
	db, err = database.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if countDB(t, db, "SELECT count(*) FROM gallery_pin") != 2 {
		t.Fatal("pins were lost on reopen")
	}
	setGalleryPin(t, db, 3, 0, false)
	catalog = media.NewService(db)
	if read(1)["data"].([]media.Object)[0]["id"] != int64(4) {
		t.Fatal("unpin did not restore creation order")
	}
	filtered := albumObject(t, catalog.List(t.Context(), media.ListRequest{Gallery: true, Filter: media.Filter{Keyword: "source 4"}}))
	if filtered["total"] != int64(1) || filtered["data"].([]media.Object)[0]["id"] != int64(4) {
		t.Fatal("pins bypassed a search filter", filtered)
	}
	// Deleting an album removes its pin without removing the original files.
	current := albumObject(t, catalog.Get(t.Context(), media.GetRequest{MediaID: albumID}))
	albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{AlbumID: albumID, ExpectedRevision: current["revision"].(int64), Delete: true}))
	if countDB(t, db, "SELECT count(*) FROM gallery_pin") != 0 || countDB(t, db, "SELECT count(*) FROM media_file") != 4 {
		t.Fatal("album pin cleanup affected files")
	}
}

func TestPinnedFileAppearsOnceOutsideCollapsedAndExpandedAlbums(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "file-pins.db"))
	for id := int64(1); id <= 4; id++ {
		addModernMedia(t, db, id, fmt.Sprintf("https://example.test/files/%d", id), fmt.Sprintf("original %d", id), filepath.Join(root, fmt.Sprintf("%d.mp4", id)), fmt.Sprint(id))
	}
	execDB(t, db, "UPDATE media_file SET media_id=1,position=20 WHERE id=2;DELETE FROM media WHERE id=2")
	catalog := media.NewService(db)
	a := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("first collection"), AddFileIDs: []int64{2, 3}}))["id"].(int64)
	b := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("second collection"), AddFileIDs: []int64{2}}))["id"].(int64)
	beforeSource := countDB(t, db, "SELECT revision FROM media WHERE id=1")
	beforeOther := countDB(t, db, "SELECT revision FROM media WHERE id=?", b)
	setGalleryPin(t, db, a, 2, true)
	if countDB(t, db, "SELECT revision FROM media WHERE id=1") != beforeSource+1 || countDB(t, db, "SELECT revision FROM media WHERE id=?", b) != beforeOther+1 {
		t.Fatal("other representations were not invalidated")
	}
	show := false
	read := func(req media.ListRequest) media.Object {
		req.Gallery, req.ShowAlbumFiles = true, &show
		return albumObject(t, catalog.List(t.Context(), req))
	}
	for _, expanded := range [][]int64{nil, {1, a, b}} {
		page := read(media.ListRequest{Limit: 100, ExpandedMediaIDs: expanded})
		rows := page["data"].([]media.Object)
		if rows[0]["galleryFile"].(media.Object)["id"] != int64(2) || rows[0]["isPinned"] != true || rows[0]["galleryFileOffset"] != 1 || rows[0]["galleryCanCollapse"] != false {
			t.Fatal("file pin did not escape its album", page)
		}
		seen := 0
		for _, row := range rows {
			if file, ok := row["galleryFile"].(media.Object); ok && file["id"] == int64(2) {
				seen++
			}
		}
		if seen != 1 || int64(len(rows)) != page["total"] {
			t.Fatal("pinned file duplicated or paging totals diverged", page)
		}
		for offset := range rows {
			one := read(media.ListRequest{Limit: 1, Offset: offset, ExpandedMediaIDs: expanded})
			if one["total"] != page["total"] || len(one["data"].([]media.Object)) != 1 {
				t.Fatal("file pins broke small pages", one)
			}
		}
	}
	filtered := read(media.ListRequest{Filter: media.Filter{Keyword: "first collection"}})
	row := filtered["data"].([]media.Object)[0]
	if row["id"] != a || row["galleryFileOffset"] != 0 {
		t.Fatal("filtered album pin lost its context", filtered)
	}
	files := read(media.ListRequest{GalleryView: "files"})
	if files["data"].([]media.Object)[0]["galleryFile"].(media.Object)["id"] != int64(2) {
		t.Fatal("files view lost an explicitly pinned hidden member", files)
	}
	albums := read(media.ListRequest{GalleryView: "albums"})
	if albums["total"] != int64(2) {
		t.Fatal("file pins appeared in album-only view", albums)
	}
	setGalleryPin(t, db, b, 2, false)
	if row := read(media.ListRequest{})["data"].([]media.Object)[0]; row["galleryFile"] != nil && row["isPinned"] == true {
		t.Fatal("unpin through another album failed", row)
	}
	if countDB(t, db, "SELECT count(*) FROM user_album_file WHERE file_id=2") != 2 {
		t.Fatal("pinning moved the file out of an album")
	}
	// A source card cannot pin an unrelated file, or overwrite a stale revision.
	pinned := true
	for _, req := range []media.EditRequest{
		{MediaID: 4, ExpectedRevision: 5, IsPinned: &pinned, PinFileID: 2},
		{MediaID: a, ExpectedRevision: 0, IsPinned: &pinned, PinFileID: 2},
		{MediaID: 4, ExpectedRevision: 5, PinFileID: 4},
	} {
		if result := catalog.Edit(t.Context(), req); result.OK {
			t.Fatal("invalid or stale pin request accepted", req)
		}
	}
	setGalleryPin(t, db, 4, 4, true)
	albumObject(t, catalog.UnregisterFiles(t.Context(), media.UnregisterRequest{MediaID: 4, ExpectedRevision: countDB(t, db, "SELECT revision FROM media WHERE id=4"), FileIDs: []int64{4}}))
	if countDB(t, db, "SELECT count(*) FROM gallery_pin") != 0 {
		t.Fatal("deleted file left a pin behind")
	}
}

func TestPinnedDownloadGroupRemainsAccessibleWhenItsFilesAreCollectedOrPinned(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "pinned-group.db"))
	for id := int64(1); id <= 2; id++ {
		addModernMedia(t, db, id, fmt.Sprintf("https://example.test/collected/%d", id), "download", filepath.Join(root, fmt.Sprintf("%d.mp4", id)), fmt.Sprint(id))
	}
	execDB(t, db, "UPDATE media_file SET media_id=1,position=9 WHERE id=2;DELETE FROM media WHERE id=2")
	catalog := media.NewService(db)
	albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("collected"), AddFileIDs: []int64{1, 2}}))
	setGalleryPin(t, db, 1, 0, true)
	setGalleryPin(t, db, 1, 2, true)
	show := false
	read := func(expanded ...int64) []media.Object {
		return albumObject(t, catalog.List(t.Context(), media.ListRequest{Gallery: true, ShowAlbumFiles: &show, ExpandedMediaIDs: expanded}))["data"].([]media.Object)
	}
	found := false
	for _, row := range read() {
		if row["id"] == int64(1) && row["galleryFile"] == nil && row["isPinned"] == true {
			found = true
		}
	}
	if !found {
		t.Fatal("explicit group pin was hidden with its collected files")
	}
	seen := map[int64]int{}
	for _, row := range read(1) {
		if file, ok := row["galleryFile"].(media.Object); ok {
			seen[file["id"].(int64)]++
		}
	}
	if seen[1] != 1 || seen[2] != 1 {
		t.Fatal("expanding a pinned group lost or duplicated files", seen)
	}
	setGalleryPin(t, db, 1, 1, true)
	found = false
	for _, row := range read(1) {
		if row["galleryFile"] == nil && row["id"] == int64(1) {
			found = row["isPinned"] == true && row["galleryCanCollapse"] == false
		}
	}
	if !found {
		t.Fatal("pinning every child removed the parent unpin action")
	}
}

func TestV11PinMigrationRollsBackAndPreservesAlbumData(t *testing.T) {
	root := migrationRoot(t)
	path := filepath.Join(root, "v11.db")
	db := latestDB(t, path)
	addModernMedia(t, db, 1, "https://example.test/v11", "keep", filepath.Join(root, "keep.mp4"), "keep")
	albumObject(t, media.NewService(db).SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("keep album"), AddFileIDs: []int64{1}}))
	execDB(t, db, "DROP TABLE gallery_pin;PRAGMA user_version=11")
	db.Close()
	opts := database.MigrationOptions{BackupDirectory: filepath.Join(root, "backups"), Checkpoint: func(step string) error {
		if step == "before_commit" {
			return errors.New("injected failure")
		}
		return nil
	}}
	if opened, err := database.OpenWithOptions(t.Context(), path, opts); err == nil {
		opened.Close()
		t.Fatal("migration did not roll back")
	}
	old := rawDB(t, path)
	if countDB(t, old, "PRAGMA user_version") != 11 || countDB(t, old, "SELECT count(*) FROM sqlite_master WHERE name='gallery_pin'") != 0 {
		t.Fatal("failed migration changed the old schema")
	}
	old.Close()
	opts.Checkpoint = nil
	upgraded, err := database.OpenWithOptions(t.Context(), path, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	if countDB(t, upgraded, "PRAGMA user_version") != 12 || countDB(t, upgraded, "SELECT count(*) FROM user_album_file WHERE file_id=1") != 1 || countDB(t, upgraded, "SELECT count(*) FROM gallery_pin") != 0 {
		t.Fatal("migration lost existing data")
	}
	assertHealthyDB(t, upgraded)
}

func TestImportedPinsAreRemappedAndLocalUnpinIsPreserved(t *testing.T) {
	root := migrationRoot(t)
	target, importer, _ := importer(t, root)
	sourcePath := filepath.Join(root, "source-pins.db")
	source := latestDB(t, sourcePath)
	addModernMedia(t, source, 7, "https://example.test/import-pin", "pinned", filepath.Join(root, "original.mp4"), "file")
	setGalleryPin(t, source, 7, 0, true)
	setGalleryPin(t, source, 7, 7, true)
	source.Close()
	if _, err := importer.Import(t.Context(), sourcePath); err != nil {
		t.Fatal(err)
	}
	if countDB(t, target, "SELECT count(*) FROM gallery_pin") != 2 || countDB(t, target, "SELECT count(*) FROM gallery_pin p JOIN media_file f ON f.id=p.file_id") != 1 {
		t.Fatal("import did not remap pins")
	}
	execDB(t, target, "DELETE FROM gallery_pin")
	if _, err := importer.Import(t.Context(), sourcePath); err != nil {
		t.Fatal(err)
	}
	if countDB(t, target, "SELECT count(*) FROM gallery_pin") != 0 {
		t.Fatal("reimport overwrote local unpin choices")
	}
}
