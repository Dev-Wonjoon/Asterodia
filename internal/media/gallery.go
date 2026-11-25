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
	// Search and favorites can still find nested albums directly. The ordinary
	// gallery displays roots, with children reached through the album explorer.
	if !req.FavoritesOnly && req.Filter.Keyword == "" && len(req.Filter.Tags) == 0 && len(req.Filter.Platforms) == 0 && len(req.Filter.Authors) == 0 && req.Filter.CreatedFrom == nil && req.Filter.CreatedTo == nil {
		where += " AND NOT EXISTS(SELECT 1 FROM album_parent WHERE album_id=m.id)"
	}
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
	// File pins occupy one global slot, even if multiple expanded albums refer
	// to that file. Album-only views keep their original album contents.
	pinFiles := req.GalleryView != "albums"
	unpinnedWhere, pinEnabled := "", "0"
	if pinFiles {
		unpinnedWhere = " AND NOT EXISTS(SELECT 1 FROM gallery_pin p WHERE p.file_id=f.id)"
		pinEnabled = "1"
	}
	visibleWhere := ""
	if hideFiles {
		visibleWhere = " AND (EXISTS(SELECT 1 FROM gallery_pin WHERE media_id=m.id) OR NOT EXISTS(SELECT 1 FROM user_album_file a WHERE a.file_id=f.id))"
	}
	viewWhere, forceFiles := "1=1", "0"
	switch req.GalleryView {
	case "albums":
		viewWhere = "userAlbum=1"
	case "files":
		viewWhere, forceFiles = "userAlbum=0 AND visibleFiles>0", "1"
	}
	if pinFiles {
		viewWhere += " AND (userAlbum=1 OR hasChildren=1 OR totalFiles=0 OR visibleFiles>0 OR pinnedAt IS NOT NULL)"
	}
	// Legacy inline views render a partially collected source as its remaining
	// file cards. Explorer entry views keep the source grouped until the user
	// explicitly expands it or selects the file-only view.
	automaticExpansion := "hasChildren=0 AND visibleFiles<totalFiles AND pinnedAt IS NULL"
	if req.AlbumCards {
		automaticExpansion = "0"
	}
	forced := "userAlbum=0 AND visibleFiles>0 AND (" + forceFiles + " OR (" + automaticExpansion + "))"
	cte := fmt.Sprintf(`WITH candidates AS (
        SELECT m.id,m.created_at,(SELECT created_at FROM gallery_pin WHERE media_id=m.id) AS pinnedAt,
          EXISTS(SELECT 1 FROM user_album WHERE media_id=m.id) AS userAlbum,
          EXISTS(SELECT 1 FROM album_parent WHERE parent_id=m.id) AS hasChildren,
          m.id IN (SELECT value FROM json_each(?)) AS requestedExpanded,
          ((SELECT count(*) FROM media_file WHERE media_id=m.id)+(SELECT count(*) FROM user_album_file WHERE album_id=m.id)) AS totalFiles,
          (SELECT count(*) FROM media_file f WHERE f.media_id=m.id%s%s) AS visibleFiles,
          ((SELECT count(*) FROM media_file f WHERE f.media_id=m.id%s)+
           (SELECT count(*) FROM user_album_file a JOIN media_file f ON f.id=a.file_id WHERE a.album_id=m.id%s)) AS remainingFiles
        FROM media m WHERE %s
    ), pinnedFiles AS (
        SELECT f.id AS fileId,p.created_at AS pinnedAt,
          coalesce((SELECT id FROM candidates WHERE id=f.media_id),
            (SELECT min(c.id) FROM candidates c JOIN user_album_file a ON a.album_id=c.id WHERE a.file_id=f.id)) AS id
        FROM gallery_pin p JOIN media_file f ON f.id=p.file_id WHERE %s
    ), albums AS (
        SELECT id,created_at,userAlbum,pinnedAt,0 AS fileId,
          CASE WHEN %s THEN visibleFiles WHEN requestedExpanded THEN max(1,remainingFiles) ELSE 1 END AS slots,
          CASE WHEN remainingFiles=0 THEN 0 WHEN %s THEN 1 ELSE requestedExpanded END AS expanded,
          CASE WHEN %s THEN 0 ELSE 1 END AS canCollapse
        FROM candidates WHERE %s
        UNION ALL
        SELECT c.id,c.created_at,c.userAlbum,p.pinnedAt,p.fileId,1,1,0
        FROM pinnedFiles p JOIN candidates c ON c.id=p.id
    ), positioned AS (
        SELECT id,fileId,slots,userAlbum,expanded,canCollapse,pinnedAt IS NOT NULL AS pinned,coalesce(sum(slots) OVER (ORDER BY pinnedAt IS NOT NULL DESC,pinnedAt DESC,created_at DESC,id DESC,fileId DESC ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING),0) AS first
        FROM albums
    ) `, visibleWhere, unpinnedWhere, unpinnedWhere, unpinnedWhere, where, pinEnabled, forced, forced, forced, viewWhere)
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
	slices, err := objects(ctx, q, cte+"SELECT id,fileId,slots,first,userAlbum,expanded,canCollapse,pinned FROM positioned WHERE first < ? AND first+slots > ? ORDER BY first", append(append([]any{}, params...), offset+limit, offset)...)
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
		album["galleryPinned"] = slice["pinned"] == int64(1)
		if slice["expanded"] == int64(0) || album["fileCount"].(int64) == 0 {
			if pinFiles && album["fileCount"].(int64) > 0 && slice["expanded"] == int64(0) && seen[id] {
				album["galleryCanCollapse"] = false
			}
			data = append(data, album)
			continue
		}
		first, slots := int(slice["first"].(int64)), int(slice["slots"].(int64))
		start, end := max(0, offset-first), min(slots, offset+limit-first)
		var files []Object
		files, err = galleryFiles(ctx, q, id, end-start, start, hideFiles && slice["userAlbum"] == int64(0) && album["isPinned"] != true, pinFiles, slice["fileId"].(int64))
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
			var pinned bool
			if err = q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM gallery_pin WHERE file_id=?)", file["id"]).Scan(&pinned); err != nil {
				return failure(err)
			}
			item["isPinned"] = pinned
			if slice["canCollapse"] == int64(0) {
				item["galleryCanCollapse"] = false
			}
			data = append(data, item)
		}
	}
	return api.Success(Object{"data": data, "total": total, "hasNextPage": int64(offset+len(data)) < total, "offset": offset})
}

// Read only the requested slice. Count each returned file's original offset
// without loading every file into Go, preserving viewer/detail navigation.
func galleryFiles(ctx context.Context, q queryer, id int64, limit, offset int, hideFiles, excludePins bool, pinnedFileID int64) ([]Object, error) {
	if pinnedFileID == 0 && !hideFiles {
		var hasPins bool
		if excludePins {
			if err := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM gallery_pin WHERE file_id IN (SELECT id FROM media_file WHERE media_id=? UNION ALL SELECT file_id FROM user_album_file WHERE album_id=?))", id, id).Scan(&hasPins); err != nil {
				return nil, err
			}
		}
		if !hasPins {
			return albumFiles(ctx, q, id, limit, offset)
		}
	}
	where := albumFileWhere
	args := []any{id, id}
	if pinnedFileID != 0 {
		where += " AND id=?"
		args = append(args, pinnedFileID)
	} else {
		if hideFiles {
			where += " AND NOT EXISTS(SELECT 1 FROM user_album_file a WHERE a.file_id=media_file.id)"
		}
		if excludePins {
			where += " AND NOT EXISTS(SELECT 1 FROM gallery_pin p WHERE p.file_id=media_file.id)"
		}
	}
	args = append(args, id, limit, offset)
	query := `SELECT visible.*,
      (SELECT count(*) FROM (
         SELECT id,position FROM media_file WHERE media_id=?
         UNION ALL SELECT file_id,position FROM user_album_file WHERE album_id=?
       ) prior WHERE prior.position<visible.albumPosition OR (prior.position=visible.albumPosition AND prior.id<visible.id)) AS originalOffset
      FROM (SELECT page.*,coalesce((SELECT position FROM user_album_file WHERE album_id=? AND file_id=page.id),page.position) AS albumPosition
        FROM (` + fileSQL + " WHERE " + where + albumFileOrder + ` LIMIT ? OFFSET ?) page) visible`
	files, err := objects(ctx, q, query, append([]any{id, id, id}, args...)...)
	for _, file := range files {
		delete(file, "albumPosition")
	}
	return files, err
}
