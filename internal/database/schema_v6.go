package database

import (
	"context"
	"strings"
)

func schemaV6() string {
	s := schemaV5SQL
	job := tableDDL(s, "local_import_job")
	updated := strings.Replace(job, `    "cancel_requested_at" integer,`, `    "cancel_requested_at" integer,
    "pause_requested_at" integer,
    "scan_complete" integer NOT NULL DEFAULT 0 CHECK(scan_complete IN (0,1)),
    "work_version" integer NOT NULL DEFAULT 2 CHECK(work_version IN (1,2)),
    "current_path" text NOT NULL DEFAULT '',
    "error_message" text,
    "active_millis" integer NOT NULL DEFAULT 0 CHECK(active_millis>=0),
    "run_started_at" integer,`, 1)
	updated = strings.Replace(updated, "'completed','failed','cancelled','interrupted'", "'completed','completed_with_errors','paused','failed','cancelled','interrupted'", 1)
	s = strings.Replace(s, job, updated, 1)
	folder := tableDDL(s, "local_import_folder")
	s = strings.Replace(s, folder, strings.Replace(folder, "'completed','failed'", "'completed','completed_with_errors','failed'", 1), 1)
	attempt := tableDDL(s, "local_import_attempt")
	updated = strings.Replace(attempt, "'completed','failed'", "'completed','completed_with_errors','failed'", 1)
	updated = strings.Replace(updated, "outcome = 'completed'", "outcome IN ('completed','completed_with_errors')", 1)
	s = strings.Replace(s, attempt, updated, 1)
	target := tableDDL(s, "local_import_target")
	updated = strings.Replace(target, `    "error_code" text,`, `    "error_code" text,
    "sort_key" text NOT NULL DEFAULT '',
    "result_kind" text CHECK(result_kind IS NULL OR result_kind IN ('added','updated','unchanged')),
    "refresh_requested" integer NOT NULL DEFAULT 0 CHECK(refresh_requested IN (0,1)),`, 1)
	s = strings.Replace(s, target, updated, 1)
	start := strings.Index(s, "CREATE TRIGGER v2_media_delete")
	end := start + strings.Index(s[start:], "END;") + len("END;")
	old := s[start:end]
	guard := strings.Replace(old, "      WHERE l.media_id = OLD.id", "      JOIN local_import_job j ON j.id = f.job_id\n      WHERE l.media_id = OLD.id", 1)
	guard = strings.Replace(guard, "f.state IN ('pending','running')", "(j.state = 'running' AND f.state IN ('pending','running','interrupted'))", 1)
	s = strings.Replace(s, old, guard, 1)
	return s + `
CREATE INDEX idx_file_path_lookup ON media_file(filepath,id);
CREATE INDEX idx_file_canonical_lookup ON media_file(canonical_path_key,id);
CREATE INDEX idx_import_target_work ON local_import_target(attempt_id,state,sort_key,id);
CREATE INDEX idx_import_folder_work ON local_import_folder(job_id,state,id);
CREATE TABLE local_import_scan_directory (
 id integer PRIMARY KEY AUTOINCREMENT,
 job_id text NOT NULL REFERENCES local_import_job(id) ON DELETE CASCADE,
 directory_path text NOT NULL,
 directory_key text NOT NULL,
 state text NOT NULL CHECK(state IN ('pending','completed','failed')),
 excluded_count integer NOT NULL DEFAULT 0 CHECK(excluded_count>=0),
 error_message text,
 UNIQUE(job_id,directory_key)
);
CREATE INDEX idx_import_scan_work ON local_import_scan_directory(job_id,state,id);
`
}

// Historical v5 DDL stays immutable. All replacements share the backed-up
// migration transaction; triggers are restored only after all tables exist.
func upgradeImportCheckpoints(ctx context.Context, q connection, checkpoint func(string) error) error {
	objects, err := records(ctx, q, "SELECT type,name,tbl_name,sql FROM sqlite_master WHERE sql IS NOT NULL AND (type='trigger' OR type='index') ORDER BY type,name")
	if err != nil {
		return err
	}
	for _, obj := range objects {
		if obj["type"] == "trigger" {
			if _, err = q.ExecContext(ctx, "DROP TRIGGER "+quote(textValue(obj["name"]))); err != nil {
				return err
			}
		}
	}
	changed := map[string]bool{"local_import_job": true, "local_import_folder": true, "local_import_attempt": true, "local_import_target": true}
	for _, name := range []string{"local_import_job", "local_import_folder", "local_import_attempt", "local_import_target"} {
		columns, e := records(ctx, q, "PRAGMA table_info("+quote(name)+")")
		if e != nil {
			return e
		}
		staging := name + "_v6"
		ddl := strings.Replace(tableDDL(schemaSQL, name), `CREATE TABLE "`+name+`"`, `CREATE TABLE "`+staging+`"`, 1)
		if _, err = q.ExecContext(ctx, ddl); err != nil {
			return err
		}
		var names []string
		for _, col := range columns {
			names = append(names, quote(textValue(col["name"])))
		}
		cols := strings.Join(names, ",")
		if _, err = q.ExecContext(ctx, "INSERT INTO "+quote(staging)+"("+cols+") SELECT "+cols+" FROM "+quote(name)+"; DROP TABLE "+quote(name)+"; ALTER TABLE "+quote(staging)+" RENAME TO "+quote(name)); err != nil {
			return err
		}
	}
	if _, err = q.ExecContext(ctx, "UPDATE local_import_job SET work_version=1,scan_complete=CASE WHEN state='scanning' THEN 0 ELSE 1 END"); err != nil {
		return err
	}
	if _, err = q.ExecContext(ctx, tableDDL(schemaSQL, "local_import_scan_directory")); err != nil {
		return err
	}
	expected, e := memoryDatabase()
	if e != nil {
		return e
	}
	defer expected.Close()
	if _, err = expected.ExecContext(ctx, schemaSQL); err != nil {
		return err
	}
	want, e := records(ctx, expected, "SELECT type,name,tbl_name,sql FROM sqlite_master WHERE sql IS NOT NULL AND (type='trigger' OR type='index') ORDER BY type,name")
	if e != nil {
		return e
	}
	for _, obj := range want {
		if obj["type"] == "trigger" || changed[textValue(obj["tbl_name"])] || obj["tbl_name"] == "local_import_scan_directory" || obj["name"] == "idx_file_path_lookup" || obj["name"] == "idx_file_canonical_lookup" {
			if _, err = q.ExecContext(ctx, textValue(obj["sql"])); err != nil {
				return err
			}
		}
	}
	if checkpoint != nil {
		return checkpoint("after_import_checkpoint_upgrade")
	}
	return nil
}
