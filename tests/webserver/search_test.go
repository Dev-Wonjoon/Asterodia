package webserver_test

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
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
