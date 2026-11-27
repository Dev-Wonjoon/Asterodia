package webserver_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"github.com/Dev-Wonjoon/Asterodia/internal/webserver"
)

func TestWebAlbumExplorerCRUDSearchAndPrivacy(t *testing.T) {
	f := newFixture(t)
	root, file, path := f.seed(t, "root album", "image", ".png")
	status(t, f.server.Start())
	_, _, body := f.request(t, "GET", "/api/albums/access", nil)
	var access struct {
		CSRF string `json:"csrf"`
	}
	if json.Unmarshal(body, &access) != nil || access.CSRF == "" {
		t.Fatal(string(body))
	}
	post := func(payload any, token string) (int, []byte) {
		t.Helper()
		raw, _ := json.Marshal(payload)
		address := status(t, f.server.Status()).URL
		req, err := http.NewRequest("POST", address+"/api/albums", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", address)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Asterodia-CSRF", token)
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
	code, body := post(map[string]any{"title": "child", "parentId": root, "addFileIds": []int64{file}}, "")
	if code != 403 {
		t.Fatal("write accepted without token", code, string(body))
	}
	code, body = post(map[string]any{"title": "child", "parentId": root, "addFileIds": []int64{file}}, access.CSRF)
	var saved struct {
		OK   bool `json:"ok"`
		Data struct {
			ID       int64 `json:"id"`
			Revision int64 `json:"revision"`
		} `json:"data"`
	}
	if code != 200 || json.Unmarshal(body, &saved) != nil || !saved.OK || saved.Data.ID <= 0 {
		t.Fatal(code, string(body))
	}
	child := saved.Data.ID
	code, _, body = f.request(t, "GET", "/api/albums/"+jsonNumber(root)+"/contents?recursive=1", nil)
	var page struct {
		Total       int               `json:"total"`
		Data        []json.RawMessage `json:"data"`
		Breadcrumbs []media.Object    `json:"breadcrumbs"`
	}
	if code != 200 || json.Unmarshal(body, &page) != nil || page.Total != 2 || strings.Contains(string(body), path) || strings.Contains(string(body), "filepath") || strings.Contains(string(body), "thumbnailPath") {
		t.Fatal(code, string(body))
	}
	code, _, body = f.request(t, "GET", "/api/albums/"+jsonNumber(child)+"/contents", nil)
	if code != 200 || json.Unmarshal(body, &page) != nil || len(page.Breadcrumbs) != 2 {
		t.Fatal(code, string(body))
	}
	code, body = post(map[string]any{"albumId": child, "expectedRevision": saved.Data.Revision, "title": "renamed"}, access.CSRF)
	if code != 200 || json.Unmarshal(body, &saved) != nil || !saved.OK {
		t.Fatal(code, string(body))
	}
	_, body = post(map[string]any{"albumId": child, "expectedRevision": saved.Data.Revision, "parentId": 0}, access.CSRF)
	if json.Unmarshal(body, &saved) != nil || !saved.OK {
		t.Fatal(string(body))
	}
	_, body = post(map[string]any{"albumId": child, "expectedRevision": saved.Data.Revision, "delete": true}, access.CSRF)
	if json.Unmarshal(body, &saved) != nil || !saved.OK {
		t.Fatal(string(body))
	}
	code, _, _ = f.request(t, "GET", "/api/files/"+jsonNumber(file)+"/info", nil)
	if code != 200 {
		t.Fatal("album deletion removed original file")
	}
	for _, suffix := range []string{"?sort=unknown", "?limit=101", "?tagMode=bad", "?offset=-1"} {
		code, _, body = f.request(t, "GET", "/api/albums/"+jsonNumber(root)+"/contents"+suffix, nil)
		if code != 400 {
			t.Fatal(suffix, code, string(body))
		}
	}
}

func jsonNumber(id int64) string { data, _ := json.Marshal(id); return string(data) }

func TestExplorerHiddenChildrenAndAncestorAreNotExposed(t *testing.T) {
	f := newFixture(t)
	root, _, _ := f.seed(t, "root", "image", ".png")
	library := media.NewService(f.db)
	name := "hidden child"
	result := library.SaveAlbum(t.Context(), media.AlbumRequest{Title: &name, ParentID: &root})
	if !result.OK {
		t.Fatal(result)
	}
	child := result.Data.(media.Object)["id"].(int64)
	if _, err := f.db.Exec("INSERT INTO tag(id,name)VALUES(1,'blocked');INSERT INTO media_tag(media_id,tag_id)VALUES(?,1)", child); err != nil {
		t.Fatal(err)
	}
	library.ConfigureContentFilters(func() media.ContentFilters { return media.ContentFilters{HiddenTags: []string{"blocked"}} })
	// The HTTP service uses this exact configured library.
	f.server = webserver.NewService(library, "127.0.0.1:0")
	t.Cleanup(f.server.Close)
	status(t, f.server.Start())
	code, _, body := f.request(t, "GET", "/api/albums/"+jsonNumber(root)+"/contents?recursive=1", nil)
	if code != 200 || strings.Contains(string(body), name) {
		t.Fatal(code, string(body))
	}
	code, _, _ = f.request(t, "GET", "/api/albums/"+jsonNumber(child)+"/contents", nil)
	if code != 404 {
		t.Fatal("hidden child direct access", code)
	}
	code, _, body = f.request(t, "GET", "/api/albums/"+jsonNumber(child)+"/contents?showHidden=1", nil)
	if code != 200 || !strings.Contains(string(body), name) {
		t.Fatal(code, string(body))
	}
	grandName := "visible grandchild"
	grand := library.SaveAlbum(t.Context(), media.AlbumRequest{Title: &grandName, ParentID: &child})
	if !grand.OK {
		t.Fatal(grand)
	}
	grandID := grand.Data.(media.Object)["id"].(int64)
	code, _, _ = f.request(t, "GET", "/api/albums/"+jsonNumber(grandID)+"/contents", nil)
	if code != 404 {
		t.Fatal("hidden ancestor exposed its descendant", code)
	}
	code, _, body = f.request(t, "GET", "/api/library?q=visible", nil)
	if code != 200 || strings.Contains(string(body), grandName) {
		t.Fatal("search exposed a hidden descendant", code, string(body))
	}
}
