package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/cookies"
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

// Cookies shares the managed source with both CLI backends and native extractors.
func (y *Ytdlp) Cookies(ctx context.Context, address *url.URL, workDir string) ([]*http.Cookie, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if address == nil || address.Scheme != "https" || address.Hostname() == "" || address.User != nil {
		return nil, ErrInvalidURL
	}
	if y.Auth != nil {
		return y.Auth.forURL(ctx, address, workDir)
	}
	if y.CookieFile != "" {
		return readNetscapeCookies(ctx, y.CookieFile, address)
	}
	records, err := y.browserCookies(ctx, workDir)
	if err != nil {
		return nil, err
	}
	selected := cookies.ForURL(records, address)
	if len(selected) == 0 {
		return nil, ErrAuthenticationRequired
	}
	return selected, nil
}

func (y *Ytdlp) browserCookies(ctx context.Context, workDir string) ([]cookies.Record, error) {
	if y.CookieBrowser == "" {
		return nil, ErrAuthenticationRequired
	}
	args, err := authArguments(y.CookieBrowser, "", workDir)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(workDir) {
		return nil, fmt.Errorf("인증 작업 폴더는 절대 경로여야 합니다")
	}
	dir, err := os.MkdirTemp(workDir, "auth-")
	if err != nil {
		return nil, err
	}
	file := filepath.Join(dir, "cookies.txt")
	defer func() { _ = os.Remove(file); _ = os.Remove(dir) }()
	command := []string{"--ignore-config", "--no-plugin-dirs", "--no-cache-dir", "--no-js-runtimes", "--no-remote-components", "--encoding", "utf-8", "--quiet", "--no-warnings"}
	command = append(command, args...)
	command = append(command, "--cookies", file)
	_, exportErr := y.runner.Run(WithOutputObserver(ctx, nil), ProcessRequest{Executable: y.Executable, Directory: dir, Arguments: command, Timeout: 30 * time.Second})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if errors.Is(exportErr, context.Canceled) || errors.Is(exportErr, context.DeadlineExceeded) || errors.Is(exportErr, ErrStopUnconfirmed) || errors.Is(exportErr, ErrOutputLimit) {
		return nil, exportErr
	}
	if _, err := os.Stat(file); err != nil {
		return nil, fmt.Errorf("%w: 브라우저 쿠키를 읽지 못했습니다. 쿠키 파일 설정으로 다시 시도하세요", ErrAuthenticationRequired)
	}
	return cookies.ReadFile(file)
}

func readNetscapeCookies(ctx context.Context, file string, address *url.URL) ([]*http.Cookie, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records, err := cookies.ReadFile(file)
	if err != nil {
		return nil, err
	}
	selected := cookies.ForURL(records, address)
	if len(selected) == 0 {
		return nil, ErrAuthenticationRequired
	}
	return selected, nil
}
