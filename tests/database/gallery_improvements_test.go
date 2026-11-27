package database_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/media"
)

func TestGalleryBulkTagsPreserveUnrelatedTagsAndReportConflicts(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "bulk.db"))
	catalog := media.NewService(db)
	for id := int64(1); id <= 2; id++ {
		addModernMedia(t, db, id, fmt.Sprintf("https://example.test/%d", id), "album", filepath.Join(root, fmt.Sprint(id)+".png"), fmt.Sprint(id))
	}
	execDB(t, db, "INSERT INTO tag(id,name) VALUES(1,'keep'),(2,'remove')")
	execDB(t, db, "INSERT INTO media_tag VALUES(1,1),(1,2),(2,1)")
	result := albumObject(t, catalog.BulkEdit(t.Context(), media.BulkEditRequest{Selection: []media.AlbumSelection{{MediaID: 1, Revision: 5}, {MediaID: 2, Revision: 4}, {MediaID: 999, Revision: 0}}, AddTags: []string{" shared ", "shared"}, RemoveTags: []string{"remove"}}))
	if len(result["updated"].([]media.Object)) != 1 || len(result["failed"].([]media.Object)) != 2 {
		t.Fatal(result)
	}
	if countDB(t, db, "SELECT count(*) FROM media_tag mt JOIN tag t ON t.id=mt.tag_id WHERE media_id=1 AND name IN ('keep','shared')") != 2 || countDB(t, db, "SELECT count(*) FROM media_tag WHERE media_id=2") != 1 || countDB(t, db, "SELECT revision FROM media WHERE id=1") != 6 {
		t.Fatal("unrelated tags or conflicted media changed")
	}
	albumObject(t, catalog.BulkEdit(t.Context(), media.BulkEditRequest{Selection: []media.AlbumSelection{{MediaID: 1, Revision: 6}}, RemoveTags: []string{"shared"}}))
	if countDB(t, db, "SELECT count(*) FROM media_tag mt JOIN tag t ON t.id=mt.tag_id WHERE media_id=1 AND name='keep'") != 1 {
		t.Fatal("removal lost unrelated tags")
	}
	// An unexpected SQL failure must undo tags and revisions for the whole batch.
	execDB(t, db, "CREATE TRIGGER fixture_bulk_failure BEFORE INSERT ON media_tag WHEN NEW.media_id=2 BEGIN SELECT RAISE(ABORT,'fixture'); END")
	failed := catalog.BulkEdit(t.Context(), media.BulkEditRequest{Selection: []media.AlbumSelection{{MediaID: 1, Revision: 7}, {MediaID: 2, Revision: 5}}, AddTags: []string{"rollback"}})
	if failed.OK || countDB(t, db, "SELECT count(*) FROM tag WHERE name='rollback'") != 0 || countDB(t, db, "SELECT revision FROM media WHERE id=1") != 7 {
		t.Fatal("partial batch committed", failed)
	}
}

func TestCollapsedGalleryMatchesWeightedPagesAndRefreshesLinkedCover(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "gallery.db"))
	catalog := media.NewService(db)
	for id := int64(1); id <= 30; id++ {
		addModernMedia(t, db, id, fmt.Sprintf("https://example.test/%d", id), fmt.Sprintf("photo %d", id), filepath.Join(root, fmt.Sprint(id)+".png"), fmt.Sprint(id))
	}
	album := albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("collection"), AddFileIDs: []int64{1, 2}}))
	parent := album["id"].(int64)
	albumObject(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: albumTitle("nested"), ParentID: &parent}))
	setGalleryPin(t, db, 3, 0, true)
	for _, hide := range []bool{true, false} {
		for _, view := range []string{"all", "albums"} {
			for _, offset := range []int{0, 4, 20, 1000} {
				req := media.ListRequest{Gallery: true, AlbumCards: true, GalleryView: view, ShowAlbumFiles: &hide, Limit: 4, Offset: offset}
				fast := albumObject(t, catalog.List(t.Context(), req))
				rows := fast["data"].([]media.Object)
				if len(rows) == 0 {
					t.Fatal("unexpected empty gallery")
				}
				req.AnchorMediaID = rows[0]["id"].(int64)
				weighted := albumObject(t, catalog.List(t.Context(), req))
				if !reflect.DeepEqual(fast, weighted) {
					a, _ := json.Marshal(fast)
					b, _ := json.Marshal(weighted)
					t.Fatalf("view=%s offset=%d\nfast=%s\nweighted=%s", view, offset, a, b)
				}
			}
		}
	}
	thumb := filepath.Join(root, "updated-cover.jpg")
	execDB(t, db, "UPDATE media_file SET thumbnail_path=?,thumbnail_origin='app_generated' WHERE id=1", thumb)
	result := albumObject(t, catalog.Metadata(t.Context(), media.MetadataRequest{MediaIDs: []int64{parent, 3}, ChangedMediaIDs: []int64{1}, FileIDs: []int64{1}}))
	rows := result["media"].([]media.Object)
	if len(rows) != 1 || rows[0]["id"] != parent || rows[0]["cover"].(media.Object)["path"] != thumb {
		t.Fatal("linked album did not refresh", result)
	}
}

func TestGalleryCollapsedPagePerformance(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "gallery-performance.db"))
	catalog := media.NewService(db)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10000; i++ {
		if _, err = tx.Exec("INSERT INTO media(source_kind,source_title,created_at) VALUES('legacy',?,?)", fmt.Sprint(i), i); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec("INSERT INTO media_file(media_id,file_key,storage_kind,filepath,kind,position,created_at) SELECT id,CAST(id AS TEXT),'unverified_legacy',? || id || '.png','image',0,0 FROM media", filepath.Join(root, "photo-")); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	req := media.ListRequest{Gallery: true, AlbumCards: true, GalleryView: "all", Limit: 24, Offset: 240}
	start := time.Now()
	page := albumObject(t, catalog.List(t.Context(), req))
	fast := time.Since(start)
	req.AnchorMediaID = page["data"].([]media.Object)[0]["id"].(int64)
	start = time.Now()
	reference := albumObject(t, catalog.List(t.Context(), req))
	weighted := time.Since(start)
	if !reflect.DeepEqual(page, reference) {
		t.Fatal("page differs from weighted reference")
	}
	t.Logf("10,000 media, next 24 cards: collapsed=%s weighted=%s", fast, weighted)
}

func TestGalleryBulkEditPerformance(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "bulk-performance.db"))
	catalog := media.NewService(db)
	selection := []media.AlbumSelection{}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for id := int64(1); id <= 100; id++ {
		if _, err = tx.Exec("INSERT INTO media(id,source_kind,source_title,created_at) VALUES(?,'legacy',?,?)", id, fmt.Sprint(id), id); err != nil {
			t.Fatal(err)
		}
		selection = append(selection, media.AlbumSelection{MediaID: id, Revision: 0})
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	albumObject(t, catalog.BulkEdit(t.Context(), media.BulkEditRequest{Selection: selection, AddTags: []string{"batch"}}))
	batch := time.Since(start)
	start = time.Now()
	for _, target := range selection {
		var request media.EditRequest
		if err = json.Unmarshal([]byte(`{"tagChange":{"addNames":["serial"]}}`), &request); err != nil {
			t.Fatal(err)
		}
		request.MediaID, request.ExpectedRevision = target.MediaID, 1
		albumObject(t, catalog.Edit(t.Context(), request))
	}
	if countDB(t, db, "SELECT count(*) FROM media_tag") != 200 {
		t.Fatal("tags were lost")
	}
	t.Logf("100 media tag additions: batch=%s serial edits=%s", batch, time.Since(start))
}
