package webserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
	"github.com/Dev-Wonjoon/Asterodia/internal/download"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

type waitingGallery struct {
	started chan context.Context
	release chan struct{}
}

func (g *waitingGallery) Discover(ctx context.Context, _ string, _ string) (engine.GalleryDLDiscovery, error) {
	select {
	case g.started <- ctx:
	case <-ctx.Done():
		return engine.GalleryDLDiscovery{}, ctx.Err()
	}
	select {
	case <-g.release:
		return engine.GalleryDLDiscovery{}, engine.ErrUnsupportedURL
	case <-ctx.Done():
		return engine.GalleryDLDiscovery{}, ctx.Err()
	}
}
func (g *waitingGallery) Download(context.Context, string, engine.GalleryDLDiscovery, string) ([]string, error) {
	return nil, engine.ErrUnsupportedURL
}
func configureWebDownloads(t *testing.T, f *fixture) (*download.Service, *waitingGallery) {
	t.Helper()
	g := &waitingGallery{started: make(chan context.Context, 4), release: make(chan struct{})}
	svc := download.NewService(f.db, func() string { return filepath.Join(f.root, "downloads") }, func(root string, _ engine.Options) download.Runner {
		return download.Runner{Storage: download.Storage{Root: root}, GalleryDL: g}
	}, nil)
	t.Cleanup(svc.Close)
	f.server.ConfigureDownloader(svc)
	return svc, g
}
func downloadPost(t *testing.T, f *fixture, path, body, token string) (int, api.Result) {
	t.Helper()
	state := status(t, f.server.Status())
	req, err := http.NewRequest("POST", state.URL+"/api/download/"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", state.URL)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Asterodia-CSRF", token)
	response, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	var result api.Result
	if strings.Contains(response.Header.Get("Content-Type"), "application/json") {
		if err = json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
	}
	return response.StatusCode, result
}
func downloadToken(t *testing.T, f *fixture) string {
	t.Helper()
	code, _, body := f.request(t, "GET", "/api/download/status", nil)
	var access struct {
		Enabled bool
		CSRF    string
	}
	json.Unmarshal(body, &access)
	if code != 200 || !access.Enabled || access.CSRF == "" {
		t.Fatal(code, string(body))
	}
	return access.CSRF
}
func TestWebDownloadPermissionsOriginAndRequestValidation(t *testing.T) {
	f := newFixture(t)
	configureWebDownloads(t, f)
	status(t, f.server.Start())
	if code, _, _ := f.request(t, "GET", "/api/download/list", nil); code != 403 {
		t.Fatal(code)
	}
	if code, _, body := f.request(t, "GET", "/api/download/status", nil); code != 200 || !bytes.Contains(body, []byte(`"csrf":""`)) {
		t.Fatal(code, string(body))
	}
	status(t, f.server.SetDownloading(true))
	token := downloadToken(t, f)
	if state := status(t, f.server.Status()); state.Editing || !state.Downloading {
		t.Fatal("download permission must be independent", state)
	}
	if code, _ := downloadPost(t, f, "enqueue", `{"url":"https://example.com/video"}`, "wrong"); code != 403 {
		t.Fatal(code)
	}
	state := status(t, f.server.Status())
	req, _ := http.NewRequest("POST", state.URL+"/api/download/enqueue", strings.NewReader(`{"url":"https://example.com/video"}`))
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Asterodia-CSRF", token)
	response, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal(response.StatusCode)
	}
	for _, body := range []string{`{"url":"https://example.com/video","mode":"replace","mediaId":1}`, `{"url":"file:///private"}`, `{"url":"https://example.com/video"} {}`, `{"url":"` + strings.Repeat("a", 33000) + `"}`} {
		if code, _ := downloadPost(t, f, "enqueue", body, token); code != 400 {
			t.Fatal("invalid request accepted", code)
		}
	}
	var count int
	if err = f.db.QueryRow("SELECT count(*) FROM download_queue").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	status(t, f.server.SetDownloading(false))
	if code, _ := downloadPost(t, f, "enqueue", `{"url":"https://example.com/video"}`, token); code != 403 {
		t.Fatal(code)
	}
}
func TestWebLinkDownloadContinuesAfterHTTPAndServerClose(t *testing.T) {
	f := newFixture(t)
	svc, gallery := configureWebDownloads(t, f)
	status(t, f.server.Start())
	status(t, f.server.SetDownloading(true))
	token := downloadToken(t, f)
	code, result := downloadPost(t, f, "enqueue", `{"url":"https://www.instagram.com/p/WebBackground/","options":{"type":"video","quality":"best","playlist":false}}`, token)
	if code != 200 || !result.OK {
		t.Fatal(code, result)
	}
	jobID := result.Data.(map[string]any)["id"].(string)
	var runContext context.Context
	select {
	case runContext = <-gallery.started:
	case <-time.After(3 * time.Second):
		t.Fatal("download was not launched")
	}
	if runContext.Err() != nil {
		t.Fatal("HTTP response cancelled the job")
	}
	_, _, data := f.request(t, "GET", "/api/download/list", nil)
	if !bytes.Contains(data, []byte(jobID)) || bytes.Contains(data, []byte(f.root)) {
		t.Fatal("public queue exposed a PC path or lost the job", string(data))
	}
	status(t, f.server.Stop())
	if runContext.Err() != nil {
		t.Fatal("stopping web server cancelled the accepted PC download")
	}
	if state := status(t, f.server.Status()); state.Downloading {
		t.Fatal("permission survived stopping server")
	}
	close(gallery.release)
	for deadline := time.Now().Add(4 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		job := svc.Get(t.Context(), download.JobRequest{JobID: jobID})
		if job.OK && job.Data.(download.Job).State == "failed" {
			return
		}
	}
	t.Fatal("PC worker did not finish after web server shutdown")
}

func TestWebLinkDownloadCancelAndRetry(t *testing.T) {
	f := newFixture(t)
	svc, gallery := configureWebDownloads(t, f)
	status(t, f.server.Start())
	status(t, f.server.SetDownloading(true))
	token := downloadToken(t, f)
	code, result := downloadPost(t, f, "enqueue", `{"url":"https://www.instagram.com/p/WebRetry/"}`, token)
	if code != 200 || !result.OK {
		t.Fatal(code, result)
	}
	id := result.Data.(map[string]any)["id"].(string)
	body, _ := json.Marshal(map[string]string{"jobId": id})
	for attempt := 0; attempt < 2; attempt++ {
		var ctx context.Context
		select {
		case ctx = <-gallery.started:
		case <-time.After(3 * time.Second):
			t.Fatal("download attempt did not start")
		}
		code, result = downloadPost(t, f, "cancel", string(body), token)
		if code != 200 || !result.OK {
			t.Fatal(code, result)
		}
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
			t.Fatal("cancellation did not reach worker")
		}
		cancelled := false
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			result = svc.Get(t.Context(), download.JobRequest{JobID: id})
			if result.OK && result.Data.(download.Job).State == "cancelled" {
				cancelled = true
				break
			}
		}
		if !cancelled {
			t.Fatal("download did not reach cancelled state")
		}
		if attempt == 0 {
			time.Sleep(20 * time.Millisecond)
			code, result = downloadPost(t, f, "retry", string(body), token)
			if code != 200 || !result.OK {
				t.Fatal(code, result)
			}
		}
	}
}
