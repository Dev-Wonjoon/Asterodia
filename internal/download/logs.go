package download

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

const maxLogBytes = 1 << 20
const maxLogEntries = 2000
const maxLogLine = 8 << 10
const maxPendingLogs = 512

type LogEntry struct {
	Sequence   int64  `json:"sequence"`
	AttemptID  string `json:"attemptId"`
	RecordedAt *int64 `json:"recordedAt"`
	Engine     string `json:"engine"`
	Stream     string `json:"stream"`
	Text       string `json:"text"`
}
type LogRequest struct {
	JobID     string `json:"jobId"`
	AttemptID string `json:"attemptId"`
	Before    int64  `json:"before"`
	Limit     int    `json:"limit"`
}
type LogAttempt struct {
	ID      string  `json:"id"`
	Number  int     `json:"number"`
	Outcome *string `json:"outcome"`
}
type LogPage struct {
	Entries          []LogEntry   `json:"entries"`
	Attempts         []LogAttempt `json:"attempts"`
	CurrentAttemptID *string      `json:"currentAttemptId"`
	State            string       `json:"state"`
	HasOlder         bool         `json:"hasOlder"`
	Truncated        bool         `json:"truncated"`
	Warning          *string      `json:"warning"`
}
type logDocument struct {
	Version   int        `json:"version"`
	Next      int64      `json:"next"`
	Truncated bool       `json:"truncated"`
	Entries   []LogEntry `json:"entries"`
}

var ansiLog = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
var headerLog = regexp.MustCompile(`(?i)(authorization|proxy-authorization|cookie|set-cookie)(\s*[:=]\s*)[^\r\n]+`)
var queryLog = regexp.MustCompile(`(https?://[^\s?"'<>]+)\?[^\s"'<>]+`)

func cleanLogText(text string) string {
	text = ansiLog.ReplaceAllString(text, "")
	text = strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' {
			return -1
		}
		if r == 127 {
			return -1
		}
		return r
	}, text)
	text = headerLog.ReplaceAllString(text, "${1}${2}[숨김]")
	text = queryLog.ReplaceAllString(text, "${1}?[쿼리 숨김]")
	if len(text) > maxLogLine {
		text = text[:maxLogLine]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
		text += " … [긴 로그 줄 일부 생략]"
	}
	return text
}
func parseLogs(raw string, truncated bool) logDocument {
	doc := logDocument{Version: 1, Truncated: truncated, Entries: []LogEntry{}}
	if raw == "" {
		return doc
	}
	if json.Unmarshal([]byte(raw), &doc) == nil && doc.Version == 1 {
		if doc.Entries == nil {
			doc.Entries = []LogEntry{}
		}
		for _, entry := range doc.Entries {
			if entry.Sequence > doc.Next {
				doc.Next = entry.Sequence
			}
		}
		doc.Truncated = doc.Truncated || truncated
		return doc
	}
	doc = logDocument{Version: 1, Truncated: truncated, Entries: []LogEntry{}}
	for _, line := range strings.Split(strings.ToValidUTF8(raw, "�"), "\n") {
		if line == "" {
			continue
		}
		doc.Next++
		doc.Entries = append(doc.Entries, LogEntry{Sequence: doc.Next, Engine: "legacy", Stream: "legacy", Text: cleanLogText(line)})
	}
	if len(doc.Entries) > maxLogEntries {
		doc.Truncated = true
		doc.Entries = doc.Entries[len(doc.Entries)-maxLogEntries:]
	}
	return doc
}
func logData(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, job string) (logDocument, error) {
	var raw string
	var bytes int64
	err := q.QueryRowContext(ctx, "SELECT coalesce(CAST(substr(CAST(log AS BLOB),-?) AS TEXT),''),coalesce(length(CAST(log AS BLOB)),0) FROM download_queue WHERE id=?", maxLogBytes, job).Scan(&raw, &bytes)
	if err != nil {
		return logDocument{}, err
	}
	return parseLogs(raw, bytes > maxLogBytes), nil
}
func (s *Service) Logs(ctx context.Context, req LogRequest) api.Result {
	if req.JobID == "" || req.Before < 0 || req.Limit < 0 || req.Limit > 200 {
		return api.Failure("INVALID_ARGUMENT", "로그 조회 범위를 확인하세요")
	}
	if req.Limit == 0 {
		req.Limit = 100
	}
	page, err := NewStore(s.db).readLogs(ctx, req)
	if err != nil {
		return fail(err)
	}
	if warning, ok := s.logWarnings.Load(req.JobID); ok {
		text := warning.(string)
		page.Warning = &text
	}
	return api.Success(page)
}
func (s *Store) readLogs(ctx context.Context, req LogRequest) (LogPage, error) {
	page := LogPage{Entries: []LogEntry{}, Attempts: []LogAttempt{}}
	doc, err := logData(ctx, s.db, req.JobID)
	if err != nil {
		return page, err
	}
	page.Truncated = doc.Truncated
	if err = s.db.QueryRowContext(ctx, "SELECT state,current_attempt_id FROM download_queue WHERE id=?", req.JobID).Scan(&page.State, &page.CurrentAttemptID); err != nil {
		return page, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,attempt_no,outcome FROM download_attempt WHERE job_id=? ORDER BY attempt_no DESC LIMIT 50", req.JobID)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var attempt LogAttempt
		if err = rows.Scan(&attempt.ID, &attempt.Number, &attempt.Outcome); err != nil {
			break
		}
		page.Attempts = append(page.Attempts, attempt)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return page, err
	}
	for _, entry := range doc.Entries {
		if (req.AttemptID == "" || entry.AttemptID == req.AttemptID) && (req.Before == 0 || entry.Sequence < req.Before) {
			page.Entries = append(page.Entries, entry)
		}
	}
	if len(page.Entries) > req.Limit {
		page.HasOlder = true
		page.Entries = page.Entries[len(page.Entries)-req.Limit:]
	}
	return page, nil
}
func (s *Store) appendLogs(ctx context.Context, job string, entries []LogEntry, truncated bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	doc, err := logData(ctx, tx, job)
	if err != nil {
		return err
	}
	doc.Truncated = doc.Truncated || truncated
	for _, entry := range entries {
		doc.Next++
		entry.Sequence = doc.Next
		doc.Entries = append(doc.Entries, entry)
	}
	if len(doc.Entries) > maxLogEntries {
		doc.Entries = doc.Entries[len(doc.Entries)-maxLogEntries:]
		doc.Truncated = true
	}
	var raw []byte
	for {
		raw, err = json.Marshal(doc)
		if err != nil {
			return err
		}
		if len(raw) <= maxLogBytes {
			break
		}
		doc.Truncated = true
		drop := max(1, len(doc.Entries)/4)
		doc.Entries = doc.Entries[drop:]
	}
	if _, err = tx.ExecContext(ctx, "UPDATE download_queue SET log=? WHERE id=?", string(raw), job); err != nil {
		return err
	}
	return tx.Commit()
}

type logRecorder struct {
	store        *Store
	job, attempt string
	mu           sync.Mutex
	pending      []LogEntry
	truncated    bool
	wake         chan struct{}
	stop         chan struct{}
	done         chan struct{}
	warning      func(error)
}

func newLogRecorder(store *Store, job, attempt string, warning func(error)) *logRecorder {
	r := &logRecorder{store: store, job: job, attempt: attempt, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), warning: warning}
	go func() {
		defer close(r.done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-r.stop:
				r.flush()
				return
			case <-ticker.C:
				r.flush()
			case <-r.wake:
				r.flush()
			}
		}
	}()
	return r
}
func (r *logRecorder) write(output engine.Output) {
	line := strings.TrimSpace(output.Text)
	if line == "" {
		return
	}
	// Structured extractor metadata and private result frames are transport,
	// not console diagnostics; they may contain signed media addresses.
	if strings.HasPrefix(line, transferPrefix) || output.Stream == "stdout" && (strings.HasPrefix(line, "__ASTERODIA_RESULT__:") || json.Valid([]byte(line))) {
		return
	}
	text := cleanLogText(line)
	now := time.Now().UnixMilli()
	r.mu.Lock()
	if len(r.pending) >= maxPendingLogs {
		copy(r.pending, r.pending[1:])
		r.pending = r.pending[:len(r.pending)-1]
		r.truncated = true
	}
	r.pending = append(r.pending, LogEntry{AttemptID: r.attempt, RecordedAt: &now, Engine: output.Engine, Stream: output.Stream, Text: text})
	wake := len(r.pending) >= 64
	r.mu.Unlock()
	if wake {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
}
func (r *logRecorder) note(text string) {
	r.write(engine.Output{Engine: "download", Stream: "system", Text: text})
}
func (r *logRecorder) close() { close(r.stop); <-r.done }
func (r *logRecorder) flush() {
	r.mu.Lock()
	entries, truncated := r.pending, r.truncated
	r.pending = nil
	r.truncated = false
	r.mu.Unlock()
	if len(entries) == 0 && !truncated {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := r.store.appendLogs(ctx, r.job, entries, truncated)
	r.warning(err)
	if err != nil {
		r.mu.Lock()
		pending := append(entries, r.pending...)
		r.truncated = r.truncated || truncated
		if len(pending) > maxPendingLogs {
			pending = pending[len(pending)-maxPendingLogs:]
			r.truncated = true
		}
		r.pending = pending
		r.mu.Unlock()
	}
}
func (s *Service) logWarning(job string, err error) {
	if err == nil {
		s.logWarnings.Delete(job)
		return
	}
	s.logWarnings.Store(job, fmt.Sprintf("일부 로그를 저장하지 못했습니다: %v", err))
	count := 0
	s.logWarnings.Range(func(key, value any) bool {
		count++
		if count > 64 {
			s.logWarnings.Delete(key)
		}
		return true
	})
}
