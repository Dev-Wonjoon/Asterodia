package webserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"io"
	"math/big"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const sessionCookie = "StorageBucketSession"

type accessSession struct {
	expires time.Time
	ctx     context.Context
	cancel  context.CancelFunc
}

func newAccessSession(expires time.Time) accessSession {
	ctx, cancel := context.WithDeadline(context.Background(), expires)
	return accessSession{expires, ctx, cancel}
}
func (s *Service) clearSessionsLocked() {
	for _, session := range s.sessions {
		session.cancel()
	}
	s.sessions = make(map[[32]byte]accessSession)
}

func (s *Service) rotateCodeLocked() error {
	n, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		return err
	}
	s.pairCode = fmt.Sprintf("%08d", n.Int64())
	s.pairExpires = time.Now().Add(5 * time.Minute)
	return nil
}

func (s *Service) sessionContext(r *http.Request) (context.Context, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || len(cookie.Value) > 128 {
		return nil, false
	}
	key := sha256.Sum256([]byte(cookie.Value))
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[key]
	if !ok {
		return nil, false
	}
	if session.ctx.Err() != nil || time.Now().After(session.expires) {
		session.cancel()
		delete(s.sessions, key)
		return nil, false
	}
	return session.ctx, true
}
func (s *Service) authorized(r *http.Request) bool { _, ok := s.sessionContext(r); return ok }

// RevokeAccess is a desktop binding. HTTP clients cannot invoke it.
func (s *Service) RevokeAccess() api.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearSessionsLocked()
	if s.server != nil {
		if s.rotateCodeLocked() != nil {
			return api.Failure("NOT_READY", "연결 코드를 생성하지 못했습니다")
		}
	}
	return api.Success(s.snapshot())
}

func (s *Service) accessAPI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/access/status" && (r.Method == "GET" || r.Method == "HEAD") {
		respond(w, r, map[string]bool{"authenticated": s.authorized(r)})
		return
	}
	if r.Method != "POST" || (r.URL.Path != "/api/access/connect" && r.URL.Path != "/api/access/logout") {
		http.NotFound(w, r)
		return
	}
	origin, err := url.Parse(r.Header.Get("Origin"))
	mediaType, _, typeErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(origin.Host, r.Host) || (origin.Scheme != "http" && origin.Scheme != "https") || typeErr != nil || mediaType != "application/json" {
		http.Error(w, "접근이 허용되지 않습니다", 403)
		return
	}
	if r.URL.Path == "/api/access/logout" {
		if cookie, e := r.Cookie(sessionCookie); e == nil {
			s.mu.Lock()
			key := sha256.Sum256([]byte(cookie.Value))
			if session, ok := s.sessions[key]; ok {
				session.cancel()
				delete(s.sessions, key)
			}
			s.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode})
		respond(w, r, map[string]bool{"authenticated": false})
		return
	}
	s.mu.Lock()
	now := time.Now()
	s.loginTokens = min(10, s.loginTokens+now.Sub(s.loginLast).Seconds()/6)
	s.loginLast = now
	if s.loginTokens < 1 {
		s.mu.Unlock()
		w.Header().Set("Retry-After", "6")
		http.Error(w, "잠시 후 다시 시도하세요", 429)
		return
	}
	s.loginTokens--
	s.mu.Unlock()
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var body struct {
		Code string `json:"code"`
	}
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || len(body.Code) != 8 {
		http.Error(w, "연결 코드를 확인하세요", 403)
		return
	}
	tokenBytes := make([]byte, 32)
	if _, err = rand.Read(tokenBytes); err != nil {
		http.Error(w, "연결할 수 없습니다", 503)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	s.mu.Lock()
	if s.server == nil || s.pairCode == "" || time.Now().After(s.pairExpires) || subtle.ConstantTimeCompare([]byte(body.Code), []byte(s.pairCode)) != 1 {
		s.mu.Unlock()
		http.Error(w, "연결 코드가 잘못되었거나 만료됐습니다", 403)
		return
	}
	for key, session := range s.sessions {
		if now.After(session.expires) {
			session.cancel()
			delete(s.sessions, key)
		}
	}
	if len(s.sessions) >= 16 {
		s.mu.Unlock()
		http.Error(w, "PC에서 기존 연결을 해제하세요", 409)
		return
	}
	if s.rotateCodeLocked() != nil {
		s.mu.Unlock()
		http.Error(w, "연결할 수 없습니다", 503)
		return
	}
	expires := now.Add(8 * time.Hour)
	s.sessions[sha256.Sum256([]byte(token))] = newAccessSession(expires)
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", MaxAge: 8 * 60 * 60, Expires: expires, HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode})
	respond(w, r, map[string]bool{"authenticated": true})
}
