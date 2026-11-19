package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type record = map[string]any
type connection interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func quote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }
func records(ctx context.Context, q connection, query string, args ...any) ([]record, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []record{}
	for rows.Next() {
		v := make([]any, len(cols))
		p := make([]any, len(cols))
		for i := range v {
			p[i] = &v[i]
		}
		if err = rows.Scan(p...); err != nil {
			return nil, err
		}
		r := record{}
		for i, key := range cols {
			if b, ok := v[i].([]byte); ok {
				v[i] = string(b)
			}
			r[key] = v[i]
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func insert(ctx context.Context, q connection, table string, row record) (int64, error) {
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	cols, args := []string{}, []any{}
	for _, k := range keys {
		cols = append(cols, quote(k))
		args = append(args, row[k])
	}
	result, err := q.ExecContext(ctx, "INSERT INTO "+quote(table)+" ("+strings.Join(cols, ",")+") VALUES ("+strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")+")", args...)
	if err != nil {
		return 0, fmt.Errorf("%s 기록 실패: %w", table, err)
	}
	return result.LastInsertId()
}
func clone(row record) record {
	out := record{}
	for k, v := range row {
		out[k] = v
	}
	return out
}
func integer(v any) (int64, error) {
	n, ok := v.(int64)
	if !ok || n > 9007199254740991 || n < -9007199254740991 {
		return 0, fmt.Errorf("지원하지 않는 정수 값: %v", v)
	}
	return n, nil
}
func textValue(v any) string { s, _ := v.(string); return s }
func jsonValue(v any) string { b, _ := json.Marshal(v); return string(b) }
func digest(v any) string    { return fmt.Sprintf("%x", sha256.Sum256([]byte(jsonValue(v)))) }
func rowKey(row record) string {
	if v, ok := row["id"]; ok && v != nil {
		return fmt.Sprint(v)
	}
	if _, ok := row["media_id"]; ok {
		return jsonValue([]any{row["media_id"], row["tag_id"]})
	}
	if _, ok := row["table_name"]; ok {
		return jsonValue([]any{row["table_name"], row["row_key"]})
	}
	return digest(row)
}
func healthy(ctx context.Context, q connection) error {
	var integrity string
	if err := q.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("DB 무결성 검사 실패: %s", integrity)
	}
	rows, err := q.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("DB 외래 키 관계가 손상되어 있습니다")
	}
	return rows.Err()
}
func tableExists(ctx context.Context, q connection, name string) bool {
	var n int
	return q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&n) == nil && n == 1
}
