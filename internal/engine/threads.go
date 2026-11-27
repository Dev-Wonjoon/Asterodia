package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var (
	threadsPostPath            = regexp.MustCompile(`^/(?:@[A-Za-z0-9_.]+/post|t)/([A-Za-z0-9_-]{1,64})/?$`)
	threadsSharePath           = regexp.MustCompile(`^/share/[A-Za-z0-9_-]{1,64}/?$`)
	ErrThreadsURL              = errors.New("Threads 게시물 링크를 입력하세요")
	ErrThreadsPublicExtraction = errors.New("Threads 공개 HTML에서 해당 게시물의 원본 미디어를 추출하지 못했습니다")
)

const threadsPublicUserAgent = "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"

type ThreadsExtractor struct {
	HTTPClient *http.Client
	deps       ExtractorDependencies
}

func NewThreadsExtractor(deps ExtractorDependencies) *ThreadsExtractor {
	return &ThreadsExtractor{HTTPClient: deps.HTTPClient, deps: deps}
}
func (*ThreadsExtractor) Name() string { return "threads" }
func (*ThreadsExtractor) Match(address *url.URL) bool {
	return siteHost(address, "threads.com", "threads.net")
}

func threadsShortcode(raw string) (string, error) {
	address, err := url.Parse(raw)
	if err != nil || !siteHost(address, "threads.com", "threads.net") || address.User != nil ||
		(address.Scheme != "http" && address.Scheme != "https") {
		return "", ErrThreadsURL
	}
	if match := threadsPostPath.FindStringSubmatch(address.EscapedPath()); len(match) > 0 {
		return match[1], nil
	}
	if threadsSharePath.MatchString(address.EscapedPath()) {
		return "", nil
	}
	return "", ErrThreadsURL
}

func (g *ThreadsExtractor) Discover(ctx context.Context, req ExtractRequest) (DownloadPlan, error) {
	if _, err := Select(req.URL, req.Options); err != nil {
		return DownloadPlan{}, err
	}
	code, err := threadsShortcode(req.URL)
	if err != nil {
		return DownloadPlan{}, err
	}
	if req.Options.Type == "audio" || req.Options.Quality == "audio" ||
		(req.Options.Quality != "" && req.Options.Quality != "best") || req.Options.Extension != "" {
		return DownloadPlan{}, fmt.Errorf("%w: Threads는 최고 화질·자동 형식으로 원본 사진과 영상을 함께 저장합니다", ErrUnsupportedMediaFormat)
	}
	public := publicDiscoveryHandler("Threads 공개 HTML", func(ctx context.Context) (DownloadPlan, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		address, _ := url.Parse(req.URL)
		address.Scheme = "https"
		response, err := g.get(ctx, address.String(), false)
		if err != nil {
			return DownloadPlan{}, err
		}
		defer response.Body.Close()
		page, err := io.ReadAll(io.LimitReader(response.Body, publicMediaHTMLLimit+1))
		if err != nil {
			return DownloadPlan{}, err
		}
		if len(page) > publicMediaHTMLLimit {
			return DownloadPlan{}, ErrOutputLimit
		}
		resolved := code
		if resolved == "" && response.Request != nil {
			resolved, _ = threadsShortcode(response.Request.URL.String())
		}
		data, err := parseThreadsPublic(page, resolved)
		if err != nil {
			return DownloadPlan{}, err
		}
		return directDownloadPlan(req, data, g.download), nil
	})
	generic := NewGenericExtractor(g.deps)
	result := RunDiscoveryChain(ctx, public, DiscoveryHandler[DownloadPlan]{Name: "CLI", Handle: func(ctx context.Context) DiscoveryStepResult[DownloadPlan] {
		plan, err := generic.Discover(ctx, req)
		decision := DiscoveryHandled
		if err != nil {
			decision = DiscoveryStop
			if errors.Is(err, ErrUnsupportedURL) && !discoveryMustStop(err) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				decision = DiscoveryNext
			}
		}
		return DiscoveryStepResult[DownloadPlan]{Decision: decision, Value: plan, Err: err}
	}})
	return result.Value, result.Err
}

func threadsURLAllowed(address *url.URL, media bool) bool {
	if address == nil || address.Scheme != "https" || address.User != nil ||
		(address.Port() != "" && address.Port() != "443") {
		return false
	}
	if !media {
		return siteHost(address, "threads.com", "threads.net")
	}
	host := strings.ToLower(address.Hostname())
	return strings.HasSuffix(host, ".cdninstagram.com") || strings.HasSuffix(host, ".fbcdn.net")
}

func (g *ThreadsExtractor) get(ctx context.Context, address string, media bool) (*http.Response, error) {
	return anonymousMediaGET(ctx, g.HTTPClient, address, media, threadsPublicUserAgent, "Threads", threadsURLAllowed)
}

func (g *ThreadsExtractor) download(ctx context.Context, original string, data GalleryDLDiscovery, dir string) ([]string, error) {
	return downloadDirectMedia(ctx, original, data, dir, "Threads", func(ctx context.Context, address, ext string, out io.Writer) error {
		return saveAnonymousMedia(ctx, address, ext, out, g.get, "Threads")
	})
}

type threadsCandidate struct {
	URL    string `json:"url"`
	Width  int64  `json:"width"`
	Height int64  `json:"height"`
}

type threadsMedia struct {
	PK        json.RawMessage `json:"pk"`
	Code      string          `json:"code"`
	MediaType int             `json:"media_type"`
	Images    struct {
		Candidates []threadsCandidate `json:"candidates"`
	} `json:"image_versions2"`
	Videos        []threadsCandidate `json:"video_versions"`
	Carousel      []*threadsMedia    `json:"carousel_media"`
	CarouselCount *int               `json:"carousel_media_count"`
	Caption       *struct {
		Text string `json:"text"`
	} `json:"caption"`
	User struct {
		PK       json.RawMessage `json:"pk"`
		ID       json.RawMessage `json:"id"`
		Username string          `json:"username"`
	} `json:"user"`
}

// Only inert JSON and an exact post shortcode may identify media. Replies,
// recommendations, executable scripts and quoted posts are never substitutes.
func parseThreadsPublic(page []byte, code string) (GalleryDLDiscovery, error) {
	var scripts [][]byte
	canonical := ""
	tokens := html.NewTokenizer(bytes.NewReader(page))
	for {
		kind := tokens.Next()
		if kind == html.ErrorToken {
			if tokens.Err() != io.EOF {
				return GalleryDLDiscovery{}, ErrThreadsPublicExtraction
			}
			break
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		tag := tokens.Token()
		attrs := map[string]string{}
		for _, attr := range tag.Attr {
			attrs[attr.Key] = attr.Val
		}
		candidate := ""
		if tag.Data == "meta" && attrs["property"] == "og:url" {
			candidate = attrs["content"]
		}
		if tag.Data == "link" && attrs["rel"] == "canonical" {
			candidate = attrs["href"]
		}
		if candidate != "" && code == "" {
			if resolved, err := threadsShortcode(candidate); err == nil && resolved != "" {
				if canonical != "" && canonical != resolved {
					return GalleryDLDiscovery{}, ErrThreadsPublicExtraction
				}
				canonical = resolved
			}
		}
		if tag.Data == "script" && strings.EqualFold(attrs["type"], "application/json") && tokens.Next() == html.TextToken {
			scripts = append(scripts, append([]byte(nil), tokens.Text()...))
		}
	}
	if code == "" {
		code = canonical
	}
	if code == "" {
		return GalleryDLDiscovery{}, ErrThreadsPublicExtraction
	}
	budget := 100000
	var matched *GalleryDLDiscovery
	var walk func(any, int)
	walk = func(value any, depth int) {
		budget--
		if depth > 128 {
			budget = -1
		}
		if budget < 0 || matched != nil {
			return
		}
		switch node := value.(type) {
		case map[string]any:
			if candidate, ok := node["code"].(string); ok && candidate != "" {
				if candidate != code {
					return
				}
				raw, _ := json.Marshal(node)
				var post threadsMedia
				if json.Unmarshal(raw, &post) == nil {
					if data, err := post.discovery(); err == nil {
						matched = &data
						return
					}
				}
				// A failed carousel/text post must not be replaced by one of its
				// children, even if that child repeats its parent's shortcode.
				return
			}
			for _, child := range node {
				walk(child, depth+1)
			}
		case []any:
			for _, child := range node {
				walk(child, depth+1)
			}
		}
	}
	for _, script := range scripts {
		decoder := json.NewDecoder(bytes.NewReader(script))
		decoder.UseNumber()
		var data any
		if decoder.Decode(&data) == nil && decoder.Decode(new(any)) == io.EOF {
			walk(data, 0)
		}
		if matched != nil {
			return *matched, nil
		}
		if budget < 0 {
			return GalleryDLDiscovery{}, ErrOutputLimit
		}
	}
	return GalleryDLDiscovery{}, ErrThreadsPublicExtraction
}

func threadsMediaURL(candidates []threadsCandidate) string {
	best, pixels := "", int64(-1)
	for _, candidate := range candidates {
		address, err := url.Parse(candidate.URL)
		if err != nil || !threadsURLAllowed(address, true) || candidate.Width < 0 || candidate.Height < 0 || candidate.Width > 100000 || candidate.Height > 100000 {
			continue
		}
		area := candidate.Width * candidate.Height
		if best == "" || area > pixels {
			best, pixels = candidate.URL, area
		}
	}
	return best
}

func (post *threadsMedia) discovery() (GalleryDLDiscovery, error) {
	postID := galleryDLIdentifier(post.PK)
	if !galleryDLNumericID.MatchString(postID) || post.Code == "" {
		return GalleryDLDiscovery{}, ErrThreadsPublicExtraction
	}
	nodes := []*threadsMedia{post}
	if post.MediaType == 8 {
		// Threads may omit the count; the complete array is then authoritative.
		// An explicitly supplied count must still agree, and every item below
		// must contain its original media rather than a gated placeholder.
		if len(post.Carousel) < 1 || len(post.Carousel) > 5000 || (post.CarouselCount != nil && len(post.Carousel) != *post.CarouselCount) {
			return GalleryDLDiscovery{}, ErrThreadsPublicExtraction
		}
		nodes = post.Carousel
	}
	title := "Threads " + post.Code
	if post.Caption != nil && strings.TrimSpace(post.Caption.Text) != "" {
		title = strings.TrimSpace(post.Caption.Text)
	}
	owner := galleryDLIdentifier(post.User.PK)
	if owner == "" {
		owner = galleryDLIdentifier(post.User.ID)
	}
	base := map[string]any{"category": "threads", "subcategory": "post", "post_id": postID, "post_shortcode": post.Code,
		"count": len(nodes), "title": title, "owner_id": owner, "username": post.User.Username}
	directory, _ := json.Marshal(base)
	data := GalleryDLDiscovery{Directories: []json.RawMessage{directory}}
	for i, node := range nodes {
		if node == nil || !galleryDLNumericID.MatchString(galleryDLIdentifier(node.PK)) {
			return GalleryDLDiscovery{}, ErrThreadsPublicExtraction
		}
		address, ext := "", "jpg"
		switch node.MediaType {
		case 1:
			address = threadsMediaURL(node.Images.Candidates)
			if parsed, err := url.Parse(address); err == nil && strings.EqualFold(filepath.Ext(parsed.Path), ".png") {
				ext = "png"
			}
		case 2:
			address, ext = threadsMediaURL(node.Videos), "mp4"
		default:
			return GalleryDLDiscovery{}, ErrThreadsPublicExtraction
		}
		if address == "" {
			return GalleryDLDiscovery{}, ErrThreadsPublicExtraction
		}
		base["media_id"], base["num"], base["extension"] = galleryDLIdentifier(node.PK), i+1, ext
		metadata, _ := json.Marshal(base)
		data.Files = append(data.Files, GalleryDLFile{URL: address, Metadata: metadata})
	}
	if _, err := data.threadsTargets(""); err != nil {
		return GalleryDLDiscovery{}, err
	}
	return data, nil
}

func (d GalleryDLDiscovery) threadsTargets(original string) ([]MediaTarget, error) {
	if len(d.Directories) != 1 || len(d.Files) == 0 {
		return nil, ErrThreadsPublicExtraction
	}
	directory := decodeMetadata(d.Directories[0])
	postID, code := metadataText(directory, "post_id"), metadataText(directory, "post_shortcode")
	if !galleryDLNumericID.MatchString(postID) || !galleryDLID.MatchString(code) || metadataText(directory, "count") != strconv.Itoa(len(d.Files)) {
		return nil, ErrThreadsPublicExtraction
	}
	if original != "" {
		requested, err := threadsShortcode(original)
		if err != nil || (requested != "" && requested != code) {
			return nil, ErrThreadsPublicExtraction
		}
	}
	seen := map[string]bool{}
	var targets []MediaTarget
	for i, file := range d.Files {
		data := decodeMetadata(file.Metadata)
		id := metadataText(data, "media_id")
		if metadataText(data, "category") != "threads" || metadataText(data, "post_id") != postID || metadataText(data, "post_shortcode") != code ||
			metadataText(data, "count") != strconv.Itoa(len(d.Files)) || metadataText(data, "num") != strconv.Itoa(i+1) || !galleryDLNumericID.MatchString(id) || seen[id] {
			return nil, ErrThreadsPublicExtraction
		}
		kind, _, err := galleryDLMediaFormat(metadataText(data, "extension"))
		if err != nil {
			return nil, err
		}
		seen[id] = true
		targets = append(targets, MediaTarget{TargetKey: "threads:" + postID + ":" + id, PostKey: "threads:" + postID, SourceURL: original,
			Kind: kind, Title: metadataText(directory, "title"), ExternalID: id, PlatformName: "Threads",
			OwnerID: metadataText(directory, "owner_id"), OwnerName: metadataText(directory, "username"), FileIndex: i})
	}
	return targets, nil
}
