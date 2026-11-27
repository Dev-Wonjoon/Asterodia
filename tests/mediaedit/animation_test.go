package mediaedit_test

import (
	"bytes"
	"crypto/sha256"
	"image/gif"
	"image/jpeg"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/mediaedit"
	"github.com/google/uuid"
)

func prepareWholeVideo(t *testing.T, f *fixture, kind string) mediaedit.Session {
	t.Helper()
	req := mediaedit.PrepareRequest{RequestID: uuid.NewString(), FileID: f.fileID, Kind: kind, FullSource: true, FPS: 1}
	v := success[mediaedit.Session](t, f.service.Prepare(t.Context(), req))
	for until := time.Now().Add(20 * time.Second); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
		v = success[mediaedit.Session](t, f.service.GetSession(t.Context(), mediaedit.Request{ID: v.ID}))
		if v.State == "ready" {
			replay := success[mediaedit.Session](t, f.service.Prepare(t.Context(), req))
			if replay.ID != v.ID {
				t.Fatal("whole-source retry created another session")
			}
			return v
		}
		if v.State == "failed" {
			var diagnostic string
			f.db.QueryRow("SELECT diagnostic FROM media_edit_session WHERE id=?", v.ID).Scan(&diagnostic)
			t.Fatal(v.Error, diagnostic)
		}
	}
	t.Fatal("whole video preparation timed out")
	return v
}

func TestWholeVideoIncludesEveryFrameAndEditsPastThreeSeconds(t *testing.T) {
	f := newFixture(t)
	source := filepath.Join(f.root, "whole-source.mp4")
	_, err := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: f.ffmpeg, Directory: f.root, Timeout: time.Minute, Arguments: []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=30000/1001", "-frames:v", "127", "-c:v", "libx264", "-pix_fmt", "yuv420p", source}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec("UPDATE media_file SET filepath=?,duration=NULL WHERE id=?", source, f.fileID); err != nil {
		t.Fatal(err)
	}
	v := prepareWholeVideo(t, f, "gif")
	if !v.Options.FullSource || !v.Options.NativeVideo || v.FrameCount != 127 || v.Options.Start != 0 || v.Options.End < 4.23 {
		t.Fatalf("missing original frames: %+v", v)
	}
	p := success[mediaedit.FramePage](t, f.service.Frames(t.Context(), mediaedit.FramesRequest{ID: v.ID, Offset: 126}))
	for until := time.Now().Add(15 * time.Second); p.State != "ready" && time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
		p = success[mediaedit.FramePage](t, f.service.FrameStatus(t.Context(), mediaedit.FramesRequest{ID: v.ID, Offset: 126}))
	}
	if p.State != "ready" || p.Offset != 96 || p.Count != 31 || len(p.Times) != 31 || p.Times[30] < 4.2 {
		t.Fatalf("last original page: %+v", p)
	}
	for _, format := range []string{"gif", "webp"} {
		j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: format, RangeStart: 100, RangeEnd: 120, Deleted: []int{110}, Crop: engine.EditCrop{Width: 160, Height: 120}}))
		j = f.waitJob(t, j.ID, "completed")
		output := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: j.ID}))
		editor := engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}
		metadata, count, err := editor.Verify(t.Context(), output)
		if err != nil || count != 19 || math.Abs(*metadata.Duration-.634) > .011 {
			t.Fatalf("native %s kept range: %+v frames=%d err=%v", format, metadata, count, err)
		}
	}
	j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Preview: true, PreviewStart: 1, Crop: engine.EditCrop{Width: 160, Height: 120}}))
	j = f.waitJob(t, j.ID, "completed")
	response := httptest.NewRecorder()
	f.service.Assets().ServeHTTP(response, httptest.NewRequest("GET", "/edit-assets/jobs/"+j.ID, nil))
	g, err := gif.DecodeAll(bytes.NewReader(response.Body.Bytes()))
	if err != nil || len(g.Image) < 1 || len(g.Image) > 60 {
		t.Fatalf("whole-source quality preview: %v", err)
	}
	var ticks int
	for _, delay := range g.Delay {
		ticks += delay
	}
	if ticks > 200 {
		t.Fatal("quality preview exceeded two seconds", ticks)
	}
}

func TestWholeHighRateVideoUsesWebPWithoutDroppingFrames(t *testing.T) {
	f := newFixture(t)
	source := filepath.Join(f.root, "high-rate.mp4")
	_, err := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: f.ffmpeg, Directory: f.root, Timeout: time.Minute, Arguments: []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=120", "-frames:v", "30", "-c:v", "libx264", source}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec("UPDATE media_file SET filepath=? WHERE id=?", source, f.fileID); err != nil {
		t.Fatal(err)
	}
	v := prepareWholeVideo(t, f, "gif")
	if v.FrameCount != 30 {
		t.Fatal("high rate source frames dropped", v.FrameCount)
	}
	req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", RangeStart: 3, RangeEnd: 18, Crop: engine.EditCrop{Width: 160, Height: 120}}
	if result := f.service.Create(t.Context(), req); result.OK || result.Error.Code != "INVALID_ARGUMENT" || !strings.Contains(result.Error.Message, "WebP") {
		t.Fatal("GIF timing limit was hidden", result)
	}
	req.Kind = "webp"
	j := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
	j = f.waitJob(t, j.ID, "completed")
	output := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: j.ID}))
	meta, count, err := (engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}).Verify(t.Context(), output)
	if err != nil || count != 15 || math.Abs(*meta.Duration-.125) > .001 {
		t.Fatalf("high rate WebP: %+v %d %v", meta, count, err)
	}
}

func TestWholeVideoCanBeTrimmedAfterPreparation(t *testing.T) {
	f := newFixture(t)
	v := prepareWholeVideo(t, f, "trim")
	if v.Options.Start != 0 || math.Abs(v.Options.End-1) > .05 {
		t.Fatal("whole trim duration", v.Options)
	}
	start, end := .2, .8
	j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "trim", TrimStart: &start, TrimEnd: &end}))
	j = f.waitJob(t, j.ID, "completed")
	output := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: j.ID}))
	meta, _, err := (engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}).Verify(t.Context(), output)
	if err != nil || math.Abs(*meta.Duration-.6) > .05 {
		t.Fatalf("whole source trim: %+v %v", meta, err)
	}
}

func TestWholeVariableRateVideoKeepsNativeTiming(t *testing.T) {
	f := newFixture(t)
	source := filepath.Join(f.root, "variable-source.mp4")
	_, err := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: f.ffmpeg, Directory: f.root, Timeout: time.Minute, Arguments: []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10:duration=1", "-vf", "setpts=(N+floor(N/3))/(10*TB)", "-fps_mode", "vfr", "-c:v", "libx264", source}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec("UPDATE media_file SET filepath=? WHERE id=?", source, f.fileID); err != nil {
		t.Fatal(err)
	}
	v := prepareWholeVideo(t, f, "gif")
	p := success[mediaedit.FramePage](t, f.service.FrameStatus(t.Context(), mediaedit.FramesRequest{ID: v.ID}))
	if v.FrameCount != 10 || len(p.Durations) != 10 || p.Durations[0] != .1 || p.Durations[2] != .2 || math.Abs(v.Options.End-1.4) > .001 {
		t.Fatalf("variable timing: %+v %+v", v, p)
	}
	for _, format := range []string{"gif", "webp"} {
		j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: format, Crop: engine.EditCrop{Width: 160, Height: 120}}))
		j = f.waitJob(t, j.ID, "completed")
		output := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: j.ID}))
		meta, count, err := (engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}).Verify(t.Context(), output)
		if err != nil || count != 10 || math.Abs(*meta.Duration-1.4) > .001 {
			t.Fatalf("%s variable timing %+v %d %v", format, meta, count, err)
		}
	}
}

func TestMaximumAnimationQuality(t *testing.T) {
	for _, format := range []string{"gif", "webp"} {
		for _, input := range []int{0, 65, 200} {
			q, err := engine.NormalizeAnimationQuality(format, engine.AnimationQuality{Preset: "max", Quality: input})
			if err != nil || q.Preset != "max" || q.Quality != 100 || q.Effort != "normal" || q.Lossless {
				t.Fatalf("maximum %s input=%d: %+v %v", format, input, q, err)
			}
		}
	}
	q, err := engine.NormalizeAnimationQuality("webp", engine.AnimationQuality{Preset: "max", Lossless: true, Effort: "precise"})
	if err != nil || !q.Lossless || q.Quality != 100 || q.Effort != "precise" {
		t.Fatalf("independent WebP lossless and effort: %+v %v", q, err)
	}
	if _, err = engine.NormalizeAnimationQuality("gif", engine.AnimationQuality{Preset: "max", Lossless: true}); err == nil {
		t.Fatal("maximum GIF must still reject WebP-only lossless mode")
	}
}

func TestAnimationAlgorithmValidation(t *testing.T) {
	legacy := engine.AnimationQuality{Preset: "balanced", Quality: 80, Effort: "normal"}
	for _, format := range []string{"gif", "webp"} {
		q, err := engine.NormalizeAnimationQuality(format, engine.AnimationQuality{Dither: "auto", WebPPreset: "none"})
		if err != nil || q != legacy {
			t.Fatalf("legacy defaults changed: %+v %v", q, err)
		}
	}
	for _, dither := range []string{"sierra2_4a", "sierra3", "floyd_steinberg", "bayer", "atkinson", "none"} {
		q, err := engine.NormalizeAnimationQuality("gif", engine.AnimationQuality{Preset: "max", Dither: dither, Effort: "fast"})
		if err != nil || q.Dither != dither || q.Quality != 100 {
			t.Fatal(q, err)
		}
	}
	for _, preset := range []string{"photo", "drawing", "icon", "text"} {
		q, err := engine.NormalizeAnimationQuality("webp", engine.AnimationQuality{WebPPreset: preset, Effort: "fast"})
		if err != nil || q.WebPPreset != preset || q.Effort != "normal" {
			t.Fatal(q, err)
		}
	}
	for _, invalid := range []struct {
		format string
		q      engine.AnimationQuality
	}{
		{"gif", engine.AnimationQuality{Dither: "invalid"}},
		{"gif", engine.AnimationQuality{Dither: "bayer:bayer_scale=0"}},
		{"webp", engine.AnimationQuality{WebPPreset: "invalid"}},
		{"gif", engine.AnimationQuality{WebPPreset: "photo"}},
		{"webp", engine.AnimationQuality{Dither: "none"}},
		{"webp", engine.AnimationQuality{Lossless: true, WebPPreset: "photo"}},
	} {
		if _, err := engine.NormalizeAnimationQuality(invalid.format, invalid.q); err == nil {
			t.Fatalf("accepted invalid advanced settings: %+v", invalid)
		}
	}
}

func TestGIFDitheringAlgorithmsApplyToNativeFrames(t *testing.T) {
	f := newFixture(t)
	v := prepareWholeVideo(t, f, "gif")
	outputs := map[[32]byte]bool{}
	for _, dither := range []string{"sierra2_4a", "sierra3", "floyd_steinberg", "bayer", "atkinson", "none"} {
		t.Run(dither, func(t *testing.T) {
			job := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", RangeStart: 1, RangeEnd: 7, Deleted: []int{3}, Crop: engine.EditCrop{Width: 160, Height: 120}, Quality: engine.AnimationQuality{Preset: "custom", Quality: 45, Effort: "fast", Dither: dither}}))
			job = f.waitJob(t, job.ID, "completed")
			if job.Options.Quality.Dither != dither {
				t.Fatal("dither selection lost", job.Options.Quality)
			}
			output := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: job.ID}))
			data := readFile(t, output)
			g, err := gif.DecodeAll(bytes.NewReader(data))
			if err != nil || len(g.Image) != 5 || g.Config.Width != 160 || g.Config.Height != 120 {
				t.Fatalf("dither changed edit: %+v %v", g, err)
			}
			outputs[sha256.Sum256(data)] = true
		})
	}
	if len(outputs) < 3 {
		t.Fatal("dithering choices did not affect output", len(outputs))
	}
}

func TestWebPOptimizationAndLosslessWithBothFramePipelines(t *testing.T) {
	for _, native := range []bool{false, true} {
		name := "sampled"
		if native {
			name = "native"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			var v mediaedit.Session
			if native {
				v = prepareWholeVideo(t, f, "gif")
			} else {
				v = f.prepare(t, "gif")
			}
			cases := []engine.AnimationQuality{{Preset: "custom", Quality: 80, Lossless: true}}
			for _, preset := range []string{"photo", "drawing", "icon", "text"} {
				cases = append(cases, engine.AnimationQuality{Preset: "custom", Quality: 80, WebPPreset: preset})
			}
			for _, q := range cases {
				job := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "webp", RangeStart: 1, RangeEnd: 7, Deleted: []int{3}, Crop: engine.EditCrop{Width: 160, Height: 120}, Quality: q}))
				job = f.waitJob(t, job.ID, "completed")
				output := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: job.ID}))
				data := readFile(t, output)
				meta, count, err := (engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}).VerifyWebP(t.Context(), output)
				if err != nil || count != 5 || *meta.Width != 160 || *meta.Height != 120 || math.Abs(*meta.Duration-.5) > .001 {
					t.Fatalf("WebP preset changed edit: %+v frames=%d err=%v", meta, count, err)
				}
				if q.Lossless && !bytes.Contains(data, []byte("VP8L")) || !q.Lossless && !bytes.Contains(data, []byte("VP8 ")) {
					t.Fatalf("wrong WebP compression mode: %+v", q)
				}
				if job.Options.Quality.WebPPreset != q.WebPPreset {
					t.Fatal("WebP optimization selection lost")
				}
			}
		})
	}
}

func TestMaximumPresetPreviewAndSave(t *testing.T) {
	f := newFixture(t)
	v := f.prepare(t, "gif")
	for _, format := range []string{"gif", "webp"} {
		for _, mode := range []struct {
			name    string
			preview bool
		}{{"save", false}, {"preview", true}} {
			t.Run(format+"/"+mode.name, func(t *testing.T) {
				request := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: format, Crop: engine.EditCrop{Width: 160, Height: 120}, Preview: mode.preview, Quality: engine.AnimationQuality{Preset: "max", Quality: 5}}
				job := success[mediaedit.Job](t, f.service.Create(t.Context(), request))
				job = f.waitJob(t, job.ID, "completed")
				if job.Options.Quality.Preset != "max" || job.Options.Quality.Quality != 100 || job.Options.Quality.Lossless {
					t.Fatalf("maximum preset was not preserved: %+v", job.Options.Quality)
				}
				response := httptest.NewRecorder()
				f.service.Assets().ServeHTTP(response, httptest.NewRequest("GET", "/edit-assets/jobs/"+job.ID, nil))
				if response.Code != 200 || response.Body.Len() == 0 || response.Header().Get("Content-Type") != "image/"+format {
					t.Fatalf("missing maximum quality output: status=%d type=%s bytes=%d", response.Code, response.Header().Get("Content-Type"), response.Body.Len())
				}
			})
		}
	}
}

func TestAnimatedWebPQualityAndPreview(t *testing.T) {
	f := newFixture(t)
	v := f.prepare(t, "webp")
	for _, lossless := range []bool{false, true} {
		req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "webp", Crop: engine.EditCrop{X: 1, Y: 1, Width: 157, Height: 117}, Deleted: []int{1, 4}, Quality: engine.AnimationQuality{Preset: "custom", Quality: 86, Effort: "fast", Lossless: lossless}}
		j := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
		j = f.waitJob(t, j.ID, "completed")
		path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: j.ID}))
		thumbnail := filepath.Join(f.root, "webp-thumb.jpg")
		if e := (engine.VideoEditor{FFmpeg: f.ffmpeg}).WebPThumbnail(t.Context(), path, thumbnail); e != nil {
			t.Fatal(e)
		}
		thumb, e := jpeg.Decode(bytes.NewReader(readFile(t, thumbnail)))
		if e != nil || thumb.Bounds().Dx() > 480 || thumb.Bounds().Dy() > 480 {
			t.Fatal("WebP thumbnail", e)
		}
		meta, count, e := (engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}).VerifyWebP(t.Context(), path)
		if e != nil || *meta.Width != 157 || *meta.Height != 117 || count < 1 || *meta.Duration < .79 || *meta.Duration > .81 {
			t.Fatalf("WebP metadata %+v %d %v", meta, count, e)
		}
		data := readFile(t, path)
		bad := filepath.Join(f.root, "broken.webp")
		os.WriteFile(bad, data[:len(data)-3], 0600)
		if _, _, e = (engine.VideoEditor{FFmpeg: f.ffmpeg}).VerifyWebP(t.Context(), bad); e == nil {
			t.Fatal("truncated WebP accepted")
		}
	}
	var before int
	f.db.QueryRow("SELECT count(*) FROM media_file").Scan(&before)
	req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{Width: 160, Height: 120}, Preview: true, PreviewStart: 5, Deleted: []int{6, 9}}
	j := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
	j = f.waitJob(t, j.ID, "completed")
	if !j.Preview || j.ResultFileID != nil {
		t.Fatal("sample published", j)
	}
	response := httptest.NewRecorder()
	f.service.Assets().ServeHTTP(response, httptest.NewRequest("GET", "/edit-assets/jobs/"+j.ID, nil))
	g, e := gif.DecodeAll(bytes.NewReader(response.Body.Bytes()))
	if e != nil || len(g.Image) != 3 {
		t.Fatalf("sample frames %v %v", g, e)
	}
	var after int
	f.db.QueryRow("SELECT count(*) FROM media_file").Scan(&after)
	if before != after {
		t.Fatal("preview registered")
	}
	// Finish writes measurements after completion; wait for its persisted snapshot.
	for until := time.Now().Add(time.Second); time.Now().Before(until); time.Sleep(10 * time.Millisecond) {
		j = success[mediaedit.Job](t, f.service.GetJob(t.Context(), mediaedit.Request{ID: j.ID}))
		if j.Resources.OutputFrames > 0 {
			break
		}
	}
	r := success[mediaedit.ResourceEstimate](t, f.service.Resources(t.Context(), req))
	if !r.MemoryKnown || r.TotalRAM < r.AvailableRAM || !r.EstimateKnown || r.LowRAM > r.HighRAM || !r.DiskKnown || !r.DiskEstimateKnown || r.Basis != "sampled" {
		t.Fatalf("resource snapshot %+v job %+v", r, j.Resources)
	}
}

func TestPreviewTwoSecondsAfterDeletion(t *testing.T) {
	f := newFixture(t)
	os.Remove(f.source)
	_, e := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: f.ffmpeg, Directory: f.root, Timeout: time.Minute, Arguments: []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=s=160x120:r=10:d=5", "-c:v", "libx264", "-preset", "ultrafast", f.source}})
	if e != nil {
		t.Fatal(e)
	}
	req := mediaedit.PrepareRequest{RequestID: uuid.NewString(), FileID: f.fileID, Kind: "gif", End: 5, FPS: 10}
	v := success[mediaedit.Session](t, f.service.Prepare(t.Context(), req))
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
		v = success[mediaedit.Session](t, f.service.GetSession(t.Context(), mediaedit.Request{ID: v.ID}))
		if v.State == "ready" {
			break
		}
		if v.State == "failed" {
			t.Fatal(v.Error)
		}
	}
	selection := uuid.NewString()
	success[string](t, f.service.Selection(t.Context(), mediaedit.SelectionRequest{ID: selection, SessionID: v.ID, Ranges: []engine.FrameRange{{First: 1, Last: 10}}, Final: true}))
	j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{Width: 160, Height: 120}, SelectionID: selection, Preview: true}))
	j = f.waitJob(t, j.ID, "completed")
	response := httptest.NewRecorder()
	f.service.Assets().ServeHTTP(response, httptest.NewRequest("GET", "/edit-assets/jobs/"+j.ID, nil))
	g, e := gif.DecodeAll(bytes.NewReader(response.Body.Bytes()))
	if e != nil || len(g.Image) != 20 {
		t.Fatalf("two second sample %+v %v", g, e)
	}
	total := 0
	for _, delay := range g.Delay {
		total += delay
	}
	if total != 200 {
		t.Fatal("sample duration", total)
	}
}

func TestOriginalPixelsAndLazyLongTimeline(t *testing.T) {
	f := newFixture(t)
	// This actual video crosses all previous limits without staging full PNGs.
	os.Remove(f.source)
	_, e := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: f.ffmpeg, Directory: f.root, Timeout: time.Minute, Arguments: []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=1920x1080:r=30:d=31", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", f.source}})
	if e != nil {
		t.Fatal(e)
	}
	f.db.Exec("UPDATE media_file SET duration=31 WHERE id=?", f.fileID)
	v := success[mediaedit.Session](t, f.service.Prepare(t.Context(), mediaedit.PrepareRequest{RequestID: uuid.NewString(), FileID: f.fileID, End: 31, FPS: 30, Kind: "gif"}))
	for until := time.Now().Add(20 * time.Second); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
		v = success[mediaedit.Session](t, f.service.GetSession(t.Context(), mediaedit.Request{ID: v.ID}))
		if v.State == "ready" {
			break
		}
		if v.State == "failed" {
			t.Fatal(v.Error)
		}
	}
	if v.State != "ready" || v.FrameCount != 930 || v.Width != 1920 || v.Height != 1080 {
		t.Fatal(v)
	}
	page := success[mediaedit.FramePage](t, f.service.FrameStatus(t.Context(), mediaedit.FramesRequest{ID: v.ID, Offset: 900}))
	if page.State != "missing" {
		t.Fatal("eager page", page)
	}
	success[mediaedit.FramePage](t, f.service.Frames(t.Context(), mediaedit.FramesRequest{ID: v.ID, Offset: 900}))
	for until := time.Now().Add(15 * time.Second); time.Now().Before(until); time.Sleep(25 * time.Millisecond) {
		page = success[mediaedit.FramePage](t, f.service.FrameStatus(t.Context(), mediaedit.FramesRequest{ID: v.ID, Offset: 900}))
		if page.State == "ready" {
			break
		}
		if page.State == "failed" {
			t.Fatal(page)
		}
	}
	if page.State != "ready" {
		t.Fatal(page)
	}
	selection := uuid.NewString()
	success[string](t, f.service.Selection(t.Context(), mediaedit.SelectionRequest{ID: selection, SessionID: v.ID, Ranges: []engine.FrameRange{{First: 0, Last: 928}}, Final: true}))
	req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{X: 20, Width: 1900, Height: 1080}, SelectionID: selection, Preview: true, PreviewStart: 929}
	j := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
	j = f.waitJob(t, j.ID, "completed")
	response := httptest.NewRecorder()
	f.service.Assets().ServeHTTP(response, httptest.NewRequest("GET", "/edit-assets/jobs/"+j.ID, nil))
	g, e := gif.DecodeAll(bytes.NewReader(response.Body.Bytes()))
	if e != nil || g.Config.Width != 1900 || g.Config.Height != 1080 || len(g.Image) != 1 {
		t.Fatalf("original crop %+v %v", g, e)
	}
	if _, e = os.Stat(filepath.Join(f.cache, v.ID, "0000.png")); !os.IsNotExist(e) {
		t.Fatal("full size frame cache")
	}
	// Cancel an actual decoder/encoder pair, after memory observation proves it started.
	full := req
	full.RequestID = uuid.NewString()
	full.Preview = false
	full.SelectionID = ""
	full.PreviewStart = 0
	running := success[mediaedit.Job](t, f.service.Create(t.Context(), full))
	started := false
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
		running = success[mediaedit.Job](t, f.service.GetJob(t.Context(), mediaedit.Request{ID: running.ID}))
		if running.Resources.Known && running.Resources.CurrentBytes > 0 {
			started = true
			break
		}
	}
	if !started {
		t.Fatal("encoder did not start", running)
	}
	if result := f.service.Cancel(t.Context(), mediaedit.Request{ID: running.ID}); !result.OK {
		t.Fatal(result.Error)
	}
	f.waitJob(t, running.ID, "cancelled")
}

func TestDeletionChunksReplayAndFinalization(t *testing.T) {
	f := newFixture(t)
	v := f.prepare(t, "gif")
	// A metadata-only timeline lets us validate many edits without producing media.
	f.db.Exec("UPDATE media_edit_session SET frame_count=10000 WHERE id=?", v.ID)
	id := uuid.NewString()
	var first mediaedit.SelectionRequest
	for offset := 0; offset < 3000; offset += 256 {
		last := min(offset+256, 3000)
		ranges := []engine.FrameRange{}
		for n := offset; n < last; n++ {
			ranges = append(ranges, engine.FrameRange{First: n * 2, Last: n * 2})
		}
		req := mediaedit.SelectionRequest{ID: id, SessionID: v.ID, Offset: offset, Ranges: ranges, Final: last == 3000}
		success[string](t, f.service.Selection(t.Context(), req))
		if offset == 0 {
			first = req
		}
	}
	success[string](t, f.service.Selection(t.Context(), first))
	first.Ranges[0].Last = 1
	if f.service.Selection(t.Context(), first).OK {
		t.Fatal("different replay allowed")
	}
	if f.service.Selection(t.Context(), mediaedit.SelectionRequest{ID: id, SessionID: v.ID, Offset: 3000, Ranges: []engine.FrameRange{{First: 7000, Last: 7001}}}).OK {
		t.Fatal("final selection changed")
	}
}

func TestLegacyCachedSessionWithoutOriginal(t *testing.T) {
	f := newFixture(t)
	v := f.prepare(t, "gif")
	if e := (engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}).Prepare(t.Context(), f.source, filepath.Join(f.cache, v.ID), 0, 1, 10, 160, nil); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec("UPDATE media_edit_session SET render_version=1,source_file_id=NULL,request_json=json_set(request_json,'$.width',160) WHERE id=?", v.ID); e != nil {
		t.Fatal(e)
	}
	if e := os.Remove(f.source); e != nil {
		t.Fatal(e)
	}
	oldPrepare := v.Options
	oldPrepare.Width = 160
	success[mediaedit.Session](t, f.service.Prepare(t.Context(), oldPrepare))

	j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{Width: 160, Height: 120}, Deleted: []int{2, 4}}))
	j = f.waitJob(t, j.ID, "completed")
	if _, e := f.db.Exec("UPDATE media_edit_job SET request_json=json_remove(request_json,'$.quality') WHERE id=?", j.ID); e != nil {
		t.Fatal(e)
	}
	oldRequest := j.Options
	oldRequest.Quality = engine.AnimationQuality{}
	duplicate := success[mediaedit.Job](t, f.service.Create(t.Context(), oldRequest))
	if duplicate.ID != j.ID {
		t.Fatal("legacy idempotency")
	}

	path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: j.ID}))
	g, e := gif.DecodeAll(bytes.NewReader(readFile(t, path)))
	if e != nil || len(g.Image) != 8 {
		t.Fatalf("legacy cached output %+v %v", g, e)
	}
}

func TestOddVideoTrimPreservesPixels(t *testing.T) {
	f := newFixture(t)
	os.Remove(f.source)
	_, e := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: f.ffmpeg, Directory: f.root, Timeout: time.Minute, Arguments: []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc=s=161x121:r=10:d=1", "-c:v", "libx264", "-pix_fmt", "yuv444p", f.source}})
	if e != nil {
		t.Fatal(e)
	}
	v := f.prepare(t, "trim")
	j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "trim", Mute: true}))
	j = f.waitJob(t, j.ID, "completed")
	path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: j.ID}))
	metadata, _, e := (engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}).Verify(t.Context(), path)
	if e != nil || *metadata.Width != 161 || *metadata.Height != 121 {
		t.Fatal("trim resized odd input", metadata, e)
	}
}

func TestSingleRemainingWebPFrame(t *testing.T) {
	f := newFixture(t)
	v := f.prepare(t, "gif")
	j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "webp", Crop: engine.EditCrop{Width: 160, Height: 120}, Deleted: []int{0, 1, 2, 3, 4, 5, 6, 7, 8}, Quality: engine.AnimationQuality{Preset: "balanced", Lossless: true}}))
	j = f.waitJob(t, j.ID, "completed")
	var duration float64
	var frames int
	if e := f.db.QueryRow("SELECT duration,frame_count FROM media_file WHERE id=?", *j.ResultFileID).Scan(&duration, &frames); e != nil || frames != 1 || duration < .099 || duration > .101 {
		t.Fatal("single WebP metadata", duration, frames, e)
	}
}
