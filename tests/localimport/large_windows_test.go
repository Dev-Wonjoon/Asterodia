package localimport_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/localimport"
)

func TestTenThousandFilesPauseResume(t *testing.T) {
	if testing.Short() {
		t.Skip("1만 파일 회귀는 전체 실행에서 확인합니다")
	}
	f := newFixture(t)
	const total = 10000
	// Tiny files and a deterministic inspector isolate the enumerator, SQLite
	// checkpoints and scheduling from codec cost. Native formats have separate tests.
	started := time.Now()
	for i := 0; i < total; i++ {
		write(t, filepath.Join(f.source, fmt.Sprintf("%d.png", i)), []byte{1})
	}
	p := scan(t, f)
	if p.Job.Counts.Discovered != total || p.Job.Counts.TotalBytes != total || len(p.Folders) != 1 || p.Folders[0].FileCount != total {
		t.Fatal(p.Job.Counts)
	}
	ok(t, f.service.Run(t.Context(), localimport.Request{JobID: p.Job.ID}))
	deadline := time.Now().Add(2 * time.Minute)
	for {
		before := time.Now()
		p = ok(t, f.service.Get(t.Context(), localimport.Request{JobID: p.Job.ID})).(localimport.Page)
		if time.Since(before) > 2*time.Second {
			t.Fatal("진행 조회가 장시간 차단됐습니다")
		}
		if p.Job.Counts.Processed >= 128 {
			break
		}
		if p.Job.State != "running" || time.Now().After(deadline) {
			t.Fatalf("대량 처리 시작 실패: %+v", p.Job)
		}
		time.Sleep(50 * time.Millisecond)
	}
	ok(t, f.service.Pause(t.Context(), localimport.Request{JobID: p.Job.ID}))
	p = wait(t, f.service, p.Job.ID, "paused")
	saved := count(t, f, "SELECT count(*) FROM media_file")
	if saved <= 0 || saved >= total || int64(p.Job.Counts.Added) != saved {
		t.Fatal("파일별 중단 체크포인트 불일치", p.Job.Counts)
	}
	ok(t, f.service.Resume(t.Context(), localimport.Request{JobID: p.Job.ID}))
	deadline = time.Now().Add(3 * time.Minute)
	for {
		p = ok(t, f.service.Get(t.Context(), localimport.Request{JobID: p.Job.ID})).(localimport.Page)
		if p.Job.State == "completed" {
			break
		}
		if p.Job.State != "running" || time.Now().After(deadline) {
			t.Fatalf("대량 처리 완료 실패: %+v", p.Job)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if p.Job.Counts.Added != total || p.Job.Counts.Processed != total || p.Job.Counts.ProcessedBytes != total || count(t, f, "SELECT count(*) FROM media_file") != total || count(t, f, "SELECT count(DISTINCT file_uuid) FROM media_file") != total {
		t.Fatal("재개 후 누락 또는 중복", p.Job.Counts)
	}
	if count(t, f, "SELECT count(*) FROM pragma_foreign_key_check()") != 0 {
		t.Fatal("외래키 불일치")
	}
	t.Logf("1만 파일 생성·탐색·등록·중단·재개: %s", time.Since(started).Round(time.Millisecond))
}

func TestLargeFolderWarningDoesNotRejectJob(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.source, "a.png"), []byte{1})
	p := scan(t, f)
	tx, err := f.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), `WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<50000) INSERT INTO local_import_target(id,attempt_id,path_key,relative_path,expected_stat_json,state,sort_key) SELECT 'warning-'||i,(SELECT current_attempt_id FROM local_import_folder WHERE job_id=?),'warning-key-'||i,i||'.png','{}','excluded',printf('%08d',i) FROM n`, p.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	p = ok(t, f.service.Get(t.Context(), localimport.Request{JobID: p.Job.ID, Limit: 25})).(localimport.Page)
	if p.Job.LargeFolders != 1 || p.Folders[0].FileCount != 50001 {
		t.Fatal("5만 초과 폴더 경고 누락", p)
	}
	p = run(t, f, p)
	if p.Job.Counts.Added != 1 {
		t.Fatal("대형 폴더를 강제로 차단했습니다", p.Job)
	}
}
