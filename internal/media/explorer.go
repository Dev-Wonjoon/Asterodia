package media

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
)

type ExploreAlbumRequest struct {
	MediaID    int64  `json:"mediaId"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
	Filter     Filter `json:"filter"`
	Recursive  bool   `json:"recursive"`
	Sort       string `json:"sort"`
	Descending bool   `json:"descending"`
}

func albumAncestors(ctx context.Context, q queryer, id int64) ([]Object, error) {
	rows, err := objects(ctx, q, `WITH RECURSIVE chain(id,depth) AS (SELECT ?,0 UNION ALL SELECT p.parent_id,c.depth+1 FROM album_parent p JOIN chain c ON p.album_id=c.id WHERE c.depth<128) SELECT m.id,coalesce(nullif(m.custom_title,''),nullif(m.source_title,''),'제목 없음') AS title FROM chain c JOIN media m ON m.id=c.id ORDER BY c.depth DESC LIMIT 129`, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, sql.ErrNoRows
	}
	if len(rows) > 128 {
		return nil, domainError("INVALID_ARGUMENT")
	}
	return rows, nil
}

func setAlbumParent(ctx context.Context, tx *sql.Tx, id, parent int64) error {
	if id == parent {
		return domainError("INVALID_ARGUMENT")
	}
	if parent == 0 {
		_, err := tx.ExecContext(ctx, "DELETE FROM album_parent WHERE album_id=?", id)
		return err
	}
	chain, err := albumAncestors(ctx, tx, parent)
	if err != nil {
		return err
	}
	for _, ancestor := range chain {
		if ancestor["id"] == id {
			return domainError("INVALID_ARGUMENT")
		}
	}
	// Check the whole moved subtree, so moving a deep tree cannot exceed the
	// navigation depth limit even when the proposed parent itself is shallow.
	var depth int
	err = tx.QueryRowContext(ctx, `WITH RECURSIVE tree(id,depth) AS (SELECT ?,1 UNION ALL SELECT p.album_id,t.depth+1 FROM album_parent p JOIN tree t ON p.parent_id=t.id) SELECT max(depth) FROM tree`, id).Scan(&depth)
	if err != nil {
		return err
	}
	if len(chain)+depth > 128 {
		return domainError("INVALID_ARGUMENT")
	}
	var revision int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM media WHERE id=?", parent).Scan(&revision); err != nil {
		return err
	}
	if err = checkWrite(ctx, tx, parent, revision); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO album_parent(album_id,parent_id) VALUES(?,?) ON CONFLICT(album_id) DO UPDATE SET parent_id=excluded.parent_id", id, parent)
	return err
}

// Explorer searches the file being displayed and the tags of its source and
// containing album. Platform/author constraints apply to that individual file.
func explorerFileFilter(f Filter) (string, []any) {
	where, args := []string{"1=1"}, []any{}
	tagOwner := "mt.media_id IN (m.id,mem.albumId)"
	if f.Keyword != "" {
		where = append(where, `(instr(lower(coalesce(f.title,'')||' '||coalesce(f.relative_path,'')||' '||coalesce(m.custom_title,m.source_title,'')||' '||coalesce(p.owner_name,p.owner_id,'')||' '||coalesce(pl.name,'')),lower(?))>0 OR EXISTS(SELECT 1 FROM media_tag mt JOIN tag t ON t.id=mt.tag_id WHERE `+tagOwner+` AND instr(lower(t.name),lower(?))>0))`)
		args = append(args, f.Keyword, f.Keyword)
	}
	if len(f.Tags) > 0 {
		if f.TagMode == "or" {
			where = append(where, "EXISTS(SELECT 1 FROM media_tag mt JOIN tag t ON t.id=mt.tag_id WHERE "+tagOwner+" AND t.name IN ("+placeholders(len(f.Tags))+"))")
			for _, value := range f.Tags {
				args = append(args, value)
			}
		} else {
			for _, value := range f.Tags {
				where = append(where, "EXISTS(SELECT 1 FROM media_tag mt JOIN tag t ON t.id=mt.tag_id WHERE "+tagOwner+" AND t.name=?)")
				args = append(args, value)
			}
		}
	}
	for _, group := range []struct {
		values []string
		column string
	}{{f.Platforms, "pl.name"}, {f.Authors, "coalesce(p.owner_name,p.owner_id)"}} {
		if len(group.values) > 0 {
			clause := group.column + " IN (" + placeholders(len(group.values)) + ")"
			if group.column == "pl.name" {
				clause = "EXISTS(SELECT 1 FROM platform ep WHERE ep.name IN (" + placeholders(len(group.values)) + ") AND (ep.id=f.platform_id OR EXISTS(SELECT 1 FROM media_platform mp WHERE mp.platform_id=ep.id AND mp.media_id IN (m.id,mem.albumId))))"
			}
			where = append(where, clause)
			for _, value := range group.values {
				args = append(args, value)
			}
		}
	}
	if f.CreatedFrom != nil {
		where = append(where, "f.created_at>=?")
		args = append(args, *f.CreatedFrom)
	}
	if f.CreatedTo != nil {
		where = append(where, "f.created_at<=?")
		args = append(args, *f.CreatedTo)
	}
	return strings.Join(where, " AND "), args
}

func (s *Service) ExploreAlbum(ctx context.Context, req ExploreAlbumRequest) api.Result {
	limit, err := pageBounds(req.Limit, req.Offset)
	order, ok := map[string]string{"": "title COLLATE NOCASE", "name": "title COLLATE NOCASE", "date": "createdAt", "size": "size", "kind": "kind", "duration": "duration"}[req.Sort]
	if err != nil || req.MediaID <= 0 || !ok {
		return failure(domainError("INVALID_ARGUMENT"))
	}
	folderWhere, folderArgs, err := filterSQL(ListRequest{Filter: req.Filter})
	if err != nil {
		return failure(err)
	}
	// Tags on referenced source files are also searchable on a user album.
	folderWhere = strings.ReplaceAll(folderWhere, "mt.media_id=m.id AND", "(mt.media_id=m.id OR mt.media_id IN (SELECT f.media_id FROM user_album_file a JOIN media_file f ON f.id=a.file_id WHERE a.album_id=m.id)) AND")
	visibleWhere, visibleArgs, err := filterSQL(ListRequest{Filter: s.visibleFilter(Filter{ShowHidden: req.Filter.ShowHidden})})
	if err != nil {
		return failure(err)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return failure(err)
	}
	defer tx.Rollback()
	chain, err := albumAncestors(ctx, tx, req.MediaID)
	if err != nil {
		return failure(err)
	}
	for _, ancestor := range chain {
		var visible int
		args := append([]any{ancestor["id"]}, visibleArgs...)
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM media m WHERE m.id=? AND "+visibleWhere, args...).Scan(&visible); err != nil {
			return failure(err)
		}
		if visible == 0 {
			return failure(sql.ErrNoRows)
		}
	}
	album, err := summary(ctx, tx, req.MediaID)
	if err != nil {
		return failure(err)
	}
	fileWhere, fileArgs := explorerFileFilter(req.Filter)
	cte := `WITH RECURSIVE visible(id) AS (SELECT m.id FROM media m WHERE ` + visibleWhere + `),
 containers(id) AS (SELECT ? UNION SELECT ap.album_id FROM album_parent ap JOIN containers c ON c.id=ap.parent_id JOIN visible v ON v.id=ap.album_id WHERE ?),
 members(fileId,albumId) AS (SELECT f.id,c.id FROM containers c JOIN media_file f ON f.media_id=c.id UNION SELECT a.file_id,c.id FROM containers c JOIN user_album_file a ON a.album_id=c.id),
 entries AS (
 SELECT 0 AS ordinal,m.id AS id,m.id AS albumId,coalesce(nullif(m.custom_title,''),nullif(m.source_title,''),'제목 없음') AS title,m.created_at AS createdAt,0 AS size,'album' AS kind,0 AS duration
 FROM album_parent ap JOIN containers c ON c.id=ap.parent_id JOIN visible v ON v.id=ap.album_id JOIN media m ON m.id=ap.album_id WHERE ` + folderWhere + `
 UNION ALL
 SELECT 1,f.id,min(mem.albumId),coalesce(nullif(f.title,''),nullif(f.relative_path,''),f.file_key),f.created_at,coalesce(f.filesize,0),f.kind,coalesce(f.duration,0)
 FROM members mem JOIN media_file f ON f.id=mem.fileId JOIN media m ON m.id=f.media_id LEFT JOIN profiles p ON p.id=f.profile_id LEFT JOIN platform pl ON pl.id=f.platform_id WHERE ` + fileWhere + ` GROUP BY f.id
 ) `
	args := append([]any{}, visibleArgs...)
	args = append(args, req.MediaID, req.Recursive)
	args = append(args, folderArgs...)
	args = append(args, fileArgs...)
	var total int64
	if err = tx.QueryRowContext(ctx, cte+"SELECT count(*) FROM entries", args...).Scan(&total); err != nil {
		return failure(err)
	}
	// A descendant or referenced source can change while the current album's
	// own revision stays unchanged. Hash the relevant revisions for safe paging.
	revisions, err := tx.QueryContext(ctx, cte+`SELECT m.id,m.revision FROM media m WHERE m.id IN (
 SELECT id FROM containers UNION SELECT ap.album_id FROM album_parent ap JOIN containers c ON c.id=ap.parent_id JOIN visible v ON v.id=ap.album_id
 UNION SELECT f.media_id FROM members mem JOIN media_file f ON f.id=mem.fileId
 ) ORDER BY m.id`, args...)
	if err != nil {
		return failure(err)
	}
	hash := sha256.New()
	for revisions.Next() {
		var id, revision int64
		if err = revisions.Scan(&id, &revision); err != nil {
			revisions.Close()
			return failure(err)
		}
		fmt.Fprintf(hash, "%d:%d;", id, revision)
	}
	err = revisions.Err()
	revisions.Close()
	if err != nil {
		return failure(err)
	}
	direction := " ASC"
	if req.Descending {
		direction = " DESC"
	}
	rows, err := objects(ctx, tx, cte+fmt.Sprintf("SELECT ordinal,id,albumId,title FROM entries ORDER BY ordinal,%s%s,id LIMIT ? OFFSET ?", order, direction), append(args, limit, req.Offset)...)
	if err != nil {
		return failure(err)
	}
	entries := []Object{}
	for _, row := range rows {
		if row["ordinal"] == int64(0) {
			child, e := summary(ctx, tx, row["id"].(int64))
			if e != nil {
				return failure(e)
			}
			entries = append(entries, Object{"kind": "album", "album": child})
		} else {
			files, e := objects(ctx, tx, fileSQL+" WHERE id=?", row["id"])
			if e != nil {
				return failure(e)
			}
			entries = append(entries, Object{"kind": "file", "file": files[0], "albumId": row["albumId"], "title": row["title"]})
		}
	}
	return api.Success(Object{"album": album, "breadcrumbs": chain, "data": entries, "total": total, "offset": req.Offset, "hasNextPage": int64(req.Offset+len(entries)) < total, "revisionToken": fmt.Sprintf("%x", hash.Sum(nil))})
}
