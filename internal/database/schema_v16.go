package database

// Filesystem dates are a disposable cache, rebuilt from the actual local paths.
const fileMetadataDDL = `
CREATE TABLE file_metadata (
 file_id integer PRIMARY KEY REFERENCES media_file(id) ON DELETE CASCADE,
 filepath text NOT NULL,
 birth_ms integer,
 generation integer NOT NULL DEFAULT 0,
 checked_at integer NOT NULL DEFAULT 0
);
CREATE INDEX idx_file_metadata_pending ON file_metadata(file_id) WHERE checked_at=0;
INSERT INTO file_metadata(file_id,filepath) SELECT id,filepath FROM media_file;
CREATE TRIGGER file_metadata_insert AFTER INSERT ON media_file BEGIN
 INSERT INTO file_metadata(file_id,filepath) SELECT new.id,new.filepath WHERE NOT EXISTS(SELECT 1 FROM file_metadata WHERE file_id=new.id);
END;
CREATE TRIGGER file_metadata_update AFTER UPDATE OF filepath,mtime_ns ON media_file WHEN new.filepath IS NOT old.filepath OR new.mtime_ns IS NOT old.mtime_ns BEGIN
 UPDATE file_metadata SET filepath=new.filepath,birth_ms=NULL,checked_at=0,generation=generation+1 WHERE file_id=new.id;
END;
CREATE VIEW gallery_file_membership AS
 SELECT media_id AS album_id,id AS file_id,position FROM media_file
 UNION SELECT album_id,file_id,position FROM user_album_file;
CREATE INDEX idx_media_title_order ON media(coalesce(nullif(custom_title,''),nullif(source_title,''),'제목 없음') COLLATE NOCASE,id);
`
