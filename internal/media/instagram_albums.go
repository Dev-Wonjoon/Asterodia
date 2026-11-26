package media

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type profileAlbumConnection interface {
	queryer
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// SyncInstagramProfileAlbums uses the caller's transaction. Zero reconciles all
// posts on upgrade/import; downloads pass their own post. Every file must agree
// on an immutable numeric account ID before a post can join a profile album.
func SyncInstagramProfileAlbums(ctx context.Context, q profileAlbumConnection, sourceID int64) error {
	rows, err := objects(ctx, q, `SELECT m.id,min(trim(p.owner_id)) AS owner,
 max(nullif(trim(p.owner_name),'')) AS name,m.created_at AS created
 FROM media m JOIN media_file f ON f.media_id=m.id
 LEFT JOIN profiles p ON p.id=f.profile_id LEFT JOIN platform pl ON pl.id=f.platform_id
 WHERE m.source_kind='download' AND (?=0 OR m.id=?)
 GROUP BY m.id HAVING count(*)=sum(CASE WHEN lower(pl.name)='instagram'
 AND trim(p.owner_id)<>'' AND trim(p.owner_id) NOT GLOB '*[^0-9]*'
 THEN 1 ELSE 0 END) AND count(DISTINCT trim(p.owner_id))=1
 ORDER BY m.created_at,m.id`, sourceID, sourceID)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for _, row := range rows {
		source := row["id"].(int64)
		owner := row["owner"].(string)
		name, _ := row["name"].(string)
		title := "Instagram " + owner
		if name != "" {
			title = "Instagram @" + strings.TrimPrefix(name, "@")
		}
		var album int64
		err = q.QueryRowContext(ctx, "SELECT media_id FROM instagram_profile_album WHERE owner_id=?", owner).Scan(&album)
		created := err == sql.ErrNoRows
		if err != nil && !created {
			return err
		}
		if created {
			result, e := q.ExecContext(ctx, "INSERT INTO media(source_kind,source_title,created_at,updated_at) VALUES('local',?,?,?)", title, row["created"], now)
			if e != nil {
				return e
			}
			album, err = result.LastInsertId()
			if err != nil {
				return err
			}
			if _, err = q.ExecContext(ctx, "INSERT INTO user_album(media_id) VALUES(?)", album); err != nil {
				return err
			}
			if _, err = q.ExecContext(ctx, "INSERT INTO instagram_profile_album(owner_id,media_id) VALUES(?,?)", owner, album); err != nil {
				return err
			}
		}
		var previous sql.NullInt64
		if err = q.QueryRowContext(ctx, "SELECT album_id FROM instagram_profile_source WHERE source_media_id=?", source).Scan(&previous); err != nil && err != sql.ErrNoRows {
			return err
		}
		if previous.Valid && previous.Int64 != album {
			return fmt.Errorf("Instagram 게시물의 계정 식별 정보가 변경되었습니다")
		}
		if _, err = q.ExecContext(ctx, "INSERT INTO instagram_profile_source(source_media_id,album_id) VALUES(?,?) ON CONFLICT DO NOTHING", source, album); err != nil {
			return err
		}
		if _, err = q.ExecContext(ctx, `INSERT INTO user_album_file(album_id,file_id,position)
 SELECT ?,f.id,(SELECT coalesce(max(position),-1) FROM user_album_file WHERE album_id=?)
 +row_number() OVER(ORDER BY f.position,f.id)
 FROM media_file f WHERE f.media_id=? AND NOT EXISTS(
 SELECT 1 FROM user_album_file a JOIN media_file existing ON existing.id=a.file_id
 WHERE a.album_id=? AND (a.file_id=f.id OR (f.external_id IS NOT NULL AND f.external_id<>'' AND existing.external_id=f.external_id)))
 ORDER BY f.position,f.id`, album, album, source, album); err != nil {
			return err
		}
		// Seed metadata once per collected post. Reconciliation/import must not
		// undo a user's later changes to the profile album.
		if !previous.Valid {
			if _, err = q.ExecContext(ctx, `INSERT INTO media_tag(media_id,tag_id)
 SELECT ?,tag_id FROM media_tag WHERE media_id=? ON CONFLICT DO NOTHING`, album, source); err != nil {
				return err
			}
			if _, err = q.ExecContext(ctx, `INSERT INTO favorite(media_id,created_at)
 SELECT ?,created_at FROM favorite WHERE media_id=? ON CONFLICT DO NOTHING`, album, source); err != nil {
				return err
			}
			if _, err = q.ExecContext(ctx, `INSERT INTO gallery_pin(media_id,created_at)
 SELECT ?,created_at FROM gallery_pin WHERE media_id=? ON CONFLICT DO NOTHING`, album, source); err != nil {
				return err
			}
		}
		if created {
			if _, err = q.ExecContext(ctx, `UPDATE user_album SET cover_file_id=
 (SELECT cover_file_id FROM media WHERE id=?) WHERE media_id=?`, source, album); err != nil {
				return err
			}
		}
		if _, err = q.ExecContext(ctx, "UPDATE media SET source_title=?,updated_at=?,revision=revision+1 WHERE id=?", title, now, album); err != nil {
			return err
		}
	}
	return nil
}
