package download

import (
	"net/url"
	"regexp"
	"strings"
)

var instagramDownloadPath = regexp.MustCompile(`^/(p|reel|reels|tv)/([A-Za-z0-9_-]{1,32})(?:/.*)?$`)

// cleanInstagramURL keeps only the post type and ID of Instagram post links.
// Other URLs, including invalid input, retain their existing validation path.
func cleanInstagramURL(original string) string {
	if strings.ContainsFunc(original, func(r rune) bool { return r <= 0x20 || r == 0x7f }) {
		return original
	}
	page, err := url.Parse(original)
	if err != nil || (page.Scheme != "http" && page.Scheme != "https") || page.User != nil {
		return original
	}
	host := strings.ToLower(page.Hostname())
	if host != "instagram.com" && host != "www.instagram.com" {
		return original
	}
	if port := page.Port(); port != "" && !(page.Scheme == "https" && port == "443") && !(page.Scheme == "http" && port == "80") {
		return original
	}
	match := instagramDownloadPath.FindStringSubmatch(page.EscapedPath())
	if len(match) == 0 {
		return original
	}
	return page.Scheme + "://www.instagram.com/" + match[1] + "/" + match[2]
}
