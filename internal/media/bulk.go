package media

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
)

type BulkEditRequest struct {
	Selection  []AlbumSelection `json:"selection"`
	AddTags    []string         `json:"addTags"`
	RemoveTags []string         `json:"removeTags"`
	IsFavorite *bool            `json:"isFavorite"`
}

// BulkEdit commits one bounded batch. Known conflicts are reported per item;
// unexpected SQL errors roll back every write and never report partial success.
func (s *Service) BulkEdit(ctx context.Context, req BulkEditRequest) api.Result {
	leave, allowed := s.UpdateGate.Enter()
	if !allowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leave()
	if len(req.Selection) == 0 || len(req.Selection) > 200 || len(req.AddTags)+len(req.RemoveTags) > 100 || (len(req.AddTags)+len(req.RemoveTags) == 0 && req.IsFavorite == nil) {
		return failure(domainError("INVALID_ARGUMENT"))
	}
	for _, names := range [][]string{req.AddTags, req.RemoveTags} {
		for _, name := range names {
			if strings.TrimSpace(name) == "" || utf8.RuneCountInString(strings.TrimSpace(name)) > 100 {
				return failure(domainError("INVALID_ARGUMENT"))
			}
		}
	}
	seen := map[int64]bool{}
	for _, target := range req.Selection {
		if target.MediaID <= 0 || target.Revision < 0 || len(target.FileIDs) > 0 || seen[target.MediaID] {
			return failure(domainError("INVALID_ARGUMENT"))
		}
		seen[target.MediaID] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return failure(err)
	}
	defer tx.Rollback()
	updated, failed := []Object{}, []Object{}
	ids := []any{}
	for _, target := range req.Selection {
		if err = checkWrite(ctx, tx, target.MediaID, target.Revision); err != nil {
			var known domainError
			if !errors.As(err, &known) && !errors.Is(err, sql.ErrNoRows) {
				return failure(err)
			}
			failed = append(failed, Object{"mediaId": target.MediaID, "error": failure(err).Error})
			continue
		}
		ids = append(ids, target.MediaID)
		updated = append(updated, Object{"mediaId": target.MediaID, "revision": target.Revision + 1})
	}
	now := time.Now().UnixMilli()
	if len(ids) > 0 {
		in := "(" + placeholders(len(ids)) + ")"
		for _, name := range req.RemoveTags {
			if _, err = tx.ExecContext(ctx, "DELETE FROM media_tag WHERE media_id IN "+in+" AND tag_id IN (SELECT id FROM tag WHERE name=?)", append(append([]any{}, ids...), strings.TrimSpace(name))...); err != nil {
				return failure(err)
			}
		}
		for _, name := range req.AddTags {
			name = strings.TrimSpace(name)
			if _, err = tx.ExecContext(ctx, "INSERT INTO tag(name,updated_at) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET updated_at=excluded.updated_at", name, now); err != nil {
				return failure(err)
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO media_tag(media_id,tag_id) SELECT m.id,t.id FROM media m JOIN tag t ON t.name=? WHERE m.id IN "+in+" ON CONFLICT DO NOTHING", append([]any{name}, ids...)...); err != nil {
				return failure(err)
			}
		}
		if req.IsFavorite != nil {
			if *req.IsFavorite {
				_, err = tx.ExecContext(ctx, "INSERT INTO favorite(media_id,created_at) SELECT id,? FROM media WHERE id IN "+in+" ON CONFLICT DO NOTHING", append([]any{now}, ids...)...)
			} else {
				_, err = tx.ExecContext(ctx, "DELETE FROM favorite WHERE media_id IN "+in, ids...)
			}
			if err != nil {
				return failure(err)
			}
		}
		if _, err = tx.ExecContext(ctx, "UPDATE media SET revision=revision+1,updated_at=? WHERE id IN "+in, append([]any{now}, ids...)...); err != nil {
			return failure(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return failure(err)
	}
	return api.Success(Object{"updated": updated, "failed": failed})
}
