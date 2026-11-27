package thumbnail

import (
	"context"
	"os"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

func thumbnailExists(path *string) bool {
	if path == nil || *path == "" {
		return false
	}
	info, err := os.Stat(*path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

func thumbnailKind(kind, path string) string {
	if kind == "unknown" {
		kind = engine.LocalKind(path)
	}
	if kind == "image" || kind == "video" || kind == "audio" {
		return kind
	}
	return ""
}

// ScheduleMissing coalesces requests. The existing worker reads at most 128
// records at a time and drains that batch before scanning further.
//
//wails:ignore
func (s *Service) ScheduleMissing() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.scanRequested = true
	s.signal()
}

func (s *Service) takeScanRequest() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	requested := s.scanRequested
	s.scanRequested = false
	return requested
}

func (s *Service) backfillBatch(ctx context.Context, after int64) (int64, bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,media_id,kind,filepath,thumbnail_path FROM media_file WHERE id>? AND kind IN ('image','video','audio','unknown') ORDER BY id LIMIT 128`, after)
	if err != nil {
		return after, false, err
	}
	type candidate struct {
		request    Request
		kind, path string
		thumbnail  *string
	}
	batch := make([]candidate, 0, 128)
	for rows.Next() {
		var item candidate
		if err = rows.Scan(&item.request.FileID, &item.request.MediaID, &item.kind, &item.path, &item.thumbnail); err != nil {
			rows.Close()
			return after, false, err
		}
		batch = append(batch, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return after, false, err
	}
	for _, item := range batch {
		after = item.request.FileID
		if thumbnailKind(item.kind, item.path) != "" && !thumbnailExists(item.thumbnail) {
			if err = s.schedule(ctx, item.request); err != nil {
				return after, false, err
			}
		}
	}
	return after, len(batch) == 128, nil
}
