package webserver

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed web/*
var assets embed.FS

func trustedHost(host string) bool {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.ToLower(name)
	return name == "127.0.0.1" || name == "localhost" || strings.HasSuffix(name, ".ts.net")
}

func (s *Service) serveHTTP(w http.ResponseWriter, r *http.Request) {
	// Add and Stop/Wait share the lifecycle lock; no new database reads after Stop.
	s.mu.Lock()
	if s.server == nil || r.Context().Err() != nil {
		s.mu.Unlock()
		http.Error(w, "서버가 종료됐습니다", 503)
		return
	}
	s.requests.Add(1)
	s.mu.Unlock()
	defer s.requests.Done()
	select {
	case s.capacity <- struct{}{}:
		defer func() { <-s.capacity }()
	default:
		http.Error(w, "요청이 많습니다", 429)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; media-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'; form-action 'self'")
	crossSite := r.Header.Get("Sec-Fetch-Site") == "cross-site"
	navigation := r.URL.Path == "/" && r.Header.Get("Sec-Fetch-Mode") == "navigate" && r.Header.Get("Sec-Fetch-Dest") == "document"
	if !trustedHost(r.Host) || (crossSite && !navigation) {
		http.Error(w, "접근이 허용되지 않습니다", 403)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(parsed.Host, r.Host) || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			http.Error(w, "접근이 허용되지 않습니다", 403)
			return
		}
	}
	if strings.HasPrefix(r.URL.Path, "/api/access/") {
		s.accessAPI(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/edit-assets/") {
		sessionCtx, allowed := s.sessionContext(r)
		if !allowed {
			http.Error(w, "PC의 연결 코드로 인증하세요", http.StatusUnauthorized)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		stop := context.AfterFunc(sessionCtx, cancel)
		defer stop()
		defer cancel()
		r = r.WithContext(ctx)
	}

	editPost := r.Method == http.MethodPost && (r.URL.Path == "/api/albums" || r.URL.Path == "/api/content-filters" || strings.HasPrefix(r.URL.Path, "/api/edit/") || strings.HasPrefix(r.URL.Path, "/api/download/"))
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !editPost {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "읽기 전용 서버입니다", 405)
		return
	}
	if r.ContentLength > 0 && !editPost {
		http.Error(w, "요청 본문은 지원하지 않습니다", 400)
		return
	}
	s.handler.ServeHTTP(w, r)
}

func (s *Service) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/library", s.list)
	s.contentFilterRoutes(mux)
	s.albumRoutes(mux)
	mux.HandleFunc("GET /api/search-suggestions", s.searchSuggestions)
	mux.HandleFunc("GET /api/suggestions", s.filterSuggestions)
	mux.HandleFunc("GET /api/library/{id}", s.detail)
	mux.HandleFunc("GET /api/library/{id}/sequence", s.sequence)
	mux.HandleFunc("GET /api/files/{id}/info", s.fileInfo)
	mux.HandleFunc("GET /api/files/{id}/{variant}", s.content)
	mux.HandleFunc("GET /licenses", s.licenseNotices)
	mux.HandleFunc("GET /licenses.txt", s.licenseText)
	s.editRoutes(mux)
	s.downloadRoutes(mux)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		data, err := assets.ReadFile("web/dist/web.html")
		if err != nil {
			http.Error(w, "웹 화면 빌드가 필요합니다. frontend에서 npm run build를 실행하세요.", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method != "HEAD" {
			_, _ = w.Write(data)
		}
	})
	web, _ := fs.Sub(assets, "web")
	ui, _ := fs.Sub(assets, "web/dist")
	mux.Handle("GET /ui/", http.StripPrefix("/ui/", http.FileServerFS(ui)))
	for _, path := range []string{"/i18n.js", "/translations.js", "/app.js", "/style.css", "/favicon.ico", "/editor.js", "/editor.css", "/search.js", "/search.css", "/licenses.css", "/player.js", "/player.css", "/album-state.js", "/viewer.js", "/viewer-queue.js", "/viewer.css"} {
		mux.Handle("GET "+path, http.FileServerFS(web))
	}
	for _, path := range []string{"/langs/ko.json", "/langs/en.json", "/shortcuts.js", "/output-controls.js"} {
		mux.Handle("GET "+path, http.FileServerFS(web))
	}
	return mux
}

func respond(w http.ResponseWriter, r *http.Request, value any) {
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Second))
	if r.Method != "HEAD" {
		_ = json.NewEncoder(w).Encode(value)
	}
}

func integer(r *http.Request, key string, fallback int) (int, error) {
	text := r.URL.Query().Get(key)
	if text == "" {
		return fallback, nil
	}
	return strconv.Atoi(text)
}

type contextFile struct {
	io.ReadSeeker
	ctx context.Context
}

func (f contextFile) Read(p []byte) (int, error) {
	if err := f.ctx.Err(); err != nil {
		return 0, err
	}
	return f.ReadSeeker.Read(p)
}

type streamWriter struct {
	http.ResponseWriter
	ctx context.Context
}

func (w streamWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	_ = http.NewResponseController(w.ResponseWriter).SetWriteDeadline(time.Now().Add(30 * time.Second))
	return w.ResponseWriter.Write(p)
}

func (s *Service) content(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "파일 ID를 확인하세요", 400)
		return
	}
	file, err := s.library.OpenContent(r.Context(), id, r.PathValue("variant"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.File.Close()
	contentType := contentTypes[strings.ToLower(filepath.Ext(file.Info.Name()))]
	if file.Kind == "audio" && contentType == "video/webm" {
		contentType = "audio/webm"
	}
	if contentType == "" || !strings.HasPrefix(contentType, file.Kind+"/") {
		http.Error(w, "재생을 지원하지 않는 형식입니다", 415)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("ETag", fmt.Sprintf(`"%x-%x"`, file.Info.Size(), file.Info.ModTime().UnixNano()))
	http.ServeContent(streamWriter{w, r.Context()}, r, file.Info.Name(), file.Info.ModTime(), contextFile{file.File, r.Context()})
}

var contentTypes = map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif", ".bmp": "image/bmp", ".tif": "image/tiff", ".tiff": "image/tiff", ".heic": "image/heic", ".heif": "image/heif", ".mp4": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime", ".mkv": "video/x-matroska", ".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".aac": "audio/aac", ".flac": "audio/flac", ".wav": "audio/wav", ".ogg": "audio/ogg", ".opus": "audio/ogg"}
