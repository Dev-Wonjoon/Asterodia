package download_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/download"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

type extractorTransport func(*http.Request) (*http.Response, error)

func (f extractorTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRunnerThreadsFreezeAndBoundTransfer(t *testing.T) {
	for _, mode := range []string{"success", "freeze-fails", "transfer-fails"} {
		t.Run(mode, func(t *testing.T) {
			const address = "https://www.threads.com/@owner/post/Fixture_123?source=user"
			root := downloadTestRoot(t)
			db := openStoreTestDB(t)
			store := download.NewStore(db)
			job, err := store.CreateInitialAttempt(t.Context(), download.InitialAttemptInput{OriginalURL: address, StorageRoot: root})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "freeze-fails" {
				execStoreTestSQL(t, db, `CREATE TRIGGER reject_threads BEFORE INSERT ON download_target WHEN NEW.position=1 BEGIN SELECT RAISE(ABORT,'threads fixture'); END`)
			}
			post := map[string]any{"pk": "123", "code": "Fixture_123", "media_type": 8, "carousel_media_count": 2,
				"user": map[string]string{"pk": "789", "username": "owner"}, "carousel_media": []any{
					map[string]any{"pk": "456", "media_type": 1, "image_versions2": map[string]any{"candidates": []any{map[string]string{"url": "https://scontent.cdninstagram.com/photo.png?token=one"}}}},
					map[string]any{"pk": "457", "media_type": 2, "video_versions": []any{map[string]string{"url": "https://video.fbcdn.net/video.mp4?token=two"}}},
				}}
			raw, _ := json.Marshal(map[string]any{"thread_items": []any{map[string]any{"post": post}}})
			page := []byte(`<script type="application/json">` + string(raw) + `</script>`)
			var photo bytes.Buffer
			if err := png.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
				t.Fatal(err)
			}
			video := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}
			pageCalls, mediaCalls := 0, 0
			client := &http.Client{Transport: extractorTransport(func(r *http.Request) (*http.Response, error) {
				payload, status := page, 200
				if r.URL.Hostname() == "www.threads.com" {
					pageCalls++
				} else {
					mediaCalls++
					var frozen int
					if err := db.QueryRow(`SELECT count(*) FROM download_target WHERE attempt_id=? AND state='pending' AND json_extract(metadata_json,'$.platformName')='Threads'`, job.AttemptID).Scan(&frozen); err != nil || frozen != 2 {
						t.Errorf("transfer before complete freeze: %d %v", frozen, err)
					}
					payload = photo.Bytes()
					if r.URL.Hostname() == "video.fbcdn.net" {
						payload = video
						if mode == "transfer-fails" {
							status = 403
						}
					}
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(payload)), ContentLength: int64(len(payload)), Request: r}, nil
			})}
			backend := galleryDLDiscoverFunc(func(context.Context, string, string) (engine.GalleryDLDiscovery, error) {
				t.Error("successful direct plan fell back to CLI")
				return engine.GalleryDLDiscovery{}, engine.ErrUnsupportedURL
			})
			runner := download.Runner{Storage: download.Storage{Root: root}, Extractors: engine.DefaultExtractors(engine.ExtractorDependencies{HTTPClient: client, GalleryDL: backend})}
			attempt, err := runner.DiscoverAttempt(t.Context(), job.JobID, job.AttemptID, address, engine.Options{})
			if err != nil || attempt.Extractor != "threads" {
				t.Fatal(attempt, err)
			}
			// The discovered plan owns its executor independently of the caller.
			runner.Extractors, runner.Ytdlp, runner.GalleryDL = nil, nil, nil
			files, err := runner.FreezeAndStageAttempt(t.Context(), store, attempt)
			if mode == "freeze-fails" {
				if err == nil || mediaCalls != 0 {
					t.Fatal("transfer ran after failed freeze", err, mediaCalls)
				}
				var count int
				if err := db.QueryRow(`SELECT count(*) FROM download_target WHERE attempt_id=?`, job.AttemptID).Scan(&count); err != nil || count != 0 {
					t.Fatal("partial target freeze", count, err)
				}
				return
			}
			if mode == "transfer-fails" {
				entries, readErr := os.ReadDir(attempt.StageDir())
				if !errors.Is(err, engine.ErrAccessDenied) || len(files) != 0 || readErr != nil || len(entries) != 0 || pageCalls != 1 || mediaCalls != 2 {
					t.Fatal("failed plan was retried or left partial files", files, err, readErr, pageCalls, mediaCalls)
				}
				return
			}
			if err != nil || len(files) != 2 || pageCalls != 1 || mediaCalls != 2 {
				t.Fatal(files, err, pageCalls, mediaCalls)
			}
			var keys, sources string
			if err := db.QueryRow(`SELECT group_concat(target_key,'|'),group_concat(source_url,'|') FROM (SELECT target_key,source_url FROM download_target WHERE attempt_id=? ORDER BY position)`, job.AttemptID).Scan(&keys, &sources); err != nil || keys != "threads:123:456|threads:123:457" || sources != address+"|"+address {
				t.Fatal(keys, sources, err)
			}
		})
	}
}

func TestRunnerGenericKeepsDiscoveredBackend(t *testing.T) {
	runner := download.Runner{Storage: download.Storage{Root: downloadTestRoot(t)}, Ytdlp: stagingYtdlp{
		ytdlpDiscoverFunc: func(context.Context, string, string) (json.RawMessage, error) {
			return json.RawMessage(`{"id":"one","format_id":"http","extractor_key":"Generic","webpage_url":"https://example.invalid/page","url":"https://cdn.invalid/one.mp4","ext":"mp4","vcodec":"h264","acodec":"aac"}`), nil
		},
		save: func(target engine.YtdlpTarget, dir string) (string, error) {
			path := filepath.Join(dir, "media.mp4")
			return path, os.WriteFile(path, []byte("bound backend "+target.ID), 0600)
		},
	}}
	attempt, err := runner.DiscoverAttempt(t.Context(), "bound-backend", "attempt", "https://example.invalid/page", engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	runner.Ytdlp = nil
	files, err := runner.StageAttempt(t.Context(), attempt)
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	raw, err := os.ReadFile(files[0].Path)
	if err != nil || !strings.Contains(string(raw), "bound backend one") {
		t.Fatal(string(raw), err)
	}
}
