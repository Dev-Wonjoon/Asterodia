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

func instagramPublicFixturePost(kind int) map[string]any {
	return map[string]any{
		"pk": json.Number(embedPostID), "code": "Fixture_123", "media_type": kind,
		"user":            map[string]any{"pk": json.Number("3401234567890123999"), "username": "fixture-owner"},
		"caption":         map[string]string{"text": "공개 응답 캡션"},
		"image_versions2": map[string]any{"candidates": []any{map[string]any{"url": embedPhotoURL, "width": 1080, "height": 1920}}},
		"video_versions":  []any{map[string]any{"url": "https://video.fbcdn.net/low.mp4", "width": 360, "height": 640}, map[string]any{"url": embedVideoURL, "width": 1080, "height": 1920}},
	}
}

func instagramPublicPage(t *testing.T, post map[string]any) []byte {
	t.Helper()
	root := map[string]any{"pk": post["pk"], "code": post["code"], "if_not_gated_logged_out": post}
	payload := map[string]any{"require": []any{[]any{"ScheduledServerJS", "handle", nil, []any{map[string]any{"__bbox": map[string]any{"require": []any{[]any{"RelayPrefetchedStreamCache", "next", nil, []any{"fixture-preloader", map[string]any{"__bbox": map[string]any{"result": map[string]any{"data": map[string]any{"xig_polaris_media": root}}}}}}}}}}}}}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return append(append([]byte(`<html><script type="application/json" data-sjs>`), raw...), []byte(`</script></html>`)...)
}

func instagramPublicClient(t *testing.T, page []byte) *http.Client {
	t.Helper()
	client := &http.Client{Transport: instagramRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("공개 조회에 계정 인증 정보가 사용되었습니다")
		}
		if r.URL.Hostname() != "www.instagram.com" {
			t.Error("대상 조회 중 미디어에 접근했습니다")
			return nil, fmt.Errorf("unexpected media request")
		}
		switch r.URL.Path {
		case "/p/Fixture_123/embed/captioned/", "/p/DgIhTYDyX9Y/embed/captioned/":
			if r.Header.Get("User-Agent") != "Mozilla/5.0" {
				t.Error("임베드 헤더가 바뀌었습니다")
			}
			return instagramResponse(r, 200, instagramEmbedFixture(t, instagramFixturePost("GraphVideo"), false)), nil
		case "/p/Fixture_123/", "/p/DgIhTYDyX9Y/":
			if !strings.HasPrefix(r.Header.Get("User-Agent"), "facebookexternalhit/1.1") {
				t.Error("공개 조회 헤더가 다릅니다")
			}
			return instagramResponse(r, 200, page), nil
		default:
			t.Error("잘못된 공개 조회 경로")
			return nil, fmt.Errorf("unexpected request")
		}
	})}
	client.Jar, _ = cookiejar.New(nil)
	address, _ := url.Parse("https://www.instagram.com/")
	client.Jar.SetCookies(address, []*http.Cookie{{Name: "sessionid", Value: "must-not-send"}})
	return client
}

func TestInstagramPublicDiscovery(t *testing.T) {
	for _, kind := range []int{1, 2, 8} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			post := instagramPublicFixturePost(kind)
			if kind == 8 {
				photo, video := instagramPublicFixturePost(1), instagramPublicFixturePost(2)
				photo["pk"], video["pk"] = embedMediaID, "3401234567890123458"
				post["carousel_media"], post["carousel_media_count"] = []any{photo, video}, 2
			}
			g := engine.GalleryDL{Executable: "must-not-run.exe", HTTPClient: instagramPublicClient(t, instagramPublicPage(t, post))}
			var trace []string
			ctx := engine.WithOutputObserver(t.Context(), func(output engine.Output) { trace = append(trace, output.Text) })
			d, err := g.Discover(ctx, embedOriginalURL, "")
			if err != nil || !d.InstagramEmbed {
				t.Fatal("공개 HTML 조회 실패", err)
			}
			if len(trace) != 2 || !strings.Contains(trace[0], "Instagram 임베드 · 조회 실패") || !strings.Contains(trace[1], "Instagram 공개 HTML · 조회 성공") {
				t.Fatal("조회 순서가 바뀌거나 공개 조회 성공 뒤 CLI를 실행했습니다", trace)
			}
			targets, err := d.Targets(embedOriginalURL)
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if kind == 8 {
				want = 2
			}
			if len(targets) != want || targets[0].PostKey != "instagram:"+embedPostID || targets[0].OwnerID != "3401234567890123999" || targets[0].Title != "공개 응답 캡션" {
				t.Fatal("공개 메타데이터가 보존되지 않았습니다", targets)
			}
			if kind == 2 && (targets[0].Kind != "video" || d.Files[0].URL != embedVideoURL) {
				t.Fatal("최고 화질 영상 주소가 선택되지 않았습니다")
			}
			if kind == 8 && (targets[0].Kind != "image" || targets[1].Kind != "video" || targets[0].ExternalID != embedMediaID) {
				t.Fatal("앨범 미디어 순서가 바뀌었습니다", targets)
			}
		})
	}
}

func TestInstagramPublicRejectsInvalidMetadata(t *testing.T) {
	for _, mode := range []string{"wrong-code", "wrong-id", "thumbnail-only", "foreign-host", "http-media", "wrong-port", "userinfo", "empty-carousel", "incomplete-carousel", "duplicate-child", "null-child", "executable-script", "caption-json"} {
		t.Run(mode, func(t *testing.T) {
			post := instagramPublicFixturePost(2)
			switch mode {
			case "wrong-code":
				post["code"] = "Other"
			case "thumbnail-only":
				delete(post, "video_versions")
			case "foreign-host":
				post["video_versions"] = []any{map[string]any{"url": "https://cdninstagram.com.example.invalid/movie.mp4"}}
			case "http-media":
				post["video_versions"] = []any{map[string]any{"url": "http://video.fbcdn.net/movie.mp4"}}
			case "wrong-port":
				post["video_versions"] = []any{map[string]any{"url": "https://video.fbcdn.net:8443/movie.mp4"}}
			case "userinfo":
				post["video_versions"] = []any{map[string]any{"url": "https://user:secret@video.fbcdn.net/movie.mp4"}}
			case "empty-carousel":
				post["media_type"], post["carousel_media_count"], post["carousel_media"] = 8, 0, []any{}
			case "incomplete-carousel":
				post["media_type"], post["carousel_media_count"], post["carousel_media"] = 8, 2, []any{instagramPublicFixturePost(1)}
			case "duplicate-child":
				post["media_type"], post["carousel_media_count"], post["carousel_media"] = 8, 2, []any{instagramPublicFixturePost(1), instagramPublicFixturePost(2)}
			case "null-child":
				post["media_type"], post["carousel_media_count"], post["carousel_media"] = 8, 1, []any{nil}
			}
			page := instagramPublicPage(t, post)
			if mode == "wrong-id" {
				page = bytes.Replace(page, []byte(`"pk":3401234567890123456`), []byte(`"pk":3401234567890123459`), 1)
			}
			if mode == "executable-script" {
				page = bytes.Replace(page, []byte(`type="application/json"`), []byte(`type="text/javascript"`), 1)
			}
			if mode == "caption-json" {
				raw, _ := json.Marshal(map[string]string{"caption": string(page)})
				page = append(append([]byte(`<script type="application/json">`), raw...), []byte(`</script>`)...)
			}
			g := engine.GalleryDL{Executable: "does-not-exist.exe", HTTPClient: instagramPublicClient(t, page)}
			d, err := g.Discover(t.Context(), embedOriginalURL, "")
			if err == nil || len(d.Files) != 0 {
				t.Fatal("불완전하거나 다른 게시물의 공개 미디어가 허용됐습니다", mode)
			}
			if mode == "thumbnail-only" && !errors.Is(err, engine.ErrInstagramPublicExtraction) {
				t.Fatal("공개 영상 추출 실패가 구분되지 않았습니다", err)
			}
			var exhausted *engine.DiscoveryChainError
			if !errors.As(err, &exhausted) || len(exhausted.Failures) != 3 || exhausted.Failures[0].Handler != "Instagram 임베드" ||
				exhausted.Failures[1].Handler != "Instagram 공개 HTML" || exhausted.Failures[2].Handler != "gallery-dl" {
				t.Fatal("조회 방식별 실패 이력이 보존되지 않았습니다", err)
			}
		})
	}
}

func TestInstagramPublicKeepsExactURLsAndRetriesCompletePreloaders(t *testing.T) {
	partial := instagramPublicFixturePost(2)
	delete(partial, "video_versions")
	post := instagramPublicFixturePost(2)
	post["video_versions"] = []any{map[string]any{"url": embedVideoURL, "width": nil, "height": nil, "type": 101}}
	page := append(instagramPublicPage(t, partial), instagramPublicPage(t, post)...)
	g := engine.GalleryDL{Executable: "must-not-run.exe", HTTPClient: instagramPublicClient(t, page)}
	d, err := g.Discover(t.Context(), embedOriginalURL, "")
	if err != nil || len(d.Files) != 1 || d.Files[0].URL != embedVideoURL {
		t.Fatal("서명 주소가 바뀌거나 완성된 응답을 읽지 못했습니다", err)
	}
}

func TestInstagramPublicStopsOnRateLimitSizeLimitAndCancellation(t *testing.T) {
	for _, mode := range []string{"rate-limit", "too-large", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			client := instagramPublicClient(t, instagramPublicPage(t, instagramPublicFixturePost(2)))
			base := client.Transport
			client.Transport = instagramRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/p/Fixture_123/" {
					switch mode {
					case "rate-limit":
						return instagramResponse(r, 429, nil), nil
					case "too-large":
						return instagramResponse(r, 200, bytes.Repeat([]byte("x"), (8<<20)+1)), nil
					case "cancelled":
						cancel()
						return instagramResponse(r, 200, instagramPublicPage(t, instagramPublicFixturePost(2))), nil
					}
				}
				return base.RoundTrip(r)
			})
			g := engine.GalleryDL{Executable: "must-not-run.exe", HTTPClient: client}
			_, err := g.Discover(ctx, embedOriginalURL, "")
			want := map[string]error{"rate-limit": engine.ErrRateLimited, "too-large": engine.ErrOutputLimit, "cancelled": context.Canceled}[mode]
			if err != want {
				t.Fatal("조회 중단 이유가 보존되지 않았습니다", mode, err)
			}
		})
	}
}

func TestInstagramPublicPhotoTransfer(t *testing.T) {
	photo := instagramPhoto(t)
	client := instagramPublicClient(t, instagramPublicPage(t, instagramPublicFixturePost(1)))
	base := client.Transport
	client.Transport = instagramRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() == "scontent.cdninstagram.com" {
			if r.URL.String() != embedPhotoURL || r.Header.Get("Cookie") != "" {
				t.Error("미디어 전송 요청이 바뀌었습니다")
			}
			return instagramResponse(r, 200, photo), nil
		}
		return base.RoundTrip(r)
	})
	g := engine.GalleryDL{Executable: "must-not-run.exe", HTTPClient: client}
	d, err := g.Discover(t.Context(), embedOriginalURL, "")
	if err != nil {
		t.Fatal(err)
	}
	stage, err := os.MkdirTemp(engineTestRoot(t, "engine-parsers"), "public-photo-")
	if err != nil {
		t.Fatal(err)
	}
	paths, err := g.Download(t.Context(), embedOriginalURL, d, stage)
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil || !bytes.Equal(data, photo) {
		t.Fatal("공개 사진 전송 실패", err)
	}
}

// Optional local evidence. The supplied HTML can contain signed URLs or private
// fields, so it is never copied into the repository or logged by this test.
func TestInstagramPublicSuppliedHTML(t *testing.T) {
	path := os.Getenv("ASTERODIA_TEST_INSTAGRAM_HTML")
	if path == "" {
		t.Skip("첨부 HTML 검증은 환경 변수로 별도 지정합니다")
	}
	page, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	g := engine.GalleryDL{Executable: "must-not-run.exe", HTTPClient: instagramPublicClient(t, page)}
	d, err := g.Discover(t.Context(), "https://www.instagram.com/p/DgIhTYDyX9Y/", "")
	if err != nil || len(d.Files) != 1 {
		t.Fatal("실제 첨부 HTML 처리 실패", err)
	}
	targets, err := d.Targets("https://www.instagram.com/p/DgIhTYDyX9Y/")
	if err != nil || targets[0].Kind != "video" {
		t.Fatal("실제 영상이 썸네일로 처리되었습니다", err)
	}
	var metadata struct {
		Extension string `json:"extension"`
	}
	if json.Unmarshal(d.Files[0].Metadata, &metadata) != nil || metadata.Extension != "mp4" {
		t.Fatal("실제 영상의 확장자가 잘못되었습니다")
	}
	t.Log("첨부 HTML에서 해당 게시물 원본 영상 1개 확인")
}
