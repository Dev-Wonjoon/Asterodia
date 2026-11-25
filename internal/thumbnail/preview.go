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
	return s.getDownloadPreview(ctx, rawURL, newPreviewHTTPClient)
}

func (s *Service) getDownloadPreview(ctx context.Context, rawURL string, newClient func() *http.Client) (*string, error) {
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
	if ctx.Err() != nil {
		return nil, nil
	}
	// Disk-only reads must never queue behind another URL's web discovery.
	local, hasMedia, err := s.cachedDownloadPreview(ctx, rawURL)
	if err != nil || local != nil || hasMedia || s.preview == nil {
		return local, err
	}
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	if ctx.Err() != nil {
		return nil, nil
	}
	// Another request or the generation worker may have populated the cache.
	local, hasMedia, err = s.cachedDownloadPreview(ctx, rawURL)
	if err != nil || local != nil || hasMedia {
		return local, err
	}
	cache := filepath.Join(filepath.Dir(s.cache), "previews")
	if err = os.MkdirAll(cache, 0700); err != nil {
		return nil, err
	}
	dest := s.downloadPreviewPath(rawURL)
	var owned bool
	if err = s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM download_queue WHERE original_url=?)", rawURL).Scan(&owned); err != nil || !owned {
		return nil, err
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
	if e != nil || !validPreviewURL(target) {
		return nil, nil
	}
	req, e := http.NewRequestWithContext(ctx, "GET", target.String(), nil)
	if e != nil {
		return nil, nil
	}
	client := newClient()
	defer client.CloseIdleConnections()
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

func (s *Service) downloadPreviewPath(rawURL string) string {
	return filepath.Join(filepath.Dir(s.cache), "previews", fmt.Sprintf("%x.jpg", sha256.Sum256([]byte(rawURL))))
}

func (s *Service) cachedAudioArtwork(ctx context.Context, fileID int64) (string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_url FROM media_file WHERE id=? AND source_url IS NOT NULL
        UNION ALL SELECT u.url FROM media_file f JOIN media m ON m.id=f.media_id JOIN download_urls u ON u.id=m.request_url_id WHERE f.id=?
        UNION ALL SELECT q.original_url FROM download_queue q JOIN media_file f ON f.media_id=q.target_media_id WHERE f.id=? LIMIT 32`, fileID, fileID, fileID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var address string
		if err = rows.Scan(&address); err != nil {
			return "", err
		}
		path := s.downloadPreviewPath(address)
		if thumbnailExists(&path) {
			return path, nil
		}
	}
	return "", rows.Err()
}

// CachedDownloadPreview is used when returning download jobs. It only reads
// registered local files and disk cache; it never waits for or invokes the web.
//
//wails:ignore
func (s *Service) CachedDownloadPreview(ctx context.Context, rawURL string) (*string, error) {
	path, _, err := s.cachedDownloadPreview(ctx, rawURL)
	return path, err
}

func (s *Service) cachedDownloadPreview(ctx context.Context, rawURL string) (*string, bool, error) {
	local, hasMedia, err := s.localDownloadPreview(ctx, rawURL)
	if err != nil || local != nil {
		return local, hasMedia, err
	}
	cache := s.downloadPreviewPath(rawURL)
	if thumbnailExists(&cache) {
		return &cache, hasMedia, nil
	}
	return nil, hasMedia, nil
}

// localDownloadPreview uses the same media association and cover ordering as
// the library. It never guesses a file from an obsolete download target path.
func (s *Service) localDownloadPreview(ctx context.Context, rawURL string) (*string, bool, error) {
	var legacy bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM download_queue WHERE original_url=?
		AND (auto_history_unknown=1 OR json_extract(options_json,'$._importedHistory')=1))`, rawURL).Scan(&legacy); err != nil {
		return nil, false, err
	}
	rows, err := s.db.QueryContext(ctx, `WITH requested AS (SELECT request_url_id FROM download_queue WHERE original_url=?)
		SELECT f.media_id,f.id,f.filepath,f.thumbnail_path,f.kind
		FROM media_file f JOIN media m ON m.id=f.media_id
		WHERE f.media_id IN (
			SELECT id FROM media WHERE request_url_id IN (SELECT request_url_id FROM requested)
			UNION SELECT target_media_id FROM download_queue WHERE original_url=?
			UNION SELECT media_id FROM media_file WHERE ? AND legacy_url_id IN (SELECT request_url_id FROM requested))
		ORDER BY CASE WHEN f.id=m.cover_file_id THEN 0 ELSE 1 END,
			CASE WHEN f.thumbnail_path IS NOT NULL OR f.kind='image' THEN 0 ELSE 1 END,f.position,f.id`, rawURL, rawURL, legacy)
	if err != nil {
		return nil, false, err
	}
	var missing []Request
	hasMedia := false
	for rows.Next() {
		var req Request
		var path, kind string
		var thumb *string
		if err = rows.Scan(&req.MediaID, &req.FileID, &path, &thumb, &kind); err != nil {
			rows.Close()
			return nil, false, err
		}
		if thumbnailExists(thumb) {
			rows.Close()
			return thumb, true, nil
		}
		if thumbnailExists(&path) {
			hasMedia = true
			if thumbnailKind(kind, path) != "" {
				missing = append(missing, req)
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	for _, req := range missing {
		status := s.Get(ctx, req)
		if status.OK && status.Data.(Status).State == "missing" {
			s.Retry(ctx, req)
		}
	}
	return nil, hasMedia, nil
}

// ReleaseDownloadPreview only removes an unshared URL cache after history has
// committed its deletion. Library thumbnails and originals are always retained.
//
//wails:ignore
func (s *Service) ReleaseDownloadPreview(ctx context.Context, rawURL string) error {
	s.previewMu.Lock()
	defer s.previewMu.Unlock()
	cache := s.downloadPreviewPath(rawURL)
	var retained bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM download_queue WHERE original_url=?)
		OR EXISTS(SELECT 1 FROM media_file WHERE filepath=? OR thumbnail_path=? OR preview_path=?)`, rawURL, cache, cache, cache).Scan(&retained)
	if err != nil || retained {
		return err
	}
	if err = os.Remove(cache); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
