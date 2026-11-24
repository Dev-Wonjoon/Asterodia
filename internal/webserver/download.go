package webserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

func (s *Service) downloadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/download/status", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		enabled, csrf := s.downloading && s.downloader != nil, s.csrf
		s.mu.Unlock()
		if !enabled {
			csrf = ""
		}
		respond(w, r, map[string]any{"enabled": enabled, "csrf": csrf})
	})
	mux.HandleFunc("/api/download/", s.downloadAPI)
}

func (s *Service) downloadAPI(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	enabled, service, csrf := s.downloading, s.downloader, s.csrf
	limited := false
	if r.Method == http.MethodPost {
		elapsed := time.Since(s.editLast).Seconds()
		s.editLast = time.Now()
		s.editTokens = min(8, s.editTokens+elapsed)
		limited = s.editTokens < 1
		if !limited {
			s.editTokens--
		}
	}
	s.mu.Unlock()
	if !enabled || service == nil {
		http.Error(w, "PC에서 모바일 다운로드를 허용하세요", 403)
		return
	}
	if limited {
		http.Error(w, "잠시 후 다시 요청하세요", 429)
		return
	}
	var result api.Result
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		switch r.URL.Path {
		case "/api/download/list":
			offset, err := integer(r, "offset", 0)
			if err != nil || offset < 0 {
				http.Error(w, "목록 위치를 확인하세요", 400)
				return
			}
			result = service.List(r.Context(), download.ListRequest{Limit: 50, Offset: offset})
		case "/api/download/job":
			result = service.Get(r.Context(), download.JobRequest{JobID: r.URL.Query().Get("jobId")})
		default:
			http.NotFound(w, r)
			return
		}
	} else if r.Method == http.MethodPost {
		origin, err := url.Parse(r.Header.Get("Origin"))
		contentType, _, ce := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || origin.Host != r.Host || (origin.Scheme != "http" && origin.Scheme != "https") || ce != nil || contentType != "application/json" || csrf == "" || subtle.ConstantTimeCompare([]byte(csrf), []byte(r.Header.Get("X-StorageBucket-CSRF"))) != 1 {
			http.Error(w, "요청 출처와 다운로드 권한을 확인하세요", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
		defer r.Body.Close()
		decode := func(value any) bool {
			d := json.NewDecoder(r.Body)
			d.DisallowUnknownFields()
			if d.Decode(value) != nil {
				http.Error(w, "요청 형식과 크기를 확인하세요", 400)
				return false
			}
			var extra any
			if d.Decode(&extra) != io.EOF {
				http.Error(w, "요청 본문은 하나여야 합니다", 400)
				return false
			}
			return true
		}
		switch r.URL.Path {
		case "/api/download/enqueue":
			// Remote downloads can create media; replacement stays a PC operation.
			var req struct {
				URL     string         `json:"url"`
				Options engine.Options `json:"options"`
			}
			if !decode(&req) {
				return
			}
			result = service.Enqueue(r.Context(), download.EnqueueRequest{URL: req.URL, Options: req.Options, Mode: "create"})
			if result.OK {
				job := result.Data.(download.Job)
				result = s.launchDownload(service, job, "manual_initial")
			}
		case "/api/download/preview":
			var req struct {
				URL     string         `json:"url"`
				Options engine.Options `json:"options"`
			}
			if !decode(&req) {
				return
			}
			kind, err := service.PreviewEngine(req.URL, req.Options)
			if err != nil {
				result = api.Failure("INVALID_ARGUMENT", err.Error())
			} else {
				result = api.Success(kind)
			}
		case "/api/download/retry", "/api/download/cancel", "/api/download/cleanup":
			var req download.JobRequest
			if !decode(&req) {
				return
			}
			switch r.URL.Path {
			case "/api/download/cancel":
				result = service.Cancel(r.Context(), req)
			case "/api/download/cleanup":
				result = service.Cleanup(r.Context(), req)
			default:
				result = service.Get(r.Context(), req)
				if result.OK {
					job := result.Data.(download.Job)
					trigger := "manual_restart"
					if job.State == "failed" {
						trigger = "manual_retry"
					}
					if job.HistoryOnly || job.CleanupRequired || (job.State != "failed" && job.State != "cancelled" && job.State != "interrupted") {
						result = api.Failure("INVALID_ARGUMENT", "실패하거나 중단된 다운로드만 다시 실행할 수 있습니다")
					} else {
						result = s.launchDownload(service, job, trigger)
					}
				}
			}
		default:
			http.NotFound(w, r)
			return
		}
	} else {
		w.Header().Set("Allow", "GET, HEAD, POST")
		w.WriteHeader(405)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if !result.OK {
		w.WriteHeader(400)
	}
	respond(w, r, publicDownloadResult(result))
}

func (s *Service) launchDownload(service *download.Service, job download.Job, trigger string) api.Result {
	if _, loaded := s.downloadRuns.LoadOrStore(job.ID, true); loaded {
		return api.Failure("ALREADY_PROCESSING", "이미 실행 중인 다운로드입니다")
	}
	go func() {
		defer s.downloadRuns.Delete(job.ID)
		// The PC download service owns cancellation and shutdown. Browser request
		// completion or closing the web server must not cancel an accepted job.
		service.Run(context.Background(), download.RunRequest{JobID: job.ID, Trigger: trigger})
	}()
	return api.Success(job)
}

func publicDownloadResult(result api.Result) api.Result {
	clean := func(job download.Job) download.Job { job.ThumbnailPath = nil; return job }
	switch data := result.Data.(type) {
	case download.Job:
		result.Data = clean(data)
	case map[string]any:
		copyData := make(map[string]any, len(data))
		for key, value := range data {
			copyData[key] = value
		}
		for _, key := range []string{"data", "activeJobs"} {
			if jobs, ok := data[key].([]download.Job); ok {
				copyJobs := make([]download.Job, len(jobs))
				for i, job := range jobs {
					copyJobs[i] = clean(job)
				}
				copyData[key] = copyJobs
			}
		}
		result.Data = copyData
	}
	return result
}
