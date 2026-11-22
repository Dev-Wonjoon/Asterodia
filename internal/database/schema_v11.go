package database

import (
	"context"
	"fmt"
	"strings"
)

// User albums contain references, never copies or ownership of media files.
const albumDDL = `
CREATE TABLE user_album (
 media_id integer PRIMARY KEY REFERENCES media(id) ON DELETE CASCADE,
 cover_file_id integer REFERENCES media_file(id) ON DELETE SET NULL
);
CREATE TABLE user_album_file (
 album_id integer NOT NULL REFERENCES user_album(media_id) ON DELETE CASCADE,
 file_id integer NOT NULL REFERENCES media_file(id) ON DELETE CASCADE,
 position integer NOT NULL CHECK(position>=0),
 PRIMARY KEY(album_id,file_id)
) WITHOUT ROWID;
CREATE INDEX idx_album_file_order ON user_album_file(album_id,position,file_id);
CREATE INDEX idx_album_file_reference ON user_album_file(file_id,album_id);
CREATE TRIGGER v11_album_owner BEFORE INSERT ON user_album
WHEN NOT EXISTS(SELECT 1 FROM media WHERE id=new.media_id AND source_kind='local') OR EXISTS(SELECT 1 FROM media_file WHERE media_id=new.media_id) OR EXISTS(SELECT 1 FROM local_folder WHERE media_id=new.media_id)
BEGIN SELECT RAISE(ABORT,'ALBUM_OWNER_MISMATCH'); END;
CREATE TRIGGER v11_album_file_insert BEFORE INSERT ON media_file
WHEN EXISTS(SELECT 1 FROM user_album WHERE media_id=new.media_id)
BEGIN SELECT RAISE(ABORT,'ALBUM_REFERENCE_ONLY'); END;
CREATE TRIGGER v11_album_file_update BEFORE UPDATE OF media_id ON media_file
WHEN EXISTS(SELECT 1 FROM user_album WHERE media_id=new.media_id)
BEGIN SELECT RAISE(ABORT,'ALBUM_REFERENCE_ONLY'); END;
CREATE TRIGGER v11_album_source BEFORE UPDATE OF source_kind ON media
WHEN new.source_kind<>'local' AND EXISTS(SELECT 1 FROM user_album WHERE media_id=new.id)
BEGIN SELECT RAISE(ABORT,'ALBUM_OWNER_MISMATCH'); END;
CREATE TRIGGER user_album_file_removed AFTER DELETE ON user_album_file BEGIN
 UPDATE user_album SET cover_file_id=NULL WHERE media_id=old.album_id AND cover_file_id=old.file_id;
 UPDATE media SET revision=revision+1 WHERE id=old.album_id;
END;
CREATE TRIGGER user_album_file_changed AFTER UPDATE OF filepath,title,thumbnail_path,preview_path ON media_file BEGIN
 UPDATE media SET revision=revision+1 WHERE id IN (SELECT album_id FROM user_album_file WHERE file_id=new.id);
END;
ALTER TABLE file_move_job ADD COLUMN operation text NOT NULL DEFAULT 'move' CHECK(operation IN ('move','copy'));
CREATE TRIGGER v11_move_operation BEFORE UPDATE OF operation ON file_move_job
WHEN new.operation<>old.operation
BEGIN SELECT RAISE(ABORT,'MOVE_OPERATION_IMMUTABLE'); END;
`

func upgradeAlbums(ctx context.Context, q connection) error {
	_, err := q.ExecContext(ctx, albumDDL+albumSearchDDL())
	return err
}

func albumSearchDDL() string {
	base := searchTablesDDL[strings.Index(searchTablesDDL, "CREATE VIEW search_source AS"):]
	base = strings.TrimSuffix(strings.TrimSpace(base), ";")
	ddl := "DROP VIEW search_source;\n" + base + `
 UNION SELECT a.album_id,'title',f.title FROM user_album_file a JOIN media_file f ON f.id=a.file_id WHERE f.title IS NOT NULL AND f.title<>''
 UNION SELECT a.album_id,'author',coalesce(p.owner_name,p.owner_id) FROM user_album_file a JOIN media_file f ON f.id=a.file_id JOIN profiles p ON p.id=f.profile_id WHERE coalesce(p.owner_name,p.owner_id)<>''
 UNION SELECT a.album_id,'platform',p.name FROM user_album_file a JOIN media_file f ON f.id=a.file_id JOIN platform p ON p.id=f.platform_id
 UNION SELECT a.album_id,'text',coalesce(f.title,'')||' '||coalesce(p.owner_name,'')||' '||coalesce(pl.name,'') FROM user_album_file a JOIN media_file f ON f.id=a.file_id LEFT JOIN profiles p ON p.id=f.profile_id LEFT JOIN platform pl ON pl.id=f.platform_id;
`
	for _, t := range []struct{ name, event, table, ids string }{
		{"insert", "AFTER INSERT", "user_album_file", "SELECT new.album_id AS media_id"},
		{"delete", "AFTER DELETE", "user_album_file", "SELECT old.album_id AS media_id"},
		{"file_update", "AFTER UPDATE OF title,profile_id,platform_id", "media_file", "SELECT album_id AS media_id FROM user_album_file WHERE file_id=new.id"},
		{"file_delete", "BEFORE DELETE", "media_file", "SELECT album_id AS media_id FROM user_album_file WHERE file_id=old.id"},
		{"profile_update", "AFTER UPDATE OF owner_name,owner_id", "profiles", "SELECT a.album_id AS media_id FROM user_album_file a JOIN media_file f ON f.id=a.file_id WHERE f.profile_id=new.id"},
		{"profile_delete", "BEFORE DELETE", "profiles", "SELECT a.album_id AS media_id FROM user_album_file a JOIN media_file f ON f.id=a.file_id WHERE f.profile_id=old.id"},
		{"platform_update", "AFTER UPDATE OF name", "platform", "SELECT a.album_id AS media_id FROM user_album_file a JOIN media_file f ON f.id=a.file_id WHERE f.platform_id=new.id"},
		{"platform_delete", "BEFORE DELETE", "platform", "SELECT a.album_id AS media_id FROM user_album_file a JOIN media_file f ON f.id=a.file_id WHERE f.platform_id=old.id"},
	} {
		ddl += fmt.Sprintf("CREATE TRIGGER search_album_%s %s ON %s BEGIN INSERT INTO search_dirty(media_id) SELECT DISTINCT candidate.media_id FROM (%s) candidate WHERE NOT EXISTS(SELECT 1 FROM search_dirty WHERE media_id=candidate.media_id); UPDATE search_state SET revision=revision+1 WHERE id=1; END;\n", t.name, t.event, t.table, t.ids)
	}
	return ddl
}
