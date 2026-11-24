package webserver_test

import (
	"strings"
	"testing"
)

func TestLocalizedLicensePageAndSharedTranslationAssets(t *testing.T) {
	f := newFixture(t)
	status(t, f.server.Start())
	for _, item := range []struct {
		path, accept, language, title string
	}{
		{"/licenses?lang=en", "ko-KR", "en", "Open-source licenses"},
		{"/licenses?lang=ko", "en-US", "ko", "오픈소스 라이선스"},
		{"/licenses", "en-US,en;q=0.9", "en", "Open-source licenses"},
		{"/licenses", "ko-KR,en;q=0.9", "ko", "오픈소스 라이선스"},
		{"/licenses", "ko;q=1,en;q=0.9", "ko", "오픈소스 라이선스"},
		{"/licenses", "ja-JP", "en", "Open-source licenses"},
	} {
		code, header, body := f.request(t, "GET", item.path, map[string]string{"Accept-Language": item.accept})
		if code != 200 || header.Get("Content-Language") != item.language || !strings.Contains(string(body), `lang="`+item.language+`"`) || !strings.Contains(string(body), item.title) {
			t.Fatalf("localized license page %s (%s): %d %s", item.path, item.accept, code, header.Get("Content-Language"))
		}
		if !strings.Contains(string(body), "lucide-react") || strings.Contains(string(body), "<script") {
			t.Fatal("license text was removed or rendered as executable markup")
		}
	}
	for _, asset := range []string{"/i18n.js", "/translations.js", "/shortcuts.js", "/langs/ko.json", "/langs/en.json"} {
		code, header, body := f.request(t, "GET", asset, nil)
		if code != 200 || len(body) == 0 || !strings.Contains(header.Get("Content-Security-Policy"), "script-src 'self'") {
			t.Fatalf("shared translation asset %s: %d", asset, code)
		}
		if strings.HasSuffix(asset, ".json") && !strings.Contains(header.Get("Content-Type"), "application/json") {
			t.Fatalf("language catalog has incorrect MIME type: %s", header.Get("Content-Type"))
		}
	}
}
