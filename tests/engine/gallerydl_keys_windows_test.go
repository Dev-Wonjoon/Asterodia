package engine_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

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
		"typename":"GraphImage","post_id":%q,"media_id":%q,"count":"1"
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
}
