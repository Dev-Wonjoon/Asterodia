package thumbnail

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

//wails:ignore
func (s *Service) ConfigurePreview(discover func(context.Context, string, string) (json.RawMessage, error)) {
	s.preview = discover
}
func (s *Service) GetDownloadPreview(ctx context.Context, rawURL string) (*string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return nil, nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, nil
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	stop := context.AfterFunc(s.ctx, cancel)
	defer cancel()
	defer stop()
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	if ctx.Err() != nil {
		return nil, nil
	}
	cache := filepath.Join(filepath.Dir(s.cache), "previews")
	if err = os.MkdirAll(cache, 0700); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("%x.jpg", sha256.Sum256([]byte(rawURL)))
	dest := filepath.Join(cache, name)
	if info, e := os.Stat(dest); e == nil && info.Size() > 0 && time.Since(info.ModTime()) < 24*time.Hour {
		return &dest, nil
	}
	if s.preview == nil {
		return nil, nil
	}
	work, e := os.MkdirTemp(cache, "discovery-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(work)
	raw, e := s.preview(ctx, rawURL, work)
	if e != nil {
		return nil, nil
	}
	var data struct {
		Thumbnail string            `json:"thumbnail"`
		Entries   []json.RawMessage `json:"entries"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return nil, nil
	}
	if data.Thumbnail == "" && len(data.Entries) > 0 {
		_ = json.Unmarshal(data.Entries[0], &data)
	}
	target, e := url.Parse(data.Thumbnail)
	if e != nil || target.Hostname() == "" || target.User != nil || (target.Scheme != "http" && target.Scheme != "https") {
		return nil, nil
	}
	req, e := http.NewRequestWithContext(ctx, "GET", target.String(), nil)
	if e != nil {
		return nil, nil
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
			return fmt.Errorf("미리보기 리다이렉트 제한")
		}
		return nil
	}}
	res, e := client.Do(req)
	if e != nil {
		return nil, nil
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, nil
	}
	payload, e := io.ReadAll(io.LimitReader(res.Body, 16*1024*1024+1))
	if e != nil || len(payload) > 16*1024*1024 {
		return nil, nil
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(payload))
	if e != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
		return nil, nil
	}
	img, _, e := image.Decode(bytes.NewReader(payload))
	if e != nil {
		return nil, nil
	}
	tmp, e := os.CreateTemp(cache, "preview-*.jpg")
	if e != nil {
		return nil, e
	}
	tempName := tmp.Name()
	defer os.Remove(tempName)
	e = jpeg.Encode(tmp, img, &jpeg.Options{Quality: 80})
	if e == nil {
		e = tmp.Sync()
	}
	closeErr := tmp.Close()
	if e != nil {
		return nil, e
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if e = os.Rename(tempName, dest); e != nil {
		return nil, e
	}
	return &dest, nil
}
