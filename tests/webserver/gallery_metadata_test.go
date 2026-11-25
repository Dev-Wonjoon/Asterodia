package webserver_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func TestGalleryFileExtensionsWithoutPrivatePaths(t *testing.T) {
	f := newFixture(t)
	id, fileID, _ := f.seed(t, "노래", "audio", ".mp3")
	status(t, f.server.Start())
	for _, endpoint := range []string{fmt.Sprintf("/api/library/%d", id), fmt.Sprintf("/api/files/%d/info", fileID)} {
		code, _, data := f.request(t, "GET", endpoint, nil)
		if code != 200 {
			t.Fatalf("%s: %d %s", endpoint, code, data)
		}
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		if body["extension"] != "MP3" {
			t.Fatal("원본 확장자 누락", body)
		}
		if bytes.Contains(data, []byte(f.root)) || bytes.Contains(data, []byte("filepath")) {
			t.Fatal("비공개 경로가 노출됐습니다", string(data))
		}
	}
}
