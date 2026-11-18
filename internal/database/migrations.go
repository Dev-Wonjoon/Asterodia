package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	whatwg "github.com/nlnwa/whatwg-url/url"
)

type MigrationOptions struct {
	BackupDirectory   string
	LegacyStorageRoot string
	// Checkpoint is an optional diagnostic hook; application callers omit it.
	Checkpoint func(string) error
}
type MigrationResult struct {
	FromVersion int    `json:"fromVersion"`
	ToVersion   int    `json:"toVersion"`
	BackupPath  string `json:"backupPath"`
	Migrated    bool   `json:"migrated"`
}

func hashBytes(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }
func normalizedURL(v any) (string, error) {
	s, ok := v.(string)
	if !ok || strings.ContainsFunc(s, func(r rune) bool { return r <= 32 || r == 127 }) {
		return "", fmt.Errorf("올바르지 않은 요청 URL입니다")
	}
	u, e := whatwg.Parse(s)
	if e != nil || (u.Scheme() != "http" && u.Scheme() != "https") || u.Host() == "" || u.Username() != "" || u.Password() != "" {
		return "", fmt.Errorf("올바르지 않은 요청 URL입니다")
	}
	return u.Href(false), nil
}

// Migrate validates known schemas before creating a verified snapshot. All SQL
// changes, preserved source records and the version marker commit together.
func Migrate(ctx context.Context, db *sql.DB, options MigrationOptions) (MigrationResult, error) {
	result := MigrationResult{ToVersion: schemaVersion}
	conn, err := db.Conn(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Close()
	if err = conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&result.FromVersion); err != nil {
		return result, err
	}
	applied, err := validateSchema(ctx, conn, result.FromVersion)
	if err != nil {
		return result, err
	}
	if err = healthy(ctx, conn); err != nil {
		return result, err
	}
	if result.FromVersion == schemaVersion {
		return result, nil
	}
	var before int64
	if err = conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&before); err != nil {
		return result, err
	}
	result.BackupPath, err = backup(ctx, conn, options.BackupDirectory, "pre-migration")
	if err != nil {
		return result, err
	}
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return result, err
	}
	defer conn.ExecContext(context.Background(), "PRAGMA foreign_keys=ON")
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return result, err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	var current int64
	if err = conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&current); err != nil {
		return result, err
	}
	if current != before {
		return result, fmt.Errorf("백업 중 DB가 변경되었습니다. 다시 시도하세요")
	}
	source, err := allData(ctx, conn)
	if err != nil {
		return result, err
	}
	fingerprint := digest(source)
	if result.FromVersion >= 3 {
		// Existing rows and all operational history are preserved by the v4 rebuild.
	} else if result.FromVersion == 2 {
		ddl := schemaV3SQL[len(baseSchema()):]
		if _, err = conn.ExecContext(ctx, ddl); err != nil {
			return result, err
		}
		var jobs int
		if err = conn.QueryRowContext(ctx, "SELECT count(*) FROM download_queue").Scan(&jobs); err != nil {
			return result, err
		}
		if jobs > 0 {
			if err = bindLegacyStorage(ctx, conn, options.LegacyStorageRoot); err != nil {
				return result, err
			}
		}
		if err = preserveV2Work(ctx, conn, source); err != nil {
			return result, err
		}
	} else {
		migrations, e := legacyMigrations()
		if e != nil {
			return result, e
		}
		for _, m := range migrations[applied:] {
			for _, statement := range strings.Split(m.SQL, "--> statement-breakpoint") {
				// The historical ALTER sequence removed media.url. Preserve and backfill
				// those references before applying that statement.
				if strings.Contains(strings.ToLower(statement), "drop column `url`") {
					if e = backfillLegacyURLs(ctx, conn); e != nil {
						return result, e
					}
				}
				if _, e = conn.ExecContext(ctx, statement); e != nil {
					return result, e
				}
			}
			if _, e = conn.ExecContext(ctx, "INSERT INTO __drizzle_migrations(hash,created_at) VALUES(?,?)", m.Hash, m.When); e != nil {
				return result, e
			}
		}
		data, e := allData(ctx, conn)
		if e != nil {
			return result, e
		}
		sequences, e := records(ctx, conn, "SELECT name,seq FROM sqlite_sequence")
		if e != nil {
			return result, e
		}
		tables, e := records(ctx, conn, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
		if e != nil {
			return result, e
		}
		if tableExists(ctx, conn, "media_fts") {
			if _, e = conn.ExecContext(ctx, "DROP TABLE media_fts"); e != nil {
				return result, e
			}
		}
		for _, t := range tables {
			name := textValue(t["name"])
			if name == "media_fts" || strings.HasPrefix(name, "media_fts_") {
				continue
			}
			if _, e = conn.ExecContext(ctx, "DROP TABLE "+quote(name)); e != nil {
				return result, e
			}
		}
		if _, e = conn.ExecContext(ctx, schemaSQL); e != nil {
			return result, e
		}
		if _, e = conn.ExecContext(ctx, "CREATE TABLE __drizzle_migrations(id SERIAL PRIMARY KEY,hash text NOT NULL,created_at numeric)"); e != nil {
			return result, e
		}
		if e = convertLegacy(ctx, conn, data, source, sequences, options.LegacyStorageRoot); e != nil {
			return result, e
		}
		for _, row := range sequences {
			name := textValue(row["name"])
			if !tableExists(ctx, conn, name) {
				continue
			}
			if _, e = conn.ExecContext(ctx, "UPDATE sqlite_sequence SET seq=max(seq,?) WHERE name=?", row["seq"], name); e != nil {
				return result, e
			}
			if name == "media" {
				if _, e = conn.ExecContext(ctx, "UPDATE sqlite_sequence SET seq=max(seq,?) WHERE name='media_file'", row["seq"]); e != nil {
					return result, e
				}
			}
		}
	}
	if result.FromVersion == 2 || result.FromVersion == 3 {
		if err = upgradeMediaReferences(ctx, conn); err != nil {
			return result, err
		}
	}
	if result.FromVersion >= 2 && result.FromVersion < 5 {
		if err = upgradeDirectories(ctx, conn, options.Checkpoint); err != nil {
			return result, err
		}
	} else if result.FromVersion < 2 {
		if err = bindRecordedDirectories(ctx, conn, "", ""); err != nil {
			return result, err
		}
	}
	if result.FromVersion >= 2 {
		if err = upgradeImportCheckpoints(ctx, conn, options.Checkpoint); err != nil {
			return result, err
		}
	}
	now := time.Now().UnixMilli()
	if _, err = insert(ctx, conn, "migration_run", record{"id": uuid.NewString(), "target_version": schemaVersion, "source_fingerprint": fingerprint, "schema_hash": hashBytes([]byte(schemaSQL)), "backup_path": result.BackupPath, "completed_at": now}); err != nil {
		return result, err
	}
	if _, err = conn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", schemaVersion)); err != nil {
		return result, err
	}
	if err = healthy(ctx, conn); err != nil {
		return result, err
	}
	if _, err = validateSchema(ctx, conn, schemaVersion); err != nil {
		return result, err
	}
	if options.Checkpoint != nil {
		if err = options.Checkpoint("before_commit"); err != nil {
			return result, err
		}
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return result, err
	}
	result.Migrated = true
	return result, nil
}

func bindLegacyStorage(ctx context.Context, q connection, fallback string) error {
	jobs, err := records(ctx, q, "SELECT id FROM download_queue")
	if err != nil {
		return err
	}
	for _, job := range jobs {
		paths, e := records(ctx, q, `SELECT manifest_path AS path FROM download_attempt WHERE job_id=? AND manifest_path IS NOT NULL UNION ALL SELECT t.staged_path FROM download_target t JOIN download_attempt a ON a.id=t.attempt_id WHERE a.job_id=? AND t.staged_path IS NOT NULL UNION ALL SELECT t.final_path FROM download_target t JOIN download_attempt a ON a.id=t.attempt_id WHERE a.job_id=? AND t.final_path IS NOT NULL`, job["id"], job["id"], job["id"])
		if e != nil {
			return e
		}
		root := ""
		for _, r := range paths {
			path := textValue(r["path"])
			slash := filepath.ToSlash(path)
			index := strings.LastIndex(strings.ToLower(slash), "/.storagebucket/")
			if !filepath.IsAbs(path) || index < 0 {
				return fmt.Errorf("구버전 작업의 저장 경로를 확인할 수 없습니다")
			}
			candidate := filepath.Clean(filepath.FromSlash(slash[:index+1]))
			if root != "" && !strings.EqualFold(root, candidate) {
				return fmt.Errorf("하나의 구버전 작업에 여러 저장 루트가 기록되어 있습니다")
			}
			root = candidate
		}
		if root == "" {
			root = fallback
		}
		if !filepath.IsAbs(root) {
			return fmt.Errorf("구버전 다운로드 저장 폴더를 지정하세요")
		}
		if _, e = insert(ctx, q, "download_job_storage", record{"job_id": job["id"], "storage_root": root, "created_at": time.Now().UnixMilli()}); e != nil {
			return e
		}
	}
	return nil
}
func preserveV2Work(ctx context.Context, q connection, source map[string][]record) error {
	for _, table := range []string{"download_queue", "download_attempt", "download_target", "maintenance_task", "local_import_job", "local_import_folder", "local_import_attempt", "local_import_target"} {
		for _, row := range source[table] {
			if _, e := insert(ctx, q, "legacy_record", record{"table_name": "migration-v2:" + table, "row_key": rowKey(row), "payload_json": jsonValue(row)}); e != nil {
				return e
			}
		}
	}
	now := time.Now().UnixMilli()
	_, err := q.ExecContext(ctx, `UPDATE download_queue SET state=CASE WHEN state IN ('pending','running') THEN 'interrupted' ELSE state END,cleanup_required=0,recovery_enabled=0,error_code='IMPORTED_HISTORY',options_json=json_set(CASE WHEN json_type(options_json)='object' THEN options_json ELSE '{}' END,'$._importedHistory',json('true')) WHERE state IN ('pending','running') OR cleanup_required=1 OR EXISTS(SELECT 1 FROM download_attempt WHERE job_id=download_queue.id AND outcome IS NULL)`)
	if err != nil {
		return err
	}
	if _, err = q.ExecContext(ctx, "UPDATE download_attempt SET phase='finished',outcome='interrupted',finished_at=? WHERE outcome IS NULL", now); err != nil {
		return err
	}
	if _, err = q.ExecContext(ctx, "UPDATE maintenance_task SET state='failed',last_error='구버전 작업 원문을 보존했습니다. 자동 실행을 보류합니다.',updated_at=? WHERE state IN ('pending','running')", now); err != nil {
		return err
	}
	if _, err = q.ExecContext(ctx, "UPDATE local_import_job SET state='interrupted',cleanup_required=0,updated_at=? WHERE state IN ('scanning','ready','pending','running') OR cleanup_required=1", now); err != nil {
		return err
	}
	if _, err = q.ExecContext(ctx, "UPDATE local_import_folder SET state='interrupted',cleanup_required=0 WHERE state IN ('pending','running') OR cleanup_required=1"); err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, "UPDATE local_import_attempt SET phase='finished',outcome='interrupted',finished_at=? WHERE outcome IS NULL", now)
	return err
}
func legacyTime(value any, table, column string) (any, error) {
	if value == nil {
		return nil, nil
	}
	if s, ok := value.(string); ok {
		for _, layout := range []string{"2006-01-02 15:04:05.999", "2006-01-02T15:04:05.999Z", "2006-01-02T15:04:05Z"} {
			if t, e := time.Parse(layout, s); e == nil {
				return t.UnixMilli(), nil
			}
		}
		return nil, fmt.Errorf("시각 형식을 확인할 수 없습니다: %s.%s", table, column)
	}
	n, err := integer(value)
	if err != nil {
		return nil, err
	}
	// The legacy Drizzle schema used mode:'timestamp' (seconds) for these
	// columns, including queue start/finish times. Do not infer units by magnitude.
	switch table + "." + column {
	case "profiles.updated_at", "download_urls.created_at", "media.created_at", "media.updated_at",
		"tag.updated_at", "favorite.created_at", "download_queue.created_at", "download_queue.updated_at",
		"download_queue.started_at", "download_queue.finished_at":
	default:
		return nil, fmt.Errorf("숫자 시각의 단위를 확인할 수 없습니다: %s.%s", table, column)
	}
	if n > 8640000000000 || n < -8640000000000 {
		return nil, fmt.Errorf("시각 범위를 벗어났습니다")
	}
	return n * 1000, nil
}
func backfillLegacyURLs(ctx context.Context, q connection) error {
	rows, err := records(ctx, q, "SELECT id,url,video_id,created_at FROM media WHERE url IS NOT NULL")
	if err != nil {
		return err
	}
	for _, r := range rows {
		if _, err = normalizedURL(r["url"]); err != nil {
			return err
		}
		if _, err = q.ExecContext(ctx, "INSERT INTO download_urls(url,video_id,created_at) VALUES(?,?,?) ON CONFLICT(url) DO NOTHING", r["url"], r["video_id"], r["created_at"]); err != nil {
			return err
		}
		if _, err = q.ExecContext(ctx, "UPDATE media SET url_id=(SELECT id FROM download_urls WHERE url=?) WHERE id=?", r["url"], r["id"]); err != nil {
			return err
		}
	}
	return nil
}
func convertLegacy(ctx context.Context, q connection, data, original map[string][]record, sequences []record, storageRoot string) error {
	for _, table := range []string{"platform", "profiles", "tag", "__drizzle_migrations"} {
		for _, r := range data[table] {
			row := clone(r)
			if table != "__drizzle_migrations" {
				for _, column := range []string{"created_at", "updated_at"} {
					if v, ok := row[column]; ok {
						t, e := legacyTime(v, table, column)
						if e != nil {
							return e
						}
						row[column] = t
					}
				}
			}
			if _, e := insert(ctx, q, table, row); e != nil {
				return e
			}
		}
	}
	urlValues := map[int64]any{}
	normalized := map[string]int64{}
	var nextURL int64
	for _, row := range sequences {
		if row["name"] == "download_urls" {
			n, e := integer(row["seq"])
			if e != nil {
				return e
			}
			if n > nextURL {
				nextURL = n
			}
		}
	}
	for _, r := range data["download_urls"] {
		id, e := integer(r["id"])
		if e != nil {
			return e
		}
		url, e := normalizedURL(r["url"])
		if e != nil {
			return e
		}
		if _, exists := normalized[url]; exists {
			return fmt.Errorf("URL 정규화 충돌이 있어 이관을 중단했습니다")
		}
		created, e := legacyTime(r["created_at"], "download_urls", "created_at")
		if e != nil {
			return e
		}
		if _, e = insert(ctx, q, "download_urls", record{"id": id, "url": r["url"], "normalized_url": url, "normalization_version": 1, "legacy_video_id": r["video_id"], "created_at": created}); e != nil {
			return e
		}
		urlValues[id] = r["url"]
		normalized[url] = id
		if id > nextURL {
			nextURL = id
		}
	}
	used := map[int64]bool{}
	for _, r := range data["media"] {
		id, e := integer(r["id"])
		if e != nil {
			return e
		}
		created, e := legacyTime(r["created_at"], "media", "created_at")
		if e != nil {
			return e
		}
		updated, e := legacyTime(r["updated_at"], "media", "updated_at")
		if e != nil {
			return e
		}
		var urlID any
		var url any
		if r["url_id"] != nil {
			n, e := integer(r["url_id"])
			if e != nil {
				return e
			}
			if used[n] {
				return fmt.Errorf("같은 URL에 여러 구버전 묶음이 있어 이관을 중단했습니다")
			}
			used[n] = true
			urlID = n
			var exists bool
			url, exists = urlValues[n]
			if !exists {
				return fmt.Errorf("기존 미디어의 URL 참조가 없습니다")
			}
		}
		if _, e = insert(ctx, q, "media", record{"id": id, "source_kind": "legacy", "source_title": r["title"], "custom_title": r["title"], "cover_mode": "auto", "revision": 0, "created_at": created, "updated_at": updated}); e != nil {
			return e
		}
		origin := "none"
		if r["thumbnail_path"] != nil {
			origin = "unverified_legacy"
		}
		if _, e = insert(ctx, q, "media_file", record{"id": id, "media_id": id, "file_key": "legacy-media:" + strconv.FormatInt(id, 10), "storage_kind": "unverified_legacy", "legacy_url_id": urlID, "source_url": url, "platform_id": r["platform_id"], "profile_id": r["profile_id"], "title": r["title"], "filepath": r["filepath"], "filesize": r["filesize"], "thumbnail_path": r["thumbnail_path"], "thumbnail_origin": origin, "kind": "unknown", "position": 0, "created_at": created}); e != nil {
			return e
		}
	}
	for _, table := range []string{"media_tag", "favorite"} {
		for _, r := range data[table] {
			row := clone(r)
			if table == "favorite" {
				t, e := legacyTime(row["created_at"], table, "created_at")
				if e != nil {
					return e
				}
				row["created_at"] = t
			}
			if _, e := insert(ctx, q, table, row); e != nil {
				return e
			}
		}
	}
	for _, r := range data["download_queue"] {
		key, e := normalizedURL(r["url"])
		if e != nil {
			return e
		}
		urlID, exists := normalized[key]
		if !exists {
			nextURL++
			urlID = nextURL
			t, e := legacyTime(r["created_at"], "download_queue", "created_at")
			if e != nil {
				return e
			}
			if t == nil {
				t = time.Now().UnixMilli()
			}
			if _, e = insert(ctx, q, "download_urls", record{"id": urlID, "url": r["url"], "normalized_url": key, "normalization_version": 1, "created_at": t}); e != nil {
				return e
			}
			normalized[key] = urlID
		}
		state := textValue(r["status"])
		if state == "pending" || state == "downloading" {
			state = "interrupted"
		}
		options := r["options"]
		for i := 0; i < 2; i++ {
			s, ok := options.(string)
			if !ok {
				break
			}
			var value any
			if e = json.Unmarshal([]byte(s), &value); e != nil {
				return fmt.Errorf("구버전 다운로드 옵션이 올바르지 않습니다")
			}
			options = value
		}
		if options != nil {
			if _, ok := options.(map[string]any); !ok {
				return fmt.Errorf("구버전 다운로드 옵션은 객체여야 합니다")
			}
		}
		row := record{"id": r["id"], "request_url_id": urlID, "original_url": r["url"], "mode": "create", "state": state, "options_json": jsonValue(options), "progress": r["progress"], "auto_run_count": nil, "auto_history_unknown": 1, "recovery_enabled": 0, "title": r["title"], "thumbnail": r["thumbnail"], "error_message": r["error_message"], "log": r["log"]}
		for _, c := range []string{"created_at", "updated_at", "started_at", "finished_at"} {
			v, e := legacyTime(r[c], "download_queue", c)
			if e != nil {
				return e
			}
			row[c] = v
		}
		if _, e = insert(ctx, q, "download_queue", row); e != nil {
			return e
		}
		if !filepath.IsAbs(storageRoot) {
			return fmt.Errorf("구버전 다운로드 저장 폴더가 필요합니다")
		}
		if _, e = insert(ctx, q, "download_job_storage", record{"job_id": r["id"], "storage_root": filepath.Clean(storageRoot), "created_at": time.Now().UnixMilli()}); e != nil {
			return e
		}
	}
	for table, rows := range original {
		for _, r := range rows {
			if _, e := insert(ctx, q, "legacy_record", record{"table_name": table, "row_key": rowKey(r), "payload_json": jsonValue(r)}); e != nil {
				return e
			}
		}
	}
	return nil
}
