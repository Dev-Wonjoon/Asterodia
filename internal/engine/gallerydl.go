package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
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
	instagramPostPath  = regexp.MustCompile(`^/(?:[A-Za-z0-9_.]+/)?(p|reel|reels|tv)/([A-Za-z0-9_-]{1,32})/?$`)
	galleryDLID        = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	galleryDLNumericID = regexp.MustCompile(`^[1-9][0-9]*$`)
)

var (
	ErrInstagramURL           = errors.New("Instagram 게시물 또는 릴스 링크를 입력하세요")
	ErrInstagramOptions       = errors.New("Instagram은 원본 사진·영상을 함께 저장합니다. 종류는 영상, 화질은 최고 화질, 형식은 자동으로 설정하세요")
	ErrAuthenticationRequired = errors.New("로그인이 필요하거나 쿠키가 만료됐습니다. 설정에서 로그인된 브라우저 또는 쿠키 파일을 지정하세요")
	ErrAccessDenied           = errors.New("게시물 접근이 제한됐습니다. 계정의 열람 권한이나 Instagram 추가 인증을 확인하세요")
	ErrMediaUnavailable       = errors.New("게시물이 삭제되었거나 현재 계정으로 찾을 수 없습니다")
	ErrRateLimited            = errors.New("Instagram 요청이 일시적으로 제한됐습니다. 잠시 후 다시 시도하세요")
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
	auth, err := authArguments(g.CookieBrowser, g.CookieFile, workDir)
	if err != nil {
		return GalleryDLDiscovery{}, err
	}
	if g.CookieFile != "" {
		defer os.Remove(filepath.Join(workDir, "cookies.txt"))
	}
	args := []string{
		"--config-ignore", "--no-input", "--no-colors", "--warning",
		"-o", "extractor.module-sources=[null]", "-o", "cache.file=null",
		"-o", "extractor.cookies=null", "-o", "extractor.cookies-update=false",
		"-o", "extractor.netrc=false", "-o", "output.stdout=utf-8",
		"-o", "output.stderr=utf-8", "-o", "output.private=true",
		// Produce direct, complete media URLs instead of ytdl: DASH requests.
		"-o", "extractor.instagram.videos=merged", "-o", "extractor.instagram.audio=true",
		"-o", "extractor.instagram.previews=false",
	}
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
	if stderr != "" {
		if classified := galleryDLFailure("", stderr); classified != nil {
			return GalleryDLDiscovery{}, classified
		}
		return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl 조회 중 경고 또는 오류:\n%s", stderr)
	}

	return discovery, parseErr
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
		return "", "", fmt.Errorf("지원하지 않는 Instagram 파일 형식입니다: %q", extension)
	}
}

func (file GalleryDLFile) SaveImage(ctx context.Context, stageDir string) (string, error) {
	var metadata struct {
		Extension string `json:"extension"`
	}
	if err := json.Unmarshal(file.Metadata, &metadata); err != nil {
		return "", err
	}
	kind, _, err := galleryDLMediaFormat(metadata.Extension)
	if err != nil {
		return "", err
	}
	if kind != "image" {
		return "", fmt.Errorf("이미지 파일이 필요합니다")
	}
	return file.SaveMedia(ctx, stageDir)
}

func (file GalleryDLFile) SaveMedia(ctx context.Context, stageDir string) (string, error) {
	var metadata struct {
		Extension string            `json:"extension"`
		Headers   map[string]string `json:"_http_headers"`
	}
	if err := json.Unmarshal(file.Metadata, &metadata); err != nil {
		return "", fmt.Errorf("gallery-dl 미디어 메타데이터 해석 실패: %w", err)
	}

	ext := strings.ToLower(metadata.Extension)
	kind, wantFormat, err := galleryDLMediaFormat(ext)
	if err != nil {
		return "", err
	}

	if !filepath.IsAbs(stageDir) {
		return "", fmt.Errorf("작업 폴더는 절대 경로여야 합니다")
	}
	entries, err := os.ReadDir(stageDir)
	if err != nil {
		return "", fmt.Errorf("작업 폴더 읽기 실패: %w", err)
	}
	if len(entries) != 0 {
		return "", fmt.Errorf("작업 폴더가 비어 있어야 합니다")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
	if err != nil {
		return "", fmt.Errorf("gallery-dl 미디어 요청 구성 실패: %w", err)
	}
	if request.URL.Hostname() == "" || request.URL.User != nil ||
		(request.URL.Scheme != "http" && request.URL.Scheme != "https") {
		return "", fmt.Errorf("gallery-dl 미디어 URL이 잘못되었습니다")
	}
	request.Header.Set("User-Agent", "Mozilla/5.0")
	for name, value := range metadata.Headers {
		switch http.CanonicalHeaderKey(name) {
		case "User-Agent", "Referer", "Origin", "Accept", "Accept-Language":
			request.Header.Set(name, value)
		}
	}

	client := http.Client{Timeout: 2 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("gallery-dl 미디어 요청 실패: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if classified := galleryDLFailure("", strconv.Itoa(response.StatusCode)); classified != nil {
			return "", fmt.Errorf("%w (HTTP %d)", classified, response.StatusCode)
		}
		return "", fmt.Errorf("gallery-dl 미디어 요청 실패: HTTP %d", response.StatusCode)
	}

	path := filepath.Join(stageDir, "media."+ext)
	output, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("작업 파일 생성 실패: %w", err)
	}
	saved := false
	defer func() {
		_ = output.Close()
		if !saved {
			_ = os.Remove(path)
		}
	}()

	size, err := io.Copy(output, response.Body)
	if err != nil {
		return "", fmt.Errorf("미디어 저장 실패: %w", err)
	}
	if size == 0 {
		return "", fmt.Errorf("저장된 미디어가 비어 있습니다")
	}
	if _, err := output.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("저장 파일 읽기 준비 실패: %w", err)
	}

	if kind == "image" {
		config, format, err := image.DecodeConfig(output)
		if err != nil {
			return "", fmt.Errorf("저장된 이미지 헤더 해석 실패: %w", err)
		}
		if format != wantFormat || config.Width <= 0 || config.Height <= 0 {
			return "", fmt.Errorf("저장된 이미지 형식 또는 크기가 잘못되었습니다: %s %dx%d", format, config.Width, config.Height)
		}
	} else {
		var header [12]byte
		if _, err := io.ReadFull(output, header[:]); err != nil || string(header[4:8]) != "ftyp" {
			return "", fmt.Errorf("저장된 MP4 파일 헤더가 올바르지 않습니다")
		}
	}
	if err := output.Close(); err != nil {
		return "", fmt.Errorf("미디어 파일 닫기 실패: %w", err)
	}
	saved = true
	return path, nil
}

func (discovery GalleryDLDiscovery) SaveImages(
	ctx context.Context,
	stageDir string,
) ([]string, error) {
	for _, file := range discovery.Files {
		var metadata struct {
			Extension string `json:"extension"`
		}
		if err := json.Unmarshal(file.Metadata, &metadata); err != nil {
			return nil, err
		}
		kind, _, err := galleryDLMediaFormat(metadata.Extension)
		if err != nil {
			return nil, err
		}
		if kind != "image" {
			return nil, fmt.Errorf("이미지 파일이 필요합니다")
		}
	}
	return discovery.SaveMedia(ctx, stageDir)
}

func (discovery GalleryDLDiscovery) SaveMedia(ctx context.Context, stageDir string) ([]string, error) {
	if len(discovery.Files) == 0 {
		return nil, fmt.Errorf("저장할 gallery-dl 미디어가 없습니다")
	}
	if !filepath.IsAbs(stageDir) {
		return nil, fmt.Errorf("작업 폴더는 절대 경로여야 합니다")
	}

	entries, err := os.ReadDir(stageDir)
	if err != nil {
		return nil, fmt.Errorf("작업 폴더 읽기 실패: %w", err)
	}
	if len(entries) != 0 {
		return nil, fmt.Errorf("작업 폴더가 비어 있어야 합니다")
	}

	paths := make([]string, 0, len(discovery.Files))
	for i, file := range discovery.Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		itemDir := filepath.Join(stageDir, fmt.Sprintf("item-%04d", i+1))
		if err := os.Mkdir(itemDir, 0o700); err != nil {
			return nil, fmt.Errorf("gallery-dl %d번째 작업 폴더 생성 실패: %w", i+1, err)
		}
		path, err := file.SaveMedia(ctx, itemDir)
		if err != nil {
			return nil, fmt.Errorf("gallery-dl %d번째 미디어 저장 실패: %w", i+1, err)
		}
		paths = append(paths, path)
	}
	return paths, nil
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
			if err != nil || mediaURL.Hostname() == "" || mediaURL.User != nil ||
				(mediaURL.Scheme != "http" && mediaURL.Scheme != "https") {
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
