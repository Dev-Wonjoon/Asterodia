package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"github.com/google/uuid"
	whatwg "github.com/nlnwa/whatwg-url/url"
)

type MigrationOptions struct {
	BackupDirectory string
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
	err = validateSchema(ctx, conn, result.FromVersion)
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
	if result.FromVersion == 3 {
		if err = upgradeMediaReferences(ctx, conn); err != nil {
			return result, err
		}
	}
	if result.FromVersion < 5 {
		if err = upgradeDirectories(ctx, conn, options.Checkpoint); err != nil {
			return result, err
		}
	}

	if result.FromVersion < 6 {
		if err = upgradeImportCheckpoints(ctx, conn, options.Checkpoint); err != nil {
			return result, err
		}
	}
	if result.FromVersion < 7 {
		if err = upgradeMediaEdits(ctx, conn); err != nil {
			return result, err
		}
	}
	if result.FromVersion < 8 {
		if err = upgradeEditStreaming(ctx, conn); err != nil {
			return result, err
		}
	}
	now := time.Now().UnixMilli()
	if result.FromVersion < 9 {
		if err = upgradeSearch(ctx, conn); err != nil {
			return result, err
		}
	}
	if result.FromVersion == 9 {
		if err = upgradeSearchTriggers(ctx, conn); err != nil {
			return result, err
		}
		if options.Checkpoint != nil {
			if err = options.Checkpoint("after_search_trigger_repair"); err != nil {
				return result, err
			}
		}
	}
	if result.FromVersion < 11 {
		if err = upgradeAlbums(ctx, conn); err != nil {
			return result, err
		}
	}
	if result.FromVersion < 12 {
		if _, err = conn.ExecContext(ctx, galleryPinDDL); err != nil {
			return result, err
		}
	}
	if result.FromVersion < 13 {
		if _, err = conn.ExecContext(ctx, albumHierarchyDDL); err != nil {
			return result, err
		}
	}
	if result.FromVersion < 14 {
		if _, err = conn.ExecContext(ctx, instagramProfileAlbumDDL); err != nil {
			return result, err
		}
		if err = media.SyncInstagramProfileAlbums(ctx, conn, 0); err != nil {
			return result, err
		}
	}
	if _, err = insert(ctx, conn, "migration_run", record{"id": uuid.NewString(), "target_version": schemaVersion, "source_fingerprint": fingerprint, "schema_hash": hashBytes([]byte(schemaSQL)), "backup_path": result.BackupPath, "completed_at": now}); err != nil {
		return result, err
	}
	if _, err = conn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", schemaVersion)); err != nil {
		return result, err
	}
	if err = healthy(ctx, conn); err != nil {
		return result, err
	}
	if err = validateSchema(ctx, conn, schemaVersion); err != nil {
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
