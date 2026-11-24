package mediaedit

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
)

type OutputOptionsRequest struct {
	Keyword string `json:"keyword"`
	Offset  int    `json:"offset"`
}
type OutputAlbum struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}
type OutputOptions struct {
	Directory   string        `json:"directory"`
	Albums      []OutputAlbum `json:"albums"`
	Tags        []string      `json:"tags"`
	HasNextPage bool          `json:"hasNextPage"`
	Offset      int           `json:"offset"`
}

func (s *Service) OutputOptions(ctx context.Context, req OutputOptionsRequest) api.Result {
	if req.Offset < 0 || len(req.Keyword) > 1000 {
		return invalid("출력 옵션 검색 값을 확인하세요")
	}
	result := OutputOptions{Directory: s.output(), Albums: []OutputAlbum{}, Tags: []string{}, Offset: req.Offset}
	rows, err := s.store.db.QueryContext(ctx, `SELECT m.id,coalesce(m.custom_title,m.source_title,'') FROM user_album a JOIN media m ON m.id=a.media_id WHERE instr(lower(coalesce(m.custom_title,m.source_title,'')),lower(?))>0 ORDER BY m.created_at DESC,m.id DESC LIMIT 21 OFFSET ?`, req.Keyword, req.Offset)
	if err != nil {
		return fail(err)
	}
	for rows.Next() {
		var item OutputAlbum
		if err = rows.Scan(&item.ID, &item.Title); err != nil {
			rows.Close()
			return fail(err)
		}
		result.Albums = append(result.Albums, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fail(err)
	}
	if len(result.Albums) > 20 {
		result.HasNextPage = true
		result.Albums = result.Albums[:20]
	}
	rows, err = s.store.db.QueryContext(ctx, "SELECT name FROM tag ORDER BY name LIMIT 100")
	if err != nil {
		return fail(err)
	}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return fail(err)
		}
		result.Tags = append(result.Tags, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fail(err)
	}
	return api.Success(result)
}

func normalizeOutput(req *CreateRequest) error {
	if req.Preview {
		req.OutputDirectory = ""
		req.AlbumIDs = nil
		req.TagNames = nil
		return nil
	}
	if len(req.AlbumIDs) > 100 || len(req.TagNames) > 100 {
		return fmt.Errorf("출력 앨범과 태그는 각각 최대 100개입니다")
	}
	albums := map[int64]bool{}
	tags := map[string]bool{}
	for _, id := range req.AlbumIDs {
		if id <= 0 {
			return fmt.Errorf("출력 앨범을 확인하세요")
		}
		albums[id] = true
	}
	for _, name := range req.TagNames {
		name = strings.TrimSpace(name)
		if name == "" || utf8.RuneCountInString(name) > 100 || strings.ContainsRune(name, 0) {
			return fmt.Errorf("출력 태그를 확인하세요")
		}
		tags[name] = true
	}
	req.AlbumIDs = nil
	for id := range albums {
		req.AlbumIDs = append(req.AlbumIDs, id)
	}
	sort.Slice(req.AlbumIDs, func(i, j int) bool { return req.AlbumIDs[i] < req.AlbumIDs[j] })
	req.TagNames = nil
	for name := range tags {
		req.TagNames = append(req.TagNames, name)
	}
	sort.Strings(req.TagNames)
	if req.OutputDirectory != "" {
		if !filepath.IsAbs(req.OutputDirectory) || strings.ContainsRune(req.OutputDirectory, 0) {
			return fmt.Errorf("PC의 저장 폴더를 확인하세요")
		}
		req.OutputDirectory = filepath.Clean(req.OutputDirectory)
	}
	return nil
}

func (s *Service) validateOutputAlbums(ctx context.Context, ids []int64) error {
	for _, id := range ids {
		var count int
		if err := s.store.db.QueryRowContext(ctx, "SELECT count(*) FROM user_album WHERE media_id=?", id).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("출력 앨범이 삭제되었거나 사용자 앨범이 아닙니다")
		}
	}
	return nil
}

// The rendered file and its logical destinations are committed together. An
// album deleted after submission is skipped so publishing can still recover.
func classifyOutput(ctx context.Context, tx *sql.Tx, req CreateRequest, mediaID, fileID int64) (int, error) {
	skipped := 0
	for _, name := range req.TagNames {
		if _, err := tx.ExecContext(ctx, "INSERT INTO tag(name,updated_at) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET updated_at=excluded.updated_at", name, now()); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO media_tag(media_id,tag_id) SELECT ?,id FROM tag WHERE name=? ON CONFLICT DO NOTHING", mediaID, name); err != nil {
			return 0, err
		}
	}
	for _, id := range req.AlbumIDs {
		result, err := tx.ExecContext(ctx, `INSERT INTO user_album_file(album_id,file_id,position) SELECT a.media_id,?,coalesce((SELECT max(position)+1 FROM user_album_file WHERE album_id=a.media_id),0) FROM user_album a WHERE a.media_id=? ON CONFLICT DO NOTHING`, fileID, id)
		if err != nil {
			return 0, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		if count == 0 {
			skipped++
		}
		if count > 0 {
			if _, err = tx.ExecContext(ctx, "UPDATE media SET revision=revision+1,updated_at=? WHERE id=?", now(), id); err != nil {
				return 0, err
			}
		}
	}
	return skipped, nil
}
