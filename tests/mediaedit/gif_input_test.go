package mediaedit_test

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/mediaedit"
	"github.com/google/uuid"
)

func gifFixture(t *testing.T) *fixture { return gifFixtureFrames(t, 1) }
func gifFixtureFrames(t *testing.T, repeat int) *fixture {
	f := newFixture(t)
	f.source = filepath.Join(f.root, "source.gif")
	animation := &gif.GIF{Config: image.Config{Width: 160, Height: 120}, Delay: []int{3, 11, 7, 25, 9, 12}, LoopCount: 0}
	animation.Delay = nil
	for n := 0; n < 6*repeat; n++ {
		animation.Delay = append(animation.Delay, []int{3, 11, 7, 25, 9, 12}[n%6])
		palette := color.Palette{color.RGBA{0, 0, 0, 0}, color.RGBA{uint8(30 + n*35), uint8(190 - n*20), 90, 255}}
		frame := image.NewPaletted(image.Rect(0, 0, 160, 120), palette)
		for y := 12; y < 108; y++ {
			for x := 12; x < 148; x++ {
				frame.SetColorIndex(x, y, 1)
			}
		}
		animation.Image = append(animation.Image, frame)
		animation.Disposal = append(animation.Disposal, gif.DisposalBackground)
	}
	file, e := os.Create(f.source)
	if e != nil {
		t.Fatal(e)
	}
	e = gif.EncodeAll(file, animation)
	file.Close()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec("UPDATE media_file SET kind='image',detected_format='gif',filepath=?,duration=? WHERE id=?", f.source, float64(repeat)*.67, f.fileID); e != nil {
		t.Fatal(e)
	}
	return f
}
func TestGIFInputPreservesFramesAndTiming(t *testing.T) {
	f := gifFixture(t)
	original := readFile(t, f.source)
	v := f.prepare(t, "gif")
	if !v.Options.NativeGIF || v.FrameCount != 6 || v.Options.End < .669 || v.Options.End > .671 {
		t.Fatal(v)
	}
	page := success[mediaedit.FramePage](t, f.service.FrameStatus(t.Context(), mediaedit.FramesRequest{ID: v.ID, Offset: 0}))
	if len(page.Times) != 6 || page.Times[3] < .209 || page.Times[3] > .211 {
		t.Fatal("native timeline", page)
	}
	for _, format := range []string{"gif", "webp", "webp-lossy"} {
		lossless := format == "webp"
		if format == "webp-lossy" {
			format = "webp"
		}
		req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: format, Crop: engine.EditCrop{X: 0, Y: 0, Width: 96, Height: 76}, RangeStart: 1, RangeEnd: 5, Deleted: []int{3}, Quality: engine.AnimationQuality{Preset: "high", Lossless: lossless}}
		job := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
		job = f.waitJob(t, job.ID, "completed")
		path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: job.ID}))
		if format == "gif" {
			out, e := gif.DecodeAll(bytes.NewReader(readFile(t, path)))
			if e != nil {
				t.Fatal(e)
			}
			if len(out.Image) != 3 || out.Delay[0] != 11 || out.Delay[1] != 7 || out.Delay[2] != 9 || out.Config.Width != 96 || out.Config.Height != 76 {
				t.Fatal("native GIF timing", out.Delay, out.Config)
			}
			_, _, _, alpha := out.Image[0].At(0, 0).RGBA()
			if alpha != 0 {
				t.Fatal("GIF transparency lost")
			}
		}
		if format == "webp" {
			meta, _, e := (engine.VideoEditor{FFmpeg: f.ffmpeg}).VerifyWebP(t.Context(), path)
			if e != nil || *meta.Duration < .269 || *meta.Duration > .271 {
				t.Fatal("native WebP timing", meta, e)
			}
			if data := readFile(t, path); data[20]&0x10 == 0 {
				t.Fatalf("WebP alpha lost lossless=%v header=%x", lossless, data[:min(100, len(data))])
			}
		}
	}
	if !bytes.Equal(original, readFile(t, f.source)) {
		t.Fatal("source GIF modified")
	}
}

func TestGIFPartialFramesAreCompositedBeforeCrop(t *testing.T) {
	f := gifFixture(t)
	palette := color.Palette{color.RGBA{0, 0, 0, 0}, color.White, color.RGBA{0, 0, 255, 255}}
	first := image.NewPaletted(image.Rect(0, 0, 160, 120), palette)
	for n := range first.Pix {
		first.Pix[n] = 1
	}
	second := image.NewPaletted(image.Rect(40, 40, 100, 80), palette)
	for n := range second.Pix {
		second.Pix[n] = 2
	}
	animation := &gif.GIF{Config: image.Config{Width: 160, Height: 120}, Image: []*image.Paletted{first, second}, Delay: []int{5, 10}, Disposal: []byte{gif.DisposalNone, gif.DisposalNone}}
	file, e := os.Create(f.source)
	if e != nil {
		t.Fatal(e)
	}
	e = gif.EncodeAll(file, animation)
	file.Close()
	if e != nil {
		t.Fatal(e)
	}
	v := f.prepare(t, "gif")
	req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{X: 30, Y: 30, Width: 80, Height: 60}, Quality: engine.AnimationQuality{Preset: "high"}}
	job := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
	job = f.waitJob(t, job.ID, "completed")
	path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: job.ID}))
	out, e := gif.DecodeAll(bytes.NewReader(readFile(t, path)))
	if e != nil {
		t.Fatal(e)
	}
	if len(out.Image) != 2 || out.Delay[0] != 5 || out.Delay[1] != 10 {
		t.Fatal("composite frame timing", out.Delay)
	}
	composited := image.NewRGBA(image.Rect(0, 0, 80, 60))
	for _, frame := range out.Image {
		draw.Draw(composited, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
	}
	r, g, b, _ := composited.At(0, 0).RGBA()
	if r < 60000 || g < 60000 || b < 60000 {
		t.Fatal("previous full canvas lost")
	}
	r, g, b, _ = composited.At(20, 20).RGBA()
	if r > 1000 || g > 1000 || b < 60000 {
		t.Fatal("partial frame crop offset lost")
	}
}

func TestLongGIFFramePreviewIsLimitedWithoutChangingOriginal(t *testing.T) {
	f := gifFixture(t)
	animation, e := gif.DecodeAll(bytes.NewReader(readFile(t, f.source)))
	if e != nil {
		t.Fatal(e)
	}
	animation.Delay[0] = 300
	file, e := os.Create(f.source)
	if e != nil {
		t.Fatal(e)
	}
	e = gif.EncodeAll(file, animation)
	file.Close()
	if e != nil {
		t.Fatal(e)
	}
	original := readFile(t, f.source)
	v := f.prepare(t, "gif")
	for _, format := range []string{"gif", "webp"} {
		req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: format, Crop: engine.EditCrop{Width: 160, Height: 120}, Preview: true}
		job := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
		job = f.waitJob(t, job.ID, "completed")
		response := httptest.NewRecorder()
		f.service.Assets().ServeHTTP(response, httptest.NewRequest("GET", "/edit-assets/jobs/"+job.ID, nil))
		if response.Code != 200 {
			t.Fatal(response.Code)
		}
		if format == "gif" {
			g, e := gif.DecodeAll(bytes.NewReader(response.Body.Bytes()))
			if e != nil || len(g.Image) != 1 || g.Delay[0] != 200 {
				t.Fatal("long GIF sample", g, e)
			}
		} else {
			path := filepath.Join(f.root, "sample.webp")
			os.WriteFile(path, response.Body.Bytes(), 0600)
			m, _, e := (engine.VideoEditor{FFmpeg: f.ffmpeg}).VerifyWebP(t.Context(), path)
			if e != nil || *m.Duration != 2 {
				t.Fatal("long WebP sample", m, e)
			}
		}
	}
	if !bytes.Equal(original, readFile(t, f.source)) {
		t.Fatal("sample changed source timing")
	}
}

func TestFrameRangeValidationAndExport(t *testing.T) {
	f := newFixture(t)
	v := f.prepare(t, "gif")
	req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{Width: 160, Height: 120}, RangeStart: 2, RangeEnd: 8, Deleted: []int{3}}
	job := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
	job = f.waitJob(t, job.ID, "completed")
	path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: job.ID}))
	out, e := gif.DecodeAll(bytes.NewReader(readFile(t, path)))
	if e != nil || len(out.Image) != 5 {
		t.Fatal("selected frame interval", out, e)
	}
	req.RangeEnd = 7
	if f.service.Create(t.Context(), req).OK {
		t.Fatal("different retry interval accepted")
	}
	req.RequestID = uuid.NewString()
	req.RangeStart = 2
	req.RangeEnd = 4
	req.Deleted = []int{2, 3}
	if f.service.Create(t.Context(), req).OK {
		t.Fatal("empty retained interval accepted")
	}
	req.RequestID = uuid.NewString()
	req.RangeEnd = 11
	req.Deleted = nil
	if f.service.Create(t.Context(), req).OK {
		t.Fatal("out of bounds interval accepted")
	}
}

func TestTrimRangeAfterPreparation(t *testing.T) {
	f := newFixture(t)
	v := f.prepare(t, "trim")
	start, end := .2, .8
	req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "trim", TrimStart: &start, TrimEnd: &end, Mute: true}
	job := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
	job = f.waitJob(t, job.ID, "completed")
	path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: job.ID}))
	meta, _, e := (engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}).Verify(t.Context(), path)
	if e != nil || *meta.Duration < .59 || *meta.Duration > .71 {
		t.Fatal("trim handles", meta, e)
	}
	req.RequestID = uuid.NewString()
	end = 1.5
	if f.service.Create(t.Context(), req).OK {
		t.Fatal("out of bounds time accepted")
	}
}
