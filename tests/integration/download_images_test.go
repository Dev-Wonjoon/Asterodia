package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
	"github.com/Dev-Wonjoon/Asterodia/internal/download"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/thumbnail"
)

type imageInspectFunc func(context.Context, string, string) (engine.LocalMetadata, error)

func (f imageInspectFunc) Inspect(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
	return f(ctx, path, kind)
}

func downloadImageSource(t *testing.T, f *fixture, data []byte, extension string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	t.Cleanup(server.Close)
	f.gallery = func(context.Context, string, string) (engine.GalleryDLDiscovery, error) {
		meta := map[string]any{"category": "pinterest", "post_id": "123456", "filename": "asset", "extension": extension, "title": "원본 이미지", "user_id": "image-owner"}
		raw, _ := json.Marshal([][]any{{2, meta}, {3, server.URL + "/image." + extension, meta}})
		return engine.ParseGalleryDLDiscovery(raw)
	}
}

func assertImageDownloadRejected(t *testing.T, f *fixture, job download.Job, result api.Result, code string) {
	t.Helper()
	errorCode(t, result, code)
	if queryInt(t, f.db, "SELECT count(*) FROM media") != 0 || queryInt(t, f.db, "SELECT count(*) FROM media_file") != 0 || queryInt(t, f.db, "SELECT cleanup_required FROM download_queue WHERE id=?", job.ID) != 0 {
		t.Fatal("검증 실패 후 부분 등록 또는 정리 작업이 남았습니다")
	}
	if err := filepath.WalkDir(filepath.Join(f.root, "downloads"), func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			t.Errorf("검증 실패 후 파일이 남았습니다: %s", path)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assertFK(t, f.db)
}

func TestDownloadNativeImageVerification(t *testing.T) {
	var pngData, jpegData bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	if err := png.Encode(&pngData, img); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&jpegData, img, nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, ext string
		data      []byte
		failure   bool
	}{
		{"png", "png", pngData.Bytes(), false},
		{"jpeg-alias", "jpg", jpegData.Bytes(), false},
		{"jpeg", "jpeg", jpegData.Bytes(), false},
		{"wrong-extension", "jpg", pngData.Bytes(), true},
		{"truncated-png", "png", pngData.Bytes()[:33], true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			downloadImageSource(t, f, tc.data, tc.ext)
			// JPEG/PNG must remain usable without ImageMagick, even when configured.
			f.service.ConfigureImages(imageInspectFunc(func(context.Context, string, string) (engine.LocalMetadata, error) {
				t.Error("기본 디코더 이미지에서 외부 검사기를 호출했습니다")
				return engine.LocalMetadata{}, errors.New("호출하면 안 됩니다")
			}))
			job := f.enqueue(t)
			result := f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"})
			if tc.failure {
				assertImageDownloadRejected(t, f, job, result, "DOWNLOAD_ENGINE_FAILED")
			} else {
				success(t, result)
				if queryInt(t, f.db, "SELECT count(*) FROM media_file WHERE width=8 AND height=6") != 1 {
					t.Fatal("기본 이미지 크기를 등록하지 못했습니다")
				}
			}
		})
	}
}

func TestDownloadImageInspectionFailures(t *testing.T) {
	for _, mode := range []string{"unconfigured", "missing-engine", "invalid-data", "wrong-format", "wrong-kind", "missing-size", "oversized", "source-changed", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			downloadImageSource(t, f, []byte("not a native image"), "webp")
			if mode == "missing-engine" {
				f.service.ConfigureImages(engine.LocalInspector{ImageMagick: filepath.Join(f.root, "missing.exe"), Cache: filepath.Join(f.root, "cache")})
			} else if mode != "unconfigured" {
				f.service.ConfigureImages(imageInspectFunc(func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
					w, h := 8, 6
					meta := engine.LocalMetadata{Kind: "image", Format: "webp", Width: &w, Height: &h}
					switch mode {
					case "invalid-data":
						return meta, errors.New("잘못된 이미지")
					case "wrong-format":
						meta.Format = "png"
					case "wrong-kind":
						meta.Kind = "video"
					case "missing-size":
						meta.Width = nil
					case "oversized":
						w = 20001
					case "source-changed":
						if err := os.WriteFile(path, []byte("changed bytes"), 0600); err != nil {
							return meta, err
						}
					case "timeout":
						return meta, context.DeadlineExceeded
					}
					return meta, nil
				}))
			}
			job := f.enqueue(t)
			result := f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"})
			code := "DOWNLOAD_ENGINE_FAILED"
			if mode == "timeout" {
				code = "CANCELLED"
			}
			assertImageDownloadRejected(t, f, job, result, code)
			if (mode == "unconfigured" || mode == "missing-engine") && !strings.Contains(result.Error.Message, "ImageMagick을 설치") {
				t.Fatal("설치 안내 누락", result.Error)
			}
		})
	}
}

func TestDownloadImageCommitCancellationBoundary(t *testing.T) {
	f := newFixture(t)
	downloadImageSource(t, f, []byte("image inspector fixture"), "webp")
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	f.service.ConfigureImages(imageInspectFunc(func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		w, h := 8, 6
		return engine.LocalMetadata{Kind: "image", Format: "webp", Width: &w, Height: &h}, nil
	}))
	job := f.enqueue(t)
	finished := make(chan api.Result, 1)
	go func() {
		finished <- f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("이미지 검증이 시작되지 않았습니다")
	}
	errorCode(t, f.service.Cancel(t.Context(), download.JobRequest{JobID: job.ID}), "CANNOT_CANCEL_COMMITTING")
	release <- struct{}{}
	select {
	case result := <-finished:
		success(t, result)
	case <-time.After(5 * time.Second):
		t.Fatal("이미지 검사 후 등록이 끝나지 않았습니다")
	}
	assertFK(t, f.db)
}

func TestDownloadExtendedImageFormats(t *testing.T) {
	magick := os.Getenv("ASTERODIA_TEST_IMAGE_EXE")
	if !filepath.IsAbs(magick) {
		t.Skip("ImageMagick의 절대 경로를 지정하세요")
	}
	for _, ext := range []string{"webp", "avif", "heic", "bmp", "tiff", "gif"} {
		t.Run(ext, func(t *testing.T) {
			f := newFixture(t)
			var data []byte
			var err error
			if ext == "heic" {
				path := os.Getenv("ASTERODIA_TEST_HEIC_FILE")
				if path == "" {
					t.Skip("읽기 검증용 HEIC 파일을 지정하세요")
				}
				data, err = os.ReadFile(path)
			} else {
				path := filepath.Join(f.root, "source."+ext)
				result, e := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: magick, Directory: f.root, Timeout: 30 * time.Second, Arguments: []string{"-size", "16x12", "xc:red", "-strip", path}})
				if e != nil {
					t.Fatalf("합성 이미지 생성 실패: %v %s", e, result.Stderr)
				}
				data, err = os.ReadFile(path)
			}
			if err != nil {
				t.Fatal(err)
			}
			inspector := engine.LocalInspector{ImageMagick: magick, Cache: filepath.Join(f.root, "cache")}
			downloadImageSource(t, f, data, ext)
			f.service.ConfigureImages(inspector)
			id := f.run(t, f.enqueue(t))
			var path, format, hash string
			var width, height int
			if err = f.db.QueryRow("SELECT filepath,detected_format,width,height,content_hash FROM media_file WHERE media_id=?", id).Scan(&path, &format, &width, &height, &hash); err != nil {
				t.Fatal(err)
			}
			if filepath.Ext(path) != "."+ext || format == "" || width <= 0 || height <= 0 || hash != fmt.Sprintf("%x", sha256.Sum256(data)) {
				t.Fatal("원본 형식 또는 검증 정보 유실", path, format, width, height, hash)
			}
			stored, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(stored, data) {
				t.Fatal("원본 파일이 변경되었습니다", err)
			}
			thumbs := thumbnail.NewService(f.db, filepath.Join(f.root, "cache", "thumbnails"), "", nil)
			thumbs.ConfigureImages(inspector)
			if err = thumbs.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			defer thumbs.Close()
			thumbs.ScheduleMedia(id)
			var preview *string
			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				if err = f.db.QueryRow("SELECT thumbnail_path FROM media_file WHERE media_id=?", id).Scan(&preview); err != nil {
					t.Fatal(err)
				}
				if preview != nil {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if preview == nil {
				t.Fatal("썸네일이 생성되지 않았습니다")
			}
			thumb, err := os.Open(*preview)
			if err != nil {
				t.Fatal(err)
			}
			defer thumb.Close()
			cfg, err := jpeg.DecodeConfig(thumb)
			if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 480 || cfg.Height > 480 {
				t.Fatal("표시용 JPEG가 올바르지 않습니다", cfg, err)
			}
			assertFK(t, f.db)
		})
	}
	for _, bad := range []string{"html", "wrong-format", "truncated"} {
		t.Run("reject-"+bad, func(t *testing.T) {
			f := newFixture(t)
			data := []byte("<html>로그인하세요</html>")
			if bad == "wrong-format" {
				var buf bytes.Buffer
				if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 6))); err != nil {
					t.Fatal(err)
				}
				data = buf.Bytes()
			} else if bad == "truncated" {
				data = []byte("RIFF\x00\x00\x00\x80WEBPVP8 ")
			}
			downloadImageSource(t, f, data, "webp")
			f.service.ConfigureImages(engine.LocalInspector{ImageMagick: magick, Cache: filepath.Join(f.root, "cache")})
			job := f.enqueue(t)
			assertImageDownloadRejected(t, f, job, f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"}), "DOWNLOAD_ENGINE_FAILED")
		})
	}
}
