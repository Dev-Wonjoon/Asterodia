package media

import (
	"context"
	"encoding/json"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
)

// Page the weighted album list first, then read only each intersecting file slice.
// Even a very large expanded album never loads all of its file objects into Go.
func listGallery(ctx context.Context, q queryer, req ListRequest, where string, args []any, limit int) api.Result {
	if len(req.ExpandedMediaIDs) > 10000 || req.AnchorMediaID < 0 {
		return failure(domainError("INVALID_ARGUMENT"))
	}
	ids := make([]int64, 0, len(req.ExpandedMediaIDs))
	seen := map[int64]bool{}
	for _, id := range req.ExpandedMediaIDs {
		if id <= 0 {
			return failure(domainError("INVALID_ARGUMENT"))
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	raw, _ := json.Marshal(ids)
	cte := `WITH albums AS (
        SELECT m.id,m.created_at,
          CASE WHEN m.id IN (SELECT value FROM json_each(?)) THEN max(1,((SELECT count(*) FROM media_file WHERE media_id=m.id)+(SELECT count(*) FROM user_album_file WHERE album_id=m.id))) ELSE 1 END AS slots
        FROM media m WHERE ` + where + `
    ), positioned AS (
        SELECT id,slots,coalesce(sum(slots) OVER (ORDER BY created_at DESC,id DESC ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING),0) AS first
        FROM albums
    ) `
	params := append([]any{string(raw)}, args...)
	var total int64
	if err := q.QueryRowContext(ctx, cte+"SELECT coalesce(sum(slots),0) FROM positioned", params...).Scan(&total); err != nil {
		return failure(err)
	}
	offset := req.Offset
	if req.AnchorMediaID > 0 {
		var first *int64
		if err := q.QueryRowContext(ctx, cte+"SELECT min(first) FROM positioned WHERE id=?", append(append([]any{}, params...), req.AnchorMediaID)...).Scan(&first); err != nil {
			return failure(err)
		}
		if first != nil {
			offset = int(*first) / limit * limit
		}
	}
	if int64(offset) >= total {
		offset = max(0, (int(total)-1)/limit*limit)
	}
	slices, err := objects(ctx, q, cte+"SELECT id,slots,first FROM positioned WHERE first < ? AND first+slots > ? ORDER BY first", append(append([]any{}, params...), offset+limit, offset)...)
	if err != nil {
		return failure(err)
	}
	data := []Object{}
	for _, slice := range slices {
		id := slice["id"].(int64)
		album, err := summary(ctx, q, id)
		if err != nil {
			return failure(err)
		}
		if !seen[id] || album["fileCount"].(int64) == 0 {
			data = append(data, album)
			continue
		}
		first, slots := int(slice["first"].(int64)), int(slice["slots"].(int64))
		start, end := max(0, offset-first), min(slots, offset+limit-first)
		files, err := albumFiles(ctx, q, id, end-start, start)
		if err != nil {
			return failure(err)
		}
		for i, file := range files {
			item := Object{}
			for key, value := range album {
				item[key] = value
			}
			item["galleryFile"], item["galleryFileOffset"] = file, start+i
			data = append(data, item)
		}
	}
	return api.Success(Object{"data": data, "total": total, "hasNextPage": int64(offset+len(data)) < total, "offset": offset})
}
