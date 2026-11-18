package database

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"github.com/google/uuid"
)

func recordedDirectory(ctx context.Context, q connection, path, key string, registered bool) (int64, error) {
	if !filepath.IsAbs(path) {
		return 0, fmt.Errorf("기존 디렉터리의 절대 경로를 확인할 수 없습니다: %s", path)
	}
	store := media.NewStore(q)
	if strings.HasPrefix(key, "winpath-v1:") {
		return store.EnsureDirectory(ctx, path, key, 1, registered)
	}
	return store.RecordedDirectory(ctx, path, registered)
}

// Iterate bounded pages and never open the filesystem when interpreting old DB
// references. Missing originals and ambiguous recorded spelling remain intact.
func bindRecordedDirectories(ctx context.Context, q connection, fileMap, locationMap string) error {
	store := media.NewStore(q)
	var last int64
	for {
		rows, err := records(ctx, q, "SELECT id,media_id,directory_path,directory_key,state FROM local_folder_location WHERE id>? ORDER BY id LIMIT 1000", last)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			last = row["id"].(int64)
			path := textValue(row["directory_path"])
			if !filepath.IsAbs(path) {
				continue
			}
			active := row["state"] == "active" || row["state"] == "reserved"
			directory, err := recordedDirectory(ctx, q, path, textValue(row["directory_key"]), active)
			if err != nil {
				return err
			}
			if row["state"] == "active" {
				if err = store.SetImportMedia(ctx, directory, row["media_id"].(int64)); err != nil {
					return err
				}
			}
			if locationMap != "" {
				_, err = q.ExecContext(ctx, "INSERT INTO "+quote(locationMap)+"(id,directory_id) VALUES(?,?)", last, directory)
			} else {
				_, err = q.ExecContext(ctx, "UPDATE local_folder_location SET directory_id=? WHERE id=?", directory, last)
			}
			if err != nil {
				return err
			}
		}
	}
	last = 0
	for {
		rows, err := records(ctx, q, "SELECT id,filepath,canonical_path_key,storage_kind FROM media_file WHERE id>? ORDER BY id LIMIT 1000", last)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			last = row["id"].(int64)
			path := textValue(row["filepath"])
			if !filepath.IsAbs(path) {
				if row["storage_kind"] == "external_reference" {
					return fmt.Errorf("외부 참조 파일 %d의 절대 경로를 확인할 수 없습니다", last)
				}
				continue
			}
			key := textValue(row["canonical_path_key"])
			if strings.HasPrefix(key, "winpath-v1:") {
				key = "winpath-v1:" + filepath.Dir(strings.TrimPrefix(key, "winpath-v1:"))
			} else {
				key = ""
			}
			directory, err := recordedDirectory(ctx, q, filepath.Dir(path), key, false)
			if err != nil {
				return err
			}
			if fileMap != "" {
				_, err = q.ExecContext(ctx, "INSERT INTO "+quote(fileMap)+"(id,directory_id) VALUES(?,?)", last, directory)
			} else {
				_, err = q.ExecContext(ctx, "UPDATE media_file SET directory_id=? WHERE id=?", directory, last)
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func upgradeDirectories(ctx context.Context, q connection, checkpoint func(string) error) error {
	expected, err := memoryDatabase()
	if err != nil {
		return err
	}
	defer expected.Close()
	if _, err = expected.ExecContext(ctx, schemaV5SQL); err != nil {
		return err
	}
	objects, err := records(ctx, expected, "SELECT type,name,tbl_name,sql FROM sqlite_master WHERE sql IS NOT NULL AND type IN ('table','trigger','index') AND name NOT LIKE 'sqlite_%' ORDER BY type,name")
	if err != nil {
		return err
	}
	oldTriggers, err := records(ctx, q, "SELECT name FROM sqlite_master WHERE type='trigger'")
	if err != nil {
		return err
	}
	for _, obj := range oldTriggers {
		if _, err = q.ExecContext(ctx, "DROP TRIGGER "+quote(textValue(obj["name"]))); err != nil {
			return err
		}
	}
	if _, err = q.ExecContext(ctx, tableDDL(schemaV5SQL, "media_directory")); err != nil {
		return err
	}
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	fileMap, locationMap := "v5_files_"+suffix, "v5_locations_"+suffix
	for _, name := range []string{fileMap, locationMap} {
		if _, err = q.ExecContext(ctx, "CREATE TEMP TABLE "+quote(name)+"(id INTEGER PRIMARY KEY,directory_id INTEGER NOT NULL)"); err != nil {
			return err
		}
		defer q.ExecContext(context.Background(), "DROP TABLE IF EXISTS temp."+quote(name))
	}
	if err = bindRecordedDirectories(ctx, q, fileMap, locationMap); err != nil {
		return err
	}
	if checkpoint != nil {
		if err = checkpoint("after_directory_backfill"); err != nil {
			return err
		}
	}
	changed := map[string]bool{"media_file": true, "local_folder_location": true, "local_import_target": true}
	for _, name := range []string{"local_folder_location", "media_file", "local_import_target"} {
		columns, e := records(ctx, q, "PRAGMA table_info("+quote(name)+")")
		if e != nil {
			return e
		}
		var sequence int64
		if name != "local_import_target" {
			if err = q.QueryRowContext(ctx, "SELECT coalesce((SELECT seq FROM sqlite_sequence WHERE name=?),0)", name).Scan(&sequence); err != nil {
				return err
			}
		}
		staging := name + "_v5"
		ddl := strings.Replace(tableDDL(schemaV5SQL, name), `CREATE TABLE "`+name+`"`, `CREATE TABLE "`+staging+`"`, 1)
		if _, err = q.ExecContext(ctx, ddl); err != nil {
			return err
		}
		insertCols, selectCols := []string{}, []string{}
		for _, col := range columns {
			column := quote(textValue(col["name"]))
			insertCols = append(insertCols, column)
			selectCols = append(selectCols, "old."+column)
		}
		join := ""
		mapping := ""
		if name == "media_file" {
			mapping = fileMap
		}
		if name == "local_folder_location" {
			mapping = locationMap
		}
		if mapping != "" {
			insertCols = append(insertCols, "directory_id")
			selectCols = append(selectCols, "mapping.directory_id")
			join = " LEFT JOIN temp." + quote(mapping) + " mapping ON mapping.id=old.id"
		}
		statement := "INSERT INTO " + quote(staging) + "(" + strings.Join(insertCols, ",") + ") SELECT " + strings.Join(selectCols, ",") + " FROM " + quote(name) + " old" + join
		if _, err = q.ExecContext(ctx, statement); err != nil {
			return err
		}
		if _, err = q.ExecContext(ctx, "DROP TABLE "+quote(name)+"; ALTER TABLE "+quote(staging)+" RENAME TO "+quote(name)); err != nil {
			return err
		}
		if name != "local_import_target" {
			if _, err = q.ExecContext(ctx, "UPDATE sqlite_sequence SET seq=max(seq,?) WHERE name=?", sequence, name); err != nil {
				return err
			}
		}
	}
	newTables := map[string]bool{"media_directory": true, "file_move_job": true, "file_move_item": true}
	for _, name := range []string{"file_move_job", "file_move_item"} {
		if _, err = q.ExecContext(ctx, tableDDL(schemaV5SQL, name)); err != nil {
			return err
		}
	}
	for _, obj := range objects {
		kind, name := textValue(obj["type"]), textValue(obj["tbl_name"])
		if kind == "trigger" || (kind == "index" && (changed[name] || newTables[name])) {
			if _, err = q.ExecContext(ctx, textValue(obj["sql"])); err != nil {
				return err
			}
		}
	}
	return nil
}
