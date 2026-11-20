package webserver_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"github.com/Dev-Wonjoon/StorageBucket/internal/webserver"
)

type fixture struct {
	db     *sql.DB
	root   string
	server *webserver.Service
	client *http.Client
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	repo, err := filepath.EvalSymlinks(filepath.Join(filepath.Dir(source), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	base, err := filepath.EvalSymlinks(os.Getenv("STORAGEBUCKET_TEST_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(filepath.Join(repo, ".test-data", "webserver"), base)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		t.Fatal(".test-data/webserver 아래의 실행별 폴더가 필요합니다")
	}
	root, err := os.MkdirTemp(base, "server-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err = database.Initialize(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	server := webserver.NewService(media.NewService(db), "127.0.0.1:0")
	t.Cleanup(server.Close)
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	return &fixture{db: db, root: root, server: server, client: &http.Client{Transport: transport, Timeout: 3 * time.Second}}
}

func status(t *testing.T, result api.Result) webserver.Status {
	t.Helper()
	if !result.OK {
		t.Fatalf("서비스 실패: %+v", result.Error)
	}
	return result.Data.(webserver.Status)
}
func failure(t *testing.T, result api.Result, code string) {
	t.Helper()
	if result.OK || result.Error.Code != code {
		t.Fatalf("기대 %s: %+v", code, result)
	}
}
func (f *fixture) request(t *testing.T, method, path string, headers map[string]string) (int, http.Header, []byte) {
	t.Helper()
	state := status(t, f.server.Status())
	req, err := http.NewRequest(method, state.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		if key == "Host" {
			req.Host = value
		} else {
			req.Header.Set(key, value)
		}
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
	return response.StatusCode, response.Header, data
}
func (f *fixture) seed(t *testing.T, title, kind, ext string) (int64, int64, string) {
	t.Helper()
	result, err := f.db.Exec("INSERT INTO media(source_kind,source_title,created_at) VALUES('legacy',?,0)", title)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	path := filepath.Join(f.root, fmt.Sprintf("private-file-%d%s", id, ext))
	var data bytes.Buffer
	if ext == ".png" {
		if err = png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 8, 6))); err != nil {
			t.Fatal(err)
		}
	} else {
		data.WriteString(strings.Repeat("fixture-media-", 100))
	}
	if err = os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	result, err = f.db.Exec("INSERT INTO media_file(media_id,file_key,storage_kind,filepath,kind,position,created_at) VALUES(?,'fixture','unverified_legacy',?,?,0,0)", id, path, kind)
	if err != nil {
		t.Fatal(err)
	}
	fileID, _ := result.LastInsertId()
	return id, fileID, path
}

func TestServerLifecycleAndLoopbackBinding(t *testing.T) {
	f := newFixture(t)
	if status(t, f.server.Status()).Running {
		t.Fatal("기본 상태는 꺼짐이어야 합니다")
	}
	first := status(t, f.server.Start())
	if !first.Running || !strings.HasPrefix(first.URL, "http://127.0.0.1:") {
		t.Fatal(first)
	}
	if status(t, f.server.Start()).URL != first.URL {
		t.Fatal("중복 시작")
	}
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		code, headers, data := f.request(t, "GET", path, nil)
		if code != 200 || len(data) == 0 || headers.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("정적 화면 %s: %d", path, code)
		}
	}
	if status(t, f.server.Stop()).Running || status(t, f.server.Stop()).Running {
		t.Fatal("종료 실패")
	}
	if response, err := f.client.Get(first.URL); err == nil {
		response.Body.Close()
		t.Fatal("종료 후 접속 허용")
	}
	status(t, f.server.Start())
	f.server.Close()
	failure(t, f.server.Start(), "NOT_READY")
	for _, address := range []string{"0.0.0.0:0", "192.168.1.5:0", "[::]:0", "localhost:0", "invalid"} {
		failure(t, webserver.NewService(media.NewService(f.db), address).Start(), "INVALID_ARGUMENT")
	}
	failure(t, webserver.NewService(nil, "").Start(), "NOT_READY")
	other := webserver.NewService(media.NewService(f.db), "")
	if state := status(t, other.Status()); state.Running || state.URL != "http://"+webserver.DefaultAddress {
		t.Fatal(state)
	}
	other.Close()
}

func TestServerPortConflict(t *testing.T) {
	f := newFixture(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := webserver.NewService(media.NewService(f.db), listener.Addr().String())
	defer server.Close()
	failure(t, server.Start(), "SERVER_START_FAILED")
	if status(t, server.Status()).Error == nil {
		t.Fatal("오류 표시 누락")
	}
	listener.Close()
	if state := status(t, server.Start()); !state.Running || state.Error != nil {
		t.Fatal(state)
	}
}

func TestFavicon(t *testing.T) {
	f := newFixture(t)
	status(t, f.server.Start())
	code, _, page := f.request(t, "GET", "/", nil)
	if code != http.StatusOK || !bytes.Contains(page, []byte(`rel="icon" type="image/x-icon" href="/favicon.ico"`)) {
		t.Fatal("페이지의 favicon 연결이 없습니다")
	}
	code, headers, data := f.request(t, "GET", "/favicon.ico", nil)
	contentType := headers.Get("Content-Type")
	if code != http.StatusOK || (contentType != "image/x-icon" && contentType != "image/vnd.microsoft.icon") || len(data) < 6 || !bytes.Equal(data[:4], []byte{0, 0, 1, 0}) || (data[4] == 0 && data[5] == 0) {
		t.Fatalf("유효한 ICO 응답이 아닙니다: 상태 %d, 형식 %s, 크기 %d", code, headers.Get("Content-Type"), len(data))
	}
	code, headHeaders, body := f.request(t, "HEAD", "/favicon.ico", nil)
	if code != http.StatusOK || len(body) != 0 || headHeaders.Get("Content-Type") != headers.Get("Content-Type") || headHeaders.Get("Content-Length") != headers.Get("Content-Length") {
		t.Fatal("favicon HEAD 응답이 GET 메타데이터와 다릅니다")
	}
}

func TestReadOnlyLibraryAndPrivacy(t *testing.T) {
	f := newFixture(t)
	id, fileID, path := f.seed(t, "<script>private title</script>", "image", ".png")
	for i := 0; i < 29; i++ {
		f.seed(t, fmt.Sprintf("item-%d", i), "audio", ".mp3")
	}
	status(t, f.server.Start())
	for _, endpoint := range []string{"/api/library?limit=24", fmt.Sprintf("/api/library/%d", id)} {
		code, _, data := f.request(t, "GET", endpoint, nil)
		if code != 200 {
			t.Fatalf("조회 %s: %d %s", endpoint, code, data)
		}
		for _, secret := range []string{f.root, "filepath", "thumbnailPath", "sourceUrl", "fileUuid", "locations"} {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatalf("경로/내부 정보 노출: %s", secret)
			}
		}
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		if endpoint == "/api/library?limit=24" && (len(body["data"].([]any)) != 24 || body["total"] != float64(30) || body["hasNextPage"] != true) {
			t.Fatal(body)
		}
	}
	code, _, data := f.request(t, "GET", "/api/library?q=private", nil)
	if code != 200 || !bytes.Contains(data, []byte(`"total":1`)) {
		t.Fatal(string(data))
	}
	code, _, data = f.request(t, "GET", "/api/library?offset=24", nil)
	if code != 200 || !bytes.Contains(data, []byte(`"hasNextPage":false`)) {
		t.Fatal(string(data))
	}
	for _, endpoint := range []string{"/api/library?limit=101", "/api/library?limit=bad", "/api/library?offset=-1", "/api/library?q=" + strings.Repeat("a", 1001), "/api/library/invalid", "/api/library/0", "/api/library/1?fileOffset=bad", "/api/library/1?fileOffset=-1", "/api/files/0/content", "/api/files/bad/content"} {
		code, _, _ := f.request(t, "GET", endpoint, nil)
		if code != 400 {
			t.Fatalf("검증 누락 %s: %d", endpoint, code)
		}
	}
	for _, endpoint := range []string{"/api/library/99999", "/api/files/99999/content", "/api/files/1/invalid", "/media?path=" + path, "/data/config.json", "/go.mod", "/wails/runtime.js"} {
		code, _, _ := f.request(t, "GET", endpoint, nil)
		if code != 404 {
			t.Fatalf("범위 외 공개 %s: %d", endpoint, code)
		}
	}
	for _, method := range []string{"POST", "PATCH", "PUT", "DELETE", "OPTIONS"} {
		code, _, _ := f.request(t, method, fmt.Sprintf("/api/library/%d", id), nil)
		if code != 405 {
			t.Fatal(method, code)
		}
	}
	if code, _, _ := f.request(t, "GET", fmt.Sprintf("/api/files/%d/content", fileID), nil); code != 200 {
		t.Fatal(code)
	}
}

func TestMediaRangesAndRegisteredIDs(t *testing.T) {
	f := newFixture(t)
	_, fileID, path := f.seed(t, "video", "video", ".mp4")
	_, _, thumb := f.seed(t, "thumbnail", "image", ".png")
	if _, err := f.db.Exec("UPDATE media_file SET thumbnail_path=?,thumbnail_origin='app_generated' WHERE id=?", thumb, fileID); err != nil {
		t.Fatal(err)
	}
	status(t, f.server.Start())
	endpoint := fmt.Sprintf("/api/files/%d/content", fileID)
	code, headers, data := f.request(t, "GET", endpoint, map[string]string{"Range": "bytes=0-7"})
	if code != 206 || string(data) != "fixture-" || headers.Get("Content-Range") == "" || headers.Get("Content-Type") != "video/mp4" {
		t.Fatal(code, headers, string(data))
	}
	tag := headers.Get("ETag")
	if code, _, data := f.request(t, "HEAD", endpoint, nil); code != 200 || len(data) != 0 {
		t.Fatal(code, len(data))
	}
	if code, _, _ := f.request(t, "HEAD", endpoint, map[string]string{"If-None-Match": tag}); code != 304 {
		t.Fatal(code)
	}
	if code, _, _ := f.request(t, "GET", endpoint, map[string]string{"Range": "bytes=999999-"}); code != 416 {
		t.Fatal(code)
	}
	if code, headers, _ := f.request(t, "GET", fmt.Sprintf("/api/files/%d/thumbnail", fileID), nil); code != 200 || headers.Get("Content-Type") != "image/png" {
		t.Fatal("영상 썸네일", code, headers)
	}
	if code, _, _ := f.request(t, "GET", fmt.Sprintf("/api/files/%d/preview", fileID), nil); code != 404 {
		t.Fatal(code)
	}
	if _, err := f.db.Exec("UPDATE media_file SET preview_path=? WHERE id=?", thumb, fileID); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := f.request(t, "GET", fmt.Sprintf("/api/files/%d/preview", fileID), nil); code != 200 {
		t.Fatal(code)
	}
	info, _ := os.Stat(path)
	if err := os.Chtimes(path, info.ModTime(), info.ModTime().Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if code, headers, _ := f.request(t, "HEAD", endpoint, map[string]string{"If-None-Match": tag}); code != 200 || headers.Get("ETag") == tag {
		t.Fatal("원본 변경 캐시")
	}
	_, svg, _ := f.seed(t, "svg", "image", ".svg")
	if code, _, _ := f.request(t, "GET", fmt.Sprintf("/api/files/%d/content", svg), nil); code != 415 {
		t.Fatal("활성 콘텐츠 제공", code)
	}
	_, missing, missingPath := f.seed(t, "missing", "image", ".png")
	os.Remove(missingPath)
	if code, _, _ := f.request(t, "GET", fmt.Sprintf("/api/files/%d/content", missing), nil); code != 404 {
		t.Fatal(code)
	}
	if _, err := media.NewService(f.db).OpenContent(t.Context(), -1, "content"); err == nil {
		t.Fatal("음수 파일 ID")
	}
	_, audio, _ := f.seed(t, "webm audio", "audio", ".webm")
	if code, headers, _ := f.request(t, "GET", fmt.Sprintf("/api/files/%d/content", audio), nil); code != 200 || headers.Get("Content-Type") != "audio/webm" {
		t.Fatal(code, headers)
	}
	_, relative, _ := f.seed(t, "relative", "image", ".png")
	if _, err := f.db.Exec("UPDATE media_file SET filepath='relative.png' WHERE id=?", relative); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := f.request(t, "GET", fmt.Sprintf("/api/files/%d/content", relative), nil); code != 404 {
		t.Fatal(code)
	}
	if _, err := f.db.Exec("UPDATE media_file SET filepath=? WHERE id=?", f.root, relative); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := f.request(t, "GET", fmt.Sprintf("/api/files/%d/content", relative), nil); code != 404 {
		t.Fatal(code)
	}
}

func TestDatabaseErrorsStayPrivate(t *testing.T) {
	f := newFixture(t)
	state := status(t, f.server.Start())
	request, err := http.NewRequest("GET", state.URL+"/api/library", strings.NewReader("unexpected body"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := f.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatal(response.StatusCode)
	}
	f.db.Close()
	for _, path := range []string{"/api/library", "/api/library/1", "/api/files/1/content"} {
		code, _, data := f.request(t, "GET", path, nil)
		if code != 500 && code != 404 {
			t.Fatal(path, code)
		}
		if strings.Contains(string(data), "sql:") || strings.Contains(string(data), f.root) {
			t.Fatal("내부 오류 정보 공개", string(data))
		}
	}
}

func TestHostOriginAndTailscaleProxy(t *testing.T) {
	f := newFixture(t)
	status(t, f.server.Start())
	for _, headers := range []map[string]string{{"Host": "evil.example"}, {"Host": "pc.ts.net.evil.example"}, {"Origin": "https://evil.example"}, {"Origin": "%"}, {"Origin": "null"}, {"Sec-Fetch-Site": "cross-site"}} {
		if code, _, _ := f.request(t, "GET", "/api/library", headers); code != 403 {
			t.Fatal(headers, code)
		}
	}
	if code, _, _ := f.request(t, "GET", "/api/library", map[string]string{"Host": "pc.tail123.ts.net", "Origin": "https://pc.tail123.ts.net", "X-Forwarded-Proto": "https"}); code != 200 {
		t.Fatal("Serve 프록시", code)
	}
	if code, _, _ := f.request(t, "GET", "/", map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}); code != 200 {
		t.Fatal("접속 링크로 첫 화면 이동", code)
	}
}

func TestStopClosesInFlightStream(t *testing.T) {
	f := newFixture(t)
	_, id, path := f.seed(t, "large", "video", ".mp4")
	if err := os.Truncate(path, 64<<20); err != nil {
		t.Fatal(err)
	}
	state := status(t, f.server.Start())
	response, err := f.client.Get(fmt.Sprintf("%s/api/files/%d/content", state.URL, id))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if _, err = io.ReadFull(response.Body, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { f.server.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("재생 중 서버 종료 지연")
	}
	if status(t, f.server.Status()).Running {
		t.Fatal("종료 상태 불일치")
	}
}
