package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Engines may update cookies, so pass a private work copy instead of the user's file.
func authArguments(browser, file, dir string) ([]string, error) {
	if file != "" {
		if !filepath.IsAbs(file) {
			return nil, fmt.Errorf("쿠키 파일 절대 경로가 필요합니다")
		}
		info, err := os.Stat(file)
		if err != nil {
			return nil, fmt.Errorf("쿠키 파일을 확인하지 못했습니다: %w", err)
		}
		if !info.Mode().IsRegular() || info.Size() > 16*1024*1024 {
			return nil, fmt.Errorf("일반 쿠키 파일은 16 MiB 이하여야 합니다")
		}
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("쿠키 파일을 읽지 못했습니다: %w", err)
		}
		if len(b) > 16*1024*1024 {
			return nil, fmt.Errorf("쿠키 파일이 너무 큽니다")
		}
		copyPath := filepath.Join(dir, "cookies.txt")
		if err = os.WriteFile(copyPath, b, 0600); err != nil {
			return nil, err
		}
		return []string{"--cookies", copyPath}, nil
	}
	if browser != "" {
		browser = strings.ToLower(browser)
		switch browser {
		case "chrome", "firefox", "edge":
		default:
			return nil, fmt.Errorf("지원하지 않는 쿠키 브라우저입니다")
		}
		return []string{"--cookies-from-browser", browser}, nil
	}
	return nil, nil
}
