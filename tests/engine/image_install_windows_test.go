package engine_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

func TestImageEngineInstallation(t *testing.T) {
	if os.Getenv("STORAGEBUCKET_IMAGE_INSTALL_TEST") != "1" {
		t.Skip("공식 배포본 다운로드 검증은 명시적으로 실행합니다")
	}
	root := engineTestRoot(t, "engine-parsers")
	bin := filepath.Join(root, "image-install")
	manager, err := engine.NewManager(bin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := manager.Install(t.Context(), "imagemagick", "fixture-install"); err != nil || !ok {
		t.Fatalf("공식 이미지 엔진 설치: %v", err)
	}
	status, err := manager.Status(t.Context())
	if err != nil || !status["imagemagick"].Installed {
		t.Fatalf("설치 엔진 상태: %v %+v", err, status)
	}
	if ok, err := manager.Install(t.Context(), "imagemagick", "fixture-reuse"); err != nil || !ok {
		t.Fatalf("기존 엔진 재사용: %v", err)
	}
}
