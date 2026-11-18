package download

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Storage struct {
	Root string
}

var stagingJSONEscapes = strings.NewReplacer(
	`\\`, `\\`,
	`\u2028`, "\u2028",
	`\u2029`, "\u2029",
)

func (s Storage) PrepareStage(jobID, attemptID string) (string, error) {
	if !filepath.IsAbs(s.Root) {
		return "", fmt.Errorf("다운로드 저장소는 절대 경로여야 합니다")
	}
	if strings.TrimSpace(jobID) == "" || strings.TrimSpace(attemptID) == "" {
		return "", fmt.Errorf("작업 ID와 시도 ID가 필요합니다")
	}

	owner := stageOwner(jobID, attemptID)

	base, err := filepath.EvalSymlinks(s.Root)
	if err != nil {
		return "", fmt.Errorf("다운로드 저장소 경로 확인 실패: %w", err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return "", fmt.Errorf("다운로드 저장소 열기 실패: %w", err)
	}
	defer root.Close()

	parent := filepath.Join(".storagebucket", "staging")
	for _, dir := range []string{".storagebucket", parent} {
		if err := root.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("작업 폴더의 상위 디렉터리 생성 실패: %w", err)
		}
		info, err := root.Lstat(dir)
		if err != nil {
			return "", fmt.Errorf("작업 폴더의 상위 디렉터리 확인 실패: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("작업 폴더의 상위 경로가 일반 디렉터리여야 합니다: %s", dir)
		}
	}

	relative := filepath.Join(parent, owner)
	if err := root.Mkdir(relative, 0o700); err != nil {
		return "", fmt.Errorf("시도별 작업 폴더 생성 실패: %w", err)
	}
	return filepath.Join(base, relative), nil
}

func stageOwner(jobID, attemptID string) string {
	var payload bytes.Buffer
	encoder := json.NewEncoder(&payload)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode([2]string{jobID, attemptID})
	encoded := stagingJSONEscapes.Replace(strings.TrimSuffix(payload.String(), "\n"))
	return fmt.Sprintf("%x", sha256.Sum256([]byte(encoded)))
}

func stageItemDir(stageDir string, position int) string {
	return filepath.Join(stageDir, fmt.Sprintf("item-%04d", position+1))
}

func prepareStageItem(stageDir string, position int) (string, error) {
	dir := stageItemDir(stageDir, position)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", fmt.Errorf("대상 작업 폴더 생성 실패: %w", err)
	}
	return dir, nil
}

func verifyStagedFile(stageDir string, position int, path string) error {
	itemDir := stageItemDir(stageDir, position)
	relative, err := filepath.Rel(itemDir, path)
	if err != nil || !filepath.IsAbs(path) ||
		relative == "." || relative == ".." || filepath.Base(relative) != relative {
		return fmt.Errorf("저장 파일이 해당 대상의 작업 폴더 바로 아래에 있어야 합니다")
	}

	resolved, err := filepath.EvalSymlinks(itemDir)
	if err != nil {
		return fmt.Errorf("대상 작업 폴더 확인 실패: %w", err)
	}
	sameDir, err := filepath.Rel(itemDir, resolved)
	if err != nil || sameDir != "." {
		return fmt.Errorf("대상 작업 폴더의 실제 경로가 변경됐습니다")
	}

	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("저장 파일 확인 실패: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return fmt.Errorf("저장 결과는 크기 0 초과의 일반 파일이어야 합니다")
	}
	return nil
}
