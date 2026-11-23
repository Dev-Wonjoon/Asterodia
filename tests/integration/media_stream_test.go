package integration_test

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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

func registeredVideo(t *testing.T, size int64) (http.Handler, string, string) {
	t.Helper()
	f := newFixture(t)
	path := filepath.Join(f.root, "range-video.mp4")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(size); err != nil {
		file.Close()
		t.Fatal(err)
	}
	for offset := int64(0); offset < size; offset += 1 << 20 {
		if _, err = file.WriteAt([]byte{byte(offset/(1<<20) + 1)}, offset); err != nil {
			file.Close()
			t.Fatal(err)
		}
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := f.db.Exec("INSERT INTO media(source_kind,custom_title,created_at) VALUES('legacy','range video',1)")
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`INSERT INTO media_file(media_id,file_key,storage_kind,filepath,kind,position,created_at) VALUES(?,'range-video','unverified_legacy',?,'video',0,1)`, id, path); err != nil {
		t.Fatal(err)
	}
	return desktop.MediaMiddleware(f.db, filepath.Join(f.root, "cache", "previews"))(http.NotFoundHandler()), "/media?path=" + url.QueryEscape(path), path
}

func TestLargeVideoResponsesStayBounded(t *testing.T) {
	const chunk = 4 << 20
	const size = 3*chunk + 37
	handler, target, path := registeredVideo(t, size)
	head := httptest.NewRecorder()
	handler.ServeHTTP(head, httptest.NewRequest("HEAD", target, nil))
	if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(size) {
		t.Fatalf("large video HEAD: %d %v", head.Code, head.Header())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, value, ifRange string
		status, length       int
		contentRange         string
	}{
		{"open-ended", "bytes=0-", "", 206, chunk, fmt.Sprintf("bytes 0-%d/%d", chunk-1, size)},
		{"seek", "bytes=4194311-", "", 206, chunk, fmt.Sprintf("bytes 4194311-%d/%d", 4194311+chunk-1, size)},
		{"oversized-closed", fmt.Sprintf("bytes=0-%d", size-1), "", 206, chunk, fmt.Sprintf("bytes 0-%d/%d", chunk-1, size)},
		{"small-closed", "bytes=9-18", "", 206, 10, fmt.Sprintf("bytes 9-18/%d", size)},
		{"suffix", "bytes=-8", "", 206, 8, fmt.Sprintf("bytes %d-%d/%d", size-8, size-1, size)},
		{"oversized-suffix", "bytes=-999999999", "", 206, chunk, fmt.Sprintf("bytes 0-%d/%d", chunk-1, size)},
		{"matching-etag", "bytes=0-", head.Header().Get("ETag"), 206, chunk, fmt.Sprintf("bytes 0-%d/%d", chunk-1, size)},
		{"matching-date", "bytes=0-", info.ModTime().UTC().Format(http.TimeFormat), 206, chunk, fmt.Sprintf("bytes 0-%d/%d", chunk-1, size)},
		{"missing-range", "", "", 400, -1, ""},
		{"stale-etag", "bytes=0-", `"old-video"`, 412, -1, ""},
		{"weak-etag", "bytes=0-", "W/" + head.Header().Get("ETag"), 412, -1, ""},
		{"stale-date", "bytes=0-", info.ModTime().Add(-time.Hour).UTC().Format(http.TimeFormat), 412, -1, ""},
		{"out-of-bounds", fmt.Sprintf("bytes=%d-", size), "", 416, -1, fmt.Sprintf("bytes */%d", size)},
		{"invalid", "bytes=abc-", "", 416, -1, fmt.Sprintf("bytes */%d", size)},
		{"reversed", "bytes=20-10", "", 416, -1, fmt.Sprintf("bytes */%d", size)},
		{"zero-suffix", "bytes=-0", "", 416, -1, fmt.Sprintf("bytes */%d", size)},
		{"multiple", "bytes=0-1,3-5", "", 416, -1, fmt.Sprintf("bytes */%d", size)},
		{"overflow", "bytes=9223372036854775808-", "", 416, -1, fmt.Sprintf("bytes */%d", size)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", target, nil)
			req.Header.Set("Range", tc.value)
			req.Header.Set("If-Range", tc.ifRange)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status || rec.Body.Len() > chunk || tc.length >= 0 && rec.Body.Len() != tc.length || rec.Header().Get("Content-Range") != tc.contentRange {
				t.Fatalf("status=%d bytes=%d headers=%v", rec.Code, rec.Body.Len(), rec.Header())
			}
			if rec.Code == 206 && rec.Header().Get("Content-Length") != strconv.Itoa(rec.Body.Len()) {
				t.Fatal("range length does not match the response body")
			}
			if req.Header.Get("Range") != tc.value {
				t.Fatal("middleware changed the caller's Range header")
			}
		})
	}

	// Follow each returned Content-Range, as a media player does, and verify that
	// capping open-ended requests neither skips nor repeats bytes at chunk seams.
	want := sha256.New()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(want, file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	got := sha256.New()
	for offset := int64(0); offset < size; {
		req := httptest.NewRequest("GET", target, nil)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var first, last, total int64
		if _, err = fmt.Sscanf(rec.Header().Get("Content-Range"), "bytes %d-%d/%d", &first, &last, &total); err != nil || rec.Code != 206 || first != offset || total != size || last-first+1 != int64(rec.Body.Len()) || rec.Body.Len() > chunk {
			t.Fatalf("invalid continuation: offset=%d status=%d headers=%v", offset, rec.Code, rec.Header())
		}
		_, _ = got.Write(rec.Body.Bytes())
		offset = last + 1
	}
	if fmt.Sprintf("%x", got.Sum(nil)) != fmt.Sprintf("%x", want.Sum(nil)) {
		t.Fatal("continued video bytes differ from the original")
	}
}

func TestSmallVideoCanStillLoadWithoutRange(t *testing.T) {
	handler, target, _ := registeredVideo(t, 128)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
	if rec.Code != 200 || rec.Body.Len() != 128 {
		t.Fatalf("small video response: status=%d bytes=%d", rec.Code, rec.Body.Len())
	}
}
