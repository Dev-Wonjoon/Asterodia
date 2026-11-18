package download_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

type ytdlpDiscoverFunc func(context.Context, string, string) (json.RawMessage, error)

func (f ytdlpDiscoverFunc) Discover(
	ctx context.Context, rawURL, stageDir string,
) (json.RawMessage, error) {
	return f(ctx, rawURL, stageDir)
}

type galleryDLDiscoverFunc func(context.Context, string, string) (engine.GalleryDLDiscovery, error)

func (f galleryDLDiscoverFunc) Discover(
	ctx context.Context, rawURL, stageDir string,
) (engine.GalleryDLDiscovery, error) {
	return f(ctx, rawURL, stageDir)
}

func TestRunnerDiscoverAttempt(t *testing.T) {
	storage := download.Storage{Root: downloadTestRoot(t)}

	for _, tc := range []struct {
		kind engine.Kind
		url  string
	}{
		{engine.KindYtdlp, "https://www.youtube.com/watch?v=BaW_jenozKc&si=fixture"},
		{engine.KindGalleryDL, "https://bsky.app/profile/example.com/post/fixture?ref=test"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			runner := download.Runner{Storage: storage}
			var calledDir string
			checkCall := func(rawURL, stageDir string) {
				t.Helper()
				if rawURL != tc.url {
					t.Fatalf("엔진이 받은 URL: %q, 기대값: %q", rawURL, tc.url)
				}
				entries, err := os.ReadDir(stageDir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("엔진에 비어 있는 작업 폴더를 넘겨야 합니다: %v", err)
				}
				calledDir = stageDir
			}

			switch tc.kind {
			case engine.KindYtdlp:
				runner.Ytdlp = ytdlpDiscoverFunc(func(
					_ context.Context, rawURL, stageDir string,
				) (json.RawMessage, error) {
					checkCall(rawURL, stageDir)
					return json.RawMessage(`{
						"id":"BaW_jenozKc","extractor_key":"Youtube",
						"webpage_url":"https://www.youtube.com/watch?v=BaW_jenozKc"
					}`), nil
				})
			case engine.KindGalleryDL:
				runner.GalleryDL = galleryDLDiscoverFunc(func(
					_ context.Context, rawURL, stageDir string,
				) (engine.GalleryDLDiscovery, error) {
					checkCall(rawURL, stageDir)
					return engine.ParseGalleryDLDiscovery([]byte(
						`[[3,"https://cdn.invalid/image.png?token=abc",{"extension":"png"}]]`,
					))
				})
			}

			result, err := runner.DiscoverAttempt(
				t.Context(), "runner-test", string(tc.kind), tc.url, engine.Options{},
			)
			if err != nil {
				t.Fatal(err)
			}
			if calledDir == "" || result.StageDir() != calledDir ||
				result.Engine != tc.kind || result.OriginalURL != tc.url {
				t.Fatalf("조회 결과가 호출과 일치하지 않습니다: %+v", result)
			}

			if tc.kind == engine.KindYtdlp {
				if len(result.YtdlpTargets) != 1 ||
					result.YtdlpTargets[0].TargetKey != "youtube:BaW_jenozKc" {
					t.Fatalf("yt-dlp 대상 불일치: %+v", result.YtdlpTargets)
				}
			} else if len(result.GalleryResult.Files) != 1 ||
				result.GalleryResult.Files[0].URL != "https://cdn.invalid/image.png?token=abc" {
				t.Fatalf("gallery-dl 대상 불일치: %+v", result.GalleryResult.Files)
			}
		})
	}
}

func (f ytdlpDiscoverFunc) DownloadSingle(
	context.Context, engine.YtdlpTarget, string, engine.Options,
) (string, error) {
	return "", errors.New("조회 테스트에서는 다운로드를 호출하면 안 됩니다")
}

func TestRunnerStageAttempt(t *testing.T) {
	if os.Getenv("STORAGEBUCKET_LIVE_TESTS") != "1" {
		t.Skip("실제 사이트 검증은 STORAGEBUCKET_LIVE_TESTS=1로 실행하세요")
	}
	ytdlpPath := os.Getenv("STORAGEBUCKET_TEST_YTDLP_EXE")
	galleryPath := os.Getenv("STORAGEBUCKET_TEST_GALLERYDL_EXE")
	if ytdlpPath == "" && galleryPath == "" {
		t.Skip("실제 저장을 검증하려면 두 엔진의 실행 경로를 지정하세요")
	}
	if !filepath.IsAbs(ytdlpPath) || !filepath.IsAbs(galleryPath) {
		t.Fatal("두 엔진의 절대 경로를 지정하세요")
	}

	runner := download.Runner{
		Storage:   download.Storage{Root: downloadTestRoot(t)},
		Ytdlp:     &engine.Ytdlp{Executable: ytdlpPath},
		GalleryDL: &engine.GalleryDL{Executable: galleryPath},
	}

	for _, tc := range []struct {
		kind engine.Kind
		url  string
	}{
		{engine.KindYtdlp, "https://raw.githubusercontent.com/chromium/chromium/main/media/test/data/bear.mp4?storagebucket=m4"},
		{engine.KindGalleryDL, "https://bsky.app/profile/fruttymoment.bsky.social/post/3lcdqq3x47s2h?storagebucket=m4"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			attempt, err := runner.DiscoverAttempt(
				t.Context(), "runner-stage-test", string(tc.kind), tc.url, engine.Options{},
			)
			if err != nil {
				t.Fatalf("조회 실패: %v", err)
			}
			if attempt.Engine != tc.kind {
				t.Fatalf("엔진: %q, 기대값: %q", attempt.Engine, tc.kind)
			}

			files, err := runner.StageAttempt(t.Context(), attempt)
			if err != nil {
				t.Fatalf("작업 파일 저장 실패: %v", err)
			}
			if len(files) != 1 || files[0].Position != 0 {
				t.Fatalf("첫 번째 대상의 저장 결과 한 건을 기대했습니다: %+v", files)
			}
			if filepath.Dir(files[0].Path) != filepath.Join(attempt.StageDir(), "item-0001") {
				t.Fatalf("저장 경로가 첫 번째 대상 폴더와 다릅니다: %s", files[0].Path)
			}
			t.Logf("작업 파일 저장 확인: %s", files[0].Path)
		})
	}
}

type stagingYtdlp struct {
	ytdlpDiscoverFunc
	save func(engine.YtdlpTarget, string) (string, error)
}

func (f stagingYtdlp) DownloadSingle(
	_ context.Context,
	target engine.YtdlpTarget,
	dir string,
	_ engine.Options,
) (string, error) {
	return f.save(target, dir)
}

func TestRunnerFreezeAndStageAttempt(t *testing.T) {
	root := downloadTestRoot(t)
	const youtubeURL = "https://www.youtube.com/watch?v=BaW_jenozKc&si=request"
	const youtubeSource = "https://www.youtube.com/watch?v=BaW_jenozKc&si=target"
	const instagramURL = "https://www.instagram.com/p/Fixture_123/?igsh=request"
	const instagramID = "3401234567890123456"
	const instagramKey = "instagram:" + instagramID + ":" + instagramID

	for _, tc := range []struct {
		name, url, key, source, kind      string
		gallery, rejectWrite, unsupported bool
	}{
		{
			name: "yt-dlp", url: youtubeURL, key: "youtube:BaW_jenozKc",
			source: youtubeSource, kind: "video",
		},
		{
			name: "gallery-dl", url: instagramURL, key: instagramKey,
			source: instagramURL, kind: "image", gallery: true,
		},
		{
			name: "freeze-failure-skips-stage", url: youtubeURL,
			rejectWrite: true,
		},
		{
			name:    "unsupported-gallery-target-skips-stage",
			url:     "https://bsky.app/profile/example.com/post/fixture?ref=test",
			gallery: true, unsupported: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			db := openStoreTestDB(t)
			store := download.NewStore(db)
			job, err := store.CreateInitialAttempt(ctx, download.InitialAttemptInput{
				OriginalURL: tc.url,
				StorageRoot: root,
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.rejectWrite {
				execStoreTestSQL(t, db, `
					CREATE TRIGGER test_abort_stage_target
					BEFORE INSERT ON download_target
					BEGIN
						SELECT RAISE(ABORT, 'TEST_ABORT_STAGE_TARGET');
					END
				`)
			}

			var stageCalls atomic.Int32
			checkFrozen := func() error {
				stageCalls.Add(1)
				var matched int
				err := db.QueryRowContext(ctx, `
					SELECT count(*)
					FROM download_queue q
					JOIN download_attempt a ON a.job_id = q.id AND a.id = q.current_attempt_id
					JOIN download_target d ON d.attempt_id = a.id
					WHERE q.id = ? AND a.id = ? AND a.phase = 'downloading'
					  AND d.position = 0 AND d.state = 'pending'
					  AND d.target_key = ? AND d.source_url = ?
					  AND json_extract(d.metadata_json, '$.kind') = ?
				`, job.JobID, job.AttemptID, tc.key, tc.source, tc.kind).Scan(&matched)
				if err != nil {
					return err
				}
				if matched != 1 {
					return fmt.Errorf("저장 시작 시 커밋된 대상 수: %d, 기대값: 1", matched)
				}
				return nil
			}

			runner := download.Runner{Storage: download.Storage{Root: job.StorageRoot}}
			if tc.gallery {
				var body bytes.Buffer
				if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
					t.Fatal(err)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if err := checkFrozen(); err != nil {
						t.Error(err)
						http.Error(w, "대상 기록을 찾지 못했습니다", http.StatusInternalServerError)
						return
					}
					if r.URL.RequestURI() != "/image.png?token=fixture" {
						t.Errorf("이미지 요청 URI: %q", r.URL.RequestURI())
						http.NotFound(w, r)
						return
					}
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(body.Bytes())
				}))
				t.Cleanup(server.Close)

				runner.GalleryDL = galleryDLDiscoverFunc(func(
					_ context.Context, rawURL, _ string,
				) (engine.GalleryDLDiscovery, error) {
					if rawURL != tc.url {
						return engine.GalleryDLDiscovery{}, fmt.Errorf("gallery-dl 요청 URL 불일치: %q", rawURL)
					}
					raw := fmt.Sprintf(`[
						[2, {"category":"instagram","subcategory":"post","post_shortcode":"Fixture_123",
						     "typename":"GraphImage","post_id":%q,"count":"1"}],
						[3, %q, {"category":"instagram","subcategory":"post","post_shortcode":"Fixture_123",
						          "typename":"GraphImage","post_id":%q,"media_id":%q,
						          "count":"1","extension":"png"}]
					]`, instagramID, server.URL+"/image.png?token=fixture", instagramID, instagramID)
					return engine.ParseGalleryDLDiscovery([]byte(raw))
				})
			} else {
				runner.Ytdlp = stagingYtdlp{
					ytdlpDiscoverFunc: func(
						_ context.Context, rawURL, _ string,
					) (json.RawMessage, error) {
						if rawURL != tc.url {
							return nil, fmt.Errorf("yt-dlp 요청 URL 불일치: %q", rawURL)
						}
						return json.RawMessage(fmt.Sprintf(`{
							"id":"BaW_jenozKc","extractor_key":"Youtube","webpage_url":%q
						}`, youtubeSource)), nil
					},
					save: func(target engine.YtdlpTarget, dir string) (string, error) {
						if err := checkFrozen(); err != nil {
							return "", err
						}
						if target.TargetKey != tc.key || target.WebpageURL != tc.source {
							return "", errors.New("조회 대상과 저장 대상이 다릅니다")
						}
						path := filepath.Join(dir, "fixture.mp4")
						return path, os.WriteFile(path, []byte("stage fixture"), 0o600)
					},
				}
			}

			attempt, err := runner.DiscoverAttempt(ctx, job.JobID, job.AttemptID, tc.url, engine.Options{})
			if err != nil {
				t.Fatal(err)
			}
			files, err := runner.FreezeAndStageAttempt(ctx, store, attempt)
			if tc.rejectWrite || tc.unsupported {
				if tc.rejectWrite && (err == nil || !strings.Contains(err.Error(), "TEST_ABORT_STAGE_TARGET")) {
					t.Fatalf("대상 기록 실패를 기대했습니다: %v", err)
				}
				if tc.unsupported && !errors.Is(err, download.ErrUnsupportedTargetDiscovery) {
					t.Fatalf("지원하지 않는 대상 오류를 기대했습니다: %v", err)
				}
				if stageCalls.Load() != 0 || len(files) != 0 {
					t.Fatalf("저장 호출=%d, 저장 결과=%v", stageCalls.Load(), files)
				}

				var phase string
				var count int
				err := db.QueryRow(`
					SELECT phase, (SELECT count(*) FROM download_target WHERE attempt_id = a.id)
					FROM download_attempt a WHERE id = ?
				`, job.AttemptID).Scan(&phase, &count)
				if err != nil || phase != "discovering" || count != 0 {
					t.Fatalf("실패 후 시도 단계=%q, 대상 수=%d, 오류=%v", phase, count, err)
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}
			if stageCalls.Load() != 1 || len(files) != 1 || files[0].Position != 0 ||
				filepath.Dir(files[0].Path) != filepath.Join(attempt.StageDir(), "item-0001") {
				t.Fatalf("저장 호출=%d, 저장 결과=%v", stageCalls.Load(), files)
			}
			var phase, state, path string
			err = db.QueryRow(`
				SELECT a.phase, d.state, d.staged_path
				FROM download_attempt a
				JOIN download_target d ON d.attempt_id = a.id
				WHERE a.job_id = ? AND a.id = ? AND d.position = 0 AND d.target_key = ?
			`, job.JobID, job.AttemptID, tc.key).Scan(&phase, &state, &path)
			if err != nil || phase != "verifying" || state != "downloaded" ||
				path != files[0].Path {
				t.Fatalf("저장 후 phase=%q state=%q path=%q 오류=%v", phase, state, path, err)
			}
		})

	}
}
