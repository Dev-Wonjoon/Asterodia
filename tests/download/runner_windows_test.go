package download_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/download"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
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

func (f galleryDLDiscoverFunc) Download(ctx context.Context, url string, d engine.GalleryDLDiscovery, dir string) ([]string, error) {
	targets, err := d.Targets(url)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(targets))
	for i, target := range targets {
		file := d.Files[target.FileIndex]
		var meta struct {
			Extension string            `json:"extension"`
			Headers   map[string]string `json:"_http_headers"`
		}
		if err = json.Unmarshal(file.Metadata, &meta); err != nil {
			return nil, err
		}
		item := filepath.Join(dir, fmt.Sprintf("item-%04d", i+1))
		if err = os.Mkdir(item, 0700); err != nil {
			return nil, err
		}
		req, e := http.NewRequestWithContext(ctx, "GET", file.URL, nil)
		if e != nil {
			return nil, e
		}
		for key, value := range meta.Headers {
			req.Header.Set(key, value)
		}
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			return nil, e
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			if res.StatusCode == 404 {
				return nil, fmt.Errorf("%w (HTTP 404)", engine.ErrMediaUnavailable)
			}
			return nil, fmt.Errorf("fixture HTTP %d", res.StatusCode)
		}
		path := filepath.Join(item, "media."+meta.Extension)
		out, e := os.Create(path)
		if e != nil {
			res.Body.Close()
			return nil, e
		}
		_, e = io.Copy(out, res.Body)
		out.Close()
		res.Body.Close()
		if e != nil {
			return nil, e
		}
		paths = append(paths, path)
	}
	return paths, nil
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
			} else if len(result.GalleryDLDiscovery.Files) != 1 ||
				result.GalleryDLDiscovery.Files[0].URL != "https://cdn.invalid/image.png?token=abc" {
				t.Fatalf("gallery-dl 대상 불일치: %+v", result.GalleryDLDiscovery.Files)
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
	if os.Getenv("ASTERODIA_LIVE_TESTS") != "1" {
		t.Skip("실제 사이트 검증은 ASTERODIA_LIVE_TESTS=1로 실행하세요")
	}
	ytdlpPath := os.Getenv("ASTERODIA_TEST_YTDLP_EXE")
	galleryPath := os.Getenv("ASTERODIA_TEST_GALLERYDL_EXE")
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
		{engine.KindYtdlp, "https://raw.githubusercontent.com/chromium/chromium/main/media/test/data/bear.mp4?asterodia=m4"},
		{engine.KindGalleryDL, "https://bsky.app/profile/fruttymoment.bsky.social/post/3lcdqq3x47s2h?asterodia=m4"},
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
			name:    "unidentifiable-gallery-target-skips-stage",
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
					if tc.unsupported {
						return engine.ParseGalleryDLDiscovery([]byte(`[[2,{"category":"future-site"}],[3,"https://cdn.invalid/image.png",{"extension":"png"}]]`))
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
							"id":"BaW_jenozKc","extractor_key":"Youtube","webpage_url":%q,
							"format_id":"http","url":"https://cdn.invalid/video.mp4","ext":"mp4","vcodec":"h264","acodec":"aac"
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

func TestRunnerGenericPreflight(t *testing.T) {
	root := downloadTestRoot(t)
	for _, mode := range []string{"direct", "second-hls", "second-empty", "embedded-hls", "second-unsupported-protocol", "freeze-fails"} {
		t.Run(mode, func(t *testing.T) {
			db := openStoreTestDB(t)
			store := download.NewStore(db)
			page := "https://example.invalid/page?view=fixture"
			job, e := store.CreateInitialAttempt(t.Context(), download.InitialAttemptInput{OriginalURL: page, StorageRoot: root})
			if e != nil {
				t.Fatal(e)
			}
			if mode == "freeze-fails" {
				execStoreTestSQL(t, db, `CREATE TRIGGER reject_generic BEFORE INSERT ON download_target WHEN NEW.position=1 BEGIN SELECT RAISE(ABORT,'generic fixture'); END`)
			}
			var stageCalls int
			runner := download.Runner{Storage: download.Storage{Root: root}, Ytdlp: stagingYtdlp{
				ytdlpDiscoverFunc: func(context.Context, string, string) (json.RawMessage, error) {
					entries := []any{}
					for i, id := range []string{"one", "two"} {
						row := map[string]any{"id": id, "extractor_key": "Generic", "webpage_url": page, "format_id": "http", "url": "https://cdn.invalid/" + id + ".mp4", "ext": "mp4", "protocol": "https", "vcodec": "h264", "acodec": "aac"}
						if mode == "embedded-hls" {
							row["extractor_key"] = "HTML5MediaEmbed"
						}
						if i == 1 && (mode == "second-hls" || mode == "embedded-hls") {
							row["protocol"] = "m3u8_native"
						}
						if i == 1 && mode == "second-empty" {
							delete(row, "format_id")
						}
						if i == 1 && mode == "second-unsupported-protocol" {
							row["protocol"] = "rtmp"
						}
						entries = append(entries, row)
					}
					raw, _ := json.Marshal(map[string]any{"_type": "playlist", "entries": entries, "n_entries": 2})
					return raw, nil
				},
				save: func(target engine.YtdlpTarget, dir string) (string, error) {
					stageCalls++
					var frozen int
					if e := db.QueryRow("SELECT count(*) FROM download_target WHERE attempt_id=?", job.AttemptID).Scan(&frozen); e != nil || frozen != 2 {
						return "", fmt.Errorf("저장 전 전체 대상 미확정: %d %v", frozen, e)
					}
					path := filepath.Join(dir, "media.mp4")
					return path, os.WriteFile(path, []byte("fixture "+target.ID), 0600)
				},
			}}
			attempt, e := runner.DiscoverAttempt(t.Context(), job.JobID, job.AttemptID, page, engine.Options{})
			if e != nil {
				t.Fatal(e)
			}
			files, e := runner.FreezeAndStageAttempt(t.Context(), store, attempt)
			if mode == "direct" || mode == "second-hls" || mode == "embedded-hls" {
				if e != nil || stageCalls != 2 || len(files) != 2 {
					t.Fatalf("Generic 저장 실패: %v %d %v", files, stageCalls, e)
				}
				return
			}
			if e == nil || stageCalls != 0 || len(files) != 0 {
				t.Fatalf("사전 검증 실패 후 저장 실행: %v %d %v", files, stageCalls, e)
			}
			if mode != "freeze-fails" && !errors.Is(e, engine.ErrUnsupportedMediaFormat) {
				t.Fatalf("형식 오류 분류: %v", e)
			}
			var count int
			if e := db.QueryRow("SELECT count(*) FROM download_target WHERE attempt_id=?", job.AttemptID).Scan(&count); e != nil || count != 0 {
				t.Fatalf("부분 대상 확정: %d %v", count, e)
			}
		})
	}
}

func TestRunnerEngineFallback(t *testing.T) {
	for _, tc := range []struct {
		name         string
		primaryError error
		galleryFirst bool
		wantFallback bool
	}{
		{"gallery-unsupported", engine.ErrUnsupportedURL, true, true},
		{"ytdlp-unsupported", engine.ErrUnsupportedURL, false, true},
		{"login", engine.ErrAuthenticationRequired, true, false},
		{"denied", engine.ErrAccessDenied, true, false},
		{"rate-limit", engine.ErrRateLimited, true, false},
		{"cancelled", context.Canceled, true, false},
		{"deadline", context.DeadlineExceeded, true, false},
		{"stop-unconfirmed", engine.ErrStopUnconfirmed, true, false},
		{"output-limit", engine.ErrOutputLimit, true, false},
		{"unsupported-with-rate-limit", errors.Join(engine.ErrUnsupportedURL, engine.ErrRateLimited), true, false},
		{"unsupported-with-stop-unconfirmed", errors.Join(engine.ErrUnsupportedURL, engine.ErrStopUnconfirmed), true, false},
		{"unsupported-with-cancel", errors.Join(engine.ErrUnsupportedURL, context.Canceled), true, false},
		{"network", errors.New("network failure"), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := os.MkdirTemp(downloadTestRoot(t), "fallback-")
			if err != nil {
				t.Fatal(err)
			}
			address := "https://example.invalid/post/1"
			if tc.galleryFirst {
				address = "https://x.com/user/status/123"
			}
			store := download.NewStore(openStoreTestDB(t))
			job, err := store.CreateInitialAttempt(t.Context(), download.InitialAttemptInput{OriginalURL: address, StorageRoot: root})
			if err != nil {
				t.Fatal(err)
			}
			ytCalls, galleryCalls := 0, 0
			runner := download.Runner{Storage: download.Storage{Root: root}, Ytdlp: ytdlpDiscoverFunc(func(ctx context.Context, rawURL, dir string) (json.RawMessage, error) {
				ytCalls++
				if rawURL != address {
					t.Error("원본 URL 변경")
				}
				if !tc.galleryFirst {
					return nil, tc.primaryError
				}
				return json.RawMessage(`{"id":"123","extractor_key":"Generic","webpage_url":"https://example.invalid/post/1","ext":"mp4","vcodec":"h264","acodec":"aac","url":"https://cdn.invalid/file.mp4"}`), nil
			}), GalleryDL: galleryDLDiscoverFunc(func(ctx context.Context, rawURL, dir string) (engine.GalleryDLDiscovery, error) {
				galleryCalls++
				if rawURL != address {
					t.Error("원본 URL 변경")
				}
				if tc.galleryFirst {
					return engine.GalleryDLDiscovery{Files: []engine.GalleryDLFile{{URL: "https://cdn.invalid/partial-plan.png"}}}, tc.primaryError
				}
				return engine.ParseGalleryDLDiscovery([]byte(`[[2,{"category":"future","post_id":"123"}],[3,"https://cdn.invalid/file.png",{"filename":"file","extension":"png"}]]`))
			})}
			attempt, err := runner.DiscoverAttempt(t.Context(), job.JobID, job.AttemptID, address, engine.Options{})
			if tc.wantFallback {
				if err != nil || ytCalls != 1 || galleryCalls != 1 {
					t.Fatal(attempt, err, ytCalls, galleryCalls)
				}
				if (attempt.Engine == engine.KindYtdlp) != tc.galleryFirst {
					t.Fatal("실제 선택 엔진 미반영", attempt.Engine)
				}
				if tc.galleryFirst && len(attempt.GalleryDLDiscovery.Files) != 0 {
					t.Fatal("실패한 조회의 일부 대상이 다른 엔진의 계획에 섞였습니다")
				}
			} else if !errors.Is(err, tc.primaryError) || ytCalls != 0 || galleryCalls != 1 {
				t.Fatal("지원하지 않음 이외의 실패에 폴백", err, ytCalls, galleryCalls)
			}
		})
	}
}

func TestRunnerUnavailableEnginesKeepDiscoveryHistory(t *testing.T) {
	runner := download.Runner{Storage: download.Storage{Root: downloadTestRoot(t)}}
	_, err := runner.DiscoverAttempt(t.Context(), "missing-engines", "attempt", "https://x.com/user/status/123", engine.Options{})
	var exhausted *engine.DiscoveryChainError
	if !errors.Is(err, engine.ErrUnsupportedURL) || !errors.As(err, &exhausted) || len(exhausted.Failures) != 2 ||
		exhausted.Failures[0].Handler != string(engine.KindGalleryDL) || exhausted.Failures[1].Handler != string(engine.KindYtdlp) {
		t.Fatal("사용 가능한 엔진이 없을 때 조회 이력이 유실됐습니다", err)
	}
}
