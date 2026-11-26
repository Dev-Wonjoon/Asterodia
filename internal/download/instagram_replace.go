package download

import (
	"context"
	"database/sql"
	"strings"
)

// Move old positions aside before upserting a post, preserving file IDs/UUIDs
// and every album, pin and edit reference for unchanged Instagram media keys.
func prepareInstagramReplacement(ctx context.Context, tx *sql.Tx, mediaID int64, files []verifiedFile) (bool, error) {
	for _, file := range files {
		if !strings.EqualFold(file.metadata.PlatformName, "instagram") {
			return false, nil
		}
	}
	var other int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM media_file f LEFT JOIN platform p ON p.id=f.platform_id
 WHERE f.media_id=? AND coalesce(lower(p.name),'')<>'instagram'`, mediaID).Scan(&other); err != nil {
		return false, err
	}
	if other != 0 {
		return false, nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE media_file SET position=position+
 (SELECT coalesce(max(position),0)+?+1 FROM media_file WHERE media_id=?) WHERE media_id=?`, len(files), mediaID, mediaID)
	return true, err
}

func removeReplacedInstagramFiles(ctx context.Context, tx *sql.Tx, mediaID int64, files []verifiedFile) error {
	// The request can reorder retained files. Positions in [0,len(files)) are the
	// newly committed set; all displaced, unmatched rows are obsolete.
	if _, err := tx.ExecContext(ctx, `UPDATE media SET cover_mode='auto',cover_file_id=NULL
 WHERE id=? AND cover_file_id IN(SELECT id FROM media_file WHERE media_id=? AND position>=?)`, mediaID, mediaID, len(files)); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM media_file WHERE media_id=? AND position>=?", mediaID, len(files))
	return err
}
