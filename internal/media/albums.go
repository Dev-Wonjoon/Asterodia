package media

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
)

const albumFileWhere = `(media_id=? OR id IN (SELECT file_id FROM user_album_file WHERE album_id=?))`
const albumFileOrder = ` ORDER BY coalesce((SELECT position FROM user_album_file a WHERE a.album_id=? AND a.file_id=media_file.id),position),id`

func albumFiles(ctx context.Context, q queryer, id int64, limit, offset int) ([]Object, error) {
	return objects(ctx, q, fileSQL+" WHERE "+albumFileWhere+albumFileOrder+" LIMIT ? OFFSET ?", id, id, id, limit, offset)
}

type AlbumRequest struct {
	AlbumID          int64   `json:"albumId"`
	ExpectedRevision int64   `json:"expectedRevision"`
	Title            *string `json:"title"`
	AddFileIDs       []int64 `json:"addFileIds"`
	AddMediaIDs      []int64 `json:"addMediaIds"`
	RemoveFileIDs    []int64 `json:"removeFileIds"`
	MoveFileID       int64   `json:"moveFileId"`
	BeforeFileID     int64   `json:"beforeFileId"`
	Delete           bool    `json:"delete"`
}

// Albums is a paged chooser; the main gallery still mixes every album in one feed.
func (s *Service) Albums(ctx context.Context, req DirectoryListRequest) api.Result {
	limit, err := pageBounds(req.Limit, req.Offset)
	if err != nil || len(req.Keyword) > 1000 {
		return failure(domainError("INVALID_ARGUMENT"))
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return failure(err)
	}
	defer tx.Rollback()
	where := ` WHERE EXISTS(SELECT 1 FROM user_album a WHERE a.media_id=m.id) AND instr(lower(coalesce(m.custom_title,m.source_title,'')),lower(?))>0`
	var total int64
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM media m"+where, req.Keyword).Scan(&total); err != nil {
		return failure(err)
	}
	ids, err := objects(ctx, tx, "SELECT m.id FROM media m"+where+" ORDER BY m.created_at DESC,m.id DESC LIMIT ? OFFSET ?", req.Keyword, limit, req.Offset)
	if err != nil {
		return failure(err)
	}
	rows := []Object{}
	for _, id := range ids {
		row, e := summary(ctx, tx, id["id"].(int64))
		if e != nil {
			return failure(e)
		}
		rows = append(rows, row)
	}
	return api.Success(Object{"data": rows, "total": total, "hasNextPage": int64(req.Offset+len(rows)) < total})
}

func (s *Service) SaveAlbum(ctx context.Context, req AlbumRequest) api.Result {
	leave, allowed := s.UpdateGate.Enter()
	if !allowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leave()
	if req.AlbumID < 0 || req.ExpectedRevision < 0 || len(req.AddFileIDs) > 20000 || len(req.AddMediaIDs) > 500 || len(req.RemoveFileIDs) > 20000 || req.MoveFileID < 0 || req.BeforeFileID < 0 {
		return failure(domainError("INVALID_ARGUMENT"))
	}
	var title string
	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
		if title == "" || utf8.RuneCountInString(title) > 1000 {
			return failure(domainError("INVALID_ARGUMENT"))
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return failure(err)
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	id := req.AlbumID
	if id == 0 {
		if req.Title == nil || req.Delete {
			return failure(domainError("INVALID_ARGUMENT"))
		}
		result, e := tx.ExecContext(ctx, "INSERT INTO media(source_kind,custom_title,created_at) VALUES('local',?,?)", title, now)
		if e != nil {
			return failure(e)
		}
		id, err = result.LastInsertId()
		if err != nil {
			return failure(err)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO user_album(media_id) VALUES(?)", id); err != nil {
			return failure(err)
		}
	} else {
		var exists int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM user_album WHERE media_id=?", id).Scan(&exists); err != nil {
			return failure(err)
		}
		if exists != 1 {
			return failure(sql.ErrNoRows)
		}
		if err = checkWrite(ctx, tx, id, req.ExpectedRevision); err != nil {
			return failure(err)
		}
	}
	if req.Delete {
		deleted, e := summary(ctx, tx, id)
		if e != nil {
			return failure(e)
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM media WHERE id=?", id); err != nil {
			return failure(err)
		}
		if err = tx.Commit(); err != nil {
			return failure(err)
		}
		return api.Success(deleted)
	}
	if req.Title != nil {
		if _, err = tx.ExecContext(ctx, "UPDATE media SET custom_title=? WHERE id=?", title, id); err != nil {
			return failure(err)
		}
	}
	add := append([]int64{}, req.AddFileIDs...)
	for _, parent := range req.AddMediaIDs {
		if parent <= 0 || parent == id {
			return failure(domainError("INVALID_ARGUMENT"))
		}
		var exists int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM media WHERE id=?", parent).Scan(&exists); err != nil {
			return failure(err)
		}
		if exists == 0 {
			return failure(sql.ErrNoRows)
		}
		rows, e := objects(ctx, tx, "SELECT id FROM media_file WHERE "+albumFileWhere+albumFileOrder+" LIMIT 20001", parent, parent, parent)
		if e != nil {
			return failure(e)
		}
		for _, row := range rows {
			add = append(add, row["id"].(int64))
		}
		if len(add) > 20000 {
			return failure(domainError("INVALID_ARGUMENT"))
		}
	}
	var position int64
	if err = tx.QueryRowContext(ctx, "SELECT coalesce(max(position),-1)+1 FROM user_album_file WHERE album_id=?", id).Scan(&position); err != nil {
		return failure(err)
	}
	for _, file := range add {
		if file <= 0 {
			return failure(domainError("INVALID_ARGUMENT"))
		}
		var exists int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM media_file WHERE id=?", file).Scan(&exists); err != nil {
			return failure(err)
		}
		if exists != 1 {
			return failure(sql.ErrNoRows)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO user_album_file(album_id,file_id,position) VALUES(?,?,?) ON CONFLICT DO NOTHING", id, file, position); err != nil {
			return failure(err)
		}
		position++
	}
	for _, file := range req.RemoveFileIDs {
		if file <= 0 {
			return failure(domainError("INVALID_ARGUMENT"))
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM user_album_file WHERE album_id=? AND file_id=?", id, file); err != nil {
			return failure(err)
		}
	}
	if req.MoveFileID > 0 {
		rows, e := objects(ctx, tx, "SELECT file_id FROM user_album_file WHERE album_id=? ORDER BY position,file_id", id)
		if e != nil {
			return failure(e)
		}
		order := []int64{}
		found, before := false, req.BeforeFileID == 0
		for _, row := range rows {
			file := row["file_id"].(int64)
			if file == req.MoveFileID {
				found = true
				continue
			}
			if file == req.BeforeFileID {
				order = append(order, req.MoveFileID)
				before = true
			}
			order = append(order, file)
		}
		if !found || !before || req.MoveFileID == req.BeforeFileID {
			return failure(domainError("INVALID_ARGUMENT"))
		}
		if req.BeforeFileID == 0 {
			order = append(order, req.MoveFileID)
		}
		for i, file := range order {
			if _, err = tx.ExecContext(ctx, "UPDATE user_album_file SET position=? WHERE album_id=? AND file_id=?", i, id, file); err != nil {
				return failure(err)
			}
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE media SET revision=revision+1,updated_at=? WHERE id=?", now, id); err != nil {
		return failure(err)
	}
	obj, err := summary(ctx, tx, id)
	if err != nil {
		return failure(err)
	}
	if err = tx.Commit(); err != nil {
		return failure(err)
	}
	return api.Success(obj)
}

type SequenceRequest struct {
	MediaID int64 `json:"mediaId"`
}
type FileRequest struct {
	FileID int64 `json:"fileId"`
}

// Only IDs are loaded for shuffling; full file records are read on demand.
func (s *Service) Sequence(ctx context.Context, req SequenceRequest) api.Result {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return failure(err)
	}
	defer tx.Rollback()
	row, err := summary(ctx, tx, req.MediaID)
	if err != nil {
		return failure(err)
	}
	rows, err := objects(ctx, tx, "SELECT id FROM media_file WHERE "+albumFileWhere+albumFileOrder, req.MediaID, req.MediaID, req.MediaID)
	if err != nil {
		return failure(err)
	}
	ids := []int64{}
	for _, file := range rows {
		ids = append(ids, file["id"].(int64))
	}
	return api.Success(Object{"ids": ids, "title": row["title"], "revision": row["revision"]})
}

func (s *Service) ReadFile(ctx context.Context, req FileRequest) api.Result {
	if req.FileID <= 0 {
		return failure(domainError("INVALID_ARGUMENT"))
	}
	rows, err := objects(ctx, s.db, fileSQL+" WHERE id=?", req.FileID)
	if err != nil {
		return failure(err)
	}
	if len(rows) == 0 {
		return failure(sql.ErrNoRows)
	}
	return api.Success(rows[0])
}
