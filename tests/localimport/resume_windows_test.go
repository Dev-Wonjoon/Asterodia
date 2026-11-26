package localimport_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/localimport"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

func TestPauseResumeWithinOneFolderPreservesEdits(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"1.png", "2.png", "3.png"} {
		write(t, filepath.Join(f.source, name), pngBytes(t, 8))
	}
	started := make(chan struct{})
	original := f.inspect
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		if filepath.Base(path) == "2.png" {
			close(started)
			<-ctx.Done()
			return engine.LocalMetadata{}, ctx.Err()
		}
		return original(ctx, path, kind)
	}
	p := scan(t, f)
	ok(t, f.service.Run(t.Context(), localimport.Request{JobID: p.Job.ID}))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("두 번째 파일 검사 대기 실패")
	}
	// Long inspection must neither hold the DB connection nor block pause.
	current := ok(t, f.service.Get(t.Context(), localimport.Request{JobID: p.Job.ID})).(localimport.Page)
	if current.Job.Counts.Added != 1 || current.Job.Counts.Processed != 1 || current.Job.CurrentPath == "" {
		t.Fatalf("실시간 진행 상태: %+v", current.Job)
	}
	id := count(t, f, "SELECT id FROM media")
	revision := count(t, f, "SELECT revision FROM media")
	fileID := count(t, f, "SELECT id FROM media_file")
	if r := media.NewService(f.db).Edit(t.Context(), media.EditRequest{MediaID: id, ExpectedRevision: revision}); r.OK || r.Error.Code != "JOB_ACTIVE" {
		t.Fatal("진행 중 묶음 편집을 차단하지 못했습니다")
	}
	ok(t, f.service.Pause(t.Context(), localimport.Request{JobID: p.Job.ID}))
	p = wait(t, f.service, p.Job.ID, "paused")
	if p.Job.Counts.Added != 1 || count(t, f, "SELECT count(*) FROM media_file") != 1 {
		t.Fatal("중단이 완료 파일을 되돌렸습니다")
	}
	title := "중단 중 수정한 제목"
	encodedTitle, _ := json.Marshal(title)
	ok(t, media.NewService(f.db).Edit(t.Context(), media.EditRequest{MediaID: id, ExpectedRevision: revision, CustomTitle: encodedTitle}))
	f.inspect = original
	// Parallel inspection may finish an uncommitted later file before pause.
	// Resume repeats only pending work, while preserving the first checkpoint.
	callsBeforeResume := f.calls.Load()
	ok(t, f.service.Resume(t.Context(), localimport.Request{JobID: p.Job.ID}))
	p = wait(t, f.service, p.Job.ID, "completed")
	if p.Job.Counts.Added != 3 || p.Job.Counts.Processed != 3 || p.Job.Counts.ProcessedBytes != p.Job.Counts.TotalBytes || f.calls.Load() != callsBeforeResume+2 {
		t.Fatalf("이어하기 중복 검사/집계: %+v calls=%d", p.Job.Counts, f.calls.Load())
	}
	if count(t, f, "SELECT count(*) FROM media WHERE id=? AND custom_title=?", id, title) != 1 || count(t, f, "SELECT count(*) FROM media_file WHERE id=?", fileID) != 1 {
		t.Fatal("기존 편집 또는 파일 ID 유실")
	}
}

func TestPauseResumeAcrossFoldersJoinsSharedWorkers(t *testing.T) {
	workers := max(1, runtime.NumCPU()/2)
	if workers < 2 {
		t.Skip("requires at least two import workers")
	}
	f := newFixture(t)
	total := workers + 2
	for i := range total {
		write(t, filepath.Join(f.source, fmt.Sprintf("%03d", i), "file.png"), []byte{1})
	}
	original := f.inspect
	entered := make(chan struct{}, total)
	var active, peak atomic.Int32
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		if filepath.Base(filepath.Dir(path)) == "000" {
			return original(ctx, path, kind)
		}
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		entered <- struct{}{}
		<-ctx.Done()
		return engine.LocalMetadata{}, ctx.Err()
	}
	p := scan(t, f)
	ok(t, f.service.Run(t.Context(), localimport.Request{JobID: p.Job.ID}))
	awaitInspections(t, entered, workers)
	if active.Load() != int32(workers) || peak.Load() != int32(workers) {
		t.Fatalf("shared workers: active=%d peak=%d", active.Load(), peak.Load())
	}
	ok(t, f.service.Pause(t.Context(), localimport.Request{JobID: p.Job.ID}))
	p = wait(t, f.service, p.Job.ID, "paused")
	if active.Load() != 0 || p.Job.Counts.Added != 1 {
		t.Fatalf("pause left workers or lost checkpoint: active=%d counts=%+v", active.Load(), p.Job.Counts)
	}
	first := count(t, f, "SELECT id FROM media_file")
	f.inspect = original
	ok(t, f.service.Resume(t.Context(), localimport.Request{JobID: p.Job.ID}))
	p = wait(t, f.service, p.Job.ID, "completed")
	if p.Job.Counts.Added != total || p.Job.Counts.Processed != total || p.Job.Counts.CompletedFolders != total || f.calls.Load() != int32(total) {
		t.Fatal(p.Job.Counts)
	}
	if count(t, f, "SELECT count(*) FROM media_file WHERE id=?", first) != 1 || count(t, f, "SELECT count(DISTINCT file_uuid) FROM media_file") != int64(total) || count(t, f, "SELECT count(*) FROM pragma_foreign_key_check()") != 0 {
		t.Fatal("resume lost a checkpoint, duplicated a file or broke a foreign key")
	}
}

func TestRetryExcludeAndNewScan(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"1.png", "broken.png", "skip.png"} {
		write(t, filepath.Join(f.source, name), pngBytes(t, 8))
	}
	original := f.inspect
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		if filepath.Base(path) == "broken.png" {
			return engine.LocalMetadata{}, fmt.Errorf("손상 파일")
		}
		return original(ctx, path, kind)
	}
	p := scan(t, f)
	files := ok(t, f.service.Files(t.Context(), localimport.Request{JobID: p.Job.ID, FileState: "pending", Limit: 2})).(localimport.FilePage)
	if len(files.Files) != 2 || files.Total != 3 || !files.HasNextPage {
		t.Fatal(files)
	}
	files = ok(t, f.service.Files(t.Context(), localimport.Request{JobID: p.Job.ID, Offset: 2, Limit: 2})).(localimport.FilePage)
	excluded := files.Files[0].ID
	ok(t, f.service.Exclude(t.Context(), localimport.Request{JobID: p.Job.ID, TargetID: excluded}))
	p = run(t, f, p)
	if p.Job.State != "completed_with_errors" || p.Job.Counts.Added != 1 || p.Job.Counts.Failed != 1 || p.Job.Counts.UserExcluded != 1 || p.Job.Counts.Processed != 3 {
		t.Fatalf("부분 성공/제외: %+v", p.Job)
	}
	failed := ok(t, f.service.Files(t.Context(), localimport.Request{JobID: p.Job.ID, FileState: "failed"})).(localimport.FilePage)
	if len(failed.Files) != 1 || failed.Files[0].Error == nil {
		t.Fatal("파일별 실패 사유 유실")
	}
	f.inspect = original
	// An explicit retry may accept a file changed since the original scan.
	write(t, filepath.Join(f.source, "broken.png"), pngBytes(t, 12))
	ok(t, f.service.Retry(t.Context(), localimport.Request{JobID: p.Job.ID, AllFailed: true}))
	ok(t, f.service.Resume(t.Context(), localimport.Request{JobID: p.Job.ID}))
	p = wait(t, f.service, p.Job.ID, "completed")
	if p.Job.Counts.Failed != 0 || p.Job.Counts.Added != 2 || p.Job.Counts.UserExcluded != 1 || p.Job.Counts.TotalBytes != p.Job.Counts.ProcessedBytes {
		t.Fatal(p.Job.Counts)
	}
	// Exclusion applies to this job only; a new scan sees the skipped original.
	p = run(t, f, scan(t, f))
	if p.Job.Counts.Added != 1 || p.Job.Counts.Unchanged != 2 || count(t, f, "SELECT count(*) FROM media_file") != 3 {
		t.Fatal(p.Job.Counts)
	}
}

func TestExcludeAllFailuresReconcilesFolder(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.source, "bad.png"), pngBytes(t, 8))
	f.inspect = func(context.Context, string, string) (engine.LocalMetadata, error) {
		return engine.LocalMetadata{}, fmt.Errorf("손상")
	}
	p := run(t, f, scan(t, f))
	ok(t, f.service.Exclude(t.Context(), localimport.Request{JobID: p.Job.ID, AllFailed: true}))
	ok(t, f.service.Resume(t.Context(), localimport.Request{JobID: p.Job.ID}))
	p = wait(t, f.service, p.Job.ID, "completed")
	if p.Job.Counts.Failed != 0 || p.Job.Counts.FailedFolders != 0 || p.Job.Counts.UserExcluded != 1 || count(t, f, "SELECT count(*) FROM media") != 0 {
		t.Fatal(p.Job.Counts)
	}
}

func TestImportProcessCrash(t *testing.T) {
	if os.Getenv("STORAGEBUCKET_IMPORT_CRASH") == "1" {
		path := os.Getenv("STORAGEBUCKET_IMPORT_DB")
		base := os.Getenv("STORAGEBUCKET_TEST_ROOT")
		rel, err := filepath.Rel(base, path)
		if err != nil || !filepath.IsAbs(base) || !filepath.IsLocal(rel) {
			t.Fatal("격리 DB가 필요합니다")
		}
		db, err := database.Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		inspector := inspectFunc(func(_ context.Context, path, kind string) (engine.LocalMetadata, error) {
			if filepath.Base(path) == "2.png" {
				os.Exit(74)
			}
			w, h := 8, 6
			return engine.LocalMetadata{Kind: kind, Format: "png", Width: &w, Height: &h}, nil
		})
		service := localimport.NewService(db, inspector, nil, nil)
		defer service.Close()
		ok(t, service.Run(t.Context(), localimport.Request{JobID: os.Getenv("STORAGEBUCKET_IMPORT_JOB")}))
		wait(t, service, os.Getenv("STORAGEBUCKET_IMPORT_JOB"), "completed")
		t.Fatal("강제 종료 지점 미도달")
	}
	f := newFixture(t)
	for _, name := range []string{"1.png", "2.png"} {
		write(t, filepath.Join(f.source, name), pngBytes(t, 8))
	}
	p := scan(t, f)
	f.service.Close()
	f.db.Close()
	path := filepath.Join(f.root, "library.db")
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestImportProcessCrash$", "-test.count=1")
	command.Env = append(os.Environ(), "STORAGEBUCKET_IMPORT_CRASH=1", "STORAGEBUCKET_IMPORT_DB="+path, "STORAGEBUCKET_IMPORT_JOB="+p.Job.ID)
	output, err := command.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 74 {
		t.Fatalf("예상하지 않은 종료: %v %s", err, output)
	}
	f.db, err = database.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.db.Close()
	if count(t, f, "SELECT count(*) FROM media_file") != 1 {
		t.Fatal("강제 종료 이전 파일 확정 유실")
	}
	first := count(t, f, "SELECT id FROM media_file")
	f.service = localimport.NewService(f.db, inspectFunc(f.inspect), nil, nil)
	defer f.service.Close()
	if err = f.service.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	p = wait(t, f.service, p.Job.ID, "interrupted")
	if p.Job.Counts.Added != 1 {
		t.Fatal("체크포인트 집계 유실")
	}
	ok(t, f.service.Resume(t.Context(), localimport.Request{JobID: p.Job.ID}))
	p = wait(t, f.service, p.Job.ID, "completed")
	if p.Job.Counts.Added != 2 || f.calls.Load() != 1 || count(t, f, "SELECT count(*) FROM media_file WHERE id=?", first) != 1 {
		t.Fatal("복구 후 중복 검사/등록")
	}
}

func TestScanResumeUsesDurableDirectoryQueue(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"done/a.png", "pending/b.png", "pending/nested/c.png"} {
		write(t, filepath.Join(f.source, name), pngBytes(t, 8))
	}
	p := scan(t, f)
	f.service.Close()
	// Model a crash after target and child-directory batches commit, before the
	// current directory's completion marker. Completed siblings stay untouched.
	if _, err := f.db.Exec("UPDATE local_import_scan_directory SET state='pending' WHERE job_id=? AND directory_path=?", p.Job.ID, filepath.Join(f.source, "pending")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE local_import_job SET state='scanning',scan_complete=0,counts_json='{}' WHERE id=?", p.Job.ID); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(f.source, "done", "later.png"), pngBytes(t, 8))
	write(t, filepath.Join(f.source, "pending", "new.png"), pngBytes(t, 8))
	reopened := localimport.NewService(f.db, inspectFunc(f.inspect), nil, nil)
	defer reopened.Close()
	if err := reopened.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	ok(t, reopened.Resume(t.Context(), localimport.Request{JobID: p.Job.ID}))
	p = wait(t, reopened, p.Job.ID, "ready")
	if p.Job.Counts.Discovered != 4 || count(t, f, "SELECT count(*) FROM local_import_target") != 4 {
		t.Fatal("탐색 재개가 완료 폴더를 다시 읽거나 중복 대상을 생성했습니다", p.Job.Counts)
	}
}

func TestEmptyOrCorruptCandidateIsFileFailure(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.source, "empty.mp4"), nil)
	write(t, filepath.Join(f.source, "good.png"), pngBytes(t, 8))
	p := run(t, f, scan(t, f))
	if p.Job.Counts.Added != 1 || p.Job.Counts.Failed != 1 || p.Job.Counts.Processed != 2 {
		t.Fatal(p.Job.Counts)
	}
	files := ok(t, f.service.Files(t.Context(), localimport.Request{JobID: p.Job.ID, FileState: "failed"})).(localimport.FilePage)
	if len(files.Files) != 1 || !strings.Contains(files.Files[0].Path, "empty.mp4") {
		t.Fatal(files)
	}
}

func TestPausedExclusionAllowsUnregisterAndKeepsCheckpoint(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"1.png", "bad.png"} {
		write(t, filepath.Join(f.source, name), pngBytes(t, 8))
	}
	original := f.inspect
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		if filepath.Base(path) == "bad.png" {
			return engine.LocalMetadata{}, fmt.Errorf("손상")
		}
		return original(ctx, path, kind)
	}
	p := run(t, f, scan(t, f))
	ok(t, f.service.Exclude(t.Context(), localimport.Request{JobID: p.Job.ID, AllFailed: true}))
	id := count(t, f, "SELECT id FROM media")
	revision := count(t, f, "SELECT revision FROM media")
	ok(t, media.NewService(f.db).UnregisterMedia(t.Context(), media.UnregisterRequest{MediaID: id, ExpectedRevision: revision}))
	if count(t, f, "SELECT count(*) FROM local_import_target WHERE state='registered' AND media_file_id IS NULL AND file_uuid_snapshot IS NOT NULL") != 1 {
		t.Fatal("등록 해제로 확정 이력이 유실됐습니다")
	}
	ok(t, f.service.Resume(t.Context(), localimport.Request{JobID: p.Job.ID}))
	wait(t, f.service, p.Job.ID, "completed")
	if count(t, f, "SELECT count(*) FROM media") != 0 {
		t.Fatal("재개가 사용자가 해제한 등록을 복구했습니다")
	}
	if _, err := os.Stat(filepath.Join(f.source, "1.png")); err != nil {
		t.Fatal("외부 원본 삭제")
	}
}

func TestChangedDuringInspectionKeepsPriorFileAndContinues(t *testing.T) {
	f := newFixture(t)
	changed := filepath.Join(f.source, "change.png")
	write(t, changed, pngBytes(t, 8))
	run(t, f, scan(t, f))
	oldID := count(t, f, "SELECT id FROM media_file")
	var oldHash string
	if err := f.db.QueryRow("SELECT content_hash FROM media_file WHERE id=?", oldID).Scan(&oldHash); err != nil {
		t.Fatal(err)
	}
	write(t, changed, pngBytes(t, 10))
	write(t, filepath.Join(f.source, "good.png"), pngBytes(t, 8))
	p := scan(t, f)
	replacement := pngBytes(t, 12)
	original := f.inspect
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		if filepath.Base(path) == "change.png" {
			if err := os.WriteFile(path, replacement, 0600); err != nil {
				return engine.LocalMetadata{}, err
			}
		}
		return original(ctx, path, kind)
	}
	p = run(t, f, p)
	if p.Job.Counts.Failed != 1 || p.Job.Counts.Added != 1 || p.Job.Counts.Updated != 0 || count(t, f, "SELECT count(*) FROM media_file WHERE id=? AND content_hash=?", oldID, oldHash) != 1 {
		t.Fatal("검사 도중 변경이 기존 파일 정보를 덮어쓰거나 다른 파일을 중단했습니다", p.Job.Counts)
	}
}
