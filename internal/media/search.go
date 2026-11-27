package media

import (
	"context"
	"database/sql"
	"strings"
	"unicode/utf8"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
)

type SearchSuggestRequest struct {
	Text          string `json:"text"`
	Kind          string `json:"kind"`
	Limit         int    `json:"limit"`
	Offset        int    `json:"offset"`
	Filter        Filter `json:"filter"`
	FavoritesOnly bool   `json:"favoritesOnly"`
}

func (s *Service) SearchSuggest(ctx context.Context, req SearchSuggestRequest) api.Result {
	if req.Limit == 0 {
		req.Limit = 10
	}
	if req.Limit < 1 || req.Limit > 50 || req.Offset < 0 || len(req.Text) > 1000 || !utf8.ValidString(req.Text) || strings.ContainsRune(req.Text, 0) {
		return failure(domainError("INVALID_ARGUMENT"))
	}
	kinds := "'title','tag','author'"
	if req.Kind != "" {
		switch req.Kind {
		case "title", "tag", "author", "platform":
			kinds = "'" + req.Kind + "'"
		default:
			return failure(domainError("INVALID_ARGUMENT"))
		}
	}
	// The typed keyword is the suggestion query, not an additional gallery constraint.
	req.Filter.Keyword = ""
	req.Filter = s.visibleFilter(req.Filter)
	where, filterArgs, err := filterSQL(ListRequest{Filter: req.Filter, FavoritesOnly: req.FavoritesOnly})
	if err != nil {
		return failure(err)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return failure(err)
	}
	defer tx.Rollback()
	var revision int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM search_state WHERE id=1").Scan(&revision); err != nil {
		return failure(err)
	}
	ready, err := searchReady(ctx, tx)
	if err != nil {
		return failure(err)
	}
	rows := []Object{}
	if strings.TrimSpace(req.Text) != "" {
		var query string
		var args []any
		folded := searchFold(req.Text)
		if ready {
			match, values := searchTermMatch(req.Text)
			var sample int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM ("+match+" LIMIT 256)", values...).Scan(&sample); err != nil {
				return failure(err)
			}
			if sample == 256 {
				rows, err = orderedSearchSuggestions(ctx, tx, req, kinds, where, filterArgs)
				if err != nil {
					return failure(err)
				}
				return searchSuggestionResult(rows, req, revision, ready)
			}
			query = "SELECT t.kind,t.value FROM search_term t WHERE t.kind IN (" + kinds + ") AND t.id IN (" + match + ") AND instr(t.normalized,?)>0 AND EXISTS(SELECT 1 FROM search_document d JOIN media m ON m.id=d.media_id WHERE d.term_id=t.id AND " + where + ") ORDER BY CASE WHEN t.normalized=? THEN 0 WHEN instr(t.normalized,?)=1 THEN 1 ELSE 2 END,t.normalized,t.kind,t.value LIMIT ? OFFSET ?"
			args = append(args, values...)
			args = append(args, folded)
			args = append(args, filterArgs...)
			args = append(args, folded, folded, req.Limit+1, req.Offset)
		} else {
			query = "SELECT s.kind,s.value FROM search_source s JOIN media m ON m.id=s.media_id WHERE s.kind IN (" + kinds + ") AND " + where + " AND instr(lower(s.value),lower(?))>0 GROUP BY s.kind,s.value ORDER BY CASE WHEN lower(s.value)=lower(?) THEN 0 WHEN instr(lower(s.value),lower(?))=1 THEN 1 ELSE 2 END,lower(s.value),s.kind,s.value LIMIT ? OFFSET ?"
			args = append(args, filterArgs...)
			args = append(args, req.Text, req.Text, req.Text, req.Limit+1, req.Offset)
		}
		rows, err = objects(ctx, tx, query, args...)
		if err != nil {
			return failure(err)
		}
	}
	return searchSuggestionResult(rows, req, revision, ready)
}

func searchSuggestionResult(rows []Object, req SearchSuggestRequest, revision int64, ready bool) api.Result {
	hasNext := len(rows) > req.Limit
	if hasNext {
		rows = rows[:req.Limit]
	}
	return api.Success(Object{"data": rows, "offset": req.Offset, "hasNextPage": hasNext, "revision": revision, "indexed": ready})
}

// Broad queries read the normalized covering index in rank order instead of
// sorting every hit. Rare queries above retain the FTS/gram candidate lookup.
func orderedSearchSuggestions(ctx context.Context, tx *sql.Tx, req SearchSuggestRequest, kinds, where string, filterArgs []any) ([]Object, error) {
	text := searchFold(req.Text)
	upperRunes := []rune(text)
	upper := ""
	for i := len(upperRunes) - 1; i >= 0; i-- {
		if upperRunes[i] < 0x10ffff {
			upper = string(upperRunes[:i]) + string(upperRunes[i]+1)
			break
		}
	}
	rows := []Object{}
	offset := req.Offset
	for rank := 0; rank < 3; rank++ {
		clause := "t.normalized=?"
		args := []any{text}
		if rank == 1 {
			clause = "t.normalized>=? AND instr(t.normalized,?)=1 AND t.normalized<>?"
			args = []any{text, text, text}
			if upper != "" {
				clause += " AND t.normalized<?"
				args = append(args, upper)
			}
		}
		if rank == 2 {
			clause = "instr(t.normalized,?)>1"
			args = []any{text}
		}
		from := " FROM search_term t INDEXED BY idx_search_term_normalized WHERE t.kind IN (" + kinds + ") AND " + clause + " AND EXISTS(SELECT 1 FROM search_document d JOIN media m ON m.id=d.media_id WHERE d.term_id=t.id AND " + where + ")"
		args = append(args, filterArgs...)
		remaining := req.Limit + 1 - len(rows)
		params := append(append([]any{}, args...), remaining, offset)
		part, err := objects(ctx, tx, "SELECT t.kind,t.value"+from+" ORDER BY t.normalized,t.kind,t.value LIMIT ? OFFSET ?", params...)
		if err != nil {
			return nil, err
		}
		rows = append(rows, part...)
		if len(part) == remaining {
			break
		}
		if len(part) == 0 && offset > 0 && rank < 2 {
			var count int
			if err = tx.QueryRowContext(ctx, "SELECT count(*)"+from, args...).Scan(&count); err != nil {
				return nil, err
			}
			offset -= count
			if offset < 0 {
				offset = 0
			}
		} else {
			offset = 0
		}
	}
	return rows, nil
}
