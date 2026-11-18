package engine_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

// Execute the same test binary as a deterministic gallery-dl CLI fixture.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--config-ignore" && os.Getenv("STORAGEBUCKET_GALLERYDL_FIXTURE") == "1" {
		args := strings.Join(os.Args[1:], "\n")
		for _, required := range []string{"extractor.instagram.videos=merged", "extractor.instagram.audio=true", "extractor.instagram.previews=false", "output.private=true"} {
			if !strings.Contains(args, required) {
				fmt.Fprintln(os.Stderr, "필수 조회 옵션 누락")
				os.Exit(2)
			}
		}
		for i, arg := range os.Args {
			if arg == "--cookies" {
				b, e := os.ReadFile(os.Args[i+1])
				if e != nil || string(b) != "synthetic cookie" {
					fmt.Fprintln(os.Stderr, "작업용 쿠키 사본 누락")
					os.Exit(2)
				}
			}
		}
		fmt.Print(os.Getenv("STORAGEBUCKET_GALLERYDL_FIXTURE_RESPONSE"))
		fmt.Fprint(os.Stderr, os.Getenv("STORAGEBUCKET_GALLERYDL_FIXTURE_STDERR"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestGalleryDLInstagramTargetKey(t *testing.T) {
	const originalURL = "https://www.instagram.com/p/Fixture_123/?igsh=fixture"
	const id = "3401234567890123456"
	const wantKey = "instagram:" + id + ":" + id

	directory := fmt.Sprintf(`{
		"category":"instagram","subcategory":"post","post_shortcode":"Fixture_123",
		"typename":"GraphImage","post_id":%q,"count":"1"
	}`, id)
	file := fmt.Sprintf(`{
		"category":"instagram","subcategory":"post","post_shortcode":"Fixture_123",
		"typename":"GraphImage","post_id":%q,"media_id":%q,"count":"1","extension":"jpg"
	}`, id, id)

	for _, tc := range []struct {
		name string
		file string
		want string
	}{
		{"legacy-string-id", file, wantKey},
		{"large-numeric-id", strings.ReplaceAll(file, `"`+id+`"`, id), wantKey},
		{"wrong-shortcode", strings.Replace(file, "Fixture_123", "OtherPost", 1), ""},
		{"wrong-media-id", strings.Replace(file, `"media_id":"`+id+`"`, `"media_id":"999"`, 1), ""},
		{"carousel", strings.Replace(file, "GraphImage", "GraphSidecar", 1), ""},
		{"incomplete-count", strings.Replace(file, `"count":"1"`, `"count":"2"`, 1), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := fmt.Sprintf(`[[2,%s],[3,"https://cdn.invalid/image.jpg?token=abc",%s]]`,
				directory, tc.file)
			discovery, err := engine.ParseGalleryDLDiscovery([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}

			key, err := discovery.InstagramTargetKey(originalURL)
			if tc.want == "" {
				if err == nil || key != "" {
					t.Fatalf("키 생성 실패를 기대했습니다. 키=%q 오류=%v", key, err)
				}
				return
			}
			if err != nil || key != tc.want {
				t.Fatalf("키=%q 기대값=%q 오류=%v", key, tc.want, err)
			}
		})
	}
	t.Run("Instagram 게시물과 옵션 선택", func(t *testing.T) {
		for _, path := range []string{"p/Fixture_123/", "reel/Fixture_123/", "reels/Fixture_123/", "author/p/Fixture_123/"} {
			kind, err := engine.Select("https://www.instagram.com/"+path, engine.Options{Type: "video", Quality: "best"})
			if err != nil || kind != engine.KindGalleryDL {
				t.Fatalf("엔진 선택: %q %v", kind, err)
			}
		}
		for _, option := range []engine.Options{{Type: "audio"}, {Quality: "720"}, {Extension: "mp4"}} {
			if _, err := engine.Select(originalURL, option); !errors.Is(err, engine.ErrInstagramOptions) {
				t.Fatalf("지원하지 않는 옵션: %v", err)
			}
		}
		for _, url := range []string{"https://www.instagram.com/author/", "https://www.instagram.com/stories/author/1/", "https://www.instagram.com/share/abc/"} {
			if _, err := engine.Select(url, engine.Options{}); !errors.Is(err, engine.ErrInstagramURL) {
				t.Fatalf("지원하지 않는 주소: %v", err)
			}
		}
	})
	t.Run("REST와 GraphQL의 여러 파일 식별", func(t *testing.T) {
		for _, api := range []string{"rest", "graphql"} {
			base := map[string]any{"category": "instagram", "subcategory": "post", "post_shortcode": "Fixture_123", "post_id": json.Number(id), "count": 2, "owner_id": json.Number("3401234567890123000"), "username": "fixture-owner", "description": "테스트 게시물"}
			if api == "rest" {
				base["type"] = "post"
			} else {
				base["typename"] = "GraphSidecar"
			}
			makeFile := func(mediaID string, num int, ext string) map[string]any {
				row := make(map[string]any)
				for k, v := range base {
					row[k] = v
				}
				row["media_id"], row["num"], row["extension"] = json.Number(mediaID), num, ext
				return row
			}
			first, second := makeFile("3401234567890123457", 1, "jpg"), makeFile("3401234567890123458", 2, "mp4")
			for _, change := range []string{"valid", "duplicate-id", "missing-file", "wrong-order", "wrong-post"} {
				t.Run(api+"/"+change, func(t *testing.T) {
					last := make(map[string]any)
					for k, v := range second {
						last[k] = v
					}
					switch change {
					case "duplicate-id":
						last["media_id"] = first["media_id"]
					case "wrong-order":
						last["num"] = 1
					case "wrong-post":
						last["post_id"] = "999"
					}
					messages := [][]any{{2, base}, {3, "https://cdn.invalid/one.jpg", first}, {3, "https://cdn.invalid/two.mp4", last}}
					if change == "missing-file" {
						messages = messages[:2]
					}
					raw, _ := json.Marshal(messages)
					discovery, err := engine.ParseGalleryDLDiscovery(raw)
					if err != nil {
						t.Fatal(err)
					}
					targets, err := discovery.InstagramTargets(originalURL)
					if change != "valid" {
						if err == nil {
							t.Fatal("잘못된 게시물 전체 정보를 허용했습니다")
						}
						return
					}
					if err != nil || len(targets) != 2 {
						t.Fatalf("여러 파일 해석: %+v %v", targets, err)
					}
					if targets[0].TargetKey != "instagram:"+id+":3401234567890123457" || targets[1].Kind != "video" || targets[1].OwnerID != "3401234567890123000" || targets[0].Title != "테스트 게시물" {
						t.Fatalf("식별자·종류·작성자·제목 유실: %+v", targets)
					}
				})
			}
		}
	})
	t.Run("릴스와 구조화된 오류", func(t *testing.T) {
		raw := fmt.Sprintf(`[[2,{"category":"instagram","subcategory":"reel","post_shortcode":"Fixture_123","post_id":%q,"count":1,"type":"reel"}],[3,"https://cdn.invalid/video.mp4",{"category":"instagram","subcategory":"reel","post_shortcode":"Fixture_123","post_id":%q,"media_id":%q,"count":1,"type":"reel","extension":"mp4","num":1}]]`, id, id, id)
		discovery, err := engine.ParseGalleryDLDiscovery([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		targets, err := discovery.InstagramTargets("https://www.instagram.com/reel/Fixture_123/")
		if err != nil || targets[0].Kind != "video" {
			t.Fatalf("릴스 해석: %+v %v", targets, err)
		}
		for _, tc := range []struct {
			class, message string
			want           error
		}{{"AuthRequired", "cookies", engine.ErrAuthenticationRequired}, {"AuthenticationError", "expired", engine.ErrAuthenticationRequired}, {"AuthorizationError", "private", engine.ErrAccessDenied}, {"NotFoundError", "post", engine.ErrMediaUnavailable}, {"HttpError", "HTTP 429", engine.ErrRateLimited}, {"AbortExtraction", "HTTP redirect to login page", engine.ErrAuthenticationRequired}, {"AbortExtraction", "HTTP redirect to challenge page", engine.ErrAccessDenied}} {
			b, _ := json.Marshal([][]any{{3, "https://cdn.invalid/partial.jpg", map[string]any{"extension": "jpg"}}, {-1, map[string]string{"error": tc.class, "message": tc.message}}})
			result, err := engine.ParseGalleryDLDiscovery(b)
			if !errors.Is(err, tc.want) || len(result.Files) != 0 {
				t.Fatalf("부분 결과·오류 처리 %s: %+v %v", tc.class, result, err)
			}
		}
	})
	t.Run("CLI 옵션·쿠키 보존·종료 코드 0의 추출 오류", func(t *testing.T) {
		root := engineTestRoot(t, "engine-parsers")
		cookie := filepath.Join(root, "original-cookies.txt")
		if e := os.WriteFile(cookie, []byte("synthetic cookie"), 0600); e != nil {
			t.Fatal(e)
		}
		t.Setenv("STORAGEBUCKET_GALLERYDL_FIXTURE", "1")
		g := engine.GalleryDL{Executable: os.Args[0], CookieFile: cookie}
		for _, tc := range []struct {
			name, raw, stderr string
			want              error
		}{
			{"valid", fmt.Sprintf(`[[2,%s],[3,"https://cdn.invalid/photo.jpg",%s]]`, directory, file), "", nil},
			{"auth", `[[-1,{"error":"AuthRequired","message":"cookies needed"}]]`, "", engine.ErrAuthenticationRequired},
			{"private", `[[-1,{"error":"AuthorizationError","message":"private"}]]`, "", engine.ErrAccessDenied},
			{"missing", `[[-1,{"error":"NotFoundError","message":"post"}]]`, "", engine.ErrMediaUnavailable},
			{"throttled", `[[-1,{"error":"HttpError","message":"429 Too Many Requests"}]]`, "", engine.ErrRateLimited},
		} {
			t.Run(tc.name, func(t *testing.T) {
				work, e := os.MkdirTemp(root, "discovery-")
				if e != nil {
					t.Fatal(e)
				}
				t.Setenv("STORAGEBUCKET_GALLERYDL_FIXTURE_RESPONSE", tc.raw)
				t.Setenv("STORAGEBUCKET_GALLERYDL_FIXTURE_STDERR", tc.stderr)
				result, e := g.Discover(t.Context(), originalURL, work)
				if tc.want != nil {
					if !errors.Is(e, tc.want) || len(result.Files) != 0 {
						t.Fatalf("명시된 추출 오류 누락: %v", e)
					}
				} else if e != nil || len(result.Files) != 1 {
					t.Fatalf("조회 실패: %+v %v", result, e)
				}
				if b, e := os.ReadFile(cookie); e != nil || string(b) != "synthetic cookie" {
					t.Fatal("원본 쿠키 변경")
				}
				if _, e := os.Stat(filepath.Join(work, "cookies.txt")); !os.IsNotExist(e) {
					t.Fatal("작업용 쿠키 사본이 남았습니다")
				}
			})
		}
	})
}
