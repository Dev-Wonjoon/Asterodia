package media

import (
	"context"
	"database/sql"
	"strings"
	"unicode/utf8"
)

type PlatformChange struct {
	AddNames    []string `json:"addNames"`
	RemoveNames []string `json:"removeNames"`
}

func changePlatforms(ctx context.Context, tx *sql.Tx, ids []any, change PlatformChange) error {
	if len(change.AddNames)+len(change.RemoveNames) > 100 {
		return domainError("INVALID_ARGUMENT")
	}
	for _, names := range [][]string{change.AddNames, change.RemoveNames} {
		for _, name := range names {
			if strings.TrimSpace(name) == "" || utf8.RuneCountInString(strings.TrimSpace(name)) > 100 {
				return domainError("INVALID_ARGUMENT")
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	clause := "(" + placeholders(len(ids)) + ")"
	for _, name := range change.RemoveNames {
		if _, err := tx.ExecContext(ctx, "DELETE FROM media_platform WHERE media_id IN "+clause+" AND platform_id IN (SELECT id FROM platform WHERE name=?)", append(append([]any{}, ids...), strings.TrimSpace(name))...); err != nil {
			return err
		}
	}
	for _, name := range change.AddNames {
		name = strings.TrimSpace(name)
		if _, err := tx.ExecContext(ctx, "INSERT INTO platform(name) VALUES(?) ON CONFLICT(name) DO NOTHING", name); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO media_platform(media_id,platform_id) SELECT m.id,p.id FROM media m JOIN platform p ON p.name=? WHERE m.id IN "+clause+" ON CONFLICT DO NOTHING", append([]any{name}, ids...)...); err != nil {
			return err
		}
	}
	return nil
}
