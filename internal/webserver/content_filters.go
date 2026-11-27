package webserver

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/media"
)

// ConfigureContentFilters connects web changes to the same preferences used by the PC app.
//
//wails:ignore
func (s *Service) ConfigureContentFilters(save func(media.ContentFilters) (media.ContentFilters, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveContentFilters = save
}

func (s *Service) contentFilterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/content-filters", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		csrf := ""
		if s.saveContentFilters != nil {
			csrf = s.csrf
		}
		s.mu.Unlock()
		respond(w, r, struct {
			media.ContentFilters
			CSRF string `json:"csrf"`
		}{s.library.GetContentFilters(), csrf})
	})
	mux.HandleFunc("POST /api/content-filters", s.setContentFilters)
}

func (s *Service) setContentFilters(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	save, csrf := s.saveContentFilters, s.csrf
	s.mu.Unlock()
	origin, err := url.Parse(r.Header.Get("Origin"))
	contentType, _, typeErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if save == nil || err != nil || origin.Host != r.Host || (origin.Scheme != "http" && origin.Scheme != "https") || typeErr != nil || contentType != "application/json" || csrf == "" || subtle.ConstantTimeCompare([]byte(csrf), []byte(r.Header.Get("X-Asterodia-CSRF"))) != 1 {
		http.Error(w, "요청 출처와 숨김 설정 변경 권한을 확인하세요", http.StatusForbidden)
		return
	}
	s.mu.Lock()
	now := time.Now()
	s.editTokens = min(8, s.editTokens+now.Sub(s.editLast).Seconds())
	s.editLast = now
	allowed := s.editTokens >= 1
	if allowed {
		s.editTokens--
	}
	s.mu.Unlock()
	if !allowed {
		http.Error(w, "잠시 후 다시 요청하세요", http.StatusTooManyRequests)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var value *media.ContentFilters
	if decoder.Decode(&value) != nil || value == nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "요청 형식과 크기를 확인하세요", http.StatusBadRequest)
		return
	}
	saved, err := save(*value)
	if err != nil {
		http.Error(w, "숨김 설정을 저장하지 못했습니다", http.StatusBadRequest)
		return
	}
	respond(w, r, saved)
}
