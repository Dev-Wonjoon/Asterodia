package database_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/media"
)

func TestGalleryPresentationSortsBeforePagingAndPreservesPins(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "presentation.db"))
	catalog := media.NewService(db)
	for id := int64(1); id <= 2; id++ {
		addModernMedia(t, db, id, fmt.Sprintf("https://test.invalid/%d", id), map[int64]string{1: "Zulu", 2: "Alpha"}[id], filepath.Join(root, fmt.Sprint(id)+".mp4"), fmt.Sprint(id))
	}
	execDB(t, db, "UPDATE media SET created_at=id*100")
	execDB(t, db, "UPDATE media_file SET title=CASE id WHEN 1 THEN 'Alpha file' ELSE 'Zulu file' END,filesize=id*1024")
	execDB(t, db, "UPDATE file_metadata SET birth_ms=CASE file_id WHEN 1 THEN 100 ELSE 50 END,checked_at=1")
	execDB(t, db, "INSERT INTO platform(id,name) VALUES(1,'Alpha platform'),(2,'Zulu platform')")
	execDB(t, db, "UPDATE media_file SET platform_id=id")
	execDB(t, db, "INSERT INTO tag(id,name,updated_at) VALUES(1,'Zulu tag',1),(2,'Alpha tag',1);INSERT INTO media_tag(media_id,tag_id) VALUES(1,1),(2,2)")
	for _, test := range []struct {
		sort       string
		descending bool
		first      int64
	}{{"name", false, 2}, {"registered", false, 1}, {"fileCreated", false, 2}, {"fileCreated", true, 1}, {"platform", false, 1}, {"tag", false, 2}} {
		t.Run(test.sort+fmt.Sprint(test.descending), func(t *testing.T) {
			req := media.ListRequest{Gallery: true, AlbumCards: true, Limit: 1, Sort: test.sort, Descending: test.descending, Details: true}
			first := albumObject(t, catalog.List(t.Context(), req))
			rows := first["data"].([]media.Object)
			if len(rows) != 1 || rows[0]["id"] != test.first || first["total"] != int64(2) {
				t.Fatal(first)
			}
			if rows[0]["byteSize"] == nil || rows[0]["fileCreatedAt"] == nil || len(rows[0]["platformNames"].([]string)) != 1 || len(rows[0]["tagNames"].([]string)) != 1 {
				t.Fatal("missing page metadata", rows)
			}
			req.Offset = 1
			second := albumObject(t, catalog.List(t.Context(), req))
			if second["data"].([]media.Object)[0]["id"] == test.first {
				t.Fatal("sort only applied to loaded page", second)
			}
		})
	}
	execDB(t, db, "INSERT INTO gallery_pin(media_id,created_at) VALUES(1,1)")
	pinned := albumObject(t, catalog.List(t.Context(), media.ListRequest{Gallery: true, AlbumCards: true, Limit: 1, Sort: "name"}))
	if pinned["data"].([]media.Object)[0]["id"] != int64(1) {
		t.Fatal("pin order lost", pinned)
	}
	execDB(t, db, "DELETE FROM gallery_pin")
	execDB(t, db, "INSERT INTO media_file(id,media_id,file_key,storage_kind,filepath,kind,position,title,created_at) VALUES(3,1,'third','app_managed',?,'video',1,'Middle file',200)", filepath.Join(root, "third.mp4"))
	files := albumObject(t, catalog.List(t.Context(), media.ListRequest{Gallery: true, AlbumCards: true, GalleryView: "files", Limit: 1, Offset: 1, Sort: "name", Details: true}))
	row := files["data"].([]media.Object)[0]
	if files["total"] != int64(3) || row["galleryFile"].(media.Object)["id"] != int64(3) || row["galleryFileOffset"] != int64(1) {
		t.Fatal("files were sorted inside albums instead of globally", files)
	}
	invalid := catalog.List(t.Context(), media.ListRequest{Gallery: true, Sort: "name; DROP TABLE media"})
	if invalid.OK || invalid.Error.Code != "INVALID_ARGUMENT" {
		t.Fatal(invalid)
	}
}

func TestFileDatesAreCachedAndRequeuedAfterPathChanges(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "dates.db"))
	catalog := media.NewService(db)
	path := filepath.Join(root, "sample.txt")
	if err := os.WriteFile(path, []byte("sample"), 0600); err != nil {
		t.Fatal(err)
	}
	addModernMedia(t, db, 1, "https://test.invalid/1", "sample", path, "one")
	if countDB(t, db, "SELECT count(*) FROM file_metadata WHERE checked_at=0") != 1 {
		t.Fatal("new file was not queued")
	}
	if count, err := catalog.RefreshFileMetadata(t.Context(), 32); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if runtime.GOOS == "windows" && countDB(t, db, "SELECT count(*) FROM file_metadata WHERE checked_at>0 AND birth_ms>1000") != 1 {
		t.Fatal("actual creation time was not collected")
	}
	if count, err := catalog.RefreshFileMetadata(t.Context(), 32); err != nil || count != 0 {
		t.Fatal("cached file was scanned again", count, err)
	}
	execDB(t, db, "UPDATE media_file SET filepath=? WHERE id=1", filepath.Join(root, "missing.txt"))
	if countDB(t, db, "SELECT count(*) FROM file_metadata WHERE checked_at=0 AND birth_ms IS NULL AND generation=1") != 1 {
		t.Fatal("changed path kept a stale date")
	}
	if count, err := catalog.RefreshFileMetadata(t.Context(), 32); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if countDB(t, db, "SELECT count(*) FROM file_metadata WHERE checked_at>0 AND birth_ms IS NULL") != 1 {
		t.Fatal("unavailable date was fabricated")
	}
}
