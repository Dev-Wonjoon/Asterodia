package media

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
)

// Page the weighted album list first, then read only each intersecting file slice.
// Even a very large expanded album never loads all of its file objects into Go.
func listGallery(ctx context.Context, q queryer, req ListRequest, where string, args []any, limit int) api.Result {
	if len(req.ExpandedMediaIDs) > 10000 || req.AnchorMediaID < 0 || (req.GalleryView != "" && req.GalleryView != "all" && req.GalleryView != "albums" && req.GalleryView != "files") {
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
	// Omitted visibility options retain the original API behavior for older clients.
	hideFiles := req.ShowAlbumFiles != nil && !*req.ShowAlbumFiles
	visibleWhere := ""
	if hideFiles {
		visibleWhere = " AND NOT EXISTS(SELECT 1 FROM user_album_file a WHERE a.file_id=f.id)"
	}
	viewWhere, forceFiles := "1=1", "0"
	switch req.GalleryView {
	case "albums":
		viewWhere = "userAlbum=1"
	case "files":
		viewWhere, forceFiles = "userAlbum=0 AND visibleFiles>0", "1"
	}
	if hideFiles && req.GalleryView != "albums" {
		viewWhere += " AND (userAlbum=1 OR totalFiles=0 OR visibleFiles>0)"
	}
	// A partially hidden source group is rendered as its remaining file cards, so
	// its cover and group-level actions cannot expose or select hidden files.
	forced := "userAlbum=0 AND (" + forceFiles + " OR visibleFiles<totalFiles)"
	cte := fmt.Sprintf(`WITH candidates AS (
        SELECT m.id,m.created_at,
          EXISTS(SELECT 1 FROM user_album WHERE media_id=m.id) AS userAlbum,
          m.id IN (SELECT value FROM json_each(?)) AS requestedExpanded,
          ((SELECT count(*) FROM media_file WHERE media_id=m.id)+(SELECT count(*) FROM user_album_file WHERE album_id=m.id)) AS totalFiles,
          (SELECT count(*) FROM media_file f WHERE f.media_id=m.id%s) AS visibleFiles
        FROM media m WHERE %s
    ), albums AS (
        SELECT id,created_at,userAlbum,
          CASE WHEN %s THEN visibleFiles WHEN requestedExpanded THEN max(1,totalFiles) ELSE 1 END AS slots,
          CASE WHEN %s THEN 1 ELSE requestedExpanded END AS expanded,
          CASE WHEN %s THEN 0 ELSE 1 END AS canCollapse
        FROM candidates WHERE %s
    ), positioned AS (
        SELECT id,slots,userAlbum,expanded,canCollapse,coalesce(sum(slots) OVER (ORDER BY created_at DESC,id DESC ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING),0) AS first
        FROM albums
    ) `, visibleWhere, where, forced, forced, forced, viewWhere)
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
	slices, err := objects(ctx, q, cte+"SELECT id,slots,first,userAlbum,expanded,canCollapse FROM positioned WHERE first < ? AND first+slots > ? ORDER BY first", append(append([]any{}, params...), offset+limit, offset)...)
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
		if slice["expanded"] == int64(0) || album["fileCount"].(int64) == 0 {
			data = append(data, album)
			continue
		}
		first, slots := int(slice["first"].(int64)), int(slice["slots"].(int64))
		start, end := max(0, offset-first), min(slots, offset+limit-first)
		var files []Object
		if hideFiles && slice["userAlbum"] == int64(0) {
			// Count the original position only for this bounded visible page. Detail
			// panes still use the complete source order, including hidden files.
			files, err = objects(ctx, q, `SELECT visible.*,
                (SELECT count(*) FROM media_file prior WHERE prior.media_id=visible.mediaId AND (prior.position<visible.position OR (prior.position=visible.position AND prior.id<visible.id))) AS originalOffset
                FROM (`+fileSQL+` WHERE media_id=? AND NOT EXISTS(SELECT 1 FROM user_album_file a WHERE a.file_id=media_file.id) ORDER BY position,id LIMIT ? OFFSET ?) visible`, id, end-start, start)
		} else {
			files, err = albumFiles(ctx, q, id, end-start, start)
		}
		if err != nil {
			return failure(err)
		}
		for i, file := range files {
			item := Object{}
			for key, value := range album {
				item[key] = value
			}
			fileOffset := start + i
			if original, ok := file["originalOffset"].(int64); ok {
				fileOffset = int(original)
				delete(file, "originalOffset")
			}
			item["galleryFile"], item["galleryFileOffset"] = file, fileOffset
			if slice["canCollapse"] == int64(0) {
				item["galleryCanCollapse"] = false
			}
			data = append(data, item)
		}
	}
	return api.Success(Object{"data": data, "total": total, "hasNextPage": int64(offset+len(data)) < total, "offset": offset})
}
