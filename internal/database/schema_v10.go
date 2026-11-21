package database

import (
	"context"
	"fmt"
	"strings"
)

// v9 was built during search development with three earlier complete layouts.
// Recognize only those exact schemas; never accept arbitrary trigger edits.
func legacySearchV9Schemas() ([]string, error) {
	canonical := searchV9DDL()
	guarded := strings.ReplaceAll(canonical, "SELECT DISTINCT candidate.media_id", "SELECT candidate.media_id")
	guarded = strings.Replace(guarded, "AFTER UPDATE OF custom_title,source_title,created_at", "AFTER UPDATE OF custom_title,source_title", 1)
	initial := canonical
	for _, event := range []string{"new.id", "old.id", "new.media_id", "old.media_id"} {
		initial = strings.ReplaceAll(initial, "SELECT "+event+" AS media_id", "SELECT "+event)
	}
	initial = strings.Replace(initial, "AFTER UPDATE OF custom_title,source_title,created_at", "AFTER UPDATE OF custom_title,source_title", 1)
	initial = strings.Replace(initial, "INSERT INTO search_gc(term_id) SELECT old.term_id WHERE NOT EXISTS(SELECT 1 FROM search_gc WHERE term_id=old.term_id)", "INSERT OR IGNORE INTO search_gc VALUES(old.term_id)", 1)
	const begin = "INSERT INTO search_dirty(media_id) SELECT DISTINCT candidate.media_id FROM ("
	const end = ") candidate WHERE NOT EXISTS(SELECT 1 FROM search_dirty WHERE media_id=candidate.media_id)"
	for strings.Contains(initial, begin) {
		start := strings.Index(initial, begin)
		finish := strings.Index(initial[start+len(begin):], end)
		if finish < 0 {
			return nil, fmt.Errorf("보존된 v9 트리거 정의를 구성할 수 없습니다")
		}
		finish += start + len(begin)
		ids := initial[start+len(begin) : finish]
		initial = initial[:start] + "INSERT OR IGNORE INTO search_dirty(media_id) " + ids + initial[finish+len(end):]
	}
	oldIndex := strings.Replace(initial, "ON search_term(normalized,kind,value)", "ON search_term(normalized,kind,id)", 1)
	return []string{schemaV8SQL + initial, schemaV8SQL + oldIndex, schemaV8SQL + guarded}, nil
}

func sameSchemaObjects(actual, want map[string]string) bool {
	if len(actual) != len(want) {
		return false
	}
	for name, sql := range want {
		if actual[name] != sql {
			return false
		}
	}
	return true
}

func knownLegacySearchV9(ctx context.Context, actual map[string]string) (bool, error) {
	layouts, err := legacySearchV9Schemas()
	if err != nil {
		return false, err
	}
	for _, ddl := range layouts {
		expected, err := memoryDatabase()
		if err != nil {
			return false, err
		}
		if _, err = expected.ExecContext(ctx, ddl); err != nil {
			expected.Close()
			return false, err
		}
		want, err := schemaObjects(ctx, expected)
		expected.Close()
		if err != nil {
			return false, err
		}
		if sameSchemaObjects(actual, want) {
			return true, nil
		}
	}
	return false, nil
}

func upgradeSearchTriggers(ctx context.Context, q connection) error {
	expected, err := memoryDatabase()
	if err != nil {
		return err
	}
	defer expected.Close()
	if _, err = expected.ExecContext(ctx, schemaV9SQL); err != nil {
		return err
	}
	triggers, err := records(ctx, expected, "SELECT name,sql FROM sqlite_master WHERE type='trigger' AND name GLOB 'search_*' ORDER BY name")
	if err != nil {
		return err
	}
	for _, trigger := range triggers {
		if _, err = q.ExecContext(ctx, "DROP TRIGGER "+quote(textValue(trigger["name"]))); err != nil {
			return err
		}
		if _, err = q.ExecContext(ctx, textValue(trigger["sql"])); err != nil {
			return err
		}
	}
	if _, err = q.ExecContext(ctx, `DROP INDEX idx_search_term_normalized;
 CREATE INDEX idx_search_term_normalized ON search_term(normalized,kind,value);
 INSERT INTO search_dirty(media_id) SELECT id FROM media WHERE NOT EXISTS(SELECT 1 FROM search_dirty WHERE media_id=media.id);
 UPDATE search_state SET revision=revision+1 WHERE id=1;`); err != nil {
		return fmt.Errorf("검색 인덱스 전환 실패: %w", err)
	}
	return nil
}
