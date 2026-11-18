package integration_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/desktop"
)

func TestRegisteredMediaRangeAndCacheValidation(t *testing.T) {
	f := newFixture(t)
	id := f.run(t, f.enqueue(t))
	path := publishedPath(t, f, id)
	handler := desktop.MediaMiddleware(f.db, filepath.Join(f.root, "cache", "previews"))(http.NotFoundHandler())
	request := func(method, rangeValue, tag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/media?path="+url.QueryEscape(path), nil)
		if rangeValue != "" {
			req.Header.Set("Range", rangeValue)
		}
		if tag != "" {
			req.Header.Set("If-None-Match", tag)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	partial := request("GET", "bytes=0-7", "")
	if partial.Code != 206 || partial.Body.Len() != 8 || partial.Header().Get("Content-Range") == "" {
		t.Fatalf("재생 범위 요청: %d %v", partial.Code, partial.Header())
	}
	tag := partial.Header().Get("ETag")
	if tag == "" || request("HEAD", "", tag).Code != 304 {
		t.Fatal("캐시 검증 실패")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// A source update within the same second still needs a different validator.
	if err = os.Chtimes(path, info.ModTime(), info.ModTime().Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	changed := request("HEAD", "", tag)
	if changed.Code != 200 || changed.Header().Get("ETag") == tag {
		t.Fatal("원본 변경 후 이전 캐시를 재사용했습니다")
	}
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, httptest.NewRequest("GET", "/media?path="+url.QueryEscape(filepath.Join(f.root, "library.db")), nil))
	if denied.Code != 404 {
		t.Fatal("미등록 파일 제공")
	}
}
