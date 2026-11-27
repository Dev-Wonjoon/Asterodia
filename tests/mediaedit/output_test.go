package mediaedit_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"github.com/Dev-Wonjoon/Asterodia/internal/mediaedit"
	"github.com/google/uuid"
)

func outputAlbum(t *testing.T, f *fixture, title string) int64 {
	t.Helper()
	result, err := f.db.Exec("INSERT INTO media(source_kind,custom_title,created_at) VALUES('local',?,0)", title)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	if _, err = f.db.Exec("INSERT INTO user_album(media_id) VALUES(?)", id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestOutputDirectoryStoresOneFileWithAlbumReferencesAndResultTags(t *testing.T) {
	f := newFixture(t)
	a, b := outputAlbum(t, f, "여행"), outputAlbum(t, f, "움짤")
	v := f.prepare(t, "gif")
	destination := filepath.Join(f.root, "working")
	req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "webp", Crop: engine.EditCrop{Width: 160, Height: 120}, OutputDirectory: destination, AlbumIDs: []int64{b, a, a}, TagNames: []string{" 여행 ", "움짤", "여행"}}
	job := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
	job = f.waitJob(t, job.ID, "completed")
	path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: job.ID}))
	if filepath.Dir(path) != destination {
		t.Fatalf("result outside working folder: %s", path)
	}
	var copies, links, tags, originalTags int
	if err := f.db.QueryRow("SELECT count(*) FROM media_file WHERE file_uuid=(SELECT file_uuid FROM media_file WHERE id=?)", *job.ResultFileID).Scan(&copies); err != nil {
		t.Fatal(err)
	}
	f.db.QueryRow("SELECT count(*) FROM user_album_file WHERE file_id=?", *job.ResultFileID).Scan(&links)
	f.db.QueryRow("SELECT count(*) FROM media_tag WHERE media_id=(SELECT media_id FROM media_file WHERE id=?)", *job.ResultFileID).Scan(&tags)
	f.db.QueryRow("SELECT count(*) FROM media_tag WHERE media_id=(SELECT media_id FROM media_file WHERE id=?)", f.fileID).Scan(&originalTags)
	if copies != 1 || links != 2 || tags != 2 || originalTags != 0 {
		t.Fatalf("copies=%d links=%d tags=%d originalTags=%d", copies, links, tags, originalTags)
	}
	physical := 0
	if err := filepath.WalkDir(destination, func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".asterodia-tmp" {
			return filepath.SkipDir
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".webp") {
			physical++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if physical != 1 {
		t.Fatalf("physical copies=%d", physical)
	}
	// Simulate interruption after file publication but before the DB commit.
	f.service.Close()
	var mediaID int64
	f.db.QueryRow("SELECT media_id FROM media_file WHERE id=?", *job.ResultFileID).Scan(&mediaID)
	if _, err := f.db.Exec("DELETE FROM media WHERE id=?", mediaID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE media_edit_job SET state='publishing',result_file_id=NULL WHERE id=?", job.ID); err != nil {
		t.Fatal(err)
	}
	f.start(t, f.ffmpeg)
	recovered := f.waitJob(t, job.ID, "completed")
	if got := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: recovered.ID})); got != path {
		t.Fatal("recovery changed destination", got)
	}
	f.db.QueryRow("SELECT count(*) FROM user_album_file WHERE file_id=?", *recovered.ResultFileID).Scan(&links)
	if links != 2 {
		t.Fatal("recovery lost album references", links)
	}
}

func TestOutputDirectoryIsFrozenAndDefaultRetriesAreIdempotent(t *testing.T) {
	f := newFixture(t)
	v := f.prepare(t, "gif")
	original := filepath.Join(f.output, "edits")
	req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{Width: 160, Height: 120}}
	job := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
	f.output = filepath.Join(f.root, "changed-default")
	again := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
	if again.ID != job.ID || again.Options.OutputDirectory != original {
		t.Fatal("retry changed destination", again)
	}
	job = f.waitJob(t, job.ID, "completed")
	path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: job.ID}))
	if filepath.Dir(path) != original {
		t.Fatal("default change affected queued work", path)
	}
	req.RequestID = uuid.NewString()
	req.AlbumIDs = []int64{999999}
	if value := f.service.Create(t.Context(), req); value.OK {
		t.Fatal("missing album accepted")
	}
	req.AlbumIDs = nil
	req.TagNames = []string{strings.Repeat("가", 101)}
	if value := f.service.Create(t.Context(), req); value.OK {
		t.Fatal("oversized tag accepted")
	}
	req.TagNames = nil
	req.OutputDirectory = "relative"
	if value := f.service.Create(t.Context(), req); value.OK {
		t.Fatal("relative directory accepted")
	}
}

func TestOutputPreviewDoesNotClassifyOrWriteToWorkingDirectory(t *testing.T) {
	f := newFixture(t)
	album := outputAlbum(t, f, "샘플")
	v := f.prepare(t, "gif")
	req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{Width: 160, Height: 120}, Preview: true, OutputDirectory: filepath.Join(f.root, "unused"), AlbumIDs: []int64{album}, TagNames: []string{"미리보기"}}
	job := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
	job = f.waitJob(t, job.ID, "completed")
	if job.ResultFileID != nil || len(job.Options.AlbumIDs) != 0 || len(job.Options.TagNames) != 0 {
		t.Fatal("preview classified", job)
	}
	var path string
	f.db.QueryRow("SELECT result_path FROM media_edit_job WHERE id=?", job.ID).Scan(&path)
	if !strings.HasPrefix(path, f.cache+string(filepath.Separator)) {
		t.Fatal("preview outside cache", path)
	}
	if _, err := os.Stat(req.OutputDirectory); !os.IsNotExist(err) {
		t.Fatal("preview touched output directory", err)
	}
	// The new transport fields stay JSON round-trippable for persisted jobs.
	if _, err := json.Marshal(job); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyPublishedOutputStillRecoversUnderEdits(t *testing.T) {
	f := newFixture(t)
	v := f.prepare(t, "gif")
	req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{Width: 160, Height: 120}}
	job := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
	job = f.waitJob(t, job.ID, "completed")
	f.service.Close()
	var path, proofJSON string
	var mediaID int64
	if err := f.db.QueryRow("SELECT result_path,proof_json FROM media_edit_job WHERE id=?", job.ID).Scan(&path, &proofJSON); err != nil {
		t.Fatal(err)
	}
	var proof media.FileProof
	if err := json.Unmarshal([]byte(proofJSON), &proof); err != nil {
		t.Fatal(err)
	}
	proof.Path = filepath.Join("edits", filepath.Base(path))
	proofBytes, _ := json.Marshal(proof)
	legacy := job.Options
	legacy.OutputDirectory = ""
	requestBytes, _ := json.Marshal(legacy)
	f.db.QueryRow("SELECT media_id FROM media_file WHERE id=?", *job.ResultFileID).Scan(&mediaID)
	if _, err := f.db.Exec("DELETE FROM media WHERE id=?", mediaID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE media_edit_job SET request_json=?,output_root=?,proof_json=?,state='publishing',result_file_id=NULL WHERE id=?", string(requestBytes), f.output, string(proofBytes), job.ID); err != nil {
		t.Fatal(err)
	}
	f.start(t, f.ffmpeg)
	f.waitJob(t, job.ID, "completed")
	if got := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: job.ID})); got != path {
		t.Fatal("legacy result moved", got)
	}
}
