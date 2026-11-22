package download

import (
	"context"
	"encoding/json"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

const transferPrefix = "__STORAGEBUCKET_PROGRESS__:"

// TransferProgress describes the current file, not publication to the library.
// It is transient and belongs to one attempt; retrying never reuses old samples.
type TransferProgress struct {
	Downloaded int64    `json:"downloaded"`
	Total      *int64   `json:"total"`
	Estimated  bool     `json:"estimated"`
	Speed      *float64 `json:"speed"`
	ETA        *float64 `json:"eta"`
	Percent    *float64 `json:"percent"`
	FileIndex  int      `json:"fileIndex"`
	FileCount  int      `json:"fileCount"`
	Processing bool     `json:"processing"`
	Batch      bool     `json:"batch"`
}

type transferSample struct {
	Status     string   `json:"status"`
	Downloaded int64    `json:"downloaded_bytes"`
	Total      *int64   `json:"total_bytes"`
	Estimate   *int64   `json:"total_bytes_estimate"`
	Speed      *float64 `json:"speed"`
	ETA        *float64 `json:"eta"`
}

type transferTracker struct {
	mu        sync.Mutex
	attempt   string
	view      TransferProgress
	streams   map[string]transferSample
	updatedAt time.Time
	seen      bool
}

type transferKey struct{}

func transferFrom(ctx context.Context) *transferTracker {
	t, _ := ctx.Value(transferKey{}).(*transferTracker)
	return t
}

func (t *transferTracker) beginFile(index, count int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.view = TransferProgress{FileIndex: index, FileCount: count}
	t.streams = make(map[string]transferSample)
	t.seen = false
}

func (t *transferTracker) output(output engine.Output) bool {
	line := strings.TrimSpace(output.Text)
	if !strings.HasPrefix(line, transferPrefix) {
		return false
	}
	// Keep private machine-readable frames out of console diagnostics, even
	// when an engine supplies a malformed frame.
	if t == nil {
		return true
	}
	var frame struct {
		Streams  int            `json:"streams"`
		FormatID string         `json:"formatId"`
		Download transferSample `json:"download"`
	}
	if json.Unmarshal([]byte(strings.TrimPrefix(line, transferPrefix)), &frame) != nil ||
		frame.Streams < 1 || frame.Streams > 2 || frame.FormatID == "" ||
		(frame.Download.Status != "downloading" && frame.Download.Status != "finished") ||
		frame.Download.Downloaded < 0 || frame.Download.Downloaded > math.MaxInt64/4 {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.view.Batch || t.view.FileCount == 0 {
		return true
	}
	if _, exists := t.streams[frame.FormatID]; !exists && len(t.streams) >= frame.Streams {
		return true
	}
	previous := t.streams[frame.FormatID]
	if previous.Status == "finished" || frame.Download.Downloaded < previous.Downloaded {
		return true
	}
	t.streams[frame.FormatID] = frame.Download
	t.view.Downloaded, t.view.Total, t.view.Percent = 0, nil, nil
	t.view.Estimated = false
	known, finished, total := len(t.streams) == frame.Streams, 0, int64(0)
	for _, sample := range t.streams {
		t.view.Downloaded += sample.Downloaded
		if sample.Status == "finished" {
			finished++
		}
		size := sample.Total
		if sample.Status == "finished" {
			size = &sample.Downloaded
		} else if size == nil || *size <= 0 {
			size = sample.Estimate
			t.view.Estimated = true
		}
		if size == nil || *size <= 0 || *size > math.MaxInt64/4 {
			known = false
		} else {
			total += max(*size, sample.Downloaded)
		}
	}
	if known && total > 0 {
		t.view.Total = &total
		percent := min(100.0, float64(t.view.Downloaded)/float64(total)*100)
		t.view.Percent = &percent
	}
	t.view.Processing = finished == frame.Streams
	t.view.Speed, t.view.ETA = validMeasure(frame.Download.Speed), validMeasure(frame.Download.ETA)
	if !known {
		// A remaining audio stream has not been measured yet.
		t.view.ETA = nil
	}
	if frame.Download.Status == "finished" {
		t.view.Speed, t.view.ETA = nil, nil
	}
	t.seen, t.updatedAt = true, time.Now()
	return true
}

func validMeasure(value *float64) *float64 {
	if value == nil || *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return nil
	}
	return value
}

func (t *transferTracker) snapshot() *TransferProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.seen {
		return nil
	}
	view := t.view
	if time.Since(t.updatedAt) > 3*time.Second {
		view.Speed, view.ETA = nil, nil
	}
	return &view
}

func (s *Service) attachTransfer(job *Job) {
	if job.State != "running" || job.CurrentAttemptID == nil {
		return
	}
	value, ok := s.transfers.Load(job.ID)
	if !ok {
		return
	}
	t := value.(*transferTracker)
	if t.attempt != *job.CurrentAttemptID {
		return
	}
	job.Transfer = t.snapshot()
	if p := job.Transfer; p != nil && p.Percent != nil && job.Phase != nil && *job.Phase == "downloading" {
		job.Progress = min(99, int((float64(p.FileIndex-1)+*p.Percent/100)/float64(max(1, p.FileCount))*100))
	}
}

// gallery-dl does not expose stable per-byte progress frames. Sample only this
// attempt's media files instead; never invent a total size or a byte percentage.
func watchGalleryTransfer(ctx context.Context, dir string, count int) func() {
	t := transferFrom(ctx)
	if t == nil {
		return func() {}
	}
	t.beginFile(1, count)
	t.mu.Lock()
	t.view.Batch = true
	t.mu.Unlock()
	stop, done := make(chan struct{}), make(chan struct{})
	sample := func() {
		var bytes int64
		completed, visited := 0, 0
		_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			visited++
			if ctx.Err() != nil || visited > 20000 {
				return fs.SkipAll
			}
			if err != nil || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			name := strings.TrimSuffix(path, ".part")
			if engine.LocalKind(name) == "" {
				return nil
			}
			info, err := entry.Info()
			if err == nil && info.Mode().IsRegular() && info.Size() > 0 {
				bytes += info.Size()
				if name == path {
					completed++
				}
			}
			return nil
		})
		t.mu.Lock()
		now := time.Now()
		if bytes >= t.view.Downloaded {
			if t.seen {
				speed := float64(bytes-t.view.Downloaded) / now.Sub(t.updatedAt).Seconds()
				t.view.Speed = validMeasure(&speed)
			}
			t.view.Downloaded = bytes
			t.view.FileIndex = min(count, completed+1)
			t.updatedAt, t.seen = now, true
		}
		t.mu.Unlock()
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sample()
			case <-stop:
				sample()
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() { close(stop); <-done }
}
