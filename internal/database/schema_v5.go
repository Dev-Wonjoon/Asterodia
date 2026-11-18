package database

import (
	_ "embed"
	"strings"
)

//go:embed schema_v5.sql
var directorySQL string

const uuidDefault = `(lower(hex(randomblob(4)))||'-'||lower(hex(randomblob(2)))||'-4'||substr(lower(hex(randomblob(2))),2)||'-'||substr('89ab',(random() & 3)+1,1)||substr(lower(hex(randomblob(2))),2)||'-'||lower(hex(randomblob(6))))`

func tableDDL(schema, name string) string {
	start := strings.Index(schema, `CREATE TABLE "`+name+`"`)
	if start < 0 {
		start = strings.Index(schema, "CREATE TABLE "+name+" (")
	}
	if start < 0 {
		panic("missing schema table: " + name)
	}
	end := strings.Index(schema[start:], ";")
	return schema[start : start+end+1]
}
func schemaV5() string {
	s := schemaV4SQL
	old := tableDDL(s, "media_file")
	file := strings.Replace(old, `    "file_key" text NOT NULL,`, `    "file_key" text NOT NULL,
    "file_uuid" text NOT NULL DEFAULT `+uuidDefault+` CHECK(length(file_uuid)=36 AND length(replace(file_uuid,'-',''))=32 AND substr(file_uuid,9,1)='-' AND substr(file_uuid,14,1)='-' AND substr(file_uuid,19,1)='-' AND substr(file_uuid,24,1)='-' AND substr(file_uuid,15,1)='4' AND substr(file_uuid,20,1) IN ('8','9','a','b') AND replace(file_uuid,'-','') NOT GLOB '*[^0-9a-f]*'),
    "filename_token" text NOT NULL DEFAULT (lower(hex(randomblob(4)))) CHECK(length(filename_token)=8 AND filename_token NOT GLOB '*[^0-9a-f]*'),
    "directory_id" integer REFERENCES media_directory(id) ON DELETE RESTRICT,`, 1)
	file = strings.Replace(file, "AND local_folder_location_id IS NOT NULL", "AND directory_id IS NOT NULL", 1)
	s = strings.Replace(s, old, file, 1)
	old = tableDDL(s, "local_folder_location")
	location := strings.Replace(old, `    "media_id" integer NOT NULL,`, `    "media_id" integer NOT NULL,
    "directory_id" integer REFERENCES media_directory(id) ON DELETE RESTRICT,`, 1)
	s = strings.Replace(s, old, location, 1)
	s = strings.Replace(s, `CREATE UNIQUE INDEX "uq_reserved_directory" ON "local_folder_location" ("directory_key")`, `CREATE UNIQUE INDEX "uq_reserved_directory" ON "local_folder_location" ("media_id","directory_key")`, 1)
	s = strings.ReplaceAll(s, "'app_managed','unverified_legacy'", "'app_managed','unverified_legacy','external_reference'")
	s = strings.ReplaceAll(s, "WHEN NEW.storage_kind = 'external_reference'", "WHEN NEW.local_folder_location_id IS NOT NULL")
	start := strings.Index(s, "CREATE TRIGGER v2_media_source_update")
	end := strings.Index(s[start:], "END;") + len("END;")
	s = s[:start] + `CREATE TRIGGER v2_media_source_update
    BEFORE UPDATE OF source_kind ON media
    WHEN (NEW.source_kind<>'local' AND EXISTS(SELECT 1 FROM local_folder WHERE media_id=OLD.id))
      OR (NEW.source_kind='local' AND EXISTS(SELECT 1 FROM media_file WHERE media_id=OLD.id AND storage_kind<>'external_reference'))
    BEGIN SELECT RAISE(ABORT,'MEDIA_SOURCE_MISMATCH'); END;` + s[start+end:]
	old = tableDDL(s, "local_import_target")
	target := strings.Replace(old, `    "inspection_json" text,`, `    "inspection_json" text,
    "media_file_id" integer REFERENCES media_file(id) ON DELETE SET NULL,
    "file_uuid_snapshot" text,
    "inspection_hash" text CHECK(inspection_hash IS NULL OR (length(inspection_hash)=64 AND inspection_hash NOT GLOB '*[^0-9a-f]*')),
    "inspector_version" text,
    "committed_at" integer,`, 1)
	target = strings.Replace(target, "'pending','inspected','unchanged','changed','failed'", "'pending','inspected','unchanged','changed','registered','failed','excluded'", 1)
	target = strings.Replace(target, `CONSTRAINT "ck_import_target_state"`, `CONSTRAINT ck_registered_checkpoint CHECK(state<>'registered' OR (committed_at IS NOT NULL AND inspection_hash IS NOT NULL AND file_uuid_snapshot IS NOT NULL AND length(file_uuid_snapshot)=36)),
    CONSTRAINT "ck_import_target_state"`, 1)
	s = strings.Replace(s, old, target, 1)
	s += `CREATE UNIQUE INDEX uq_file_uuid ON media_file(file_uuid);
CREATE INDEX idx_file_directory ON media_file(directory_id,media_id,id);
CREATE INDEX idx_local_directory ON local_folder_location(directory_id,media_id);
CREATE INDEX idx_import_target_state ON local_import_target(attempt_id,state,id);
`
	return s + directorySQL
}
