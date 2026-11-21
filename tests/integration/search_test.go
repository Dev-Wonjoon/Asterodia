package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

func searchDB(t *testing.T) (*sql.DB, *media.Service) {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)&_pragma=temp_store(2)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err = database.Initialize(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	s := media.NewService(db)
	t.Cleanup(s.Close)
	return db, s
}
func searchExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err, query)
	}
}
func drainSearch(t *testing.T, s *media.Service) {
	t.Helper()
	for i := 0; i < 10000; i++ {
		work, err := s.RefreshSearchIndex(t.Context(), 128)
		if err != nil {
			t.Fatal(err)
		}
		if !work {
			return
		}
	}
	t.Fatal("인덱스가 끝나지 않습니다")
}
func searchPage(t *testing.T, s *media.Service, req media.SearchSuggestRequest) map[string]any {
	t.Helper()
	return success(t, s.SearchSuggest(t.Context(), req)).(media.Object)
}

func TestSearchContainsAndIndexParity(t *testing.T) {
	db, s := searchDB(t)
	for i, title := range []string{"여행", "여행기", "은행", "제주도 여행", "비행기", "행", "행복", "aBc", "a%b_\"c", "가 나 다", "가😀나", "' OR 1=1 --"} {
		searchExec(t, db, "INSERT INTO media(id,source_kind,source_title,created_at) VALUES(?,'legacy',?,?)", i+1, title, i)
	}
	for i := 0; i < 24; i++ {
		searchExec(t, db, "INSERT INTO media(source_kind,source_title,created_at)VALUES('legacy',?,?)", fmt.Sprintf("아주 긴 여행 후보 %02d", i), i)
	}
	searchExec(t, db, "UPDATE media SET custom_title='사용자 제목' WHERE id=4")
	searchExec(t, db, "INSERT INTO tag(id,name) VALUES(1,'여행')")
	searchExec(t, db, "INSERT INTO media_tag(media_id,tag_id)VALUES(1,1),(2,1)")
	searchExec(t, db, "INSERT INTO platform(id,name)VALUES(1,'video-platform')")
	searchExec(t, db, "INSERT INTO profiles(id,owner_id,owner_name,platform_id,updated_at)VALUES(1,'author-id','여행 작가',1,1)")
	searchExec(t, db, `INSERT INTO media_file(media_id,file_key,file_uuid,filename_token,storage_kind,kind,position,filepath,created_at,platform_id,profile_id,title)VALUES(1,'fixture','11111111-1111-4111-8111-111111111111','11111111','unverified_legacy','unknown',0,'not-a-real-file',1,1,1,'동영상 여행 기록')`)
	searchExec(t, db, "INSERT INTO favorite(media_id,created_at)VALUES(1,1)")
	queries := []string{"행", "여행", "행기", "제주도", "%", "_", "\"", "가 나", "aB", "😀", "가😀나", "' OR 1=1 --", "사용자 제목 제주도", "동영상 여행 기록 여행 작가 video-platform"}
	before := map[string][]byte{}
	listBefore := map[string][]byte{}
	for _, q := range queries {
		page := searchPage(t, s, media.SearchSuggestRequest{Text: q, Limit: 50})
		if page["indexed"] != false {
			t.Fatal("초기 검색은 원본을 사용해야 합니다")
		}
		before[q], _ = json.Marshal(page["data"])
		listBefore[q], _ = json.Marshal(success(t, s.List(t.Context(), media.ListRequest{Filter: media.Filter{Keyword: q}})))
	}
	drainSearch(t, s)
	for _, q := range queries {
		page := searchPage(t, s, media.SearchSuggestRequest{Text: q, Limit: 50})
		if page["indexed"] != true {
			t.Fatal("인덱스를 사용하지 않습니다")
		}
		after, _ := json.Marshal(page["data"])
		if string(after) != string(before[q]) {
			t.Fatalf("%q 후보 불일치: %s / %s", q, before[q], after)
		}
		listAfter, _ := json.Marshal(success(t, s.List(t.Context(), media.ListRequest{Filter: media.Filter{Keyword: q}})))
		if string(listAfter) != string(listBefore[q]) {
			t.Fatalf("%q 갤러리 결과 불일치", q)
		}
	}
	first := searchPage(t, s, media.SearchSuggestRequest{Text: "행"})
	if first["data"].([]media.Object)[0]["value"] != "행" || first["hasNextPage"] != true {
		t.Fatal("정확 일치 우선/페이지 누락", first)
	}
	seen := map[string]bool{}
	for offset := 0; ; offset += 10 {
		page := searchPage(t, s, media.SearchSuggestRequest{Text: "행", Offset: offset})
		for _, row := range page["data"].([]media.Object) {
			key := fmt.Sprint(row["kind"], row["value"])
			if seen[key] {
				t.Fatal("중복 후보", key)
			}
			seen[key] = true
		}
		if page["hasNextPage"] == false {
			break
		}
	}
	if len(seen) < 30 {
		t.Fatal("한 글자 후보가 잘렸습니다", len(seen))
	}
	for _, req := range []media.SearchSuggestRequest{{Text: "행", Filter: media.Filter{Authors: []string{"여행 작가"}}}, {Text: "행", FavoritesOnly: true}, {Text: "행", Filter: media.Filter{Tags: []string{"여행"}, Keyword: "존재하지 않는 입력"}}} {
		rows := searchPage(t, s, req)["data"].([]media.Object)
		if len(rows) == 0 || len(rows) > 5 {
			t.Fatal("현재 필터 누락", rows)
		}
	}
	for _, req := range []media.SearchSuggestRequest{{Text: "a\x00b"}, {Text: strings.Repeat("x", 1001)}, {Text: "행", Offset: -1}, {Text: "행", Limit: 51}, {Text: "행", Kind: "bad"}, {Text: "행", Filter: media.Filter{TagMode: "bad"}}} {
		errorCode(t, s.SearchSuggest(t.Context(), req), "INVALID_ARGUMENT")
	}
}

func TestSearchIndexUpdatesRecoveryAndWorker(t *testing.T) {
	db, s := searchDB(t)
	searchExec(t, db, "INSERT INTO media(id,source_kind,source_title,created_at)VALUES(1,'legacy','원본 여행',1)")
	drainSearch(t, s)
	searchExec(t, db, "UPDATE media SET source_title='새로운 은행' WHERE id=1")
	page := searchPage(t, s, media.SearchSuggestRequest{Text: "은행"})
	if len(page["data"].([]media.Object)) != 1 || page["indexed"] != false {
		t.Fatal("대기 중 원본 검색 누락", page)
	}
	searchExec(t, db, `CREATE TRIGGER fail_search BEFORE INSERT ON search_gram BEGIN SELECT RAISE(ABORT,'fixture'); END`)
	if _, err := s.RefreshSearchIndex(t.Context(), 1); err == nil {
		t.Fatal("실패 주입 누락")
	}
	if queryInt(t, db, "SELECT count(*) FROM search_dirty") != 1 || queryInt(t, db, "SELECT count(*) FROM search_term WHERE value='원본 여행'") != 1 {
		t.Fatal("실패한 배치를 부분 반영했습니다")
	}
	searchExec(t, db, "DROP TRIGGER fail_search")
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.RefreshSearchIndex(cancelled, 1); err == nil {
		t.Fatal("취소 누락")
	}
	s.StartSearchIndex()
	s.StartSearchIndex()
	deadline := time.Now().Add(3 * time.Second)
	for queryInt(t, db, "SELECT count(*) FROM search_dirty") > 0 {
		if time.Now().After(deadline) {
			t.Fatal("작업자 갱신 시간 초과")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Close()
	if queryInt(t, db, "SELECT count(*) FROM search_term WHERE value='원본 여행'") != 0 {
		t.Fatal("사용하지 않는 후보 정리 누락")
	}
	searchExec(t, db, "DELETE FROM media WHERE id=1")
	if len(searchPage(t, s, media.SearchSuggestRequest{Text: "은행"})["data"].([]media.Object)) != 0 {
		t.Fatal("삭제한 미디어 후보가 남았습니다")
	}
	drainSearch(t, s)
	if queryInt(t, db, "SELECT count(*) FROM search_term") != 0 || queryInt(t, db, "SELECT count(*) FROM search_gram") != 0 {
		t.Fatal("검색 잔여 인덱스 정리 누락")
	}
}

func TestSearchMetadataChanges(t *testing.T) {
	db, s := searchDB(t)
	searchExec(t, db, "INSERT INTO media(id,source_kind,source_title,created_at)VALUES(1,'legacy','제목',1)")
	searchExec(t, db, "INSERT INTO tag(id,name)VALUES(1,'옛 태그');INSERT INTO media_tag VALUES(1,1)")
	drainSearch(t, s)
	for _, stmt := range []string{"UPDATE tag SET name='새 태그' WHERE id=1", "DELETE FROM media_tag WHERE media_id=1"} {
		searchExec(t, db, stmt)
		before := searchPage(t, s, media.SearchSuggestRequest{Text: "태그"})["data"]
		drainSearch(t, s)
		after := searchPage(t, s, media.SearchSuggestRequest{Text: "태그"})["data"]
		if !reflect.DeepEqual(before, after) {
			t.Fatal("태그 변경의 인덱스 결과 불일치", before, after)
		}
	}
	searchExec(t, db, "INSERT INTO platform VALUES(1,'플랫폼');INSERT INTO profiles VALUES(1,'고유작성자', '기존 작가',1,1)")
	searchExec(t, db, `INSERT INTO media_file(media_id,file_key,file_uuid,filename_token,storage_kind,kind,position,filepath,created_at,platform_id,profile_id,title)VALUES(1,'fixture','11111111-1111-4111-8111-111111111111','11111111','unverified_legacy','unknown',0,'not-a-real-file',1,1,1,'파일 제목')`)
	searchExec(t, db, `INSERT OR ABORT INTO media_file(media_id,file_key,file_uuid,filename_token,storage_kind,kind,position,filepath,created_at,platform_id,profile_id,title)VALUES(1,'fixture-2','22222222-2222-4222-8222-222222222222','22222222','unverified_legacy','unknown',1,'not-a-real-file-2',1,1,1,'파일 제목')`)
	drainSearch(t, s)
	for _, stmt := range []string{"UPDATE profiles SET owner_name='새로운 작가' WHERE id=1", "UPDATE platform SET name='새 플랫폼' WHERE id=1", "UPDATE media_file SET title='변경한 제목' WHERE media_id=1", "UPDATE profiles SET owner_name=NULL WHERE id=1", "DELETE FROM media_file WHERE media_id=1"} {
		searchExec(t, db, stmt)
		queries := []media.SearchSuggestRequest{{Text: "작가"}, {Text: "고유작성자"}, {Text: "플랫폼", Kind: "platform"}, {Text: "제목"}}
		before := []any{}
		for _, q := range queries {
			before = append(before, searchPage(t, s, q)["data"])
		}
		drainSearch(t, s)
		for i, q := range queries {
			after := searchPage(t, s, q)["data"]
			if !reflect.DeepEqual(before[i], after) {
				t.Fatal("메타데이터 갱신 불일치", stmt, before[i], after)
			}
		}
	}
}

func TestSearchBroadPagination(t *testing.T) {
	db, s := searchDB(t)
	for i := 0; i < 410; i++ {
		title := fmt.Sprintf("가운데행_%03d", i)
		if i < 100 {
			title = fmt.Sprintf("행접두어_%03d", i)
		}
		if i == 0 {
			title = "행"
		}
		searchExec(t, db, "INSERT INTO media(source_kind,source_title,created_at)VALUES('legacy',?,?)", title, i)
	}
	searchExec(t, db, "INSERT INTO tag(id,name)VALUES(1,'행태그');INSERT INTO media_tag VALUES(1,1)")
	requests := []media.SearchSuggestRequest{{Text: "행", Offset: 0}, {Text: "행", Offset: 3}, {Text: "행", Offset: 95}, {Text: "행", Offset: 100}, {Text: "행", Offset: 405}, {Text: "행", Offset: 500}, {Text: "행", Filter: media.Filter{Tags: []string{"행태그"}}}}
	before := []any{}
	for _, req := range requests {
		page := searchPage(t, s, req)
		before = append(before, []any{page["data"], page["hasNextPage"]})
	}
	drainSearch(t, s)
	for i, req := range requests {
		page := searchPage(t, s, req)
		if !reflect.DeepEqual(before[i], []any{page["data"], page["hasNextPage"]}) {
			t.Fatal("다수 후보 페이지 불일치", req, before[i], page)
		}
	}
}
