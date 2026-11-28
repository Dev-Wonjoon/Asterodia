package media

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// StartSearchIndex owns one cancellable worker. Persistent dirty rows survive shutdown.
//
//wails:ignore
func (s *Service) StartSearchIndex() {
	s.searchMu.Lock()
	defer s.searchMu.Unlock()
	if s.searchCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.searchCancel = cancel
	s.searchWG.Add(1)
	go func() {
		defer s.searchWG.Done()
		for {
			work, err := s.RefreshSearchIndex(ctx, 32)
			delay := 10 * time.Millisecond
			if !work || err != nil {
				delay = 250 * time.Millisecond
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

//wails:ignore
func (s *Service) Close() {
	s.searchMu.Lock()
	if s.searchCancel != nil {
		s.searchCancel()
	}
	if s.metadataCancel != nil {
		s.metadataCancel()
	}
	s.searchMu.Unlock()
	s.searchWG.Wait()
}

func searchFold(text string) string {
	// Match SQLite lower(): preserve non-ASCII text and literal punctuation.
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, text)
}

// RefreshSearchIndex processes a bounded batch atomically; no partially indexed terms are visible.
//
//wails:ignore
func (s *Service) RefreshSearchIndex(ctx context.Context, limit int) (bool, error) {
	if limit < 1 || limit > 128 {
		return false, domainError("INVALID_ARGUMENT")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	ids, err := objects(ctx, tx, "SELECT media_id FROM search_dirty ORDER BY media_id LIMIT ?", limit)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		mediaID := id["media_id"].(int64)
		sources, err := objects(ctx, tx, "SELECT kind,value FROM search_source WHERE media_id=?", mediaID)
		if err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM search_document WHERE media_id=?", mediaID); err != nil {
			return false, err
		}
		for _, source := range sources {
			kind, value := source["kind"].(string), source["value"].(string)
			normalized := searchFold(value)
			var termID int64
			err = tx.QueryRowContext(ctx, "SELECT id FROM search_term WHERE kind=? AND value=?", kind, value).Scan(&termID)
			if err == sql.ErrNoRows {
				res, e := tx.ExecContext(ctx, "INSERT INTO search_term(kind,value,normalized) VALUES(?,?,?)", kind, value, normalized)
				if e != nil {
					return false, e
				}
				termID, e = res.LastInsertId()
				if e != nil {
					return false, e
				}
				runes := []rune(normalized)
				grams := map[string]bool{}
				for i := range runes {
					grams[string(runes[i:i+1])] = true
					if i+1 < len(runes) {
						grams[string(runes[i:i+2])] = true
					}
				}
				values, args := []string{}, []any{}
				flush := func() error {
					if len(values) == 0 {
						return nil
					}
					_, e := tx.ExecContext(ctx, "INSERT INTO search_gram(gram,term_id) VALUES "+strings.Join(values, ","), args...)
					values, args = nil, nil
					return e
				}
				for gram := range grams {
					values = append(values, "(?,?)")
					args = append(args, gram, termID)
					if len(values) == 128 {
						if e = flush(); e != nil {
							return false, e
						}
					}
				}
				if e = flush(); e != nil {
					return false, e
				}
			} else if err != nil {
				return false, err
			}
			if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO search_document VALUES(?,?)", mediaID, termID); err != nil {
				return false, err
			}
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM search_dirty WHERE media_id=?", mediaID); err != nil {
			return false, err
		}
	}
	gc, err := objects(ctx, tx, "SELECT term_id FROM search_gc ORDER BY term_id LIMIT 128")
	if err != nil {
		return false, err
	}
	for _, row := range gc {
		id := row["term_id"]
		if _, err = tx.ExecContext(ctx, "DELETE FROM search_term WHERE id=? AND NOT EXISTS(SELECT 1 FROM search_document WHERE term_id=?)", id, id); err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM search_gc WHERE term_id=?", id); err != nil {
			return false, err
		}
	}
	return len(ids) > 0 || len(gc) > 0, tx.Commit()
}

func searchReady(ctx context.Context, q queryer) (bool, error) {
	var dirty bool
	err := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM search_dirty)").Scan(&dirty)
	return !dirty, err
}

func searchTermMatch(text string) (string, []any) {
	runes := []rune(searchFold(text))
	if len(runes) < 3 {
		return "SELECT term_id FROM search_gram WHERE gram=?", []any{string(runes)}
	}
	phrase := "\"" + strings.ReplaceAll(string(runes[:3]), "\"", "\"\"") + "\""
	return "SELECT rowid FROM search_fts WHERE search_fts MATCH ?", []any{phrase}
}
