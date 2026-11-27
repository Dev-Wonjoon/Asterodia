package mediaedit_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/mediaedit"
)

func TestOverviewDoesNotIndexFramesAndReusesUnchangedSource(t *testing.T) {
	f := newFixture(t)
	quickSource(t, f, "30", false)
	started := time.Now()
	first := success[mediaedit.OverviewFrame](t, f.service.Overview(t.Context(), mediaedit.OverviewRequest{FileID: f.fileID, Index: 6}))
	cold := time.Since(started)
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(first.Image, "data:image/jpeg;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Dx() > 240 || img.Bounds().Dy() > 135 || first.FrameCount != 129 || math.Abs(first.Time-first.Duration/2) > .000001 {
		t.Fatalf("overview=%+v decode=%v", first, err)
	}
	for _, table := range []string{"media_edit_session", "media_edit_page"} {
		var count int
		if err = f.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("overview indexed frames: %s=%d (%v)", table, count, err)
		}
	}
	started = time.Now()
	cached := success[mediaedit.OverviewFrame](t, f.service.Overview(t.Context(), mediaedit.OverviewRequest{FileID: f.fileID, Index: 6}))
	warm := time.Since(started)
	if cached.Image != first.Image || cached.Time != first.Time {
		t.Fatal("unchanged overview was not reused")
	}
	t.Logf("overview cold=%s cached=%s, no timeline or frame pages", cold, warm)
	quickSource(t, f, "120", false)
	changed := success[mediaedit.OverviewFrame](t, f.service.Overview(t.Context(), mediaedit.OverviewRequest{FileID: f.fileID, Index: 6}))
	if changed.FrameCount != 516 {
		t.Fatalf("source replacement reused old metadata: %d", changed.FrameCount)
	}
	for _, req := range []mediaedit.OverviewRequest{{FileID: 0}, {FileID: f.fileID, Index: -1}, {FileID: f.fileID, Index: 12}} {
		if f.service.Overview(t.Context(), req).OK {
			t.Fatal("invalid overview request accepted")
		}
	}
}

func TestThousandFrameFastTimelineAndLateSeekMatchDecodedFrames(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.root, "thousand.mp4")
	editor := engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}
	_, err := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: f.ffmpeg, Directory: f.root, Timeout: time.Minute,
		Arguments: []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=30000/1001", "-frames:v", "1000", "-c:v", "libx264", "-preset", "veryfast", "-g", "120", "-bf", "3", "-pix_fmt", "yuv420p", path}})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := editor.ProbeVideo(t.Context(), path)
	if err != nil || metadata.FrameCount == nil || *metadata.FrameCount != 1000 {
		t.Fatalf("frame metadata: %+v %v", metadata, err)
	}
	started := time.Now()
	fast, err := editor.ReadVideoTimeline(t.Context(), path, metadata)
	if err != nil {
		t.Fatal(err)
	}
	fastTime := time.Since(started)
	slowMetadata := metadata
	slowMetadata.FrameCount = nil
	started = time.Now()
	decoded, err := editor.ReadVideoTimeline(t.Context(), path, slowMetadata)
	if err != nil {
		t.Fatal(err)
	}
	slowTime := time.Since(started)
	if len(fast.Delays) != 1000 || len(decoded.Delays) != 1000 {
		t.Fatal("lost source frames")
	}
	for n := range fast.Delays {
		if fast.Delays[n] != decoded.Delays[n] || math.Abs(fast.FrameTimes[n]-decoded.FrameTimes[n]) > .000001 {
			t.Fatalf("source timing changed at frame %d: %d/%d", n, fast.Delays[n], decoded.Delays[n])
		}
	}
	legacy, direct := filepath.Join(f.root, "legacy-page"), filepath.Join(f.root, "direct-page")
	for _, dir := range []string{legacy, direct} {
		if err = os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	started = time.Now()
	if err = editor.VideoClipFramePage(t.Context(), path, legacy, 1000, 960, 48, 0, *metadata.Duration); err != nil {
		t.Fatal(err)
	}
	legacyTime := time.Since(started)
	started = time.Now()
	if err = editor.VideoClipFramePage(t.Context(), path, direct, 1000, 960, 48, 0, *metadata.Duration, fast.FrameTimes[960]); err != nil {
		t.Fatal(err)
	}
	seekTime := time.Since(started)
	for n := 960; n < 1000; n++ {
		name := fmt.Sprintf("%012d.jpg", n)
		want, e := os.ReadFile(filepath.Join(legacy, name))
		if e != nil {
			t.Fatal(e)
		}
		got, e := os.ReadFile(filepath.Join(direct, name))
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("direct seek changed displayed frame %d", n)
		}
	}
	t.Logf("1000 frames: timeline fast=%s decoded=%s; last page direct=%s from-start=%s", fastTime, slowTime, seekTime, legacyTime)
}

func TestDirectFramePagesPreserveOffsetAndVariableTiming(t *testing.T) {
	for _, variable := range []bool{false, true} {
		t.Run(fmt.Sprint(variable), func(t *testing.T) {
			f := newFixture(t)
			path := filepath.Join(f.root, "timed.mp4")
			args := []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=30:duration=8"}
			if variable {
				args = append(args, "-vf", "setpts=(N+floor(N/3))/(30*TB)", "-fps_mode", "vfr")
			}
			args = append(args, "-c:v", "libx264", "-g", "90", "-bf", "3", "-output_ts_offset", "5", path)
			if _, err := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: f.ffmpeg, Directory: f.root, Timeout: time.Minute, Arguments: args}); err != nil {
				t.Fatal(err)
			}
			editor := engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}
			metadata, err := editor.ProbeVideo(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			fast, err := editor.ReadVideoClipTimeline(t.Context(), path, metadata, 1.25, 6.25)
			if err != nil {
				t.Fatal(err)
			}
			metadata.FrameCount = nil
			decoded, err := editor.ReadVideoClipTimeline(t.Context(), path, metadata, 1.25, 6.25)
			if err != nil {
				t.Fatal(err)
			}
			if len(fast.Delays) != len(decoded.Delays) || math.Abs(fast.StartTime-decoded.StartTime) > .000001 {
				t.Fatal("interval frame index changed")
			}
			for n := range fast.Delays {
				if fast.Delays[n] != decoded.Delays[n] || math.Abs(fast.FrameTimes[n]-decoded.FrameTimes[n]) > .000001 {
					t.Fatalf("frame timing changed at %d", n)
				}
			}
			for _, seek := range []bool{false, true} {
				dir := filepath.Join(f.root, fmt.Sprint(seek))
				if err = os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
				var exact []float64
				if seek {
					exact = []float64{fast.StartTime + fast.FrameTimes[48]}
				}
				if err = editor.VideoClipFramePage(t.Context(), path, dir, len(fast.Delays), 48, 48, 1.25, 6.25, exact...); err != nil {
					t.Fatal(err)
				}
			}
			for n := 48; n < min(96, len(fast.Delays)); n++ {
				name := fmt.Sprintf("%012d.jpg", n)
				want, err := os.ReadFile(filepath.Join(f.root, "false", name))
				if err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(filepath.Join(f.root, "true", name))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(want, got) {
					t.Fatalf("wrong displayed frame %d (variable=%v)", n, variable)
				}
			}
		})
	}
}

func TestShortAV1IntervalUsesExactPacketTimingFromLongerSource(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.root, "longer-av1.mp4")
	_, err := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: f.ffmpeg, Directory: f.root, Timeout: time.Minute,
		Arguments: []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=30:duration=40", "-c:v", "libaom-av1", "-cpu-used", "8", "-threads", "2", "-crf", "45", "-g", "90", path}})
	if err != nil {
		t.Fatal(err)
	}
	editor := engine.VideoEditor{FFmpeg: f.ffmpeg, FFprobe: f.probe}
	metadata, err := editor.ProbeVideo(t.Context(), path)
	if err != nil || metadata.FrameCount == nil || *metadata.FrameCount != 1200 {
		t.Fatalf("AV1 metadata: %+v %v", metadata, err)
	}
	fast, err := editor.ReadVideoClipTimeline(t.Context(), path, metadata, 12.1, 15.1)
	if err != nil {
		t.Fatal(err)
	}
	metadata.FrameCount = nil
	decoded, err := editor.ReadVideoClipTimeline(t.Context(), path, metadata, 12.1, 15.1)
	if err != nil {
		t.Fatal(err)
	}
	if len(fast.Delays) != 90 || len(decoded.Delays) != 90 || math.Abs(fast.StartTime-decoded.StartTime) > .000001 {
		t.Fatal("AV1 interval boundaries changed")
	}
	for n := range fast.Delays {
		if fast.Delays[n] != decoded.Delays[n] || math.Abs(fast.FrameTimes[n]-decoded.FrameTimes[n]) > .000001 {
			t.Fatalf("AV1 timing mismatch %d", n)
		}
	}
}
