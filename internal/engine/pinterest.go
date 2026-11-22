package engine

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

var pinterestDomains = []string{"pinterest.com", "pinterest.fr", "pinterest.de", "pinterest.ch", "pinterest.jp", "pinterest.cl", "pinterest.ca", "pinterest.it", "pinterest.co.uk", "pinterest.nz", "pinterest.ru", "pinterest.com.au", "pinterest.at", "pinterest.pt", "pinterest.co.kr", "pinterest.es", "pinterest.com.mx", "pinterest.dk", "pinterest.ph", "pinterest.th", "pinterest.com.uy", "pinterest.co", "pinterest.nl", "pinterest.info", "pinterest.kr", "pinterest.ie", "pinterest.vn", "pinterest.com.vn", "pinterest.ec", "pinterest.mx", "pinterest.in", "pinterest.pe", "pinterest.co.at", "pinterest.hu", "pinterest.co.in", "pinterest.co.nz", "pinterest.id", "pinterest.com.ec", "pinterest.com.py", "pinterest.tw", "pinterest.be", "pinterest.uk", "pinterest.com.bo", "pinterest.com.pe"}

func isPinterestHost(host string) bool {
	if host == "pin.it" {
		return true
	}
	for _, domain := range pinterestDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// Pinterest sends HLS videos to gallery-dl's Python ytdl downloader. The app
// resolves those requests with its installed yt-dlp executable instead.
func pinterestManifest(address string, metadata map[string]json.RawMessage) (string, bool) {
	if metadataText(metadata, "category") != "pinterest" || metadataText(metadata, "_ytdl_manifest") != "hls" || metadataText(metadata, "extension") != "mp4" || !strings.HasPrefix(address, "ytdl:") {
		return "", false
	}
	raw := strings.TrimPrefix(address, "ytdl:")
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || !strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		return "", false
	}
	return raw, true
}

func (d GalleryDLDiscovery) PinterestVideoURLs() map[int]string {
	urls := make(map[int]string)
	for i, file := range d.Files {
		if address, ok := pinterestManifest(file.URL, d.pinterestMetadata(file)); ok {
			urls[i] = address
		}
	}
	return urls
}

func (d GalleryDLDiscovery) pinterestMetadata(file GalleryDLFile) map[string]json.RawMessage {
	data := make(map[string]json.RawMessage)
	if len(d.Directories) == 1 {
		for key, value := range decodeMetadata(d.Directories[0]) {
			data[key] = value
		}
	}
	for key, value := range decodeMetadata(file.Metadata) {
		data[key] = value
	}
	return data
}

// Restrict re-extraction to the frozen non-streaming files in a mixed pin.
func (d GalleryDLDiscovery) pinterestFileFilter() (string, error) {
	if len(d.Files) == 0 || metadataText(d.pinterestMetadata(d.Files[0]), "category") != "pinterest" {
		return "", nil
	}
	// Older frozen image plans use post_id and have no extractor file numbers.
	// Their downloads still reconcile every sidecar against the frozen targets.
	legacy := true
	for _, file := range d.Files {
		data := d.pinterestMetadata(file)
		if metadataText(data, "id", "num") != "" || !galleryDLNumericID.MatchString(metadataText(data, "post_id")) {
			legacy = false
			break
		}
	}
	if legacy {
		return "", nil
	}
	numbers := make([]string, 0, len(d.Files))
	seen := make(map[int]bool)
	for _, file := range d.Files {
		data := d.pinterestMetadata(file)
		number, err := strconv.Atoi(metadataText(data, "num"))
		if err != nil || number < 1 || seen[number] || metadataText(data, "category") != "pinterest" {
			return "", fmt.Errorf("Pinterest 파일 순서를 확인할 수 없습니다")
		}
		seen[number] = true
		numbers = append(numbers, strconv.Itoa(number))
	}
	return "num in [" + strings.Join(numbers, ",") + "]", nil
}
