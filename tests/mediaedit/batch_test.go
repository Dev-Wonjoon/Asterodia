package mediaedit_test

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/mediaedit"
	"github.com/google/uuid"
)

func batchSource(t *testing.T, f *fixture) mediaedit.BatchSelection {
	t.Helper()
	var selected mediaedit.BatchSelection
	if err := f.db.QueryRow("SELECT m.id,m.revision FROM media m JOIN media_file f ON f.media_id=m.id WHERE f.id=?", f.fileID).Scan(&selected.MediaID, &selected.Revision); err != nil {
		t.Fatal(err)
	}
	selected.FileIDs = []int64{f.fileID}
	return selected
}

func awaitBatch(t *testing.T, f *fixture, id string) mediaedit.Job {
	t.Helper()
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		job := success[mediaedit.Job](t, f.service.GetJob(t.Context(), mediaedit.Request{ID: id}))
		if job.State == "completed" || job.State == "failed" || job.State == "cancelled" || job.State == "interrupted" {
			return job
		}
	}
	t.Fatal("batch timed out")
	return mediaedit.Job{}
}

func TestBatchConversionFormatsAndSourcePreservation(t *testing.T) {
	f := newFixture(t)
	original, err := os.ReadFile(f.source)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(original)
	formats := success[[]engine.OutputFormat](t, f.service.Formats(t.Context()))
	if len(formats) < 7 {
		t.Fatal("managed FFmpeg formats missing", formats)
	}
	for _, format := range []engine.FormatOptions{
		{Type: "video", Extension: "webm", VideoCodec: "vp9", AudioCodec: "opus"},
		{Type: "video", Extension: "mov", VideoCodec: "h264", AudioCodec: "aac"},
		{Type: "video", Extension: "mp4", VideoCodec: "hevc", AudioCodec: "aac"},
		{Type: "video", Extension: "mkv", VideoCodec: "av1", AudioCodec: "opus"},
		{Type: "audio", Extension: "m4a", AudioCodec: "aac"},
		{Type: "audio", Extension: "ogg", AudioCodec: "vorbis"},
		{Type: "audio", Extension: "flac", AudioCodec: "flac"},
		{Type: "audio", Extension: "wav", AudioCodec: "pcm"},
		{Type: "audio", Extension: "webm", AudioCodec: "opus"},
	} {
		t.Run(format.Type+"-"+format.Extension, func(t *testing.T) {
			req := mediaedit.BatchRequest{RequestID: uuid.NewString(), Selection: []mediaedit.BatchSelection{batchSource(t, f)}, Mode: "convert", Format: format}
			result := success[mediaedit.BatchResult](t, f.service.Batch(t.Context(), req))
			if len(result.Jobs) != 1 || len(result.Failures) != 0 {
				t.Fatal(result)
			}
			replay := success[mediaedit.BatchResult](t, f.service.Batch(t.Context(), req))
			if len(replay.Jobs) != 1 || replay.Jobs[0].ID != result.Jobs[0].ID {
				t.Fatal("submission duplicated")
			}
			job := awaitBatch(t, f, result.Jobs[0].ID)
			if job.State != "completed" {
				var detail string
				_ = f.db.QueryRow("SELECT diagnostic FROM media_edit_job WHERE id=?", job.ID).Scan(&detail)
				t.Fatal(job.State, job.Error, detail)
			}
			path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: job.ID}))
			if filepath.Ext(path) != "."+format.Extension {
				t.Fatal(path)
			}
			meta, err := (engine.LocalInspector{FFmpeg: f.ffmpeg, FFprobe: f.probe}).Inspect(t.Context(), path, format.Type)
			if err != nil || meta.Kind != format.Type {
				t.Fatal(meta, err)
			}
			if bad := f.service.RetryBatch(t.Context(), mediaedit.Request{ID: job.ID}); bad.OK {
				t.Fatal("completed file was retried")
			}
		})
	}
	after, err := os.ReadFile(f.source)
	if err != nil || sha256.Sum256(after) != hash {
		t.Fatal("original changed", err)
	}
}

func TestBatchWebPLengthBoundsAndRetry(t *testing.T) {
	f := newFixture(t)
	req := mediaedit.BatchRequest{RequestID: uuid.NewString(), Selection: []mediaedit.BatchSelection{batchSource(t, f)}, Mode: "webp", Start: 0, Duration: 30, Width: 96, FPS: 12}
	bad := req
	bad.Duration = 30.001
	if f.service.Batch(t.Context(), bad).OK {
		t.Fatal("accepted more than 30 seconds")
	}
	result := success[mediaedit.BatchResult](t, f.service.Batch(t.Context(), req))
	if len(result.Jobs) != 1 {
		t.Fatal(result)
	}
	job := awaitBatch(t, f, result.Jobs[0].ID)
	if job.State != "completed" {
		var diagnostic string
		_ = f.db.QueryRow("SELECT diagnostic FROM media_edit_job WHERE id=?", job.ID).Scan(&diagnostic)
		t.Fatal(job, diagnostic)
	}
	path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: job.ID}))
	meta, _, err := (engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}).Verify(t.Context(), path)
	if err != nil || *meta.Width != 96 || *meta.Duration > 1.1 {
		t.Fatal(meta, err)
	}
	if entries, e := os.ReadDir(f.cache); e != nil || len(entries) != 0 {
		t.Fatal("batch created frame caches", entries, e)
	}
	changed := req
	changed.Start = .5
	if f.service.Batch(t.Context(), changed).OK {
		t.Fatal("request id reused for another interval")
	}
	req.RequestID = uuid.NewString()
	req.Start = 2
	result = success[mediaedit.BatchResult](t, f.service.Batch(t.Context(), req))
	job = awaitBatch(t, f, result.Jobs[0].ID)
	if job.State != "failed" {
		t.Fatal("out of bounds source succeeded", job)
	}
	retry := success[mediaedit.Job](t, f.service.RetryBatch(t.Context(), mediaedit.Request{ID: job.ID}))
	if retry.ID == job.ID {
		t.Fatal("retry overwrote history")
	}
	if got := awaitBatch(t, f, retry.ID); got.State != "failed" {
		t.Fatal(got)
	}
}

func TestBatchQueuesMoreThanEditorLimitAndCancels(t *testing.T) {
	f := newFixture(t)
	selected := batchSource(t, f)
	for i := 1; i < 15; i++ {
		res, err := f.db.Exec(`INSERT INTO media_file(media_id,file_key,storage_kind,filepath,kind,position,duration,created_at) VALUES(?,?,'unverified_legacy',?,'video',?,1,0)`, selected.MediaID, uuid.NewString(), f.source, i)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		selected.FileIDs = append(selected.FileIDs, id)
	}
	req := mediaedit.BatchRequest{RequestID: uuid.NewString(), Selection: []mediaedit.BatchSelection{selected}, Mode: "webp", Duration: 30, Width: 64, FPS: 10}
	result := success[mediaedit.BatchResult](t, f.service.Batch(t.Context(), req))
	if len(result.Jobs) != 15 {
		t.Fatal(result)
	}
	if got := f.service.CancelBatch(t.Context(), mediaedit.Request{ID: req.RequestID}); !got.OK {
		t.Fatal(got)
	}
	for _, j := range result.Jobs {
		job := awaitBatch(t, f, j.ID)
		if job.State != "cancelled" && job.State != "completed" {
			t.Fatal(job)
		}
	}
}

func TestBatchWebPEnforcesThirtySecondsOnLongSources(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.root, "long.mp4")
	_, err := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: f.ffmpeg, Directory: f.root, Timeout: time.Minute, Arguments: []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=64x48:rate=5:duration=32", "-c:v", "libx264", path}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec("UPDATE media_file SET filepath=? WHERE id=?", path, f.fileID); err != nil {
		t.Fatal(err)
	}
	req := mediaedit.BatchRequest{RequestID: uuid.NewString(), Selection: []mediaedit.BatchSelection{batchSource(t, f)}, Mode: "webp", Duration: 30, Width: 64, FPS: 5}
	result := success[mediaedit.BatchResult](t, f.service.Batch(t.Context(), req))
	job := awaitBatch(t, f, result.Jobs[0].ID)
	if job.State != "completed" {
		t.Fatal(job)
	}
	output := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: job.ID}))
	meta, frames, err := (engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}).Verify(t.Context(), output)
	if err != nil || meta.Duration == nil || *meta.Duration > 30 || *meta.Duration < 29.99 || frames != 150 {
		t.Fatal(meta, frames, err)
	}
}

func TestBatchOverlappingSelectionsQueueEachFileOnce(t *testing.T) {
	f := newFixture(t)
	selected := batchSource(t, f)
	whole := selected
	whole.FileIDs = nil
	req := mediaedit.BatchRequest{RequestID: uuid.NewString(), Selection: []mediaedit.BatchSelection{selected, whole, selected}, Mode: "webp", Duration: 1, Width: 64, FPS: 10}
	result := success[mediaedit.BatchResult](t, f.service.Batch(t.Context(), req))
	if len(result.Jobs) != 1 || len(result.Failures) != 0 {
		t.Fatal(result)
	}
	if job := awaitBatch(t, f, result.Jobs[0].ID); job.State != "completed" {
		t.Fatal(job)
	}
}
