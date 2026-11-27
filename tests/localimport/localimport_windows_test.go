package localimport_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
	"github.com/Dev-Wonjoon/Asterodia/internal/database"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/localimport"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
)

type inspectFunc func(context.Context, string, string) (engine.LocalMetadata, error)

func (f inspectFunc) Inspect(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
	return f(ctx, path, kind)
}

type fixture struct {
	root, source string
	db           *sql.DB
	service      *localimport.Service
	inspect      inspectFunc
	calls        atomic.Int32
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base := os.Getenv("ASTERODIA_TEST_ROOT")
	if !filepath.IsAbs(base) {
		t.Fatal("격리된 테스트 경로가 필요합니다")
	}
	resolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repo, err = filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(filepath.Join(repo, ".test-data", "local-import"), resolved)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		t.Fatal(".test-data/local-import 실행별 폴더가 필요합니다")
	}
	root, err := os.MkdirTemp(resolved, "fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	db, err := database.Open(t.Context(), filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := &fixture{root: root, source: filepath.Join(root, "source"), db: db}
	if err = os.Mkdir(f.source, 0700); err != nil {
		t.Fatal(err)
	}
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		f.calls.Add(1)
		if ctx.Err() != nil {
			return engine.LocalMetadata{}, ctx.Err()
		}
		w, h := 8, 6
		duration := 0.2
		out := engine.LocalMetadata{Kind: kind, Format: strings.TrimPrefix(filepath.Ext(path), ".")}
		if kind != "audio" {
			out.Width = &w
			out.Height = &h
		}
		if kind != "image" {
			out.Duration = &duration
		}
		return out, nil
	}
	f.service = localimport.NewService(db, inspectFunc(func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		return f.inspect(ctx, path, kind)
	}), func() (string, error) { return f.source, nil }, nil)
	t.Cleanup(f.service.Close)
	return f
}
func ok(t *testing.T, result api.Result) any {
	t.Helper()
	if !result.OK {
		t.Fatalf("서비스 오류: %+v", result.Error)
	}
	return result.Data
}
func count(t *testing.T, f *fixture, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := f.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func pngBytes(t *testing.T, width int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, width, 6))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func wait(t *testing.T, s *localimport.Service, id string, wanted ...string) localimport.Page {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		p := ok(t, s.Get(t.Context(), localimport.Request{JobID: id})).(localimport.Page)
		for _, state := range wanted {
			if p.Job != nil && p.Job.State == state {
				return p
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("가져오기 상태 대기 시간 초과")
	return localimport.Page{}
}
func scan(t *testing.T, f *fixture) localimport.Page {
	t.Helper()
	p := ok(t, f.service.Choose(t.Context())).(localimport.Page)
	return wait(t, f.service, p.Job.ID, "ready", "failed")
}
func run(t *testing.T, f *fixture, p localimport.Page) localimport.Page {
	t.Helper()
	ok(t, f.service.Run(t.Context(), localimport.Request{JobID: p.Job.ID}))
	return wait(t, f.service, p.Job.ID, "completed", "completed_with_errors", "failed", "cancelled", "interrupted")
}
func TestRecursiveMixedMediaAndNaturalOrder(t *testing.T) {
	f := newFixture(t)
	data := pngBytes(t, 8)
	for _, name := range []string{"10.png", "2.png", "1.png", "video.mp4", "audio.mp3", "nested/a.png"} {
		write(t, filepath.Join(f.source, filepath.FromSlash(name)), data)
	}
	write(t, filepath.Join(f.source, "notes.txt"), []byte("not media"))
	write(t, filepath.Join(f.source, ".asterodia", "ignored.png"), data)
	p := scan(t, f)
	if p.Job.Counts.Discovered != 6 || p.Job.Counts.Excluded != 2 {
		t.Fatalf("탐색 집계: %+v", p.Job.Counts)
	}
	p = run(t, f, p)
	if p.Job.Counts.Added != 6 || p.Job.Counts.CompletedFolders != 2 || p.Job.Counts.FailedFolders != 0 {
		t.Fatalf("등록 집계: %+v %+v", p.Job.Counts, p.Folders)
	}
	if count(t, f, "SELECT count(*) FROM media WHERE source_kind='local'") != 2 || count(t, f, "SELECT count(*) FROM media_file WHERE storage_kind='external_reference'") != 6 {
		t.Fatal("외부 참조 묶음 등록 누락")
	}
	var titles string
	if err := f.db.QueryRow("SELECT group_concat(title,',') FROM (SELECT f.title FROM media_file f JOIN media m ON m.id=f.media_id WHERE m.source_title='source' ORDER BY f.position)").Scan(&titles); err != nil {
		t.Fatal(err)
	}
	if titles != "1.png,2.png,10.png,audio.mp3,video.mp4" {
		t.Fatal(titles)
	}
	if count(t, f, "SELECT count(*) FROM pragma_foreign_key_check()") != 0 {
		t.Fatal("외래 키 불일치")
	}
	got, err := os.ReadFile(filepath.Join(f.source, "1.png"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("원본을 변경했습니다")
	}
}
func TestReimportPreservesEditsAndAppendsNewFiles(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.source, "2.png"), pngBytes(t, 8))
	run(t, f, scan(t, f))
	mediaID := count(t, f, "SELECT id FROM media")
	fileID := count(t, f, "SELECT id FROM media_file")
	_, err := f.db.Exec("UPDATE media SET custom_title='사용자 제목',cover_mode='manual',cover_file_id=?,revision=revision+1 WHERE id=?", fileID, mediaID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec("INSERT INTO favorite(media_id,created_at) VALUES(?,1)", mediaID); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(f.source, "1.png"), pngBytes(t, 7))
	p := run(t, f, scan(t, f))
	if p.Job.Counts.Added != 1 || p.Job.Counts.Unchanged != 1 || count(t, f, "SELECT count(*) FROM media") != 1 {
		t.Fatalf("재등록 집계: %+v", p.Job.Counts)
	}
	if count(t, f, "SELECT count(*) FROM media WHERE id=? AND custom_title='사용자 제목' AND cover_file_id=?", mediaID, fileID) != 1 || count(t, f, "SELECT count(*) FROM favorite WHERE media_id=?", mediaID) != 1 {
		t.Fatal("사용자 편집 유실")
	}
	if count(t, f, "SELECT position FROM media_file WHERE title='1.png'") != 1 {
		t.Fatal("신규 파일을 기존 순서 앞에 삽입했습니다")
	}
	if count(t, f, "SELECT count(*) FROM media_file WHERE id=?", fileID) != 1 {
		t.Fatal("기존 파일 ID 변경")
	}
	// Overlapping root must find the original actual folder, not create another.
	f.source = f.root
	p = run(t, f, scan(t, f))
	if count(t, f, "SELECT count(*) FROM media") != 1 {
		t.Fatal("겹치는 루트가 중복 묶음을 생성했습니다")
	}
}
func TestFilePartialSuccessAndContinue(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"bad/1.png", "bad/broken.png", "good/a.png"} {
		write(t, filepath.Join(f.source, filepath.FromSlash(name)), pngBytes(t, 8))
	}
	original := f.inspect
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		if strings.Contains(path, "broken") {
			return engine.LocalMetadata{}, fmt.Errorf("손상 파일")
		}
		return original(ctx, path, kind)
	}
	p := run(t, f, scan(t, f))
	if p.Job.State != "completed_with_errors" || p.Job.Counts.CompletedFolders != 1 || p.Job.Counts.FailedFolders != 1 || p.Job.Counts.Added != 2 || p.Job.Counts.Failed != 1 {
		t.Fatalf("실패/성공 집계: %+v %+v", p.Job.Counts, p.Folders)
	}
	if count(t, f, "SELECT count(*) FROM media_file WHERE filepath LIKE '%bad%'") != 1 {
		t.Fatal("같은 폴더의 정상 파일이 등록되지 않았습니다")
	}
	if count(t, f, "SELECT count(*) FROM media") != 2 {
		t.Fatal("다른 폴더 처리가 중단됐습니다")
	}
}
func TestChangedFilesAndScanSnapshot(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.source, "a.png")
	write(t, path, pngBytes(t, 8))
	run(t, f, scan(t, f))
	calls := f.calls.Load()
	p := run(t, f, scan(t, f))
	if p.Job.Counts.Unchanged != 1 || f.calls.Load() != calls {
		t.Fatal("변경 없는 파일을 재검사했습니다")
	}
	write(t, path, pngBytes(t, 10))
	p = run(t, f, scan(t, f))
	if p.Job.Counts.Updated != 1 {
		t.Fatalf("변경 감지: %+v", p.Job.Counts)
	}
	p = scan(t, f)
	write(t, path, pngBytes(t, 11))
	p = run(t, f, p)
	if p.Job.Counts.FailedFolders != 1 || p.Job.Counts.Added != 0 || p.Job.Counts.Updated != 0 {
		t.Fatalf("탐색 후 변경된 원본을 등록했습니다: %+v", p.Job.Counts)
	}
}
func TestCancelAndRecover(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.source, "a.png"), pngBytes(t, 8))
	started := make(chan struct{})
	f.inspect = func(ctx context.Context, _, _ string) (engine.LocalMetadata, error) {
		close(started)
		<-ctx.Done()
		return engine.LocalMetadata{}, ctx.Err()
	}
	p := scan(t, f)
	ok(t, f.service.Run(t.Context(), localimport.Request{JobID: p.Job.ID}))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("검사 시작 실패")
	}
	ok(t, f.service.Cancel(t.Context(), localimport.Request{JobID: p.Job.ID}))
	p = wait(t, f.service, p.Job.ID, "cancelled")
	if count(t, f, "SELECT count(*) FROM media") != 0 {
		t.Fatal("취소한 폴더가 등록됐습니다")
	}
	// A ready job's durable scan can be identified after interruption.
	p = scan(t, f)
	f.service.Close()
	if _, err := f.db.Exec("UPDATE local_import_job SET state='running' WHERE id=?", p.Job.ID); err != nil {
		t.Fatal(err)
	}
	reopened := localimport.NewService(f.db, nil, nil, nil)
	defer reopened.Close()
	if err := reopened.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	p = wait(t, reopened, p.Job.ID, "interrupted")
	if p.Folders[0].State != "interrupted" {
		t.Fatal(p)
	}
	if _, err := os.Stat(filepath.Join(f.source, "a.png")); err != nil {
		t.Fatal("복구가 원본을 삭제했습니다")
	}
}
func TestUnregisterNeverDeletesLocalOriginals(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.source, "a.png")
	data := pngBytes(t, 8)
	write(t, path, data)
	run(t, f, scan(t, f))
	id := count(t, f, "SELECT id FROM media")
	revision := count(t, f, "SELECT revision FROM media")
	ok(t, media.NewService(f.db).UnregisterMedia(t.Context(), media.UnregisterRequest{MediaID: id, ExpectedRevision: revision}))
	if count(t, f, "SELECT count(*) FROM maintenance_task WHERE kind='deletion_cleanup'") != 0 {
		t.Fatal("외부 원본의 삭제 작업을 만들었습니다")
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, data) {
		t.Fatal("외부 원본을 변경했습니다")
	}
}
func TestExcludeJunctionAndPaginate(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 28; i++ {
		write(t, filepath.Join(f.source, fmt.Sprint(i), "a.png"), pngBytes(t, 8))
	}
	other := filepath.Join(f.root, "outside-selection")
	write(t, filepath.Join(other, "foreign.png"), pngBytes(t, 8))
	link := filepath.Join(f.source, "linked")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, other).CombinedOutput(); err != nil {
		t.Fatalf("테스트 정션: %s %v", output, err)
	}
	p := scan(t, f)
	if p.Job.Counts.Discovered != 28 || p.Job.Counts.Excluded != 1 {
		t.Fatalf("정션 제외: %+v", p.Job.Counts)
	}
	p = ok(t, f.service.Get(t.Context(), localimport.Request{JobID: p.Job.ID, Limit: 25})).(localimport.Page)
	if len(p.Folders) != 25 || !p.HasNextPage || p.Total != 28 {
		t.Fatal(p)
	}
	p = ok(t, f.service.Get(t.Context(), localimport.Request{JobID: p.Job.ID, Limit: 25, Offset: 25})).(localimport.Page)
	if len(p.Folders) != 3 || p.HasNextPage {
		t.Fatal(p)
	}
}

func TestCaseAliasAndCommittedRecoveryCounts(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.source, "a.png"), pngBytes(t, 8))
	p := run(t, f, scan(t, f))
	f.source = strings.ToUpper(f.source)
	p = run(t, f, scan(t, f))
	if p.Job.Counts.Unchanged != 1 || count(t, f, "SELECT count(*) FROM media") != 1 {
		t.Fatal("대소문자 경로 별칭이 중복 등록됐습니다")
	}
	f.service.Close()
	if _, err := f.db.Exec("UPDATE local_import_job SET state='running',counts_json='{}' WHERE id=?", p.Job.ID); err != nil {
		t.Fatal(err)
	}
	reopened := localimport.NewService(f.db, nil, nil, nil)
	defer reopened.Close()
	if err := reopened.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	p = wait(t, reopened, p.Job.ID, "interrupted")
	if p.Job.Counts.CompletedFolders != 1 || p.Job.Counts.Unchanged != 1 {
		t.Fatal("커밋 이후 집계 저장 전 중단된 결과를 복원하지 못했습니다")
	}
}

func TestRescanSharedDirectoryKeepsExistingPostGroups(t *testing.T) {
	f := newFixture(t)
	data := pngBytes(t, 8)
	write(t, filepath.Join(f.source, "loose.png"), data)
	run(t, f, scan(t, f))
	original := count(t, f, "SELECT id FROM media")
	directory := count(t, f, "SELECT id FROM media_directory WHERE import_media_id=?", original)
	path := filepath.Join(f.source, "post-image.png")
	write(t, path, data)
	_, key, err := media.CanonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec("INSERT INTO download_urls(id,url,normalized_url,normalization_version,created_at) VALUES(1,'https://example.test/post','https://example.test/post',1,1); INSERT INTO media(id,source_kind,request_url_id,source_title,custom_title,created_at) VALUES(100,'download',1,'게시글 원본','보존할 제목',1); INSERT INTO favorite(media_id,created_at) VALUES(100,1)"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`INSERT INTO media_file(media_id,file_key,storage_kind,directory_id,title,filepath,canonical_path_key,relative_path,filesize,mtime_ns,hash_algorithm,content_hash,kind,position,detected_format,width,height,created_at) VALUES(100,'post-photo','external_reference',?,'게시글 사진',?,?,?,?,'1','sha256',?,'image',0,'png',8,6,1)`, directory, path, key, filepath.Base(path), len(data), fmt.Sprintf("%x", sha256.Sum256(data))); err != nil {
		t.Fatal(err)
	}
	var identity string
	if err = f.db.QueryRow("SELECT file_uuid FROM media_file WHERE media_id=100").Scan(&identity); err != nil {
		t.Fatal(err)
	}
	result := run(t, f, scan(t, f))
	if result.Job.Counts.Added != 0 || result.Job.Counts.Unchanged != 2 || count(t, f, "SELECT count(*) FROM media") != 2 {
		t.Fatalf("공유 폴더 재탐색이 게시글을 합쳤습니다: %+v", result.Job.Counts)
	}
	if count(t, f, "SELECT count(*) FROM media_file WHERE media_id=100 AND file_uuid=? AND directory_id=?", identity, directory) != 1 || count(t, f, "SELECT count(*) FROM media WHERE id=100 AND custom_title='보존할 제목'") != 1 || count(t, f, "SELECT count(*) FROM favorite WHERE media_id=100") != 1 {
		t.Fatal("게시글 식별자·편집 정보 유실")
	}
}
