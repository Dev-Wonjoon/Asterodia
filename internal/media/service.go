package media

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
)

type Service struct {
	contentFilters func() ContentFilters
	UpdateGate     *api.UpdateGate
	db             *sql.DB
	searchMu       sync.Mutex
	searchCancel   context.CancelFunc
	searchWG       sync.WaitGroup
}

func NewService(db *sql.DB) *Service { return &Service{db: db} }

type Filter struct {
	ShowHidden      bool `json:"showHidden"`
	hiddenPlatforms []string
	hiddenTags      []string
	Keyword         string   `json:"keyword"`
	Tags            []string `json:"tags"`
	TagMode         string   `json:"tagMode"`
	Platforms       []string `json:"platforms"`
	Authors         []string `json:"authors"`
	CreatedFrom     *int64   `json:"createdFrom"`
	CreatedTo       *int64   `json:"createdTo"`
}
type ListRequest struct {
	Limit            int     `json:"limit"`
	Offset           int     `json:"offset"`
	FavoritesOnly    bool    `json:"favoritesOnly"`
	Filter           Filter  `json:"filter"`
	Gallery          bool    `json:"gallery"`
	AlbumCards       bool    `json:"albumCards"`
	GalleryView      string  `json:"galleryView"`
	ShowAlbumFiles   *bool   `json:"showAlbumFiles"`
	ExpandedMediaIDs []int64 `json:"expandedMediaIds"`
	AnchorMediaID    int64   `json:"anchorMediaId"`
}
type GetRequest struct {
	MediaID    int64 `json:"mediaId"`
	FileLimit  int   `json:"fileLimit"`
	FileOffset int   `json:"fileOffset"`
}
type EditRequest struct {
	MediaID          int64           `json:"mediaId"`
	ExpectedRevision int64           `json:"expectedRevision"`
	CustomTitle      json.RawMessage `json:"customTitle"`
	Cover            *struct {
		Mode   string `json:"mode"`
		FileID int64  `json:"fileId"`
	} `json:"cover"`
	TagIDs    *[]int64 `json:"tagIds"`
	TagChange *struct {
		AddNames  []string `json:"addNames"`
		RemoveIDs []int64  `json:"removeIds"`
	} `json:"tagChange"`
	IsFavorite *bool `json:"isFavorite"`
	IsPinned   *bool `json:"isPinned"`
	PinFileID  int64 `json:"pinFileId"`
}
type UnregisterRequest struct {
	MediaID          int64   `json:"mediaId"`
	ExpectedRevision int64   `json:"expectedRevision"`
	FileIDs          []int64 `json:"fileIds"`
}
type SuggestRequest struct {
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Limit int    `json:"limit"`
}
type Object = map[string]any
type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type domainError string

func (e domainError) Error() string { return string(e) }
func failure(err error) api.Result {
	code := "INTERNAL_ERROR"
	message := "라이브러리 작업을 처리하지 못했습니다."
	var d domainError
	if errors.As(err, &d) {
		code = string(d)
	}
	if errors.Is(err, sql.ErrNoRows) {
		code = "NOT_FOUND"
	}
	switch code {
	case "NOT_FOUND":
		message = "등록된 묶음이나 파일을 찾을 수 없습니다."
	case "REVISION_CONFLICT":
		message = "묶음 정보가 변경되었습니다. 새로고침 후 다시 확인하세요."
	case "INVALID_ARGUMENT":
		message = "요청 값이나 목록 범위를 확인하세요."
	case "INVALID_COVER":
		message = "표지는 같은 묶음에 등록된 파일이어야 합니다."
	case "JOB_ACTIVE":
		message = "관련 작업이 진행 중입니다. 완료 후 다시 시도하세요."
	}
	return api.Failure(code, message)
}
func objects(ctx context.Context, q queryer, stmt string, args ...any) ([]Object, error) {
	rows, err := q.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := []Object{}
	for rows.Next() {
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		obj := Object{}
		for i, col := range columns {
			if b, ok := values[i].([]byte); ok {
				values[i] = string(b)
			}
			obj[col] = values[i]
		}
		result = append(result, obj)
	}
	return result, rows.Err()
}

const summarySQL = `SELECT m.id,m.source_kind AS sourceKind,u.url AS requestUrl,m.source_title AS sourceTitle,m.custom_title AS customTitle,coalesce(nullif(m.custom_title,''),nullif(m.source_title,''),'제목 없음') AS title,m.revision,m.cover_mode AS coverMode,m.created_at AS createdAt,m.updated_at AS updatedAt,((SELECT count(*) FROM media_file WHERE media_id=m.id)+(SELECT count(*) FROM user_album_file WHERE album_id=m.id)) AS fileCount,(SELECT count(*) FROM media_file WHERE media_id=m.id AND storage_kind='app_managed') AS managedFileCount,EXISTS(SELECT 1 FROM user_album a WHERE a.media_id=m.id) AS isUserAlbum,EXISTS(SELECT 1 FROM instagram_profile_album ipa WHERE ipa.media_id=m.id) AS isProfileAlbum,EXISTS(SELECT 1 FROM favorite WHERE media_id=m.id) AS isFavorite FROM media m LEFT JOIN download_urls u ON u.id=m.request_url_id`

func summary(ctx context.Context, q queryer, id int64) (Object, error) {
	rows, err := objects(ctx, q, summarySQL+" WHERE m.id=?", id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, sql.ErrNoRows
	}
	obj := rows[0]
	obj["isFavorite"] = obj["isFavorite"] == int64(1)
	var pinned bool
	if err = q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM gallery_pin WHERE media_id=?)", id).Scan(&pinned); err != nil {
		return nil, err
	}
	obj["isPinned"] = pinned
	obj["isUserAlbum"] = obj["isUserAlbum"] == int64(1)
	obj["isProfileAlbum"] = obj["isProfileAlbum"] == int64(1)
	var parent sql.NullInt64
	var children int64
	if err = q.QueryRowContext(ctx, "SELECT (SELECT parent_id FROM album_parent WHERE album_id=?),(SELECT count(*) FROM album_parent WHERE parent_id=?)", id, id).Scan(&parent, &children); err != nil {
		return nil, err
	}
	obj["parentId"] = nil
	if parent.Valid {
		obj["parentId"] = parent.Int64
	}
	obj["childAlbumCount"] = children
	covers, err := objects(ctx, q, `SELECT f.id AS fileId,coalesce(f.thumbnail_path,f.preview_path,CASE WHEN f.kind='image' THEN f.filepath END) AS path,f.kind,f.filepath,f.detected_format AS detectedFormat FROM media_file f WHERE f.media_id=? OR f.id IN (SELECT file_id FROM user_album_file WHERE album_id=?) ORDER BY CASE WHEN f.id=coalesce((SELECT cover_file_id FROM user_album WHERE media_id=?),(SELECT cover_file_id FROM media WHERE id=?)) THEN 0 ELSE 1 END,CASE WHEN f.thumbnail_path IS NOT NULL OR f.kind='image' THEN 0 ELSE 1 END,coalesce((SELECT position FROM user_album_file WHERE album_id=? AND file_id=f.id),f.position),f.id LIMIT 1`, id, id, id, id, id)
	if obj["isUserAlbum"] == true {
		var cover sql.NullInt64
		if e := q.QueryRowContext(ctx, "SELECT cover_file_id FROM user_album WHERE media_id=?", id).Scan(&cover); e != nil {
			return nil, e
		}
		if cover.Valid {
			obj["coverMode"] = "manual"
		}
	}

	if err != nil {
		return nil, err
	}
	obj["cover"] = nil
	obj["extension"] = ""
	if len(covers) > 0 {
		obj["extension"] = FileExtension(covers[0])
		delete(covers[0], "filepath")
		delete(covers[0], "detectedFormat")
	}
	if len(covers) > 0 && covers[0]["path"] != nil {
		obj["cover"] = covers[0]
	}
	return obj, nil
}

// FileExtension uses the original file, never its JPEG thumbnail or HTTP route.
func FileExtension(file Object) string {
	path, _ := file["filepath"].(string)
	extension := strings.TrimPrefix(filepath.Ext(path), ".")
	if extension == "" {
		format, _ := file["detectedFormat"].(string)
		extension = strings.TrimPrefix(strings.Split(format, ",")[0], ".")
	}
	return strings.ToUpper(extension)
}
func pageBounds(limit, offset int) (int, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 || offset < 0 {
		return 0, domainError("INVALID_ARGUMENT")
	}
	return limit, nil
}
func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }
func filterSQL(req ListRequest) (string, []any, error) {
	// Retain post records for history and re-download, while listing a fully
	// collected post only through its profile album. Removing the album or a
	// membership exposes the original source again.
	where := []string{`NOT EXISTS(SELECT 1 FROM instagram_profile_source ips WHERE ips.source_media_id=m.id AND NOT EXISTS(SELECT 1 FROM media_file f WHERE f.media_id=m.id AND NOT EXISTS(SELECT 1 FROM user_album_file a JOIN media_file collected ON collected.id=a.file_id WHERE a.album_id=ips.album_id AND (a.file_id=f.id OR (f.external_id IS NOT NULL AND f.external_id<>'' AND collected.external_id=f.external_id)))))`}
	args := []any{}
	f := req.Filter
	if len(f.Tags) > 100 || len(f.Platforms) > 100 || len(f.Authors) > 100 || len(f.Keyword) > 2000 || (f.TagMode != "" && f.TagMode != "and" && f.TagMode != "or") || (f.CreatedFrom != nil && f.CreatedTo != nil && *f.CreatedFrom > *f.CreatedTo) {
		return "", nil, domainError("INVALID_ARGUMENT")
	}
	if req.FavoritesOnly {
		where = append(where, "EXISTS(SELECT 1 FROM favorite WHERE media_id=m.id)")
	}
	if f.Keyword != "" {
		where = append(where, `(instr(lower(coalesce(m.custom_title,'')||' '||coalesce(m.source_title,'')),lower(?))>0 OR EXISTS(SELECT 1 FROM media_tag mt JOIN tag t ON t.id=mt.tag_id WHERE mt.media_id=m.id AND instr(lower(t.name),lower(?))>0) OR EXISTS(SELECT 1 FROM media_file f LEFT JOIN profiles p ON p.id=f.profile_id LEFT JOIN platform pl ON pl.id=f.platform_id WHERE (f.media_id=m.id OR f.id IN (SELECT file_id FROM user_album_file WHERE album_id=m.id)) AND instr(lower(coalesce(f.title,'')||' '||coalesce(p.owner_name,'')||' '||coalesce(pl.name,'')),lower(?))>0))`)
		args = append(args, f.Keyword, f.Keyword, f.Keyword)
	}
	if f.CreatedFrom != nil {
		where = append(where, "m.created_at>=?")
		args = append(args, *f.CreatedFrom)
	}
	if f.CreatedTo != nil {
		where = append(where, "m.created_at<=?")
		args = append(args, *f.CreatedTo)
	}
	if len(f.hiddenTags) > 0 {
		where = append(where, "NOT EXISTS(SELECT 1 FROM media_tag mt JOIN tag t ON t.id=mt.tag_id WHERE (mt.media_id IN "+visibilityAncestors+" OR mt.media_id IN (SELECT f.media_id FROM user_album_file a JOIN media_file f ON f.id=a.file_id WHERE a.album_id IN "+visibilityAncestors+")) AND t.name IN ("+placeholders(len(f.hiddenTags))+"))")
		for _, value := range f.hiddenTags {
			args = append(args, value)
		}
	}
	if len(f.hiddenPlatforms) > 0 {
		where = append(where, "NOT EXISTS(SELECT 1 FROM media_file f JOIN platform p ON p.id=f.platform_id WHERE (f.media_id IN "+visibilityAncestors+" OR f.id IN (SELECT file_id FROM user_album_file WHERE album_id IN "+visibilityAncestors+")) AND p.name IN ("+placeholders(len(f.hiddenPlatforms))+"))")
		for _, value := range f.hiddenPlatforms {
			args = append(args, value)
		}
	}
	if len(f.Tags) > 0 {
		if f.TagMode == "or" {
			where = append(where, "EXISTS(SELECT 1 FROM media_tag mt JOIN tag t ON t.id=mt.tag_id WHERE mt.media_id=m.id AND t.name IN ("+placeholders(len(f.Tags))+"))")
			for _, v := range f.Tags {
				args = append(args, v)
			}
		} else {
			for _, v := range f.Tags {
				where = append(where, "EXISTS(SELECT 1 FROM media_tag mt JOIN tag t ON t.id=mt.tag_id WHERE mt.media_id=m.id AND t.name=?)")
				args = append(args, v)
			}
		}
	}
	for _, group := range []struct {
		values []string
		clause string
	}{{f.Platforms, "EXISTS(SELECT 1 FROM media_file f JOIN platform p ON p.id=f.platform_id WHERE (f.media_id=m.id OR f.id IN (SELECT file_id FROM user_album_file WHERE album_id=m.id)) AND p.name IN (%s))"}, {f.Authors, "EXISTS(SELECT 1 FROM media_file f JOIN profiles p ON p.id=f.profile_id WHERE (f.media_id=m.id OR f.id IN (SELECT file_id FROM user_album_file WHERE album_id=m.id)) AND coalesce(p.owner_name,p.owner_id) IN (%s))"}} {
		if len(group.values) > 0 {
			where = append(where, fmt.Sprintf(group.clause, placeholders(len(group.values))))
			for _, v := range group.values {
				args = append(args, v)
			}
		}
	}
	return strings.Join(where, " AND "), args, nil
}
func (s *Service) List(ctx context.Context, req ListRequest) api.Result {
	req.Filter = s.visibleFilter(req.Filter)
	limit, err := pageBounds(req.Limit, req.Offset)
	if err != nil {
		return failure(err)
	}
	where, args, err := filterSQL(req)
	if err != nil {
		return failure(err)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return failure(err)
	}
	defer tx.Rollback()
	var total int64
	if req.Filter.Keyword != "" {
		ready, e := searchReady(ctx, tx)
		if e != nil {
			return failure(e)
		}
		if ready {
			match, values := searchTermMatch(req.Filter.Keyword)
			withoutKeyword := req
			withoutKeyword.Filter.Keyword = ""
			where, args, err = filterSQL(withoutKeyword)
			if err != nil {
				return failure(err)
			}
			// 'text' stores the original concatenated search fields verbatim; tag
			// terms preserve the old literal substring semantics without scanning originals.
			where = "m.id IN (SELECT media_id FROM search_document WHERE term_id IN (SELECT id FROM search_term WHERE kind IN ('text','tag') AND id IN (" + match + ") AND instr(normalized,?)>0)) AND " + where
			args = append(append(values, searchFold(req.Filter.Keyword)), args...)
		}
	}
	if req.Gallery {
		return listGallery(ctx, tx, req, where, args, limit)
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM media m WHERE "+where, args...).Scan(&total); err != nil {
		return failure(err)
	}
	ids, err := objects(ctx, tx, "SELECT m.id FROM media m WHERE "+where+" ORDER BY m.created_at DESC,m.id DESC LIMIT ? OFFSET ?", append(args, limit, req.Offset)...)
	if err != nil {
		return failure(err)
	}
	data := []Object{}
	for _, row := range ids {
		obj, e := summary(ctx, tx, row["id"].(int64))
		if e != nil {
			return failure(e)
		}
		data = append(data, obj)
	}
	return api.Success(Object{"data": data, "total": total, "hasNextPage": int64(req.Offset+len(data)) < total})
}

const fileSQL = `SELECT id,media_id AS mediaId,file_key AS fileKey,file_uuid AS fileUuid,filename_token AS filenameToken,directory_id AS directoryId,storage_kind AS storageKind,source_url AS sourceUrl,title,filepath,relative_path AS relativePath,kind,position,filesize,duration,detected_format AS detectedFormat,width,height,orientation,frame_count AS frameCount,page_count AS pageCount,primary_image_index AS primaryImageIndex,thumbnail_path AS thumbnailPath,thumbnail_origin AS thumbnailOrigin,preview_path AS previewPath,created_at AS createdAt FROM media_file`

func (s *Service) Get(ctx context.Context, req GetRequest) api.Result {
	limit, err := pageBounds(req.FileLimit, req.FileOffset)
	if err != nil || req.MediaID <= 0 {
		return failure(domainError("INVALID_ARGUMENT"))
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return failure(err)
	}
	defer tx.Rollback()
	obj, err := summary(ctx, tx, req.MediaID)
	if err != nil {
		return failure(err)
	}
	files, err := albumFiles(ctx, tx, req.MediaID, limit, req.FileOffset)
	if err != nil {
		return failure(err)
	}
	total := obj["fileCount"].(int64)
	obj["files"] = Object{"data": files, "total": total, "hasNextPage": int64(req.FileOffset+len(files)) < total}
	tags, err := objects(ctx, tx, "SELECT t.id,t.name FROM tag t JOIN media_tag mt ON mt.tag_id=t.id WHERE mt.media_id=? ORDER BY t.name,t.id", req.MediaID)
	if err != nil {
		return failure(err)
	}
	obj["tags"] = tags
	favorites, err := objects(ctx, tx, "SELECT id,created_at AS createdAt FROM favorite WHERE media_id=?", req.MediaID)
	if err != nil {
		return failure(err)
	}
	obj["favorite"] = nil
	if len(favorites) > 0 {
		obj["favorite"] = favorites[0]
	}
	folders, err := objects(ctx, tx, "SELECT id,source_title AS sourceTitle,revision FROM local_folder WHERE media_id=?", req.MediaID)
	if err != nil {
		return failure(err)
	}
	obj["localFolder"] = nil
	if len(folders) > 0 {
		obj["localFolder"] = folders[0]
	}
	locations, err := objects(ctx, tx, "SELECT d.id,d.directory_path AS directoryPath,'active' AS state FROM media_directory d WHERE EXISTS(SELECT 1 FROM media_file f WHERE f.media_id=? AND f.directory_id=d.id) ORDER BY d.id", req.MediaID)
	if err != nil {
		return failure(err)
	}
	if len(locations) == 0 {
		locations, err = objects(ctx, tx, "SELECT id,directory_path AS directoryPath,state FROM local_folder_location WHERE media_id=? ORDER BY id", req.MediaID)
		if err != nil {
			return failure(err)
		}
	}
	obj["locations"] = locations
	return api.Success(obj)
}
func checkWrite(ctx context.Context, tx *sql.Tx, id, expected int64) error {
	if id <= 0 || expected < 0 {
		return domainError("INVALID_ARGUMENT")
	}
	var revision int64
	if err := tx.QueryRowContext(ctx, "SELECT revision FROM media WHERE id=?", id).Scan(&revision); err != nil {
		return err
	}
	if revision != expected {
		return domainError("REVISION_CONFLICT")
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM download_queue WHERE target_media_id=? AND (state IN ('pending','running') OR cleanup_required=1))+(SELECT count(*) FROM local_import_folder f JOIN local_folder l ON l.id=f.local_folder_id JOIN local_import_job j ON j.id=f.job_id WHERE l.media_id=? AND ((j.state='running' AND f.state IN ('pending','running','interrupted')) OR f.cleanup_required=1))`, id, id).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return domainError("JOB_ACTIVE")
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM media_edit_session s JOIN media_file f ON f.id=s.source_file_id WHERE f.media_id=? AND (s.state IN ('pending','preparing','stop_unconfirmed') OR EXISTS(SELECT 1 FROM media_edit_page p WHERE p.session_id=s.id AND p.state IN ('pending','running')) OR EXISTS(SELECT 1 FROM media_edit_job j WHERE j.session_id=s.id AND j.state IN ('pending','running','verifying','publishing','stop_unconfirmed')))`, id).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return domainError("JOB_ACTIVE")
	}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM file_move_item WHERE media_id=? AND state IN ('pending','copying','verified','committed','cleanup_pending')", id).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return domainError("JOB_ACTIVE")
	}
	return nil
}

type DirectoryListRequest struct {
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
	Keyword string `json:"keyword"`
}

func (s *Service) Directories(ctx context.Context, req DirectoryListRequest) api.Result {
	limit, err := pageBounds(req.Limit, req.Offset)
	if err != nil || len(req.Keyword) > 1000 {
		return failure(domainError("INVALID_ARGUMENT"))
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return failure(err)
	}
	defer tx.Rollback()
	var total int64
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM media_directory WHERE registered=1 AND instr(lower(directory_path),lower(?))>0", req.Keyword).Scan(&total); err != nil {
		return failure(err)
	}
	rows, err := objects(ctx, tx, `SELECT d.id,d.directory_path AS directoryPath,d.key_version AS keyVersion,d.import_media_id AS importMediaId,(SELECT count(*) FROM media_file f WHERE f.directory_id=d.id) AS fileCount,(SELECT count(DISTINCT f.media_id) FROM media_file f WHERE f.directory_id=d.id) AS mediaCount FROM media_directory d WHERE d.registered=1 AND instr(lower(d.directory_path),lower(?))>0 ORDER BY d.id LIMIT ? OFFSET ?`, req.Keyword, limit, req.Offset)
	if err != nil {
		return failure(err)
	}
	return api.Success(Object{"data": rows, "total": total, "hasNextPage": int64(req.Offset+len(rows)) < total})
}
func (s *Service) Edit(ctx context.Context, req EditRequest) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	if req.TagIDs != nil && req.TagChange != nil {
		return failure(domainError("INVALID_ARGUMENT"))
	}
	if req.PinFileID < 0 || (req.PinFileID != 0 && req.IsPinned == nil) {
		return failure(domainError("INVALID_ARGUMENT"))
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return failure(err)
	}
	defer tx.Rollback()
	if err = checkWrite(ctx, tx, req.MediaID, req.ExpectedRevision); err != nil {
		return failure(err)
	}
	now := time.Now().UnixMilli()
	if len(req.CustomTitle) > 0 {
		var title *string
		if json.Unmarshal(req.CustomTitle, &title) != nil {
			return failure(domainError("INVALID_ARGUMENT"))
		}
		if title != nil {
			value := strings.TrimSpace(*title)
			if utf8.RuneCountInString(value) > 1000 {
				return failure(domainError("INVALID_ARGUMENT"))
			}
			if value == "" {
				title = nil
			} else {
				title = &value
			}
		}
		if _, err = tx.ExecContext(ctx, "UPDATE media SET custom_title=? WHERE id=?", title, req.MediaID); err != nil {
			return failure(err)
		}
	}
	if req.Cover != nil {
		var fileID any
		if req.Cover.Mode == "manual" {
			var count int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM media_file WHERE "+albumFileWhere+" AND id=?", req.MediaID, req.MediaID, req.Cover.FileID).Scan(&count); err != nil {
				return failure(err)
			}
			if count != 1 {
				return failure(domainError("INVALID_COVER"))
			}
			fileID = req.Cover.FileID
		} else if req.Cover.Mode != "auto" {
			return failure(domainError("INVALID_ARGUMENT"))
		}
		var isAlbum int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM user_album WHERE media_id=?", req.MediaID).Scan(&isAlbum); err != nil {
			return failure(err)
		}
		if isAlbum == 1 {
			_, err = tx.ExecContext(ctx, "UPDATE user_album SET cover_file_id=? WHERE media_id=?", fileID, req.MediaID)
		} else {
			_, err = tx.ExecContext(ctx, "UPDATE media SET cover_mode=?,cover_file_id=? WHERE id=?", req.Cover.Mode, fileID, req.MediaID)
		}
		if err != nil {
			return failure(err)
		}
	}
	if req.TagIDs != nil {
		if len(*req.TagIDs) > 100 {
			return failure(domainError("INVALID_ARGUMENT"))
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM media_tag WHERE media_id=?", req.MediaID); err != nil {
			return failure(err)
		}
		for _, id := range *req.TagIDs {
			var n int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM tag WHERE id=?", id).Scan(&n); err != nil {
				return failure(err)
			}
			if n != 1 {
				return failure(domainError("INVALID_ARGUMENT"))
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO media_tag(media_id,tag_id) VALUES(?,?) ON CONFLICT DO NOTHING", req.MediaID, id); err != nil {
				return failure(err)
			}
		}
	}
	if req.TagChange != nil {
		if len(req.TagChange.AddNames)+len(req.TagChange.RemoveIDs) > 100 {
			return failure(domainError("INVALID_ARGUMENT"))
		}
		for _, id := range req.TagChange.RemoveIDs {
			if _, err = tx.ExecContext(ctx, "DELETE FROM media_tag WHERE media_id=? AND tag_id=?", req.MediaID, id); err != nil {
				return failure(err)
			}
		}
		for _, name := range req.TagChange.AddNames {
			name = strings.TrimSpace(name)
			if name == "" || utf8.RuneCountInString(name) > 100 {
				return failure(domainError("INVALID_ARGUMENT"))
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO tag(name,updated_at) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET updated_at=excluded.updated_at", name, now); err != nil {
				return failure(err)
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO media_tag(media_id,tag_id) SELECT ?,id FROM tag WHERE name=? ON CONFLICT DO NOTHING", req.MediaID, name); err != nil {
				return failure(err)
			}
		}
	}
	if req.IsFavorite != nil {
		if *req.IsFavorite {
			_, err = tx.ExecContext(ctx, "INSERT INTO favorite(media_id,created_at) VALUES(?,?) ON CONFLICT(media_id) DO NOTHING", req.MediaID, now)
		} else {
			_, err = tx.ExecContext(ctx, "DELETE FROM favorite WHERE media_id=?", req.MediaID)
		}
		if err != nil {
			return failure(err)
		}
	}
	if req.IsPinned != nil {
		column, id := "media_id", req.MediaID
		if req.PinFileID != 0 {
			var owner, revision int64
			if err = tx.QueryRowContext(ctx, "SELECT f.media_id,m.revision FROM media_file f JOIN media m ON m.id=f.media_id WHERE f.id=? AND (f.media_id=? OR EXISTS(SELECT 1 FROM user_album_file WHERE album_id=? AND file_id=f.id))", req.PinFileID, req.MediaID, req.MediaID).Scan(&owner, &revision); err != nil {
				return failure(err)
			}
			if err = checkWrite(ctx, tx, owner, revision); err != nil {
				return failure(err)
			}
			column, id = "file_id", req.PinFileID
		}
		if *req.IsPinned {
			_, err = tx.ExecContext(ctx, "INSERT INTO gallery_pin("+column+",created_at) VALUES(?,?) ON CONFLICT("+column+") DO NOTHING", id, now)
		} else {
			_, err = tx.ExecContext(ctx, "DELETE FROM gallery_pin WHERE "+column+"=?", id)
		}
		if err != nil {
			return failure(err)
		}
		if req.PinFileID != 0 {
			// Invalidate every representation, including the source group, so a
			// stale card cannot overwrite a pin toggled through another album.
			if _, err = tx.ExecContext(ctx, `UPDATE media SET revision=revision+1 WHERE id<>? AND (id=(SELECT media_id FROM media_file WHERE id=?) OR id IN (SELECT album_id FROM user_album_file WHERE file_id=?))`, req.MediaID, req.PinFileID, req.PinFileID); err != nil {
				return failure(err)
			}
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE media SET revision=revision+1,updated_at=? WHERE id=?", now, req.MediaID); err != nil {
		return failure(err)
	}
	obj, err := summary(ctx, tx, req.MediaID)
	if err != nil {
		return failure(err)
	}
	if err = tx.Commit(); err != nil {
		return failure(err)
	}
	return api.Success(obj)
}
func (s *Service) UnregisterMedia(ctx context.Context, req UnregisterRequest) api.Result {
	return s.unregister(ctx, req, true)
}
func (s *Service) UnregisterFiles(ctx context.Context, req UnregisterRequest) api.Result {
	return s.unregister(ctx, req, false)
}
func (s *Service) unregister(ctx context.Context, req UnregisterRequest, all bool) api.Result {
	leaveUpdate, updateAllowed := s.UpdateGate.Enter()
	if !updateAllowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leaveUpdate()
	if !all && (len(req.FileIDs) == 0 || len(req.FileIDs) > 200) {
		return failure(domainError("INVALID_ARGUMENT"))
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return failure(err)
	}
	defer tx.Rollback()
	if err = checkWrite(ctx, tx, req.MediaID, req.ExpectedRevision); err != nil {
		return failure(err)
	}
	var isAlbum int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM user_album WHERE media_id=?", req.MediaID).Scan(&isAlbum); err != nil {
		return failure(err)
	}
	if isAlbum == 1 {
		var revision any
		if all {
			_, err = tx.ExecContext(ctx, "DELETE FROM media WHERE id=?", req.MediaID)
		} else {
			for _, file := range req.FileIDs {
				if file <= 0 {
					return failure(domainError("INVALID_ARGUMENT"))
				}
				if _, err = tx.ExecContext(ctx, "DELETE FROM user_album_file WHERE album_id=? AND file_id=?", req.MediaID, file); err != nil {
					return failure(err)
				}
			}
			_, err = tx.ExecContext(ctx, "UPDATE media SET revision=revision+1,updated_at=? WHERE id=?", time.Now().UnixMilli(), req.MediaID)
			if err == nil {
				var r int64
				err = tx.QueryRowContext(ctx, "SELECT revision FROM media WHERE id=?", req.MediaID).Scan(&r)
				revision = r
			}
		}
		if err != nil {
			return failure(err)
		}
		if err = tx.Commit(); err != nil {
			return failure(err)
		}
		return api.Success(Object{"mediaId": req.MediaID, "parentRemoved": all, "revision": revision, "cleanupTaskId": nil})
	}
	clause := "media_id=?"
	args := []any{req.MediaID}
	if !all {
		clause += " AND id IN (" + placeholders(len(req.FileIDs)) + ")"
		seen := map[int64]bool{}
		for _, id := range req.FileIDs {
			if seen[id] || id <= 0 {
				return failure(domainError("INVALID_ARGUMENT"))
			}
			seen[id] = true
			args = append(args, id)
		}
	}
	rows, err := objects(ctx, tx, "SELECT id,filepath,storage_kind AS storageKind FROM media_file WHERE "+clause, args...)
	if err != nil {
		return failure(err)
	}
	if !all && len(rows) != len(req.FileIDs) {
		return failure(sql.ErrNoRows)
	}
	paths := []string{}
	for _, row := range rows {
		if row["storageKind"] == "app_managed" {
			paths = append(paths, row["filepath"].(string))
		}
	}
	var taskID any
	if len(paths) > 0 {
		var id string
		if err = tx.QueryRowContext(ctx, "SELECT lower(hex(randomblob(16)))").Scan(&id); err != nil {
			return failure(err)
		}
		taskID = id
		b, _ := json.Marshal(Object{"paths": paths})
		now := time.Now().UnixMilli()
		if _, err = tx.ExecContext(ctx, `INSERT INTO maintenance_task(id,kind,state,owner_kind,payload_json,created_at,updated_at) VALUES(?,'deletion_cleanup','pending','system',?,?,?)`, id, string(b), now, now); err != nil {
			return failure(err)
		}
	}
	// Clear the composite cover FK before removing its file.
	if _, err = tx.ExecContext(ctx, "UPDATE media SET cover_mode='auto',cover_file_id=NULL WHERE id=? AND cover_file_id IN (SELECT id FROM media_file WHERE "+clause+")", append([]any{req.MediaID}, args...)...); err != nil {
		return failure(err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM media_file WHERE "+clause, args...); err != nil {
		return failure(err)
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM media_file WHERE media_id=?", req.MediaID).Scan(&count); err != nil {
		return failure(err)
	}
	removed := all || count == 0
	var revision any
	if removed {
		// The attempt -> media and media -> attempt composite link is detached first.
		if _, err = tx.ExecContext(ctx, "UPDATE media SET last_download_commit_attempt_id=NULL WHERE id=?", req.MediaID); err != nil {
			return failure(err)
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM media WHERE id=?", req.MediaID); err != nil {
			return failure(err)
		}
	} else {
		revision = req.ExpectedRevision + 1
		if _, err = tx.ExecContext(ctx, "UPDATE media SET revision=revision+1,updated_at=? WHERE id=?", time.Now().UnixMilli(), req.MediaID); err != nil {
			return failure(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return failure(err)
	}
	return api.Success(Object{"mediaId": req.MediaID, "parentRemoved": removed, "revision": revision, "cleanupTaskId": taskID})
}
func (s *Service) Suggest(ctx context.Context, req SuggestRequest) api.Result {
	limit, err := pageBounds(req.Limit, 0)
	if err != nil || len(req.Text) > 1000 {
		return failure(domainError("INVALID_ARGUMENT"))
	}
	stmt := ""
	switch req.Kind {
	case "tag":
		stmt = "SELECT name FROM tag"
	case "platform":
		stmt = "SELECT name FROM platform"
	case "author":
		stmt = "SELECT DISTINCT coalesce(owner_name,owner_id) AS name FROM profiles"
	default:
		return failure(domainError("INVALID_ARGUMENT"))
	}
	rows, err := objects(ctx, s.db, "SELECT name FROM ("+stmt+") WHERE instr(lower(name),lower(?))>0 ORDER BY name LIMIT ?", req.Text, limit)
	if err != nil {
		return failure(err)
	}
	values := []string{}
	for _, row := range rows {
		values = append(values, row["name"].(string))
	}
	return api.Success(values)
}

// CheckWrite applies the catalog revision and active-work guards to file moves.
func CheckWrite(ctx context.Context, tx *sql.Tx, id, expected int64) error {
	return checkWrite(ctx, tx, id, expected)
}
