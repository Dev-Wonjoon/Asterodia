package mediaedit_test

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"github.com/Dev-Wonjoon/StorageBucket/internal/mediaedit"
	"github.com/Dev-Wonjoon/StorageBucket/internal/webserver"
	"github.com/google/uuid"
)

func TestMobileOutputOptionsAndJobsKeepPhysicalPathsPrivate(t *testing.T) {
	f := newFixture(t)
	album := outputAlbum(t, f, "여행")
	v := f.prepare(t, "gif")
	req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{Width: 160, Height: 120}, OutputDirectory: filepath.Join(f.root, "private-working"), AlbumIDs: []int64{album}}
	job := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
	server := webserver.NewService(media.NewService(f.db), "127.0.0.1:0")
	server.ConfigureEditor(f.service)
	t.Cleanup(server.Close)
	state := success[webserver.Status](t, server.Start())
	if !server.SetEditing(true).OK {
		t.Fatal("editing not enabled")
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	connectMobileClient(t, client, server)
	csrf := ""
	request := func(method, path, body string) (int, []byte) {
		t.Helper()
		r, err := http.NewRequest(method, state.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", state.URL)
		r.Header.Set("X-StorageBucket-CSRF", csrf)
		response, err := client.Do(r)
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
	_, status := request("GET", "/api/edit/status", "")
	var access struct{ CSRF string }
	json.Unmarshal(status, &access)
	csrf = access.CSRF
	code, body := request("GET", "/api/edit/output-options", "")
	var options struct{ Data mediaedit.OutputOptions }
	if err := json.Unmarshal(body, &options); err != nil || code != 200 || options.Data.Directory != "" || len(options.Data.Albums) != 1 {
		t.Fatalf("output choices: %d %s %v", code, body, err)
	}
	for _, path := range []string{"/api/edit/job?id=" + job.ID, "/api/edit/list"} {
		code, body = request("GET", path, "")
		if code != 200 || strings.Contains(string(body), "outputDirectory") {
			t.Fatalf("PC output path exposed: %d %s", code, body)
		}
	}
	req.RequestID = uuid.NewString()
	encoded, _ := json.Marshal(req)
	for _, path := range []string{"/api/edit/create", "/api/edit/resources"} {
		if code, _ = request("POST", path, string(encoded)); code != 400 {
			t.Fatal("mobile supplied physical output path", path, code)
		}
	}
	req.OutputDirectory = ""
	req.TagNames = []string{"모바일 결과"}
	encoded, _ = json.Marshal(req)
	code, body = request("POST", "/api/edit/create", string(encoded))
	if code != 200 || strings.Contains(string(body), "outputDirectory") {
		t.Fatalf("mobile classification failed: %d %s", code, body)
	}
	mobile := f.waitJob(t, req.RequestID, "completed")
	var count int
	f.db.QueryRow("SELECT count(*) FROM user_album_file WHERE album_id=? AND file_id=?", album, *mobile.ResultFileID).Scan(&count)
	if count != 1 {
		t.Fatal("mobile output not linked", count)
	}
}
