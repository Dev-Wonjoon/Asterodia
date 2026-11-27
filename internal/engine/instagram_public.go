package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Public link previews receive the logged-out media preloader rather than the
// browser app shell. Never attach account cookies or credentials to this path.
const instagramPublicUserAgent = "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)"

type instagramPublicCandidate struct {
	URL    string `json:"url"`
	Width  int64  `json:"width"`
	Height int64  `json:"height"`
}

type instagramPublicMedia struct {
	PK        json.RawMessage `json:"pk"`
	Code      string          `json:"code"`
	MediaType int             `json:"media_type"`
	User      struct {
		PK       json.RawMessage `json:"pk"`
		ID       json.RawMessage `json:"id"`
		Username string          `json:"username"`
		FullName string          `json:"full_name"`
	} `json:"user"`
	Caption *struct {
		Text string `json:"text"`
	} `json:"caption"`
	Images struct {
		Candidates []instagramPublicCandidate `json:"candidates"`
	} `json:"image_versions2"`
	Videos        []instagramPublicCandidate `json:"video_versions"`
	Carousel      []*instagramPublicMedia    `json:"carousel_media"`
	CarouselCount *int                       `json:"carousel_media_count"`
}

func (g *InstagramExtractor) discoverInstagramPublic(ctx context.Context, shortcode string) (GalleryDLDiscovery, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	response, err := g.instagramGETWithAgent(ctx, "https://www.instagram.com/p/"+shortcode+"/", false, instagramPublicUserAgent)
	if err != nil {
		return GalleryDLDiscovery{}, err
	}
	defer response.Body.Close()
	page, err := io.ReadAll(io.LimitReader(response.Body, publicMediaHTMLLimit+1))
	if err != nil {
		return GalleryDLDiscovery{}, err
	}
	if len(page) > publicMediaHTMLLimit {
		return GalleryDLDiscovery{}, ErrOutputLimit
	}
	return parseInstagramPublic(page, shortcode)
}

// Read inert JSON preloaders only. Executable scripts, captions containing JSON,
// and media belonging to recommendations must never become download targets.
func parseInstagramPublic(page []byte, shortcode string) (GalleryDLDiscovery, error) {
	tokenizer := html.NewTokenizer(bytes.NewReader(page))
	var matched *GalleryDLDiscovery
	var matchedErr error
	recordError := func(err error) {
		if !errors.Is(matchedErr, ErrInstagramPublicExtraction) {
			matchedErr = err
		}
	}
	budget := 100000
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
			if root, ok := node["xig_polaris_media"].(map[string]any); ok && root["code"] == shortcode {
				rawRoot, err := json.Marshal(root)
				if err != nil {
					return
				}
				var envelope struct {
					PK    json.RawMessage       `json:"pk"`
					Code  string                `json:"code"`
					Media *instagramPublicMedia `json:"if_not_gated_logged_out"`
				}
				if json.Unmarshal(rawRoot, &envelope) != nil {
					return
				}
				if envelope.Media == nil {
					recordError(ErrAuthenticationRequired)
					return
				}
				post := envelope.Media
				if envelope.Code != post.Code || !galleryDLNumericID.MatchString(galleryDLIdentifier(envelope.PK)) || galleryDLIdentifier(envelope.PK) != galleryDLIdentifier(post.PK) {
					recordError(fmt.Errorf("Instagram 공개 응답의 게시물 식별 정보가 일치하지 않습니다"))
					return
				}
				embed, err := post.embed()
				if err != nil {
					recordError(fmt.Errorf("%w: %v", ErrInstagramPublicExtraction, err))
					return
				}
				discovery, err := embed.discovery(shortcode)
				if err != nil {
					recordError(fmt.Errorf("%w: %v", ErrInstagramPublicExtraction, err))
					return
				}
				matched = &discovery
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
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			if tokenizer.Err() != io.EOF {
				return GalleryDLDiscovery{}, fmt.Errorf("Instagram 공개 HTML을 읽지 못했습니다")
			}
			if matched != nil {
				return *matched, nil
			}
			if budget < 0 {
				return GalleryDLDiscovery{}, ErrOutputLimit
			}
			if matchedErr != nil {
				return GalleryDLDiscovery{}, matchedErr
			}
			return GalleryDLDiscovery{}, fmt.Errorf("Instagram 공개 HTML에 해당 게시물의 미디어 정보가 없습니다")
		case html.StartTagToken:
			tag := tokenizer.Token()
			if tag.Data != "script" {
				continue
			}
			jsonScript := false
			for _, attr := range tag.Attr {
				if attr.Key == "type" && strings.EqualFold(attr.Val, "application/json") {
					jsonScript = true
				}
			}
			if !jsonScript || tokenizer.Next() != html.TextToken {
				continue
			}
			decoder := json.NewDecoder(bytes.NewReader(tokenizer.Text()))
			decoder.UseNumber()
			var data any
			if decoder.Decode(&data) != nil || decoder.Decode(new(any)) != io.EOF {
				continue
			}
			walk(data, 0)
		}
	}
}

func instagramPublicURL(candidates []instagramPublicCandidate) string {
	var best string
	var pixels int64 = -1
	for _, candidate := range candidates {
		address, err := url.Parse(candidate.URL)
		if err != nil || !instagramEmbedURLAllowed(address, true) || candidate.Width < 0 || candidate.Height < 0 || candidate.Width > 100000 || candidate.Height > 100000 {
			continue
		}
		area := candidate.Width * candidate.Height
		if best == "" || area > pixels {
			best = candidate.URL
			pixels = area
		}
	}
	return best
}

func (post *instagramPublicMedia) embed() (*instagramEmbedMedia, error) {
	if post == nil || !galleryDLNumericID.MatchString(galleryDLIdentifier(post.PK)) {
		return nil, fmt.Errorf("Instagram 공개 응답에 미디어 식별 정보가 없습니다")
	}
	node := &instagramEmbedMedia{ID: post.PK, Shortcode: post.Code}
	isVideo := false
	switch post.MediaType {
	case 1:
		node.TypeName = "GraphImage"
		node.DisplayURL = instagramPublicURL(post.Images.Candidates)
		if node.DisplayURL == "" {
			return nil, fmt.Errorf("Instagram 공개 응답에 유효한 사진 주소가 없습니다")
		}
	case 2:
		isVideo = true
		node.TypeName = "GraphVideo"
		node.VideoURL = instagramPublicURL(post.Videos)
		if node.VideoURL == "" {
			return nil, fmt.Errorf("Instagram 공개 응답에 원본 영상 주소가 없습니다")
		}
	case 8:
		node.TypeName = "GraphSidecar"
		if post.CarouselCount == nil || *post.CarouselCount < 1 || *post.CarouselCount > 5000 || len(post.Carousel) != *post.CarouselCount {
			return nil, fmt.Errorf("Instagram 공개 응답의 전체 미디어 목록이 없습니다")
		}
		// Reuse the existing complete-carousel validation and target identities.
		var rawEdges []map[string]any
		for _, item := range post.Carousel {
			if item == nil || item.MediaType == 8 {
				return nil, fmt.Errorf("Instagram 공개 응답의 앨범 항목이 잘못되었습니다")
			}
			child, err := item.embed()
			if err != nil {
				return nil, err
			}
			rawEdges = append(rawEdges, map[string]any{"node": child})
		}
		raw, _ := json.Marshal(map[string]any{"edge_sidecar_to_children": map[string]any{"edges": rawEdges}})
		if err := json.Unmarshal(raw, node); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("Instagram 공개 응답의 미디어 유형을 지원하지 않습니다")
	}
	node.IsVideo = &isVideo
	node.Owner.ID = post.User.PK
	if len(node.Owner.ID) == 0 {
		node.Owner.ID = post.User.ID
	}
	node.Owner.Username = post.User.Username
	node.Owner.FullName = post.User.FullName
	if post.Caption != nil {
		raw, _ := json.Marshal(map[string]any{"edge_media_to_caption": map[string]any{"edges": []any{map[string]any{"node": map[string]string{"text": post.Caption.Text}}}}})
		if err := json.Unmarshal(raw, node); err != nil {
			return nil, err
		}
	}
	return node, nil
}
