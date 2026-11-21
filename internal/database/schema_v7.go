package database

import (
	"context"
	"strings"
)

const editDDL = `
CREATE TABLE media_edit_session (
 id text PRIMARY KEY NOT NULL,
 source_file_id integer REFERENCES media_file(id) ON DELETE SET NULL,
 source_uuid text NOT NULL,
 source_path text NOT NULL,
 source_title text NOT NULL,
 source_identity text NOT NULL DEFAULT '',
 source_size integer NOT NULL DEFAULT 0,
 source_mtime text NOT NULL DEFAULT '',
 request_json text NOT NULL CHECK(json_valid(request_json)),
 state text NOT NULL CHECK(state IN ('pending','preparing','ready','failed','cancelled','interrupted','stop_unconfirmed','released')),
 width integer NOT NULL DEFAULT 0,
 height integer NOT NULL DEFAULT 0,
 frame_count integer NOT NULL DEFAULT 0 CHECK(frame_count BETWEEN 0 AND 900),
 progress integer NOT NULL DEFAULT 0 CHECK(progress BETWEEN 0 AND 100),
 cancel_requested integer NOT NULL DEFAULT 0 CHECK(cancel_requested IN (0,1)),
 error_message text NOT NULL DEFAULT '',
 diagnostic text NOT NULL DEFAULT '',
 created_at integer NOT NULL,
 updated_at integer NOT NULL
);
CREATE TABLE media_edit_job (
 id text PRIMARY KEY NOT NULL,
 session_id text NOT NULL REFERENCES media_edit_session(id) ON DELETE RESTRICT,
 request_json text NOT NULL CHECK(json_valid(request_json)),
 state text NOT NULL CHECK(state IN ('pending','running','verifying','publishing','completed','failed','cancelled','interrupted','stop_unconfirmed')),
 progress integer NOT NULL DEFAULT 0 CHECK(progress BETWEEN 0 AND 100),
 cancel_requested integer NOT NULL DEFAULT 0 CHECK(cancel_requested IN (0,1)),
 output_root text NOT NULL,
 result_file_id integer REFERENCES media_file(id) ON DELETE SET NULL,
 result_uuid text NOT NULL,
 result_path text NOT NULL DEFAULT '',
 proof_json text,
 error_message text NOT NULL DEFAULT '',
 diagnostic text NOT NULL DEFAULT '',
 created_at integer NOT NULL,
 updated_at integer NOT NULL
);
CREATE INDEX idx_edit_session_state ON media_edit_session(state,created_at);
CREATE INDEX idx_edit_job_state ON media_edit_job(state,created_at);
CREATE TRIGGER edit_source_delete BEFORE DELETE ON media_file
WHEN EXISTS(SELECT 1 FROM media_edit_session s WHERE s.source_file_id=OLD.id AND
 (s.state IN ('pending','preparing','stop_unconfirmed') OR EXISTS(SELECT 1 FROM media_edit_job j WHERE j.session_id=s.id AND j.state IN ('pending','running','verifying','publishing','stop_unconfirmed'))))
BEGIN SELECT RAISE(ABORT,'EDIT_ACTIVE'); END;
CREATE TRIGGER edit_source_move BEFORE UPDATE OF filepath,storage_kind ON media_file
WHEN EXISTS(SELECT 1 FROM media_edit_session s WHERE s.source_file_id=OLD.id AND
 (s.state IN ('pending','preparing','stop_unconfirmed') OR EXISTS(SELECT 1 FROM media_edit_job j WHERE j.session_id=s.id AND j.state IN ('pending','running','verifying','publishing','stop_unconfirmed'))))
BEGIN SELECT RAISE(ABORT,'EDIT_ACTIVE'); END;
`

func schemaV7() string {
	s := schemaV6SQL
	s = strings.ReplaceAll(s, "source_kind IN ('download','local','legacy')", "source_kind IN ('download','local','legacy','edited')")
	s = strings.ReplaceAll(s, "source_kind IN ('local','legacy')", "source_kind IN ('local','legacy','edited')")
	s = strings.ReplaceAll(s, "source_kind IN ('download','legacy')", "source_kind IN ('download','legacy','edited')")
	return s + editDDL
}

// Rebuild only the media constraint; all rows, IDs, and sequence values survive.
// Migrate owns the backup, foreign-key mode, and encompassing transaction.
func upgradeMediaEdits(ctx context.Context, q connection) error {
	triggers, err := records(ctx, q, "SELECT name FROM sqlite_master WHERE type='trigger'")
	if err != nil {
		return err
	}
	for _, t := range triggers {
		if _, err = q.ExecContext(ctx, "DROP TRIGGER "+quote(textValue(t["name"]))); err != nil {
			return err
		}
	}
	var sequence int64
	if err = q.QueryRowContext(ctx, "SELECT coalesce((SELECT seq FROM sqlite_sequence WHERE name='media'),0)").Scan(&sequence); err != nil {
		return err
	}
	ddl := strings.Replace(tableDDL(schemaV7SQL, "media"), `CREATE TABLE "media"`, `CREATE TABLE "media_v7"`, 1)
	if _, err = q.ExecContext(ctx, ddl); err != nil {
		return err
	}
	if _, err = q.ExecContext(ctx, `INSERT INTO media_v7 SELECT * FROM media; DROP TABLE media; ALTER TABLE media_v7 RENAME TO media; UPDATE sqlite_sequence SET seq=max(seq,?) WHERE name='media'`, sequence); err != nil {
		return err
	}
	// The table and indexes are new; create triggers from the final schema once.
	if _, err = q.ExecContext(ctx, editDDL[:strings.Index(editDDL, "CREATE TRIGGER")]); err != nil {
		return err
	}
	expected, err := memoryDatabase()
	if err != nil {
		return err
	}
	defer expected.Close()
	if _, err = expected.ExecContext(ctx, schemaV7SQL); err != nil {
		return err
	}
	objects, err := records(ctx, expected, "SELECT type,tbl_name,sql FROM sqlite_master WHERE sql IS NOT NULL AND type IN ('index','trigger') ORDER BY type,name")
	if err != nil {
		return err
	}
	for _, obj := range objects {
		if obj["type"] == "trigger" || obj["tbl_name"] == "media" {
			if _, err = q.ExecContext(ctx, textValue(obj["sql"])); err != nil {
				return err
			}
		}
	}
	return nil
}
