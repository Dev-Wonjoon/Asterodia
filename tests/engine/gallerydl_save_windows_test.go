package engine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGalleryDLSaveImages(t *testing.T) {
	exe := os.Getenv("ASTERODIA_TEST_GALLERYDL_EXE")
	if !filepath.IsAbs(exe) {
		t.Skip("gallery-dl 실제 실행 파일이 필요합니다")
	}
	root := engineTestRoot(t, "gallerydl-download")
	var data bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 8, 6))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing.png" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/blocked.png" {
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(data.Bytes())
	}))
	defer server.Close()
	for _, mode := range []string{"normal", "missing", "changed-identity", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			stage, err := os.MkdirTemp(root, "engine-stage-")
			if err != nil {
				t.Fatal(err)
			}
			g := engine.GalleryDL{Executable: exe}
			address := server.URL + "/image.png?token=fixture"
			if mode == "missing" {
				address = server.URL + "/missing.png"
			}
			if mode == "cancel" {
				address = server.URL + "/blocked.png"
			}
			d, err := g.Discover(t.Context(), address, stage)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "changed-identity" {
				var meta map[string]any
				json.Unmarshal(d.Files[0].Metadata, &meta)
				meta["filename"] = "unexpected"
				d.Files[0].Metadata, _ = json.Marshal(meta)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			if mode == "cancel" {
				ctx, cancel = context.WithTimeout(ctx, 800*time.Millisecond)
				defer cancel()
			}
			paths, err := g.Download(ctx, address, d, stage)
			if mode != "normal" {
				if err == nil || len(paths) != 0 {
					t.Fatalf("실패 파일 공개: paths=%v err=%v", paths, err)
				}
				if mode == "changed-identity" && !strings.Contains(err.Error(), "식별") {
					t.Fatal(err)
				}
				if _, e := os.Stat(filepath.Join(stage, "item-0001")); !os.IsNotExist(e) {
					t.Fatal("실패 결과가 최종 준비 폴더로 옮겨졌습니다", e)
				}
				return
			}
			if err != nil || len(paths) != 1 {
				t.Fatalf("다운로드 실패: %v %v", paths, err)
			}
			got, err := os.ReadFile(paths[0])
			if err != nil || !bytes.Equal(got, data.Bytes()) {
				t.Fatal("원본 내용 불일치", err)
			}
			entries, err := os.ReadDir(stage)
			if err != nil || len(entries) != 1 || entries[0].Name() != "item-0001" {
				t.Fatal("임시 메타데이터 미정리", entries, err)
			}
		})
	}
}

func TestGalleryDLDownloadResults(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := engineTestRoot(t, "gallerydl-download")
	t.Setenv("ASTERODIA_GALLERYDL_FIXTURE", "1")
	for _, mode := range []string{"ordered", "reordered", "extension-alias", "generic-id-schema", "partial", "changed-id", "changed-post", "extra-file", "instagram"} {
		t.Run(mode, func(t *testing.T) {
			stage, err := os.MkdirTemp(root, "results-")
			if err != nil {
				t.Fatal(err)
			}
			address := "https://x.com/fixture/status/123"
			base := map[string]any{"category": "twitter", "subcategory": "tweet", "tweet_id": "123", "count": 2, "content": "fixture"}
			first := map[string]any{"category": "twitter", "tweet_id": "123", "filename": "one", "extension": "jpg", "num": 1}
			second := map[string]any{"category": "twitter", "tweet_id": "123", "filename": "two", "extension": "mp4", "num": 2}
			if mode == "generic-id-schema" {
				base = map[string]any{"category": "future", "id": "parent", "count": 2}
				first = map[string]any{"category": "future", "id": "media-1", "extension": "jpg", "num": 1}
				second = map[string]any{"category": "future", "id": "media-2", "extension": "mp4", "num": 2}
			}
			if mode == "instagram" {
				address = "https://www.instagram.com/p/Fixture_123/"
				base = map[string]any{"category": "instagram", "subcategory": "post", "post_shortcode": "Fixture_123", "post_id": "123", "typename": "GraphSidecar", "count": 2}
				first = map[string]any{}
				second = map[string]any{}
				for key, value := range base {
					first[key] = value
					second[key] = value
				}
				first["media_id"], first["extension"], first["num"] = "124", "jpg", 1
				second["media_id"], second["extension"], second["num"] = "125", "mp4", 2
			}
			if mode == "extension-alias" {
				first["extension"] = "jpeg"
			}
			raw, _ := json.Marshal([][]any{{2, base}, {3, "https://cdn.invalid/one.jpg", first}, {3, "https://cdn.invalid/two.mp4", second}})
			d, err := engine.ParseGalleryDLDiscovery(raw)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "changed-id" {
				second["filename"] = "different"
			}
			if mode == "extension-alias" {
				first["extension"] = "jpg"
			}
			if mode == "changed-post" {
				second["tweet_id"] = "999"
			}
			// Output filename ordering deliberately differs from extractor order.
			a, _ := json.Marshal(first)
			b, _ := json.Marshal(second)
			records := []map[string]any{{"Name": "z-first.jpg", "Metadata": json.RawMessage(a), "Payload": "first media"}, {"Name": "a-second.mp4", "Metadata": json.RawMessage(b), "Payload": "second media"}}
			if mode == "reordered" {
				records[0], records[1] = records[1], records[0]
			}
			if mode == "partial" {
				records = records[:1]
			}
			if mode == "extra-file" {
				records = append(records, map[string]any{"Name": "unreported.txt", "Payload": "extra"})
			}
			payload, _ := json.Marshal(records)
			t.Setenv("ASTERODIA_GALLERYDL_DOWNLOAD_RESULTS", string(payload))
			paths, err := (&engine.GalleryDL{Executable: exe}).Download(t.Context(), address, d, stage)
			if mode != "ordered" && mode != "reordered" && mode != "extension-alias" && mode != "generic-id-schema" && mode != "instagram" {
				if err == nil || len(paths) != 0 {
					t.Fatal("다른 결과를 공개했습니다", paths, err)
				}
				return
			}
			if err != nil || len(paths) != 2 {
				t.Fatal(paths, err)
			}
			for i, want := range []string{"first media", "second media"} {
				got, e := os.ReadFile(paths[i])
				if e != nil || string(got) != want {
					t.Fatal("추출 순서와 파일 매칭 실패", i, string(got), e)
				}
			}
		})
	}
}
