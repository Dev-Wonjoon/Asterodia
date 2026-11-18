package engine_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

func TestGalleryDLSaveImages(t *testing.T) {
	root := engineTestRoot(t, "gallerydl-download")

	makePNG := func(pixel color.NRGBA) []byte {
		t.Helper()
		img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		img.SetNRGBA(0, 0, pixel)
		var buffer bytes.Buffer
		if err := png.Encode(&buffer, img); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes()
	}
	first := makePNG(color.NRGBA{R: 255, A: 255})
	second := makePNG(color.NRGBA{B: 255, A: 255})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		switch r.URL.RequestURI() {
		case "/first.png?token=first":
			_, _ = w.Write(first)
		case "/second.png?token=second":
			_, _ = w.Write(second)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	for _, tc := range []struct {
		name       string
		secondPath string
		missing    bool
	}{
		{name: "two-files", secondPath: "/second.png?token=second"},
		{name: "missing-second", secondPath: "/missing.png?token=second", missing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stageDir, err := os.MkdirTemp(root, "stage-")
			if err != nil {
				t.Fatal(err)
			}

			metadata := map[string]string{"extension": "png"}
			raw, err := json.Marshal([][]any{
				{2, map[string]string{"category": "fixture"}},
				{3, server.URL + "/first.png?token=first", metadata},
				{3, server.URL + tc.secondPath, metadata},
			})
			if err != nil {
				t.Fatal(err)
			}
			discovery, err := engine.ParseGalleryDLDiscovery(raw)
			if err != nil {
				t.Fatal(err)
			}

			paths, err := discovery.SaveImages(t.Context(), stageDir)
			if tc.missing {
				if err == nil || !strings.Contains(err.Error(), "2번째") ||
					!strings.Contains(err.Error(), "HTTP 404") || len(paths) != 0 {
					t.Fatalf("두 번째 파일의 HTTP 404와 빈 결과를 기대했습니다: 경로=%v 오류=%v",
						paths, err)
				}
				missingPath := filepath.Join(stageDir, "item-0002", "media.png")
				if _, err := os.Stat(missingPath); !os.IsNotExist(err) {
					t.Fatalf("누락된 이미지의 파일이 없어야 합니다: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(paths) != 2 {
				t.Fatalf("저장 결과 %d건, 기대값 2건", len(paths))
			}
			if filepath.Dir(paths[0]) == filepath.Dir(paths[1]) {
				t.Fatal("두 이미지가 서로 다른 작업 폴더에 있어야 합니다")
			}
			for i, want := range [][]byte{first, second} {
				got, err := os.ReadFile(paths[i])
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("%d번째 저장 파일이 조회 대상의 원본과 다릅니다", i+1)
				}
			}
		})
	}
}
