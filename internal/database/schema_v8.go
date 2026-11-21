package database

import (
	"context"
	"strings"
)

const editV8DDL = `
CREATE TABLE media_edit_page (
 session_id text NOT NULL REFERENCES media_edit_session(id) ON DELETE CASCADE,
 page_offset integer NOT NULL CHECK(page_offset>=0),
 state text NOT NULL CHECK(state IN ('pending','running','ready','failed')),
 error_message text NOT NULL DEFAULT '',
 accessed_at integer NOT NULL,
 PRIMARY KEY(session_id,page_offset)
);
CREATE TABLE media_edit_selection (
 id text PRIMARY KEY NOT NULL,
 session_id text NOT NULL REFERENCES media_edit_session(id) ON DELETE CASCADE,
 item_count integer NOT NULL DEFAULT 0 CHECK(item_count>=0),
 finished integer NOT NULL DEFAULT 0 CHECK(finished IN (0,1)),
 created_at integer NOT NULL
);
CREATE TABLE media_edit_range (
 selection_id text NOT NULL REFERENCES media_edit_selection(id) ON DELETE CASCADE,
 position integer NOT NULL CHECK(position>=0),
 first_frame integer NOT NULL CHECK(first_frame>=0),
 last_frame integer NOT NULL CHECK(last_frame>=first_frame),
 PRIMARY KEY(selection_id,position)
);
`

func schemaV8() string {
	s := strings.Replace(schemaV7SQL, "frame_count BETWEEN 0 AND 900", "frame_count>=0", 1)
	s = strings.Replace(s, " source_title text NOT NULL,", " source_title text NOT NULL,\n render_version integer NOT NULL DEFAULT 1 CHECK(render_version IN (1,2)),", 1)
	s = strings.Replace(s, " result_uuid text NOT NULL,", " result_uuid text NOT NULL,\n preview integer NOT NULL DEFAULT 0 CHECK(preview IN (0,1)),\n resources_json text NOT NULL DEFAULT '{}' CHECK(json_valid(resources_json)),", 1)
	s = strings.ReplaceAll(s, "s.state IN ('pending','preparing','stop_unconfirmed') OR EXISTS", "s.state IN ('pending','preparing','stop_unconfirmed') OR EXISTS(SELECT 1 FROM media_edit_page p WHERE p.session_id=s.id AND p.state IN ('pending','running')) OR EXISTS")
	return s + editV8DDL
}

func upgradeEditStreaming(ctx context.Context, q connection) error {
	objects, e := records(ctx, q, "SELECT type,name,sql FROM sqlite_master WHERE sql IS NOT NULL AND type IN ('trigger','index') ORDER BY type,name")
	if e != nil {
		return e
	}
	for _, o := range objects {
		if o["type"] == "trigger" {
			if _, e = q.ExecContext(ctx, "DROP TRIGGER "+quote(textValue(o["name"]))); e != nil {
				return e
			}
		}
	}
	for _, name := range []string{"media_edit_session", "media_edit_job"} {
		columns, e := records(ctx, q, "PRAGMA table_info("+quote(name)+")")
		if e != nil {
			return e
		}
		names := []string{}
		for _, col := range columns {
			names = append(names, quote(textValue(col["name"])))
		}
		ddl := strings.Replace(tableDDL(schemaV8SQL, name), "CREATE TABLE "+name+" (", "CREATE TABLE "+name+"_v8 (", 1)
		if _, e = q.ExecContext(ctx, ddl); e != nil {
			return e
		}
		cols := strings.Join(names, ",")
		if _, e = q.ExecContext(ctx, "INSERT INTO "+name+"_v8("+cols+") SELECT "+cols+" FROM "+name+"; DROP TABLE "+name+"; ALTER TABLE "+name+"_v8 RENAME TO "+name); e != nil {
			return e
		}
	}
	if _, e = q.ExecContext(ctx, editV8DDL); e != nil {
		return e
	}
	expected, err := memoryDatabase()
	if err != nil {
		return err
	}
	defer expected.Close()
	if _, err = expected.ExecContext(ctx, schemaV8SQL); err != nil {
		return err
	}
	objects, err = records(ctx, expected, "SELECT type,name,sql FROM sqlite_master WHERE sql IS NOT NULL AND type IN ('trigger','index') ORDER BY type,name")
	if err != nil {
		return err
	}
	for _, o := range objects {
		if o["type"] == "trigger" || strings.HasPrefix(textValue(o["name"]), "idx_edit_") {
			if _, e = q.ExecContext(ctx, textValue(o["sql"])); e != nil {
				return e
			}
		}
	}
	return nil
}
