package webserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

func (s *Service) albumRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/albums/access", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		csrf := s.csrf
		s.mu.Unlock()
		respond(w, r, map[string]string{"csrf": csrf})
	})
	mux.HandleFunc("GET /api/albums/{id}/contents", s.exploreAlbum)
	mux.HandleFunc("POST /api/albums", s.saveAlbum)
}

func (s *Service) exploreAlbum(w http.ResponseWriter, r *http.Request) {
	id, e1 := strconv.ParseInt(r.PathValue("id"), 10, 64)
	limit, e2 := integer(r, "limit", 100)
	offset, e3 := integer(r, "offset", 0)
	if e1 != nil || e2 != nil || e3 != nil || id <= 0 || limit < 1 || limit > 100 || offset < 0 {
		http.Error(w, "앨범과 조회 범위를 확인하세요", 400)
		return
	}
	filter := mobileSearchFilter(r)
	filter.Keyword = r.URL.Query().Get("q")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result := s.library.ExploreAlbum(ctx, media.ExploreAlbumRequest{MediaID: id, Limit: limit, Offset: offset, Filter: filter, Recursive: r.URL.Query().Get("recursive") == "1", Sort: r.URL.Query().Get("sort"), Descending: r.URL.Query().Get("descending") == "1"})
	if apiError(w, r, result) {
		return
	}
	page := result.Data.(media.Object)
	entries := []media.Object{}
	for _, entry := range page["data"].([]media.Object) {
		if entry["kind"] == "album" {
			entries = append(entries, media.Object{"kind": "album", "album": publicSummary(entry["album"].(media.Object))})
		} else {
			entries = append(entries, media.Object{"kind": "file", "file": publicFile(entry["file"].(media.Object)), "albumId": entry["albumId"], "title": entry["title"]})
		}
	}
	respond(w, r, media.Object{"album": publicSummary(page["album"].(media.Object)), "breadcrumbs": page["breadcrumbs"], "data": entries, "total": page["total"], "offset": page["offset"], "hasNextPage": page["hasNextPage"], "revisionToken": page["revisionToken"]})
}

func (s *Service) saveAlbum(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	csrf := s.csrf
	s.mu.Unlock()
	origin, err := url.Parse(r.Header.Get("Origin"))
	contentType, _, typeErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || origin.Host != r.Host || (origin.Scheme != "http" && origin.Scheme != "https") || typeErr != nil || contentType != "application/json" || csrf == "" || subtle.ConstantTimeCompare([]byte(csrf), []byte(r.Header.Get("X-StorageBucket-CSRF"))) != 1 {
		http.Error(w, "요청 출처와 앨범 변경 권한을 확인하세요", 403)
		return
	}
	s.mu.Lock()
	now := time.Now()
	s.editTokens = min(8, s.editTokens+now.Sub(s.editLast).Seconds())
	s.editLast = now
	allowed := s.editTokens >= 1
	if allowed {
		s.editTokens--
	}
	s.mu.Unlock()
	if !allowed {
		http.Error(w, "잠시 후 다시 요청하세요", 429)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var req *media.AlbumRequest
	if decoder.Decode(&req) != nil || req == nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "앨범 요청 형식과 크기를 확인하세요", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result := s.library.SaveAlbum(ctx, *req)
	if result.OK {
		result.Data = publicSummary(result.Data.(media.Object))
	}
	respond(w, r, result)
}
