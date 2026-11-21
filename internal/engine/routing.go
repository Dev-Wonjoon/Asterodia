package engine

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

var ErrInvalidURL = errors.New("올바른 HTTP 또는 HTTPS URL이 아닙니다")

var galleryFirstHosts = [...]string{
	"instagram.com",
	"x.com",
	"twitter.com",
	"reddit.com",
	"redd.it",
	"tumblr.com",
	"imgur.com",
	"pinterest.com",
	"pin.it",
	"flickr.com",
	"flic.kr",
	"bsky.app",
}

func Select(rawURL string, options Options) (Kind, error) {
	if rawURL == "" || strings.ContainsFunc(rawURL, func(r rune) bool {
		return r <= 0x20 || r == 0x7f
	}) {
		return "", ErrInvalidURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", ErrInvalidURL
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrInvalidURL
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "" || parsed.User != nil {
		return "", ErrInvalidURL
	}

	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 0 || value > 65535 {
			return "", ErrInvalidURL
		}
	}
	manifestPath := strings.ToLower(parsed.Path)
	if strings.HasSuffix(manifestPath, ".m3u8") || strings.HasSuffix(manifestPath, ".mpd") ||
		options.Type == "audio" ||
		options.Quality == "audio" ||
		(options.Quality != "" && options.Quality != "best") ||
		options.Extension != "" {
		return KindYtdlp, nil
	}

	for _, domain := range galleryFirstHosts {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return KindGalleryDL, nil
		}
	}

	return KindYtdlp, nil
}
