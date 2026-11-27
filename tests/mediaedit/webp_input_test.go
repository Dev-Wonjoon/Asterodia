package mediaedit_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/mediaedit"
	"github.com/google/uuid"
)

func TestWebPInputPreservesTimingCropAndTransparency(t *testing.T) {
	f := gifFixture(t)
	v := f.prepare(t, "gif")
	job := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "webp", Crop: engine.EditCrop{Width: 160, Height: 120}, RangeEnd: 6, Quality: engine.AnimationQuality{Preset: "max", Lossless: true}}))
	f.waitJob(t, job.ID, "completed")
	f.source = success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: job.ID}))
	original := readFile(t, f.source)
	if _, err := f.db.Exec("UPDATE media_file SET filepath=?,kind='image',detected_format='webp' WHERE id=?", f.source, f.fileID); err != nil {
		t.Fatal(err)
	}
	v = prepareWholeVideo(t, f, "webp")
	if !v.Options.NativeWebP || v.Options.NativeGIF || v.FrameCount != 6 || math.Abs(v.Options.End-.67) > .001 {
		t.Fatal(v)
	}
	page := success[mediaedit.FramePage](t, f.service.FrameStatus(t.Context(), mediaedit.FramesRequest{ID: v.ID}))
	if len(page.Times) != 6 || math.Abs(page.Times[3]-.21) > .001 {
		t.Fatal(page)
	}
	for _, format := range []string{"webp", "gif"} {
		job = success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: format, Crop: engine.EditCrop{Width: 96, Height: 76}, RangeStart: 1, RangeEnd: 5, Deleted: []int{3}, Quality: engine.AnimationQuality{Preset: "max", Lossless: format == "webp"}}))
		f.waitJob(t, job.ID, "completed")
		path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: job.ID}))
		if format == "webp" {
			timeline, err := engine.ReadWebPTimeline(t.Context(), path)
			if err != nil || len(timeline.Delays) != 3 || timeline.Delays[0] != 110 || timeline.Delays[1] != 70 || timeline.Delays[2] != 90 || timeline.Width != 96 || timeline.Height != 76 {
				t.Fatal(timeline, err)
			}
		} else {
			out, err := gif.DecodeAll(bytes.NewReader(readFile(t, path)))
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Delay) != 3 || out.Delay[0] != 11 || out.Delay[1] != 7 || out.Delay[2] != 9 {
				t.Fatal(out.Delay)
			}
			if _, _, _, a := out.Image[0].At(0, 0).RGBA(); a != 0 {
				t.Fatal("WebP alpha lost in GIF output")
			}
		}
	}
	if !bytes.Equal(original, readFile(t, f.source)) {
		t.Fatal("source WebP modified")
	}
}

func staticWebP(t *testing.T, f *fixture, name string, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	input := filepath.Join(f.root, name+".png")
	out, err := os.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(out, img)
	out.Close()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.root, name+".webp")
	if _, err = new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: f.ffmpeg, Directory: f.root, Timeout: time.Minute, Arguments: []string{"-nostdin", "-v", "error", "-i", input, "-c:v", "libwebp", "-lossless", "1", path}}); err != nil {
		t.Fatal(err)
	}
	return readFile(t, path)
}
func webpChunk(tag string, data []byte) []byte {
	out := make([]byte, 8+len(data)+(len(data)&1))
	copy(out, tag)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(data)))
	copy(out[8:], data)
	return out
}
func webp24(data []byte, n int) { data[0] = byte(n); data[1] = byte(n >> 8); data[2] = byte(n >> 16) }
func webpSubframe(data []byte, x, y, w, h, delay, flags int) []byte {
	header := make([]byte, 16)
	webp24(header, x/2)
	webp24(header[3:], y/2)
	webp24(header[6:], w-1)
	webp24(header[9:], h-1)
	webp24(header[12:], delay)
	header[15] = byte(flags)
	for pos := 12; pos+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		end := pos + 8 + size + (size & 1)
		tag := string(data[pos : pos+4])
		if tag == "VP8 " || tag == "VP8L" || tag == "ALPH" {
			header = append(header, data[pos:end]...)
		}
		pos = end
	}
	return webpChunk("ANMF", header)
}
func TestWebPPartialFramesBlendDisposeAndDecodeLatePages(t *testing.T) {
	f := newFixture(t)
	red := staticWebP(t, f, "red", 160, 120, color.NRGBA{R: 255, A: 255})
	blue := staticWebP(t, f, "blue", 40, 40, color.NRGBA{B: 255, A: 128})
	clear := staticWebP(t, f, "clear", 20, 20, color.NRGBA{})
	canvas := make([]byte, 10)
	canvas[0] = 0x12
	webp24(canvas[4:], 159)
	webp24(canvas[7:], 119)
	data := append(webpChunk("VP8X", canvas), webpChunk("ANIM", make([]byte, 6))...)
	data = append(data, webpSubframe(red, 0, 0, 160, 120, 30, 2)...)
	data = append(data, webpSubframe(blue, 40, 40, 40, 40, 110, 1)...)
	data = append(data, webpSubframe(clear, 0, 0, 20, 20, 70, 2)...)
	header := make([]byte, 12)
	copy(header, "RIFF")
	copy(header[8:], "WEBP")
	binary.LittleEndian.PutUint32(header[4:], uint32(len(data)+4))
	data = append(header, data...)
	f.source = filepath.Join(f.root, "partial.webp")
	if err := os.WriteFile(f.source, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE media_file SET filepath=?,kind='image',detected_format='webp' WHERE id=?", f.source, f.fileID); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.root, "late-page")
	os.Mkdir(dir, 0700)
	if err := (engine.VideoEditor{}).WebPFramePage(t.Context(), f.source, dir, 3, 1, 2); err != nil {
		t.Fatal(err)
	}
	for n, name := range []string{"000000000001.jpg", "000000000002.jpg"} {
		img, err := jpeg.Decode(bytes.NewReader(readFile(t, filepath.Join(dir, name))))
		if err != nil {
			t.Fatal(err)
		}
		r, _, b, _ := img.At(50, 50).RGBA()
		if n == 0 && (r < 28000 || r > 38000 || b < 28000 || b > 38000) {
			t.Fatal("alpha blend incorrect", r, b)
		}
		if n == 1 && (r > 3000 || b > 3000) {
			t.Fatal("dispose-to-background was not applied", r, b)
		}
	}
	v := prepareWholeVideo(t, f, "gif")
	job := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{Width: 100, Height: 100}, RangeEnd: 3, Deleted: []int{1}}))
	f.waitJob(t, job.ID, "completed")
	path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: job.ID}))
	out, err := gif.DecodeAll(bytes.NewReader(readFile(t, path)))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Image) != 2 || out.Delay[0] != 3 || out.Delay[1] != 7 {
		t.Fatal(out.Delay)
	}
	if _, _, _, a := out.Image[1].At(50, 50).RGBA(); a != 0 {
		t.Fatal("deleted frames must still participate in disposal")
	}
	if _, _, _, a := out.Image[1].At(5, 5).RGBA(); a != 0 {
		t.Fatal("replace did not clear earlier pixels")
	}
	if r, _, _, a := out.Image[1].At(90, 90).RGBA(); r < 60000 || a != 65535 {
		t.Fatal("unchanged canvas pixels lost")
	}
}

func TestStaticWebPAndMalformedContainer(t *testing.T) {
	f := newFixture(t)
	data := staticWebP(t, f, "still", 160, 120, color.NRGBA{G: 255, A: 255})
	f.source = filepath.Join(f.root, "still.webp")
	if _, err := f.db.Exec("UPDATE media_file SET filepath=?,kind='image',detected_format='webp' WHERE id=?", f.source, f.fileID); err != nil {
		t.Fatal(err)
	}
	v := prepareWholeVideo(t, f, "webp")
	if v.FrameCount != 1 || !v.Options.NativeWebP {
		t.Fatal(v)
	}
	job := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "webp", Crop: engine.EditCrop{Width: 80, Height: 60}, RangeEnd: 1}))
	f.waitJob(t, job.ID, "completed")
	bad := filepath.Join(f.root, "truncated.webp")
	os.WriteFile(bad, data[:len(data)-1], 0600)
	if _, err := engine.ReadWebPTimeline(t.Context(), bad); err == nil {
		t.Fatal("truncated WebP accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := engine.ReadWebPTimeline(ctx, f.source); err == nil {
		t.Fatal("cancelled timeline read continued")
	}
}
