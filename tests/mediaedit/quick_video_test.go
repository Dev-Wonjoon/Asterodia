package mediaedit_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/mediaedit"
	"github.com/google/uuid"
)

func prepareVideoMode(t *testing.T, f *fixture, req mediaedit.PrepareRequest) mediaedit.Session {
	t.Helper()
	req.RequestID, req.FileID, req.FPS, req.Kind = uuid.NewString(), f.fileID, 1000, "gif"
	v := success[mediaedit.Session](t, f.service.Prepare(t.Context(), req))
	if req.QuickVideo && v.State != "ready" {
		t.Fatal("basic video metadata waited in the encoding queue", v.State)
	}
	for until := time.Now().Add(20 * time.Second); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
		v = success[mediaedit.Session](t, f.service.GetSession(t.Context(), mediaedit.Request{ID: v.ID}))
		if v.State == "ready" {
			if replay := success[mediaedit.Session](t, f.service.Prepare(t.Context(), req)); replay.ID != v.ID {
				t.Fatal("retry duplicated session")
			}
			if req.OriginalFrames {
				changed := req
				changed.Start += .01
				if result := f.service.Prepare(t.Context(), changed); result.OK {
					t.Fatal("request ID accepted another source interval")
				}
			}
			return v
		}
		if v.State == "failed" {
			var diagnostic string
			_ = f.db.QueryRow("SELECT diagnostic FROM media_edit_session WHERE id=?", v.ID).Scan(&diagnostic)
			t.Fatal(v.Error, diagnostic)
		}
	}
	t.Fatal("video metadata preparation timed out")
	return v
}

func quickSource(t *testing.T, f *fixture, rate string, offset bool) {
	t.Helper()
	path := filepath.Join(f.root, "quick-"+uuid.NewString()+".mp4")
	args := []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=" + rate + ":duration=4.3", "-c:v", "libx264", "-g", "90", "-pix_fmt", "yuv420p"}
	if offset {
		args = append(args, "-output_ts_offset", "5")
	}
	args = append(args, path)
	if _, err := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: f.ffmpeg, Directory: f.root, Timeout: time.Minute, Arguments: args}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE media_file SET filepath=?,duration=NULL WHERE id=?", path, f.fileID); err != nil {
		t.Fatal(err)
	}
}

func TestQuickVideoSkipsFrameCachesAndCreatesSelectedNativeFrames(t *testing.T) {
	f := newFixture(t)
	quickSource(t, f, "30", false)
	v := prepareVideoMode(t, f, mediaedit.PrepareRequest{QuickVideo: true, FullSource: true})
	if !v.Options.QuickVideo || v.Options.NativeVideo || v.FrameCount != 0 || v.Options.End < 4.29 {
		t.Fatal(v)
	}
	if entries, err := os.ReadDir(filepath.Join(f.cache, v.ID)); err != nil && !os.IsNotExist(err) || len(entries) != 0 {
		t.Fatal("quick preparation generated frame caches", entries, err)
	}
	var pages int
	if err := f.db.QueryRow("SELECT count(*) FROM media_edit_page WHERE session_id=?", v.ID).Scan(&pages); err != nil || pages != 0 {
		t.Fatal("quick preparation queued frames", pages, err)
	}
	start, end := 1.25, 2.75
	for _, format := range []string{"gif", "webp"} {
		j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: format, TrimStart: &start, TrimEnd: &end, Crop: engine.EditCrop{Width: 77, Height: 89}}))
		j = f.waitJob(t, j.ID, "completed")
		path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: j.ID}))
		metadata, count, err := (engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}).Verify(t.Context(), path)
		if err != nil || count != 45 || *metadata.Width != 77 || *metadata.Height != 89 || math.Abs(*metadata.Duration-1.483) > .015 {
			t.Fatalf("quick %s: metadata=%+v frames=%d err=%v", format, metadata, count, err)
		}
	}
	for _, change := range []func(*mediaedit.CreateRequest){
		func(r *mediaedit.CreateRequest) { r.RangeEnd = 1 }, func(r *mediaedit.CreateRequest) { r.Deleted = []int{0} }, func(r *mediaedit.CreateRequest) { r.SelectionID = uuid.NewString() },
	} {
		req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", TrimStart: &start, TrimEnd: &end, Crop: engine.EditCrop{Width: 160, Height: 120}}
		change(&req)
		if result := f.service.Create(t.Context(), req); result.OK {
			t.Fatal("quick mode accepted frame selection", req)
		}
	}
	start, end = 0, 4
	j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "webp", Preview: true, TrimStart: &start, TrimEnd: &end, Crop: engine.EditCrop{Width: 160, Height: 120}}))
	j = f.waitJob(t, j.ID, "completed")
	if j.Resources.OutputFrames > 60 {
		t.Fatal("quick preview exceeded two seconds", j.Resources)
	}
	start, end = 2.3, 2.6
	j = success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "trim", TrimStart: &start, TrimEnd: &end}))
	f.waitJob(t, j.ID, "completed")
	if result := f.service.Release(t.Context(), mediaedit.Request{ID: v.ID}); !result.OK {
		t.Fatal("quick session without a frame cache could not be released", result.Error)
	}
}

func TestPreciseVideoIndexesOnlyTheChosenInterval(t *testing.T) {
	f := newFixture(t)
	quickSource(t, f, "30", false)
	v := prepareVideoMode(t, f, mediaedit.PrepareRequest{OriginalFrames: true, Start: 2.3, End: 3.1})
	if !v.Options.NativeVideo || v.Options.QuickVideo || v.FrameCount != 24 || v.Options.Start != 2.3 || v.Options.End != 3.1 {
		t.Fatal(v)
	}
	page := success[mediaedit.FramePage](t, f.service.FrameStatus(t.Context(), mediaedit.FramesRequest{ID: v.ID}))
	if page.State != "ready" || len(page.Times) != 24 || math.Abs(page.Times[0]-2.3) > .001 || page.Times[23] >= 3.1 {
		t.Fatal(page)
	}
	j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "webp", RangeStart: 2, RangeEnd: 22, Deleted: []int{7}, Crop: engine.EditCrop{Width: 160, Height: 120}}))
	j = f.waitJob(t, j.ID, "completed")
	path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: j.ID}))
	metadata, count, err := (engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}).Verify(t.Context(), path)
	if err != nil || count != 19 || math.Abs(*metadata.Duration-.633) > .003 {
		t.Fatal(metadata, count, err)
	}
}

func TestQuickVideoNonzeroTimestampAndHighRate(t *testing.T) {
	f := newFixture(t)
	quickSource(t, f, "30", true)
	v := prepareVideoMode(t, f, mediaedit.PrepareRequest{QuickVideo: true, FullSource: true})
	start, end := 1.25, 2.75
	j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "webp", TrimStart: &start, TrimEnd: &end, Crop: engine.EditCrop{Width: 160, Height: 120}}))
	f.waitJob(t, j.ID, "completed")
	quickSource(t, f, "120", false)
	v = prepareVideoMode(t, f, mediaedit.PrepareRequest{QuickVideo: true, FullSource: true})
	start, end = 1, 1.25
	for _, format := range []string{"gif", "webp"} {
		j = success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: format, TrimStart: &start, TrimEnd: &end, Crop: engine.EditCrop{Width: 160, Height: 120}}))
		state := "completed"
		if format == "gif" {
			state = "failed"
		}
		j = f.waitJob(t, j.ID, state)
		if format == "gif" && !strings.Contains(j.Error, "WebP") {
			t.Fatal(j.Error)
		}
	}
}

func TestQuickVideoPreservesVariableFrameTimingWithinSelection(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.root, "quick-variable.mp4")
	_, err := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: f.ffmpeg, Directory: f.root, Timeout: time.Minute, Arguments: []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10:duration=1", "-vf", "setpts=(N+floor(N/3))/(10*TB)", "-fps_mode", "vfr", "-c:v", "libx264", path}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec("UPDATE media_file SET filepath=? WHERE id=?", path, f.fileID); err != nil {
		t.Fatal(err)
	}
	v := prepareVideoMode(t, f, mediaedit.PrepareRequest{QuickVideo: true, FullSource: true})
	start, end := .4, .9
	for _, format := range []string{"gif", "webp"} {
		j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: format, TrimStart: &start, TrimEnd: &end, Crop: engine.EditCrop{Width: 160, Height: 120}}))
		j = f.waitJob(t, j.ID, "completed")
		output := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: j.ID}))
		meta, count, e := (engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}).Verify(t.Context(), output)
		if e != nil || count != 4 || math.Abs(*meta.Duration-.5) > .002 {
			t.Fatal(format, meta, count, e)
		}
	}
}
