package database_test

import (
	"fmt"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"path/filepath"
	"testing"
)

func TestManualPlatformsPreserveSourcesAndSupportBulkFilters(t *testing.T) {
	root := migrationRoot(t)
	db := latestDB(t, filepath.Join(root, "platforms.db"))
	catalog := media.NewService(db)
	for id := int64(1); id <= 2; id++ {
		addModernMedia(t, db, id, fmt.Sprintf("https://example.test/%d", id), "album", filepath.Join(root, fmt.Sprint(id)+".png"), fmt.Sprint(id))
	}
	execDB(t, db, "INSERT INTO platform(name) VALUES('Instagram') ON CONFLICT DO NOTHING")
	execDB(t, db, "UPDATE media_file SET platform_id=(SELECT id FROM platform WHERE name='Instagram') WHERE media_id=1")
	result := albumObject(t, catalog.BulkEdit(t.Context(), media.BulkEditRequest{Selection: []media.AlbumSelection{{MediaID: 1, Revision: 5}, {MediaID: 2, Revision: 5}}, AddPlatforms: []string{" Archive ", "Archive"}}))
	if len(result["updated"].([]media.Object)) != 2 {
		t.Fatal(result)
	}
	if countDB(t, db, "SELECT count(*) FROM media_platform") != 2 || countDB(t, db, "SELECT count(*) FROM media_file f JOIN platform p ON p.id=f.platform_id WHERE f.media_id=1 AND p.name='Instagram'") != 1 {
		t.Fatal("classification changed source or duplicated links")
	}
	detail := albumObject(t, catalog.Get(t.Context(), media.GetRequest{MediaID: 1, FileLimit: 1}))
	if len(detail["platforms"].([]media.Object)) != 1 {
		t.Fatal(detail)
	}
	listed := albumObject(t, catalog.List(t.Context(), media.ListRequest{Limit: 24, Filter: media.Filter{Platforms: []string{"Archive"}}}))
	if listed["total"] != int64(2) {
		t.Fatal(listed)
	}
	conflict := albumObject(t, catalog.BulkEdit(t.Context(), media.BulkEditRequest{Selection: []media.AlbumSelection{{MediaID: 1, Revision: 5}}, RemovePlatforms: []string{"Archive"}}))
	if len(conflict["failed"].([]media.Object)) != 1 || countDB(t, db, "SELECT count(*) FROM media_platform") != 2 {
		t.Fatal("stale revision changed classifications", conflict)
	}
	albumObject(t, catalog.BulkEdit(t.Context(), media.BulkEditRequest{Selection: []media.AlbumSelection{{MediaID: 1, Revision: 6}}, RemovePlatforms: []string{"Archive"}}))
	listed = albumObject(t, catalog.List(t.Context(), media.ListRequest{Limit: 24, Filter: media.Filter{Platforms: []string{"Instagram"}}}))
	if listed["total"] != int64(1) {
		t.Fatal("original source no longer searchable", listed)
	}
}
