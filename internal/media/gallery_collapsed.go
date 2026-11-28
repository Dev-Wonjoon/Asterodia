package media

import (
	"context"
	"github.com/Dev-Wonjoon/Asterodia/internal/api"
)

func listCollapsedGallery(ctx context.Context, q queryer, req ListRequest, where string, args []any, limit int) api.Result {
	if req.GalleryView == "albums" {
		where += " AND EXISTS(SELECT 1 FROM user_album WHERE media_id=m.id)"
	} else if req.ShowAlbumFiles != nil && !*req.ShowAlbumFiles {
		where += ` AND (EXISTS(SELECT 1 FROM user_album WHERE media_id=m.id)
          OR EXISTS(SELECT 1 FROM album_parent WHERE parent_id=m.id)
          OR EXISTS(SELECT 1 FROM gallery_pin WHERE media_id=m.id)
          OR (NOT EXISTS(SELECT 1 FROM media_file WHERE media_id=m.id) AND NOT EXISTS(SELECT 1 FROM user_album_file WHERE album_id=m.id))
          OR EXISTS(SELECT 1 FROM media_file f WHERE f.media_id=m.id AND NOT EXISTS(
            SELECT 1 FROM user_album_file a WHERE a.file_id=f.id AND NOT EXISTS(SELECT 1 FROM instagram_profile_album WHERE media_id=a.album_id))))`
	}
	var total int64
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM media m WHERE "+where, args...).Scan(&total); err != nil {
		return failure(err)
	}
	offset := req.Offset
	if int64(offset) >= total {
		offset = max(0, (int(total)-1)/limit*limit)
	}
	page, err := objects(ctx, q, `SELECT m.id FROM media m LEFT JOIN gallery_pin p ON p.media_id=m.id WHERE `+where+` ORDER BY p.created_at IS NOT NULL DESC,p.created_at DESC,`+collapsedGalleryOrder(req)+` LIMIT ? OFFSET ?`, append(append([]any{}, args...), limit, offset)...)
	if err != nil {
		return failure(err)
	}
	ids := []int64{}
	for _, row := range page {
		ids = append(ids, row["id"].(int64))
	}
	rows, err := summaries(ctx, q, ids)
	if err != nil {
		return failure(err)
	}
	byID := map[int64]Object{}
	for _, row := range rows {
		byID[row["id"].(int64)] = row
	}
	data := []Object{}
	for _, id := range ids {
		row := byID[id]
		row["galleryPinned"] = row["isPinned"]
		data = append(data, row)
	}
	if req.Details {
		if err = enrichGalleryRows(ctx, q, data); err != nil {
			return failure(err)
		}
	}
	return api.Success(Object{"data": data, "total": total, "hasNextPage": int64(offset+len(data)) < total, "offset": offset})
}
