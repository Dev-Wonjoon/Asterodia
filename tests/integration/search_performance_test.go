package integration_test

import (
	"fmt"
	"github.com/Dev-Wonjoon/Asterodia/internal/database"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/media"
)

func TestSearchPerformance(t *testing.T) {
	count, err := strconv.Atoi(os.Getenv("ASTERODIA_SEARCH_PERF_COUNT"))
	if err != nil || count < 10000 {
		t.Skip("대용량 측정은 ASTERODIA_SEARCH_PERF_COUNT>=10000으로 명시 실행")
	}
	db, s := searchDB(t)
	if root := os.Getenv("ASTERODIA_SEARCH_PERF_ROOT"); root != "" {
		repo, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(filepath.Join(repo, ".test-data", "search"), resolved)
		if err != nil || rel == "." || !filepath.IsLocal(rel) {
			t.Fatal("격리 검색 실행 폴더가 필요합니다")
		}
		folder, err := os.MkdirTemp(resolved, "performance-")
		if err != nil {
			t.Fatal(err)
		}
		db, err = database.Open(t.Context(), filepath.Join(folder, "library.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		s = media.NewService(db)
		t.Cleanup(s.Close)
		t.Log("디스크 DB:", folder)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare("INSERT INTO media(source_kind,source_title,created_at)VALUES('legacy',?,?)")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		if _, err = stmt.Exec(fmt.Sprintf("서울여행 사진 기록 %06d", i), i); err != nil {
			t.Fatal(err)
		}
	}
	stmt.Close()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	beforeBytes := queryInt(t, db, "PRAGMA page_count") * queryInt(t, db, "PRAGMA page_size")
	start := time.Now()
	drainSearch(t, s)
	t.Logf("%d 항목 인덱싱: %s", count, time.Since(start))
	afterBytes := queryInt(t, db, "PRAGMA page_count") * queryInt(t, db, "PRAGMA page_size")
	t.Logf("DB 페이지 용량: 전=%d 후=%d 증가=%d bytes, 단어=%d, 짧은 검색 인덱스=%d", beforeBytes, afterBytes, afterBytes-beforeBytes, queryInt(t, db, "SELECT count(*) FROM search_term"), queryInt(t, db, "SELECT count(*) FROM search_gram"))
	for _, q := range []string{"행", "여행", "서울여행", "없는후보"} {
		var samples []time.Duration
		for i := 0; i < 40; i++ {
			start = time.Now()
			page := searchPage(t, s, media.SearchSuggestRequest{Text: q})
			samples = append(samples, time.Since(start))
			if page["indexed"] != true {
				t.Fatal("인덱스 미사용")
			}
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		t.Logf("%q 추천: p50=%s p95=%s max=%s", q, samples[20], samples[38], samples[39])
	}
	for _, q := range []string{"행", "서울여행"} {
		start = time.Now()
		success(t, s.List(t.Context(), media.ListRequest{Limit: 24, Filter: media.Filter{Keyword: q}}))
		t.Logf("%q 갤러리 24개+전체 개수: %s", q, time.Since(start))
	}
}
