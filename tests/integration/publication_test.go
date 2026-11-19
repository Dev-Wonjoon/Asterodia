package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/maintenance"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"modernc.org/sqlite"
)

func publicationMetadata(f *fixture, title, owner string, count int) {
	original := f.gallery
	f.gallery = func(ctx context.Context, url, dir string) (engine.GalleryDLDiscovery, error) {
		discovery, err := original(ctx, url, dir)
		if err != nil {
			return discovery, err
		}
		var metadata map[string]any
		if err = json.Unmarshal(discovery.Directories[0], &metadata); err != nil {
			return discovery, err
		}
		metadata["description"], metadata["owner_id"], metadata["count"] = title, owner, count
		if count > 1 {
			metadata["typename"] = "GraphSidecar"
		}
		discovery.Directories[0], _ = json.Marshal(metadata)
		originalFile := discovery.Files[0]
		discovery.Files = nil
		for i := 0; i < count; i++ {
			metadata["media_id"], metadata["num"] = fmt.Sprint(123456+i), i+1
			file := originalFile
			file.Metadata, _ = json.Marshal(metadata)
			discovery.Files = append(discovery.Files, file)
		}
		return discovery, nil
	}
}

func publishedPath(t *testing.T, f *fixture, id int64) string {
	t.Helper()
	var path string
	if err := f.db.QueryRow("SELECT filepath FROM media_file WHERE media_id=? ORDER BY position LIMIT 1", id).Scan(&path); err != nil {
		t.Fatal(err)
	}
	return path
}

func unregisterPublication(t *testing.T, f *fixture, id int64) {
	t.Helper()
	fileID := queryInt(t, f.db, "SELECT id FROM media_file WHERE media_id=?", id)
	revision := queryInt(t, f.db, "SELECT revision FROM media WHERE id=?", id)
	success(t, f.catalog.UnregisterFiles(t.Context(), media.UnregisterRequest{MediaID: id, ExpectedRevision: revision, FileIDs: []int64{fileID}}))
	cleaner := maintenance.NewCleaner(f.db)
	defer cleaner.Close()
	if err := cleaner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationPaths(t *testing.T) {
	for _, tc := range []struct{ name, title, owner, expected string }{
		{"normal", "고양이 일상", "account_42", "instagram/account_42/고양이 일상.png"},
		{"reserved", "CON.txt", "CON", "instagram/_CON/_CON.txt.png"},
		{"reserved-unicode", "COM¹", "42", "instagram/42/_COM¹.png"},
		{"id-fallback", "...", "", "instagram/unknown/123456.png"},
		{"invalid", "../../제목:<bad>?*\x01. ", "42", ""},
		{"long-unicode", strings.Repeat("가😀", 100), "42", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			publicationMetadata(f, tc.title, tc.owner, 1)
			path := publishedPath(t, f, f.run(t, f.enqueue(t)))
			rel, err := filepath.Rel(filepath.Join(f.root, "downloads"), path)
			if err != nil || !filepath.IsLocal(rel) || len(strings.Split(rel, string(filepath.Separator))) != 3 {
				t.Fatalf("저장 경로 탈출: %q %v", path, err)
			}
			var token string
			if err = f.db.QueryRow("SELECT filename_token FROM media_file WHERE filepath=?", path).Scan(&token); err != nil {
				t.Fatal(err)
			}
			normalized := strings.TrimSuffix(path, filepath.Ext(path))
			if !strings.HasSuffix(normalized, "_"+token) {
				t.Fatal("파일명과 DB 토큰 불일치", path, token)
			}
			normalized = strings.TrimSuffix(normalized, "_"+token) + filepath.Ext(path)
			expectedRel, _ := filepath.Rel(filepath.Join(f.root, "downloads"), normalized)
			if tc.expected != "" && filepath.ToSlash(expectedRel) != tc.expected {
				t.Fatalf("경로: %q, 기대: %q", rel, tc.expected)
			}
			name := filepath.Base(path)
			if strings.ContainsAny(name, "<>:\"/\\|?*\x01") || strings.HasSuffix(name, " ") || len(utf16.Encode([]rune(name))) > 135 {
				t.Fatalf("안전하지 않은 파일명: %q", name)
			}
			if _, err = os.Stat(path); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPublicationCollisionAndMultipleFiles(t *testing.T) {
	f := newFixture(t)
	publicationMetadata(f, "게시물", "42", 2)
	foreign := filepath.Join(f.root, "downloads", "instagram", "42", "게시물.png")
	if err := os.MkdirAll(filepath.Dir(foreign), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("existing user file")
	if err := os.WriteFile(foreign, original, 0600); err != nil {
		t.Fatal(err)
	}
	id := f.run(t, f.enqueue(t))
	rows, err := f.db.Query("SELECT filepath FROM media_file WHERE media_id=? ORDER BY position", id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	i := 2
	for rows.Next() {
		var path string
		if err = rows.Scan(&path); err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^게시물_[0-9a-f]{8}\.png$`).MatchString(filepath.Base(path)) {
			t.Fatal(path)
		}
		i++
	}
	if err = rows.Err(); err != nil || i != 4 {
		t.Fatalf("여러 파일 등록 누락: %d %v", i, err)
	}
	if got, err := os.ReadFile(foreign); err != nil || !bytes.Equal(got, original) {
		t.Fatal("기존 파일이 덮어써졌습니다")
	}
}

func TestPublicationCleanerPreservesChangedFiles(t *testing.T) {
	for _, mode := range []string{"same-content-replacement", "modified-original"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			id := f.run(t, f.enqueue(t))
			path := publishedPath(t, f, id)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "same-content-replacement" {
				// Keep the old file alive, guaranteeing the replacement has a new ID.
				if err = os.Rename(path, filepath.Join(f.root, "moved-original.png")); err != nil {
					t.Fatal(err)
				}
			} else {
				data = append(data, []byte("user edit")...)
			}
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			unregisterPublication(t, f, id)
			if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, data) {
				t.Fatal("사용자가 교체하거나 수정한 파일을 삭제했습니다")
			}
			if queryInt(t, f.db, "SELECT count(*) FROM maintenance_task WHERE kind='deletion_cleanup' AND state='failed'") == 0 {
				t.Fatal("소유권 불일치를 기록하지 않았습니다")
			}
		})
	}
}

func TestPublicationRejectsJunction(t *testing.T) {
	f := newFixture(t)
	root := filepath.Join(f.root, "downloads")
	target := filepath.Join(f.root, "other-files")
	for _, dir := range []string{root, target} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "instagram")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("격리된 정션 생성: %s %v", output, err)
	}
	job := f.enqueue(t)
	errorCode(t, f.service.Run(t.Context(), download.RunRequest{JobID: job.ID, Trigger: "manual_initial"}), "DOWNLOAD_ENGINE_FAILED")
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatalf("정션을 따라 다른 폴더에 저장했습니다: %v %v", entries, err)
	}
}

func TestPublicationConcurrentCollision(t *testing.T) {
	const count = 4
	fixtures := make([]*fixture, count)
	jobs := make([]download.Job, count)
	for i := range fixtures {
		f := newFixture(t)
		fixtures[i] = f
		shared := filepath.Join(fixtures[0].root, "downloads")
		f.service = download.NewService(f.db, func() string { return shared }, func(root string, _ engine.Options) download.Runner {
			return download.Runner{Storage: download.Storage{Root: root}, GalleryDL: f.gallery}
		}, nil)
		t.Cleanup(f.service.Close)
		jobs[i] = f.enqueue(t)
	}
	start := make(chan struct{})
	results := make(chan api.Result, count)
	for i, f := range fixtures {
		go func() {
			<-start
			results <- f.service.Run(t.Context(), download.RunRequest{JobID: jobs[i].ID, Trigger: "manual_initial"})
		}()
	}
	close(start)
	for range count {
		success(t, <-results)
	}
	paths := map[string]bool{}
	for _, f := range fixtures {
		path := publishedPath(t, f, queryInt(t, f.db, "SELECT id FROM media"))
		if paths[path] {
			t.Fatalf("동시 다운로드가 같은 경로를 사용했습니다: %s", path)
		}
		paths[path] = true
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPublicationCrashRecovery(t *testing.T) {
	if os.Getenv("STORAGEBUCKET_PUBLICATION_CRASH_CHILD") == "1" {
		if err := sqlite.RegisterScalarFunction("fixture_crash", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
			os.Exit(77)
			return nil, nil
		}); err != nil {
			t.Fatal(err)
		}
		f := newFixture(t)
		if err := os.WriteFile(filepath.Join(os.Getenv("STORAGEBUCKET_TEST_ROOT"), "child-root.txt"), []byte(f.root), 0600); err != nil {
			t.Fatal(err)
		}
		foreign := filepath.Join(f.root, "downloads", "instagram", "unknown", "Instagram Fixture.png")
		if err := os.MkdirAll(filepath.Dir(foreign), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(foreign, []byte("user file"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec("CREATE TRIGGER fixture_crash_trigger BEFORE INSERT ON media_file BEGIN SELECT fixture_crash(); END"); err != nil {
			t.Fatal(err)
		}
		f.run(t, f.enqueue(t))
		t.Fatal("파일 배치 후 DB 등록 지점에서 종료되지 않았습니다")
	}
	f := newFixture(t)
	childBase := filepath.Join(f.root, "crash-child")
	if err := os.Mkdir(childBase, 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestPublicationCrashRecovery$", "-test.timeout=30s")
	for _, value := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(value), "STORAGEBUCKET_TEST_ROOT=") && !strings.HasPrefix(strings.ToUpper(value), "STORAGEBUCKET_PUBLICATION_CRASH_CHILD=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "STORAGEBUCKET_TEST_ROOT="+childBase, "STORAGEBUCKET_PUBLICATION_CRASH_CHILD=1")
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 77 {
		t.Fatalf("파일 배치 후 종료 실패: %s %v", output, err)
	}
	encoded, err := os.ReadFile(filepath.Join(childBase, "child-root.txt"))
	if err != nil {
		t.Fatal(err)
	}
	childRoot := string(encoded)
	resolved, err := filepath.EvalSymlinks(childRoot)
	if err != nil || filepath.Dir(resolved) != childBase {
		t.Fatalf("자식 테스트 경로가 격리 폴더를 벗어났습니다: %q %v", childRoot, err)
	}
	filename := filepath.Join(childRoot, "library.db")
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DROP TRIGGER fixture_crash_trigger"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	db, err = database.Open(t.Context(), filename)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var job, path string
	if err = db.QueryRow("SELECT a.job_id,t.final_path FROM download_target t JOIN download_attempt a ON a.id=t.attempt_id").Scan(&job, &path); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^Instagram Fixture_[0-9a-f]{8}\.png$`).MatchString(filepath.Base(path)) {
		t.Fatalf("최종 경로 배치 전에 종료됐습니다: %s", path)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if queryInt(t, db, "SELECT count(*) FROM media_file") != 0 {
		t.Fatal("미완료 등록 트랜잭션이 커밋되었습니다")
	}
	svc := download.NewService(db, func() string { return filepath.Join(childRoot, "downloads") }, func(root string, _ engine.Options) download.Runner {
		return download.Runner{Storage: download.Storage{Root: root}, GalleryDL: f.gallery}
	}, nil)
	defer svc.Close()
	if err = svc.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("미커밋 공개 파일이 정리되지 않았습니다: %v", err)
	}
	foreign := filepath.Join(filepath.Dir(path), "Instagram Fixture.png")
	if got, err := os.ReadFile(foreign); err != nil || string(got) != "user file" {
		t.Fatal("복구가 기존 사용자 파일을 변경했습니다")
	}
	got := success(t, svc.Get(t.Context(), download.JobRequest{JobID: job})).(download.Job)
	if got.State != "interrupted" || got.CleanupRequired {
		t.Fatalf("복구 상태: %+v", got)
	}
	success(t, svc.Run(t.Context(), download.RunRequest{JobID: job, Trigger: "manual_restart"}))
	if queryInt(t, db, "SELECT count(*) FROM media_file") != 1 {
		t.Fatal("복구 후 다시 다운로드할 수 없습니다")
	}
	assertFK(t, db)
}
