package desktop

import (
	"fmt"
	"os"
	"path/filepath"
)

type Paths struct {
	Root      string
	Data      string
	Bin       string
	Cache     string
	Downloads string
}

// baseDir가 비어 있으면 실제 실행 파일의 디렉터리를 사용한다.
// 개발·검증 시에는 별도의 절대 경로를 전달할 수 있다.
func ResolvePaths(baseDir string) (Paths, error) {
	if baseDir == "" {
		executable, err := os.Executable()
		if err != nil {
			return Paths{}, fmt.Errorf("실행 파일 경로 확인: %w", err)
		}

		executable, err = filepath.EvalSymlinks(executable)
		if err != nil {
			return Paths{}, fmt.Errorf("실제 실행 파일 경로 확인: %w", err)
		}

		baseDir = filepath.Dir(executable)
	}

	if !filepath.IsAbs(baseDir) {
		return Paths{}, fmt.Errorf(
			"기준 디렉터리는 절대 경로여야 합니다: %s",
			baseDir,
		)
	}

	root := filepath.Clean(baseDir)

	return Paths{
		Root:      root,
		Data:      filepath.Join(root, "data"),
		Bin:       filepath.Join(root, "bin"),
		Cache:     filepath.Join(root, "cache"),
		Downloads: filepath.Join(root, "downloads"),
	}, nil
}
