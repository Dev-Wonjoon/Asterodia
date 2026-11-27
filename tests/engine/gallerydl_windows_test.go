package engine_test

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

func TestParseGalleryDLDiscovery(t *testing.T) {
	const firstURL = "https://cdn.invalid/a.png?token=abc"
	const secondURL = "https://cdn.invalid/b.png?token=xyz"
	const firstFile = `[3,"` + firstURL + `",{"extension":"png"}]`
	const secondFile = `[3,"` + secondURL + `",{"extension":"png"}]`

	t.Run("두 파일의 순서와 URL 쿼리 보존", func(t *testing.T) {
		raw := `[[2,{"category":"fixture"}],` + firstFile + `,` + secondFile + `]`
		result, err := engine.ParseGalleryDLDiscovery([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Directories) != 1 || len(result.Files) != 2 {
			t.Fatalf("디렉터리 %d건, 파일 %d건", len(result.Directories), len(result.Files))
		}
		if result.Files[0].URL != firstURL || result.Files[1].URL != secondURL {
			t.Fatalf("파일 URL 또는 순서가 달라졌습니다: %+v", result.Files)
		}
		if string(result.Files[0].Metadata) != `{"extension":"png"}` {
			t.Fatalf("파일 메타데이터가 달라졌습니다: %s", result.Files[0].Metadata)
		}
	})

	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"부분 결과 뒤 추출 오류", `[` + firstFile + `,[-1,{"error":"ValueError"}]]`},
		{"미해결 하위 요청", `[` + firstFile + `,[6,"https://example.invalid/child",{}]]`},
		{"빈 파일 목록", `[[2,{"category":"fixture"}]]`},
		{"잘못된 파일 URL", `[[3,"file:///a.png",{}]]`},
		{"잘못된 메타데이터", `[[3,"https://cdn.invalid/a.png",null]]`},
		{"깨진 JSON", `[[3,`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := engine.ParseGalleryDLDiscovery([]byte(tc.raw))
			if err == nil || len(result.Files) != 0 {
				t.Fatalf("오류와 빈 결과를 기대했습니다. 파일 %d건, 오류 %v",
					len(result.Files), err)
			}
		})
	}
}

func TestGalleryDLDiscoverBluesky(t *testing.T) {
	if os.Getenv("ASTERODIA_LIVE_TESTS") != "1" {
		t.Skip("실제 사이트 검증은 ASTERODIA_LIVE_TESTS=1로 실행하세요")
	}
	executable := os.Getenv("ASTERODIA_TEST_GALLERYDL_EXE")
	if executable == "" {
		t.Skip("실제 조회를 실행하려면 ASTERODIA_TEST_GALLERYDL_EXE를 지정하세요")
	}
	if !filepath.IsAbs(executable) {
		t.Fatal("gallery-dl.exe의 절대 경로를 지정하세요")
	}

	const originalURL = "https://bsky.app/profile/fruttymoment.bsky.social/post/3lcdqq3x47s2h?asterodia=m3"
	const wantPostID = "3lcdqq3x47s2h"

	kind, err := engine.Select(originalURL, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if kind != engine.KindGalleryDL {
		t.Fatalf("선택된 엔진: %q, 기대값: %q", kind, engine.KindGalleryDL)
	}

	root := engineTestRoot(t, "gallerydl-discovery")
	workDir, err := os.MkdirTemp(root, "stage-")
	if err != nil {
		t.Fatal(err)
	}

	gallery := engine.GalleryDL{Executable: executable}
	discovery, err := gallery.Discover(t.Context(), originalURL, workDir)
	if err != nil {
		t.Fatalf("실제 게시물 조회 실패: %v", err)
	}
	if len(discovery.Directories) != 1 || len(discovery.Files) != 1 {
		t.Fatalf("디렉터리 %d건, 파일 %d건, 각각 1건을 기대했습니다",
			len(discovery.Directories), len(discovery.Files))
	}

	var metadata struct {
		Category    string `json:"category"`
		Subcategory string `json:"subcategory"`
		PostID      string `json:"post_id"`
		Filename    string `json:"filename"`
		Extension   string `json:"extension"`
	}
	if err := json.Unmarshal(discovery.Files[0].Metadata, &metadata); err != nil {
		t.Fatalf("파일 메타데이터 해석 실패: %v", err)
	}
	if metadata.Category != "bluesky" || metadata.Subcategory != "post" ||
		metadata.PostID != wantPostID || metadata.Filename == "" || metadata.Extension == "" {
		t.Fatalf("파일 식별 정보 불일치: %+v", metadata)
	}

	entries, err := os.ReadDir(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("조회 작업 폴더에 항목이 생겼습니다: %d건", len(entries))
	}
	t.Logf("조회 확인: 게시물=%s 파일=%s 확장자=%s",
		metadata.PostID, metadata.Filename, metadata.Extension)

	savedPaths, err := (&engine.GalleryDL{Executable: executable}).Download(t.Context(), originalURL, discovery, workDir)
	if err != nil {
		t.Fatalf("이미지 저장 실패: %v", err)
	}

	savedPath := savedPaths[0]
	savedFile, err := os.Open(savedPath)
	if err != nil {
		t.Fatal(err)
	}
	defer savedFile.Close()

	decoded, format, err := image.Decode(savedFile)
	if err != nil {
		t.Fatalf("저장된 이미지 디코딩 실패: %v", err)
	}

	entries, err = os.ReadDir(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(savedPath) {
		t.Fatalf("작업 폴더에 확정한 이미지 한 개가 있어야 합니다")
	}

	bounds := decoded.Bounds()
	t.Logf("이미지 저장 및 디코딩 확인: %s %dx%d %s",
		format, bounds.Dx(), bounds.Dy(), savedPath)
}
