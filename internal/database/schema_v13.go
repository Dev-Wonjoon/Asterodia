package database

// Hierarchy changes only album references. Deleting a parent promotes its children
// to the gallery root; it never removes their albums or physical files.
const albumHierarchyDDL = `
CREATE TABLE album_parent (
 album_id integer PRIMARY KEY REFERENCES user_album(media_id) ON DELETE CASCADE,
 parent_id integer NOT NULL REFERENCES media(id) ON DELETE CASCADE,
 CHECK(album_id<>parent_id)
);
CREATE INDEX idx_album_parent ON album_parent(parent_id,album_id);
CREATE TRIGGER v13_album_cycle_insert BEFORE INSERT ON album_parent BEGIN
 SELECT RAISE(ABORT,'ALBUM_CYCLE') WHERE EXISTS(
  WITH RECURSIVE ancestors(id) AS (SELECT new.parent_id UNION SELECT p.parent_id FROM album_parent p JOIN ancestors a ON p.album_id=a.id)
  SELECT 1 FROM ancestors WHERE id=new.album_id
 );
END;
CREATE TRIGGER v13_album_cycle_update BEFORE UPDATE ON album_parent BEGIN
 SELECT RAISE(ABORT,'ALBUM_CYCLE') WHERE EXISTS(
  WITH RECURSIVE ancestors(id) AS (SELECT new.parent_id UNION SELECT p.parent_id FROM album_parent p JOIN ancestors a ON p.album_id=a.id)
  SELECT 1 FROM ancestors WHERE id=new.album_id
 );
END;
CREATE TRIGGER v13_album_parent_insert AFTER INSERT ON album_parent BEGIN
 UPDATE media SET revision=revision+1 WHERE id IN (new.album_id,new.parent_id);
END;
CREATE TRIGGER v13_album_parent_update AFTER UPDATE ON album_parent BEGIN
 UPDATE media SET revision=revision+1 WHERE id IN (new.album_id,new.parent_id,old.parent_id);
END;
CREATE TRIGGER v13_album_parent_delete AFTER DELETE ON album_parent BEGIN
 UPDATE media SET revision=revision+1 WHERE id IN (old.album_id,old.parent_id);
END;
`
