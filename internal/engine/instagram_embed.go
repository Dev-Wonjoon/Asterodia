package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Dev-Wonjoon/StorageBucket/internal/netguard"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const instagramEmbedHTMLLimit = 8 << 20

// The minimal agent receives the embeddable payload; a full browser agent can
// instead receive the Instagram app shell without contextJSON.
const instagramEmbedUserAgent = "Mozilla/5.0"

var instagramEmbedInit = regexp.MustCompile(`"init",\s*\[\s*\],\s*\[`)

// Match complete HTML tags, including > inside quoted attributes. The Simple
// embed publishes one photo in these tags instead of a contextJSON payload.
var instagramEmbedTag = regexp.MustCompile(`(?is)</?([a-z][a-z0-9:-]*)\b(?:[^"'<>]|"[^"]*"|'[^']*')*>`)
var instagramEmbedAttribute = regexp.MustCompile("(?is)([a-z_:][a-z0-9_:.-]*)\\s*=\\s*(?:\"([^\"]*)\"|'([^']*)'|([^\\s\"'=<>`]+))")

type instagramEmbedMedia struct {
	ID         json.RawMessage `json:"id"`
	Shortcode  string          `json:"shortcode"`
	TypeName   string          `json:"__typename"`
	IsVideo    *bool           `json:"is_video"`
	DisplayURL string          `json:"display_url"`
	VideoURL   string          `json:"video_url"`
	Owner      struct {
		ID       json.RawMessage `json:"id"`
		Username string          `json:"username"`
		FullName string          `json:"full_name"`
	} `json:"owner"`
	Caption struct {
		Edges []struct {
			Node struct {
				Text string `json:"text"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"edge_media_to_caption"`
	Sidecar *struct {
		Edges []struct {
			Node *instagramEmbedMedia `json:"node"`
		} `json:"edges"`
	} `json:"edge_sidecar_to_children"`
}

// Every request is anonymous, including when a caller supplies a client with a
// cookie jar. Redirects stay within Instagram or its media CDN, respectively.
func (g *GalleryDL) instagramGET(ctx context.Context, address string, media bool) (*http.Response, error) {
	return g.instagramGETWithAgent(ctx, address, media, instagramEmbedUserAgent)
}

func (g *GalleryDL) instagramGETWithAgent(ctx context.Context, address string, media bool, agent string) (*http.Response, error) {
	parsed, err := url.Parse(address)
	if err != nil || !instagramEmbedURLAllowed(parsed, media) {
		return nil, fmt.Errorf("Instagram 응답에 허용되지 않은 주소가 있습니다")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", agent)
	request.Header.Set("Accept", "*/*")
	if !media {
		request.Header.Set("Accept", "text/html")
	}
	client := *netguard.NewClient()
	client.Timeout = 0
	defer client.CloseIdleConnections()
	if g.HTTPClient != nil {
		client = *g.HTTPClient
	}
	client.Jar = nil
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if strings.HasPrefix(next.URL.Path, "/accounts/login") {
			return ErrAuthenticationRequired
		}
		if len(via) >= 5 || !instagramEmbedURLAllowed(next.URL, media) {
			return fmt.Errorf("Instagram 리다이렉트 주소가 잘못되었습니다")
		}
		if previousRedirect != nil {
			return previousRedirect(next, via)
		}
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		// Avoid including signed CDN URLs in diagnostic messages.
		if e, ok := err.(*url.Error); ok {
			err = e.Err
		}
		return nil, fmt.Errorf("Instagram 요청 실패: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		if classified := galleryDLFailure("", fmt.Sprintf("HTTP %d", response.StatusCode)); classified != nil {
			return nil, classified
		}
		return nil, fmt.Errorf("Instagram 요청 실패: HTTP %d", response.StatusCode)
	}
	return response, nil
}

func instagramEmbedURLAllowed(address *url.URL, media bool) bool {
	if address == nil || address.Scheme != "https" || address.User != nil ||
		(address.Port() != "" && address.Port() != "443") {
		return false
	}
	host := strings.ToLower(address.Hostname())
	if !media {
		return host == "www.instagram.com" || host == "instagram.com"
	}
	return strings.HasSuffix(host, ".cdninstagram.com") || strings.HasSuffix(host, ".fbcdn.net")
}

func (g *GalleryDL) discoverInstagramEmbed(ctx context.Context, shortcode string) (GalleryDLDiscovery, error) {
	if err := ctx.Err(); err != nil {
		return GalleryDLDiscovery{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	response, err := g.instagramGET(ctx, "https://www.instagram.com/p/"+shortcode+"/embed/captioned/", false)
	if err != nil {
		return GalleryDLDiscovery{}, err
	}
	defer response.Body.Close()
	page, err := io.ReadAll(io.LimitReader(response.Body, instagramEmbedHTMLLimit+1))
	if err != nil {
		return GalleryDLDiscovery{}, err
	}
	if len(page) > instagramEmbedHTMLLimit {
		return GalleryDLDiscovery{}, ErrOutputLimit
	}
	simple := false
	for _, match := range instagramEmbedInit.FindAllIndex(page, -1) {
		var init struct {
			ContextJSON    string `json:"contextJSON"`
			IsRichEmbed    *bool  `json:"isRichEmbed"`
			IsSidecar      *bool  `json:"isSidecar"`
			IsProfileEmbed bool   `json:"isProfileEmbed"`
			IsGuideEmbed   bool   `json:"isGuideEmbed"`
		}
		decoder := json.NewDecoder(bytes.NewReader(page[match[1]:]))
		if decoder.Decode(&init) != nil {
			continue
		}
		if init.ContextJSON == "" {
			simple = simple || (init.IsRichEmbed != nil && !*init.IsRichEmbed &&
				init.IsSidecar != nil && !*init.IsSidecar && !init.IsProfileEmbed && !init.IsGuideEmbed)
			continue
		}
		var contextData struct {
			GQL struct {
				Media *instagramEmbedMedia `json:"shortcode_media"`
				XDT   *instagramEmbedMedia `json:"xdt_shortcode_media"`
			} `json:"gql_data"`
		}
		if json.Unmarshal([]byte(init.ContextJSON), &contextData) != nil {
			continue
		}
		post := contextData.GQL.Media
		if post == nil {
			post = contextData.GQL.XDT
		}
		if post != nil {
			discovery, err := post.discovery(shortcode)
			if err != nil && post.Shortcode == shortcode && galleryDLNumericID.MatchString(galleryDLIdentifier(post.ID)) {
				return GalleryDLDiscovery{}, fmt.Errorf("%w: %v", ErrInstagramPublicExtraction, err)
			}
			return discovery, err
		}
	}
	if simple {
		return discoverInstagramSimpleEmbed(page, shortcode)
	}
	return GalleryDLDiscovery{}, fmt.Errorf("Instagram 임베드에 게시물 데이터가 없습니다")
}

func instagramEmbedAttributes(tag []byte) map[string]string {
	attrs := make(map[string]string)
	for _, match := range instagramEmbedAttribute.FindAllSubmatch(tag, -1) {
		for _, value := range match[2:] {
			if value != nil {
				attrs[strings.ToLower(string(match[1]))] = html.UnescapeString(string(value))
				break
			}
		}
	}
	return attrs
}

func instagramEmbedHasClass(attrs map[string]string, name string) bool {
	for _, class := range strings.Fields(attrs["class"]) {
		if class == name {
			return true
		}
	}
	return false
}

func discoverInstagramSimpleEmbed(page []byte, shortcode string) (GalleryDLDiscovery, error) {
	var root map[string]string
	var images []string
	username := ""
	var caption strings.Builder
	captionDepth, previousEnd := 0, 0
	captionSeen, skipAuthor := false, false
	for _, match := range instagramEmbedTag.FindAllSubmatchIndex(page, -1) {
		tag := page[match[0]:match[1]]
		name := strings.ToLower(string(page[match[2]:match[3]]))
		closing := tag[1] == '/'
		if captionDepth > 0 && !skipAuthor {
			caption.Write(page[previousEnd:match[0]])
		}
		previousEnd = match[1]
		if closing {
			if name == "div" && captionDepth > 0 {
				captionDepth--
			}
			if name == "a" {
				skipAuthor = false
			}
			continue
		}
		attrs := instagramEmbedAttributes(tag)
		if name == "div" {
			if instagramEmbedHasClass(attrs, "Embed") {
				if root != nil {
					return GalleryDLDiscovery{}, fmt.Errorf("Instagram Simple 임베드에 게시물이 여러 개 있습니다")
				}
				root = attrs
			}
			if captionDepth > 0 {
				captionDepth++
			}
			if instagramEmbedHasClass(attrs, "CaptionComments") {
				captionDepth = 0
			}
			if instagramEmbedHasClass(attrs, "Caption") && !captionSeen {
				captionDepth, captionSeen = 1, true
			}
		}
		if name == "a" {
			if captionDepth > 0 && instagramEmbedHasClass(attrs, "CaptionUsername") {
				skipAuthor = true
			}
			if instagramEmbedHasClass(attrs, "Username") {
				owner, err := url.Parse(attrs["href"])
				if err == nil && instagramEmbedURLAllowed(owner, false) {
					candidate := strings.Trim(owner.Path, "/")
					if candidate != "" && !strings.Contains(candidate, "/") {
						username = candidate
					}
				}
			}
		}
		if name == "br" && captionDepth > 0 && !skipAuthor {
			caption.WriteByte('\n')
		}
		if name == "img" && instagramEmbedHasClass(attrs, "EmbeddedMediaImage") {
			images = append(images, attrs["src"])
		}
	}
	// Simple video/sidecar embeds can contain only a preview image. Never publish
	// that thumbnail as if it were the full video or the complete carousel.
	if root == nil || root["data-media-type"] != "GraphImage" || len(images) != 1 {
		return GalleryDLDiscovery{}, fmt.Errorf("Instagram Simple 임베드에 단일 사진 원본 정보가 없습니다")
	}
	resolved, err := instagramShortcode(root["data-permalink"])
	if err != nil || resolved != shortcode {
		return GalleryDLDiscovery{}, fmt.Errorf("Instagram Simple 임베드의 게시물 링크가 일치하지 않습니다")
	}
	raw, err := json.Marshal(map[string]any{
		"id": root["data-media-id"], "shortcode": shortcode, "__typename": "GraphImage",
		"is_video": false, "display_url": images[0],
		"owner":                 map[string]string{"id": root["data-owner-id"], "username": username},
		"edge_media_to_caption": map[string]any{"edges": []any{map[string]any{"node": map[string]string{"text": strings.TrimSpace(html.UnescapeString(caption.String()))}}}},
	})
	if err != nil {
		return GalleryDLDiscovery{}, err
	}
	var post instagramEmbedMedia
	if err := json.Unmarshal(raw, &post); err != nil {
		return GalleryDLDiscovery{}, err
	}
	return post.discovery(shortcode)
}

func (post *instagramEmbedMedia) discovery(shortcode string) (GalleryDLDiscovery, error) {
	if post.Shortcode != shortcode || !galleryDLNumericID.MatchString(galleryDLIdentifier(post.ID)) {
		return GalleryDLDiscovery{}, fmt.Errorf("Instagram 임베드의 게시물 식별 정보가 일치하지 않습니다")
	}
	nodes := []*instagramEmbedMedia{post}
	if post.TypeName == "GraphSidecar" {
		if post.Sidecar == nil || len(post.Sidecar.Edges) == 0 || len(post.Sidecar.Edges) > 5000 {
			return GalleryDLDiscovery{}, fmt.Errorf("Instagram 임베드의 전체 미디어 목록이 없습니다")
		}
		nodes = nil
		for _, edge := range post.Sidecar.Edges {
			nodes = append(nodes, edge.Node)
		}
	} else if post.TypeName != "GraphImage" && post.TypeName != "GraphVideo" {
		return GalleryDLDiscovery{}, fmt.Errorf("Instagram 임베드의 미디어 유형을 지원하지 않습니다")
	}
	caption := ""
	if len(post.Caption.Edges) > 0 {
		caption = post.Caption.Edges[0].Node.Text
	}
	metadata := instagramMetadata{
		Category: "instagram", Subcategory: "post", Shortcode: shortcode,
		TypeName: post.TypeName, PostID: post.ID, Count: json.RawMessage(fmt.Sprint(len(nodes))),
		OwnerID: post.Owner.ID, Username: post.Owner.Username, FullName: post.Owner.FullName, Description: caption,
	}
	if post.TypeName == "GraphVideo" {
		metadata.Subcategory = "reel"
	}
	directory, err := json.Marshal(metadata)
	if err != nil {
		return GalleryDLDiscovery{}, err
	}
	discovery := GalleryDLDiscovery{Directories: []json.RawMessage{directory}, InstagramEmbed: true}
	for i, node := range nodes {
		if node == nil || node.IsVideo == nil || !galleryDLNumericID.MatchString(galleryDLIdentifier(node.ID)) {
			return GalleryDLDiscovery{}, fmt.Errorf("Instagram 임베드에 개별 미디어 정보가 없습니다")
		}
		address, extension := node.DisplayURL, "jpg"
		if *node.IsVideo {
			// A video without a video URL must never become a thumbnail download.
			address, extension = node.VideoURL, "mp4"
		}
		parsed, err := url.Parse(address)
		if err != nil || !instagramEmbedURLAllowed(parsed, true) {
			return GalleryDLDiscovery{}, fmt.Errorf("Instagram 임베드에 유효한 미디어 주소가 없습니다")
		}
		if !*node.IsVideo {
			if ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(parsed.Path), ".")); ext == "png" {
				extension = ext
			}
		}
		metadata.MediaID, metadata.Num = node.ID, json.RawMessage(fmt.Sprint(i+1))
		metadata.Extension = extension
		raw, err := json.Marshal(metadata)
		if err != nil {
			return GalleryDLDiscovery{}, err
		}
		discovery.Files = append(discovery.Files, GalleryDLFile{URL: address, Metadata: raw})
	}
	if _, err := discovery.InstagramTargets("https://www.instagram.com/p/" + shortcode + "/"); err != nil {
		return GalleryDLDiscovery{}, err
	}
	return discovery, nil
}

func (g *GalleryDL) downloadInstagramEmbed(ctx context.Context, originalURL string, expected GalleryDLDiscovery, stageDir string) (paths []string, resultErr error) {
	targets, err := expected.InstagramTargets(originalURL)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(stageDir) {
		return nil, fmt.Errorf("작업 폴더는 절대 경로여야 합니다")
	}
	entries, err := os.ReadDir(stageDir)
	if err != nil {
		return nil, err
	}
	if len(entries) != 0 {
		return nil, fmt.Errorf("작업 폴더가 비어 있어야 합니다")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var createdFiles, createdDirs []string
	defer func() {
		if resultErr != nil {
			// Remove only files/directories created by this operation, never the stage itself.
			for _, path := range createdFiles {
				_ = os.Remove(path)
			}
			for _, dir := range createdDirs {
				_ = os.Remove(dir)
			}
		}
	}()
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file := expected.Files[target.FileIndex]
		metadata := decodeMetadata(file.Metadata)
		dir := filepath.Join(stageDir, fmt.Sprintf("item-%04d", i+1))
		if err := os.Mkdir(dir, 0700); err != nil {
			return nil, err
		}
		createdDirs = append(createdDirs, dir)
		path := filepath.Join(dir, "media."+metadataText(metadata, "extension"))
		partial := path + ".part"
		out, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		createdFiles = append(createdFiles, partial)
		err = g.saveInstagramMedia(ctx, file.URL, metadataText(metadata, "extension"), out)
		closeErr := out.Close()
		if err != nil {
			return nil, fmt.Errorf("Instagram %d번째 파일 저장 실패: %w", i+1, err)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if err := os.Rename(partial, path); err != nil {
			return nil, err
		}
		createdFiles = append(createdFiles, path)
		paths = append(paths, path)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return paths, nil
}

func (g *GalleryDL) saveInstagramMedia(ctx context.Context, address, extension string, out io.Writer) error {
	response, err := g.instagramGET(ctx, address, true)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	prefix := make([]byte, 512)
	n, err := io.ReadFull(response.Body, prefix)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return err
	}
	prefix = prefix[:n]
	typeName := http.DetectContentType(prefix)
	wantType := map[string]string{"jpg": "image/jpeg", "jpeg": "image/jpeg", "png": "image/png", "mp4": "video/mp4"}[extension]
	if n == 0 || wantType == "" || typeName != wantType {
		return fmt.Errorf("Instagram 응답이 요청한 사진·영상 파일이 아닙니다")
	}
	written, err := io.Copy(out, io.MultiReader(bytes.NewReader(prefix), response.Body))
	if err != nil {
		return err
	}
	if response.ContentLength >= 0 && written != response.ContentLength {
		return io.ErrUnexpectedEOF
	}
	return ctx.Err()
}
