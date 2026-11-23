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
	if name == "gallery_pin" || table == "gallery_pin" {
		return "", false
	}
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

func TestGalleryViewsHideAlbumMembersAndRestoreLastRemovedMembership(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "gallery-visibility.db"))
	catalog := media.NewService(db)
	for id := int64(1); id <= 4; id++ {
		addModernMedia(t, db, id, fmt.Sprintf("https://example.test/view/%d", id), fmt.Sprintf("original %d", id), filepath.Join(root, fmt.Sprintf("file-%d.mp4", id)), fmt.Sprint(id))
	}
	execDB(t, db, "UPDATE media_file SET media_id=1,position=9 WHERE id=2; DELETE FROM media WHERE id=2")
	first := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("첫 앨범"), AddFileIDs: []int64{1, 3}}))
	second := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("둘째 앨범"), AddFileIDs: []int64{1}}))
	show := false
	read := func(view string, expanded ...int64) media.Object {
		t.Helper()
		return albumObject(t, catalog.List(t.Context(), media.ListRequest{Gallery: true, GalleryView: view, ShowAlbumFiles: &show, ExpandedMediaIDs: expanded, Limit: 24}))
	}
	all := read("all")
	if all["total"] != int64(4) {
		t.Fatal("hidden files affected gallery totals", all)
	}
	for _, row := range all["data"].([]media.Object) {
		if row["id"] == int64(3) {
			t.Fatal("fully collected source remained visible", row)
		}
		if row["id"] == int64(1) && (row["galleryFile"].(media.Object)["id"] != int64(2) || row["galleryFileOffset"] != 1 || row["galleryCanCollapse"] != false) {
			t.Fatal("partially collected source exposed hidden files or lost original offsets", row)
		}
	}
	files := read("files")
	if files["total"] != int64(2) {
		t.Fatal(files)
	}
	for _, row := range files["data"].([]media.Object) {
		file := row["galleryFile"].(media.Object)
		if row["isUserAlbum"] == true || (file["id"] != int64(2) && file["id"] != int64(4)) || row["galleryCanCollapse"] != false {
			t.Fatal("file-only view contains an album or hidden member", row)
		}
	}
	albums := read("albums", first["id"].(int64))
	if albums["total"] != int64(3) {
		t.Fatal("album contents were hidden", albums)
	}
	for _, row := range albums["data"].([]media.Object) {
		if row["isUserAlbum"] != true {
			t.Fatal("album-only view includes a source card", row)
		}
	}
	// Every visible slot must remain reachable exactly once across small pages.
	seen := map[string]bool{}
	for offset := 0; offset < 4; offset++ {
		page := albumObject(t, catalog.List(t.Context(), media.ListRequest{Gallery: true, GalleryView: "all", ShowAlbumFiles: &show, Limit: 1, Offset: offset}))
		rows := page["data"].([]media.Object)
		if len(rows) != 1 || page["total"] != int64(4) || page["hasNextPage"] != (offset < 3) {
			t.Fatal("visibility broke pagination", page)
		}
		key := fmt.Sprint(rows[0]["id"])
		if seen[key] {
			t.Fatal("duplicate page entry", key)
		}
		seen[key] = true
	}
	show = true
	if read("all")["total"] != int64(5) || read("files")["total"] != int64(4) || read("albums")["total"] != int64(2) {
		t.Fatal("show hidden files did not restore the original cards")
	}
	// Existing adapters that omit the options retain their original list shape.
	legacy := albumObject(t, catalog.List(t.Context(), media.ListRequest{Gallery: true}))
	if legacy["total"] != int64(5) {
		t.Fatal(legacy)
	}
	show = false
	albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{AlbumID: first["id"].(int64), ExpectedRevision: first["revision"].(int64), RemoveFileIDs: []int64{1}}))
	if read("files")["total"] != int64(2) {
		t.Fatal("file with another membership was revealed")
	}
	albumObject(t, catalog.UnregisterMedia(t.Context(), media.UnregisterRequest{MediaID: second["id"].(int64), ExpectedRevision: second["revision"].(int64)}))
	if read("files")["total"] != int64(3) || countDB(t, db, "SELECT count(*) FROM media_file") != 4 {
		t.Fatal("last album removal did not reveal the original file")
	}
	if result := catalog.List(t.Context(), media.ListRequest{Gallery: true, GalleryView: "invalid"}); result.OK {
		t.Fatal("invalid gallery view accepted")
	}
}

func TestGalleryHiddenFilePagingKeepsOriginalOffsetsAndSearchFilters(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "gallery-file-pages.db"))
	catalog := media.NewService(db)
	addModernMedia(t, db, 1, "https://example.test/large", "large-source", filepath.Join(root, "source.mp4"), "original")
	addModernMedia(t, db, 2, "https://example.test/other", "other-source", filepath.Join(root, "other.mp4"), "other")
	members := []int64{1}
	for i := 0; i < 70; i++ {
		id := int64(10 + i)
		execDB(t, db, "INSERT INTO media_file(id,media_id,file_key,storage_kind,filepath,kind,position,created_at) VALUES(?,1,?,'app_managed',?,'video',?,1000)", id, fmt.Sprint(id), filepath.Join(root, fmt.Sprintf("file-%d.mp4", id)), (i+1)*3)
		if i%2 == 0 {
			members = append(members, id)
		}
	}
	album := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("collection"), AddFileIDs: members}))
	show := false
	read := func(req media.ListRequest) media.Object {
		t.Helper()
		req.Gallery, req.GalleryView, req.ShowAlbumFiles, req.Limit = true, "files", &show, 24
		return albumObject(t, catalog.List(t.Context(), req))
	}
	seen := map[int64]bool{}
	for offset := 0; offset < 36; offset += 24 {
		page := read(media.ListRequest{Offset: offset})
		rows := page["data"].([]media.Object)
		if page["total"] != int64(36) || len(rows) != min(24, 36-offset) || page["hasNextPage"] != (offset == 0) {
			t.Fatal("filtered file pages have incorrect bounds", page)
		}
		for _, row := range rows {
			file := row["galleryFile"].(media.Object)
			id := file["id"].(int64)
			if seen[id] || (id != 2 && (id < 11 || id%2 == 0)) {
				t.Fatal("duplicate or hidden file in page", row)
			}
			seen[id] = true
			if id != 2 && row["galleryFileOffset"] != int(id-9) {
				t.Fatal("visible offset no longer locates the original detail row", row)
			}
		}
	}
	execDB(t, db, "INSERT INTO favorite(media_id,created_at) VALUES(1,0)")
	for _, req := range []media.ListRequest{{Filter: media.Filter{Keyword: "large-source"}}, {FavoritesOnly: true}} {
		if page := read(req); page["total"] != int64(35) {
			t.Fatal("view options ignored search or favorites", page)
		}
	}
	for {
		more, err := catalog.RefreshSearchIndex(t.Context(), 128)
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
	}
	if page := read(media.ListRequest{Filter: media.Filter{Keyword: "large-source"}}); page["total"] != int64(35) {
		t.Fatal("indexed search changed file visibility", page)
	}
	empty := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("empty")}))
	page := albumObject(t, catalog.List(t.Context(), media.ListRequest{Gallery: true, GalleryView: "albums", ShowAlbumFiles: &show, ExpandedMediaIDs: []int64{album["id"].(int64), empty["id"].(int64)}}))
	if page["total"] != int64(len(members)+1) {
		t.Fatal("album view hid member files or the empty album", page)
	}
}

func TestMergeAlbumsPreservesSourcesAndRejectsStaleSelection(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "merge-albums.db"))
	catalog := media.NewService(db)
	for id := int64(1); id <= 3; id++ {
		addModernMedia(t, db, id, fmt.Sprintf("https://example.test/merge/%d", id), fmt.Sprint(id), filepath.Join(root, fmt.Sprintf("file-%d.mp4", id)), fmt.Sprint(id))
	}
	existing := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("기존"), AddFileIDs: []int64{2, 1}}))
	revision := countDB(t, db, "SELECT revision FROM media WHERE id=2")
	request := media.MergeAlbumRequest{Title: "모음", Selection: []media.AlbumSelection{{MediaID: existing["id"].(int64), Revision: existing["revision"].(int64)}, {MediaID: 2, Revision: revision, FileIDs: []int64{2}}}}
	merged := albumObject(t, catalog.MergeAlbum(t.Context(), request))
	sequence := albumObject(t, catalog.Sequence(t.Context(), media.SequenceRequest{MediaID: merged["id"].(int64)}))
	if fmt.Sprint(sequence["ids"]) != "[2 1]" || merged["fileCount"] != int64(2) || countDB(t, db, "SELECT count(*) FROM media_file") != 3 || countDB(t, db, "SELECT count(*) FROM media WHERE id IN (1,2,3)") != 3 {
		t.Fatal("병합이 순서·중복 제거·원본을 보존하지 못했습니다", merged, sequence)
	}
	before := countDB(t, db, "SELECT count(*) FROM user_album")
	for _, change := range []string{"stale", "foreign-file", "empty-title", "empty-selection"} {
		t.Run(change, func(t *testing.T) {
			bad := media.MergeAlbumRequest{Title: "실패", Selection: []media.AlbumSelection{{MediaID: 2, Revision: revision}}}
			switch change {
			case "stale":
				bad.Selection[0].Revision++
			case "foreign-file":
				bad.Selection[0].FileIDs = []int64{3}
			case "empty-title":
				bad.Title = " "
			case "empty-selection":
				bad.Selection = nil
			}
			if result := catalog.MergeAlbum(t.Context(), bad); result.OK {
				t.Fatal("잘못된 병합을 허용했습니다", result)
			}
			if countDB(t, db, "SELECT count(*) FROM user_album") != before {
				t.Fatal("실패한 병합의 일부가 남았습니다")
			}
		})
	}
}

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
	if countDB(t, upgraded, "PRAGMA user_version") != 12 || textDB(t, upgraded, "SELECT file_uuid FROM media_file WHERE id=7") != identity {
		t.Fatal("migration lost identity")
	}
	backup := rawDB(t, textDB(t, upgraded, "SELECT backup_path FROM migration_run WHERE target_version=12"))
	if countDB(t, backup, "PRAGMA user_version") != 10 {
		t.Fatal("backup not preserved")
	}
}
