package mediaedit_test

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"github.com/Dev-Wonjoon/Asterodia/internal/mediaedit"
	"github.com/Dev-Wonjoon/Asterodia/internal/webserver"
	"github.com/google/uuid"
)

func TestMobileBatchPermissionsAndPrivateOutputPaths(t *testing.T) {
	f := newFixture(t)
	server := webserver.NewService(media.NewService(f.db), "127.0.0.1:0")
	server.ConfigureEditor(f.service)
	t.Cleanup(server.Close)
	state := success[webserver.Status](t, server.Start())
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	connectMobileClient(t, client, server)
	csrf := ""
	request := func(method, path, body string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, state.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", state.URL)
		req.Header.Set("X-Asterodia-CSRF", csrf)
		response, err := client.Do(req)
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
	req := mediaedit.BatchRequest{RequestID: uuid.NewString(), Selection: []mediaedit.BatchSelection{batchSource(t, f)}, Mode: "webp", Duration: 1, Width: 64, FPS: 10, OutputDirectory: filepath.Join(f.root, "private-batch-output")}
	if code, _ := request("POST", "/api/edit/batch", "{}"); code != 403 {
		t.Fatal("editing permission bypass", code)
	}
	if !server.SetEditing(true).OK {
		t.Fatal("enable editing failed")
	}
	_, body := request("GET", "/api/edit/status", "")
	var access struct{ CSRF string }
	if err := json.Unmarshal(body, &access); err != nil {
		t.Fatal(err)
	}
	if code, _ := request("POST", "/api/edit/batch", "{}"); code != 403 {
		t.Fatal("CSRF bypass", code)
	}
	csrf = access.CSRF
	encoded, _ := json.Marshal(req)
	if code, _ := request("POST", "/api/edit/batch", string(encoded)); code != 400 {
		t.Fatal("mobile supplied output directory", code)
	}
	result := success[mediaedit.BatchResult](t, f.service.Batch(t.Context(), req))
	if len(result.Jobs) != 1 {
		t.Fatal(result)
	}
	for _, path := range []string{"/api/edit/list", "/api/edit/job?id=" + result.Jobs[0].ID} {
		code, data := request("GET", path, "")
		if code != 200 || strings.Contains(string(data), "outputDirectory") || strings.Contains(string(data), "private-batch-output") {
			t.Fatalf("physical paths leaked: %d %s", code, data)
		}
	}
	if job := awaitBatch(t, f, result.Jobs[0].ID); job.State != "completed" {
		t.Fatal(job)
	}
	req.RequestID = uuid.NewString()
	req.OutputDirectory = ""
	encoded, _ = json.Marshal(req)
	code, data := request("POST", "/api/edit/batch", string(encoded))
	if code != 200 || strings.Contains(string(data), "outputDirectory") {
		t.Fatalf("mobile batch failed: %d %s", code, data)
	}
	var mobile struct{ Data mediaedit.BatchResult }
	if err := json.Unmarshal(data, &mobile); err != nil || len(mobile.Data.Jobs) != 1 {
		t.Fatal(string(data), err)
	}
	if job := awaitBatch(t, f, mobile.Data.Jobs[0].ID); job.State != "completed" {
		t.Fatal(job)
	}
}
