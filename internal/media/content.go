package media

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

type Content struct {
	File *os.File
	Info os.FileInfo
	Kind string
}

//wails:ignore
func (s *Service) OpenContent(ctx context.Context, fileID int64, variant string) (*Content, error) {
	if fileID <= 0 {
		return nil, fmt.Errorf("등록된 파일 ID가 필요합니다")
	}
	var original, kind string
	var thumbnail, preview *string
	if err := s.db.QueryRowContext(ctx, "SELECT filepath,kind,thumbnail_path,preview_path FROM media_file WHERE id=?", fileID).Scan(&original, &kind, &thumbnail, &preview); err != nil {
		return nil, err
	}
	path := original
	switch variant {
	case "content":
	case "thumbnail", "preview":
		selected := preview
		if variant == "thumbnail" && thumbnail != nil && *thumbnail != "" {
			selected = thumbnail
		}
		if selected != nil && *selected != "" {
			path, kind = *selected, "image"
		} else if kind != "image" {
			return nil, sql.ErrNoRows
		}
	default:
		return nil, sql.ErrNoRows
	}
	if !filepath.IsAbs(path) {
		return nil, sql.ErrNoRows
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, sql.ErrNoRows
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		file.Close()
		return nil, sql.ErrNoRows
	}
	return &Content{File: file, Info: opened, Kind: kind}, nil
}
