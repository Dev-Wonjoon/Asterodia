package integration_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

func concurrentJobs(t *testing.T, f *fixture, count int) []download.Job {
	t.Helper()
	jobs := make([]download.Job, count)
	for i := range jobs {
		jobs[i] = success(t, f.service.Enqueue(t.Context(), download.EnqueueRequest{Mode: "create", URL: fmt.Sprintf("https://www.instagram.com/p/Parallel%d/", i)})).(download.Job)
		if !jobs[i].Waiting {
			t.Fatal("실행 요청 전 작업이 대기로 표시되지 않았습니다")
		}
	}
	return jobs
}

func awaitDownload[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("다운로드 상태 변경 대기 시간 초과")
		var zero T
		return zero
	}
}

// Observe the public log so cancellation happens after Run has registered the
// attempt, rather than racing with a goroutine that has not entered Run yet.
func awaitDownloadRun(t *testing.T, f *fixture, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		page := success(t, f.service.Logs(t.Context(), download.LogRequest{JobID: id})).(download.LogPage)
		if len(page.Entries) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("실행 요청이 등록되지 않았습니다")
}

func TestQueueRunsThreeDownloadsAndCancelsWaitingJobIndependently(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan string, 5)
	var calls atomic.Int32
	f.gallery = func(ctx context.Context, url, _ string) (engine.GalleryDLDiscovery, error) {
		calls.Add(1)
		started <- url
		<-ctx.Done()
		return engine.GalleryDLDiscovery{}, ctx.Err()
	}
	jobs := concurrentJobs(t, f, 5)
	results := make([]chan api.Result, len(jobs))
	start := func(index int) {
		results[index] = make(chan api.Result, 1)
		go func() {
			results[index] <- f.service.Run(ctx, download.RunRequest{JobID: jobs[index].ID, Trigger: "manual_initial"})
		}()
	}
	for i := range 3 {
		start(i)
	}
	for range 3 {
		awaitDownload(t, started)
	}
	for _, job := range jobs[:3] {
		if success(t, f.service.Get(ctx, download.JobRequest{JobID: job.ID})).(download.Job).Waiting {
			t.Fatal("실제 실행 중인 작업을 대기로 표시했습니다")
		}
	}
	start(3)
	awaitDownloadRun(t, f, jobs[3].ID)
	page := success(t, f.service.List(ctx, download.ListRequest{})).(map[string]any)
	for _, key := range []string{"data", "activeJobs"} {
		for _, job := range page[key].([]download.Job) {
			if job.ID == jobs[3].ID && !job.Waiting {
				t.Fatalf("%s 목록에서 실행 자리 대기 상태가 빠졌습니다", key)
			}
		}
	}
	success(t, f.service.Cancel(ctx, download.JobRequest{JobID: jobs[3].ID}))
	errorCode(t, awaitDownload(t, results[3]), "CANCELLED")
	if calls.Load() != 3 {
		t.Fatalf("대기 작업이 실행됐습니다: %d", calls.Load())
	}
	for i := range 3 {
		select {
		case result := <-results[i]:
			t.Fatalf("다른 작업이 함께 종료됐습니다: %+v", result)
		default:
		}
	}
	start(4)
	awaitDownloadRun(t, f, jobs[4].ID)
	if calls.Load() != 3 {
		t.Fatal("동시 실행 한도를 초과했습니다")
	}
	success(t, f.service.Cancel(ctx, download.JobRequest{JobID: jobs[0].ID}))
	errorCode(t, awaitDownload(t, results[0]), "CANCELLED")
	if awaitDownload(t, started) != jobs[4].URL {
		t.Fatal("취소된 대기 작업이 다시 실행됐습니다")
	}
	if success(t, f.service.Get(ctx, download.JobRequest{JobID: jobs[4].ID})).(download.Job).Waiting {
		t.Fatal("자리를 얻은 작업이 대기 상태로 남았습니다")
	}
	for _, i := range []int{1, 2, 4} {
		success(t, f.service.Cancel(ctx, download.JobRequest{JobID: jobs[i].ID}))
		errorCode(t, awaitDownload(t, results[i]), "CANCELLED")
	}
	for _, job := range jobs {
		final := success(t, f.service.Get(ctx, download.JobRequest{JobID: job.ID})).(download.Job)
		if final.State != "cancelled" || final.Waiting {
			t.Fatalf("취소 후 잘못된 상태: %+v", final)
		}
	}
	assertFK(t, f.db)
}

func TestQueueCloseStopsRunningAndWaitingDownloads(t *testing.T) {
	f := newFixture(t)
	started := make(chan struct{}, 4)
	f.gallery = func(ctx context.Context, _, _ string) (engine.GalleryDLDiscovery, error) {
		started <- struct{}{}
		<-ctx.Done()
		return engine.GalleryDLDiscovery{}, ctx.Err()
	}
	jobs := concurrentJobs(t, f, 4)
	results := make(chan api.Result, len(jobs))
	for _, job := range jobs[:3] {
		go func() {
			results <- f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"})
		}()
	}
	for range 3 {
		awaitDownload(t, started)
	}
	go func() {
		results <- f.service.Run(t.Context(), download.RunRequest{JobID: jobs[3].ID, Trigger: "manual_initial"})
	}()
	awaitDownloadRun(t, f, jobs[3].ID)
	closed := make(chan struct{})
	go func() { f.service.Close(); close(closed) }()
	awaitDownload(t, closed)
	for range jobs {
		errorCode(t, awaitDownload(t, results), "CANCELLED")
	}
	select {
	case <-started:
		t.Fatal("종료 중 대기 작업이 실행됐습니다")
	default:
	}
	assertFK(t, f.db)
}

func TestQueuePublishesConcurrentDownloadsWithoutPathCollisions(t *testing.T) {
	f := newFixture(t)
	discover := f.gallery
	started, release := make(chan struct{}, 6), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	f.gallery = func(ctx context.Context, url, dir string) (engine.GalleryDLDiscovery, error) {
		started <- struct{}{}
		select {
		case <-release:
			return discover(ctx, url, dir)
		case <-ctx.Done():
			return engine.GalleryDLDiscovery{}, ctx.Err()
		}
	}
	jobs := concurrentJobs(t, f, 6)
	results := make(chan api.Result, len(jobs))
	for _, job := range jobs {
		go func() {
			results <- f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"})
		}()
	}
	for range 3 {
		awaitDownload(t, started)
	}
	unblock()
	paths := map[string]bool{}
	for range jobs {
		result := success(t, awaitDownload(t, results)).(map[string]any)
		path := publishedPath(t, f, result["mediaIdSnapshot"].(int64))
		if paths[path] {
			t.Fatalf("동시 다운로드가 같은 최종 경로를 사용했습니다: %s", path)
		}
		paths[path] = true
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	if queryInt(t, f.db, "SELECT count(*) FROM download_queue WHERE state='completed'") != int64(len(jobs)) {
		t.Fatal("일부 다운로드를 완료하지 못했습니다")
	}
	assertFK(t, f.db)
}
