package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Dev-Wonjoon/StorageBucket/internal/licenses"
)

type Dialogs struct {
	Directory  func(string) (string, error)
	CookieFile func() (string, error)
	Reveal     func(string) error
}
type Service struct {
	mu       sync.Mutex
	filename string
	values   map[string]json.RawMessage
	dialogs  Dialogs
}
type Snapshot struct{ DownloadPath, CookieBrowser, CookieFilePath string }

func New(filename, defaultDownloads string, dialogs Dialogs) (*Service, error) {
	s := &Service{filename: filename, values: map[string]json.RawMessage{}, dialogs: dialogs}
	b, err := os.ReadFile(filename)
	if err == nil {
		if err = json.Unmarshal(b, &s.values); err != nil || s.values == nil {
			return nil, fmt.Errorf("설정 파일 형식이 올바르지 않습니다")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if s.read("downloadPath") == "" {
		s.values["downloadPath"], _ = json.Marshal(defaultDownloads)
	}
	return s, nil
}
func (s *Service) read(key string) string {
	var v string
	_ = json.Unmarshal(s.values[key], &v)
	return v
}

//wails:ignore
func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{s.read("downloadPath"), strings.ToLower(s.read("cookieBrowser")), s.read("cookieFilePath")}
}
func (s *Service) save(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, had := s.values[key]
	s.values[key], _ = json.Marshal(value)
	if err := s.persist(); err != nil {
		if had {
			s.values[key] = old
		} else {
			delete(s.values, key)
		}
		return err
	}
	return nil
}
func (s *Service) persist() error {
	if err := os.MkdirAll(filepath.Dir(s.filename), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.values, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.filename), "config-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, s.filename)
}
func (s *Service) GetDownloadPath() string { return s.Snapshot().DownloadPath }
func (s *Service) SetDownloadPath() (*string, error) {
	if s.dialogs.Directory == nil {
		return nil, fmt.Errorf("폴더 선택 창을 사용할 수 없습니다")
	}
	v, err := s.dialogs.Directory(s.GetDownloadPath())
	if err != nil || v == "" {
		return nil, err
	}
	if !filepath.IsAbs(v) {
		return nil, fmt.Errorf("절대 경로가 필요합니다")
	}
	if err = os.MkdirAll(v, 0700); err != nil {
		return nil, err
	}
	if err = s.save("downloadPath", filepath.Clean(v)); err != nil {
		return nil, err
	}
	return &v, nil
}
func (s *Service) GetCookieBrowser() string { return s.Snapshot().CookieBrowser }
func (s *Service) SetCookieBrowser(browser string) (bool, error) {
	browser = strings.ToLower(browser)
	switch browser {
	case "", "chrome", "firefox", "edge":
	default:
		return false, fmt.Errorf("지원하지 않는 브라우저입니다")
	}
	err := s.save("cookieBrowser", browser)
	return err == nil, err
}
func (s *Service) GetCookieFilePath() string { return s.Snapshot().CookieFilePath }
func (s *Service) SetCookieFilePath() (*string, error) {
	if s.dialogs.CookieFile == nil {
		return nil, fmt.Errorf("파일 선택 창을 사용할 수 없습니다")
	}
	v, err := s.dialogs.CookieFile()
	if err != nil || v == "" {
		return nil, err
	}
	info, err := os.Stat(v)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(v) || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("쿠키 파일이 필요합니다")
	}
	if err = s.save("cookieFilePath", v); err != nil {
		return nil, err
	}
	return &v, nil
}
func (s *Service) ClearCookieFilePath() (bool, error) {
	err := s.save("cookieFilePath", "")
	return err == nil, err
}
func (s *Service) ShowItemInFolder(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("파일 절대 경로가 필요합니다")
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	if s.dialogs.Reveal == nil {
		return fmt.Errorf("파일 탐색기를 사용할 수 없습니다")
	}
	return s.dialogs.Reveal(path)
}

// GetEngineLicenses retains the existing binding name and now includes all notices.
func (s *Service) GetEngineLicenses() []licenses.Entry { return licenses.List() }
