package download_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
	"github.com/Dev-Wonjoon/Asterodia/internal/download"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

type progressFixtureYtdlp struct {
	*engine.Ytdlp
	raw json.RawMessage
}

func (f progressFixtureYtdlp) Discover(context.Context, string, string) (json.RawMessage, error) {
	return f.raw, nil
}

func TestMediaTransferVisibleThroughServiceWhileDownloading(t *testing.T) {
	executable := os.Getenv("ASTERODIA_TEST_YTDLP_EXE")
	if executable == "" {
		t.Skip("yt-dlp 실행 경로 필요")
	}
	const total = 4 << 20
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", fmt.Sprint(total))
		_, _ = w.Write(make([]byte, 64<<10))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	root := downloadTestRoot(t)
	url := server.URL + "/video.mp4"
	raw := json.RawMessage(fmt.Sprintf(`{"id":"fixture","title":"Progress fixture","extractor":"generic","extractor_key":"Generic","webpage_url":%q,"formats":[{"format_id":"mp4","url":%q,"ext":"mp4","protocol":"http","vcodec":"h264","acodec":"aac"}]}`, url, url))
	db := openStoreTestDB(t)
	service := download.NewService(db, func() string { return root }, func(root string, _ engine.Options) download.Runner {
		return download.Runner{Storage: download.Storage{Root: root}, Ytdlp: progressFixtureYtdlp{Ytdlp: &engine.Ytdlp{Executable: executable}, raw: raw}}
	}, nil)
	defer service.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	enqueued := service.Enqueue(ctx, download.EnqueueRequest{URL: url, Mode: "create"})
	if !enqueued.OK {
		t.Fatal(enqueued.Error)
	}
	job := enqueued.Data.(download.Job)
	done := make(chan api.Result, 1)
	go func() { done <- service.Run(ctx, download.RunRequest{JobID: job.ID, Trigger: "manual_initial"}) }()
	var live download.Job
	for {
		got := service.Get(ctx, download.JobRequest{JobID: job.ID})
		if !got.OK {
			t.Fatal(got.Error)
		}
		live = got.Data.(download.Job)
		if live.Transfer != nil && live.Transfer.Downloaded > 0 {
			break
		}
		select {
		case result := <-done:
			t.Fatalf("download ended without exposing live progress: %+v", result)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
	if live.Transfer.Total == nil || *live.Transfer.Total != total || live.Transfer.Percent == nil || *live.Transfer.Percent >= 100 || live.Transfer.FileCount != 1 {
		t.Fatalf("잘못된 실시간 수치: %+v", live.Transfer)
	}
	listed := service.List(ctx, download.ListRequest{})
	if !listed.OK {
		t.Fatal(listed.Error)
	}
	page := listed.Data.(map[string]any)
	for _, key := range []string{"data", "activeJobs"} {
		jobs := page[key].([]download.Job)
		if len(jobs) != 1 || jobs[0].Transfer == nil || jobs[0].Transfer.Downloaded <= 0 {
			t.Fatalf("%s 누락: %+v", key, jobs)
		}
	}
	if result := service.Cancel(ctx, download.JobRequest{JobID: job.ID}); !result.OK {
		t.Fatal(result.Error)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	ended := service.Get(ctx, download.JobRequest{JobID: job.ID})
	if !ended.OK || ended.Data.(download.Job).Transfer != nil {
		t.Fatalf("취소 후 진행 수치 잔존: %+v", ended)
	}
	logs := service.Logs(ctx, download.LogRequest{JobID: job.ID})
	if !logs.OK {
		t.Fatal(logs.Error)
	}
	for _, entry := range logs.Data.(download.LogPage).Entries {
		if strings.Contains(entry.Text, "__ASTERODIA_PROGRESS__:") {
			t.Fatal("private progress frames leaked into diagnostics")
		}
	}
	// The progress view does not create library records or alter staged identity.
	var count int
	if err := db.QueryRow("SELECT count(*) FROM media").Scan(&count); err != nil || count != 0 {
		t.Fatal("cancelled download created media", count, err)
	}
}
