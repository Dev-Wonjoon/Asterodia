package database

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

//go:embed legacy/*.sql legacy/journal.json
var legacyFiles embed.FS

type legacyMigration struct {
	SQL, Hash string
	When      int64
}

func legacyMigrations() ([]legacyMigration, error) {
	var journal struct {
		Entries []struct {
			Tag  string `json:"tag"`
			When int64  `json:"when"`
		} `json:"entries"`
	}
	b, err := legacyFiles.ReadFile("legacy/journal.json")
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &journal); err != nil {
		return nil, err
	}
	out := []legacyMigration{}
	for _, entry := range journal.Entries {
		b, e := legacyFiles.ReadFile("legacy/" + entry.Tag + ".sql")
		if e != nil {
			return nil, e
		}
		out = append(out, legacyMigration{string(b), hashBytes(b), entry.When})
	}
	return out, nil
}
func memoryDatabase() (*sql.DB, error) {
	db, e := sql.Open("sqlite", "file:"+uuid.NewString()+"?mode=memory&cache=private&_pragma=temp_store(2)")
	if e == nil {
		db.SetMaxOpenConns(1)
	}
	return db, e
}

// Normalize identifier quoting and formatting, while preserving string literals.
func sqlShape(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if unicode.IsSpace(rune(c)) || c == ';' {
			i++
			continue
		}
		if c == '-' && i+1 < len(s) && s[i+1] == '-' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && (s[i] != '*' || s[i+1] != '/') {
				i++
			}
			i += 2
			continue
		}
		if c == '\'' {
			start := i
			i++
			for i < len(s) {
				if s[i] == '\'' {
					i++
					if i < len(s) && s[i] == '\'' {
						i++
						continue
					}
					break
				}
				i++
			}
			out.WriteString(s[start:i])
			continue
		}
		if c == '"' || c == '`' || c == '[' {
			end := c
			if c == '[' {
				end = ']'
			}
			i++
			for i < len(s) && s[i] != end {
				out.WriteByte(byte(unicode.ToLower(rune(s[i]))))
				i++
			}
			i++
			continue
		}
		out.WriteByte(byte(unicode.ToLower(rune(c))))
		i++
	}
	return out.String()
}
func schemaObjects(ctx context.Context, q connection) (map[string]string, error) {
	rows, err := records(ctx, q, "SELECT type,name,sql FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name")
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	fts := tableExists(ctx, q, "media_fts")
	for _, row := range rows {
		name := textValue(row["name"])
		if name == "__drizzle_migrations" {
			continue
		}
		if fts && (name == "media_fts" || strings.HasPrefix(name, "media_fts_")) && row["type"] == "table" {
			if name == "media_fts" && sqlShape(textValue(row["sql"])) != sqlShape("CREATE VIRTUAL TABLE media_fts USING fts5(title, owner_name, url, tag_names)") {
				return nil, fmt.Errorf("지원하지 않는 검색 인덱스입니다")
			}
			continue
		}
		if isSearchFTSTable(name) {
			if name == "search_fts" && sqlShape(textValue(row["sql"])) != sqlShape("CREATE VIRTUAL TABLE search_fts USING fts5(normalized,content='search_term',content_rowid='id',tokenize='trigram',detail=none)") {
				return nil, fmt.Errorf("지원하지 않는 검색 인덱스입니다")
			}
			if name != "search_fts" {
				continue
			}
		}
		out[textValue(row["type"])+":"+name] = sqlShape(textValue(row["sql"]))
	}
	return out, nil
}

func isFTSTable(name string) bool {
	switch name {
	case "media_fts", "media_fts_data", "media_fts_idx", "media_fts_content", "media_fts_docsize", "media_fts_config":
		return true
	}
	return false
}

func isSearchFTSTable(name string) bool {
	switch name {
	case "search_fts", "search_fts_data", "search_fts_idx", "search_fts_docsize", "search_fts_config":
		return true
	}
	return false
}
func baseSchema() string {
	start := strings.Index(schemaV3SQL, "CREATE TABLE download_job_storage")
	if start < 0 {
		return schemaV3SQL
	}
	return schemaV3SQL[:start]
}
func validateSchema(ctx context.Context, q connection, version int) (int, error) {
	expected, err := memoryDatabase()
	if err != nil {
		return 0, err
	}
	defer expected.Close()
	count := 0
	if version == 0 || version == 1 {
		if !tableExists(ctx, q, "__drizzle_migrations") {
			return 0, fmt.Errorf("구버전 DB의 Drizzle 이력이 없습니다")
		}
		history, e := records(ctx, q, "SELECT hash,created_at FROM __drizzle_migrations ORDER BY created_at")
		if e != nil {
			return 0, e
		}
		migrations, e := legacyMigrations()
		if e != nil {
			return 0, e
		}
		if len(history) == 0 || len(history) > len(migrations) {
			return 0, fmt.Errorf("지원하지 않는 구버전 이력입니다")
		}
		for i, row := range history {
			n, e := integer(row["created_at"])
			if e != nil || !migrationHashMatches(textValue(row["hash"]), migrations[i]) || n != migrations[i].When {
				return 0, fmt.Errorf("구버전 DB의 이력이 배포된 마이그레이션과 다릅니다")
			}
			if _, e = expected.ExecContext(ctx, migrations[i].SQL); e != nil {
				return 0, e
			}
		}
		count = len(history)
	} else if version >= 2 && version <= schemaVersion {
		ddl := schemaV3SQL
		if version == 4 {
			ddl = schemaV4SQL
		}
		if version == 5 {
			ddl = schemaV5SQL
		}
		if version == 6 {
			ddl = schemaV6SQL
		}
		if version == 7 {
			ddl = schemaV7SQL
		}
		if version == 8 {
			ddl = schemaV8SQL
		}
		if version == 9 {
			ddl = schemaV9SQL
		}
		if version == 10 {
			ddl = schemaSQL
		}
		if version == 2 {
			ddl = baseSchema()
		}
		if _, err = expected.ExecContext(ctx, ddl); err != nil {
			return 0, err
		}
	} else {
		return 0, fmt.Errorf("지원하지 않는 DB 버전 %d입니다", version)
	}
	actual, e := schemaObjects(ctx, q)
	if e != nil {
		return 0, e
	}
	want, e := schemaObjects(ctx, expected)
	if e != nil {
		return 0, e
	}
	if version == 9 && !sameSchemaObjects(actual, want) {
		known, err := knownLegacySearchV9(ctx, actual)
		if err != nil {
			return 0, err
		}
		if known {
			return count, nil
		}
	}
	if len(actual) != len(want) {
		return 0, fmt.Errorf("DB 버전 %d의 테이블·인덱스 구성이 알려진 스키마와 다릅니다", version)
	}
	for name, sql := range want {
		if actual[name] != sql {
			return 0, fmt.Errorf("DB 구조를 확인할 수 없습니다: %s", name)
		}
	}
	return count, nil
}

func migrationHashMatches(hash string, migration legacyMigration) bool {
	lf := strings.ReplaceAll(migration.SQL, "\r\n", "\n")
	return hash == migration.Hash || hash == hashBytes([]byte(lf)) || hash == hashBytes([]byte(strings.ReplaceAll(lf, "\n", "\r\n")))
}
func allData(ctx context.Context, q connection) (map[string][]record, error) {
	tables, err := records(ctx, q, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return nil, err
	}
	data := map[string][]record{}
	for _, table := range tables {
		name := textValue(table["name"])
		if isFTSTable(name) || strings.HasPrefix(name, "search_") {
			continue
		}
		rows, e := records(ctx, q, "SELECT * FROM "+quote(name))
		if e != nil {
			return nil, e
		}
		sort.Slice(rows, func(i, j int) bool { return rowKey(rows[i]) < rowKey(rows[j]) })
		data[name] = rows
	}
	return data, nil
}
