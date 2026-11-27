package filemove_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
	"github.com/Dev-Wonjoon/Asterodia/internal/database"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/filemove"
	"github.com/Dev-Wonjoon/Asterodia/internal/localimport"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"modernc.org/sqlite"
)

type fixture struct {
	db                          *sql.DB
	s                           *filemove.Service
	root, filename, destination string
	dir                         int64
}

func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func ok(t *testing.T, r api.Result) any {
	t.Helper()
	if !r.OK {
		t.Fatalf("%+v", r.Error)
	}
	return r.Data
}
func execSQL(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	_, e := db.Exec(q, args...)
	must(t, e)
}
func createFixture(t *testing.T) *fixture {
	t.Helper()
	base := os.Getenv("ASTERODIA_TEST_ROOT")
	repo, e := filepath.Abs("../..")
	must(t, e)
	actual, e := filepath.EvalSymlinks(base)
	must(t, e)
	rel, e := filepath.Rel(filepath.Join(repo, ".test-data", "filemove"), actual)
	if e != nil || !filepath.IsLocal(rel) || rel == "." {
		t.Fatal("격리된 filemove 테스트 경로가 필요합니다")
	}
	root, e := os.MkdirTemp(actual, "move-")
	must(t, e)
	f := &fixture{root: root, filename: filepath.Join(root, "library.db"), destination: filepath.Join(root, "B")}
	must(t, os.Mkdir(f.destination, 0700))
	f.db, e = database.Open(t.Context(), f.filename)
	must(t, e)
	path, key, e := media.CanonicalPath(f.destination)
	must(t, e)
	f.dir, e = media.NewStore(f.db).EnsureDirectory(t.Context(), path, key, 1, true)
	must(t, e)
	f.s = filemove.NewService(f.db, nil)
	t.Cleanup(func() { f.s.Close(); f.db.Close() })
	for i := 1; i <= 2; i++ {
		execSQL(t, f.db, "INSERT INTO download_urls(id,url,normalized_url,normalization_version,created_at)VALUES(?,?,?,1,1)", i, fmt.Sprintf("https://example.test/%d", i), fmt.Sprintf("https://example.test/%d", i))
		execSQL(t, f.db, "INSERT INTO media(id,source_kind,request_url_id,custom_title,revision,created_at,updated_at)VALUES(?,'download',?,'편집 제목',5,1,1)", i, i)
		execSQL(t, f.db, "INSERT INTO favorite(media_id,created_at)VALUES(?,1)", i)
		for j := 0; j < 2; j++ {
			path := filepath.Join(root, fmt.Sprintf("A%d-%d.mp4", i, j))
			must(t, os.WriteFile(path, bytes.Repeat([]byte{byte(i), byte(j), 7}, 4096), 0600))
			execSQL(t, f.db, `INSERT INTO media_file(media_id,file_key,storage_kind,title,filepath,kind,position,detected_format,duration,width,height,created_at)VALUES(?,?,'app_managed','게시글',?,'video',?,'mp4',1,8,6,1)`, i, fmt.Sprint(j), path, j)
		}
	}
	return f
}
func (f *fixture) create(t *testing.T) string {
	return ok(t, f.s.Create(t.Context(), filemove.CreateRequest{Selection: []filemove.Selection{{MediaID: 1, Revision: 5}, {MediaID: 2, Revision: 5}}, DirectoryID: f.dir})).(map[string]string)["jobId"]
}
func (f *fixture) wait(t *testing.T, id string) filemove.Status {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		r := ok(t, f.s.Get(t.Context(), filemove.Request{JobID: id})).(filemove.Status)
		if r.State != "running" && r.State != "pausing" {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("이동 완료 시간 초과")
	return filemove.Status{}
}
func TestMoveMultiplePostsPreservesIdentityAndEdits(t *testing.T) {
	f := createFixture(t)
	f.s.Close()
	var notifications atomic.Int32
	f.s = filemove.NewService(f.db, func(id int64) { notifications.Add(1) })
	before := map[int64]string{}
	rows, e := f.db.Query("SELECT id,file_uuid FROM media_file")
	must(t, e)
	for rows.Next() {
		var id int64
		var u string
		must(t, rows.Scan(&id, &u))
		before[id] = u
	}
	rows.Close()
	// Force a real token collision for the first file, never overwrite the occupant.
	var token string
	must(t, f.db.QueryRow("SELECT filename_token FROM media_file WHERE id=1").Scan(&token))
	occupant := filepath.Join(f.destination, "게시글_"+token+".mp4")
	must(t, os.WriteFile(occupant, []byte("user content"), 0600))
	id := f.create(t)
	r := f.s.Create(t.Context(), filemove.CreateRequest{Selection: []filemove.Selection{{MediaID: 1, Revision: 5}}, DirectoryID: f.dir})
	if r.OK {
		t.Fatal("중복 예약 허용")
	}
	if r := media.NewService(f.db).Edit(t.Context(), media.EditRequest{MediaID: 1, ExpectedRevision: 5}); r.OK || r.Error.Code != "JOB_ACTIVE" {
		t.Fatalf("활성 이동 편집 차단: %+v", r)
	}
	ok(t, f.s.Run(t.Context(), filemove.Request{JobID: id}))
	v := f.wait(t, id)
	if v.Completed != 4 || v.Failed != 0 || v.State != "completed" || notifications.Load() != 4 {
		t.Fatalf("%+v", v)
	}
	rows, e = f.db.Query("SELECT id,file_uuid,filename_token,filepath,storage_kind FROM media_file ORDER BY id")
	must(t, e)
	for rows.Next() {
		var id int64
		var u, tok, path, kind string
		must(t, rows.Scan(&id, &u, &tok, &path, &kind))
		if before[id] != u || filepath.Dir(path) != f.destination || kind != "external_reference" || !strings.HasSuffix(path, "_"+tok+".mp4") {
			t.Fatalf("식별/위치 손실 %d %s", id, path)
		}
		if _, e = os.Stat(path); e != nil {
			t.Fatal(e)
		}
	}
	rows.Close()
	for _, item := range v.Items {
		if _, e = os.Stat(item.Source); !os.IsNotExist(e) {
			t.Fatalf("원본 정리 실패: %s %v", item.Source, e)
		}
	}
	data, e := os.ReadFile(occupant)
	must(t, e)
	if string(data) != "user content" {
		t.Fatal("기존 파일 덮어쓰기")
	}
	var edits int
	must(t, f.db.QueryRow("SELECT count(*) FROM media WHERE custom_title='편집 제목' AND EXISTS(SELECT 1 FROM favorite WHERE media_id=media.id) AND source_kind='download'").Scan(&edits))
	if edits != 2 {
		t.Fatal("게시글 병합/편집 손실")
	}
	var fk any
	err := f.db.QueryRow("PRAGMA foreign_key_check").Scan(&fk)
	if err != sql.ErrNoRows {
		t.Fatalf("외래키: %v", err)
	}
}
func TestMoveRollbackAndRetry(t *testing.T) {
	f := createFixture(t)
	id := f.create(t)
	execSQL(t, f.db, `CREATE TRIGGER fixture_reject BEFORE UPDATE OF filepath ON media_file BEGIN SELECT RAISE(ABORT,'copy commit failure'); END`)
	ok(t, f.s.Run(t.Context(), filemove.Request{JobID: id}))
	v := f.wait(t, id)
	if v.Failed != 4 || v.Completed != 0 {
		t.Fatalf("%+v", v)
	}
	for _, item := range v.Items {
		if _, e := os.Stat(item.Source); e != nil {
			t.Fatal("커밋 전 원본 손실", e)
		}
	}
	entries, e := os.ReadDir(f.destination)
	must(t, e)
	if len(entries) != 0 {
		t.Fatal("실패 사본 정리 실패")
	}
	execSQL(t, f.db, "DROP TRIGGER fixture_reject")
	ok(t, f.s.Retry(t.Context(), filemove.Request{JobID: id}))
	v = f.wait(t, id)
	if v.Completed != 4 {
		t.Fatalf("재시도 %+v", v)
	}
}
func TestMoveChangedSourceAndMissingDestination(t *testing.T) {
	f := createFixture(t)
	id := f.create(t)
	var path string
	must(t, f.db.QueryRow("SELECT filepath FROM media_file WHERE id=1").Scan(&path))
	must(t, os.WriteFile(path, []byte("changed"), 0600))
	ok(t, f.s.Run(t.Context(), filemove.Request{JobID: id}))
	v := f.wait(t, id)
	if v.Failed != 1 || v.Completed != 3 {
		t.Fatalf("부분 성공 %+v", v)
	}
	b, e := os.ReadFile(path)
	must(t, e)
	if string(b) != "changed" {
		t.Fatal("변경 원본 훼손")
	}
}
func TestMoveCleanupPendingRecovery(t *testing.T) {
	f := createFixture(t)
	id := f.create(t)
	execSQL(t, f.db, `CREATE TRIGGER fixture_cleanup BEFORE UPDATE OF state ON file_move_item WHEN NEW.state='completed' BEGIN SELECT RAISE(ABORT,'cleanup record failure'); END`)
	ok(t, f.s.Run(t.Context(), filemove.Request{JobID: id}))
	v := f.wait(t, id)
	if v.CleanupPending != 4 {
		t.Fatalf("%+v", v)
	}
	execSQL(t, f.db, "DROP TRIGGER fixture_cleanup")
	f.s.Close()
	f.s = filemove.NewService(f.db, nil)
	must(t, f.s.Recover(t.Context()))
	ok(t, f.s.Retry(t.Context(), filemove.Request{JobID: id}))
	v = f.wait(t, id)
	if v.Completed != 4 {
		t.Fatalf("정리 재개 %+v", v)
	}
}
func TestMoveValidationAndFilename(t *testing.T) {
	f := createFixture(t)
	if f.s.Create(t.Context(), filemove.CreateRequest{}).OK {
		t.Fatal("빈 요청 허용")
	}
	if f.s.Get(t.Context(), filemove.Request{Offset: -1}).OK {
		t.Fatal("음수 offset 허용")
	}
	if f.s.Pause(t.Context(), filemove.Request{JobID: "missing"}).OK {
		t.Fatal("없는 작업 중단 허용")
	}
	if r := f.s.Get(t.Context(), filemove.Request{}); !r.OK || r.Data != nil {
		t.Fatalf("빈 작업 %+v", r)
	}
	for _, value := range []string{"CON", "../../제목:*", "", strings.Repeat("😀", 200)} {
		token, e := media.NewFilenameToken()
		must(t, e)
		name, e := media.Filename(value, "fallback", ".MP4", token)
		must(t, e)
		if !regexp.MustCompile(`_[0-9a-f]{8}\.mp4$`).MatchString(name) || strings.ContainsAny(name, `<>:"/\|?*`) {
			t.Fatal(name)
		}
	}
	if _, e := media.Filename("title", "id", ".mp4", "invalid"); e == nil {
		t.Fatal("잘못된 토큰 허용")
	}
}
func TestMoveCrashRecovery(t *testing.T) {
	mode := os.Getenv("ASTERODIA_MOVE_CRASH_CHILD")
	if mode != "" {
		must(t, sqlite.RegisterScalarFunction("move_crash", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) { os.Exit(79); return nil, nil }))
		f := createFixture(t)
		must(t, os.WriteFile(filepath.Join(os.Getenv("ASTERODIA_TEST_ROOT"), "child-path.txt"), []byte(f.root), 0600))
		id := f.create(t)
		state := "committed"
		if mode == "after-commit" {
			state = "completed"
		}
		execSQL(t, f.db, fmt.Sprintf(`CREATE TRIGGER fixture_crash AFTER UPDATE OF state ON file_move_item WHEN NEW.state='%s' BEGIN SELECT move_crash(); END`, state))
		ok(t, f.s.Run(t.Context(), filemove.Request{JobID: id}))
		f.wait(t, id)
		t.Fatal("강제 종료 미발생")
	}
	for _, mode := range []string{"before-commit", "after-commit"} {
		t.Run(mode, func(t *testing.T) {
			f := createFixture(t)
			childBase := filepath.Join(f.root, "child")
			must(t, os.Mkdir(childBase, 0700))
			exe, e := os.Executable()
			must(t, e)
			cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestMoveCrashRecovery$", "-test.timeout=30s")
			for _, v := range os.Environ() {
				if !strings.HasPrefix(strings.ToUpper(v), "ASTERODIA_TEST_ROOT=") && !strings.HasPrefix(v, "ASTERODIA_MOVE_CRASH_CHILD=") {
					cmd.Env = append(cmd.Env, v)
				}
			}
			cmd.Env = append(cmd.Env, "ASTERODIA_TEST_ROOT="+childBase, "ASTERODIA_MOVE_CRASH_CHILD="+mode)
			output, e := cmd.CombinedOutput()
			if exit, yes := e.(*exec.ExitError); !yes || exit.ExitCode() != 79 {
				t.Fatalf("강제종료 %s %v", output, e)
			}
			b, e := os.ReadFile(filepath.Join(childBase, "child-path.txt"))
			must(t, e)
			root := string(b)
			rel, e := filepath.Rel(childBase, root)
			if e != nil || !filepath.IsLocal(rel) {
				t.Fatal("테스트 경로 이탈")
			}
			rawDB, e := sql.Open("sqlite", filepath.Join(root, "library.db"))
			must(t, e)
			execSQL(t, rawDB, "DROP TRIGGER fixture_crash")
			must(t, rawDB.Close())
			db, e := database.Open(t.Context(), filepath.Join(root, "library.db"))
			must(t, e)
			defer db.Close()
			svc := filemove.NewService(db, nil)
			defer svc.Close()
			must(t, svc.Recover(t.Context()))
			r := ok(t, svc.Get(t.Context(), filemove.Request{})).(filemove.Status)
			if r.State != "interrupted" {
				t.Fatalf("%+v", r)
			}
			recovered := &fixture{db: db, s: svc}
			ok(t, svc.Run(t.Context(), filemove.Request{JobID: r.JobID}))
			r = recovered.wait(t, r.JobID)
			if r.Completed != 4 || r.Failed != 0 {
				j, _ := json.Marshal(r)
				t.Fatalf("복구 %s", j)
			}
		})
	}
}

func TestPauseResumeAndDirectoryReplacement(t *testing.T) {
	t.Run("pause-and-resume", func(t *testing.T) {
		f := createFixture(t)
		var source string
		must(t, f.db.QueryRow("SELECT filepath FROM media_file WHERE id=1").Scan(&source))
		must(t, os.Truncate(source, 128*1024*1024))
		id := f.create(t)
		ok(t, f.s.Run(t.Context(), filemove.Request{JobID: id}))
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			v := ok(t, f.s.Get(t.Context(), filemove.Request{JobID: id})).(filemove.Status)
			if v.ProcessedBytes > 1024*1024 && v.State == "running" {
				break
			}
			time.Sleep(time.Millisecond)
		}
		ok(t, f.s.Pause(t.Context(), filemove.Request{JobID: id}))
		v := f.wait(t, id)
		if v.State != "paused" {
			t.Fatalf("%+v", v)
		}
		count := v.Completed
		f.s.Close()
		f.s = filemove.NewService(f.db, nil)
		must(t, f.s.Recover(t.Context()))
		ok(t, f.s.Run(t.Context(), filemove.Request{JobID: id}))
		v = f.wait(t, id)
		if v.Completed != 4 || v.Completed < count {
			t.Fatalf("이어하기 %+v", v)
		}
	})
	t.Run("destination-replaced", func(t *testing.T) {
		f := createFixture(t)
		id := f.create(t)
		must(t, os.Rename(f.destination, f.destination+"-old"))
		must(t, os.Mkdir(f.destination, 0700))
		ok(t, f.s.Run(t.Context(), filemove.Request{JobID: id}))
		v := f.wait(t, id)
		if v.Failed != 4 {
			t.Fatalf("다른 폴더를 수락함 %+v", v)
		}
		for _, item := range v.Items {
			if _, e := os.Stat(item.Source); e != nil {
				t.Fatal(e)
			}
		}
	})
	t.Run("same-directory-no-op", func(t *testing.T) {
		f := createFixture(t)
		var path string
		must(t, f.db.QueryRow("SELECT filepath FROM media_file WHERE id=1").Scan(&path))
		canonical, key, e := media.CanonicalPath(filepath.Dir(path))
		must(t, e)
		dir, e := media.NewStore(f.db).EnsureDirectory(t.Context(), canonical, key, 1, true)
		must(t, e)
		id := ok(t, f.s.Create(t.Context(), filemove.CreateRequest{Selection: []filemove.Selection{{MediaID: 1, Revision: 5}}, DirectoryID: dir})).(map[string]string)["jobId"]
		ok(t, f.s.Run(t.Context(), filemove.Request{JobID: id}))
		v := f.wait(t, id)
		if v.Completed != 2 || v.Items[0].State != "excluded" {
			t.Fatalf("%+v", v)
		}
		if f.s.Run(t.Context(), filemove.Request{JobID: id}).OK {
			t.Fatal("완료된 작업 재실행 허용")
		}
	})
}

func TestMovePreservesReplacedCommittedCopyAndSharedSources(t *testing.T) {
	t.Run("changed-copy", func(t *testing.T) {
		f := createFixture(t)
		id := f.create(t)
		execSQL(t, f.db, `CREATE TRIGGER fixture_cleanup BEFORE UPDATE OF state ON file_move_item WHEN NEW.state='completed' BEGIN SELECT RAISE(ABORT,'cleanup record failure'); END`)
		ok(t, f.s.Run(t.Context(), filemove.Request{JobID: id}))
		v := f.wait(t, id)
		execSQL(t, f.db, "DROP TRIGGER fixture_cleanup")
		path := v.Items[0].Target
		must(t, os.WriteFile(path, []byte("user changed destination"), 0600))
		ok(t, f.s.Retry(t.Context(), filemove.Request{JobID: id}))
		v = f.wait(t, id)
		if v.CleanupPending != 1 || v.Completed != 3 {
			t.Fatalf("%+v", v)
		}
		b, e := os.ReadFile(path)
		must(t, e)
		if string(b) != "user changed destination" {
			t.Fatal("변경 사본 손실")
		}
	})
	t.Run("shared-source", func(t *testing.T) {
		f := createFixture(t)
		var path string
		must(t, f.db.QueryRow("SELECT filepath FROM media_file WHERE id=1").Scan(&path))
		execSQL(t, f.db, "UPDATE media_file SET filepath=? WHERE id=3", path)
		id := f.create(t)
		ok(t, f.s.Run(t.Context(), filemove.Request{JobID: id}))
		v := f.wait(t, id)
		if v.Failed != 2 || v.Completed != 2 {
			t.Fatalf("%+v", v)
		}
		if _, e := os.Stat(path); e != nil {
			t.Fatal(e)
		}
	})
}

// TestPrepareNativeVerification writes only an opt-in disposable app fixture.
func TestPrepareNativeVerification(t *testing.T) {
	output := os.Getenv("ASTERODIA_NATIVE_FIXTURE_OUTPUT")
	if output == "" {
		return
	}
	repo, e := filepath.Abs("../..")
	must(t, e)
	rel, e := filepath.Rel(filepath.Join(repo, ".test-data"), output)
	if e != nil || !filepath.IsLocal(rel) {
		t.Fatal("검증 경로 출력도 저장소 안이어야 합니다")
	}
	f := createFixture(t)
	for id := 1; id <= 4; id++ {
		var old string
		must(t, f.db.QueryRow("SELECT filepath FROM media_file WHERE id=?", id).Scan(&old))
		name := strings.TrimSuffix(old, filepath.Ext(old)) + ".png"
		picture := image.NewRGBA(image.Rect(0, 0, 480, 300))
		for y := 0; y < 300; y++ {
			for x := 0; x < 480; x++ {
				picture.Set(x, y, color.RGBA{uint8((x + id*40) % 256), uint8(y * 255 / 300), 160, 255})
			}
		}
		out, e := os.Create(name)
		must(t, e)
		must(t, png.Encode(out, picture))
		must(t, out.Close())
		execSQL(t, f.db, "UPDATE media_file SET filepath=?,kind='image',detected_format='png',width=480,height=300,title=? WHERE id=?", name, fmt.Sprintf("검증 사진 %d", id), id)
	}
	execSQL(t, f.db, "UPDATE media SET custom_title='파일 이동 검증 게시글 '||id")
	f.s.Close()
	must(t, f.db.Close())
	data, e := os.ReadFile(f.filename)
	must(t, e)
	must(t, os.WriteFile(filepath.Join(f.root, "asterodia.db"), data, 0600))
	must(t, os.WriteFile(output, []byte(f.root), 0600))
}

type moveInspector struct{}

func (moveInspector) Inspect(context.Context, string, string) (engine.LocalMetadata, error) {
	w, h, d := 8, 6, 1.0
	return engine.LocalMetadata{Kind: "video", Format: "mp4", Width: &w, Height: &h, Duration: &d}, nil
}
func TestImporterDoesNotRegisterInFlightMoveDestination(t *testing.T) {
	f := createFixture(t)
	id := f.create(t)
	target := filepath.Join(f.destination, "moving_12345678.mp4")
	must(t, os.WriteFile(target, bytes.Repeat([]byte("media fixture"), 128), 0600))
	execSQL(t, f.db, "UPDATE file_move_item SET target_path=?,target_key=?,state='copying' WHERE rowid=(SELECT min(rowid) FROM file_move_item WHERE job_id=?)", target, "move:"+strings.ToLower(target), id)
	importer := localimport.NewService(f.db, moveInspector{}, func() (string, error) { return f.destination, nil }, nil)
	defer importer.Close()
	page := ok(t, importer.Choose(t.Context())).(localimport.Page)
	deadline := time.Now().Add(5 * time.Second)
	for page.Job.State == "scanning" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		page = ok(t, importer.Get(t.Context(), localimport.Request{JobID: page.Job.ID})).(localimport.Page)
	}
	ok(t, importer.Run(t.Context(), localimport.Request{JobID: page.Job.ID}))
	for time.Now().Before(deadline) {
		page = ok(t, importer.Get(t.Context(), localimport.Request{JobID: page.Job.ID})).(localimport.Page)
		if page.Job.State != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var count int
	must(t, f.db.QueryRow("SELECT count(*) FROM media_file WHERE filepath=?", target).Scan(&count))
	if count != 0 || page.Job.Counts.Failed != 1 {
		t.Fatalf("이동 중 사본 등록 차단 실패: %+v count=%d", page.Job, count)
	}
}

type inspectionFunc func(context.Context, string, string) (engine.LocalMetadata, error)

func (f inspectionFunc) Inspect(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
	return f(ctx, path, kind)
}
func TestMoveInspectsLegacyMetadataBeforePublication(t *testing.T) {
	for _, mode := range []string{"missing-info", "unknown-kind", "inspection-error", "invalid-kind", "no-engine"} {
		t.Run(mode, func(t *testing.T) {
			f := createFixture(t)
			execSQL(t, f.db, "UPDATE media_file SET detected_format=NULL,width=NULL,height=NULL,duration=NULL WHERE id=1")
			if mode == "unknown-kind" {
				execSQL(t, f.db, "UPDATE media_file SET kind='unknown' WHERE id=1")
			}
			calls := 0
			if mode != "no-engine" {
				f.s.ConfigureInspector(inspectionFunc(func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
					calls++
					if mode == "inspection-error" {
						return engine.LocalMetadata{}, fmt.Errorf("broken media")
					}
					if mode == "invalid-kind" {
						return engine.LocalMetadata{Kind: "unknown"}, nil
					}
					return (moveInspector{}).Inspect(ctx, path, kind)
				}))
			}
			id := f.create(t)
			ok(t, f.s.Run(t.Context(), filemove.Request{JobID: id}))
			v := f.wait(t, id)
			if mode == "missing-info" || mode == "unknown-kind" {
				if v.Completed != 4 || calls != 1 {
					t.Fatalf("검사/이동 %+v calls=%d", v, calls)
				}
			} else {
				if v.Failed != 1 || v.Completed != 3 {
					t.Fatalf("정상 파일만 이동 %+v", v)
				}
			}
		})
	}
}

func TestMoveLegacyVideoAndAudioWithNativeInspector(t *testing.T) {
	ffmpeg, e := exec.LookPath("ffmpeg")
	if e != nil {
		return
	}
	ffprobe := filepath.Join(filepath.Dir(ffmpeg), "ffprobe.exe")
	if _, e = os.Stat(ffprobe); e != nil {
		return
	}
	for _, kind := range []string{"video", "audio"} {
		t.Run(kind, func(t *testing.T) {
			f := createFixture(t)
			ext := ".mp4"
			args := []string{"-y", "-f", "lavfi", "-i", "testsrc2=size=16x16:rate=2", "-t", "0.5", "-c:v", "libx264", "-pix_fmt", "yuv420p"}
			if kind == "audio" {
				ext = ".mp3"
				args = []string{"-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.5", "-c:a", "libmp3lame"}
			}
			source := filepath.Join(f.root, "legacy-real"+ext)
			args = append(args, source)
			_, e := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: ffmpeg, Directory: f.root, Timeout: 20 * time.Second, Arguments: args})
			must(t, e)
			execSQL(t, f.db, "UPDATE media_file SET filepath=?,kind='unknown',detected_format=NULL,width=NULL,height=NULL,duration=NULL WHERE id=1", source)
			f.s.ConfigureInspector(engine.LocalInspector{FFprobe: ffprobe, FFmpeg: ffmpeg, Cache: filepath.Join(f.root, "cache")})
			id := f.create(t)
			ok(t, f.s.Run(t.Context(), filemove.Request{JobID: id}))
			v := f.wait(t, id)
			if v.Completed != 4 || v.Failed != 0 {
				t.Fatalf("실제 미디어 이동 %+v", v)
			}
			var path, storedKind string
			var duration float64
			must(t, f.db.QueryRow("SELECT filepath,kind,duration FROM media_file WHERE id=1").Scan(&path, &storedKind, &duration))
			if storedKind != kind || duration <= 0 {
				t.Fatalf("메타데이터 %s %f", storedKind, duration)
			}
			must(t, engine.VerifyMedia(t.Context(), ffmpeg, path, kind))
		})
	}
}

func TestMoveRejectsOversizedSelectionBeforeReadingSources(t *testing.T) {
	f := createFixture(t)
	execSQL(t, f.db, `WITH RECURSIVE numbers(n) AS (VALUES(2) UNION ALL SELECT n+1 FROM numbers WHERE n<20002) INSERT INTO media_file(media_id,file_key,storage_kind,filepath,kind,position,created_at) SELECT 1,'limit-'||n,'app_managed','Z:\nonexistent\file.mp4','video',n,1 FROM numbers`)
	result := f.s.Create(t.Context(), filemove.CreateRequest{Selection: []filemove.Selection{{MediaID: 1, Revision: 5}}, DirectoryID: f.dir})
	if result.OK || result.Error.Code != "INVALID_ARGUMENT" {
		t.Fatalf("대상 수 제한 %+v", result)
	}
	var jobs int
	must(t, f.db.QueryRow("SELECT count(*) FROM file_move_job").Scan(&jobs))
	if jobs != 0 {
		t.Fatal("초과 요청의 부분 작업 생성")
	}
}
