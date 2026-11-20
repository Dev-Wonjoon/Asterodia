package engine

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

var ErrUnsupportedURL = errors.New("설치된 엔진이 이 주소를 지원하지 않습니다")

// MediaTarget is the app-facing projection of an extractor result. Transport
// data stays with the engine; this projection only describes library records.
type MediaTarget struct {
	TargetKey, PostKey, SourceURL, Kind, Title, ExternalID string
	PlatformName, OwnerID, OwnerName                       string
	FileIndex                                              int
}

func metadataText(data map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw := data[key]
		var value string
		if json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
		if galleryDLNumericID.Match(raw) {
			return string(raw)
		}
	}
	return ""
}

func decodeMetadata(raw json.RawMessage) map[string]json.RawMessage {
	var data map[string]json.RawMessage
	_ = json.Unmarshal(raw, &data)
	return data
}

// Targets uses extractor-provided category and identity fields, never the
// input hostname to reinterpret one site's result as another site's schema.
func (d GalleryDLDiscovery) Targets(originalURL string) ([]MediaTarget, error) {
	if len(d.Files) == 0 {
		return nil, fmt.Errorf("엔진 추출 결과에 미디어가 없습니다")
	}
	directory := map[string]json.RawMessage{}
	if len(d.Directories) > 0 {
		directory = decodeMetadata(d.Directories[0])
	}
	category := metadataText(directory, "category")
	if category == "instagram" {
		// Use the extractor's resolved shortcode, including for share/redirect
		// links. The original user URL remains the source stored in the library.
		resolved := "https://www.instagram.com/p/" + url.PathEscape(metadataText(directory, "post_shortcode")) + "/"
		items, err := d.InstagramTargets(resolved)
		if err != nil {
			return nil, err
		}
		targets := make([]MediaTarget, 0, len(items))
		for _, item := range items {
			parts := strings.Split(item.TargetKey, ":")
			targets = append(targets, MediaTarget{TargetKey: item.TargetKey, PostKey: strings.Join(parts[:2], ":"), SourceURL: originalURL, Kind: item.Kind, Title: item.Title, ExternalID: item.MediaID, PlatformName: "Instagram", OwnerID: item.OwnerID, OwnerName: item.OwnerName, FileIndex: item.FileIndex})
		}
		return targets, nil
	}
	if len(d.Directories) > 1 {
		return nil, fmt.Errorf("단일 게시글 링크가 필요합니다. 프로필·타임라인 전체 수집은 지원하지 않습니다")
	}
	targets := make([]MediaTarget, 0, len(d.Files))
	seen := make(map[string]bool)
	postKey := ""
	for i, file := range d.Files {
		data := make(map[string]json.RawMessage, len(directory)+8)
		for key, value := range directory {
			data[key] = value
		}
		for key, value := range decodeMetadata(file.Metadata) {
			data[key] = value
		}
		cat := metadataText(data, "category")
		if cat == "" {
			return nil, fmt.Errorf("추출 결과에 사이트 식별 정보가 없습니다")
		}
		// A generic `id` can name a file rather than its post. Only explicit
		// post fields define a post identity; otherwise the source link does.
		postID := metadataText(data, "tweet_id", "post_id", "status_id", "submission_id", "post_shortcode", "shortcode", "uri")
		if postID == "" {
			postID = fmt.Sprintf("url-%x", sha256.Sum256([]byte(originalURL)))
		}
		post := cat + ":" + postID
		if postKey != "" && post != postKey {
			return nil, fmt.Errorf("단일 게시글 결과에 서로 다른 게시글이 포함됐습니다")
		}
		postKey = post
		ext := canonicalMediaExtension(metadataText(data, "extension"))
		kind := LocalKind("media." + ext)
		if kind == "" {
			return nil, fmt.Errorf("앱에서 등록할 수 없는 미디어 형식입니다: %q", ext)
		}
		id := metadataText(data, "media_id", "image_id", "video_id", "filename", "id")
		if id == "" {
			return nil, fmt.Errorf("추출 결과에 파일 식별 정보가 없습니다")
		}
		identity := id + ":" + ext
		if position := metadataText(data, "num", "index", "position"); position != "" {
			identity += ":" + position
		}
		key := fmt.Sprintf("gallery:%s:%x", cat, sha256.Sum256([]byte(post+":"+identity)))
		if seen[key] {
			return nil, fmt.Errorf("추출 결과에 중복 파일 식별 정보가 있습니다")
		}
		seen[key] = true
		title := metadataText(data, "title", "content", "caption", "description", "filename")
		if title == "" {
			title = cat + " " + postID
		}
		owner := decodeMetadata(data["author"])
		if len(owner) == 0 {
			owner = decodeMetadata(data["user"])
		}
		ownerID := metadataText(data, "owner_id", "user_id", "author_id", "username", "uploader_id")
		if ownerID == "" {
			ownerID = metadataText(owner, "id", "id_str", "user_id", "did", "screen_name", "handle")
		}
		ownerName := metadataText(data, "fullname", "uploader", "username", "author")
		if ownerName == "" {
			ownerName = metadataText(owner, "name", "nick", "display_name", "screen_name", "handle")
		}
		platform := strings.ToUpper(cat[:1]) + cat[1:]
		targets = append(targets, MediaTarget{TargetKey: key, PostKey: post, SourceURL: originalURL, Kind: kind, Title: title, ExternalID: id, PlatformName: platform, OwnerID: ownerID, OwnerName: ownerName, FileIndex: i})
	}
	return targets, nil
}

func mediaExtension(path string) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
}

func canonicalMediaExtension(extension string) string {
	switch ext := strings.ToLower(extension); ext {
	case "jpg", "jpe":
		return "jpeg"
	case "tif":
		return "tiff"
	default:
		return ext
	}
}

func mergeTransportJSON(raw map[string]json.RawMessage, projected any, remove ...string) ([]byte, error) {
	merged := make(map[string]json.RawMessage, len(raw)+8)
	for key, value := range raw {
		merged[key] = value
	}
	for _, key := range remove {
		delete(merged, key)
	}
	known, err := json.Marshal(projected)
	if err != nil {
		return nil, err
	}
	for key, value := range decodeMetadata(known) {
		merged[key] = value
	}
	return json.Marshal(merged)
}

func (f *ytdlpDirectFormat) UnmarshalJSON(data []byte) error {
	type plain ytdlpDirectFormat
	if err := json.Unmarshal(data, (*plain)(f)); err != nil {
		return err
	}
	f.raw = decodeMetadata(data)
	return nil
}
func (f ytdlpDirectFormat) MarshalJSON() ([]byte, error) {
	type plain ytdlpDirectFormat
	return mergeTransportJSON(f.raw, plain(f), "http_headers", "downloader_options", "filepath", "filename", "_filename")
}
func (i *ytdlpDownloadInfo) UnmarshalJSON(data []byte) error {
	type plain ytdlpDownloadInfo
	if err := json.Unmarshal(data, (*plain)(i)); err != nil {
		return err
	}
	i.raw = decodeMetadata(data)
	return nil
}
func (i ytdlpDownloadInfo) MarshalJSON() ([]byte, error) {
	type plain ytdlpDownloadInfo
	return mergeTransportJSON(i.raw, plain(i), "requested_downloads", "requested_formats", "requested_entries", "entries", "filepath", "filename", "_filename", "webpage_url", "original_url", "url", "format_id")
}

func (o *ytdlpDownloaderOptions) UnmarshalJSON(data []byte) error {
	type plain ytdlpDownloaderOptions
	if err := json.Unmarshal(data, (*plain)(o)); err != nil {
		return err
	}
	o.raw = decodeMetadata(data)
	return nil
}
func (o ytdlpDownloaderOptions) MarshalJSON() ([]byte, error) {
	type plain ytdlpDownloaderOptions
	return mergeTransportJSON(o.raw, plain(o), "http_chunk_size")
}
