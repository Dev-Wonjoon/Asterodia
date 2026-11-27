package engine_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
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
	t.Run("Generic 직접 파일과 스트리밍 판정", func(t *testing.T) {
		for _, tc := range []struct {
			name, ext, protocol                  string
			audio, silent, fragment, empty, want bool
		}{
			{name: "direct-mp4", ext: "mp4", protocol: "https", want: true},
			{name: "silent-mp4", ext: "mp4", protocol: "https", silent: true, want: true},
			{name: "direct-m4a", ext: "m4a", protocol: "https", audio: true, want: true},
			{name: "hls", ext: "mp4", protocol: "m3u8_native", want: true},
			{name: "hls-ffmpeg", ext: "mp4", protocol: "m3u8", want: true},
			{name: "dash", ext: "mp4", protocol: "http_dash_segments", fragment: true, want: true},
			{name: "fragmented-http", ext: "mp4", protocol: "https", fragment: true},
			{name: "unsupported-webm", ext: "webm", protocol: "https"},
			{name: "missing-formats", empty: true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				row := map[string]any{"id": "one", "extractor_key": "Generic", "webpage_url": "https://example.invalid/page"}
				if !tc.empty {
					row["format_id"], row["url"], row["ext"], row["protocol"], row["height"], row["vcodec"], row["acodec"] = "source", "https://cdn.invalid/media?signature=fresh", tc.ext, tc.protocol, 720, "h264", "aac"
					if tc.audio {
						row["vcodec"] = "none"
					}
					if tc.silent {
						row["acodec"] = "none"
					}
					if tc.fragment {
						row["fragments"] = []map[string]string{{"url": "https://cdn.invalid/segment"}}
					}
				}
				raw, _ := json.Marshal(row)
				targets, e := engine.ParseYtdlpTargets(raw, "https://example.invalid/page")
				if e != nil {
					t.Fatal(e)
				}
				options := engine.Options{}
				if tc.audio {
					options.Type = "audio"
				}
				e = targets[0].ValidateDirectDownload(options)
				if tc.want {
					if e != nil {
						t.Fatal(e)
					}
				} else if !errors.Is(e, engine.ErrUnsupportedMediaFormat) {
					t.Fatalf("미지원 형식 오류 누락: %v", e)
				}
			})
		}
	})
	t.Run("Generic 여러 항목의 ID와 완전성", func(t *testing.T) {
		for _, mode := range []string{"valid", "duplicate", "missing", "incomplete"} {
			t.Run(mode, func(t *testing.T) {
				entry := func(id string) map[string]string {
					return map[string]string{"id": id, "extractor_key": "Generic", "webpage_url": "https://example.invalid/page?view=one"}
				}
				entries := []any{entry("one"), entry("two")}
				switch mode {
				case "duplicate":
					entries[1] = entry("one")
				case "missing":
					entries[1] = nil
				}
				row := map[string]any{"_type": "playlist", "entries": entries, "n_entries": 2}
				if mode == "incomplete" {
					row["n_entries"] = 3
				}
				raw, _ := json.Marshal(row)
				targets, e := engine.ParseYtdlpTargets(raw, "https://example.invalid/page?view=one")
				if mode != "valid" {
					if e == nil || len(targets) != 0 {
						t.Fatal("불완전하거나 중복된 대상을 허용했습니다")
					}
					return
				}
				if e != nil || len(targets) != 2 || targets[0].TargetKey == targets[1].TargetKey {
					t.Fatalf("여러 대상 식별 실패: %+v %v", targets, e)
				}
			})
		}
	})
}
