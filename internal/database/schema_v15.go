package database

import "strings"

// Manual classifications never overwrite a downloaded file's source platform.
func manualPlatformDDL() string {
	ddl := `
CREATE TABLE media_platform (
 media_id integer NOT NULL REFERENCES media(id) ON DELETE CASCADE,
 platform_id integer NOT NULL REFERENCES platform(id) ON DELETE CASCADE,
 PRIMARY KEY(media_id,platform_id)
) WITHOUT ROWID;
CREATE INDEX idx_media_platform_reverse ON media_platform(platform_id,media_id);
CREATE INDEX idx_file_thumbnail_path ON media_file(thumbnail_path) WHERE thumbnail_path IS NOT NULL;
CREATE INDEX idx_file_preview_path ON media_file(preview_path) WHERE preview_path IS NOT NULL;
CREATE INDEX idx_edit_source_file ON media_edit_session(source_file_id,state);
CREATE INDEX idx_move_media_state ON file_move_item(media_id,state);
CREATE INDEX idx_import_folder_state ON local_import_folder(local_folder_id,state,cleanup_required);
CREATE INDEX idx_download_target_media ON download_queue(target_media_id,state,cleanup_required);
CREATE INDEX idx_media_tag_reverse ON media_tag(tag_id,media_id);
CREATE VIEW media_platform_sources AS
 SELECT media_id,platform_id FROM media_platform
 UNION ALL SELECT media_id,platform_id FROM media_file WHERE platform_id IS NOT NULL
 UNION ALL SELECT a.album_id,f.platform_id FROM user_album_file a JOIN media_file f ON f.id=a.file_id WHERE f.platform_id IS NOT NULL
 UNION ALL SELECT DISTINCT a.album_id,p.platform_id FROM user_album_file a JOIN media_file f ON f.id=a.file_id JOIN media_platform p ON p.media_id=f.media_id;
`
	base := albumSearchDDL()
	start := strings.Index(base, "CREATE VIEW search_source AS")
	end := strings.Index(base[start:], ";") + start
	ddl += "DROP VIEW search_source;\n" + base[start:end] + `
 UNION SELECT mp.media_id,'platform',p.name FROM media_platform mp JOIN platform p ON p.id=mp.platform_id
 UNION SELECT DISTINCT a.album_id,'platform',p.name FROM user_album_file a JOIN media_file f ON f.id=a.file_id JOIN media_platform mp ON mp.media_id=f.media_id JOIN platform p ON p.id=mp.platform_id;
CREATE TRIGGER manual_platform_insert AFTER INSERT ON media_platform BEGIN
 INSERT INTO search_dirty SELECT new.media_id WHERE NOT EXISTS(SELECT 1 FROM search_dirty WHERE media_id=new.media_id);
 INSERT INTO search_dirty SELECT DISTINCT a.album_id FROM user_album_file a JOIN media_file f ON f.id=a.file_id WHERE f.media_id=new.media_id AND NOT EXISTS(SELECT 1 FROM search_dirty WHERE media_id=a.album_id);
 UPDATE search_state SET revision=revision+1 WHERE id=1;
END;
CREATE TRIGGER manual_platform_delete AFTER DELETE ON media_platform BEGIN
 INSERT INTO search_dirty SELECT old.media_id WHERE NOT EXISTS(SELECT 1 FROM search_dirty WHERE media_id=old.media_id);
 INSERT INTO search_dirty SELECT DISTINCT a.album_id FROM user_album_file a JOIN media_file f ON f.id=a.file_id WHERE f.media_id=old.media_id AND NOT EXISTS(SELECT 1 FROM search_dirty WHERE media_id=a.album_id);
 UPDATE search_state SET revision=revision+1 WHERE id=1;
END;
`
	return ddl
}
