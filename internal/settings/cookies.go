package settings

import (
	"fmt"
	"github.com/Dev-Wonjoon/Asterodia/internal/cookies"
)

type CookieState struct {
	Groups  []cookies.Group `json:"groups"`
	Warning string          `json:"warning"`
}

//wails:ignore
func (s *Service) CookieStore() *cookies.Store { return s.cookieStore }

func (s *Service) GetCookies() CookieState {
	s.mu.Lock()
	warning := s.cookieWarning
	s.mu.Unlock()
	return CookieState{Groups: s.cookieStore.Groups(), Warning: warning}
}

func (s *Service) ImportCookies() (*CookieState, error) {
	if s.dialogs.CookieFile == nil {
		return nil, fmt.Errorf("파일 선택 창을 사용할 수 없습니다")
	}
	filename, err := s.dialogs.CookieFile()
	if err != nil || filename == "" {
		return nil, err
	}
	if err := s.cookieStore.ImportFile(filename); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cookieWarning = ""
	s.mu.Unlock()
	state := s.GetCookies()
	return &state, nil
}
func (s *Service) DeleteCookie(id string) (CookieState, error) {
	if err := s.cookieStore.Delete(id); err != nil {
		return CookieState{}, err
	}
	return s.GetCookies(), nil
}
func (s *Service) DeleteCookieSite(site string) (CookieState, error) {
	if err := s.cookieStore.DeleteSite(site); err != nil {
		return CookieState{}, err
	}
	return s.GetCookies(), nil
}
func (s *Service) SetCookieSiteEnabled(site string, enabled bool) (CookieState, error) {
	if err := s.cookieStore.SetEnabled(site, enabled); err != nil {
		return CookieState{}, err
	}
	return s.GetCookies(), nil
}
