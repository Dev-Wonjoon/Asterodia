package engine

import (
	"bytes"
	"context"
	"encoding/json"
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
	instagramPostPath  = regexp.MustCompile(`^/(p|reel)/([A-Za-z0-9_-]{1,32})/?$`)
	galleryDLID        = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	galleryDLNumericID = regexp.MustCompile(`^[1-9][0-9]*$`)
)

func (discovery GalleryDLDiscovery) InstagramTargetKey(originalURL string) (string, error) {
	if len(discovery.Directories) != 1 || len(discovery.Files) != 1 {
		return "", fmt.Errorf("Instagram 단일 게시물에는 디렉터리와 파일이 각각 한 건이어야 합니다")
	}

	page, err := url.Parse(originalURL)
	if err != nil {
		return "", fmt.Errorf("Instagram 게시물 URL 해석 실패: %w", err)
	}
	host := strings.ToLower(page.Hostname())
	match := instagramPostPath.FindStringSubmatch(page.EscapedPath())
	if (host != "instagram.com" && host != "www.instagram.com") || len(match) == 0 {
		return "", fmt.Errorf("Instagram 게시물 shortcode를 확인할 수 없습니다")
	}
	shortcode := match[2]

	type instagramMetadata struct {
		Category    string          `json:"category"`
		Subcategory string          `json:"subcategory"`
		Shortcode   string          `json:"post_shortcode"`
		TypeName    string          `json:"typename"`
		PostID      json.RawMessage `json:"post_id"`
		MediaID     json.RawMessage `json:"media_id"`
		Count       json.RawMessage `json:"count"`
	}

	var directory, file instagramMetadata
	if err := json.Unmarshal(discovery.Directories[0], &directory); err != nil {
		return "", fmt.Errorf("Instagram 디렉터리 메타데이터 해석 실패: %w", err)
	}
	if err := json.Unmarshal(discovery.Files[0].Metadata, &file); err != nil {
		return "", fmt.Errorf("Instagram 파일 메타데이터 해석 실패: %w", err)
	}

	for _, metadata := range []instagramMetadata{directory, file} {
		if metadata.Category != "instagram" ||
			(metadata.Subcategory != "post" && metadata.Subcategory != "reel") ||
			metadata.Shortcode != shortcode ||
			galleryDLIdentifier(metadata.Count) != "1" {
			return "", fmt.Errorf("Instagram 단일 게시물의 식별 정보가 일치하지 않습니다")
		}
	}
	if (directory.TypeName != "GraphImage" && directory.TypeName != "GraphVideo") ||
		file.TypeName != directory.TypeName {
		return "", fmt.Errorf("Instagram 단일 미디어 유형을 확인할 수 없습니다")
	}

	postID := galleryDLIdentifier(directory.PostID)
	filePostID := galleryDLIdentifier(file.PostID)
	mediaID := galleryDLIdentifier(file.MediaID)
	if postID == "" || filePostID != postID || mediaID != postID {
		return "", fmt.Errorf("Instagram 게시물 ID와 미디어 ID가 일치하지 않습니다")
	}

	return "instagram:" + postID + ":" + mediaID, nil
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
	if err != nil {
		if stderr != "" {
			return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl 대상 조회 실패: %w\n%s", err, stderr)
		}
		return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl 대상 조회 실패: %w", err)
	}
	if stderr != "" {
		return GalleryDLDiscovery{}, fmt.Errorf("gallery-dl 조회 중 경고 또는 오류:\n%s", stderr)
	}

	return ParseGalleryDLDiscovery([]byte(result.Stdout))
}

func (file GalleryDLFile) SaveImage(ctx context.Context, stageDir string) (string, error) {
	var metadata struct {
		Extension string `json:"extension"`
	}
	if err := json.Unmarshal(file.Metadata, &metadata); err != nil {
		return "", fmt.Errorf("gallery-dl 이미지 메타데이터 해석 실패: %w", err)
	}

	ext := strings.ToLower(metadata.Extension)
	var wantFormat string
	switch ext {
	case "jpg", "jpeg":
		wantFormat = "jpeg"
	case "png":
		wantFormat = "png"
	default:
		return "", fmt.Errorf("지원하지 않는 gallery-dl 이미지 확장자: %q", ext)
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
		return "", fmt.Errorf("gallery-dl 이미지 요청 구성 실패: %w", err)
	}
	if request.URL.Hostname() == "" || request.URL.User != nil ||
		(request.URL.Scheme != "http" && request.URL.Scheme != "https") {
		return "", fmt.Errorf("gallery-dl 이미지 URL이 잘못되었습니다")
	}

	client := http.Client{Timeout: 2 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("gallery-dl 이미지 요청 실패: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gallery-dl 이미지 요청 실패: HTTP %d", response.StatusCode)
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
		return "", fmt.Errorf("이미지 저장 실패: %w", err)
	}
	if size == 0 {
		return "", fmt.Errorf("저장된 이미지가 비어 있습니다")
	}
	if _, err := output.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("저장 파일 읽기 준비 실패: %w", err)
	}

	config, format, err := image.DecodeConfig(output)
	if err != nil {
		return "", fmt.Errorf("저장된 이미지 헤더 해석 실패: %w", err)
	}
	if format != wantFormat || config.Width <= 0 || config.Height <= 0 {
		return "", fmt.Errorf("저장된 이미지 형식 또는 크기가 잘못되었습니다: %s %dx%d",
			format, config.Width, config.Height)
	}
	if err := output.Close(); err != nil {
		return "", fmt.Errorf("이미지 파일 닫기 실패: %w", err)
	}
	saved = true
	return path, nil
}

func (discovery GalleryDLDiscovery) SaveImages(
	ctx context.Context,
	stageDir string,
) ([]string, error) {
	if len(discovery.Files) == 0 {
		return nil, fmt.Errorf("저장할 gallery-dl 이미지가 없습니다")
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
		path, err := file.SaveImage(ctx, itemDir)
		if err != nil {
			return nil, fmt.Errorf("gallery-dl %d번째 이미지 저장 실패: %w", i+1, err)
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
