package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

func TestDownloadLogChild(t *testing.T) {
	mode := os.Getenv("STORAGEBUCKET_LOG_CHILD")
	if mode == "" {
		return
	}
	fmt.Println("[download] 42.0% of 1.00MiB at 1MiB/s ETA 00:01")
	fmt.Fprintln(os.Stderr, "WARNING: actual child warning")
	fmt.Fprintln(os.Stderr, "Authorization: Bearer should-not-leak")
	fmt.Fprintln(os.Stderr, "Cookie: session=should-not-leak")
	fmt.Fprintln(os.Stderr, "https://example.test/file?signature=should-not-leak")
	fmt.Println(`{"private_extractor_metadata":"should-not-leak"}`)
	fmt.Println(`__STORAGEBUCKET_RESULT__:{"filepath":"private transport"}`)
	if mode == "wait" {
		time.Sleep(30 * time.Second)
	}
	if mode == "burst" {
		for i := 0; i < 4000; i++ {
			fmt.Printf("[download] log-%d %s\n", i, strings.Repeat("한글", 300))
		}
	}
	if mode == "fail" {
		fmt.Fprintln(os.Stderr, "ERROR: native fixture failure")
		os.Exit(7)
	}
	os.Exit(0)
}
func logChild(ctx context.Context, dir string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	_, err = new(engine.ProcessRunner).Run(ctx, engine.ProcessRequest{Executable: executable, Directory: dir, Timeout: 15 * time.Second, Arguments: []string{"-test.run=^TestDownloadLogChild$", "-test.count=1"}, StreamOutput: true})
	return err
}
func logs(t *testing.T, f *fixture, job string, req download.LogRequest) download.LogPage {
	t.Helper()
	req.JobID = job
	return success(t, f.service.Logs(t.Context(), req)).(download.LogPage)
}

func TestDiscoveryFallbackTracePersistsAfterSuccess(t *testing.T) {
	f := newFixture(t)
	original := f.gallery
	f.gallery = func(ctx context.Context, url, dir string) (engine.GalleryDLDiscovery, error) {
		chain := engine.RunDiscoveryChain(ctx,
			engine.DiscoveryHandler[engine.GalleryDLDiscovery]{Name: "fixture-first", Handle: func(context.Context) engine.DiscoveryStepResult[engine.GalleryDLDiscovery] {
				return engine.DiscoveryStepResult[engine.GalleryDLDiscovery]{Decision: engine.DiscoveryNext, Err: errors.New("https://example.invalid/?signature=private-discovery-value")}
			}},
			engine.DiscoveryHandler[engine.GalleryDLDiscovery]{Name: "fixture-second", Handle: func(ctx context.Context) engine.DiscoveryStepResult[engine.GalleryDLDiscovery] {
				value, err := original(ctx, url, dir)
				return engine.DiscoveryStepResult[engine.GalleryDLDiscovery]{Decision: engine.DiscoveryHandled, Value: value, Err: err}
			}},
		)
		return chain.Value, chain.Err
	}
	job := f.enqueue(t)
	f.run(t, job)
	page := logs(t, f, job.ID, download.LogRequest{Limit: 200})
	var text strings.Builder
	for _, entry := range page.Entries {
		text.WriteString(entry.Text)
		text.WriteByte('\n')
	}
	got := text.String()
	failed, succeeded := strings.Index(got, "fixture-first · 조회 실패"), strings.Index(got, "fixture-second · 조회 성공")
	if failed < 0 || succeeded <= failed || strings.Contains(got, "private-discovery-value") {
		t.Fatal("성공한 작업의 조회 이력이 유실되거나 비공개 주소가 기록됐습니다", got)
	}
}

func TestDownloadOutputLogsPersistAndSeparateAttempts(t *testing.T) {
	f := newFixture(t)
	original := f.gallery
	f.gallery = func(ctx context.Context, url, dir string) (engine.GalleryDLDiscovery, error) {
		if err := logChild(ctx, dir); err != nil {
			return engine.GalleryDLDiscovery{}, err
		}
		return original(ctx, url, dir)
	}
	job := f.enqueue(t)
	if _, err := f.db.Exec("UPDATE download_queue SET log='previous unstructured log' WHERE id=?", job.ID); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STORAGEBUCKET_LOG_CHILD", "fail")
	errorCode(t, f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"}), "DOWNLOAD_ENGINE_FAILED")
	got := success(t, f.service.Get(t.Context(), download.JobRequest{JobID: job.ID})).(download.Job)
	if got.ErrorMessage == nil || !strings.Contains(*got.ErrorMessage, "7") {
		t.Fatal("실제 오류 전달 누락", got.ErrorMessage)
	}
	page := logs(t, f, job.ID, download.LogRequest{Limit: 200})
	var text strings.Builder
	var last int64
	var actualStdout, actualStderr bool
	for _, entry := range page.Entries {
		if entry.Sequence <= last {
			t.Fatal("로그 순번이 증가하지 않습니다")
		}
		last = entry.Sequence
		text.WriteString(entry.Text)
		actualStdout = actualStdout || (entry.Stream == "stdout" && strings.Contains(entry.Text, "42.0%"))
		actualStderr = actualStderr || (entry.Stream == "stderr" && strings.Contains(entry.Text, "native fixture failure"))
	}
	if !actualStdout || !actualStderr || !strings.Contains(text.String(), "previous unstructured log") || strings.Contains(text.String(), "should-not-leak") || strings.Contains(text.String(), "private transport") {
		t.Fatal("실제 출력 보존/민감정보 제외 실패", text.String())
	}
	firstAttempt := *page.CurrentAttemptID
	t.Setenv("STORAGEBUCKET_LOG_CHILD", "ok")
	success(t, f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_retry"}))
	page = logs(t, f, job.ID, download.LogRequest{Limit: 200})
	if len(page.Attempts) != 2 || *page.CurrentAttemptID == firstAttempt {
		t.Fatal("재시도 구분 유실")
	}
	old := logs(t, f, job.ID, download.LogRequest{AttemptID: firstAttempt, Limit: 200})
	for _, entry := range old.Entries {
		if entry.AttemptID != firstAttempt {
			t.Fatal("다른 시도의 출력 혼합")
		}
	}
	tail := logs(t, f, job.ID, download.LogRequest{Limit: 2})
	if len(tail.Entries) != 2 || !tail.HasOlder {
		t.Fatal("로그 페이지 조회 누락")
	}
	previous := logs(t, f, job.ID, download.LogRequest{Before: tail.Entries[0].Sequence, Limit: 2})
	if len(previous.Entries) != 2 || previous.Entries[1].Sequence >= tail.Entries[0].Sequence {
		t.Fatal("이전 로그 커서 오류")
	}
	// A new service reads persisted logs, independently of in-memory observers.
	reopened := download.NewService(f.db, nil, nil, nil)
	defer reopened.Close()
	persisted := success(t, reopened.Logs(t.Context(), download.LogRequest{JobID: job.ID, Limit: 200})).(download.LogPage)
	if len(persisted.Entries) != len(page.Entries) {
		t.Fatal("서비스 재생성 후 로그 유실")
	}
	assertFK(t, f.db)
}
func TestLiveLogsDoNotBlockGetOrCancel(t *testing.T) {
	f := newFixture(t)
	t.Setenv("STORAGEBUCKET_LOG_CHILD", "wait")
	f.gallery = func(ctx context.Context, url, dir string) (engine.GalleryDLDiscovery, error) {
		return engine.GalleryDLDiscovery{}, logChild(ctx, dir)
	}
	job := f.enqueue(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.service.Run(context.Background(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"})
	}()
	deadline := time.Now().Add(5 * time.Second)
	seen := false
	for time.Now().Before(deadline) {
		page := logs(t, f, job.ID, download.LogRequest{Limit: 200})
		for _, entry := range page.Entries {
			seen = seen || (entry.Stream == "stdout" && strings.Contains(entry.Text, "42.0%"))
		}
		if seen {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !seen {
		t.Fatal("실행 중 실제 출력이 갱신되지 않았습니다")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	success(t, f.service.Get(ctx, download.JobRequest{JobID: job.ID}))
	success(t, f.service.Cancel(ctx, download.JobRequest{JobID: job.ID}))
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("로그 처리 때문에 엔진 취소가 막혔습니다")
	}
	if logs(t, f, job.ID, download.LogRequest{}).State != "cancelled" {
		t.Fatal("취소 상태 유실")
	}
}
func TestLogRetentionAndStorageFailure(t *testing.T) {
	t.Run("bounded", func(t *testing.T) {
		f := newFixture(t)
		t.Setenv("STORAGEBUCKET_LOG_CHILD", "burst")
		original := f.gallery
		f.gallery = func(ctx context.Context, url, dir string) (engine.GalleryDLDiscovery, error) {
			if err := logChild(ctx, dir); err != nil {
				return engine.GalleryDLDiscovery{}, err
			}
			return original(ctx, url, dir)
		}
		job := f.enqueue(t)
		f.run(t, job)
		page := logs(t, f, job.ID, download.LogRequest{Limit: 200})
		if !page.Truncated || len(page.Entries) > 200 {
			t.Fatal("로그 보관 한도 누락")
		}
		var raw string
		if err := f.db.QueryRow("SELECT log FROM download_queue WHERE id=?", job.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if len(raw) > 1<<20 || !json.Valid([]byte(raw)) {
			t.Fatal("로그 크기/형식 한도 위반")
		}
	})
	t.Run("logging-failure-keeps-download", func(t *testing.T) {
		f := newFixture(t)
		if _, err := f.db.Exec("CREATE TRIGGER fixture_log_failure BEFORE UPDATE OF log ON download_queue BEGIN SELECT RAISE(ABORT,'log storage unavailable'); END"); err != nil {
			t.Fatal(err)
		}
		job := f.enqueue(t)
		f.run(t, job)
		page := logs(t, f, job.ID, download.LogRequest{})
		if page.State != "completed" || page.Warning == nil {
			t.Fatal("로깅 실패가 다운로드 결과를 바꾸거나 경고를 숨겼습니다")
		}
	})
}
func TestLogRequestBounds(t *testing.T) {
	f := newFixture(t)
	for _, req := range []download.LogRequest{{}, {JobID: "job", Limit: 201}, {JobID: "job", Before: -1}, {JobID: "job", Limit: -1}} {
		errorCode(t, f.service.Logs(t.Context(), req), "INVALID_ARGUMENT")
	}
	errorCode(t, f.service.Logs(t.Context(), download.LogRequest{JobID: "missing"}), "NOT_FOUND")
}
