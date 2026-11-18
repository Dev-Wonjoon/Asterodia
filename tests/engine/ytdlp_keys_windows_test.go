package engine_test

import (
	"encoding/json"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

func TestYtdlpTargetKeyCompatibility(t *testing.T) {
	cases := []struct {
		name, id, extractor, pageURL, want string
	}{
		{
			name:      "youtube",
			id:        "BaW_jenozKc",
			extractor: "Youtube",
			pageURL:   "https://www.youtube.com/watch?v=BaW_jenozKc&start=1",
			want:      "youtube:BaW_jenozKc",
		},
		{
			name:      "extractor-origin",
			id:        "123",
			extractor: "Fixture",
			pageURL:   "https://EXAMPLE.invalid:443/watch/123?custom=value",
			want:      "ytdlp:v1:68774613598c35c4ef81e2b5fb6e5da4c8b71ef26060ace728b01fb75e657895",
		},
		{
			name:      "different-origin",
			id:        "123",
			extractor: "Fixture",
			pageURL:   "https://other.invalid/watch/123?custom=value",
			want:      "ytdlp:v1:2dcf63682b7cdddd75a75d4484e0ed4b6cdc840a4241695b1b6ca859f49c1bf8",
		},
		{
			name:      "generic-query",
			id:        "123",
			extractor: "Generic",
			pageURL:   "https://EXAMPLE.invalid:443/watch/123?custom=value",
			want:      "ytdlp:v1:514a4d6fbcfabfa3b86da7b804509cdf5e63f6606cc72f3975c20b142defaf52",
		},
		{
			name:      "generic-other-query",
			id:        "123",
			extractor: "Generic",
			pageURL:   "https://example.invalid/watch/123?custom=other",
			want:      "ytdlp:v1:9fc3c0d3d1fc9dd525323dfd36147e063430bf89fa26ee9d0d01fa0ac97bb184",
		},
		{
			name:      "javascript-json-encoding",
			id:        "x<&\u2028\u2029",
			extractor: "Fixture",
			pageURL:   "https://example.invalid/watch/123",
			want:      "ytdlp:v1:c227c7000688ba42d2f197d26aed3bae45e70184d5ee6b82bad3b7ae13db3568",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, signature := range []string{"old", "new"} {
				raw, err := json.Marshal(map[string]string{
					"id":            tc.id,
					"extractor_key": tc.extractor,
					"webpage_url":   tc.pageURL,
					"url":           "https://cdn.invalid/file.mp4?signature=" + signature,
				})
				if err != nil {
					t.Fatal(err)
				}

				targets, err := engine.ParseYtdlpTargets(raw, tc.pageURL)
				if err != nil {
					t.Fatal(err)
				}
				if len(targets) != 1 {
					t.Fatalf("대상 수: %d, 기대값: 1", len(targets))
				}
				if got := targets[0].TargetKey; got != tc.want {
					t.Fatalf("서명 %s의 대상 키\n실제: %s\n기대: %s",
						signature, got, tc.want)
				}
			}
		})
	}
}