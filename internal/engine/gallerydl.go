package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type GalleryDL struct {
	Executable    string
	Auth          *ManagedAuth
	CookieBrowser string
	CookieFile    string
	HTTPClient    *http.Client
	runner        ProcessRunner
}

type GalleryDLFile struct {
	URL      string
	Metadata json.RawMessage
}

type GalleryDLDiscovery struct {
	Directories []json.RawMessage
	Files       []GalleryDLFile
}

var (
	galleryDLID        = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	galleryDLNumericID = regexp.MustCompile(`^[1-9][0-9]*$`)
)

var (
	ErrAuthenticationRequired = errors.New("로그인이 필요하거나 쿠키가 만료됐습니다. 설정에서 로그인된 브라우저 또는 쿠키 파일을 지정하세요")
	ErrAccessDenied           = errors.New("게시물 접근이 제한됐습니다. 계정의 열람 권한이나 사이트의 추가 인증을 확인하세요")
	ErrMediaUnavailable       = errors.New("게시물이 삭제되었거나 현재 계정으로 찾을 수 없습니다")
	ErrRateLimited            = errors.New("사이트 요청이 일시적으로 제한됐습니다. 잠시 후 다시 시도하세요")
)

func galleryDLIdentifier(raw json.RawMessage) string {
	var value string
	if err := json.Unmarshal(raw, &value); err == nil && galleryDLID.MatchString(value) {
		return value
	}

	number := string(bytes.TrimSpace(raw))
	if galleryDLNumericID.MatchString(number) {
		return number
	}
	return ""
}

// Discover executes gallery-dl only. Site-specific strategies live in extractors.
func (g *GalleryDL) Discover(ctx context.Context, originalURL, workDir string) (GalleryDLDiscovery, error) {
	return g.discoverGalleryDL(ctx, originalURL, workDir)
}

func (g *GalleryDL) discoverGalleryDL(ctx context.Context, originalURL, workDir string) (GalleryDLDiscovery, error) {
	auth, finishAuth, err := prepareAuthentication(ctx, g.Auth, g.CookieBrowser, g.CookieFile, originalURL, workDir)
	if err != nil {
		return GalleryDLDiscovery{}, err
	}
	defer finishAuth(false)
	args := append(g.arguments(originalURL), "--warning")
	args = append(args, networkArguments(ctx)...)
	args = append(args, auth...)
	args = append(args, "-J", "--", originalURL)
	result, err := g.runner.Run(ctx, ProcessRequest{
		Executable: g.Executable,
		Directory:  workDir,
		Timeout:    2 * time.Minute,
		Arguments:  args,
	})

	if err == nil {
		err = finishAuth(true)
	}

	stderr := strings.TrimSpace(result.Stderr)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrStopUnconfirmed) || errors.Is(err, ErrOutputLimit) {
		return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl 대상 조회 실패: %w", err)
	}
	// DataJob may return exit code zero with an explicit extraction error in JSON.
	discovery, parseErr := ParseGalleryDLDiscovery([]byte(result.Stdout))
	if parseErr != nil && (errors.Is(parseErr, ErrAuthenticationRequired) || errors.Is(parseErr, ErrAccessDenied) || errors.Is(parseErr, ErrMediaUnavailable) || errors.Is(parseErr, ErrRateLimited)) {
		return GalleryDLDiscovery{}, parseErr
	}
	if err != nil {
		if classified := galleryDLFailure("", stderr); classified != nil {
			return GalleryDLDiscovery{}, classified
		}
		if stderr != "" {
			return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl 대상 조회 실패: %w\n%s", err, stderr)
		}
		return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl 대상 조회 실패: %w", err)
	}
	if parseErr != nil && stderr != "" {
		if classified := galleryDLFailure("", stderr); classified != nil {
			return GalleryDLDiscovery{}, classified
		}
	}

	return discovery, parseErr
}

func (g *GalleryDL) arguments(originalURL string) []string {
	args := []string{
		"--config-ignore", "--no-input", "--no-colors",
		"-o", "extractor.module-sources=[null]", "-o", "cache.file=null",
		"-o", "extractor.cookies=null", "-o", "extractor.cookies-update=false",
		"-o", "extractor.netrc=false", "-o", "output.stdout=utf-8",
		"-o", "output.stderr=utf-8", "-o", "output.private=true",
		// Produce direct, complete media URLs instead of ytdl: DASH requests.
		"-o", "extractor.instagram.videos=merged", "-o", "extractor.instagram.audio=true",
		"-o", "extractor.instagram.previews=false",
	}
	authenticated := g.CookieBrowser != "" || g.CookieFile != ""
	if g.Auth != nil {
		authenticated = g.Auth.HasCookies(originalURL)
	}
	if !authenticated {
		args = append(args, "-o", "extractor.instagram.api=graphql")
	}
	return args
}

func galleryDLFailure(class, message string) error {
	text := strings.ToLower(message)
	hasStatus := func(code string) bool {
		return text == code || strings.HasPrefix(text, code+" ") || strings.Contains(text, "http "+code) || strings.Contains(text, "http error "+code) || strings.Contains(text, "'"+code+" ") || strings.Contains(text, "("+code+" ")
	}
	switch {
	case hasStatus("429"), strings.Contains(text, "rate limit"), strings.Contains(text, "please wait a few minutes"):
		return ErrRateLimited
	case class == "AuthRequired", class == "AuthenticationError", hasStatus("401"), strings.Contains(text, "login"), strings.Contains(text, "log in"), strings.Contains(text, "cookies needed"), strings.Contains(text, "cookies required"):
		return ErrAuthenticationRequired
	case class == "AuthorizationError", class == "ChallengeError", hasStatus("403"), strings.Contains(text, "challenge"), strings.Contains(text, "checkpoint"), strings.Contains(text, "private account"):
		return ErrAccessDenied
	case class == "NotFoundError", hasStatus("404"), strings.Contains(text, "could not be found"):
		return ErrMediaUnavailable
	case class == "NoExtractorError", strings.Contains(text, "unsupported url"), strings.Contains(text, "no suitable extractor"):
		return ErrUnsupportedURL
	}
	return nil
}

func galleryDLMediaFormat(extension string) (kind, format string, err error) {
	switch strings.ToLower(extension) {
	case "jpg", "jpeg":
		return "image", "jpeg", nil
	case "png":
		return "image", "png", nil
	case "mp4":
		return "video", "mp4", nil
	default:
		return "", "", fmt.Errorf("앱에서 등록할 수 없는 미디어 형식입니다: %q", extension)
	}
}

func ParseGalleryDLDiscovery(raw []byte) (GalleryDLDiscovery, error) {
	var messages [][]json.RawMessage
	if err := json.Unmarshal(raw, &messages); err != nil {
		return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl 조회 JSON 해석 실패: %w", err)
	}
	if len(messages) > 20_000 {
		return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl 조회 메시지 한도를 초과했습니다")
	}

	var discovery GalleryDLDiscovery
	for i, message := range messages {
		if len(message) == 0 {
			return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl %d번째 메시지가 비어 있습니다", i+1)
		}
		var kind int
		if err := json.Unmarshal(message[0], &kind); err != nil {
			return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl %d번째 메시지 종류 해석 실패: %w", i+1, err)
		}

		switch kind {
		case -1:
			if len(message) == 2 {
				var failure struct {
					Error   string `json:"error"`
					Message string `json:"message"`
				}
				if json.Unmarshal(message[1], &failure) == nil {
					if classified := galleryDLFailure(failure.Error, failure.Message); classified != nil {
						return GalleryDLDiscovery{}, classified
					}
				}
			}
			return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl %d번째 메시지에서 추출이 실패했습니다", i+1)
		case 2:
			if len(message) != 2 || !galleryDLObject(message[1]) {
				return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl %d번째 디렉터리 정보가 잘못되었습니다", i+1)
			}
			discovery.Directories = append(discovery.Directories, message[1])
		case 3:
			if len(message) != 3 || !galleryDLObject(message[2]) {
				return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl %d번째 파일 정보가 잘못되었습니다", i+1)
			}
			var address string
			if err := json.Unmarshal(message[1], &address); err != nil {
				return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl %d번째 파일 URL 해석 실패: %w", i+1, err)
			}
			mediaURL, err := url.Parse(address)
			_, pinterestHLS := pinterestManifest(address, discovery.pinterestMetadata(GalleryDLFile{Metadata: message[2]}))
			if !pinterestHLS && (err != nil || mediaURL.Hostname() == "" || mediaURL.User != nil ||
				(mediaURL.Scheme != "http" && mediaURL.Scheme != "https")) {
				return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl %d번째 파일 URL이 잘못되었습니다", i+1)
			}
			discovery.Files = append(discovery.Files, GalleryDLFile{
				URL:      address,
				Metadata: message[2],
			})
			if len(discovery.Files) > 5000 {
				return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl 조회 파일 한도를 초과했습니다")
			}
		case 6:
			return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl %d번째 하위 추출 요청이 해석되지 않았습니다", i+1)
		default:
			return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl %d번째 메시지 종류 %d를 지원하지 않습니다", i+1, kind)
		}
	}

	if len(discovery.Files) == 0 {
		return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl 조회 결과에 파일이 없습니다")
	}
	return discovery, nil
}

func galleryDLObject(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '{'
}
