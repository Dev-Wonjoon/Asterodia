package download_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	_ "modernc.org/sqlite"
)

func TestStoreCreateInitialAttempt(t *testing.T) {
	const original = "https://EXAMPLE.test:443/watch?v=abc&list=p#part"
	const normalized = "https://example.test/watch?v=abc&list=p#part"
	input := download.InitialAttemptInput{
		OriginalURL: original,
		StorageRoot: filepath.Join(os.Getenv("STORAGEBUCKET_TEST_ROOT"), "downloads"),
		Options:     engine.Options{Type: "video", Quality: "480", Extension: "mp4"},
	}
	if !filepath.IsAbs(input.StorageRoot) {
		t.Fatal("STORAGEBUCKET_TEST_ROOT에 실행별 절대 경로를 지정하세요")
	}

	for _, name := range []string{"new-url", "existing-url"} {
		t.Run(name, func(t *testing.T) {
			db := openStoreTestDB(t)
			if name == "existing-url" {
				execStoreTestSQL(t, db, `
					INSERT INTO download_urls
						(id, url, normalized_url, normalization_version, legacy_video_id, created_at)
					VALUES (42, ?, ?, 1, 'legacy-id', 123)
				`, normalized, normalized)
			}

			ctx := context.Background()
			store := download.NewStore(db)
			got, err := store.CreateInitialAttempt(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if got.RequestURLID <= 0 || got.JobID == "" || got.AttemptID == "" ||
				got.JobID == got.AttemptID || got.StorageRoot != input.StorageRoot {
				t.Fatalf("잘못된 작업·시도 식별 정보: %+v", got)
			}

			var matched int
			err = db.QueryRow(`
				SELECT count(*)
				FROM download_queue q
				JOIN download_urls u ON u.id = q.request_url_id
				JOIN download_attempt a ON a.job_id = q.id AND a.id = q.current_attempt_id
				JOIN download_job_storage s ON s.job_id = q.id
				WHERE q.id = ? AND a.id = ? AND u.id = ?
				  AND q.original_url = ? AND u.normalized_url = ? AND u.normalization_version = 1
				  AND q.mode = 'create' AND q.state = 'running'
				  AND q.options_json = '{"type":"video","quality":"480","extension":"mp4"}'
				  AND q.target_media_id IS NULL AND q.target_media_id_snapshot IS NULL
				  AND q.progress = 0 AND q.auto_run_count = 0 AND q.cleanup_required = 0
				  AND a.attempt_no = 1 AND a."trigger" = 'manual_initial'
				  AND a.phase = 'discovering' AND a.outcome IS NULL
				  AND q.created_at > 0 AND q.created_at = q.started_at
				  AND q.started_at = a.started_at AND s.created_at = q.created_at
				  AND s.storage_root = ?
			`, got.JobID, got.AttemptID, got.RequestURLID, original, normalized, input.StorageRoot).
				Scan(&matched)
			if err != nil || matched != 1 {
				t.Fatalf("커밋된 관계 및 상태: 일치 건수=%d, 오류=%v", matched, err)
			}

			var savedURL string
			var legacyID sql.NullString
			var createdAt int64
			if err := db.QueryRow(`
				SELECT url, legacy_video_id, created_at FROM download_urls WHERE id = ?
			`, got.RequestURLID).Scan(&savedURL, &legacyID, &createdAt); err != nil {
				t.Fatal(err)
			}
			if name == "existing-url" {
				if got.RequestURLID != 42 || savedURL != normalized ||
					!legacyID.Valid || legacyID.String != "legacy-id" || createdAt != 123 {
					t.Fatal("기존 URL 행의 ID, 원문, 레거시 ID 또는 생성 시각이 바뀌었습니다")
				}
			} else if savedURL != original || legacyID.Valid {
				t.Fatal("새 URL 행에 요청 원문이 그대로 기록되어야 합니다")
			}

			if _, err := store.CreateInitialAttempt(ctx, input); !errors.Is(err, download.ErrAlreadyProcessing) {
				t.Fatalf("같은 URL의 두 번째 활성 작업을 막아야 합니다: %v", err)
			}
			assertStoreCounts(t, db, 1)
		})
	}

	t.Run("rollback-on-current-attempt-update", func(t *testing.T) {
		db := openStoreTestDB(t)
		execStoreTestSQL(t, db, `
			CREATE TRIGGER test_abort_current_attempt
			BEFORE UPDATE OF current_attempt_id ON download_queue
			WHEN OLD.state = 'pending' AND OLD.current_attempt_id IS NULL
			  AND NEW.state = 'running'
			  AND EXISTS (
			    SELECT 1 FROM download_attempt
			    WHERE job_id = NEW.id AND id = NEW.current_attempt_id
			  )
			  AND EXISTS (SELECT 1 FROM download_job_storage WHERE job_id = NEW.id)
			BEGIN
				SELECT RAISE(ABORT, 'TEST_ABORT_CURRENT_ATTEMPT');
			END
		`)

		got, err := download.NewStore(db).CreateInitialAttempt(context.Background(), input)
		if err == nil || !strings.Contains(err.Error(), "TEST_ABORT_CURRENT_ATTEMPT") {
			t.Fatalf("시도와 저장 루트를 삽입한 뒤 큐 갱신에서 실패해야 합니다: %v", err)
		}
		if got != (download.InitialAttempt{}) {
			t.Fatalf("실패한 기록의 ID를 반환했습니다: %+v", got)
		}
		assertStoreCounts(t, db, 0)
	})
}

func TestStoreFreezeTargets(t *testing.T) {
	root := os.Getenv("STORAGEBUCKET_TEST_ROOT")
	if !filepath.IsAbs(root) {
		t.Fatal("STORAGEBUCKET_TEST_ROOT에 실행별 절대 경로를 지정하세요")
	}

	targets := []download.TargetInput{
		{
			TargetKey: "youtube:z",
			SourceURL: "https://EXAMPLE.test:443/watch?v=z&list=abc#part",
			Metadata:  download.TargetMetadata{Kind: "video", Title: "첫 번째"},
		},
		{
			TargetKey: "youtube:a",
			SourceURL: "https://example.test/watch?v=a&list=abc",
			Metadata:  download.TargetMetadata{Kind: "video", Title: "두 번째"},
		},
	}

	setup := func(t *testing.T) (*sql.DB, *download.Store, download.InitialAttempt) {
		t.Helper()
		db := openStoreTestDB(t)
		store := download.NewStore(db)
		job, err := store.CreateInitialAttempt(t.Context(), download.InitialAttemptInput{
			OriginalURL: "https://example.test/playlist?list=abc",
			StorageRoot: filepath.Join(root, "downloads"),
		})
		if err != nil {
			t.Fatal(err)
		}
		execStoreTestSQL(t, db, "UPDATE download_queue SET updated_at = 1 WHERE id = ?", job.JobID)
		return db, store, job
	}

	assertState := func(t *testing.T, db *sql.DB, job download.InitialAttempt, wantPhase string, wantCount int) {
		t.Helper()
		var phase, state string
		var count, violations int
		var updatedAt int64
		err := db.QueryRow(`
			SELECT a.phase, q.state, q.updated_at,
			       (SELECT count(*) FROM download_target WHERE attempt_id = a.id),
			       (SELECT count(*) FROM pragma_foreign_key_check())
			FROM download_queue q
			JOIN download_attempt a ON a.job_id = q.id AND a.id = q.current_attempt_id
			WHERE q.id = ? AND a.id = ?
		`, job.JobID, job.AttemptID).Scan(&phase, &state, &updatedAt, &count, &violations)
		if err != nil {
			t.Fatal(err)
		}
		if phase != wantPhase || state != "running" || count != wantCount || violations != 0 {
			t.Fatalf("phase=%q state=%q targets=%d foreign_key_violations=%d", phase, state, count, violations)
		}
		if (wantCount == 0 && updatedAt != 1) || (wantCount > 0 && updatedAt <= 1) {
			t.Fatalf("큐 갱신 시각이 대상 기록 결과와 맞지 않습니다: %d", updatedAt)
		}
	}

	t.Run("ordered-targets-and-no-refreeze", func(t *testing.T) {
		db, store, job := setup(t)
		if err := store.FreezeTargets(t.Context(), job.JobID, job.AttemptID, targets); err != nil {
			t.Fatal(err)
		}

		for position, want := range targets {
			var key, source, kind, title, state string
			err := db.QueryRow(`
				SELECT target_key, source_url, json_extract(metadata_json, '$.kind'),
				       json_extract(metadata_json, '$.title'), state
				FROM download_target
				WHERE attempt_id = ? AND position = ?
			`, job.AttemptID, position).Scan(&key, &source, &kind, &title, &state)
			if err != nil {
				t.Fatal(err)
			}
			if key != want.TargetKey || source != want.SourceURL ||
				kind != want.Metadata.Kind || title != want.Metadata.Title || state != "pending" {
				t.Fatalf("위치 %d의 대상 기록이 일치하지 않습니다", position)
			}
		}

		err := store.FreezeTargets(t.Context(), job.JobID, job.AttemptID, targets)
		if !errors.Is(err, download.ErrAttemptCannotFreeze) {
			t.Fatalf("이미 확정된 시도에 대한 오류를 기대했습니다: %v", err)
		}
		assertState(t, db, job, "downloading", 2)
	})

	t.Run("empty-and-duplicate-targets", func(t *testing.T) {
		db, store, job := setup(t)
		for _, invalid := range [][]download.TargetInput{nil, {targets[0], targets[0]}} {
			if err := store.FreezeTargets(t.Context(), job.JobID, job.AttemptID, invalid); err == nil {
				t.Fatal("비어 있거나 키가 중복된 대상 목록을 거부해야 합니다")
			}
			assertState(t, db, job, "discovering", 0)
		}
	})

	t.Run("wrong-job-and-cancel-request", func(t *testing.T) {
		db, store, job := setup(t)
		err := store.FreezeTargets(t.Context(), "other-job", job.AttemptID, targets)
		if !errors.Is(err, download.ErrAttemptCannotFreeze) {
			t.Fatalf("다른 작업의 시도를 거부해야 합니다: %v", err)
		}

		execStoreTestSQL(t, db, `
			UPDATE download_attempt SET cancel_requested_at = 2 WHERE id = ?
		`, job.AttemptID)
		err = store.FreezeTargets(t.Context(), job.JobID, job.AttemptID, targets)
		if !errors.Is(err, download.ErrAttemptCannotFreeze) {
			t.Fatalf("취소 요청된 시도를 거부해야 합니다: %v", err)
		}
		assertState(t, db, job, "discovering", 0)
	})

	t.Run("rollback-on-phase-update", func(t *testing.T) {
		db, store, job := setup(t)
		execStoreTestSQL(t, db, `
			CREATE TRIGGER test_abort_download_phase
			BEFORE UPDATE OF phase ON download_attempt
			WHEN OLD.phase = 'discovering' AND NEW.phase = 'downloading'
			  AND (SELECT count(*) FROM download_target WHERE attempt_id = OLD.id) = 2
			BEGIN
				SELECT RAISE(ABORT, 'TEST_ABORT_DOWNLOAD_PHASE');
			END
		`)
		err := store.FreezeTargets(t.Context(), job.JobID, job.AttemptID, targets)
		if err == nil || !strings.Contains(err.Error(), "TEST_ABORT_DOWNLOAD_PHASE") {
			t.Fatalf("대상 두 행을 삽입한 뒤 단계 갱신에서 실패해야 합니다: %v", err)
		}
		assertState(t, db, job, "discovering", 0)
	})
}

func TestStoreRecordStagedResults(t *testing.T) {
	root := os.Getenv("STORAGEBUCKET_TEST_ROOT")
	if !filepath.IsAbs(root) {
		t.Fatal("STORAGEBUCKET_TEST_ROOT에 실행별 절대 경로를 지정하세요")
	}

	targets := []download.TargetInput{
		{
			TargetKey: "youtube:first",
			SourceURL: "https://example.test/watch?v=first",
			Metadata:  download.TargetMetadata{Kind: "video"},
		},
		{
			TargetKey: "youtube:second",
			SourceURL: "https://example.test/watch?v=second",
			Metadata:  download.TargetMetadata{Kind: "video"},
		},
	}
	results := []download.StagedResult{
		{Position: 0, TargetKey: targets[0].TargetKey, Path: filepath.Join(root, "item-0001", "media.mp4")},
		{Position: 1, TargetKey: targets[1].TargetKey, Path: filepath.Join(root, "item-0002", "media.mp4")},
	}

	for _, name := range []string{"complete", "missing-result", "wrong-key", "rollback-on-phase-update"} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			db := openStoreTestDB(t)
			store := download.NewStore(db)
			job, err := store.CreateInitialAttempt(ctx, download.InitialAttemptInput{
				OriginalURL: "https://example.test/playlist?list=fixture",
				StorageRoot: root,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.FreezeTargets(ctx, job.JobID, job.AttemptID, targets); err != nil {
				t.Fatal(err)
			}
			execStoreTestSQL(t, db, "UPDATE download_queue SET updated_at = 1 WHERE id = ?", job.JobID)

			input := append([]download.StagedResult(nil), results...)
			switch name {
			case "complete":
				// 결과 입력 순서를 뒤집어도 확정 순번으로 연결되어야 합니다.
				input[0], input[1] = input[1], input[0]
			case "missing-result":
				input = input[:1]
			case "wrong-key":
				input[1].TargetKey = "youtube:unexpected"
			case "rollback-on-phase-update":
				execStoreTestSQL(t, db, `
					CREATE TRIGGER test_abort_verifying_phase
					BEFORE UPDATE OF phase ON download_attempt
					WHEN NEW.phase = 'verifying'
					  AND (
					    SELECT count(*) FROM download_target
					    WHERE attempt_id = OLD.id AND state = 'downloaded'
					      AND staged_path IS NOT NULL
					  ) = 2
					BEGIN
						SELECT RAISE(ABORT, 'TEST_ABORT_VERIFYING_PHASE');
					END
				`)
			}

			err = store.RecordStagedResults(ctx, job.JobID, job.AttemptID, input)
			switch name {
			case "complete":
				if err != nil {
					t.Fatal(err)
				}
				repeatErr := store.RecordStagedResults(ctx, job.JobID, job.AttemptID, input)
				if !errors.Is(repeatErr, download.ErrAttemptCannotRecordResults) {
					t.Fatalf("이미 기록한 시도의 재기록을 막아야 합니다: %v", repeatErr)
				}
			case "rollback-on-phase-update":
				if err == nil || !strings.Contains(err.Error(), "TEST_ABORT_VERIFYING_PHASE") {
					t.Fatalf("대상 두 건을 갱신한 뒤 단계 갱신에서 실패해야 합니다: %v", err)
				}
			default:
				if !errors.Is(err, download.ErrAttemptCannotRecordResults) {
					t.Fatalf("확정 목록과 일치하지 않는 결과를 거부해야 합니다: %v", err)
				}
			}

			var phase, queueState string
			var updatedAt int64
			if err := db.QueryRow(`
				SELECT a.phase, q.state, q.updated_at
				FROM download_queue q
				JOIN download_attempt a ON a.id = q.current_attempt_id AND a.job_id = q.id
				WHERE q.id = ? AND a.id = ?
			`, job.JobID, job.AttemptID).Scan(&phase, &queueState, &updatedAt); err != nil {
				t.Fatal(err)
			}
			if queueState != "running" {
				t.Fatalf("큐 상태: %q, 기대값: running", queueState)
			}
			if name == "complete" {
				if phase != "verifying" || updatedAt <= 1 {
					t.Fatalf("커밋 후 phase=%q updated_at=%d", phase, updatedAt)
				}
			} else if phase != "downloading" || updatedAt != 1 {
				t.Fatalf("롤백 후 phase=%q updated_at=%d", phase, updatedAt)
			}

			for _, want := range results {
				var key, state string
				var path, finalPath sql.NullString
				if err := db.QueryRow(`
					SELECT target_key, state, staged_path, final_path
					FROM download_target WHERE attempt_id = ? AND position = ?
				`, job.AttemptID, want.Position).Scan(&key, &state, &path, &finalPath); err != nil {
					t.Fatal(err)
				}
				if key != want.TargetKey || finalPath.Valid {
					t.Fatalf("순번 %d의 키 또는 최종 경로가 바뀌었습니다", want.Position)
				}
				if name == "complete" {
					if state != "downloaded" || !path.Valid || path.String != want.Path {
						t.Fatalf("순번 %d의 결과가 기록되지 않았습니다: %s %+v", want.Position, state, path)
					}
				} else if state != "pending" || path.Valid {
					t.Fatalf("순번 %d에 부분 갱신이 남았습니다: %s %+v", want.Position, state, path)
				}
			}
		})
	}
}

func openStoreTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)&_pragma=temp_store(2)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	// Use the application-owned current schema, independently of archived Electron paths.
	if err := database.Initialize(t.Context(), db); err != nil {
		t.Fatal(err)
	}

	var fk, version int
	err = db.QueryRow(`
		SELECT foreign_keys, user_version FROM pragma_foreign_keys(), pragma_user_version()
	`).Scan(&fk, &version)
	if err != nil || fk != 1 || version != 10 {
		t.Fatalf("테스트 DB: foreign_keys=%d, user_version=%d, 오류=%v", fk, version, err)
	}
	return db
}

func execStoreTestSQL(t *testing.T, db *sql.DB, statement string, args ...any) {
	t.Helper()
	if _, err := db.Exec(statement, args...); err != nil {
		t.Fatalf("테스트 SQL 실행 실패: %v\n%s", err, statement)
	}
}

func assertStoreCounts(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	for _, table := range []string{
		"download_urls", "download_queue", "download_job_storage", "download_attempt",
	} {
		var count int
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s 행 수=%d, 기대값=%d, 오류=%v", table, count, want, err)
		}
	}

	var targets, violations int
	err := db.QueryRow(`
		SELECT (SELECT count(*) FROM download_target),
		       (SELECT count(*) FROM pragma_foreign_key_check())
	`).Scan(&targets, &violations)
	if err != nil || targets != 0 || violations != 0 {
		t.Fatalf("대상 수=%d, 외래 키 위반=%d, 오류=%v", targets, violations, err)
	}
}
