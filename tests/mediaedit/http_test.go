package mediaedit_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"github.com/Dev-Wonjoon/StorageBucket/internal/mediaedit"
	"github.com/Dev-Wonjoon/StorageBucket/internal/webserver"
	"github.com/google/uuid"
)

func TestMobileWriteBoundaryAndSharedJobs(t *testing.T) {
	f := newFixture(t)
	server := webserver.NewService(media.NewService(f.db), "127.0.0.1:0")
	server.ConfigureEditor(f.service)
	t.Cleanup(server.Close)
	state := success[webserver.Status](t, server.Start())
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	request := func(method, path, body, csrf, origin string) (int, []byte, http.Header) {
		t.Helper()
		req, e := http.NewRequest(method, state.URL+path, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			req.Header.Set("X-StorageBucket-CSRF", csrf)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		response, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		data, e := io.ReadAll(response.Body)
		if e != nil {
			t.Fatal(e)
		}
		return response.StatusCode, data, response.Header
	}
	code, _, _ := request("POST", "/api/edit/prepare", "{}", "", state.URL)
	if code != 403 {
		t.Fatal(code)
	}
	if !server.SetEditing(true).OK {
		t.Fatal("편집 허용 실패")
	}
	_, body, _ := request("GET", "/api/edit/status", "", "", "")
	var access struct {
		Enabled bool
		CSRF    string
	}
	if e := json.Unmarshal(body, &access); e != nil || !access.Enabled || access.CSRF == "" {
		t.Fatal(string(body), e)
	}
	for _, bad := range []struct {
		csrf, origin, body string
		code               int
	}{{access.CSRF, "", "{}", 403}, {"wrong", state.URL, "{}", 403}, {access.CSRF, "https://evil.test", "{}", 403}, {access.CSRF, state.URL, `{"filepath":"C:/private.mp4"}`, 400}, {access.CSRF, state.URL, strings.Repeat(" ", 33<<10) + "{}", 400}} {
		code, _, _ := request("POST", "/api/edit/prepare", bad.body, bad.csrf, bad.origin)
		if code != bad.code {
			t.Fatalf("검증 기대 %d 실제 %d", bad.code, code)
		}
	}
	req := mediaedit.PrepareRequest{RequestID: uuid.NewString(), FileID: f.fileID, Kind: "gif", FullSource: true}
	encoded, _ := json.Marshal(req)
	code, body, _ = request("POST", "/api/edit/prepare", string(encoded), access.CSRF, state.URL)
	if code != 200 {
		t.Fatal(code, string(body))
	}
	if bytes.Contains(body, []byte(f.root)) || bytes.Contains(body, []byte("source_uuid")) {
		t.Fatal("내부 경로·식별자 노출")
	}
	code, _, _ = request("POST", "/api/edit/prepare", string(encoded), access.CSRF, state.URL)
	if code != 200 {
		t.Fatal("중복 요청 거절", code)
	}
	var count int
	if e := f.db.QueryRow("SELECT count(*) FROM media_edit_session").Scan(&count); e != nil || count != 1 {
		t.Fatal("중복 생성", count, e)
	}
	code, _, _ = request("GET", "/api/edit/create", "", "", "")
	if code != 404 {
		t.Fatal("GET으로 생성 허용", code)
	}
	code, _, _ = request("GET", "/edit-assets/sessions/not-a-uuid/0", "", "", "")
	if code != 404 {
		t.Fatal("잘못된 프레임 ID", code)
	}
	limited := false
	for n := 0; n < 10; n++ {
		code, _, _ = request("POST", "/api/edit/prepare", string(encoded), access.CSRF, state.URL)
		if code == 429 {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("쓰기 요청 빈도 제한 누락")
	}
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
		v := success[mediaedit.Session](t, f.service.GetSession(t.Context(), mediaedit.Request{ID: req.RequestID}))
		if v.State == "ready" {
			break
		}
	}
	code, body, headers := request("GET", "/edit-assets/sessions/"+req.RequestID+"/0?thumbnail=1", "", "", "")
	if code != 200 || headers.Get("Content-Type") != "image/jpeg" {
		t.Fatal("프레임 썸네일 제공 실패", code, string(body))
	}
	config, _, e := image.DecodeConfig(bytes.NewReader(body))
	if e != nil || config.Width > 120 || config.Height > 120 {
		t.Fatal("모바일 썸네일 크기", config, e)
	}
	server.Stop()
	if success[webserver.Status](t, server.Status()).Editing {
		t.Fatal("서버 종료 뒤 쓰기 허용 유지")
	}
	if success[mediaedit.Session](t, f.service.GetSession(t.Context(), mediaedit.Request{ID: req.RequestID})).ID != req.RequestID {
		t.Fatal("웹 서버 종료가 공통 작업을 없앴습니다")
	}
}

func TestMobileGIFInputMetadata(t *testing.T) {
	f := gifFixture(t)
	server := webserver.NewService(media.NewService(f.db), "127.0.0.1:0")
	server.ConfigureEditor(f.service)
	t.Cleanup(server.Close)
	state := success[webserver.Status](t, server.Start())
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	var mediaID int64
	if e := f.db.QueryRow("SELECT media_id FROM media_file WHERE id=?", f.fileID).Scan(&mediaID); e != nil {
		t.Fatal(e)
	}
	response, e := client.Get(fmt.Sprintf("%s/api/library/%d", state.URL, mediaID))
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	body, e := io.ReadAll(response.Body)
	if e != nil {
		t.Fatal(e)
	}
	if response.StatusCode != 200 || !bytes.Contains(body, []byte(`"detectedFormat":"gif"`)) || bytes.Contains(body, []byte(f.root)) {
		t.Fatal("GIF detail metadata", string(body))
	}
}
