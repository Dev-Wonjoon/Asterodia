package webserver_test

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/media"
)

func TestSearchSuggestionsHTTP(t *testing.T) {
	f := newFixture(t)
	for i, title := range []string{"여행", "은행", "행복"} {
		if _, err := f.db.Exec("INSERT INTO media(id,source_kind,source_title,created_at)VALUES(?,'legacy',?,1)", i+1, title); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec("INSERT INTO tag VALUES(1,'여행',1);INSERT INTO media_tag VALUES(1,1)"); err != nil {
		t.Fatal(err)
	}
	status(t, f.server.Start())
	endpoint := "/api/search-suggestions?q=" + url.QueryEscape("행")
	for _, indexed := range []bool{false, true} {
		if indexed {
			catalog := media.NewService(f.db)
			for {
				work, err := catalog.RefreshSearchIndex(t.Context(), 128)
				if err != nil {
					t.Fatal(err)
				}
				if !work {
					break
				}
			}
		}
		code, headers, data := f.request(t, "GET", endpoint+"&limit=1", nil)
		if code != 200 || headers.Get("Cache-Control") != "private, no-store" {
			t.Fatal(code, headers)
		}
		var page struct {
			Data    []struct{ Kind, Value string }
			HasNext bool `json:"hasNextPage"`
			Indexed bool
		}
		if err := json.Unmarshal(data, &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Data) != 1 || !page.HasNext || page.Indexed != indexed || !strings.Contains(page.Data[0].Value, "행") {
			t.Fatal(string(data))
		}
		code, _, data = f.request(t, "GET", endpoint+"&tag="+url.QueryEscape("여행"), nil)
		if code != 200 || strings.Contains(string(data), "은행") {
			t.Fatal("필터 누락", string(data))
		}
	}
	for _, path := range []string{endpoint + "&limit=51", endpoint + "&offset=-1", "/api/search-suggestions?q=" + strings.Repeat("a", 1001), "/api/search-suggestions?q=%00"} {
		if code, _, _ := f.request(t, "GET", path, nil); code != 400 {
			t.Fatal(path, code)
		}
	}
	if code, _, data := f.request(t, "HEAD", endpoint, nil); code != 200 || len(data) > 0 {
		t.Fatal(code, len(data))
	}
	if code, _, _ := f.request(t, "POST", endpoint, nil); code != 405 {
		t.Fatal("쓰기 요청 허용", code)
	}
	if code, _, _ := f.request(t, "GET", endpoint, map[string]string{"Sec-Fetch-Site": "cross-site"}); code != 403 {
		t.Fatal("교차 사이트 요청 허용", code)
	}
	for _, path := range []string{"/search.js", "/search.css"} {
		if code, _, _ := f.request(t, "GET", path, nil); code != 200 {
			t.Fatal(path, code)
		}
	}
}

func TestResponsiveSearchPageAssets(t *testing.T) {
	f := newFixture(t)
	status(t, f.server.Start())
	code, headers, page := f.request(t, "GET", "/", nil)
	if code != 200 || !strings.Contains(string(page), `id="root"`) || strings.Contains(string(page), `src="/app.js"`) || !strings.Contains(headers.Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatalf("shared search entry: %d %s", code, page)
	}
	assets := regexp.MustCompile(`(?:src|href)="(/ui/[^" ]+)"`).FindAllSubmatch(page, -1)
	if len(assets) < 2 {
		t.Fatalf("missing React assets: %s", page)
	}
	for _, asset := range assets {
		code, _, data := f.request(t, "GET", string(asset[1]), nil)
		if code != 200 || len(data) == 0 {
			t.Fatalf("missing asset %s: %d", asset[1], code)
		}
	}
}

func TestSeparateSearchFiltersHTTP(t *testing.T) {
	f := newFixture(t)
	first, file, _ := f.seed(t, "first", "video", ".mp4")
	second, _, _ := f.seed(t, "second", "image", ".png")
	_, _, _ = f.seed(t, "other", "image", ".png")
	for _, stmt := range []string{
		"INSERT INTO platform(id,name) VALUES(1,'YouTube'),(2,'Pinterest')",
		"INSERT INTO profiles(id,owner_id,owner_name,platform_id,updated_at) VALUES(1,'artist','작가',1,0)",
		"INSERT INTO tag(id,name,updated_at) VALUES(1,'여행',0),(2,'풍경',0)",
	} {
		if _, err := f.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec("UPDATE media_file SET platform_id=1,profile_id=1 WHERE id=?", file); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("INSERT INTO media_tag VALUES(?,1),(?,2)", first, second); err != nil {
		t.Fatal(err)
	}
	status(t, f.server.Start())
	for kind, value := range map[string]string{"tag": "여행", "platform": "YouTube", "author": "작가"} {
		code, _, data := f.request(t, "GET", "/api/suggestions?"+url.Values{"kind": {kind}, "q": {value}}.Encode(), nil)
		var names []string
		if err := json.Unmarshal(data, &names); err != nil {
			t.Fatal(err)
		}
		if code != 200 || len(names) != 1 || names[0] != value {
			t.Fatalf("%s suggestions: %d %s", kind, code, data)
		}
	}
	readTotal := func(values url.Values) int {
		t.Helper()
		code, _, data := f.request(t, "GET", "/api/library?"+values.Encode(), nil)
		var page struct{ Total int }
		if err := json.Unmarshal(data, &page); err != nil {
			t.Fatal(err)
		}
		if code != 200 {
			t.Fatal(code, string(data))
		}
		return page.Total
	}
	filter := url.Values{"tag": {"여행", "풍경"}, "tagMode": {"or"}}
	if count := readTotal(filter); count != 2 {
		t.Fatal("tag OR", count)
	}
	filter.Set("tagMode", "and")
	if count := readTotal(filter); count != 0 {
		t.Fatal("tag AND", count)
	}
	filter = url.Values{"tag": {"여행"}, "platform": {"YouTube"}, "author": {"작가"}}
	if count := readTotal(filter); count != 1 {
		t.Fatal("combined filters", count)
	}
	filter.Set("platform", "Pinterest")
	if count := readTotal(filter); count != 0 {
		t.Fatal("platform filter omitted", count)
	}
	code, _, data := f.request(t, "GET", "/api/search-suggestions?kind=platform&q=You", nil)
	if code != 200 || !strings.Contains(string(data), `"kind":"platform"`) {
		t.Fatal(code, string(data))
	}
	for _, endpoint := range []string{"/api/suggestions?kind=bad", "/api/suggestions?kind=tag&limit=51", "/api/search-suggestions?kind=bad", "/api/library?tagMode=bad"} {
		if code, _, _ := f.request(t, "GET", endpoint, nil); code != 400 {
			t.Fatal(endpoint, code)
		}
	}
}
