package engine_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

// This opt-in test downloads the official pinned release into an isolated bin.
func TestGalleryDLUpgradeAndReuse(t *testing.T) {
	if os.Getenv("ASTERODIA_GALLERYDL_UPDATE_TEST") != "1" {
		t.Skip("공식 gallery-dl 업데이트 검증은 명시적으로 실행합니다")
	}
	root := engineTestRoot(t, "engine-parsers")
	bin, err := os.MkdirTemp(root, "gallery-update-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(bin) })
	old := os.Getenv("ASTERODIA_TEST_GALLERYDL_OLD_EXE")
	latest := os.Getenv("ASTERODIA_TEST_GALLERYDL_LATEST_EXE")
	if !filepath.IsAbs(old) || !filepath.IsAbs(latest) {
		t.Fatal("구버전과 검증된 최신 버전 엔진의 절대 경로가 필요합니다")
	}
	copyEngine := func(destination string) {
		t.Helper()
		source, err := os.Open(old)
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()
		target, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(target, source)
		closeErr := target.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
	}
	hash := func(path string) [32]byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return sha256.Sum256(data)
	}
	destination := filepath.Join(bin, "gallery-dl.exe")
	copyEngine(destination)
	oldHash := hash(destination)
	var phases []string
	manager, err := engine.NewManager(bin, func(progress engine.InstallProgress) { phases = append(phases, progress.Phase) })
	if err != nil {
		t.Fatal(err)
	}
	status, err := manager.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if status["gallery-dl"].Installed || !strings.Contains(status["gallery-dl"].Error, "1.32.20") {
		t.Fatalf("구버전 상태: %+v", status["gallery-dl"])
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if installed, err := manager.Install(cancelled, "gallery-dl", "cancel-update"); installed || !errors.Is(err, context.Canceled) {
		t.Fatalf("취소 상태: %t %v", installed, err)
	}
	if hash(destination) != oldHash {
		t.Fatal("취소된 업데이트가 기존 엔진을 변경했습니다")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	phases = nil
	if installed, err := manager.Install(ctx, "gallery-dl", "upgrade"); err != nil || !installed {
		t.Fatalf("구버전 교체: %v", err)
	}
	status, err = manager.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current := status["gallery-dl"]
	if !current.Installed || current.Version == nil || *current.Version != "1.32.20" {
		t.Fatalf("최신 엔진 상태: %+v", current)
	}
	if hash(destination) != hash(latest) || hash(destination) == oldHash {
		t.Fatal("공식 검증 파일과 교체된 엔진이 다릅니다")
	}
	if !strings.Contains(strings.Join(phases, ","), "downloading") {
		t.Fatal("실제 다운로드가 실행되지 않았습니다")
	}
	t.Log("1.32.1을 업데이트 대상으로 판정하고 공식 1.32.20 설치 성공")
	before, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	phases = nil
	if installed, err := manager.Install(ctx, "gallery-dl", "reuse"); err != nil || !installed {
		t.Fatalf("최신 엔진 재사용: %v", err)
	}
	after, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) || strings.Contains(strings.Join(phases, ","), "downloading") {
		t.Fatal("최신 엔진을 불필요하게 다시 설치했습니다")
	}
	if hash(destination) != hash(latest) {
		t.Fatal("재사용 중 엔진 변경")
	}
	t.Log("최신 설치본을 다운로드·덮어쓰기 없이 재사용")
}
