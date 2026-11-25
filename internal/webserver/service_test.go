package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"github.com/Dev-Wonjoon/StorageBucket/internal/mediaedit"
)

func TestPublicDownloadErrorsDoNotExposeDiagnostics(t *testing.T) {
	private := `open C:\Users\private\cookies.txt: https://cdn.example/media?signature=secret`
	path := `C:\Users\private\thumbnail.jpg`
	code := "DOWNLOAD_ENGINE_FAILED"
	job := download.Job{ID: "job", State: "failed", ErrorCode: &code, ErrorMessage: &private, ThumbnailPath: &path}
	jobs := []download.Job{job}
	for _, result := range []api.Result{
		{Error: &api.Error{Code: code, Message: private, Context: map[string]int64{private: 1}}, Data: private},
		api.Failure(private, private),
		{},
		api.Success(job),
		api.Success(map[string]any{"data": jobs, "activeJobs": jobs, "total": 1}),
	} {
		public := publicDownloadResult(result)
		data, err := json.Marshal(public)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "private") || strings.Contains(string(data), "signature") || strings.Contains(string(data), "secret") || strings.Contains(string(data), "cdn.example") {
			t.Fatalf("public response exposed diagnostics: %s", data)
		}
		if !public.OK && (public.Error == nil || public.Error.Code != code || public.Error.Message == "" || public.Data != nil) {
			t.Fatalf("public error lost its stable envelope: %+v", public)
		}
	}
	if *job.ErrorMessage != private || *job.ErrorCode != code || *job.ThumbnailPath != path {
		t.Fatal("redacting a web response mutated desktop diagnostics")
	}
	if jobs[0].ThumbnailPath == nil || *jobs[0].ErrorMessage != private || *jobs[0].ErrorCode != code {
		t.Fatal("redacting a web list mutated its original job slice")
	}
	for _, code := range []string{"INVALID_ARGUMENT", "AUTHENTICATION_REQUIRED", "CLEANUP_REQUIRED", "ENGINE_STOP_UNCONFIRMED", "CANCELLED"} {
		public := publicDownloadResult(api.Failure(code, private))
		if public.Error.Code != code || public.Error.Message == private || public.Error.Message == "" {
			t.Fatalf("known error code was lost or raw text retained: %+v", public)
		}
	}
	messageOnly := publicDownloadResult(api.Success(download.Job{ErrorMessage: &private})).Data.(download.Job)
	unknownCode := publicDownloadResult(api.Success(download.Job{ErrorCode: &private})).Data.(download.Job)
	for _, job := range []download.Job{messageOnly, unknownCode} {
		if job.ErrorCode == nil || *job.ErrorCode != "DOWNLOAD_ENGINE_FAILED" || job.ErrorMessage == nil || *job.ErrorMessage == private {
			t.Fatalf("partial or unknown job error was not sanitized: %+v", job)
		}
	}
}

func TestShutdownAllowsInFlightHandlerToReadStatus(t *testing.T) {
	for _, closeService := range []bool{false, true} {
		name := "Stop"
		if closeService {
			name = "Close"
		}
		t.Run(name, func(t *testing.T) {
			s := NewService(media.NewService(nil), "127.0.0.1:0")
			t.Cleanup(s.Close)
			entered := make(chan struct{})
			handlerDone := make(chan bool, 1)
			s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-r.Context().Done()
				statusDone := make(chan struct{})
				go func() { s.Status(); close(statusDone) }()
				select {
				case <-statusDone:
					handlerDone <- true
				case <-time.After(time.Second):
					// Return so the original deadlock also fails without hanging cleanup.
					handlerDone <- false
				}
			})
			result := s.Start()
			if !result.OK {
				t.Fatal(result.Error)
			}
			r := httptest.NewRequest(http.MethodGet, "http://localhost/api/edit/status", nil)
			r = r.WithContext(s.server.BaseContext(nil))
			requestDone := make(chan struct{})
			go func() {
				defer close(requestDone)
				allowTestRequest(s, r)
				s.serveHTTP(httptest.NewRecorder(), r)
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("request did not enter handler")
			}
			stopped := make(chan struct{})
			go func() {
				if closeService {
					s.Close()
				} else {
					s.Stop()
				}
				close(stopped)
			}()
			select {
			case <-stopped:
			case <-time.After(3 * time.Second):
				t.Fatal("shutdown did not finish")
			}
			if !<-handlerDone {
				t.Error("shutdown held the state lock while waiting for the handler")
			}
			<-requestDone
			if s.Status().Data.(Status).Running {
				t.Fatal("server remained running after shutdown")
			}
		})
	}
}

func TestStartWaitsForPreviousHandlers(t *testing.T) {
	s := NewService(media.NewService(nil), "127.0.0.1:0")
	t.Cleanup(s.Close)
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	// Always release before cleanup, including when an assertion fails.
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(cancelled)
		<-release
	})
	result := s.Start()
	if !result.OK {
		t.Fatal(result.Error)
	}
	r := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	r = r.WithContext(s.server.BaseContext(nil))
	allowTestRequest(s, r)
	go s.serveHTTP(httptest.NewRecorder(), r)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not enter handler")
	}
	stopped := make(chan struct{})
	go func() { s.Stop(); close(stopped) }()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("request was not cancelled")
	}
	started := make(chan bool, 1)
	go func() { started <- s.Start().OK }()
	select {
	case <-started:
		t.Fatal("server restarted before old handlers finished")
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not finish after releasing handler")
	}
	select {
	case ok := <-started:
		if !ok {
			t.Fatal("restart failed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("restart did not finish")
	}
}

func TestRejectedWritesDoNotConsumeSharedRateLimit(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			s := NewService(nil, "")
			s.server = &http.Server{}
			s.downloader, s.editor = &download.Service{}, &mediaedit.Service{}
			s.downloading, s.editing = enabled, enabled
			s.csrf, s.editTokens, s.editLast = "test-csrf", 8, time.Now()
			for _, path := range []string{"/api/download/preview", "/api/edit/prepare"} {
				for _, headers := range []struct{ origin, token, contentType string }{
					{"", s.csrf, "application/json"},
					{"http://localhost", "", "application/json"},
					{"http://localhost", "wrong", "application/json"},
					{"http://localhost", s.csrf, "text/plain"},
				} {
					for i := 0; i < 12; i++ {
						r := httptest.NewRequest(http.MethodPost, "http://localhost"+path, strings.NewReader("{}"))
						r.Header.Set("Origin", headers.origin)
						r.Header.Set("X-StorageBucket-CSRF", headers.token)
						r.Header.Set("Content-Type", headers.contentType)
						w := httptest.NewRecorder()
						allowTestRequest(s, r)
						s.serveHTTP(w, r)
						if w.Code != http.StatusForbidden {
							t.Fatalf("%s: rejected write returned %d, want 403", path, w.Code)
						}
					}
				}
			}
			if s.editTokens != 8 {
				t.Fatalf("rejected writes consumed shared tokens: %v", s.editTokens)
			}
		})
	}
}

func TestAuthorizedWritesRemainRateLimited(t *testing.T) {
	for _, path := range []string{"/api/download/preview", "/api/edit/prepare"} {
		t.Run(path, func(t *testing.T) {
			s := NewService(nil, "")
			s.server = &http.Server{}
			s.downloader, s.editor = &download.Service{}, &mediaedit.Service{}
			s.downloading, s.editing = true, true
			s.csrf, s.editTokens, s.editLast = "test-csrf", 8, time.Now()
			post := func(path, body string) int {
				r := httptest.NewRequest(http.MethodPost, "http://localhost"+path, strings.NewReader(body))
				r.Header.Set("Origin", "http://localhost")
				r.Header.Set("X-StorageBucket-CSRF", s.csrf)
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				allowTestRequest(s, r)
				s.serveHTTP(w, r)
				return w.Code
			}
			body, want := `{"url":"https://example.com/video"}`, http.StatusOK
			if path == "/api/edit/prepare" {
				body, want = "{", http.StatusBadRequest
			}
			for i := 0; i < 8; i++ {
				if code := post(path, body); code != want {
					t.Fatalf("authorized write %d: got %d, want %d", i, code, want)
				}
			}
			if code := post(path, body); code != http.StatusTooManyRequests {
				t.Fatalf("authorized flood returned %d, want 429", code)
			}
			if code := post("/api/edit/resources", "{"); code != http.StatusBadRequest {
				t.Fatalf("resource estimation lost its rate-limit exemption: %d", code)
			}
		})
	}
}
