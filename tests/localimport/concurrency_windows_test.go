package localimport_test

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/localimport"
)

func parallelFiles(t *testing.T, f *fixture, total int) {
	t.Helper()
	for i := range total {
		write(t, filepath.Join(f.source, fmt.Sprintf("%04d.png", i)), []byte{1})
	}
}

func awaitInspections(t *testing.T, entered <-chan struct{}, count int) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for range count {
		select {
		case <-entered:
		case <-timer.C:
			t.Fatalf("parallel inspections did not start (%d expected)", count)
		}
	}
}

func TestParallelImportCPUWorkersCancelAndResume(t *testing.T) {
	workers := max(1, runtime.NumCPU()/2)
	if workers < 2 {
		t.Skip("parallel import requires at least four logical CPUs")
	}
	f := newFixture(t)
	total := workers*3 + 1
	parallelFiles(t, f, total)
	entered := make(chan struct{}, total)
	var active, peak, bootstrap atomic.Int32
	original := f.inspect
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		if filepath.Base(path) == "0000.png" {
			bootstrap.Add(1)
			return original(ctx, path, kind)
		}
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		entered <- struct{}{}
		<-ctx.Done()
		return engine.LocalMetadata{}, ctx.Err()
	}
	p := scan(t, f)
	ok(t, f.service.Run(t.Context(), localimport.Request{JobID: p.Job.ID}))
	awaitInspections(t, entered, workers)
	if int(active.Load()) != workers || int(peak.Load()) != workers {
		t.Fatalf("worker limit: active=%d peak=%d want=%d", active.Load(), peak.Load(), workers)
	}
	ok(t, f.service.Cancel(t.Context(), localimport.Request{JobID: p.Job.ID}))
	p = wait(t, f.service, p.Job.ID, "cancelled")
	if active.Load() != 0 || p.Job.Counts.Added != 1 || count(t, f, "SELECT count(*) FROM media_file") != 1 {
		t.Fatalf("workers or checkpoints survived cancellation: active=%d counts=%+v", active.Load(), p.Job.Counts)
	}
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		if filepath.Base(path) == "0000.png" {
			bootstrap.Add(1)
		}
		return original(ctx, path, kind)
	}
	ok(t, f.service.Resume(t.Context(), localimport.Request{JobID: p.Job.ID}))
	p = wait(t, f.service, p.Job.ID, "completed")
	if p.Job.Counts.Added != total || p.Job.Counts.Processed != total || bootstrap.Load() != 1 || int(f.calls.Load()) != total || count(t, f, "SELECT count(DISTINCT file_uuid) FROM media_file") != int64(total) {
		t.Fatalf("resume duplicated a checkpoint: %+v calls=%d bootstrap=%d", p.Job.Counts, f.calls.Load(), bootstrap.Load())
	}
}

func TestParallelImportBoundsResultsAndCommitsNaturalOrder(t *testing.T) {
	workers := max(1, runtime.NumCPU()/2)
	if workers < 2 {
		t.Skip("parallel import requires at least four logical CPUs")
	}
	f := newFixture(t)
	total := workers*4 + 3
	parallelFiles(t, f, total)
	entered := make(chan struct{}, total)
	release := make(chan struct{})
	original := f.inspect
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		if filepath.Base(path) != "0000.png" {
			entered <- struct{}{}
		}
		if filepath.Base(path) == "0001.png" {
			select {
			case <-release:
			case <-ctx.Done():
				return engine.LocalMetadata{}, ctx.Err()
			}
		}
		return original(ctx, path, kind)
	}
	p := scan(t, f)
	ok(t, f.service.Run(t.Context(), localimport.Request{JobID: p.Job.ID}))
	awaitInspections(t, entered, workers*2)
	// Later files finish first, but neither commit order nor the bounded window
	// may advance beyond the unfinished first file of this batch.
	select {
	case <-entered:
		t.Fatal("the result window grew beyond two files per worker")
	case <-time.After(50 * time.Millisecond):
	}
	if count(t, f, "SELECT count(*) FROM media_file") != 1 {
		t.Fatal("out-of-order results were committed")
	}
	close(release)
	p = wait(t, f.service, p.Job.ID, "completed")
	if p.Job.Counts.Added != total {
		t.Fatal(p.Job.Counts)
	}
	rows, err := f.db.Query("SELECT position,title FROM media_file ORDER BY position")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var position int
		var title string
		if err = rows.Scan(&position, &title); err != nil {
			t.Fatal(err)
		}
		if position != i || title != fmt.Sprintf("%04d.png", i) {
			t.Fatalf("natural order at %d: %d %q", i, position, title)
		}
		i++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if i != total {
		t.Fatalf("missing files: %d/%d", i, total)
	}
}

func TestParallelImportCloseJoinsWorkers(t *testing.T) {
	workers := max(1, runtime.NumCPU()/2)
	f := newFixture(t)
	parallelFiles(t, f, workers*2+1)
	entered := make(chan struct{}, workers*2)
	var active atomic.Int32
	original := f.inspect
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		if filepath.Base(path) == "0000.png" {
			return original(ctx, path, kind)
		}
		active.Add(1)
		defer active.Add(-1)
		entered <- struct{}{}
		<-ctx.Done()
		return engine.LocalMetadata{}, ctx.Err()
	}
	p := scan(t, f)
	ok(t, f.service.Run(t.Context(), localimport.Request{JobID: p.Job.ID}))
	awaitInspections(t, entered, workers)
	done := make(chan struct{})
	go func() { f.service.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not join cancelled workers")
	}
	if active.Load() != 0 || count(t, f, "SELECT count(*) FROM media_file") != 1 {
		t.Fatal("Close returned with live workers or extra commits")
	}
}

func TestParallelImportRejectsChangedResultBeforeOrderedCommit(t *testing.T) {
	if runtime.NumCPU()/2 < 2 {
		t.Skip("parallel import requires at least four logical CPUs")
	}
	f := newFixture(t)
	parallelFiles(t, f, 4)
	release, inspected := make(chan struct{}), make(chan struct{})
	original := f.inspect
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		if filepath.Base(path) == "0001.png" {
			select {
			case <-release:
			case <-ctx.Done():
				return engine.LocalMetadata{}, ctx.Err()
			}
		}
		metadata, err := original(ctx, path, kind)
		if filepath.Base(path) == "0002.png" {
			close(inspected)
		}
		return metadata, err
	}
	p := scan(t, f)
	ok(t, f.service.Run(t.Context(), localimport.Request{JobID: p.Job.ID}))
	awaitInspections(t, inspected, 1)
	write(t, filepath.Join(f.source, "0002.png"), []byte{1, 2, 3})
	close(release)
	p = wait(t, f.service, p.Job.ID, "completed_with_errors")
	if p.Job.Counts.Added != 3 || p.Job.Counts.Failed != 1 || p.Job.Counts.Processed != 4 || count(t, f, "SELECT count(*) FROM media_file WHERE title='0002.png'") != 0 {
		t.Fatalf("changed speculative result was committed: %+v", p.Job.Counts)
	}
}

func TestImportPipelineRefillsBeforeSlowBatchTailFinishes(t *testing.T) {
	workers := max(1, runtime.NumCPU()/2)
	if workers < 2 {
		t.Skip("requires at least two import workers")
	}
	f := newFixture(t)
	window := workers * 2
	parallelFiles(t, f, window*3+1)
	release, tailStarted, refilled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	original := f.inspect
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		switch filepath.Base(path) {
		case fmt.Sprintf("%04d.png", window):
			close(tailStarted)
			select {
			case <-release:
			case <-ctx.Done():
				return engine.LocalMetadata{}, ctx.Err()
			}
		case fmt.Sprintf("%04d.png", window+1):
			close(refilled)
		}
		return original(ctx, path, kind)
	}
	p := scan(t, f)
	ok(t, f.service.Run(t.Context(), localimport.Request{JobID: p.Job.ID}))
	awaitInspections(t, tailStarted, 1)
	// The next window starts while the previous window's last file is blocked.
	awaitInspections(t, refilled, 1)
	once.Do(func() { close(release) })
	p = wait(t, f.service, p.Job.ID, "completed")
	if p.Job.Counts.Added != window*3+1 {
		t.Fatal(p.Job.Counts)
	}
}

func TestSlowFolderDoesNotBlockLaterFolders(t *testing.T) {
	workers := max(1, runtime.NumCPU()/2)
	if workers < 2 {
		t.Skip("requires at least two import workers")
	}
	f := newFixture(t)
	write(t, filepath.Join(f.source, "00-slow", "0000.png"), []byte{1})
	write(t, filepath.Join(f.source, "00-slow", "0001.png"), []byte{1})
	lastFolder := fmt.Sprintf("folder-%03d", workers+2)
	for i := 1; i <= workers+2; i++ {
		write(t, filepath.Join(f.source, fmt.Sprintf("folder-%03d", i), "0000.png"), []byte{1})
	}
	release, blocked, lastStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	original := f.inspect
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		if filepath.Base(path) == "0001.png" {
			close(blocked)
			select {
			case <-release:
			case <-ctx.Done():
				return engine.LocalMetadata{}, ctx.Err()
			}
		}
		if filepath.Base(filepath.Dir(path)) == lastFolder {
			close(lastStarted)
		}
		return original(ctx, path, kind)
	}
	p := scan(t, f)
	ok(t, f.service.Run(t.Context(), localimport.Request{JobID: p.Job.ID}))
	awaitInspections(t, blocked, 1)
	awaitInspections(t, lastStarted, 1)
	// Admission must advance beyond the initial active-folder window even while
	// the first folder still has an unfinished ordered result.
	once.Do(func() { close(release) })
	p = wait(t, f.service, p.Job.ID, "completed")
	if p.Job.Counts.Added != workers+4 || p.Job.Counts.CompletedFolders != workers+3 || count(t, f, "SELECT count(*) FROM media") != int64(workers+3) {
		t.Fatal(p.Job.Counts)
	}
}

func TestSharedImportBootstrapsAfterFirstFileFailure(t *testing.T) {
	f := newFixture(t)
	for _, folder := range []string{"one", "two"} {
		for i := range 3 {
			write(t, filepath.Join(f.source, folder, fmt.Sprintf("%04d.png", i)), []byte{1})
		}
	}
	original := f.inspect
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		if filepath.Base(path) == "0000.png" {
			return engine.LocalMetadata{}, fmt.Errorf("broken bootstrap candidate")
		}
		return original(ctx, path, kind)
	}
	p := run(t, f, scan(t, f))
	if p.Job.Counts.Added != 4 || p.Job.Counts.Failed != 2 || p.Job.Counts.Processed != 6 || p.Job.Counts.FailedFolders != 2 || count(t, f, "SELECT count(*) FROM media") != 2 {
		t.Fatal(p.Job.Counts)
	}
	if count(t, f, "SELECT count(*) FROM media_file WHERE (position=0 AND title='0001.png') OR (position=1 AND title='0002.png')") != 4 {
		t.Fatal("failed bootstrap changed grouping or file order")
	}
}
