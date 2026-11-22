package filemove_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/filemove"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

func TestCopySelectedFilePreservesOriginalAndNames(t *testing.T) {
	f := createFixture(t)
	var original, sibling string
	must(t, f.db.QueryRow("SELECT filepath FROM media_file WHERE id=1").Scan(&original))
	must(t, f.db.QueryRow("SELECT filepath FROM media_file WHERE id=2").Scan(&sibling))
	before, e := os.ReadFile(original)
	must(t, e)
	occupied := filepath.Join(f.destination, filepath.Base(original))
	must(t, os.WriteFile(occupied, []byte("keep existing"), 0600))
	id := ok(t, f.s.Create(t.Context(), filemove.CreateRequest{Selection: []filemove.Selection{{MediaID: 1, Revision: 5, FileIDs: []int64{1}}}, DirectoryID: f.dir, Operation: "copy"})).(map[string]string)["jobId"]
	ok(t, f.s.Run(t.Context(), filemove.Request{JobID: id}))
	status := f.wait(t, id)
	if status.State != "completed" || status.Total != 1 || status.Operation != "copy" {
		t.Fatal(status)
	}
	target := filepath.Join(f.destination, "A1-0 (1).mp4")
	copied, e := os.ReadFile(target)
	must(t, e)
	if !bytes.Equal(before, copied) {
		t.Fatal("copy content changed")
	}
	after, e := os.ReadFile(original)
	must(t, e)
	if !bytes.Equal(before, after) {
		t.Fatal("original changed")
	}
	_, e = os.Stat(sibling)
	must(t, e)
	var recorded string
	must(t, f.db.QueryRow("SELECT filepath FROM media_file WHERE id=1").Scan(&recorded))
	if recorded != original {
		t.Fatal("copy relocated DB reference")
	}
	occupant, e := os.ReadFile(occupied)
	must(t, e)
	if string(occupant) != "keep existing" {
		t.Fatal("overwrote collision")
	}
	// Running a completed copy is idempotent and never deletes its original.
	if f.s.Run(t.Context(), filemove.Request{JobID: id}).OK {
		t.Fatal("completed copy ran again")
	}
	_, e = os.Stat(original)
	must(t, e)
}

func TestGatherUserAlbumMovesOnlyMembersAndUpdatesEveryReference(t *testing.T) {
	f := createFixture(t)
	catalog := media.NewService(f.db)
	name := "collected"
	first := ok(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: &name, AddFileIDs: []int64{1, 3}})).(map[string]any)
	second := ok(t, catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: &name, AddFileIDs: []int64{1}})).(map[string]any)
	var original, sibling string
	must(t, f.db.QueryRow("SELECT filepath FROM media_file WHERE id=1").Scan(&original))
	must(t, f.db.QueryRow("SELECT filepath FROM media_file WHERE id=2").Scan(&sibling))
	id := ok(t, f.s.Create(t.Context(), filemove.CreateRequest{Selection: []filemove.Selection{{MediaID: first["id"].(int64), Revision: first["revision"].(int64)}}, DirectoryID: f.dir, Operation: "move"})).(map[string]string)["jobId"]
	ok(t, f.s.Run(t.Context(), filemove.Request{JobID: id}))
	status := f.wait(t, id)
	if status.State != "completed" || status.Total != 2 {
		t.Fatal(status)
	}
	if _, e := os.Stat(original); !os.IsNotExist(e) {
		t.Fatal("original not cleaned after verified move")
	}
	_, e := os.Stat(sibling)
	must(t, e)
	result := ok(t, catalog.Get(t.Context(), media.GetRequest{MediaID: second["id"].(int64)})).(map[string]any)
	files := result["files"].(media.Object)["data"].([]media.Object)
	if len(files) != 1 || files[0]["id"] != int64(1) || filepath.Dir(files[0]["filepath"].(string)) != f.destination {
		t.Fatal(result)
	}
	invalid := f.s.Create(t.Context(), filemove.CreateRequest{Selection: []filemove.Selection{{MediaID: second["id"].(int64), Revision: result["revision"].(int64), FileIDs: []int64{2}}}, DirectoryID: f.dir, Operation: "copy"})
	if invalid.OK {
		t.Fatal("accepted file outside selected album")
	}
}
