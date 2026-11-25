package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type GalleryDL struct {
	Executable    string
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
	// Anonymous Instagram HTML results transfer directly instead of CLI re-extraction.
	InstagramEmbed bool `json:"instagramEmbed,omitempty"`
}

var (
	instagramPostPath  = regexp.MustCompile(`^/(?:[A-Za-z0-9_.]+/)?(p|reel|reels|tv)/([A-Za-z0-9_-]{1,32})/?$`)
	galleryDLID        = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	galleryDLNumericID = regexp.MustCompile(`^[1-9][0-9]*$`)
)

var (
	ErrInstagramURL              = errors.New("Instagram 게시물 또는 릴스 링크를 입력하세요")
	ErrInstagramOptions          = errors.New("Instagram은 원본 사진·영상을 함께 저장합니다. 종류는 영상, 화질은 최고 화질, 형식은 자동으로 설정하세요")
	ErrAuthenticationRequired    = errors.New("로그인이 필요하거나 쿠키가 만료됐습니다. 설정에서 로그인된 브라우저 또는 쿠키 파일을 지정하세요")
	ErrInstagramPublicExtraction = errors.New("Instagram 공개 게시물 정보는 확인했지만 원본 미디어 주소를 추출하지 못했습니다")
	ErrAccessDenied              = errors.New("게시물 접근이 제한됐습니다. 계정의 열람 권한이나 사이트의 추가 인증을 확인하세요")
	ErrMediaUnavailable          = errors.New("게시물이 삭제되었거나 현재 계정으로 찾을 수 없습니다")
	ErrRateLimited               = errors.New("사이트 요청이 일시적으로 제한됐습니다. 잠시 후 다시 시도하세요")
)

func instagramShortcode(rawURL string) (string, error) {
	page, err := url.Parse(rawURL)
	if err != nil {
		return "", ErrInstagramURL
	}
	host := strings.ToLower(page.Hostname())
	match := instagramPostPath.FindStringSubmatch(page.EscapedPath())
	if (host != "instagram.com" && host != "www.instagram.com") || len(match) == 0 {
		return "", ErrInstagramURL
	}
	return match[2], nil
}

type InstagramTarget struct {
	TargetKey, MediaID, Kind, Title, OwnerID, OwnerName string
	FileIndex                                           int
}

type instagramMetadata struct {
	Category    string          `json:"category"`
	Subcategory string          `json:"subcategory"`
	Shortcode   string          `json:"post_shortcode"`
	TypeName    string          `json:"typename"`
	Type        string          `json:"type"`
	PostID      json.RawMessage `json:"post_id"`
	MediaID     json.RawMessage `json:"media_id"`
	Count       json.RawMessage `json:"count"`
	Num         json.RawMessage `json:"num"`
	OwnerID     json.RawMessage `json:"owner_id"`
	Username    string          `json:"username"`
	FullName    string          `json:"fullname"`
	Description string          `json:"description"`
	Extension   string          `json:"extension"`
	AudioURL    string          `json:"audio_url"`
}

func (discovery GalleryDLDiscovery) InstagramTargets(originalURL string) ([]InstagramTarget, error) {
	shortcode, err := instagramShortcode(originalURL)
	if err != nil {
		return nil, err
	}
	if len(discovery.Directories) != 1 || len(discovery.Files) == 0 {
		return nil, fmt.Errorf("Instagram 게시물 한 건의 전체 파일이 필요합니다")
	}
	var directory instagramMetadata
	if err = json.Unmarshal(discovery.Directories[0], &directory); err != nil {
		return nil, err
	}
	count := strconv.Itoa(len(discovery.Files))
	postID := galleryDLIdentifier(directory.PostID)
	valid := func(v instagramMetadata) bool {
		return v.Category == "instagram" && (v.Subcategory == "post" || v.Subcategory == "reel") && v.Shortcode == shortcode && galleryDLIdentifier(v.Count) == count && galleryDLIdentifier(v.PostID) == postID
	}
	if postID == "" || !valid(directory) {
		return nil, fmt.Errorf("Instagram 게시물 식별 정보 또는 전체 파일 수가 일치하지 않습니다")
	}
	switch directory.TypeName {
	case "GraphImage", "GraphVideo":
	case "GraphSidecar":
	case "":
		if directory.Type != "post" && directory.Type != "reel" {
			return nil, fmt.Errorf("Instagram 미디어 유형을 확인할 수 없습니다")
		}
	default:
		return nil, fmt.Errorf("지원하지 않는 Instagram 미디어 유형입니다")
	}
	title := strings.TrimSpace(directory.Description)
	if title == "" {
		title = "Instagram " + shortcode
	}
	seen := map[string]bool{}
	targets := make([]InstagramTarget, 0, len(discovery.Files))
	for i, file := range discovery.Files {
		var metadata instagramMetadata
		if err = json.Unmarshal(file.Metadata, &metadata); err != nil {
			return nil, err
		}
		id := galleryDLIdentifier(metadata.MediaID)
		if !valid(metadata) || metadata.TypeName != directory.TypeName || id == "" {
			return nil, fmt.Errorf("Instagram 개별 미디어 식별 정보가 중복되거나 일치하지 않습니다")
		}
		// gallery-dl counts separate background music as well. Observe those
		// records to prove completeness, but only publish the post's visual media.
		if metadata.AudioURL != "" {
			if metadata.AudioURL != file.URL {
				return nil, fmt.Errorf("Instagram 배경음악 식별 정보가 일치하지 않습니다")
			}
			continue
		}
		if seen[id] {
			return nil, fmt.Errorf("Instagram 미디어 ID가 중복되었습니다")
		}
		if (directory.TypeName == "GraphImage" || directory.TypeName == "GraphVideo" || len(discovery.Files) == 1) && id != postID {
			return nil, fmt.Errorf("Instagram 단일 게시물 ID와 미디어 ID가 일치하지 않습니다")
		}
		if len(discovery.Files) > 1 || len(metadata.Num) > 0 {
			if galleryDLIdentifier(metadata.Num) != strconv.Itoa(len(targets)+1) {
				return nil, fmt.Errorf("Instagram 파일 순서가 누락되거나 일치하지 않습니다")
			}
		}
		kind, _, e := galleryDLMediaFormat(metadata.Extension)
		if e != nil {
			return nil, e
		}
		if (directory.TypeName == "GraphImage" && kind != "image") || (directory.TypeName == "GraphVideo" && kind != "video") {
			return nil, fmt.Errorf("Instagram 미디어 종류와 확장자가 일치하지 않습니다")
		}
		seen[id] = true
		name := directory.Username
		if name == "" {
			name = directory.FullName
		}
		targets = append(targets, InstagramTarget{TargetKey: "instagram:" + postID + ":" + id, MediaID: id, Kind: kind, Title: title, OwnerID: galleryDLIdentifier(directory.OwnerID), OwnerName: name, FileIndex: i})
	}
	if len(targets) == 0 || ((directory.TypeName == "GraphImage" || directory.TypeName == "GraphVideo") && len(targets) != 1) {
		return nil, fmt.Errorf("Instagram 게시물의 사진·영상 수가 일치하지 않습니다")
	}
	return targets, nil
}

func (discovery GalleryDLDiscovery) InstagramTargetKey(originalURL string) (string, error) {
	if len(discovery.Files) == 1 {
		targets, err := discovery.InstagramTargets(originalURL)
		if err != nil {
			return "", err
		}
		return targets[0].TargetKey, nil
	}
	return "", fmt.Errorf("Instagram 단일 미디어가 필요합니다")
}

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

func (g *GalleryDL) Discover(
	ctx context.Context,
	originalURL string,
	workDir string,
) (GalleryDLDiscovery, error) {
	cli := g.instagramDiscoveryHandler("gallery-dl", func(ctx context.Context) (GalleryDLDiscovery, error) {
		return g.discoverGalleryDL(ctx, originalURL, workDir)
	}, true)
	handlers := []DiscoveryHandler[GalleryDLDiscovery]{cli}
	if g.CookieBrowser == "" && g.CookieFile == "" {
		if shortcode, err := instagramShortcode(originalURL); err == nil {
			handlers = []DiscoveryHandler[GalleryDLDiscovery]{
				g.instagramDiscoveryHandler("Instagram 임베드", func(ctx context.Context) (GalleryDLDiscovery, error) {
					return g.discoverInstagramEmbed(ctx, shortcode)
				}, false),
				g.instagramDiscoveryHandler("Instagram 공개 HTML", func(ctx context.Context) (GalleryDLDiscovery, error) {
					return g.discoverInstagramPublic(ctx, shortcode)
				}, false),
				cli,
			}
		}
	}
	result := RunDiscoveryChain(ctx, handlers...)
	return result.Value, result.Err
}

func (g *GalleryDL) instagramDiscoveryHandler(name string, resolve func(context.Context) (GalleryDLDiscovery, error), final bool) DiscoveryHandler[GalleryDLDiscovery] {
	return DiscoveryHandler[GalleryDLDiscovery]{Name: name, Handle: func(ctx context.Context) DiscoveryStepResult[GalleryDLDiscovery] {
		value, err := resolve(ctx)
		decision := DiscoveryHandled
		if err != nil {
			decision = DiscoveryNext
			// A public HTTP strategy may time out independently of the request.
			// The final CLI's process timeout must stop the entire attempt.
			if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrOutputLimit) || errors.Is(err, ErrStopUnconfirmed) ||
				(final && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))) {
				decision = DiscoveryStop
			}
		}
		return DiscoveryStepResult[GalleryDLDiscovery]{Decision: decision, Value: value, Err: err}
	}}
}

func (g *GalleryDL) discoverGalleryDL(ctx context.Context, originalURL, workDir string) (GalleryDLDiscovery, error) {
	auth, err := authArguments(g.CookieBrowser, g.CookieFile, workDir)
	if err != nil {
		return GalleryDLDiscovery{}, err
	}
	if g.CookieFile != "" {
		defer os.Remove(filepath.Join(workDir, "cookies.txt"))
	}
	args := append(g.arguments(), "--warning")
	args = append(args, networkArguments(ctx)...)
	args = append(args, auth...)
	args = append(args, "-J", "--", originalURL)
	result, err := g.runner.Run(ctx, ProcessRequest{
		Executable: g.Executable,
		Directory:  workDir,
		Timeout:    2 * time.Minute,
		Arguments:  args,
	})

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

func (g *GalleryDL) arguments() []string {
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
	if g.CookieBrowser == "" && g.CookieFile == "" {
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
