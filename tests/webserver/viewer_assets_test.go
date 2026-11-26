package webserver_test

import (
	"strings"
	"testing"
)

func TestSharedViewerModulesAreServed(t *testing.T) {
	f := newFixture(t)
	status(t, f.server.Start())
	for _, path := range []string{"/viewer.js", "/viewer-viewport.js", "/viewer-picker.js", "/viewer-video-renderer.js", "/player-performance.js", "/viewer.css", "/player.js", "/player.css", "/shortcuts.js"} {
		code, headers, body := f.request(t, "GET", path, nil)
		if code != 200 || len(body) == 0 {
			t.Fatalf("shared viewer asset %s: %d", path, code)
		}
		if strings.HasSuffix(path, ".js") && !strings.Contains(headers.Get("Content-Type"), "javascript") {
			t.Fatalf("incorrect module MIME type for %s: %s", path, headers.Get("Content-Type"))
		}
	}
}
