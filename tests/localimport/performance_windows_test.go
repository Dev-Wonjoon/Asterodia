package localimport_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

// Opt-in comparison of the real hash/checkpoint path with controlled inspection
// latency, or the real ImageMagick inspector when STORAGEBUCKET_IMPORT_PERF_NATIVE
// is 1. Timings exclude fixture generation and scanning and have no pass threshold.
func TestImportPipelinePerformance(t *testing.T) {
	if os.Getenv("STORAGEBUCKET_IMPORT_PERF") != "1" {
		t.Skip("set STORAGEBUCKET_IMPORT_PERF=1 for scheduler throughput measurements")
	}
	for _, tc := range []struct {
		name           string
		folders, files int
	}{{"many_small_folders", 48, 3}, {"single_folder", 1, 144}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			data := make([]byte, 128*1024)
			native := os.Getenv("STORAGEBUCKET_IMPORT_PERF_NATIVE") == "1"
			if native {
				if !filepath.IsAbs(os.Getenv("STORAGEBUCKET_TEST_IMAGE_EXE")) {
					t.Fatal("native measurement requires STORAGEBUCKET_TEST_IMAGE_EXE")
				}
				img := image.NewRGBA(image.Rect(0, 0, 1024, 768))
				for i := range img.Pix {
					img.Pix[i] = byte(i*31 + i/1024)
				}
				var encoded bytes.Buffer
				if err := png.Encode(&encoded, img); err != nil {
					t.Fatal(err)
				}
				data = encoded.Bytes()
			}
			for folder := range tc.folders {
				for file := range tc.files {
					write(t, filepath.Join(f.source, fmt.Sprintf("folder-%03d", folder), fmt.Sprintf("%04d.png", file)), data)
				}
			}
			var active, peak atomic.Int32
			original := f.inspect
			if native {
				inspector := engine.LocalInspector{ImageMagick: os.Getenv("STORAGEBUCKET_TEST_IMAGE_EXE"), Cache: filepath.Join(f.root, "cache"), InspectionThreads: 1}
				original = inspector.Inspect
			}
			f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
				n := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
				}
				if native {
					return original(ctx, path, kind)
				}
				delay := 8 * time.Millisecond
				if filepath.Base(path) == "0016.png" {
					delay = 80 * time.Millisecond
				}
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return engine.LocalMetadata{}, ctx.Err()
				case <-timer.C:
				}
				return original(ctx, path, kind)
			}
			page := scan(t, f)
			started := time.Now()
			page = run(t, f, page)
			elapsed := time.Since(started)
			total := tc.folders * tc.files
			if page.Job.State != "completed" || page.Job.Counts.Added != total || page.Job.Counts.Processed != total || active.Load() != 0 {
				t.Fatal(page.Job)
			}
			t.Logf("native=%v files=%d elapsed=%s files/s=%.1f peak_inspections=%d", native, total, elapsed.Round(time.Millisecond), float64(total)/elapsed.Seconds(), peak.Load())
		})
	}
}
