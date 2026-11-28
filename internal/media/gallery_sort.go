package media

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
)

const albumBirthSQL = `(SELECT min(x.birth_ms) FROM file_metadata x WHERE x.file_id IN (SELECT id FROM media_file WHERE media_id=m.id UNION SELECT file_id FROM user_album_file WHERE album_id=m.id))`
const albumPlatformSQL = `(SELECT min(p.name COLLATE NOCASE) FROM media_platform_sources ps JOIN platform p ON p.id=ps.platform_id WHERE ps.media_id=m.id)`
const albumTagSQL = `(SELECT min(t.name COLLATE NOCASE) FROM media_tag mt JOIN tag t ON t.id=mt.tag_id WHERE mt.media_id=m.id)`
const albumTitleSQL = `coalesce(nullif(m.custom_title,''),nullif(m.source_title,''),'제목 없음')`

func validGallerySort(value string) bool {
	switch value {
	case "", "name", "registered", "fileCreated", "platform", "tag":
		return true
	}
	return false
}

func galleryDirection(req ListRequest) string {
	if req.Descending {
		return " DESC"
	}
	return " ASC"
}

func collapsedGalleryOrder(req ListRequest) string {
	if req.Sort == "" {
		return "m.created_at DESC,m.id DESC"
	}
	key := map[string]string{"name": albumTitleSQL + " COLLATE NOCASE", "registered": "m.created_at", "fileCreated": albumBirthSQL, "platform": albumPlatformSQL + " COLLATE NOCASE", "tag": albumTagSQL + " COLLATE NOCASE"}[req.Sort]
	return "(" + key + ") IS NULL," + key + galleryDirection(req) + ",m.id DESC"
}

// Flatten visible file slots inside SQLite before applying sort and pagination.
// The existing fast collapsed/default path remains independent of file counts.
func listSortedGallery(ctx context.Context, q queryer, req ListRequest, cte string, params []any, limit int) api.Result {
	visible := "1=1"
	if req.GalleryView != "albums" {
		visible += " AND NOT EXISTS(SELECT 1 FROM gallery_pin WHERE file_id=f.id)"
	}
	if req.ShowAlbumFiles != nil && !*req.ShowAlbumFiles {
		visible += ` AND (a.userAlbum=1 AND NOT (EXISTS(SELECT 1 FROM instagram_profile_album WHERE media_id=a.id) AND '` + req.GalleryView + `'='files') OR a.pinnedAt IS NOT NULL OR NOT EXISTS(SELECT 1 FROM user_album_file u WHERE u.file_id=f.id AND NOT EXISTS(SELECT 1 FROM instagram_profile_album WHERE media_id=u.album_id)))`
	}
	cte += fmt.Sprintf(`, entries AS (
  SELECT a.id,0 AS fileId,0 AS albumPosition,a.pinnedAt,a.canCollapse,
   %s AS title,m.created_at AS registered,%s AS fileCreated,%s AS platform,%s AS tag
  FROM albums a JOIN media m ON m.id=a.id WHERE a.expanded=0 OR NOT EXISTS(SELECT 1 FROM gallery_file_membership WHERE album_id=a.id)
  UNION ALL
  SELECT a.id,f.id,l.position,a.pinnedAt,a.canCollapse,
   coalesce(nullif(f.title,''),nullif(f.relative_path,''),f.file_key),f.created_at,(SELECT birth_ms FROM file_metadata WHERE file_id=f.id),%s,%s
  FROM albums a JOIN media m ON m.id=a.id JOIN gallery_file_membership l ON l.album_id=a.id JOIN media_file f ON f.id=l.file_id
  WHERE a.expanded=1 AND (a.fileId=f.id OR a.fileId=0 AND (%s))
 ) `, albumTitleSQL, albumBirthSQL, albumPlatformSQL, albumTagSQL, albumPlatformSQL, albumTagSQL, visible)
	key := map[string]string{"name": "title COLLATE NOCASE", "registered": "registered", "fileCreated": "fileCreated", "platform": "platform COLLATE NOCASE", "tag": "tag COLLATE NOCASE"}[req.Sort]
	order := "pinnedAt IS NOT NULL DESC,pinnedAt DESC,(" + key + ") IS NULL," + key + galleryDirection(req) + ",id DESC,fileId DESC"
	var total int64
	if err := q.QueryRowContext(ctx, cte+"SELECT count(*) FROM entries", params...).Scan(&total); err != nil {
		return failure(err)
	}
	offset := req.Offset
	if req.AnchorMediaID > 0 {
		var anchor *int64
		if err := q.QueryRowContext(ctx, cte+", numbered AS (SELECT id,row_number() OVER(ORDER BY "+order+")-1 AS n FROM entries) SELECT min(n) FROM numbered WHERE id=?", append(append([]any{}, params...), req.AnchorMediaID)...).Scan(&anchor); err != nil {
			return failure(err)
		}
		if anchor != nil {
			offset = int(*anchor) / limit * limit
		}
	}
	if int64(offset) >= total {
		offset = max(0, (int(total)-1)/limit*limit)
	}
	page, err := objects(ctx, q, cte+", selected AS MATERIALIZED (SELECT id,fileId,albumPosition,canCollapse,pinnedAt IS NOT NULL AS pinned FROM entries ORDER BY "+order+" LIMIT ? OFFSET ?) SELECT selected.*,(SELECT count(*) FROM gallery_file_membership prior WHERE prior.album_id=selected.id AND (prior.position<selected.albumPosition OR prior.position=selected.albumPosition AND prior.file_id<selected.fileId)) AS originalOffset FROM selected", append(append([]any{}, params...), limit, offset)...)
	if err != nil {
		return failure(err)
	}
	ids, fileIDs := []int64{}, []int64{}
	for _, row := range page {
		ids = append(ids, row["id"].(int64))
		if id := row["fileId"].(int64); id > 0 {
			fileIDs = append(fileIDs, id)
		}
	}
	albums, err := summaries(ctx, q, ids)
	if err != nil {
		return failure(err)
	}
	byID := map[int64]Object{}
	for _, row := range albums {
		byID[row["id"].(int64)] = row
	}
	raw, _ := json.Marshal(fileIDs)
	files, err := objects(ctx, q, fileSQL+" WHERE id IN (SELECT value FROM json_each(?))", string(raw))
	if err != nil {
		return failure(err)
	}
	byFile := map[int64]Object{}
	for _, row := range files {
		byFile[row["id"].(int64)] = row
	}
	data := []Object{}
	for _, row := range page {
		item := Object{}
		for k, v := range byID[row["id"].(int64)] {
			item[k] = v
		}
		item["galleryPinned"] = row["pinned"] == int64(1)
		if fileID := row["fileId"].(int64); fileID > 0 {
			item["galleryFile"] = byFile[fileID]
			item["galleryFileOffset"] = row["originalOffset"]
			item["galleryCanCollapse"] = row["canCollapse"] == int64(1)
			item["isPinned"] = item["galleryPinned"]
		}
		data = append(data, item)
	}
	if req.Details {
		if err = enrichGalleryRows(ctx, q, data); err != nil {
			return failure(err)
		}
	}
	return api.Success(Object{"data": data, "total": total, "hasNextPage": int64(offset+len(data)) < total, "offset": offset})
}

// Two bounded queries enrich the current page; no filesystem reads or per-row RPCs.
func enrichGalleryRows(ctx context.Context, q queryer, data []Object) error {
	if len(data) == 0 {
		return nil
	}
	ids := []int64{}
	for _, row := range data {
		ids = append(ids, row["id"].(int64))
	}
	raw, _ := json.Marshal(ids)
	rows, err := objects(ctx, q, `SELECT m.id,
 (SELECT sum(filesize) FROM media_file WHERE id IN (SELECT id FROM media_file WHERE media_id=m.id UNION SELECT file_id FROM user_album_file WHERE album_id=m.id)) AS byteSize,
 `+albumBirthSQL+` AS fileCreatedAt,
 (SELECT json_group_array(name) FROM (SELECT DISTINCT p.name FROM media_platform_sources ps JOIN platform p ON p.id=ps.platform_id WHERE ps.media_id=m.id ORDER BY p.name COLLATE NOCASE)) AS platformNames,
 (SELECT json_group_array(name) FROM (SELECT t.name FROM media_tag mt JOIN tag t ON t.id=mt.tag_id WHERE mt.media_id=m.id ORDER BY t.name COLLATE NOCASE)) AS tagNames
 FROM media m WHERE m.id IN (SELECT value FROM json_each(?))`, string(raw))
	if err != nil {
		return err
	}
	meta := map[int64]Object{}
	for _, row := range rows {
		for _, key := range []string{"platformNames", "tagNames"} {
			var values []string
			if value, ok := row[key].(string); ok {
				if err = json.Unmarshal([]byte(value), &values); err != nil {
					return err
				}
			}
			if values == nil {
				values = []string{}
			}
			row[key] = values
		}
		meta[row["id"].(int64)] = row
	}
	fileIDs := []int64{}
	for _, row := range data {
		if file, ok := row["galleryFile"].(Object); ok {
			fileIDs = append(fileIDs, file["id"].(int64))
		}
	}
	rawFiles, _ := json.Marshal(fileIDs)
	files, err := objects(ctx, q, `SELECT f.id,f.filesize AS byteSize,x.birth_ms AS fileCreatedAt FROM media_file f LEFT JOIN file_metadata x ON x.file_id=f.id WHERE f.id IN (SELECT value FROM json_each(?))`, string(rawFiles))
	if err != nil {
		return err
	}
	fileMeta := map[int64]Object{}
	for _, row := range files {
		fileMeta[row["id"].(int64)] = row
	}
	for _, row := range data {
		for key, value := range meta[row["id"].(int64)] {
			if key != "id" {
				row[key] = value
			}
		}
		if file, ok := row["galleryFile"].(Object); ok {
			for key, value := range fileMeta[file["id"].(int64)] {
				if key != "id" {
					row[key] = value
				}
			}
		}
	}
	return nil
}
