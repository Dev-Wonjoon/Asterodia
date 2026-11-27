package database_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/media"
)

func TestGalleryLargeAlbumCover(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "large-cover.db"))
	catalog := media.NewService(db)
	execDB(t, db, "INSERT INTO media(id,source_kind,source_title,created_at) VALUES(1,'legacy','large album',0)")
	execDB(t, db, `WITH RECURSIVE n(id) AS (SELECT 1 UNION ALL SELECT id+1 FROM n WHERE id<10000)
      INSERT INTO media_file(id,media_id,file_key,storage_kind,filepath,kind,position,created_at)
      SELECT id,1,CAST(id AS TEXT),'unverified_legacy',? || id || '.png','image',id,0 FROM n`, filepath.Join(root, "photo-"))
	for _, id := range []int64{10000, 1} {
		execDB(t, db, "UPDATE media SET cover_mode='manual',cover_file_id=? WHERE id=1", id)
		start := time.Now()
		result := albumObject(t, catalog.Metadata(t.Context(), media.MetadataRequest{MediaIDs: []int64{1}}))
		cover := result["media"].([]media.Object)[0]["cover"].(media.Object)
		if cover["fileId"] != id {
			t.Fatal("manual cover lost", cover)
		}
		t.Logf("10,000-file album cover %d: %s", id, time.Since(start))
	}
}
