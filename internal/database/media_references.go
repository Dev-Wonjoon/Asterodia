package database

import (
	"context"
	"strings"
)

// SQLite CHECK constraints require a table rebuild. Foreign keys are disabled
// by Migrate, and the entire rebuild shares its backed-up atomic transaction.
func upgradeMediaReferences(ctx context.Context, q connection) error {
	objects, err := records(ctx, q, "SELECT type,name,sql FROM sqlite_master WHERE sql IS NOT NULL AND (type='trigger' OR (type='index' AND tbl_name='media_file')) ORDER BY type,name")
	if err != nil {
		return err
	}
	for _, obj := range objects {
		if _, err = q.ExecContext(ctx, "DROP "+textValue(obj["type"])+" "+quote(textValue(obj["name"]))); err != nil {
			return err
		}
	}
	var sequence int64
	if err = q.QueryRowContext(ctx, "SELECT coalesce((SELECT seq FROM sqlite_sequence WHERE name='media_file'),0)").Scan(&sequence); err != nil {
		return err
	}
	start := strings.Index(schemaV4SQL, `CREATE TABLE "media_file"`)
	ddl := schemaV4SQL[start:]
	ddl = ddl[:strings.Index(ddl, ";")+1]
	ddl = strings.Replace(ddl, `CREATE TABLE "media_file"`, `CREATE TABLE "media_file_v4"`, 1)
	if _, err = q.ExecContext(ctx, ddl); err != nil {
		return err
	}
	if _, err = q.ExecContext(ctx, "INSERT INTO media_file_v4 SELECT * FROM media_file; DROP TABLE media_file; ALTER TABLE media_file_v4 RENAME TO media_file"); err != nil {
		return err
	}
	for _, obj := range objects {
		if _, err = q.ExecContext(ctx, textValue(obj["sql"])); err != nil {
			return err
		}
	}
	_, err = q.ExecContext(ctx, "UPDATE sqlite_sequence SET seq=max(seq,?) WHERE name='media_file'", sequence)
	return err
}
