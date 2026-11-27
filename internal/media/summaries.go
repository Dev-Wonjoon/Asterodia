package media

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
)

// Read page metadata and covers together, independent of the number of cards.
func summaries(ctx context.Context, q queryer, ids []int64) ([]Object, error) {
	if len(ids) == 0 {
		return []Object{}, nil
	}
	raw, _ := json.Marshal(ids)
	query := strings.Replace(summarySQL, " FROM media m", `,EXISTS(SELECT 1 FROM gallery_pin WHERE media_id=m.id) AS isPinned,
        (SELECT parent_id FROM album_parent WHERE album_id=m.id) AS parentId,
        (SELECT count(*) FROM album_parent WHERE parent_id=m.id) AS childAlbumCount,
        (SELECT cover_file_id FROM user_album WHERE media_id=m.id) AS albumCover FROM media m`, 1)
	rows, err := objects(ctx, q, query+" WHERE m.id IN (SELECT value FROM json_each(?))", string(raw))
	if err != nil {
		return nil, err
	}
	covers, err := objects(ctx, q, `WITH requested AS (SELECT value AS id FROM json_each(?))
        SELECT m.id AS albumId,f.id AS fileId,coalesce(f.thumbnail_path,f.preview_path,CASE WHEN f.kind='image' THEN f.filepath END) AS path,
          f.kind,f.filepath,f.detected_format AS detectedFormat
        FROM requested r JOIN media m ON m.id=r.id JOIN media_file f ON f.id=(
          SELECT id FROM (
            SELECT candidate.id,
              CASE WHEN candidate.id=coalesce((SELECT cover_file_id FROM user_album WHERE media_id=m.id),m.cover_file_id) THEN 0 ELSE 1 END AS chosen,
              CASE WHEN candidate.thumbnail_path IS NOT NULL OR candidate.kind='image' THEN 0 ELSE 1 END AS ready,
              coalesce((SELECT position FROM user_album_file WHERE album_id=m.id AND file_id=candidate.id),candidate.position) AS albumPosition
            FROM media_file candidate WHERE candidate.media_id=m.id OR candidate.id IN (SELECT file_id FROM user_album_file WHERE album_id=m.id)
          ) ORDER BY chosen,ready,albumPosition,id LIMIT 1
        )`, string(raw))
	if err != nil {
		return nil, err
	}
	byID := map[int64]Object{}
	for _, cover := range covers {
		id := cover["albumId"].(int64)
		delete(cover, "albumId")
		byID[id] = cover
	}
	for _, row := range rows {
		for _, key := range []string{"isFavorite", "isPinned", "isUserAlbum", "isProfileAlbum"} {
			row[key] = row[key] == int64(1)
		}
		if row["isUserAlbum"] == true && row["albumCover"] != nil {
			row["coverMode"] = "manual"
		}
		delete(row, "albumCover")
		row["cover"], row["extension"] = nil, ""
		if cover := byID[row["id"].(int64)]; cover != nil {
			row["extension"] = FileExtension(cover)
			delete(cover, "filepath")
			delete(cover, "detectedFormat")
			if cover["path"] != nil {
				row["cover"] = cover
			}
		}
	}
	return rows, nil
}

type MetadataRequest struct {
	MediaIDs        []int64 `json:"mediaIds"`
	FileIDs         []int64 `json:"fileIds"`
	ChangedMediaIDs []int64 `json:"changedMediaIds"`
}

// Metadata refreshes only already displayed cards after a thumbnail notification.
func (s *Service) Metadata(ctx context.Context, req MetadataRequest) api.Result {
	if len(req.MediaIDs)+len(req.FileIDs) > 400 || len(req.ChangedMediaIDs) > 200 {
		return failure(domainError("INVALID_ARGUMENT"))
	}
	for _, ids := range [][]int64{req.MediaIDs, req.FileIDs, req.ChangedMediaIDs} {
		for _, id := range ids {
			if id <= 0 {
				return failure(domainError("INVALID_ARGUMENT"))
			}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return failure(err)
	}
	defer tx.Rollback()
	if len(req.ChangedMediaIDs) > 0 {
		visible, _ := json.Marshal(req.MediaIDs)
		changed, _ := json.Marshal(req.ChangedMediaIDs)
		affected, e := objects(ctx, tx, `SELECT id FROM media WHERE id IN (SELECT value FROM json_each(?)) AND
          (id IN (SELECT value FROM json_each(?)) OR id IN (SELECT a.album_id FROM user_album_file a JOIN media_file f ON f.id=a.file_id WHERE f.media_id IN (SELECT value FROM json_each(?))))`, string(visible), string(changed), string(changed))
		if e != nil {
			return failure(e)
		}
		req.MediaIDs = []int64{}
		for _, row := range affected {
			req.MediaIDs = append(req.MediaIDs, row["id"].(int64))
		}
	}
	rows, err := summaries(ctx, tx, req.MediaIDs)
	if err != nil {
		return failure(err)
	}
	raw, _ := json.Marshal(req.FileIDs)
	fileQuery := fileSQL + " WHERE id IN (SELECT value FROM json_each(?))"
	fileArgs := []any{string(raw)}
	if len(req.ChangedMediaIDs) > 0 {
		changed, _ := json.Marshal(req.ChangedMediaIDs)
		fileQuery += " AND media_id IN (SELECT value FROM json_each(?))"
		fileArgs = append(fileArgs, string(changed))
	}
	files, err := objects(ctx, tx, fileQuery, fileArgs...)
	if err != nil {
		return failure(err)
	}
	return api.Success(Object{"media": rows, "files": files})
}
