package desktop

import (
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

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
			var allowed bool
			err := db.QueryRowContext(r.Context(), "SELECT EXISTS(SELECT 1 FROM media_file WHERE filepath=? OR thumbnail_path=? OR preview_path=?)", path, path, path).Scan(&allowed)
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
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", "sandbox")
			w.Header().Set("Cache-Control", "private, max-age=60")
			http.ServeFile(w, r, path)
		})
	}
}
