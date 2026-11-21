package webserver

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/mediaedit"
)

func (s *Service) editRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/edit/status", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		enabled, csrf := s.editing && s.editor != nil, s.csrf
		s.mu.Unlock()
		if !enabled {
			csrf = ""
		}
		respond(w, r, map[string]any{"enabled": enabled, "csrf": csrf})
	})
	mux.HandleFunc("/api/edit/", s.editAPI)
	mux.HandleFunc("GET /edit-assets/", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		enabled, editor := s.editing, s.editor
		s.mu.Unlock()
		if !enabled || editor == nil {
			http.Error(w, "모바일 편집이 꺼져 있습니다", 403)
			return
		}
		editor.Assets().ServeHTTP(streamWriter{w}, r)
	})
}

func (s *Service) editAPI(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	enabled, editor, csrf := s.editing, s.editor, s.csrf
	if r.Method == "POST" && r.URL.Path != "/api/edit/resources" {
		elapsed := time.Since(s.editLast).Seconds()
		s.editLast = time.Now()
		s.editTokens = min(8, s.editTokens+elapsed)
		if s.editTokens < 1 {
			s.mu.Unlock()
			http.Error(w, "잠시 후 다시 요청하세요", 429)
			return
		}
		s.editTokens--
	}
	s.mu.Unlock()
	if !enabled || editor == nil {
		http.Error(w, "PC에서 모바일 편집을 허용하세요", 403)
		return
	}
	var result api.Result
	if r.Method == "GET" || r.Method == "HEAD" {
		switch r.URL.Path {
		case "/api/edit/list":
			result = editor.List(r.Context())
		case "/api/edit/session":
			result = editor.GetSession(r.Context(), mediaedit.Request{ID: r.URL.Query().Get("id")})
		case "/api/edit/job":
			result = editor.GetJob(r.Context(), mediaedit.Request{ID: r.URL.Query().Get("id")})
		case "/api/edit/frames":
			offset, e := integer(r, "offset", 0)
			if e != nil {
				http.Error(w, "프레임 위치를 확인하세요", 400)
				return
			}
			result = editor.FrameStatus(r.Context(), mediaedit.FramesRequest{ID: r.URL.Query().Get("id"), Offset: offset})
		default:
			http.NotFound(w, r)
			return
		}
	} else if r.Method == "POST" {
		origin, e := url.Parse(r.Header.Get("Origin"))
		contentType, _, ce := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if e != nil || origin.Host != r.Host || (origin.Scheme != "https" && origin.Scheme != "http") || ce != nil || contentType != "application/json" || csrf == "" || subtle.ConstantTimeCompare([]byte(csrf), []byte(r.Header.Get("X-StorageBucket-CSRF"))) != 1 {
			http.Error(w, "요청 출처와 편집 권한을 확인하세요", 403)
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
		case "/api/edit/frames":
			var req mediaedit.FramesRequest
			if !decode(&req) {
				return
			}
			result = editor.Frames(r.Context(), req)
		case "/api/edit/selection":
			var req mediaedit.SelectionRequest
			if !decode(&req) {
				return
			}
			result = editor.Selection(r.Context(), req)
		case "/api/edit/resources":
			var req mediaedit.CreateRequest
			if !decode(&req) {
				return
			}
			result = editor.Resources(r.Context(), req)
		case "/api/edit/prepare":
			var req mediaedit.PrepareRequest
			if !decode(&req) {
				return
			}
			result = editor.Prepare(r.Context(), req)
		case "/api/edit/create":
			var req mediaedit.CreateRequest
			if !decode(&req) {
				return
			}
			result = editor.Create(r.Context(), req)
		case "/api/edit/cancel":
			var req mediaedit.Request
			if !decode(&req) {
				return
			}
			result = editor.Cancel(r.Context(), req)
		case "/api/edit/release":
			var req mediaedit.Request
			if !decode(&req) {
				return
			}
			result = editor.Release(r.Context(), req)
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
		code := 400
		if result.Error.Code == "NOT_FOUND" {
			code = 404
		}
		if result.Error.Code == "EDIT_FAILED" {
			code = 500
		}
		w.WriteHeader(code)
	}
	respond(w, r, result)
}
