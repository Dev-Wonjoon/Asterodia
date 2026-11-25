package webserver_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"github.com/Dev-Wonjoon/StorageBucket/internal/settings"
	"github.com/Dev-Wonjoon/StorageBucket/internal/webserver"
)

func contentFilterToken(t *testing.T, f *fixture) string {
	t.Helper()
	code, _, body := f.request(t, "GET", "/api/content-filters", nil)
	var access struct {
		CSRF string `json:"csrf"`
	}
	if code != 200 || json.Unmarshal(body, &access) != nil || access.CSRF == "" {
		t.Fatal(code, string(body))
	}
	return access.CSRF
}

func postContentFilters(t *testing.T, f *fixture, token, body string, headers map[string]string) (int, []byte) {
	t.Helper()
	address := status(t, f.server.Status()).URL
	req, err := http.NewRequest("POST", address+"/api/content-filters", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", address)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-StorageBucket-CSRF", token)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, data
}

func TestWebContentFilterChangesAreSharedPersistedAndApplied(t *testing.T) {
	f := newFixture(t)
	_, _, _ = f.seed(t, "web item", "image", ".png")
	if _, err := f.db.Exec("INSERT INTO platform(id,name) VALUES(1,'web-site');UPDATE media_file SET platform_id=1"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.root, "preferences.json")
	config, err := settings.New(path, f.root, settings.Dialogs{})
	if err != nil {
		t.Fatal(err)
	}
	library := media.NewService(f.db)
	library.ConfigureContentFilters(config.GetContentFilters)
	f.server = webserver.NewService(library, "127.0.0.1:0")
	f.server.ConfigureContentFilters(config.SetContentFilters)
	t.Cleanup(f.server.Close)
	status(t, f.server.Start())
	token := contentFilterToken(t, f)
	wanted := media.ContentFilters{HiddenPlatforms: []string{"web-site"}, HiddenTags: []string{"spoiler"}}
	code, body := postContentFilters(t, f, token, `{"hiddenPlatforms":[" web-site ","web-site"],"hiddenTags":["spoiler"]}`, nil)
	var saved media.ContentFilters
	if code != 200 || json.Unmarshal(body, &saved) != nil || !reflect.DeepEqual(saved, wanted) || !reflect.DeepEqual(config.GetContentFilters(), wanted) {
		t.Fatal(code, string(body), config.GetContentFilters())
	}
	reloaded, err := settings.New(path, f.root, settings.Dialogs{})
	if err != nil || !reflect.DeepEqual(reloaded.GetContentFilters(), wanted) {
		t.Fatal(reloaded, err)
	}
	code, body = postContentFilters(t, f, token, `{"hiddenPlatforms":[" "],"hiddenTags":[]}`, nil)
	if code != 400 || !reflect.DeepEqual(config.GetContentFilters(), wanted) {
		t.Fatal("invalid write changed saved preferences", code, string(body))
	}
	for _, test := range []struct {
		path  string
		total int
	}{{"/api/library", 0}, {"/api/library?showHidden=1", 1}} {
		code, _, body = f.request(t, "GET", test.path, nil)
		var page struct {
			Total int `json:"total"`
		}
		if code != 200 || json.Unmarshal(body, &page) != nil || page.Total != test.total {
			t.Fatal(code, string(body))
		}
	}
	// PC changes appear through the same web endpoint.
	if _, err := config.SetContentFilters(media.ContentFilters{HiddenPlatforms: []string{"web-site"}}); err != nil {
		t.Fatal(err)
	}
	_, _, body = f.request(t, "GET", "/api/content-filters", nil)
	if json.Unmarshal(body, &saved) != nil || len(saved.HiddenTags) != 0 || len(saved.HiddenPlatforms) != 1 {
		t.Fatal(string(body))
	}
	code, body = postContentFilters(t, f, token, `{"hiddenPlatforms":[],"hiddenTags":[]}`, nil)
	if code != 200 {
		t.Fatal(code, string(body))
	}
	_, _, body = f.request(t, "GET", "/api/library", nil)
	var page struct {
		Total int `json:"total"`
	}
	if json.Unmarshal(body, &page) != nil || page.Total != 1 {
		t.Fatal(string(body))
	}
	status(t, f.server.Stop())
	status(t, f.server.Start())
	code, _ = postContentFilters(t, f, token, `{"hiddenTags":["stale-token"]}`, nil)
	if code != 403 || len(config.GetContentFilters().HiddenTags) != 0 {
		t.Fatal("stale token accepted", code)
	}
}

func TestWebContentFilterWritesRejectInvalidRequests(t *testing.T) {
	for _, test := range []struct {
		name, body string
		headers    map[string]string
		code       int
	}{
		{"missing origin", `{}`, map[string]string{"Origin": ""}, 403},
		{"foreign origin", `{}`, map[string]string{"Origin": "https://example.com"}, 403},
		{"cross site", `{}`, map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"missing token", `{}`, map[string]string{"X-StorageBucket-CSRF": ""}, 403},
		{"wrong type", `{}`, map[string]string{"Content-Type": "text/plain"}, 403},
		{"unknown field", `{"otherSetting":true}`, nil, 400},
		{"invalid json", `{`, nil, 400},
		{"null", `null`, nil, 400},
		{"extra body", `{} {}`, nil, 400},
		{"too large", `{"hiddenTags":["` + strings.Repeat("a", 256<<10) + `"]}`, nil, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			calls := 0
			f.server.ConfigureContentFilters(func(value media.ContentFilters) (media.ContentFilters, error) { calls++; return value, nil })
			status(t, f.server.Start())
			code, body := postContentFilters(t, f, contentFilterToken(t, f), test.body, test.headers)
			if code != test.code || calls != 0 {
				t.Fatal(code, calls, string(body))
			}
		})
	}
}

func TestWebContentFilterWritesAreRateLimited(t *testing.T) {
	f := newFixture(t)
	calls := 0
	f.server.ConfigureContentFilters(func(value media.ContentFilters) (media.ContentFilters, error) { calls++; return value, nil })
	status(t, f.server.Start())
	token := contentFilterToken(t, f)
	for i := 0; i < 9; i++ {
		code, body := postContentFilters(t, f, token, `{}`, nil)
		wanted := 200
		if i == 8 {
			wanted = 429
		}
		if code != wanted {
			t.Fatal(i, code, string(body))
		}
	}
	if calls != 8 {
		t.Fatal(calls)
	}
}

func TestHTTPContentFiltersAndTemporaryOverride(t *testing.T) {
	f := newFixture(t)
	id, _, _ := f.seed(t, "hidden item", "image", ".png")
	if _, err := f.db.Exec("INSERT INTO tag(id,name) VALUES(1,'blocked');INSERT INTO media_tag(media_id,tag_id) VALUES(?,1)", id); err != nil {
		t.Fatal(err)
	}
	library := media.NewService(f.db)
	library.ConfigureContentFilters(func() media.ContentFilters {
		return media.ContentFilters{HiddenPlatforms: []string{}, HiddenTags: []string{"blocked"}}
	})
	f.server.Close()
	f.server = webserver.NewService(library, "127.0.0.1:0")
	t.Cleanup(f.server.Close)
	status(t, f.server.Start())
	code, _, body := f.request(t, "GET", "/api/content-filters", nil)
	if code != 200 || !strings.Contains(string(body), "blocked") {
		t.Fatal(code, string(body))
	}
	for _, test := range []struct {
		path  string
		total int
	}{{"/api/library", 0}, {"/api/library?showHidden=1", 1}} {
		code, _, body = f.request(t, "GET", test.path, nil)
		var page struct {
			Total int `json:"total"`
		}
		if code != 200 || json.Unmarshal(body, &page) != nil || page.Total != test.total {
			t.Fatal(code, string(body))
		}
	}
}
