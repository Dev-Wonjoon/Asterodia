package download

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"github.com/google/uuid"
	whatwgurl "github.com/nlnwa/whatwg-url/url"
)

var (
	ErrAlreadyProcessing  = errors.New("같은 URL의 작업이 이미 처리 중입니다")
	ErrCleanupRequired    = errors.New("같은 URL의 이전 작업을 먼저 정리해야 합니다")
	ErrRedownloadRequired = errors.New(
		"같은 URL의 미디어가 이미 있어 재다운로드 확인이 필요합니다",
	)
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

type InitialAttemptInput struct {
	OriginalURL      string
	StorageRoot      string
	Options          engine.Options
	ReplaceMediaID   *int64
	ExpectedRevision *int64
}

type InitialAttempt struct {
	RequestURLID int64
	JobID        string
	AttemptID    string
	StorageRoot  string
}

func (s *Store) CreateInitialAttempt(
	ctx context.Context,
	input InitialAttemptInput,
) (InitialAttempt, error) {
	input.OriginalURL = cleanInstagramURL(input.OriginalURL)
	normalizedURL, err := normalizeRequestURL(input.OriginalURL)
	if err != nil {
		return InitialAttempt{}, err
	}
	if !filepath.IsAbs(input.StorageRoot) || strings.ContainsRune(input.StorageRoot, '\x00') {
		return InitialAttempt{}, errors.New("다운로드 저장 루트는 유효한 절대 경로여야 합니다")
	}

	options, err := initialOptionsJSON(input.Options)
	if err != nil {
		return InitialAttempt{}, err
	}
	jobID, err := uuid.NewRandom()
	if err != nil {
		return InitialAttempt{}, fmt.Errorf("작업 ID 생성 실패: %w", err)
	}
	attemptID, err := uuid.NewRandom()
	if err != nil {
		return InitialAttempt{}, fmt.Errorf("시도 ID 생성 실패: %w", err)
	}
	record := InitialAttempt{
		JobID:       jobID.String(),
		AttemptID:   attemptID.String(),
		StorageRoot: filepath.Clean(input.StorageRoot),
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return InitialAttempt{}, fmt.Errorf("다운로드 기록 트랜잭션 시작 실패: %w", err)
	}
	defer tx.Rollback()

	const normalizationVersion = 1
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO download_urls (url, normalized_url, normalization_version, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (normalization_version, normalized_url) DO NOTHING
	`, input.OriginalURL, normalizedURL, normalizationVersion, now); err != nil {
		return InitialAttempt{}, fmt.Errorf("요청 URL 기록 실패: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM download_urls
		WHERE normalization_version = ? AND normalized_url = ?
	`, normalizationVersion, normalizedURL).Scan(&record.RequestURLID); err != nil {
		return InitialAttempt{}, fmt.Errorf("요청 URL ID 조회 실패: %w", err)
	}
	if err := checkInitialAttemptConflicts(ctx, tx, record.RequestURLID); err != nil {
		if !errors.Is(err, ErrRedownloadRequired) || input.ReplaceMediaID == nil {
			return InitialAttempt{}, err
		}
	}
	mode := "create"
	if input.ReplaceMediaID != nil {
		if input.ExpectedRevision == nil {
			return InitialAttempt{}, ErrRevisionConflict
		}
		var revision, urlID int64
		if err := tx.QueryRowContext(ctx, "SELECT revision,request_url_id FROM media WHERE id=? AND source_kind='download'", *input.ReplaceMediaID).Scan(&revision, &urlID); err != nil || revision != *input.ExpectedRevision || urlID != record.RequestURLID {
			return InitialAttempt{}, ErrRevisionConflict
		}
		if err := media.CheckWrite(ctx, tx, *input.ReplaceMediaID, *input.ExpectedRevision); err != nil {
			return InitialAttempt{}, err
		}
		var value map[string]any
		_ = json.Unmarshal([]byte(options), &value)
		value["_expectedRevision"] = revision
		encoded, _ := json.Marshal(value)
		options = string(encoded)
		mode = "replace"
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO download_queue (
			id, request_url_id, original_url, mode, state, options_json,
			current_attempt_id, created_at, updated_at, target_media_id, target_media_id_snapshot
		)
		VALUES (?, ?, ?, ?, 'pending', ?, NULL, ?, ?, ?, ?)
	`, record.JobID, record.RequestURLID, input.OriginalURL, mode, options, now, now, input.ReplaceMediaID, input.ReplaceMediaID); err != nil {
		return InitialAttempt{}, fmt.Errorf("다운로드 작업 기록 실패: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO download_job_storage (job_id, storage_root, created_at)
		VALUES (?, ?, ?)
	`, record.JobID, record.StorageRoot, now); err != nil {
		return InitialAttempt{}, fmt.Errorf("작업 저장 루트 기록 실패: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO download_attempt (id, job_id, attempt_no, trigger, phase, started_at,target_media_id,target_media_id_snapshot)
		VALUES (?, ?, 1, 'manual_initial', 'discovering', ?,?,?)
	`, record.AttemptID, record.JobID, now, input.ReplaceMediaID, input.ReplaceMediaID); err != nil {
		return InitialAttempt{}, fmt.Errorf("첫 다운로드 시도 기록 실패: %w", err)
	}

	updated, err := tx.ExecContext(ctx, `
		UPDATE download_queue
		SET state = 'running', current_attempt_id = ?, started_at = ?, updated_at = ?
		WHERE id = ? AND state = 'pending' AND current_attempt_id IS NULL
	`, record.AttemptID, now, now, record.JobID)
	if err != nil {
		return InitialAttempt{}, fmt.Errorf("현재 다운로드 시도 연결 실패: %w", err)
	}
	count, err := updated.RowsAffected()
	if err != nil {
		return InitialAttempt{}, fmt.Errorf("다운로드 작업 갱신 건수 확인 실패: %w", err)
	}
	if count != 1 {
		return InitialAttempt{}, fmt.Errorf("다운로드 작업 갱신 건수가 1이어야 합니다: %d", count)
	}

	if err := tx.Commit(); err != nil {
		return InitialAttempt{}, fmt.Errorf("다운로드 기록 커밋 실패: %w", err)
	}
	return record, nil
}

func normalizeRequestURL(original string) (string, error) {
	if strings.ContainsFunc(original, func(r rune) bool {
		return r <= 0x20 || r == 0x7f
	}) {
		return "", engine.ErrInvalidURL
	}
	parsed, err := whatwgurl.Parse(cleanInstagramURL(original))
	if err != nil || (parsed.Scheme() != "http" && parsed.Scheme() != "https") ||
		parsed.Host() == "" || parsed.Username() != "" || parsed.Password() != "" {
		return "", engine.ErrInvalidURL
	}
	return parsed.Href(false), nil
}

func initialOptionsJSON(options engine.Options) (string, error) {
	kind := options.Type
	if options.Quality == "audio" {
		kind = "audio"
	}
	if _, err := engine.NormalizeFormat(engine.FormatOptions{Type: kind, Extension: options.Extension, VideoCodec: options.VideoCodec, AudioCodec: options.AudioCodec}); err != nil {
		return "", err
	}
	switch options.Type {
	case "", "video", "audio":
	default:
		return "", errors.New("지원하지 않는 다운로드 유형입니다")
	}
	switch options.Quality {
	case "", "best", "8k", "4k", "2k", "1080", "720", "480", "audio":
	default:
		return "", errors.New("지원하지 않는 다운로드 화질입니다")
	}
	data, err := json.Marshal(options)
	if err != nil {
		return "", fmt.Errorf("다운로드 옵션 직렬화 실패: %w", err)
	}
	return string(data), nil
}

func checkInitialAttemptConflicts(ctx context.Context, tx *sql.Tx, requestURLID int64) error {
	var cleanupRequired bool
	err := tx.QueryRowContext(ctx, `
		SELECT q.cleanup_required
		FROM download_queue q
		WHERE q.request_url_id = ?
		  AND (
		    q.state IN ('pending', 'running')
		    OR q.cleanup_required = 1
		    OR EXISTS (
		        SELECT 1 FROM download_attempt a
		        WHERE a.job_id = q.id AND a.outcome IS NULL
		    )
		  )
		ORDER BY q.cleanup_required DESC, q.id
		LIMIT 1
	`, requestURLID).Scan(&cleanupRequired)
	switch {
	case err == nil && cleanupRequired:
		return ErrCleanupRequired
	case err == nil:
		return ErrAlreadyProcessing
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("같은 URL의 다운로드 작업 확인 실패: %w", err)
	}

	var mediaExists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM media WHERE request_url_id = ?)
	`, requestURLID).Scan(&mediaExists); err != nil {
		return fmt.Errorf("같은 URL의 기존 미디어 확인 실패: %w", err)
	}
	if mediaExists {
		return ErrRedownloadRequired
	}
	return nil
}

var ErrAttemptCannotFreeze = errors.New("현재 시도에 다운로드 대상을 확정할 수 없습니다")

type TargetMetadata struct {
	Kind         string   `json:"kind"`
	Title        string   `json:"title,omitempty"`
	ExternalID   string   `json:"externalId,omitempty"`
	PlatformName string   `json:"platformName,omitempty"`
	OwnerID      string   `json:"ownerId,omitempty"`
	OwnerName    string   `json:"ownerName,omitempty"`
	Duration     *float64 `json:"duration,omitempty"`
}

type TargetInput struct {
	TargetKey string
	SourceURL string
	Metadata  TargetMetadata
}

func (s *Store) FreezeTargets(
	ctx context.Context,
	jobID, attemptID string,
	targets []TargetInput,
) error {
	if strings.TrimSpace(jobID) == "" || strings.TrimSpace(attemptID) == "" {
		return errors.New("작업 ID와 시도 ID가 필요합니다")
	}
	if len(targets) == 0 {
		return errors.New("확정할 다운로드 대상이 없습니다")
	}

	keys := make(map[string]struct{}, len(targets))
	metadata := make([]string, len(targets))
	ids := make([]string, len(targets))
	for i, target := range targets {
		if strings.TrimSpace(target.TargetKey) == "" || strings.ContainsRune(target.TargetKey, '\x00') {
			return fmt.Errorf("대상 %d의 키가 올바르지 않습니다", i+1)
		}
		if _, exists := keys[target.TargetKey]; exists {
			return fmt.Errorf("대상 %d의 키가 중복되었습니다", i+1)
		}
		keys[target.TargetKey] = struct{}{}

		// 검증에만 사용하고 저장할 때는 SourceURL 원문을 사용합니다.
		if _, err := normalizeRequestURL(target.SourceURL); err != nil {
			return fmt.Errorf("대상 %d의 URL 검증 실패: %w", i+1, err)
		}
		switch target.Metadata.Kind {
		case "image", "video", "audio", "unknown":
		default:
			return fmt.Errorf("대상 %d의 미디어 종류가 올바르지 않습니다", i+1)
		}
		if target.Metadata.Duration != nil && *target.Metadata.Duration < 0 {
			return fmt.Errorf("대상 %d의 재생 시간이 올바르지 않습니다", i+1)
		}
		data, err := json.Marshal(target.Metadata)
		if err != nil {
			return fmt.Errorf("대상 %d의 메타데이터 직렬화 실패: %w", i+1, err)
		}
		metadata[i] = string(data)

		id, err := uuid.NewRandom()
		if err != nil {
			return fmt.Errorf("대상 %d의 ID 생성 실패: %w", i+1, err)
		}
		ids[i] = id.String()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("다운로드 대상 기록 트랜잭션 시작 실패: %w", err)
	}
	defer tx.Rollback()

	updated, err := tx.ExecContext(ctx, `
		UPDATE download_queue
		SET updated_at = ?
		WHERE id = ? AND current_attempt_id = ?
		  AND state = 'running' AND cleanup_required = 0
		  AND EXISTS (
		    SELECT 1 FROM download_attempt a
		    WHERE a.id = download_queue.current_attempt_id
		      AND a.job_id = download_queue.id
		      AND a.phase = 'discovering'
		      AND a.outcome IS NULL AND a.finished_at IS NULL
		      AND a.committed_at IS NULL AND a.cancel_requested_at IS NULL
		      AND a.target_media_id IS download_queue.target_media_id
		      AND a.target_media_id_snapshot IS download_queue.target_media_id_snapshot
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM download_target t
		    WHERE t.attempt_id = download_queue.current_attempt_id
		  )
	`, time.Now().UnixMilli(), jobID, attemptID)
	if err != nil {
		return fmt.Errorf("현재 다운로드 시도 확인 실패: %w", err)
	}
	count, err := updated.RowsAffected()
	if err != nil {
		return fmt.Errorf("현재 다운로드 작업 갱신 건수 확인 실패: %w", err)
	}
	if count != 1 {
		return ErrAttemptCannotFreeze
	}

	for position, target := range targets {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO download_target (
				id, attempt_id, target_key, source_url, metadata_json, position, state
			)
			VALUES (?, ?, ?, ?, ?, ?, 'pending')
		`, ids[position], attemptID, target.TargetKey, target.SourceURL,
			metadata[position], position); err != nil {
			return fmt.Errorf("대상 %d 기록 실패: %w", position+1, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE download_attempt
		SET phase = 'downloading'
		WHERE id = ? AND job_id = ?
	`, attemptID, jobID); err != nil {
		return fmt.Errorf("다운로드 시도 단계 갱신 실패: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("다운로드 대상 기록 커밋 실패: %w", err)
	}
	return nil
}

var ErrAttemptCannotRecordResults = errors.New("현재 시도에 다운로드 결과를 기록할 수 없습니다")

type StagedResult struct {
	Position  int
	TargetKey string
	Path      string
}

func (s *Store) RecordStagedResults(
	ctx context.Context,
	jobID, attemptID string,
	results []StagedResult,
) error {
	if strings.TrimSpace(jobID) == "" || strings.TrimSpace(attemptID) == "" {
		return errors.New("작업 ID와 시도 ID가 필요합니다")
	}
	if len(results) == 0 {
		return errors.New("기록할 다운로드 결과가 없습니다")
	}

	prepared := make([]StagedResult, len(results))
	positions := make(map[int]struct{}, len(results))
	paths := make(map[string]struct{}, len(results))
	for i, result := range results {
		if result.Position < 0 || strings.TrimSpace(result.TargetKey) == "" ||
			strings.ContainsRune(result.TargetKey, '\x00') {
			return fmt.Errorf("결과 %d의 대상 순번 또는 키가 올바르지 않습니다", i+1)
		}
		if _, exists := positions[result.Position]; exists {
			return fmt.Errorf("결과의 대상 순번이 중복되었습니다: %d", result.Position)
		}
		if !filepath.IsAbs(result.Path) || strings.ContainsRune(result.Path, '\x00') {
			return fmt.Errorf("결과 %d에 유효한 절대 경로가 필요합니다", i+1)
		}

		result.Path = filepath.Clean(result.Path)
		pathKey := result.Path
		if filepath.Separator == '\\' {
			pathKey = strings.ToLower(pathKey)
		}
		if _, exists := paths[pathKey]; exists {
			return fmt.Errorf("결과 %d의 파일 경로가 중복되었습니다", i+1)
		}
		positions[result.Position] = struct{}{}
		paths[pathKey] = struct{}{}
		prepared[i] = result
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("다운로드 결과 기록 트랜잭션 시작 실패: %w", err)
	}
	defer tx.Rollback()

	updated, err := tx.ExecContext(ctx, `
		UPDATE download_queue
		SET updated_at = ?
		WHERE id = ? AND current_attempt_id = ?
		  AND state = 'running' AND cleanup_required = 0
		  AND EXISTS (
		    SELECT 1 FROM download_attempt a
		    WHERE a.id = download_queue.current_attempt_id
		      AND a.job_id = download_queue.id
		      AND a.phase = 'downloading'
		      AND a.outcome IS NULL AND a.finished_at IS NULL
		      AND a.committed_at IS NULL AND a.cancel_requested_at IS NULL
		      AND a.target_media_id IS download_queue.target_media_id
		      AND a.target_media_id_snapshot IS download_queue.target_media_id_snapshot
		  )
		  AND (
		    SELECT count(*) FROM download_target t
		    WHERE t.attempt_id = download_queue.current_attempt_id
		  ) = ?
	`, time.Now().UnixMilli(), jobID, attemptID, len(prepared))
	if err != nil {
		return fmt.Errorf("다운로드 결과 기록 조건 확인 실패: %w", err)
	}
	count, err := updated.RowsAffected()
	if err != nil {
		return fmt.Errorf("다운로드 작업 갱신 건수 확인 실패: %w", err)
	}
	if count != 1 {
		return ErrAttemptCannotRecordResults
	}

	for _, result := range prepared {
		updated, err := tx.ExecContext(ctx, `
			UPDATE download_target
			SET staged_path = ?, state = 'downloaded'
			WHERE attempt_id = ? AND position = ? AND target_key = ?
			  AND state IN ('pending', 'downloading')
			  AND staged_path IS NULL AND final_path IS NULL
		`, result.Path, attemptID, result.Position, result.TargetKey)
		if err != nil {
			return fmt.Errorf("순번 %d의 다운로드 결과 기록 실패: %w", result.Position, err)
		}
		count, err := updated.RowsAffected()
		if err != nil {
			return fmt.Errorf("순번 %d의 대상 갱신 건수 확인 실패: %w", result.Position, err)
		}
		if count != 1 {
			return fmt.Errorf("%w: 확정 대상과 일치하지 않는 결과 순번 %d",
				ErrAttemptCannotRecordResults, result.Position)
		}
	}

	updated, err = tx.ExecContext(ctx, `
		UPDATE download_attempt
		SET phase = 'verifying'
		WHERE id = ? AND job_id = ? AND phase = 'downloading'
	`, attemptID, jobID)
	if err != nil {
		return fmt.Errorf("다운로드 시도를 verifying으로 갱신하지 못했습니다: %w", err)
	}
	count, err = updated.RowsAffected()
	if err != nil {
		return fmt.Errorf("다운로드 시도 갱신 건수 확인 실패: %w", err)
	}
	if count != 1 {
		return ErrAttemptCannotRecordResults
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("다운로드 결과 기록 커밋 실패: %w", err)
	}
	return nil
}
