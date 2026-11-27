package download_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/download"
)

func TestPrepareDownloadStage(t *testing.T) {
	configured := downloadTestRoot(t)
	root, err := filepath.EvalSymlinks(configured)
	if err != nil {
		t.Fatal(err)
	}

	// go test는 이 테스트 패키지의 소스 폴더에서 실행합니다.
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repo, err = filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	category := filepath.Join(repo, ".test-data", "download-storage")
	relative, err := filepath.Rel(category, root)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("실행별 테스트 폴더는 .test-data/download-storage 아래에 있어야 합니다")
	}

	storage := download.Storage{Root: root}
	stage, err := storage.PrepareStage("job-1", "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	const owner = "e3677bfd8cee25dc27e5c906bf980d9c31610052a3f797d86d717415f2de01c1"
	want := filepath.Join(root, ".asterodia", "staging", owner)
	if stage != want {
		t.Fatalf("작업 폴더: %q, 기대값: %q", stage, want)
	}

	marker := filepath.Join(stage, "existing.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.PrepareStage("job-1", "attempt-1"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("같은 시도의 재생성에서 os.ErrExist를 기대했습니다: %v", err)
	}
	if contents, err := os.ReadFile(marker); err != nil || string(contents) != "keep" {
		t.Fatalf("기존 파일이 보존돼야 합니다: 내용=%q 오류=%v", contents, err)
	}

	next, err := storage.PrepareStage("job-1", "attempt-2")
	if err != nil {
		t.Fatal(err)
	}
	if next == stage || filepath.Dir(next) != filepath.Dir(stage) {
		t.Fatalf("새 시도는 같은 staging 아래의 별도 폴더여야 합니다: %q", next)
	}
}

func downloadTestRoot(t *testing.T) string {
	t.Helper()

	configured := os.Getenv("ASTERODIA_TEST_ROOT")
	if !filepath.IsAbs(configured) {
		t.Fatal("ASTERODIA_TEST_ROOT에 절대 경로를 지정하세요")
	}
	root, err := filepath.EvalSymlinks(configured)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repo, err = filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	category := filepath.Join(repo, ".test-data", "download-storage")
	relative, err := filepath.Rel(category, root)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("실행별 테스트 폴더는 .test-data/download-storage 아래에 있어야 합니다")
	}
	return root
}
