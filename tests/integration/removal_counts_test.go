package integration_test

import (
	"fmt"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

func TestRemovalSummaryUsesFileOwnership(t *testing.T) {
	f := newFixture(t)
	row, err := f.db.Exec("INSERT INTO media(source_kind,source_title,created_at) VALUES('legacy','Mixed ownership',0)")
	if err != nil {
		t.Fatal(err)
	}
	id, err := row.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	for n, kind := range []string{"app_managed", "unverified_legacy", "unverified_legacy"} {
		_, err = f.db.Exec("INSERT INTO media_file(media_id,file_key,storage_kind,filepath,kind,position,created_at) VALUES(?,?,?,?,'image',?,0)", id, fmt.Sprint(n), kind, fmt.Sprintf("C:/fixture/%d.png", n), n)
		if err != nil {
			t.Fatal(err)
		}
	}
	detail := success(t, f.catalog.Get(t.Context(), media.GetRequest{MediaID: id, FileLimit: 1})).(media.Object)
	if detail["fileCount"] != int64(3) || detail["managedFileCount"] != int64(1) {
		t.Fatal(detail)
	}
	page := success(t, f.catalog.List(t.Context(), media.ListRequest{Gallery: true, Limit: 24})).(media.Object)
	rows := page["data"].([]media.Object)
	if len(rows) != 1 || rows[0]["managedFileCount"] != int64(1) {
		t.Fatal(page)
	}
}
