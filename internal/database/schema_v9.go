package database

import (
	"context"
	"fmt"
	"strings"
)

const searchTablesDDL = `
CREATE TABLE search_state (id integer PRIMARY KEY CHECK(id=1), revision integer NOT NULL DEFAULT 0);
INSERT INTO search_state(id) VALUES(1);
CREATE TABLE search_dirty (media_id integer PRIMARY KEY);
CREATE TABLE search_term (
 id integer PRIMARY KEY,
 kind text NOT NULL CHECK(kind IN ('title','tag','author','platform','text')),
 value text NOT NULL,
 normalized text NOT NULL,
 UNIQUE(kind,value)
);
CREATE INDEX idx_search_term_normalized ON search_term(normalized,kind,value);
CREATE TABLE search_document (
 media_id integer NOT NULL REFERENCES media(id) ON DELETE CASCADE,
 term_id integer NOT NULL REFERENCES search_term(id) ON DELETE CASCADE,
 PRIMARY KEY(media_id,term_id)
) WITHOUT ROWID;
CREATE INDEX idx_search_document_term ON search_document(term_id,media_id);
CREATE TABLE search_gram (
 gram text NOT NULL,
 term_id integer NOT NULL REFERENCES search_term(id) ON DELETE CASCADE,
 PRIMARY KEY(gram,term_id)
) WITHOUT ROWID;
CREATE INDEX idx_search_gram_term ON search_gram(term_id);
CREATE TABLE search_gc (term_id integer PRIMARY KEY);
CREATE VIRTUAL TABLE search_fts USING fts5(normalized,content='search_term',content_rowid='id',tokenize='trigram',detail=none);
CREATE TRIGGER search_term_insert AFTER INSERT ON search_term BEGIN
 INSERT INTO search_fts(rowid,normalized) VALUES(new.id,new.normalized);
END;
CREATE TRIGGER search_term_delete AFTER DELETE ON search_term BEGIN
 INSERT INTO search_fts(search_fts,rowid,normalized) VALUES('delete',old.id,old.normalized);
END;
CREATE TRIGGER search_document_delete AFTER DELETE ON search_document BEGIN
 INSERT INTO search_gc(term_id) SELECT old.term_id WHERE NOT EXISTS(SELECT 1 FROM search_gc WHERE term_id=old.term_id);
END;
CREATE VIEW search_source AS
 SELECT id AS media_id,'title' AS kind,custom_title AS value FROM media WHERE custom_title IS NOT NULL AND custom_title<>''
 UNION SELECT id,'title',source_title FROM media WHERE source_title IS NOT NULL AND source_title<>''
 UNION SELECT media_id,'title',title FROM media_file WHERE title IS NOT NULL AND title<>''
 UNION SELECT mt.media_id,'tag',t.name FROM media_tag mt JOIN tag t ON t.id=mt.tag_id
 UNION SELECT f.media_id,'author',coalesce(p.owner_name,p.owner_id) FROM media_file f JOIN profiles p ON p.id=f.profile_id WHERE coalesce(p.owner_name,p.owner_id)<>''
 UNION SELECT f.media_id,'platform',p.name FROM media_file f JOIN platform p ON p.id=f.platform_id
 UNION SELECT id,'text',coalesce(custom_title,'')||' '||coalesce(source_title,'') FROM media
 UNION SELECT f.media_id,'text',coalesce(f.title,'')||' '||coalesce(p.owner_name,'')||' '||coalesce(pl.name,'') FROM media_file f LEFT JOIN profiles p ON p.id=f.profile_id LEFT JOIN platform pl ON pl.id=f.platform_id;
`

func searchV9DDL() string {
	var ddl strings.Builder
	ddl.WriteString(searchTablesDDL)
	for _, trigger := range []struct{ name, event, table, ids string }{
		{"media_insert", "AFTER INSERT", "media", "SELECT new.id AS media_id"},
		{"media_update", "AFTER UPDATE OF custom_title,source_title,created_at", "media", "SELECT new.id AS media_id"},
		{"media_delete", "AFTER DELETE", "media", "SELECT old.id AS media_id"},
		{"file_insert", "AFTER INSERT", "media_file", "SELECT new.media_id AS media_id"},
		{"file_update", "AFTER UPDATE OF media_id,title,profile_id,platform_id", "media_file", "SELECT old.media_id AS media_id UNION SELECT new.media_id"},
		{"file_delete", "AFTER DELETE", "media_file", "SELECT old.media_id AS media_id"},
		{"tag_link_insert", "AFTER INSERT", "media_tag", "SELECT new.media_id AS media_id"},
		{"tag_link_delete", "AFTER DELETE", "media_tag", "SELECT old.media_id AS media_id"},
		{"tag_update", "AFTER UPDATE OF name", "tag", "SELECT media_id FROM media_tag WHERE tag_id=new.id"},
		{"tag_delete", "BEFORE DELETE", "tag", "SELECT media_id FROM media_tag WHERE tag_id=old.id"},
		{"profile_update", "AFTER UPDATE OF owner_name,owner_id", "profiles", "SELECT media_id FROM media_file WHERE profile_id=new.id"},
		{"profile_delete", "BEFORE DELETE", "profiles", "SELECT media_id FROM media_file WHERE profile_id=old.id"},
		{"platform_update", "AFTER UPDATE OF name", "platform", "SELECT media_id FROM media_file WHERE platform_id=new.id"},
		{"platform_delete", "BEFORE DELETE", "platform", "SELECT media_id FROM media_file WHERE platform_id=old.id"},
	} {
		// Outer OR ABORT/REPLACE statements override trigger conflict policies.
		// Avoid duplicate inserts before they reach conflict handling at all.
		fmt.Fprintf(&ddl, "CREATE TRIGGER search_%s %s ON %s BEGIN INSERT INTO search_dirty(media_id) SELECT DISTINCT candidate.media_id FROM (%s) candidate WHERE NOT EXISTS(SELECT 1 FROM search_dirty WHERE media_id=candidate.media_id); UPDATE search_state SET revision=revision+1 WHERE id=1; END;\n", trigger.name, trigger.event, trigger.table, trigger.ids)
	}
	for _, event := range []string{"INSERT", "DELETE", "UPDATE"} {
		fmt.Fprintf(&ddl, "CREATE TRIGGER search_favorite_%s AFTER %s ON favorite BEGIN UPDATE search_state SET revision=revision+1 WHERE id=1; END;\n", strings.ToLower(event), event)
	}
	return ddl.String()
}

func upgradeSearch(ctx context.Context, q connection) error {
	if _, err := q.ExecContext(ctx, searchV9DDL()); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, "INSERT OR IGNORE INTO search_dirty SELECT id FROM media")
	return err
}
