package engine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

const threadsOriginal = "https://www.threads.com/@fixture/post/Fixture_123?ref=source"

func threadsFixturePost(kind int) map[string]any {
	post := map[string]any{
		"pk": json.Number(embedPostID), "code": "Fixture_123", "media_type": kind,
		"user":    map[string]any{"pk": "9001", "username": "fixture"},
		"caption": map[string]string{"text": "Threads 원본 사진·영상"},
		"image_versions2": map[string]any{"candidates": []any{
			map[string]any{"url": "https://scontent.cdninstagram.com/preview.jpg", "width": 10, "height": 10},
			map[string]any{"url": embedPhotoURL, "width": 1000, "height": 1000},
		}},
	}
	if kind == 2 {
		post["video_versions"] = []any{map[string]any{"url": embedVideoURL, "width": 1920, "height": 1080}}
	}
	if kind == 8 {
		photo, video := threadsFixturePost(1), threadsFixturePost(2)
		photo["pk"], video["pk"] = embedMediaID, "3401234567890123458"
		post["carousel_media_count"], post["carousel_media"] = 2, []any{photo, video}
	}
	return post
}

func threadsFixturePage(t *testing.T, post map[string]any) []byte {
	t.Helper()
	recommended := threadsFixturePost(2)
	recommended["code"], recommended["pk"] = "OtherPost", "9901"
	raw, err := json.Marshal(map[string]any{"data": []any{recommended, map[string]any{"thread_items": []any{map[string]any{"post": post}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return []byte(`<html><script type="application/json" data-sjs>` + string(raw) + `</script></html>`)
}

func threadsClient(t *testing.T, page []byte, media func(*http.Request) (*http.Response, error)) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	for _, host := range []string{"https://www.threads.com", "https://scontent.cdninstagram.com", "https://video.fbcdn.net"} {
		address, _ := url.Parse(host)
		jar.SetCookies(address, []*http.Cookie{{Name: "private", Value: "must-not-leak"}})
	}
	return &http.Client{Jar: jar, Transport: instagramRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("anonymous request leaked credentials")
		}
		if !strings.Contains(r.Header.Get("User-Agent"), "Googlebot/2.1") {
			t.Error("missing public page agent")
		}
		if strings.HasSuffix(r.URL.Hostname(), "threads.com") || strings.HasSuffix(r.URL.Hostname(), "threads.net") {
			return instagramResponse(r, 200, page), nil
		}
		if media == nil {
			t.Error("unexpected media request")
			return nil, fmt.Errorf("unexpected request")
		}
		return media(r)
	})}
}

func TestThreadsPublicDiscovery(t *testing.T) {
	for _, kind := range []int{1, 2, 8} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			extractor := engine.NewThreadsExtractor(engine.ExtractorDependencies{HTTPClient: threadsClient(t, threadsFixturePage(t, threadsFixturePost(kind)), nil)})
			plan, err := engine.DiscoverWithExtractors(t.Context(), engine.ExtractRequest{URL: threadsOriginal}, []engine.Extractor{extractor})
			if err != nil || plan.Extractor != "threads" || plan.Transfer != engine.TransferDirect {
				t.Fatal(plan, err)
			}
			targets, err := plan.MediaTargets()
			want := 1
			if kind == 8 {
				want = 2
			}
			if err != nil || len(targets) != want {
				t.Fatal(targets, err)
			}
			for _, target := range targets {
				if target.PostKey != "threads:"+embedPostID || target.PlatformName != "Threads" || target.SourceURL != threadsOriginal || target.OwnerID != "9001" || target.OwnerName != "fixture" {
					t.Fatal(target)
				}
			}
			if kind == 2 && (targets[0].Kind != "video" || plan.Files[0].URL != embedVideoURL) {
				t.Fatal("video became preview")
			}
			if kind == 1 && plan.Files[0].URL != embedPhotoURL {
				t.Fatal("best photo was not selected")
			}
			if kind == 8 && (targets[0].Kind != "image" || targets[1].Kind != "video") {
				t.Fatal("carousel order changed")
			}
		})
	}
}

func TestThreadsPublicCarouselWithoutCount(t *testing.T) {
	post := threadsFixturePost(8)
	delete(post, "carousel_media_count")
	extractor := engine.NewThreadsExtractor(engine.ExtractorDependencies{HTTPClient: threadsClient(t, threadsFixturePage(t, post), nil)})
	plan, err := extractor.Discover(t.Context(), engine.ExtractRequest{URL: threadsOriginal})
	if err != nil || len(plan.Files) != 2 {
		t.Fatal(plan, err)
	}
}

// Explicitly opt in with a public post URL. This checks discovery only and does
// not download the remote media into a test fixture.
func TestThreadsLiveDiscovery(t *testing.T) {
	address := os.Getenv("ASTERODIA_TEST_THREADS_URL")
	if os.Getenv("ASTERODIA_LIVE_TESTS") != "1" || address == "" {
		t.Skip("set ASTERODIA_LIVE_TESTS=1 and ASTERODIA_TEST_THREADS_URL")
	}
	extractor := engine.NewThreadsExtractor(engine.ExtractorDependencies{})
	plan, err := engine.DiscoverWithExtractors(t.Context(), engine.ExtractRequest{URL: address}, []engine.Extractor{extractor})
	if err != nil {
		t.Fatal(err)
	}
	targets, err := plan.MediaTargets()
	if err != nil || len(targets) == 0 || plan.Transfer != engine.TransferDirect {
		t.Fatal("public Threads discovery failed", err)
	}
	t.Logf("Threads live discovery: %d targets, transfer=%s", len(targets), plan.Transfer)
}

func TestThreadsRejectsIncompleteOrUnrelatedMedia(t *testing.T) {
	for _, mode := range []string{"other-post", "video-preview", "partial-carousel", "duplicate-media", "hostile-cdn", "text-only", "executable-script", "caption-json", "missing-share-identity"} {
		t.Run(mode, func(t *testing.T) {
			post := threadsFixturePost(8)
			address := threadsOriginal
			switch mode {
			case "other-post":
				post["code"] = "AnotherCode"
			case "video-preview":
				post = threadsFixturePost(2)
				delete(post, "video_versions")
			case "partial-carousel":
				post["carousel_media_count"] = 3
			case "duplicate-media":
				post["carousel_media"].([]any)[1].(map[string]any)["pk"] = embedMediaID
			case "hostile-cdn":
				post = threadsFixturePost(2)
				post["video_versions"] = []any{map[string]any{"url": "https://video.fbcdn.net.evil.invalid/file.mp4"}}
			case "text-only":
				post["media_type"] = 19
			case "missing-share-identity":
				address = "https://www.threads.com/share/opaque"
			}
			page := threadsFixturePage(t, post)
			if mode == "executable-script" {
				page = bytes.ReplaceAll(page, []byte(`type="application/json"`), []byte(`type="text/javascript"`))
			}
			if mode == "caption-json" {
				page = []byte(`<script type="application/json">{"caption":"{\"code\":\"Fixture_123\"}"}</script>`)
			}
			extractor := engine.NewThreadsExtractor(engine.ExtractorDependencies{HTTPClient: threadsClient(t, page, nil)})
			plan, err := extractor.Discover(t.Context(), engine.ExtractRequest{URL: address})
			if !errors.Is(err, engine.ErrThreadsPublicExtraction) || len(plan.Files) != 0 {
				t.Fatal("invalid post accepted", plan, err)
			}
		})
	}
}

func TestThreadsShareAndLegacyLinks(t *testing.T) {
	for _, address := range []string{"https://www.threads.net/@fixture/post/Fixture_123", "https://www.threads.com/t/Fixture_123", "https://www.threads.com/share/opaque"} {
		t.Run(address, func(t *testing.T) {
			page := append([]byte(`<link rel="canonical" href="https://www.threads.com/@fixture/post/Fixture_123">`), threadsFixturePage(t, threadsFixturePost(1))...)
			extractor := engine.NewThreadsExtractor(engine.ExtractorDependencies{HTTPClient: threadsClient(t, page, nil)})
			plan, err := extractor.Discover(t.Context(), engine.ExtractRequest{URL: address})
			if err != nil {
				t.Fatal(err)
			}
			targets, err := plan.MediaTargets()
			if err != nil || len(targets) != 1 || targets[0].SourceURL != address {
				t.Fatal(targets, err)
			}
		})
	}
}

func TestThreadsStopsOnRateLimitSizeLimitAndCancellation(t *testing.T) {
	for _, mode := range []string{"rate-limit", "size-limit", "cancelled", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			client := &http.Client{Transport: instagramRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				switch mode {
				case "rate-limit":
					return instagramResponse(r, 429, nil), nil
				case "size-limit":
					return instagramResponse(r, 200, bytes.Repeat([]byte("x"), (8<<20)+1)), nil
				case "redirect":
					response := instagramResponse(r, 302, nil)
					response.Header.Set("Location", "https://evil.invalid/post")
					return response, nil
				default:
					cancel()
					return nil, ctx.Err()
				}
			})}
			extractor := engine.NewThreadsExtractor(engine.ExtractorDependencies{HTTPClient: client})
			_, err := extractor.Discover(ctx, engine.ExtractRequest{URL: threadsOriginal})
			want := map[string]error{"rate-limit": engine.ErrRateLimited, "size-limit": engine.ErrOutputLimit, "cancelled": context.Canceled}[mode]
			if calls != 1 || err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatal(calls, err)
			}
		})
	}
}

func TestThreadsDirectTransfer(t *testing.T) {
	photo := instagramPhoto(t)
	video := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}
	for _, mode := range []string{"success", "expired", "html-response", "truncated", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			stage, err := os.MkdirTemp(engineTestRoot(t, "engine-parsers"), "threads-transfer-")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			client := threadsClient(t, threadsFixturePage(t, threadsFixturePost(8)), func(r *http.Request) (*http.Response, error) {
				calls++
				payload := photo
				if r.URL.String() == embedVideoURL {
					payload = video
					switch mode {
					case "expired":
						return instagramResponse(r, 403, nil), nil
					case "html-response":
						payload = []byte("<html>login</html>")
					case "cancelled":
						cancel()
						return nil, ctx.Err()
					}
				} else if r.URL.String() != embedPhotoURL {
					t.Error("signed media URL changed")
				}
				response := instagramResponse(r, 200, payload)
				if mode == "truncated" && calls == 2 {
					response.ContentLength += 100
				}
				return response, nil
			})
			extractor := engine.NewThreadsExtractor(engine.ExtractorDependencies{HTTPClient: client})
			plan, err := extractor.Discover(ctx, engine.ExtractRequest{URL: threadsOriginal})
			if err != nil {
				t.Fatal(err)
			}
			paths, err := plan.Download(ctx, stage, nil)
			if mode != "success" {
				entries, readErr := os.ReadDir(stage)
				if err == nil || len(paths) != 0 || readErr != nil || len(entries) != 0 {
					t.Fatal("partial files survived failure", paths, entries, err, readErr)
				}
				return
			}
			if err != nil || len(paths) != 2 || calls != 2 {
				t.Fatal(paths, calls, err)
			}
			for i, want := range [][]byte{photo, video} {
				got, err := os.ReadFile(paths[i])
				if err != nil || !bytes.Equal(got, want) {
					t.Fatal("saved media mismatch", err)
				}
			}
		})
	}
}
