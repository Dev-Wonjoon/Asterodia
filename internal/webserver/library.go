package webserver

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

func publicSummary(row map[string]any) map[string]any {
	result := map[string]any{"id": row["id"], "title": row["title"], "isUserAlbum": row["isUserAlbum"], "fileCount": row["fileCount"], "createdAt": row["createdAt"], "cover": nil}
	if cover, ok := row["cover"].(map[string]any); ok {
		result["cover"] = map[string]any{"fileId": cover["fileId"], "kind": cover["kind"]}
	}
	if file, ok := row["galleryFile"].(map[string]any); ok {
		result["galleryFile"] = map[string]any{"id": file["id"], "title": file["title"], "kind": file["kind"]}
		result["galleryFileOffset"] = row["galleryFileOffset"]
	}
	return result
}

func (s *Service) sequence(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "앨범을 확인하세요", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result := s.library.Sequence(ctx, media.SequenceRequest{MediaID: id})
	if apiError(w, r, result) {
		return
	}
	respond(w, r, result.Data)
}

func (s *Service) fileInfo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "파일을 확인하세요", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result := s.library.ReadFile(ctx, media.FileRequest{FileID: id})
	if apiError(w, r, result) {
		return
	}
	file := result.Data.(media.Object)
	respond(w, r, map[string]any{"id": file["id"], "title": file["title"], "kind": file["kind"], "duration": file["duration"]})
}

func apiError(w http.ResponseWriter, r *http.Request, result api.Result) bool {
	if result.OK {
		return false
	}
	status := 500
	if result.Error.Code == "INVALID_ARGUMENT" {
		status = 400
	}
	if result.Error.Code == "NOT_FOUND" {
		status = 404
	}
	http.Error(w, "라이브러리를 읽지 못했습니다", status)
	return true
}

func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	limit, e1 := integer(r, "limit", 24)
	offset, e2 := integer(r, "offset", 0)
	q := r.URL.Query().Get("q")
	if e1 != nil || e2 != nil || limit < 1 || limit > 100 || offset < 0 || len(q) > 1000 {
		http.Error(w, "조회 범위를 확인하세요", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	filter := mobileSearchFilter(r)
	filter.Keyword = q
	expanded := []int64{}
	if raw := r.URL.Query().Get("expanded"); raw != "" {
		if len(raw) > 100000 {
			http.Error(w, "조회 범위를 확인하세요", 400)
			return
		}
		for _, value := range strings.Split(raw, ",") {
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				http.Error(w, "앨범을 확인하세요", 400)
				return
			}
			expanded = append(expanded, id)
		}
	}
	anchor, anchorErr := integer(r, "anchor", 0)
	if anchorErr != nil || anchor < 0 {
		http.Error(w, "앨범을 확인하세요", 400)
		return
	}
	result := s.library.List(ctx, media.ListRequest{Limit: limit, Offset: offset, Filter: filter, Gallery: r.URL.Query().Get("gallery") == "1", ExpandedMediaIDs: expanded, AnchorMediaID: int64(anchor)})
	if apiError(w, r, result) {
		return
	}
	page := result.Data.(map[string]any)
	items := []map[string]any{}
	for _, row := range page["data"].([]media.Object) {
		items = append(items, publicSummary(row))
	}
	respond(w, r, map[string]any{"data": items, "total": page["total"], "hasNextPage": page["hasNextPage"], "offset": page["offset"]})
}

func mobileSearchFilter(r *http.Request) media.Filter {
	query := r.URL.Query()
	return media.Filter{Tags: query["tag"], Authors: query["author"], Platforms: query["platform"], TagMode: "and"}
}

func (s *Service) searchSuggestions(w http.ResponseWriter, r *http.Request) {
	limit, e1 := integer(r, "limit", 10)
	offset, e2 := integer(r, "offset", 0)
	if e1 != nil || e2 != nil || limit < 1 || limit > 50 || offset < 0 {
		http.Error(w, "조회 범위를 확인하세요", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result := s.library.SearchSuggest(ctx, media.SearchSuggestRequest{Text: r.URL.Query().Get("q"), Limit: limit, Offset: offset, Filter: mobileSearchFilter(r)})
	if apiError(w, r, result) {
		return
	}
	respond(w, r, result.Data)
}

func (s *Service) detail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	offset, e2 := integer(r, "fileOffset", 0)
	if err != nil || e2 != nil || id <= 0 || offset < 0 {
		http.Error(w, "조회 범위를 확인하세요", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result := s.library.Get(ctx, media.GetRequest{MediaID: id, FileOffset: offset, FileLimit: 24})
	if apiError(w, r, result) {
		return
	}
	row := result.Data.(map[string]any)
	detail := publicSummary(row)
	page := row["files"].(media.Object)
	files := []map[string]any{}
	for _, file := range page["data"].([]media.Object) {
		files = append(files, map[string]any{"id": file["id"], "title": file["title"], "kind": file["kind"], "width": file["width"], "height": file["height"], "duration": file["duration"], "detectedFormat": file["detectedFormat"]})
	}
	detail["files"] = map[string]any{"data": files, "total": page["total"], "hasNextPage": page["hasNextPage"]}
	respond(w, r, detail)
}
