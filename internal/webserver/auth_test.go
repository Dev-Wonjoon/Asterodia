package webserver

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func allowTestRequest(s *Service, r *http.Request) {
	s.mu.Lock()
	if s.sessions == nil {
		s.sessions = make(map[[32]byte]accessSession)
	}
	key := sha256.Sum256([]byte("test-session"))
	if previous, ok := s.sessions[key]; ok {
		previous.cancel()
	}
	s.sessions[key] = newAccessSession(time.Now().Add(time.Hour))
	s.mu.Unlock()
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "test-session"})
}

func authFixture(t *testing.T) *Service {
	t.Helper()
	s := NewService(nil, "")
	s.server = &http.Server{}
	s.sessions = make(map[[32]byte]accessSession)
	s.loginTokens = 10
	s.loginLast = time.Now()
	if s.rotateCodeLocked() != nil {
		t.Fatal("code generation failed")
	}
	return s
}

func authRequest(s *Service, path, body, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
	method := "POST"
	if path == "/api/access/status" {
		method = "GET"
	}
	r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", origin)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.serveHTTP(w, r)
	return w
}

func TestWebAccessCodeSessionAndRevocation(t *testing.T) {
	s := authFixture(t)
	code := s.pairCode
	for _, path := range []string{"/api/library", "/api/files/1/content", "/api/download/status", "/api/edit/status", "/api/albums/access", "/api/content-filters", "/edit-assets/jobs/id"} {
		r := httptest.NewRequest("GET", "http://localhost"+path, nil)
		w := httptest.NewRecorder()
		s.serveHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("unauthenticated %s returned %d", path, w.Code)
		}
	}
	status := authRequest(s, "/api/access/status", "", "", nil)
	if strings.Contains(status.Body.String(), code) || strings.Contains(status.Body.String(), "csrf") {
		t.Fatal("access status leaked secrets")
	}
	body, _ := json.Marshal(map[string]string{"code": code})
	for _, origin := range []string{"", "https://evil.example"} {
		if w := authRequest(s, "/api/access/connect", string(body), origin, nil); w.Code != 403 {
			t.Fatal("login accepted foreign origin", w.Code)
		}
	}
	w := authRequest(s, "/api/access/connect", string(body), "http://localhost", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("session cookie missing")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge != 28800 {
		t.Fatal("unsafe session cookie", cookie)
	}
	if w := authRequest(s, "/api/access/connect", string(body), "http://localhost", nil); w.Code != 403 {
		t.Fatal("connection code was reusable")
	}
	if w := authRequest(s, "/api/access/status", "", "", cookie); !strings.Contains(w.Body.String(), `"authenticated":true`) {
		t.Fatal(w.Body.String())
	}
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	r := httptest.NewRequest("GET", "http://localhost/api/library", nil)
	r.AddCookie(cookie)
	reply := httptest.NewRecorder()
	s.serveHTTP(reply, r)
	if reply.Code != 204 {
		t.Fatal("valid session blocked", reply.Code)
	}
	if !s.RevokeAccess().OK {
		t.Fatal("revoke failed")
	}
	if w := authRequest(s, "/api/access/status", "", "", cookie); strings.Contains(w.Body.String(), `"authenticated":true`) {
		t.Fatal("revoked cookie accepted")
	}
}

func TestWebAccessExpiryAndFailedAttemptLimit(t *testing.T) {
	s := authFixture(t)
	s.pairExpires = time.Now().Add(-time.Second)
	body := `{"code":"` + s.pairCode + `"}`
	if w := authRequest(s, "/api/access/connect", body, "http://localhost", nil); w.Code != 403 {
		t.Fatal("expired code accepted")
	}
	for range 10 {
		_ = authRequest(s, "/api/access/connect", `{"code":"invalid!"}`, "http://localhost", nil)
	}
	w := authRequest(s, "/api/access/connect", body, "http://localhost", nil)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("code brute force was not rate limited", w.Code)
	}
	s.sessions[sha256.Sum256([]byte("expired"))] = newAccessSession(time.Now().Add(-time.Second))
	if w := authRequest(s, "/api/access/status", "", "", &http.Cookie{Name: sessionCookie, Value: "expired"}); strings.Contains(w.Body.String(), `"authenticated":true`) {
		t.Fatal("expired session accepted")
	}
	s.Stop()
	if s.pairCode != "" || len(s.sessions) != 0 {
		t.Fatal("stop retained authentication")
	}
}

func TestRevokingAccessCancelsInFlightRequests(t *testing.T) {
	s := authFixture(t)
	defer s.Close()
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	done := make(chan struct{})
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(cancelled) })
	r := httptest.NewRequest("GET", "http://localhost/api/library", nil)
	allowTestRequest(s, r)
	go func() { defer close(done); s.serveHTTP(httptest.NewRecorder(), r) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request did not enter handler")
	}
	s.RevokeAccess()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("revocation did not cancel active request")
	}
	<-done
}

func TestAccessCodeToggleInvalidatesSessionsAndAnonymousRequests(t *testing.T) {
	s := authFixture(t)
	defer s.Close()
	if !s.snapshot().RequireCode {
		t.Fatal("code authentication must default to on")
	}
	r := httptest.NewRequest("GET", "http://localhost/api/library", nil)
	allowTestRequest(s, r)
	sessionCtx, _ := s.sessionContext(r)
	if !s.SetRequireCode(false).OK || sessionCtx.Err() == nil {
		t.Fatal("disabling code did not clear old sessions")
	}
	status := s.snapshot()
	if status.RequireCode || status.PairCode != "" || status.CodeExpires != 0 || status.Connected != 0 {
		t.Fatal("code controls remain active", status)
	}
	w := authRequest(s, "/api/access/status", "", "", nil)
	if !strings.Contains(w.Body.String(), `"authenticated":true`) || !strings.Contains(w.Body.String(), `"required":false`) {
		t.Fatal("anonymous client was not admitted", w.Body.String())
	}
	entered, cancelled, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(cancelled)
	})
	go func() {
		defer close(done)
		s.serveHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://localhost/api/library", nil))
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("anonymous request was blocked")
	}
	if !s.SetRequireCode(true).OK {
		t.Fatal("enabling code failed")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("enabling code did not cancel the anonymous request")
	}
	<-done
	if s.pairCode == "" {
		t.Fatal("enabling code did not generate a code")
	}
	for _, cookie := range []*http.Cookie{nil, {Name: sessionCookie, Value: "test-session"}} {
		if s.authorized(httptest.NewRequest("GET", "http://localhost/api/library", nil)) {
			t.Fatal("anonymous access survived enabling code")
		}
		w := authRequest(s, "/api/access/status", "", "", cookie)
		if strings.Contains(w.Body.String(), `"authenticated":true`) {
			t.Fatal("old session survived enabling code")
		}
	}
	w = authRequest(s, "/api/access/connect", `{"code":"`+s.pairCode+`"}`, "http://localhost", nil)
	if w.Code != 200 {
		t.Fatal("fresh code was rejected", w.Code, w.Body.String())
	}
}

func TestAccessCodePreferenceFailuresRetainCurrentPolicy(t *testing.T) {
	s := authFixture(t)
	defer s.Close()
	s.ConfigureAccessCode(true, func(bool) error { return errors.New("disk full") })
	code := s.pairCode
	r := httptest.NewRequest("GET", "http://localhost/api/library", nil)
	allowTestRequest(s, r)
	ctx, _ := s.sessionContext(r)
	if s.SetRequireCode(false).OK || !s.requireCode || s.pairCode != code || ctx.Err() != nil {
		t.Fatal("failed save changed access policy or disconnected a session")
	}
	s.ConfigureAccessCode(false, func(bool) error { return errors.New("disk full") })
	s.pairCode, s.pairExpires = "", time.Time{}
	if s.SetRequireCode(true).OK || s.requireCode || s.pairCode != "" {
		t.Fatal("failed save did not restore the open policy")
	}
}

func TestOpenAccessKeepsHostAndOriginChecks(t *testing.T) {
	s := authFixture(t)
	defer s.Close()
	s.SetRequireCode(false)
	for _, configure := range []func(*http.Request){
		func(r *http.Request) { r.Host = "evil.example" },
		func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
		func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
	} {
		r := httptest.NewRequest("GET", "http://localhost/api/library", nil)
		configure(r)
		w := httptest.NewRecorder()
		s.serveHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("open access bypassed request origin checks", w.Code)
		}
	}
}
