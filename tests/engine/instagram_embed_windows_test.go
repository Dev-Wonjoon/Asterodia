package engine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

type instagramRoundTripper func(*http.Request) (*http.Response, error)

type instagramReader func([]byte) (int, error)

func (fn instagramReader) Read(p []byte) (int, error) { return fn(p) }

func (fn instagramRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func instagramResponse(request *http.Request, status int, body []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: request}
}

func instagramUnavailableClient() *http.Client {
	return &http.Client{Transport: instagramRoundTripper(func(r *http.Request) (*http.Response, error) {
		return instagramResponse(r, http.StatusNotFound, nil), nil
	})}
}

func instagramNoEmbedClient(t *testing.T) *http.Client {
	return &http.Client{Transport: instagramRoundTripper(func(r *http.Request) (*http.Response, error) {
		t.Error("인증 모드에서 비로그인 임베드 요청이 발생했습니다")
		return nil, fmt.Errorf("unexpected anonymous request")
	})}
}

const embedPostID = "3401234567890123456"
const embedMediaID = "3401234567890123457"
const embedPhotoURL = "https://scontent.cdninstagram.com/photo.jpg?signature=one%2Btwo&token=x"
const embedVideoURL = "https://video.fbcdn.net/video.mp4?signature=three%2Bfour"
const embedOriginalURL = "https://www.instagram.com/p/Fixture_123/?utm_source=fixture"

func instagramEmbedFixture(t *testing.T, post map[string]any, xdt bool) []byte {
	t.Helper()
	key := "shortcode_media"
	if xdt {
		key = "xdt_shortcode_media"
	}
	contextJSON, err := json.Marshal(map[string]any{"gql_data": map[string]any{key: post}})
	if err != nil {
		t.Fatal(err)
	}
	init, err := json.Marshal(map[string]any{"contextJSON": string(contextJSON)})
	if err != nil {
		t.Fatal(err)
	}
	// Unrelated initializers and bracket-like caption text must not confuse parsing.
	return []byte(`<html><script>["init",[],[{"other":true}]], ["init", [], [` + string(init) + `]],</script></html>`)
}

func instagramFixturePost(kind string) map[string]any {
	return map[string]any{
		"id": embedPostID, "shortcode": "Fixture_123", "__typename": kind,
		"is_video": kind == "GraphVideo", "display_url": embedPhotoURL,
		"owner":                 map[string]any{"id": json.Number("3401234567890123999"), "username": "fixture-owner"},
		"edge_media_to_caption": map[string]any{"edges": []any{map[string]any{"node": map[string]any{"text": "따옴표 \"와 괄호 ]], 및 줄바꿈\n캡션"}}}},
	}
}

func instagramCarousel() map[string]any {
	post := instagramFixturePost("GraphSidecar")
	post["edge_sidecar_to_children"] = map[string]any{"edges": []any{
		map[string]any{"node": map[string]any{"id": json.Number(embedMediaID), "is_video": false, "display_url": embedPhotoURL}},
		map[string]any{"node": map[string]any{"id": "3401234567890123458", "is_video": true, "video_url": embedVideoURL, "display_url": embedPhotoURL}},
	}}
	return post
}

func instagramFixtureClient(t *testing.T, html []byte, media func(*http.Request) (*http.Response, error)) *http.Client {
	t.Helper()
	return &http.Client{Transport: instagramRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() == "www.instagram.com" && request.URL.Path == "/p/Fixture_123/" {
			if !strings.HasPrefix(request.Header.Get("User-Agent"), "facebookexternalhit/1.1") || request.Header.Get("Cookie") != "" || request.Header.Get("Authorization") != "" {
				t.Error("공개 HTML 조회에 잘못된 헤더가 사용되었습니다")
			}
			return instagramResponse(request, http.StatusNotFound, nil), nil
		}
		if request.Header.Get("User-Agent") != "Mozilla/5.0" {
			t.Error("실제로 검증한 임베드 User-Agent와 다릅니다")
		}
		if request.Header.Get("Cookie") != "" || request.Header.Get("Authorization") != "" {
			t.Error("비로그인 요청에 인증 정보가 포함되었습니다")
		}
		if request.URL.Hostname() == "www.instagram.com" {
			if request.URL.Path != "/p/Fixture_123/embed/captioned/" || request.URL.RawQuery != "" {
				t.Errorf("임베드 주소가 잘못되었습니다: %s", request.URL)
			}
			return instagramResponse(request, http.StatusOK, html), nil
		}
		if media == nil {
			t.Errorf("조회 중 미디어 요청이 발생했습니다: %s", request.URL.Hostname())
			return nil, fmt.Errorf("unexpected media request")
		}
		return media(request)
	})}
}

func TestInstagramEmbedDiscovery(t *testing.T) {
	for _, kind := range []string{"GraphImage", "GraphVideo", "GraphSidecar"} {
		t.Run(kind, func(t *testing.T) {
			post := instagramFixturePost(kind)
			address := embedOriginalURL
			if kind == "GraphVideo" {
				post["video_url"] = embedVideoURL
				address = "https://www.instagram.com/reels/Fixture_123/?utm_source=fixture"
			}
			if kind == "GraphSidecar" {
				post = instagramCarousel()
			}
			post["id"] = json.Number(embedPostID)
			client := instagramFixtureClient(t, instagramEmbedFixture(t, post, kind == "GraphVideo"), nil)
			// Even an explicitly supplied authenticated jar must not be used.
			client.Jar, _ = cookiejar.New(nil)
			page, _ := url.Parse("https://www.instagram.com/")
			client.Jar.SetCookies(page, []*http.Cookie{{Name: "sessionid", Value: "must-not-send"}})
			g := engine.GalleryDL{Executable: "must-not-run.exe", HTTPClient: client}
			d, err := g.Discover(t.Context(), address, "unused")
			if err != nil || !d.InstagramEmbed {
				t.Fatalf("임베드 조회 실패: %+v %v", d, err)
			}
			targets, err := d.Targets(address)
			if err != nil {
				t.Fatal(err)
			}
			wantCount := 1
			if kind == "GraphSidecar" {
				wantCount = 2
			}
			if len(targets) != wantCount || targets[0].PostKey != "instagram:"+embedPostID || targets[0].OwnerID != "3401234567890123999" || targets[0].OwnerName != "fixture-owner" || targets[0].SourceURL != address || !strings.Contains(targets[0].Title, "캡션") {
				t.Fatalf("게시물 정보가 보존되지 않았습니다: %+v", targets)
			}
			if kind == "GraphVideo" {
				if targets[0].Kind != "video" || d.Files[0].URL != embedVideoURL {
					t.Fatal("영상 대신 썸네일을 선택했습니다")
				}
			} else if d.Files[0].URL != embedPhotoURL {
				t.Fatal("사진 주소의 쿼리가 바뀌었습니다")
			}
			if kind == "GraphSidecar" && (targets[0].ExternalID != embedMediaID || targets[1].Kind != "video" || targets[1].FileIndex != 1) {
				t.Fatalf("미디어 ID 또는 순서가 달라졌습니다: %+v", targets)
			}
		})
	}
}

func TestInstagramEmbedRejectsIncompleteData(t *testing.T) {
	for _, scenario := range []string{"wrong-post", "missing-video", "missing-type", "empty-carousel", "missing-child", "duplicate-id", "foreign-host", "host-suffix", "http-media"} {
		t.Run(scenario, func(t *testing.T) {
			post := instagramFixturePost("GraphImage")
			switch scenario {
			case "wrong-post":
				post["shortcode"] = "Other"
			case "missing-video":
				post["__typename"], post["is_video"] = "GraphVideo", true
			case "missing-type":
				delete(post, "is_video")
			case "empty-carousel":
				post["__typename"] = "GraphSidecar"
			case "missing-child":
				post["__typename"] = "GraphSidecar"
				post["edge_sidecar_to_children"] = map[string]any{"edges": []any{map[string]any{"node": nil}}}
			case "duplicate-id":
				post = instagramCarousel()
				children := post["edge_sidecar_to_children"].(map[string]any)["edges"].([]any)
				children[1].(map[string]any)["node"].(map[string]any)["id"] = embedMediaID
			case "foreign-host":
				post["display_url"] = "https://example.invalid/photo.jpg"
			case "host-suffix":
				post["display_url"] = "https://cdninstagram.com.example.invalid/photo.jpg"
			case "http-media":
				post["display_url"] = "http://scontent.cdninstagram.com/photo.jpg"
			}
			g := engine.GalleryDL{Executable: "does-not-exist.exe", HTTPClient: instagramFixtureClient(t, instagramEmbedFixture(t, post, false), nil)}
			d, err := g.Discover(t.Context(), embedOriginalURL, "")
			if err == nil || len(d.Files) != 0 {
				t.Fatal("불완전한 임베드 결과를 허용했습니다", d, err)
			}
		})
	}
}

func instagramSimpleFixture() []byte {
	return []byte(`<html><script>["init",[],[{"isRichEmbed":false,"isSidecar":false,"isProfileEmbed":false,"isGuideEmbed":false,"contextJSON":null}]],</script>
<div class="Embed extra" data-media-type="GraphImage" data-media-id="` + embedPostID + `" data-owner-id="3401234567890123999" data-permalink="https://www.instagram.com/p/Fixture_123/?utm_source=ig_embed&amp;test=1">
<a class='Username' href='https://www.instagram.com/fixture-owner/'><img class='Avatar' src='https://scontent.cdninstagram.com/avatar.jpg' /><span>fixture-owner</span></a>
<img alt='Example > image &quot;quoted&quot;' class='EmbeddedMediaImage' src='https://scontent.cdninstagram.com/photo.jpg?signature=one%2Btwo&amp;token=x' />
<div class='Caption'><a class='CaptionUsername'>fixture-owner</a><br />설명 &amp; 인용 &gt;<br /><a href='/explore/tags/example/'>#태그</a><br />두 번째 줄<div class='CaptionComments'>포함하면 안 되는 댓글</div></div></div></html>`)
}

func TestInstagramEmbedSimplePhoto(t *testing.T) {
	photo := instagramPhoto(t)
	client := instagramFixtureClient(t, instagramSimpleFixture(), func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != embedPhotoURL {
			t.Errorf("HTML 엔티티 처리 후 주소가 바뀌었습니다: %s", r.URL)
		}
		return instagramResponse(r, http.StatusOK, photo), nil
	})
	g := engine.GalleryDL{Executable: "must-not-run.exe", HTTPClient: client}
	d, err := g.Discover(t.Context(), embedOriginalURL, "unused")
	if err != nil || !d.InstagramEmbed {
		t.Fatal("contextJSON=null인 Simple 사진 조회 실패", d, err)
	}
	targets, err := d.Targets(embedOriginalURL)
	if err != nil || len(targets) != 1 {
		t.Fatal("Simple 사진 대상 생성 실패", targets, err)
	}
	target := targets[0]
	if target.ExternalID != embedPostID || target.OwnerID != "3401234567890123999" || target.OwnerName != "fixture-owner" || target.Kind != "image" || target.Title != "설명 & 인용 >\n#태그\n두 번째 줄" {
		t.Fatalf("Simple 사진 식별 정보 또는 캡션이 다릅니다: %+v", target)
	}
	stage, err := os.MkdirTemp(engineTestRoot(t, "engine-parsers"), "instagram-simple-")
	if err != nil {
		t.Fatal(err)
	}
	paths, err := g.Download(t.Context(), embedOriginalURL, d, stage)
	if err != nil || len(paths) != 1 {
		t.Fatal("Simple 사진 저장 실패", paths, err)
	}
	got, err := os.ReadFile(paths[0])
	if err != nil || !bytes.Equal(got, photo) {
		t.Fatal("Simple 사진 내용이 다릅니다", err)
	}
}

func TestInstagramEmbedSimpleRejectsPreviews(t *testing.T) {
	for _, scenario := range []string{"video", "carousel", "wrong-post", "missing-id", "avatar-only", "duplicate-image", "foreign-host", "not-simple"} {
		t.Run(scenario, func(t *testing.T) {
			page := string(instagramSimpleFixture())
			switch scenario {
			case "video":
				page = strings.ReplaceAll(page, `data-media-type="GraphImage"`, `data-media-type="GraphVideo"`)
			case "carousel":
				page = strings.ReplaceAll(page, `data-media-type="GraphImage"`, `data-media-type="GraphSidecar"`)
			case "wrong-post":
				page = strings.ReplaceAll(page, "/p/Fixture_123/", "/p/Other/")
			case "missing-id":
				page = strings.ReplaceAll(page, embedPostID, "")
			case "avatar-only":
				page = strings.ReplaceAll(page, "EmbeddedMediaImage", "Avatar")
			case "duplicate-image":
				page += `<img class="EmbeddedMediaImage" src="` + embedPhotoURL + `" />`
			case "foreign-host":
				page = strings.ReplaceAll(page, "scontent.cdninstagram.com/photo", "example.invalid/photo")
			case "not-simple":
				page = strings.ReplaceAll(page, `"isRichEmbed":false`, `"isRichEmbed":true`)
			}
			g := engine.GalleryDL{Executable: "does-not-exist.exe", HTTPClient: instagramFixtureClient(t, []byte(page), nil)}
			d, err := g.Discover(t.Context(), embedOriginalURL, "")
			if err == nil || len(d.Files) != 0 {
				t.Fatal("불완전한 Simple 임베드를 허용했습니다", d, err)
			}
		})
	}
}

func TestInstagramEmbedHTTPFailures(t *testing.T) {
	t.Run("cancelled-before-request", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		g := engine.GalleryDL{HTTPClient: &http.Client{Transport: instagramRoundTripper(func(r *http.Request) (*http.Response, error) {
			t.Error("취소 후 HTTP 요청이 발생했습니다")
			return nil, context.Canceled
		})}}
		if _, err := g.Discover(ctx, embedOriginalURL, ""); !errors.Is(err, context.Canceled) {
			t.Fatal("취소 오류가 보존되지 않았습니다", err)
		}
	})
	for _, status := range []int{401, 403, 404, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			g := engine.GalleryDL{Executable: "does-not-exist.exe", HTTPClient: &http.Client{Transport: instagramRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				return instagramResponse(r, status, nil), nil
			})}}
			_, err := g.Discover(t.Context(), embedOriginalURL, "")
			want := map[int]error{401: engine.ErrAuthenticationRequired, 403: engine.ErrAccessDenied, 404: engine.ErrMediaUnavailable, 429: engine.ErrRateLimited}[status]
			wantCalls := 2
			if status == 429 {
				wantCalls = 1
			}
			if !errors.Is(err, want) || calls != wantCalls {
				t.Fatalf("상태 %d 분류 실패: calls=%d error=%v", status, calls, err)
			}
		})
	}
	t.Run("redirect", func(t *testing.T) {
		calls := 0
		g := engine.GalleryDL{Executable: "does-not-exist.exe", HTTPClient: &http.Client{Transport: instagramRoundTripper(func(r *http.Request) (*http.Response, error) {
			calls++
			response := instagramResponse(r, 302, nil)
			response.Header.Set("Location", "https://www.instagram.com/accounts/login/")
			return response, nil
		})}}
		_, err := g.Discover(t.Context(), embedOriginalURL, "")
		if !errors.Is(err, engine.ErrAuthenticationRequired) || calls != 2 {
			t.Fatal("로그인 리다이렉트 분류 실패", calls, err)
		}
	})
}

// Opt-in verification of actual embed extraction AND file transfer. No CLI,
// account session, or external package is needed for this path.
func TestInstagramEmbedLive(t *testing.T) {
	if os.Getenv("ASTERODIA_LIVE_TESTS") != "1" {
		t.Skip("실제 사이트 검증은 ASTERODIA_LIVE_TESTS=1로 실행하세요")
	}
	address := os.Getenv("ASTERODIA_TEST_INSTAGRAM_URL")
	if address == "" {
		t.Skip("ASTERODIA_TEST_INSTAGRAM_URL에 게시물 주소를 지정하세요")
	}
	stage, err := os.MkdirTemp(engineTestRoot(t, "engine-parsers"), "instagram-live-")
	if err != nil {
		t.Fatal(err)
	}
	g := engine.GalleryDL{HTTPClient: &http.Client{Transport: instagramRoundTripper(func(request *http.Request) (*http.Response, error) {
		response, err := http.DefaultTransport.RoundTrip(request)
		if err == nil && request.URL.Hostname() == "www.instagram.com" {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
			response.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			response.Body = io.NopCloser(bytes.NewReader(body))
			t.Logf("실제 임베드 HTTP=%d bytes=%d contextJSON=%t", response.StatusCode, len(body), bytes.Contains(body, []byte("contextJSON")))
		}
		return response, err
	})}}
	d, err := g.Discover(t.Context(), address, stage)
	if err != nil || !d.InstagramEmbed {
		t.Fatalf("실제 임베드 조회 실패: %v", err)
	}
	targets, err := d.Targets(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("비로그인 임베드 조회: %d개 미디어", len(targets))
	paths, err := g.Download(t.Context(), address, d, stage)
	if err != nil || len(paths) != len(targets) {
		t.Fatalf("실제 미디어 저장 실패: files=%d error=%v", len(paths), err)
	}
	for i, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			t.Fatal("저장한 미디어가 없습니다", i, err)
		}
		if targets[i].Kind == "image" {
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			_, _, decodeErr := image.DecodeConfig(file)
			file.Close()
			if decodeErr != nil {
				t.Fatalf("실제 사진 디코딩 실패: %v", decodeErr)
			}
		}
		t.Logf("저장 확인: %s (%d bytes)", path, info.Size())
	}
}

func instagramPhoto(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstagramEmbedDownload(t *testing.T) {
	photo := instagramPhoto(t)
	// A minimal MP4 file-type box is enough to verify transport and MIME sniffing.
	video := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}
	for _, scenario := range []string{"success", "expired", "truncated", "html-response", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			root := engineTestRoot(t, "engine-parsers")
			stage, err := os.MkdirTemp(root, "instagram-embed-")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			client := instagramFixtureClient(t, instagramEmbedFixture(t, instagramCarousel(), false), func(r *http.Request) (*http.Response, error) {
				calls++
				body := photo
				if r.URL.Path == "/video.mp4" {
					if r.URL.String() != embedVideoURL {
						t.Error("영상 주소가 변경되었습니다")
					}
					switch scenario {
					case "expired":
						return instagramResponse(r, http.StatusForbidden, nil), nil
					case "cancelled":
						response := instagramResponse(r, http.StatusOK, video)
						response.Body = io.NopCloser(instagramReader(func(p []byte) (int, error) {
							if _, err := os.Stat(filepath.Join(stage, "item-0002", "media.mp4.part")); err != nil {
								t.Error("전송 중인 파일이 .part 경로에 없습니다", err)
							}
							cancel()
							return 0, ctx.Err()
						}))
						return response, nil
					case "html-response":
						return instagramResponse(r, http.StatusOK, []byte("<html>login required</html>")), nil
					}
					body = video
				} else if r.URL.String() != embedPhotoURL {
					t.Error("사진 주소가 변경되었습니다")
				}
				response := instagramResponse(r, http.StatusOK, body)
				if scenario == "truncated" && r.URL.Path == "/video.mp4" {
					response.ContentLength += 100
				}
				return response, nil
			})
			g := engine.GalleryDL{Executable: "must-not-run.exe", HTTPClient: client}
			d, err := g.Discover(ctx, embedOriginalURL, stage)
			if err != nil {
				t.Fatal(err)
			}
			paths, err := g.Download(ctx, embedOriginalURL, d, stage)
			if scenario != "success" {
				if err == nil || len(paths) != 0 {
					t.Fatal("실패한 묶음의 파일을 반환했습니다", paths, err)
				}
				if scenario == "cancelled" && !errors.Is(err, context.Canceled) {
					t.Fatal("취소 오류가 보존되지 않았습니다", err)
				}
				entries, readErr := os.ReadDir(stage)
				if readErr != nil || len(entries) != 0 {
					t.Fatal("부분 다운로드가 남았습니다", entries, readErr)
				}
				return
			}
			if err != nil || len(paths) != 2 || calls != 2 {
				t.Fatal("직접 다운로드 실패", paths, calls, err)
			}
			for i, want := range [][]byte{photo, video} {
				ext := ".jpg"
				if i == 1 {
					ext = ".mp4"
				}
				if paths[i] != filepath.Join(stage, fmt.Sprintf("item-%04d", i+1), "media"+ext) {
					t.Error("기존 저장 경로 계약이 바뀌었습니다", paths[i])
				}
				got, err := os.ReadFile(paths[i])
				if err != nil || !bytes.Equal(got, want) {
					t.Fatal("저장한 파일의 내용이 다릅니다", i, err)
				}
			}
			if _, _, err := image.DecodeConfig(bytes.NewReader(photo)); err != nil {
				t.Fatal("사진 디코딩 실패", err)
			}
		})
	}
}
