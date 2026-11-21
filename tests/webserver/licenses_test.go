package webserver_test

import (
	"strconv"
	"strings"
	"testing"
)

func TestOfflineLicenseRoutes(t *testing.T) {
	f := newFixture(t)
	status(t, f.server.Start())
	for _, path := range []string{"/licenses", "/licenses.txt", "/licenses.css"} {
		code, header, body := f.request(t, "GET", path, nil)
		if code != 200 || len(body) == 0 || header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("GET %s: %d", path, code)
		}
		headCode, headHeader, headBody := f.request(t, "HEAD", path, nil)
		if headCode != 200 || len(headBody) != 0 || headHeader.Get("Content-Length") != strconv.Itoa(len(body)) {
			t.Fatalf("HEAD %s: %d %v", path, headCode, headHeader)
		}
		if path == "/licenses" {
			for _, text := range []string{"lucide-react", "ImageMagick", "Go standard library", "John Chadwick", "전체 원문 저장", "&lt;"} {
				if !strings.Contains(string(body), text) {
					t.Fatalf("HTML missing %s", text)
				}
			}
			if strings.Contains(string(body), "<script") {
				t.Fatal("notice text became executable HTML")
			}
		}
		if path == "/licenses.txt" && (!strings.HasPrefix(header.Get("Content-Type"), "text/plain") || header.Get("Content-Disposition") == "" || !strings.Contains(string(body), "THIRD-PARTY LICENSES")) {
			t.Fatal("missing offline text download")
		}
		if code, _, _ := f.request(t, "POST", path, nil); code != 405 {
			t.Fatalf("write allowed: %s", path)
		}
		if code, _, _ := f.request(t, "GET", path, map[string]string{"Origin": "https://untrusted.example"}); code != 403 {
			t.Fatalf("cross-origin allowed: %s", path)
		}
	}
	_, _, home := f.request(t, "GET", "/", nil)
	if !strings.Contains(string(home), `href="/licenses"`) {
		t.Fatal("missing mobile footer link")
	}
}
