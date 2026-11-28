package mediaedit

import (
	"fmt"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//wails:ignore
func (s *Service) Assets() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/edit-assets/"), "/")
		if len(parts) < 2 || !validID(parts[1]) {
			http.NotFound(w, r)
			return
		}
		var file *os.File
		var err error
		name := ""
		contentType := "image/png"
		if len(parts) == 3 && parts[0] == "sessions" {
			n, e := strconv.Atoi(parts[2])
			if e != nil || n < 0 {
				http.NotFound(w, r)
				return
			}
			v, e := s.store.session(r.Context(), parts[1])
			if e != nil || v.State != "ready" || n >= v.FrameCount {
				http.NotFound(w, r)
				return
			}
			root, e := os.OpenRoot(s.cache)
			if e != nil {
				http.NotFound(w, r)
				return
			}
			name = fmt.Sprintf("%04d.png", n)
			if r.URL.Query().Get("thumbnail") == "1" {
				name = fmt.Sprintf("t%04d.jpg", n)
				contentType = "image/jpeg"
			}
			if v.RenderVersion >= 2 {
				name = fmt.Sprintf("%012d.jpg", n)
				if r.URL.Query().Get("thumbnail") == "1" {
					name = fmt.Sprintf("t%04d.jpg", n)
				}
				contentType = "image/jpeg"
				name = filepath.Join(pageName(n/framePageSize*framePageSize), name)
			}
			file, err = root.Open(filepath.Join(v.ID, name))
			root.Close()
		} else if len(parts) == 2 && parts[0] == "jobs" {
			j, e := s.store.job(r.Context(), parts[1])
			if e != nil || j.State != "completed" || (!j.Preview && j.ResultFileID == nil) {
				http.NotFound(w, r)
				return
			}
			var path string
			if j.Preview {
				ext := ".gif"
				if j.Options.Kind == "webp" {
					ext = ".webp"
				}
				root, e := os.OpenRoot(s.cache)
				if e != nil {
					http.NotFound(w, r)
					return
				}
				path = filepath.Join("previews", j.ID+ext)
				file, err = root.Open(path)
				root.Close()
			} else {
				err = s.store.db.QueryRowContext(r.Context(), "SELECT filepath FROM media_file WHERE id=? AND file_uuid=(SELECT result_uuid FROM media_edit_job WHERE id=?)", *j.ResultFileID, j.ID).Scan(&path)
				if err == nil {
					var opened *media.Content
					opened, err = media.NewService(s.store.db).OpenContent(r.Context(), *j.ResultFileID, "content")
					if err == nil {
						file = opened.File
					}
				}
			}
			name = filepath.Base(path)
			contentType = "image/gif"
			if j.Options.Kind == "trim" {
				contentType = "video/mp4"
			} else if j.Options.Kind == "webp" {
				contentType = "image/webp"
			}
			if j.Options.Batch != nil && j.Options.Batch.Mode == "convert" {
				contentType = mime.TypeByExtension(filepath.Ext(path))
				if contentType == "" {
					contentType = "application/octet-stream"
				}
			}
		} else {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-store")
		disposition := "inline"
		if r.URL.Query().Get("download") == "1" {
			disposition = "attachment"
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
		http.ServeContent(w, r, name, info.ModTime(), file)
	})
}
