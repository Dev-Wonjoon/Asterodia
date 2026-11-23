package desktop

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The Windows WebView response writer buffers the entire response before it
// passes it to WebView2. Bound each video response, even for bytes=0- requests.
const maxVideoResponseBytes int64 = 4 << 20

// Only registered library paths (or content-addressed preview cache entries) can
// be read by the WebView. Arbitrary absolute file paths are never served.
func MediaMiddleware(db *sql.DB, previewCache string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/media" {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method != "GET" && r.Method != "HEAD" {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			path := r.URL.Query().Get("path")
			if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
				http.NotFound(w, r)
				return
			}
			path = filepath.Clean(path)
			var allowed, video bool
			err := db.QueryRowContext(r.Context(), `SELECT
                EXISTS(SELECT 1 FROM media_file WHERE filepath=? OR thumbnail_path=? OR preview_path=?),
                EXISTS(SELECT 1 FROM media_file WHERE filepath=? AND kind='video')`, path, path, path, path).Scan(&allowed, &video)
			if err != nil {
				http.Error(w, "미디어 등록 조회 실패", 500)
				return
			}
			if !allowed {
				rel, e := filepath.Rel(previewCache, path)
				allowed = e == nil && filepath.Base(rel) == rel && regexp.MustCompile(`^[0-9a-f]{64}\.jpg$`).MatchString(rel)
			}
			if !allowed {
				http.NotFound(w, r)
				return
			}
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() {
				http.NotFound(w, r)
				return
			}
			file, err := os.Open(path)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer file.Close()
			info, err = file.Stat()
			if err != nil || !info.Mode().IsRegular() {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", "sandbox")
			// External originals can change at the same path after a rescan. Use
			// nanosecond validators instead of replaying a cached old file for 60s.
			w.Header().Set("Cache-Control", "private, no-cache")
			etag := fmt.Sprintf(`"%x-%x"`, info.Size(), info.ModTime().UnixNano())
			w.Header().Set("ETag", etag)
			if video && r.Method == http.MethodGet && info.Size() > maxVideoResponseBytes {
				w.Header().Set("Accept-Ranges", "bytes")
				// Never turn a missing/invalid Range or stale If-Range into an
				// unbounded 200 response. Native video elements use byte ranges.
				if r.Header.Get("Range") == "" {
					http.Error(w, "큰 영상은 범위 요청으로 읽어야 합니다.", http.StatusBadRequest)
					return
				}
				if value := r.Header.Get("If-Range"); value != "" && value != etag {
					date, err := http.ParseTime(value)
					if err != nil || date.Unix() != info.ModTime().Unix() {
						http.Error(w, "원본 영상이 변경되었습니다. 다시 열어 주세요.", http.StatusPreconditionFailed)
						return
					}
				}
				value, ok := boundedVideoRange(r.Header.Get("Range"), info.Size())
				if !ok {
					w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", info.Size()))
					http.Error(w, "영상 범위 요청을 확인하세요.", http.StatusRequestedRangeNotSatisfiable)
					return
				}
				r = r.Clone(r.Context())
				r.Header.Set("Range", value)
			}
			// Pin the size used for validators and range bounds, even if an
			// external original grows after this request opened it.
			http.ServeContent(w, r, info.Name(), info.ModTime(), io.NewSectionReader(file, 0, info.Size()))
		})
	}
}

func boundedVideoRange(value string, size int64) (string, bool) {
	if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") || size <= 0 {
		return "", false
	}
	first, last, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(value, "bytes=")), "-")
	if !ok {
		return "", false
	}
	first, last = strings.TrimSpace(first), strings.TrimSpace(last)
	var start int64
	end := size - 1
	if first == "" {
		suffix, err := strconv.ParseInt(last, 10, 64)
		if err != nil || suffix <= 0 {
			return "", false
		}
		start = size - min(suffix, size)
	} else {
		var err error
		start, err = strconv.ParseInt(first, 10, 64)
		if err != nil || start < 0 || start >= size {
			return "", false
		}
		if last != "" {
			end, err = strconv.ParseInt(last, 10, 64)
			if err != nil || end < start {
				return "", false
			}
			end = min(end, size-1)
		}
	}
	end = min(end, start+min(maxVideoResponseBytes, size-start)-1)
	return fmt.Sprintf("bytes=%d-%d", start, end), true
}
