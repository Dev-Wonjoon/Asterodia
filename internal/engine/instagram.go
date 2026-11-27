package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var instagramPostPath = regexp.MustCompile(`^/(?:[A-Za-z0-9_.]+/)?(p|reel|reels|tv)/([A-Za-z0-9_-]{1,32})/?$`)

var (
	ErrInstagramURL              = errors.New("Instagram 게시물 또는 릴스 링크를 입력하세요")
	ErrInstagramOptions          = errors.New("Instagram은 원본 사진·영상을 함께 저장합니다. 종류는 영상, 화질은 최고 화질, 형식은 자동으로 설정하세요")
	ErrInstagramPublicExtraction = errors.New("Instagram 공개 게시물 정보는 확인했지만 원본 미디어 주소를 추출하지 못했습니다")
)

type InstagramExtractor struct {
	HTTPClient *http.Client
	deps       ExtractorDependencies
}

func NewInstagramExtractor(deps ExtractorDependencies) *InstagramExtractor {
	return &InstagramExtractor{HTTPClient: deps.HTTPClient, deps: deps}
}
func (*InstagramExtractor) Name() string                { return "instagram" }
func (*InstagramExtractor) Match(address *url.URL) bool { return siteHost(address, "instagram.com") }

func (g *InstagramExtractor) Discover(ctx context.Context, req ExtractRequest) (DownloadPlan, error) {
	kind, err := Select(req.URL, req.Options)
	if err != nil {
		return DownloadPlan{}, err
	}
	generic := NewGenericExtractor(g.deps)
	shortcode, shortcodeErr := instagramShortcode(req.URL)
	authenticated := g.deps.Authenticated
	if g.deps.AuthenticatedFor != nil {
		authenticated = g.deps.AuthenticatedFor(req.URL)
	}
	if kind == KindYtdlp || authenticated || shortcodeErr != nil {
		return generic.Discover(ctx, req)
	}
	handlers := []DiscoveryHandler[DownloadPlan]{
		publicDiscoveryHandler("Instagram 임베드", func(ctx context.Context) (DownloadPlan, error) {
			data, err := g.discoverInstagramEmbed(ctx, shortcode)
			if err != nil {
				return DownloadPlan{}, err
			}
			return directDownloadPlan(req, data, g.downloadInstagramEmbed), nil
		}),
		publicDiscoveryHandler("Instagram 공개 HTML", func(ctx context.Context) (DownloadPlan, error) {
			data, err := g.discoverInstagramPublic(ctx, shortcode)
			if err != nil {
				return DownloadPlan{}, err
			}
			return directDownloadPlan(req, data, g.downloadInstagramEmbed), nil
		}),
		{Name: "CLI", Handle: func(ctx context.Context) DiscoveryStepResult[DownloadPlan] {
			plan, err := generic.Discover(ctx, req)
			decision := DiscoveryHandled
			if err != nil {
				decision = DiscoveryNext
				if discoveryMustStop(err) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					decision = DiscoveryStop
				}
			}
			return DiscoveryStepResult[DownloadPlan]{Decision: decision, Value: plan, Err: err}
		}},
	}
	result := RunDiscoveryChain(ctx, handlers...)
	return result.Value, result.Err
}

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
