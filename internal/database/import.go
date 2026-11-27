package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type ImportResult struct {
	Cancelled      bool   `json:"cancelled"`
	SourceVersion  int    `json:"sourceVersion"`
	AddedMedia     int    `json:"addedMedia"`
	AddedFiles     int    `json:"addedFiles"`
	SkippedMedia   int    `json:"skippedMedia"`
	SkippedFiles   int    `json:"skippedFiles"`
	AddedHistory   int    `json:"addedHistory"`
	MissingFiles   int    `json:"missingFiles"`
	BackupPath     string `json:"backupPath"`
	SourceSnapshot string `json:"sourceSnapshot"`
}
type Importer struct {
	db                *sql.DB
	filename, backups string
	mu                sync.Mutex
}

func NewImporter(db *sql.DB, filename, backups string) *Importer {
	return &Importer{db: db, filename: filename, backups: backups}
}
func (i *Importer) Import(ctx context.Context, sourcePath string) (ImportResult, error) {
	result := ImportResult{}
	if !i.mu.TryLock() {
		return result, fmt.Errorf("다른 DB 가져오기가 진행 중입니다")
	}
	defer i.mu.Unlock()
	if !filepath.IsAbs(sourcePath) {
		return result, fmt.Errorf("가져올 DB 절대 경로가 필요합니다")
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return result, fmt.Errorf("DB 파일을 선택하세요")
	}
	if current, e := os.Stat(i.filename); e == nil && os.SameFile(current, info) {
		return result, fmt.Errorf("현재 사용 중인 DB는 가져올 수 없습니다")
	}
	source, err := connectFile(sourcePath, "ro")
	if err != nil {
		return result, err
	}
	defer source.Close()
	if err = source.QueryRowContext(ctx, "PRAGMA user_version").Scan(&result.SourceVersion); err != nil {
		return result, fmt.Errorf("SQLite DB를 읽지 못했습니다: %w", err)
	}
	if err = validateSchema(ctx, source, result.SourceVersion); err != nil {
		return result, err
	}
	if err = healthy(ctx, source); err != nil {
		return result, err
	}
	result.SourceSnapshot, err = Backup(ctx, source, i.backups, "import-source")
	if err != nil {
		return result, err
	}
	snapshot, err := connectFile(result.SourceSnapshot, "rw")
	if err != nil {
		return result, err
	}
	defer snapshot.Close()
	if _, err = Migrate(ctx, snapshot, MigrationOptions{BackupDirectory: i.backups}); err != nil {
		return result, err
	}
	data, err := allData(ctx, snapshot)
	if err != nil {
		return result, err
	}
	sourceID := sourceIdentity(data, sourcePath)
	conn, err := i.db.Conn(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Close()
	if err = checkImportIdle(ctx, conn); err != nil {
		return result, err
	}
	if err = healthy(ctx, conn); err != nil {
		return result, err
	}
	var before int64
	if err = conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&before); err != nil {
		return result, err
	}
	result.BackupPath, err = backup(ctx, conn, i.backups, "pre-import")
	if err != nil {
		return result, err
	}
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return result, err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	var after int64
	if err = conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&after); err != nil {
		return result, err
	}
	if before != after {
		return result, fmt.Errorf("백업 중 현재 DB가 변경되었습니다. 다시 시도하세요")
	}
	if err = checkImportIdle(ctx, conn); err != nil {
		return result, err
	}
	merger := merge{ctx: ctx, q: conn, data: data, source: sourceID, sourceDir: filepath.Dir(sourcePath), targetDir: filepath.Dir(i.filename), result: &result, maps: map[string]map[int64]int64{}, newMedia: map[int64]bool{}, changed: map[int64]bool{}}
	if err = merger.run(); err != nil {
		return result, err
	}
	if err = healthy(ctx, conn); err != nil {
		return result, err
	}
	if err = rebuildFTS(ctx, conn); err != nil {
		return result, err
	}
	// Keep a durable identity without altering the current v3 schema.
	if _, err = conn.ExecContext(ctx, `INSERT INTO legacy_record(table_name,row_key,payload_json) VALUES('_asterodia_database','identity',COALESCE((SELECT payload_json FROM legacy_record WHERE table_name=? AND row_key='identity'),?)) ON CONFLICT DO NOTHING`, legacyDatabaseIdentity, jsonValue(record{"id": uuid.NewString()})); err != nil {
		return result, err
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return result, err
	}
	return result, nil
}
func checkImportIdle(ctx context.Context, q connection) error {
	var active int
	if err := q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM download_queue WHERE state IN ('pending','running') OR cleanup_required=1)+(SELECT count(*) FROM maintenance_task WHERE state IN ('pending','running'))+(SELECT count(*) FROM local_import_job WHERE state IN ('scanning','pending','running') OR cleanup_required=1)+(SELECT count(*) FROM file_move_item WHERE state IN ('pending','copying','verified','committed','cleanup_pending'))`).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return fmt.Errorf("다운로드·가져오기·파일 이동·정리 작업을 마친 뒤 DB를 가져오세요")
	}
	if err := q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM media_edit_session WHERE state IN ('pending','preparing','stop_unconfirmed'))+(SELECT count(*) FROM media_edit_job WHERE state IN ('pending','running','verifying','publishing','stop_unconfirmed'))`).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return fmt.Errorf("미디어 편집 작업을 마친 뒤 DB를 가져오세요")
	}
	return nil
}
func sourceIdentity(data map[string][]record, path string) string {
	for _, row := range data["legacy_record"] {
		if isDatabaseIdentity(textValue(row["table_name"])) && row["row_key"] == "identity" {
			var identity struct {
				ID string `json:"id"`
			}
			if json.Unmarshal([]byte(textValue(row["payload_json"])), &identity) == nil && identity.ID != "" {
				return digest([]string{"database", identity.ID})
			}
		}
	}
	for _, row := range data["migration_run"] {
		// Preserve import identities of Wails databases converted before 2.1.0.
		// This does not enable opening or converting Electron database formats.
		if row["target_version"] == int64(2) && row["source_fingerprint"] != nil {
			return digest([]any{"electron-v2", row["source_fingerprint"]})
		}
	}
	return digest([]string{"source-path", pathIdentity(path, filepath.Dir(path))})
}
func pathIdentity(path, base string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return strings.ToLower(filepath.Clean(path))
}

type merge struct {
	ctx               context.Context
	q                 connection
	data              map[string][]record
	source, sourceDir string
	result            *ImportResult
	maps              map[string]map[int64]int64
	newMedia, changed map[int64]bool
	targetDir         string
	sourceFiles       map[int64][]record
	targetPaths       map[string]map[int64]bool
}

func (m *merge) remap(row record, column, table string) error {
	if row[column] == nil {
		return nil
	}
	old, e := integer(row[column])
	if e != nil {
		return e
	}
	value, ok := m.maps[table][old]
	if !ok {
		return fmt.Errorf("가져온 %s 참조를 연결하지 못했습니다: %v", table, old)
	}
	row[column] = value
	return nil
}
func (m *merge) remember(table string, old, target int64, identity string) error {
	if m.maps[table] == nil {
		m.maps[table] = map[int64]int64{}
	}
	m.maps[table][old] = target
	_, err := m.q.ExecContext(m.ctx, `INSERT INTO legacy_record(table_name,row_key,payload_json) VALUES(?,?,?) ON CONFLICT(table_name,row_key) DO UPDATE SET payload_json=excluded.payload_json`, "import-map:"+m.source+":"+table, fmt.Sprint(old), jsonValue(record{"target": target, "identity": identity}))
	return err
}
func (m *merge) previous(table string, old int64, identity string) (int64, error) {
	var payload string
	err := m.q.QueryRowContext(m.ctx, "SELECT payload_json FROM legacy_record WHERE table_name=? AND row_key=?", "import-map:"+m.source+":"+table, fmt.Sprint(old)).Scan(&payload)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var value struct {
		Target   int64  `json:"target"`
		Identity string `json:"identity"`
	}
	if json.Unmarshal([]byte(payload), &value) != nil {
		return 0, fmt.Errorf("가져오기 연결 기록이 손상되었습니다")
	}
	if value.Identity != identity {
		return 0, nil
	}
	var exists bool
	err = m.q.QueryRowContext(m.ctx, "SELECT EXISTS(SELECT 1 FROM "+quote(table)+" WHERE id=?)", value.Target).Scan(&exists)
	if err != nil {
		return 0, err
	}
	if exists {
		return value.Target, nil
	}
	return 0, nil
}
func (m *merge) run() error {
	// IDs may not reflect the user's file order, especially after earlier merges.
	type orderedFile struct {
		row                  record
		parent, position, id int64
	}
	ordered := make([]orderedFile, 0, len(m.data["media_file"]))
	for _, row := range m.data["media_file"] {
		parent, e := integer(row["media_id"])
		if e != nil {
			return e
		}
		position, e := integer(row["position"])
		if e != nil {
			return e
		}
		id, e := integer(row["id"])
		if e != nil {
			return e
		}
		ordered = append(ordered, orderedFile{row, parent, position, id})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].parent != ordered[j].parent {
			return ordered[i].parent < ordered[j].parent
		}
		if ordered[i].position != ordered[j].position {
			return ordered[i].position < ordered[j].position
		}
		return ordered[i].id < ordered[j].id
	})
	for i := range ordered {
		m.data["media_file"][i] = ordered[i].row
	}
	m.sourceFiles = map[int64][]record{}
	for _, file := range m.data["media_file"] {
		id, e := integer(file["media_id"])
		if e != nil {
			return e
		}
		m.sourceFiles[id] = append(m.sourceFiles[id], file)
	}
	for _, parent := range m.data["media"] {
		id, e := integer(parent["id"])
		if e != nil {
			return e
		}
		if len(m.sourceFiles[id]) == 0 && !m.isUserAlbum(id) {
			return fmt.Errorf("파일이 없는 묶음은 가져올 수 없습니다")
		}
	}
	m.targetPaths = map[string]map[int64]bool{}
	paths, err := records(m.ctx, m.q, "SELECT media_id,filepath FROM media_file")
	if err != nil {
		return err
	}
	for _, file := range paths {
		key := pathIdentity(textValue(file["filepath"]), m.targetDir)
		if m.targetPaths[key] == nil {
			m.targetPaths[key] = map[int64]bool{}
		}
		m.targetPaths[key][file["media_id"].(int64)] = true
	}
	// Preserve every source row as inert provenance. Source cleanup/import jobs
	// must never become executable against the selected DB's original files.
	for table, rows := range m.data {
		for _, row := range rows {
			name, key, payload := "import:"+m.source+":"+table, rowKey(row), jsonValue(row)
			if table == "legacy_record" {
				original := textValue(row["table_name"])
				if strings.HasPrefix(original, "import-map:") || isDatabaseIdentity(original) {
					continue
				}
				name = "import-origin:" + m.source + ":" + original
				if strings.HasPrefix(original, "import:") || strings.HasPrefix(original, "import-origin:") {
					name = original
				}
				key = textValue(row["row_key"])
				payload = textValue(row["payload_json"])
			}
			if _, err := m.q.ExecContext(m.ctx, `INSERT INTO legacy_record(table_name,row_key,payload_json) VALUES(?,?,?) ON CONFLICT(table_name,row_key) DO UPDATE SET payload_json=excluded.payload_json`, name, key, payload); err != nil {
				return err
			}
		}
	}
	for _, table := range []string{"platform", "tag", "download_urls", "profiles"} {
		if err := m.lookupRows(table); err != nil {
			return err
		}
	}
	if err := m.directories(); err != nil {
		return err
	}
	for _, row := range m.data["media"] {
		if err := m.parent(row); err != nil {
			return err
		}
	}
	if err := m.localFolders(); err != nil {
		return err
	}
	if err := m.directoryDefaults(); err != nil {
		return err
	}
	for _, row := range m.data["media_file"] {
		if err := m.file(row); err != nil {
			return err
		}
	}
	// Album membership follows remapped file IDs and never imports another physical copy.
	for _, r := range m.data["user_album"] {
		old, e := integer(r["media_id"])
		if e != nil {
			return e
		}
		target := m.maps["media"][old]
		if !m.newMedia[target] {
			continue
		}
		row := clone(r)
		row["media_id"] = target
		if e = m.remap(row, "cover_file_id", "media_file"); e != nil {
			return e
		}
		if _, e = insert(m.ctx, m.q, "user_album", row); e != nil {
			return e
		}
	}
	for _, r := range m.data["user_album_file"] {
		old, e := integer(r["album_id"])
		if e != nil {
			return e
		}
		if !m.newMedia[m.maps["media"][old]] {
			continue
		}
		row := clone(r)
		if e = m.remap(row, "album_id", "media"); e != nil {
			return e
		}
		if e = m.remap(row, "file_id", "media_file"); e != nil {
			return e
		}
		if _, e = insert(m.ctx, m.q, "user_album_file", row); e != nil {
			return e
		}
	}
	// All albums exist before hierarchy links are restored. Re-imports preserve
	// the user's current placement of already known albums.
	for _, r := range m.data["album_parent"] {
		old, e := integer(r["album_id"])
		if e != nil {
			return e
		}
		if !m.newMedia[m.maps["media"][old]] {
			continue
		}
		row := clone(r)
		if e = m.remap(row, "album_id", "media"); e != nil {
			return e
		}
		if e = m.remap(row, "parent_id", "media"); e != nil {
			return e
		}
		if _, e = insert(m.ctx, m.q, "album_parent", row); e != nil {
			return e
		}
	}
	for _, r := range m.data["gallery_pin"] {
		row := clone(r)
		if err := m.remap(row, "media_id", "media"); err != nil {
			return err
		}
		if err := m.remap(row, "file_id", "media_file"); err != nil {
			return err
		}
		var owner int64
		if row["media_id"] != nil {
			owner, err = integer(row["media_id"])
		} else {
			err = m.q.QueryRowContext(m.ctx, "SELECT media_id FROM media_file WHERE id=?", row["file_id"]).Scan(&owner)
		}
		if err != nil {
			return err
		}
		// Preserve local choices when importing an already known group again.
		if m.newMedia[owner] {
			if _, err = m.q.ExecContext(m.ctx, "INSERT INTO gallery_pin(media_id,file_id,created_at) VALUES(?,?,?) ON CONFLICT DO NOTHING", row["media_id"], row["file_id"], row["created_at"]); err != nil {
				return err
			}
		}
	}
	for _, table := range []string{"media_tag", "favorite"} {
		for _, r := range m.data[table] {
			row := clone(r)
			old, e := integer(row["media_id"])
			if e != nil {
				return e
			}
			if !m.newMedia[m.maps["media"][old]] {
				continue
			}
			delete(row, "id")
			if e = m.remap(row, "media_id", "media"); e != nil {
				return e
			}
			if table == "media_tag" {
				if e = m.remap(row, "tag_id", "tag"); e != nil {
					return e
				}
			}
			if _, e = insert(m.ctx, m.q, table, row); e != nil {
				return e
			}
		}
	}
	for _, row := range m.data["media"] {
		old, e := integer(row["id"])
		if e != nil {
			return e
		}
		target := m.maps["media"][old]
		if m.newMedia[target] && row["cover_file_id"] != nil {
			cover, e := integer(row["cover_file_id"])
			if e != nil {
				return e
			}
			id, ok := m.maps["media_file"][cover]
			if !ok {
				return fmt.Errorf("가져온 표지 파일을 연결하지 못했습니다")
			}
			if _, e = m.q.ExecContext(m.ctx, "UPDATE media SET cover_mode='manual',cover_file_id=? WHERE id=?", id, target); e != nil {
				return e
			}
		}
	}
	for id := range m.changed {
		if !m.newMedia[id] {
			if _, e := m.q.ExecContext(m.ctx, "UPDATE media SET revision=revision+1,updated_at=? WHERE id=?", time.Now().UnixMilli(), id); e != nil {
				return e
			}
		}
	}
	for _, row := range m.data["instagram_profile_album"] {
		old, err := integer(row["media_id"])
		if err != nil {
			return err
		}
		if _, err = m.q.ExecContext(m.ctx, "INSERT INTO instagram_profile_album(owner_id,media_id) VALUES(?,?) ON CONFLICT DO NOTHING", row["owner_id"], m.maps["media"][old]); err != nil {
			return err
		}
	}
	if err := media.SyncInstagramProfileAlbums(m.ctx, m.q, 0); err != nil {
		return err
	}
	return m.history()
}
func (m *merge) lookupRows(table string) error {
	m.maps[table] = map[int64]int64{}
	for _, r := range m.data[table] {
		row := clone(r)
		old, e := integer(row["id"])
		if e != nil {
			return e
		}
		delete(row, "id")
		var target int64
		var err error
		switch table {
		case "platform", "tag":
			err = m.q.QueryRowContext(m.ctx, "SELECT id FROM "+quote(table)+" WHERE name=?", row["name"]).Scan(&target)
		case "download_urls":
			key, e := normalizedURL(row["url"])
			if e != nil {
				return e
			}
			if row["normalized_url"] != key || row["normalization_version"] != int64(1) {
				return fmt.Errorf("지원하지 않는 URL 정규화 정보입니다")
			}
			err = m.q.QueryRowContext(m.ctx, "SELECT id FROM download_urls WHERE normalized_url=? AND normalization_version=1", key).Scan(&target)
		case "profiles":
			if e = m.remap(row, "platform_id", "platform"); e != nil {
				return e
			}
			err = m.q.QueryRowContext(m.ctx, "SELECT id FROM profiles WHERE owner_id=? AND platform_id=?", row["owner_id"], row["platform_id"]).Scan(&target)
		}
		if err == sql.ErrNoRows {
			target, err = insert(m.ctx, m.q, table, row)
		}
		if err != nil {
			return err
		}
		m.maps[table][old] = target
	}
	return nil
}
func (m *merge) parent(r record) error {
	old, err := integer(r["id"])
	if err != nil {
		return err
	}
	kind := textValue(r["source_kind"])
	identity := ""
	var target int64
	// A profile album has a global account identity across imported databases.
	for _, album := range m.data["instagram_profile_album"] {
		if album["media_id"] != old {
			continue
		}
		err = m.q.QueryRowContext(m.ctx, "SELECT media_id FROM instagram_profile_album WHERE owner_id=?", album["owner_id"]).Scan(&target)
		if err == nil {
			return m.remember("media", old, target, digest([]any{"instagram", album["owner_id"]}))
		}
		if err != sql.ErrNoRows {
			return err
		}
	}
	switch kind {
	case "download":
		urlID, e := integer(r["request_url_id"])
		if e != nil {
			return e
		}
		newURL, ok := m.maps["download_urls"][urlID]
		if !ok {
			return fmt.Errorf("묶음 URL 참조가 없습니다")
		}
		identity = digest([]any{kind, newURL})
		err = m.q.QueryRowContext(m.ctx, "SELECT id FROM media WHERE request_url_id=?", newURL).Scan(&target)
	case "local":
		// A directory is a shared location, never the identity of a post/group.
		identity = digest([]any{kind, old})
		target, err = m.previous("media", old, identity)
		if err == nil && target == 0 {
			// v3/v4 remembered the folder spelling as the import-map identity.
			// Reuse that explicit provenance without inferring group ownership
			// merely because another group happens to occupy the same directory.
			for _, location := range m.data["local_folder_location"] {
				if location["media_id"] == old && location["state"] != "history" {
					target, err = m.previous("media", old, pathIdentity(textValue(location["directory_path"]), m.sourceDir))
					if err != nil || target != 0 {
						break
					}
				}
			}
		}
		if err == nil && target == 0 {
			candidate := int64(0)
			for _, file := range m.sourceFiles[old] {
				var owner int64
				e := m.q.QueryRowContext(m.ctx, "SELECT media_id FROM media_file WHERE file_uuid=?", file["file_uuid"]).Scan(&owner)
				if e != nil && e != sql.ErrNoRows {
					return e
				}
				if e == nil {
					if candidate != 0 && candidate != owner {
						return fmt.Errorf("가져온 묶음의 파일 식별자가 여러 묶음에 나뉘어 있습니다")
					}
					candidate = owner
				}
			}
			target = candidate
		}
	case "legacy", "edited":
		paths := []string{}
		for _, f := range m.sourceFiles[old] {
			paths = append(paths, pathIdentity(textValue(f["filepath"]), m.sourceDir))
		}
		if len(paths) == 0 {
			return fmt.Errorf("파일이 없는 묶음은 가져올 수 없습니다")
		}
		identity = digest(paths)
		target, err = m.previous("media", old, identity)
		if err == nil && target == 0 {
			candidates := map[int64]bool{}
			for _, path := range paths {
				for id := range m.targetPaths[path] {
					candidates[id] = true
				}
			}
			if len(candidates) > 1 {
				return fmt.Errorf("같은 파일 경로가 여러 묶음에 있어 자동 병합할 수 없습니다")
			}
			for id := range candidates {
				target = id
			}
		}
	default:
		return fmt.Errorf("지원하지 않는 묶음 종류입니다")
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if target == 0 {
		row := clone(r)
		delete(row, "id")
		row["cover_mode"] = "auto"
		row["cover_file_id"] = nil
		row["last_download_commit_attempt_id"] = nil
		if err = m.remap(row, "request_url_id", "download_urls"); err != nil {
			return err
		}
		target, err = insert(m.ctx, m.q, "media", row)
		if err != nil {
			return err
		}
		m.newMedia[target] = true
		m.result.AddedMedia++
	} else {
		var targetKind string
		if err = m.q.QueryRowContext(m.ctx, "SELECT source_kind FROM media WHERE id=?", target).Scan(&targetKind); err != nil {
			return err
		}
		if (kind == "local") != (targetKind == "local") {
			return fmt.Errorf("보관 방식이 다른 묶음은 자동 병합할 수 없습니다")
		}
		m.result.SkippedMedia++
	}
	return m.remember("media", old, target, identity)
}
func (m *merge) localFolders() error {
	m.maps["local_folder"] = map[int64]int64{}
	m.maps["local_folder_location"] = map[int64]int64{}
	for _, r := range m.data["local_folder"] {
		row := clone(r)
		old, e := integer(row["id"])
		if e != nil {
			return e
		}
		delete(row, "id")
		if e = m.remap(row, "media_id", "media"); e != nil {
			return e
		}
		var id int64
		e = m.q.QueryRowContext(m.ctx, "SELECT id FROM local_folder WHERE media_id=?", row["media_id"]).Scan(&id)
		if e == sql.ErrNoRows {
			id, e = insert(m.ctx, m.q, "local_folder", row)
		}
		if e != nil {
			return e
		}
		m.maps["local_folder"][old] = id
	}
	for _, r := range m.data["local_folder_location"] {
		row := clone(r)
		old, e := integer(row["id"])
		if e != nil {
			return e
		}
		delete(row, "id")
		if e = m.remap(row, "media_id", "media"); e != nil {
			return e
		}
		if e = m.remap(row, "local_folder_id", "local_folder"); e != nil {
			return e
		}
		var id, owner int64
		if e = m.remap(row, "directory_id", "media_directory"); e != nil {
			return e
		}
		e = m.q.QueryRowContext(m.ctx, "SELECT id,media_id FROM local_folder_location WHERE media_id=? AND directory_key=? AND state=?", row["media_id"], row["directory_key"], row["state"]).Scan(&id, &owner)
		if e == nil && owner != row["media_id"] {
			return fmt.Errorf("다른 묶음이 사용 중인 로컬 폴더입니다")
		}
		if e == sql.ErrNoRows {
			id, e = insert(m.ctx, m.q, "local_folder_location", row)
		}
		if e != nil {
			return e
		}
		m.maps["local_folder_location"][old] = id
	}
	return nil
}
func (m *merge) file(r record) error {
	old, err := integer(r["id"])
	if err != nil {
		return err
	}
	row := clone(r)
	delete(row, "id")
	for _, ref := range []struct{ column, table string }{{"media_id", "media"}, {"legacy_url_id", "download_urls"}, {"platform_id", "platform"}, {"profile_id", "profiles"}, {"local_folder_location_id", "local_folder_location"}, {"directory_id", "media_directory"}} {
		if err = m.remap(row, ref.column, ref.table); err != nil {
			return err
		}
	}
	parent := row["media_id"].(int64)
	sourcePath := textValue(row["filepath"])
	if sourcePath == "" {
		return fmt.Errorf("파일 경로가 비어 있습니다")
	}
	if !filepath.IsAbs(sourcePath) {
		sourcePath = filepath.Join(m.sourceDir, sourcePath)
	}
	row["filepath"] = filepath.Clean(sourcePath)
	if row["directory_id"] == nil {
		directory, e := media.NewStore(m.q).RecordedDirectory(m.ctx, filepath.Dir(sourcePath), false)
		if e != nil {
			return e
		}
		row["directory_id"] = directory
	}
	if row["storage_kind"] == "app_managed" {
		row["storage_kind"] = "unverified_legacy"
	}
	if strings.HasPrefix(textValue(row["file_key"]), "legacy-media:") {
		row["file_key"] = "legacy-import:" + digest(pathIdentity(sourcePath, m.sourceDir))
	}
	matches, e := records(m.ctx, m.q, "SELECT id,file_key,filepath,content_hash,filesize FROM media_file WHERE media_id=?", parent)
	if e != nil {
		return e
	}
	for _, existing := range matches {
		samePath := pathIdentity(textValue(existing["filepath"]), m.targetDir) == pathIdentity(sourcePath, m.sourceDir)
		sameKey := existing["file_key"] == row["file_key"]
		if !samePath && !sameKey {
			continue
		}
		if existing["content_hash"] != nil && row["content_hash"] != nil && existing["content_hash"] != row["content_hash"] {
			return fmt.Errorf("같은 파일 식별자의 내용이 달라 자동 병합할 수 없습니다")
		}
		if samePath && existing["filesize"] != nil && row["filesize"] != nil && existing["filesize"] != row["filesize"] {
			return fmt.Errorf("같은 경로의 파일 크기가 달라 자동 병합할 수 없습니다")
		}
		m.result.SkippedFiles++
		return m.remember("media_file", old, existing["id"].(int64), digest([]any{parent, row["file_key"], pathIdentity(sourcePath, m.sourceDir)}))
	}
	var next int64
	if err = m.q.QueryRowContext(m.ctx, "SELECT coalesce(max(position),-1)+1 FROM media_file WHERE media_id=?", parent).Scan(&next); err != nil {
		return err
	}
	row["position"] = next
	id, err := insert(m.ctx, m.q, "media_file", row)
	if err != nil {
		return err
	}
	m.result.AddedFiles++
	m.changed[parent] = true
	if info, e := os.Stat(sourcePath); e != nil || !info.Mode().IsRegular() {
		m.result.MissingFiles++
	}
	return m.remember("media_file", old, id, digest([]any{parent, row["file_key"], pathIdentity(sourcePath, m.sourceDir)}))
}

func (m *merge) directories() error {
	m.maps["media_directory"] = map[int64]int64{}
	for _, row := range m.data["media_directory"] {
		path := textValue(row["directory_path"])
		if !filepath.IsAbs(path) {
			path = filepath.Join(m.sourceDir, path)
		}
		version, e := integer(row["key_version"])
		if e != nil {
			return e
		}
		registered, e := integer(row["registered"])
		if e != nil {
			return e
		}
		id, e := media.NewStore(m.q).EnsureDirectory(m.ctx, path, textValue(row["directory_key"]), int(version), registered == 1)
		if e != nil {
			return e
		}
		old, e := integer(row["id"])
		if e != nil {
			return e
		}
		m.maps["media_directory"][old] = id
	}
	return nil
}
func (m *merge) directoryDefaults() error {
	for _, row := range m.data["media_directory"] {
		if row["import_media_id"] == nil {
			continue
		}
		old, e := integer(row["id"])
		if e != nil {
			return e
		}
		owner, e := integer(row["import_media_id"])
		if e != nil {
			return e
		}
		if _, e = m.q.ExecContext(m.ctx, "UPDATE media_directory SET import_media_id=? WHERE id=? AND import_media_id IS NULL", m.maps["media"][owner], m.maps["media_directory"][old]); e != nil {
			return e
		}
	}
	return nil
}
func (m *merge) history() error {
	storage := map[string]any{}
	for _, r := range m.data["download_job_storage"] {
		storage[textValue(r["job_id"])] = r["storage_root"]
	}
	for _, r := range m.data["download_queue"] {
		row := clone(r)
		id := "import:" + digest([]any{r["id"], r["original_url"], r["created_at"]})
		var flags struct {
			Imported bool `json:"_importedHistory"`
		}
		_ = json.Unmarshal([]byte(textValue(r["options_json"])), &flags)
		if original := textValue(r["id"]); flags.Imported && strings.HasPrefix(original, "import:") {
			id = original
		}
		var exists bool
		if err := m.q.QueryRowContext(m.ctx, "SELECT EXISTS(SELECT 1 FROM download_queue WHERE id=?)", id).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		row["id"] = id
		if err := m.remap(row, "request_url_id", "download_urls"); err != nil {
			return err
		}
		if r["target_media_id"] != nil {
			if err := m.remap(row, "target_media_id", "media"); err != nil {
				return err
			}
		}
		row["target_media_id_snapshot"] = row["target_media_id"]
		row["current_attempt_id"] = nil
		row["cleanup_required"] = 0
		row["recovery_enabled"] = 0
		if row["state"] == "pending" || row["state"] == "running" {
			row["state"] = "interrupted"
		}
		options := map[string]any{}
		_ = json.Unmarshal([]byte(textValue(row["options_json"])), &options)
		if options == nil {
			options = map[string]any{}
		}
		options["_importedHistory"] = true
		row["options_json"] = jsonValue(options)
		if _, err := insert(m.ctx, m.q, "download_queue", row); err != nil {
			return err
		}
		root := storage[textValue(r["id"])]
		if root == nil {
			root = filepath.Join(m.sourceDir, "downloads")
		}
		if _, err := insert(m.ctx, m.q, "download_job_storage", record{"job_id": id, "storage_root": root, "created_at": time.Now().UnixMilli()}); err != nil {
			return err
		}
		m.result.AddedHistory++
	}
	return nil
}
func rebuildFTS(ctx context.Context, q connection) error {
	if !tableExists(ctx, q, "media_fts") {
		return nil
	}
	_, err := q.ExecContext(ctx, `DELETE FROM media_fts; INSERT INTO media_fts(rowid,title,owner_name,url,tag_names) SELECT m.id,coalesce(m.custom_title,m.source_title,''),coalesce((SELECT group_concat(p.owner_name,' ') FROM media_file f JOIN profiles p ON p.id=f.profile_id WHERE f.media_id=m.id),''),coalesce(u.url,''),coalesce((SELECT group_concat(t.name,' ') FROM media_tag mt JOIN tag t ON t.id=mt.tag_id WHERE mt.media_id=m.id),'') FROM media m LEFT JOIN download_urls u ON u.id=m.request_url_id`)
	return err
}

func (m *merge) isUserAlbum(id int64) bool {
	for _, r := range m.data["user_album"] {
		if r["media_id"] == id {
			return true
		}
	}
	return false
}
