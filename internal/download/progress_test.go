package download

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

func reportTransfer(t *testing.T, tracker *transferTracker, streams int, format, payload string) {
	t.Helper()
	frame := fmt.Sprintf(`%s{"streams":%d,"formatId":%q,"download":%s}`, transferPrefix, streams, format, payload)
	if !tracker.output(engine.Output{Engine: "yt-dlp", Stream: "stdout", Text: frame}) {
		t.Fatal("progress frame was not consumed")
	}
}

func TestTransferProgressBytesAndUnknownTotals(t *testing.T) {
	tracker := &transferTracker{}
	tracker.beginFile(1, 1)
	reportTransfer(t, tracker, 1, "mp4", `{"status":"downloading","downloaded_bytes":250,"total_bytes":1000,"speed":50,"eta":15}`)
	p := tracker.snapshot()
	if p.Downloaded != 250 || *p.Total != 1000 || *p.Percent != 25 || *p.Speed != 50 || *p.ETA != 15 {
		t.Fatal(p)
	}
	reportTransfer(t, tracker, 1, "mp4", `{"status":"downloading","downloaded_bytes":500,"total_bytes_estimate":2000}`)
	p = tracker.snapshot()
	if !p.Estimated || *p.Total != 2000 || *p.Percent != 25 {
		t.Fatal(p)
	}
	tracker.beginFile(2, 3)
	if tracker.snapshot() != nil {
		t.Fatal("previous file leaked into next file")
	}
	reportTransfer(t, tracker, 1, "mp4", `{"status":"downloading","downloaded_bytes":800,"speed":0}`)
	p = tracker.snapshot()
	if p.Total != nil || p.Percent != nil || p.Downloaded != 800 || p.FileIndex != 2 || p.FileCount != 3 {
		t.Fatal(p)
	}
	tracker.updatedAt = time.Now().Add(-4 * time.Second)
	if tracker.snapshot().Speed != nil {
		t.Fatal("stale speed retained")
	}
}

func TestTransferProgressMergeDoesNotFinishAfterVideo(t *testing.T) {
	tracker := &transferTracker{attempt: "current"}
	tracker.beginFile(1, 1)
	reportTransfer(t, tracker, 2, "video", `{"status":"finished","downloaded_bytes":1000,"total_bytes":1000}`)
	if p := tracker.snapshot(); p.Percent != nil || p.Processing {
		t.Fatal("video completion was mistaken for both streams", p)
	}
	reportTransfer(t, tracker, 2, "audio", `{"status":"downloading","downloaded_bytes":100,"total_bytes":200,"speed":50,"eta":2}`)
	if p := tracker.snapshot(); p.Downloaded != 1100 || *p.Total != 1200 || *p.Percent >= 100 {
		t.Fatal(p)
	}
	reportTransfer(t, tracker, 2, "audio", `{"status":"finished","downloaded_bytes":200,"total_bytes":200}`)
	p := tracker.snapshot()
	if *p.Percent != 100 || !p.Processing || p.Speed != nil || p.ETA != nil {
		t.Fatal(p)
	}
	service := &Service{}
	service.transfers.Store("job", tracker)
	phase, attempt := "downloading", "current"
	job := Job{ID: "job", State: "running", Phase: &phase, CurrentAttemptID: &attempt}
	service.attachTransfer(&job)
	if job.Transfer == nil || job.Progress != 99 {
		t.Fatal("transfer completion must not report committed job", job)
	}
	other := "retry"
	job.Transfer, job.CurrentAttemptID = nil, &other
	service.attachTransfer(&job)
	if job.Transfer != nil {
		t.Fatal("old attempt leaked into retry")
	}
	job.CurrentAttemptID, job.State = &attempt, "cancelled"
	service.attachTransfer(&job)
	if job.Transfer != nil {
		t.Fatal("cancelled job still exposed live progress")
	}
}

func TestTransferProgressRejectsDiagnosticsAndBadFrames(t *testing.T) {
	tracker := &transferTracker{}
	tracker.beginFile(1, 1)
	if tracker.output(engine.Output{Text: "[download] normal diagnostics"}) {
		t.Fatal("swallowed diagnostics")
	}
	for _, raw := range []string{"not-json", `{"streams":0}`, `{"streams":1,"formatId":"v","download":{"status":"downloading","downloaded_bytes":-1}}`} {
		if !tracker.output(engine.Output{Text: transferPrefix + raw}) || tracker.snapshot() != nil {
			t.Fatal("bad private frame was published", raw)
		}
	}
	reportTransfer(t, tracker, 1, "v", `{"status":"downloading","downloaded_bytes":10,"speed":-5,"eta":-2}`)
	if p := tracker.snapshot(); p.Speed != nil || p.ETA != nil {
		t.Fatal(p)
	}
	reportTransfer(t, tracker, 1, "v", `{"status":"downloading","downloaded_bytes":1}`)
	if tracker.snapshot().Downloaded != 10 {
		t.Fatal("delayed sample reduced bytes")
	}
}

func TestGalleryTransferCountsOnlyMediaAndStops(t *testing.T) {
	dir := t.TempDir()
	for name, size := range map[string]int{"media.mp4.part": 200, "cover.jpg": 50, "metadata.info.json": 900, "cookies.txt": 800} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("x", size)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tracker := &transferTracker{}
	ctx := context.WithValue(t.Context(), transferKey{}, tracker)
	stop := watchGalleryTransfer(ctx, dir, 3)
	stop() // joins the final sample, without leaving a watcher behind
	p := tracker.snapshot()
	if p.Downloaded != 250 || !p.Batch || p.Total != nil || p.Percent != nil || p.FileIndex != 2 || p.FileCount != 3 {
		t.Fatal(p)
	}
}
