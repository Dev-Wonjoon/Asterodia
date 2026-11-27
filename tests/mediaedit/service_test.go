package mediaedit_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
	"github.com/Dev-Wonjoon/Asterodia/internal/database"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"github.com/Dev-Wonjoon/Asterodia/internal/mediaedit"
	"github.com/google/uuid"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "-nostdin" {
		time.Sleep(90 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type fixture struct {
	db                                         *sql.DB
	root, source, cache, output, ffmpeg, probe string
	fileID                                     int64
	service                                    *mediaedit.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	repo, e := filepath.EvalSymlinks(filepath.Join(filepath.Dir(source), "..", ".."))
	if e != nil {
		t.Fatal(e)
	}
	base, e := filepath.EvalSymlinks(os.Getenv("ASTERODIA_TEST_ROOT"))
	if e != nil {
		t.Fatal(e)
	}
	rel, e := filepath.Rel(filepath.Join(repo, ".test-data", "mediaedit"), base)
	if e != nil || rel == "." || !filepath.IsLocal(rel) {
		t.Fatal(".test-data/mediaedit/<run> 경로가 필요합니다")
	}
	root, e := os.MkdirTemp(base, "case-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	ffmpeg, e := exec.LookPath("ffmpeg.exe")
	if e != nil {
		t.Fatal(e)
	}
	probe, e := exec.LookPath("ffprobe.exe")
	if e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, e = db.Exec("PRAGMA foreign_keys=ON;PRAGMA temp_store=MEMORY"); e != nil {
		t.Fatal(e)
	}
	if e = database.Initialize(t.Context(), db); e != nil {
		t.Fatal(e)
	}
	f := &fixture{db: db, root: root, source: filepath.Join(root, "source.mp4"), cache: filepath.Join(root, "cache"), output: filepath.Join(root, "output"), ffmpeg: ffmpeg, probe: probe}
	for n := 0; n < 10; n++ {
		img := image.NewRGBA(image.Rect(0, 0, 160, 120))
		for y := 0; y < 120; y++ {
			for x := 0; x < 160; x++ {
				img.Set(x, y, color.RGBA{uint8(n * 23), uint8(x), uint8(y), 255})
			}
		}
		out, e := os.Create(filepath.Join(root, fmt.Sprintf("input-%02d.png", n)))
		if e != nil {
			t.Fatal(e)
		}
		e = png.Encode(out, img)
		out.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
	_, e = new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: ffmpeg, Directory: root, Timeout: time.Minute, Arguments: []string{"-nostdin", "-v", "error", "-framerate", "10", "-i", filepath.Join(root, "input-%02d.png"), "-f", "lavfi", "-i", "sine=frequency=800:duration=1", "-c:v", "libx264", "-crf", "0", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", f.source}})
	if e != nil {
		t.Fatal(e)
	}
	res, e := db.Exec("INSERT INTO media(source_kind,source_title,created_at)VALUES('legacy','합성 영상',0)")
	if e != nil {
		t.Fatal(e)
	}
	id, _ := res.LastInsertId()
	res, e = db.Exec("INSERT INTO media_file(media_id,file_key,storage_kind,filepath,kind,position,duration,created_at)VALUES(?,'source','unverified_legacy',?,'video',0,1,0)", id, f.source)
	if e != nil {
		t.Fatal(e)
	}
	f.fileID, _ = res.LastInsertId()
	f.start(t, ffmpeg)
	return f
}
func (f *fixture) start(t *testing.T, ffmpeg string) {
	t.Helper()
	f.service = mediaedit.NewService(f.db, f.cache, engine.VideoEditor{FFmpeg: ffmpeg, FFprobe: f.probe}, func() string { return filepath.Join(f.output, "edits") }, nil)
	if e := f.service.Start(); e != nil {
		t.Fatal(e)
	}
	svc := f.service
	t.Cleanup(svc.Close)
}

func TestEditDraftHistoryKeepsTwentyAndEvictsOldest(t *testing.T) {
	f := newFixture(t)
	var drafts []mediaedit.Session
	for n := 0; n < 20; n++ {
		drafts = append(drafts, prepareVideoMode(t, f, mediaedit.PrepareRequest{QuickVideo: true, FullSource: true}))
		if _, err := f.db.Exec("UPDATE media_edit_session SET created_at=? WHERE id=?", n+1, drafts[n].ID); err != nil {
			t.Fatal(err)
		}
	}
	list := func(want int) []mediaedit.Session {
		t.Helper()
		data := success[map[string]any](t, f.service.List(t.Context()))
		items := data["sessions"].([]mediaedit.Session)
		if len(items) != want {
			t.Fatalf("listed drafts = %d, want %d", len(items), want)
		}
		return items
	}
	list(20)
	oldCache := filepath.Join(f.cache, drafts[0].ID)
	if err := os.MkdirAll(oldCache, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldCache, "frame.jpg"), []byte("temporary frame"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("INSERT INTO media_edit_page(session_id,page_offset,state,accessed_at) VALUES(?,0,'ready',0)", drafts[0].ID); err != nil {
		t.Fatal(err)
	}
	req := mediaedit.PrepareRequest{RequestID: uuid.NewString(), FileID: f.fileID, QuickVideo: true, FullSource: true, FPS: 1000, Kind: "gif"}
	invalid := req
	invalid.FileID = -1
	if result := f.service.Prepare(t.Context(), invalid); result.OK {
		t.Fatal("잘못된 새 초안을 허용했습니다")
	}
	list(20)
	if _, err := os.Stat(filepath.Join(oldCache, "frame.jpg")); err != nil {
		t.Fatal("새 초안 검증 전에 기존 초안을 정리했습니다", err)
	}
	created := success[mediaedit.Session](t, f.service.Prepare(t.Context(), req))
	if created.ID != req.RequestID {
		t.Fatal("21번째 초안을 만들지 못했습니다")
	}
	for _, item := range list(20) {
		if item.ID == drafts[0].ID {
			t.Fatal("가장 오래된 초안이 목록에 남았습니다")
		}
	}
	old := success[mediaedit.Session](t, f.service.GetSession(t.Context(), mediaedit.Request{ID: drafts[0].ID}))
	if old.State != "released" {
		t.Fatal("오래된 초안이 정리되지 않았습니다", old.State)
	}
	if _, err := os.Stat(oldCache); !os.IsNotExist(err) {
		t.Fatal("오래된 초안의 임시 프레임이 남았습니다", err)
	}
	var count int
	if err := f.db.QueryRow("SELECT count(*) FROM media_edit_page WHERE session_id=?", old.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("프레임 페이지 내역이 남았습니다", count, err)
	}
	if again := success[mediaedit.Session](t, f.service.Prepare(t.Context(), req)); again.ID != created.ID {
		t.Fatal("동일 요청의 새 초안이 중복 생성됐습니다")
	}
	list(20)
	if _, err := os.Stat(f.source); err != nil {
		t.Fatal("원본 파일이 변경되었습니다", err)
	}
}

func insertHistoryJob(t *testing.T, f *fixture, sessionID, state string, created int, preview bool, proof any) string {
	t.Helper()
	id := uuid.NewString()
	request := fmt.Sprintf(`{"requestId":%q,"sessionId":%q,"kind":"gif"}`, id, sessionID)
	_, err := f.db.Exec(`INSERT INTO media_edit_job(id,session_id,request_json,state,output_root,result_uuid,created_at,updated_at,preview,proof_json) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, sessionID, request, state, f.output, uuid.NewString(), created, created, preview, proof)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestEditJobHistoryProtectsActiveRecoveryAndPublishedFiles(t *testing.T) {
	f := newFixture(t)
	draft := prepareVideoMode(t, f, mediaedit.PrepareRequest{QuickVideo: true, FullSource: true})
	protected := []string{
		insertHistoryJob(t, f, draft.ID, "running", 0, false, nil),
		insertHistoryJob(t, f, draft.ID, "interrupted", 1, false, "{}"),
		insertHistoryJob(t, f, draft.ID, "stop_unconfirmed", 2, false, nil),
	}
	if err := os.MkdirAll(filepath.Join(f.cache, "previews"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.output, 0700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(f.output, "kept.gif")
	if err := os.WriteFile(output, []byte("published result"), 0600); err != nil {
		t.Fatal(err)
	}
	var oldest []string
	for n := 0; n < 20; n++ {
		preview := n%2 == 0
		id := insertHistoryJob(t, f, draft.ID, []string{"completed", "failed", "cancelled"}[n%3], n+3, preview, nil)
		oldest = append(oldest, id)
		if preview {
			if err := os.WriteFile(filepath.Join(f.cache, "previews", id+".gif"), []byte("sample"), 0600); err != nil {
				t.Fatal(err)
			}
		} else if _, err := f.db.Exec("UPDATE media_edit_job SET result_path=? WHERE id=?", output, id); err != nil {
			t.Fatal(err)
		}
	}
	request := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: draft.ID, Kind: "gif", Crop: engine.EditCrop{Width: 160, Height: 120}}
	result := f.service.Create(t.Context(), request)
	if !result.OK {
		t.Fatal("새 작업 등록 실패", result.Error)
	}
	deadline := time.Now().Add(5 * time.Second)
	var jobs []mediaedit.Job
	for time.Now().Before(deadline) {
		data := success[map[string]any](t, f.service.List(t.Context()))
		jobs = data["jobs"].([]mediaedit.Job)
		if len(jobs) <= 20 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(jobs) != 20 {
		t.Fatal("작업 내역이 20개로 정리되지 않았습니다", len(jobs))
	}
	for _, id := range protected {
		if value := f.service.GetJob(t.Context(), mediaedit.Request{ID: id}); !value.OK {
			t.Fatal("진행 중 또는 복구 작업이 삭제되었습니다", id)
		}
	}
	if value := f.service.GetJob(t.Context(), mediaedit.Request{ID: oldest[0]}); value.OK {
		t.Fatal("오래된 완료 내역이 남았습니다")
	}
	if _, err := os.Stat(filepath.Join(f.cache, "previews", oldest[0]+".gif")); !os.IsNotExist(err) {
		t.Fatal("오래된 미리보기 파일이 남았습니다", err)
	}
	if contents, err := os.ReadFile(output); err != nil || string(contents) != "published result" {
		t.Fatal("저장된 결과 파일이 변경되었습니다", err)
	}
	if _, err := os.Stat(f.source); err != nil {
		t.Fatal("원본 파일이 사라졌습니다", err)
	}
}

func TestEditHistoryPrunesOnRestartAndKeepsDraftsUsedByRecovery(t *testing.T) {
	f := newFixture(t)
	f.service.Close()
	var ids []string
	for n := 0; n < 23; n++ {
		id := uuid.NewString()
		ids = append(ids, id)
		request := fmt.Sprintf(`{"requestId":%q,"fileId":%d,"kind":"gif"}`, id, f.fileID)
		_, err := f.db.Exec(`INSERT INTO media_edit_session(id,source_file_id,source_uuid,source_path,source_title,request_json,state,created_at,updated_at) VALUES(?,?,?,?,?,?,'ready',1,1)`, id, f.fileID, uuid.NewString(), f.source, "history", request)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(f.cache, id), 0700); err != nil {
			t.Fatal(err)
		}
	}
	job := insertHistoryJob(t, f, ids[0], "interrupted", 0, false, "{}")
	f.start(t, f.ffmpeg)
	data := success[map[string]any](t, f.service.List(t.Context()))
	sessions := data["sessions"].([]mediaedit.Session)
	if len(sessions) != 20 {
		t.Fatal("재시작 후 초안이 정리되지 않았습니다", len(sessions))
	}
	if oldest := success[mediaedit.Session](t, f.service.GetSession(t.Context(), mediaedit.Request{ID: ids[0]})); oldest.State != "ready" {
		t.Fatal("복구 작업이 참조하는 초안이 정리되었습니다")
	}
	for _, id := range ids[1:4] {
		if draft := success[mediaedit.Session](t, f.service.GetSession(t.Context(), mediaedit.Request{ID: id})); draft.State != "released" {
			t.Fatal("오래된 비활성 초안이 정리되지 않았습니다", id)
		}
		if _, err := os.Stat(filepath.Join(f.cache, id)); !os.IsNotExist(err) {
			t.Fatal("초안 캐시가 남았습니다", err)
		}
	}
	if value := f.service.GetJob(t.Context(), mediaedit.Request{ID: job}); !value.OK {
		t.Fatal("재시작 시 복구 내역이 삭제되었습니다")
	}
}
func success[T any](t *testing.T, r api.Result) T {
	t.Helper()
	if !r.OK {
		t.Fatalf("서비스 실패 %+v", r.Error)
	}
	v, ok := r.Data.(T)
	if !ok {
		t.Fatalf("응답 타입 %T", r.Data)
	}
	return v
}
func (f *fixture) prepare(t *testing.T, kind string) mediaedit.Session {
	t.Helper()
	req := mediaedit.PrepareRequest{RequestID: uuid.NewString(), FileID: f.fileID, Start: 0, End: 1, FPS: 10, Width: 160, Kind: kind}
	v := success[mediaedit.Session](t, f.service.Prepare(t.Context(), req))
	for until := time.Now().Add(15 * time.Second); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
		v = success[mediaedit.Session](t, f.service.GetSession(t.Context(), mediaedit.Request{ID: v.ID}))
		if v.State == "ready" {
			return v
		}
		if v.State == "failed" {
			var detail string
			f.db.QueryRow("SELECT diagnostic FROM media_edit_session WHERE id=?", v.ID).Scan(&detail)
			t.Fatalf("준비 실패 %s %+v", detail, v)
		}
	}
	t.Fatal("프레임 준비 시간 초과")
	return v
}
func (f *fixture) waitJob(t *testing.T, id, state string) mediaedit.Job {
	t.Helper()
	for until := time.Now().Add(20 * time.Second); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
		v := success[mediaedit.Job](t, f.service.GetJob(t.Context(), mediaedit.Request{ID: id}))
		if v.State == state {
			return v
		}
		if v.State == "failed" || v.State == "interrupted" || v.State == "cancelled" {
			var detail string
			f.db.QueryRow("SELECT diagnostic FROM media_edit_job WHERE id=?", id).Scan(&detail)
			t.Fatalf("작업 실패 %s %+v", detail, v)
		}
	}
	t.Fatal("작업 시간 초과")
	return mediaedit.Job{}
}
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestGIFCropDeleteAndIdempotency(t *testing.T) {
	f := newFixture(t)
	original := sha256.Sum256(readFile(t, f.source))
	v := f.prepare(t, "gif")
	if v.FrameCount != 10 || v.Width != 160 || v.Height != 120 {
		t.Fatal(v)
	}
	req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{X: 10, Y: 8, Width: 80, Height: 60}, Deleted: []int{8, 3, 1}}
	j := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
	same := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
	if same.ID != j.ID {
		t.Fatal("중복 작업 생성")
	}
	req.Crop.Width++
	if f.service.Create(t.Context(), req).OK {
		t.Fatal("같은 ID의 다른 옵션 허용")
	}
	j = f.waitJob(t, j.ID, "completed")
	path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: j.ID}))
	file, e := os.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	animation, e := gif.DecodeAll(file)
	file.Close()
	if e != nil {
		t.Fatal(e)
	}
	if len(animation.Image) != 7 || animation.Config.Width != 80 || animation.Config.Height != 60 {
		t.Fatal("프레임·크롭 불일치", len(animation.Image), animation.Config)
	}
	delay := 0
	for _, d := range animation.Delay {
		delay += d
	}
	if delay != 70 {
		t.Fatal("삭제한 프레임의 시간이 남음", delay)
	}
	kept := []int{0, 2, 4, 5, 6, 7, 9}
	for n, index := range kept {
		pngFile, e := os.Open(filepath.Join(f.root, fmt.Sprintf("input-%02d.png", index)))
		if e != nil {
			t.Fatal(e)
		}
		frame, e := png.Decode(pngFile)
		pngFile.Close()
		if e != nil {
			t.Fatal(e)
		}
		r1, g1, b1, _ := frame.At(50, 38).RGBA()
		r2, g2, b2, _ := animation.Image[n].At(40, 30).RGBA()
		difference := func(a, b uint32) uint32 {
			if a > b {
				return a - b
			}
			return b - a
		}
		if difference(r1, r2) > 30*257 || difference(g1, g2) > 30*257 || difference(b1, b2) > 30*257 {
			t.Fatalf("프레임 순서 또는 크롭 위치 불일치 %d 원본 %d", n, index)
		}
	}
	if sha256.Sum256(readFile(t, f.source)) != original {
		t.Fatal("원본 변경")
	}
	var kind, sourceUUID string
	if e = f.db.QueryRow("SELECT m.source_kind,s.source_uuid FROM media_edit_job j JOIN media_edit_session s ON s.id=j.session_id JOIN media_file f ON f.id=j.result_file_id JOIN media m ON m.id=f.media_id WHERE j.id=?", j.ID).Scan(&kind, &sourceUUID); e != nil || kind != "edited" || sourceUUID == "" {
		t.Fatal(kind, e)
	}
	if !f.service.Release(t.Context(), mediaedit.Request{ID: v.ID}).OK {
		t.Fatal("자료 정리 실패")
	}
	if _, e = os.Stat(path); e != nil {
		t.Fatal("결과가 자료 정리로 사라짐", e)
	}
}

func TestTrimAndMute(t *testing.T) {
	f := newFixture(t)
	v := f.prepare(t, "trim")
	for _, mute := range []bool{false, true} {
		req := mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "trim", Mute: mute}
		j := success[mediaedit.Job](t, f.service.Create(t.Context(), req))
		f.waitJob(t, j.ID, "completed")
		path := success[string](t, f.service.ResultPath(t.Context(), mediaedit.Request{ID: j.ID}))
		_, e := (engine.LocalInspector{FFmpeg: f.ffmpeg, FFprobe: f.probe}).Inspect(t.Context(), path, "audio")
		if (e == nil) == mute {
			t.Fatal("오디오 유지·제거 불일치", mute, e)
		}
	}
}

func TestCancelAndActiveSourceProtection(t *testing.T) {
	f := newFixture(t)
	f.service.Close()
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	f.start(t, executable)
	v := success[mediaedit.Session](t, f.service.Prepare(t.Context(), mediaedit.PrepareRequest{RequestID: uuid.NewString(), FileID: f.fileID, Start: 0, End: 1, FPS: 10, Width: 160, Kind: "gif"}))
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); time.Sleep(10 * time.Millisecond) {
		v = success[mediaedit.Session](t, f.service.GetSession(t.Context(), mediaedit.Request{ID: v.ID}))
		if v.State == "preparing" {
			break
		}
	}
	if _, e = f.db.Exec("DELETE FROM media_file WHERE id=?", f.fileID); e == nil {
		t.Fatal("실행 중 원본 등록 해제 허용")
	}
	if !f.service.Cancel(t.Context(), mediaedit.Request{ID: v.ID}).OK {
		t.Fatal("취소 실패")
	}
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); time.Sleep(10 * time.Millisecond) {
		v = success[mediaedit.Session](t, f.service.GetSession(t.Context(), mediaedit.Request{ID: v.ID}))
		if v.State == "cancelled" {
			if v.Cancelling {
				t.Fatal("취소 완료 뒤 취소 중 표시 유지")
			}
			return
		}
	}
	t.Fatal("취소 시간 초과", v)
}

func TestSingleRemainingGIFFrame(t *testing.T) {
	f := newFixture(t)
	v := f.prepare(t, "gif")
	deleted := []int{}
	for n := 1; n < v.FrameCount; n++ {
		deleted = append(deleted, n)
	}
	j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{Width: 79, Height: 59}, Deleted: deleted}))
	f.waitJob(t, j.ID, "completed")
}

func TestGIFAtMaximumFrameCount(t *testing.T) {
	f := newFixture(t)
	looped := filepath.Join(f.root, "looped.mp4")
	_, e := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: f.ffmpeg, Directory: f.root, Timeout: time.Minute, Arguments: []string{"-nostdin", "-v", "error", "-stream_loop", "29", "-i", f.source, "-t", "30", "-c", "copy", looped}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec("UPDATE media_file SET filepath=?,duration=30 WHERE id=?", looped, f.fileID); e != nil {
		t.Fatal(e)
	}
	v := success[mediaedit.Session](t, f.service.Prepare(t.Context(), mediaedit.PrepareRequest{RequestID: uuid.NewString(), FileID: f.fileID, Kind: "gif", Start: 0, End: 30, FPS: 30, Width: 160}))
	for until := time.Now().Add(30 * time.Second); time.Now().Before(until); time.Sleep(30 * time.Millisecond) {
		v = success[mediaedit.Session](t, f.service.GetSession(t.Context(), mediaedit.Request{ID: v.ID}))
		if v.State == "ready" || v.State == "failed" {
			break
		}
	}
	if v.State != "ready" || v.FrameCount != 900 {
		t.Fatal("최대 프레임 준비 실패", v)
	}
	j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{Width: 80, Height: 60}, Deleted: []int{1, 33, 899}}))
	f.waitJob(t, j.ID, "completed")
}

func TestPublicationRecoveryAndCollision(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			f := newFixture(t)
			v := f.prepare(t, "gif")
			id := uuid.NewString()
			if collision {
				if e := os.MkdirAll(filepath.Join(f.output, "edits"), 0700); e != nil {
					t.Fatal(e)
				}
				path := filepath.Join(f.output, "edits", media.PathComponent(v.Title, "영상", 70)+"_"+id+".gif")
				if e := os.WriteFile(path, []byte("기존 파일"), 0600); e != nil {
					t.Fatal(e)
				}
			} else {
				if _, e := f.db.Exec("CREATE TRIGGER injected_failure BEFORE INSERT ON media WHEN NEW.source_kind='edited' BEGIN SELECT RAISE(ABORT,'injected commit failure'); END"); e != nil {
					t.Fatal(e)
				}
			}
			success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: id, SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{Width: v.Width, Height: v.Height}, Deleted: []int{1}}))
			for until := time.Now().Add(15 * time.Second); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
				j := success[mediaedit.Job](t, f.service.GetJob(t.Context(), mediaedit.Request{ID: id}))
				if j.State == "interrupted" || j.State == "failed" {
					break
				}
			}
			f.service.Close()
			if !collision {
				if _, e := f.db.Exec("DROP TRIGGER injected_failure"); e != nil {
					t.Fatal(e)
				}
			}
			f.start(t, f.ffmpeg)
			if collision {
				var path string
				path = filepath.Join(f.output, "edits", media.PathComponent(v.Title, "영상", 70)+"_"+id+".gif")
				if string(readFile(t, path)) != "기존 파일" {
					t.Fatal("기존 파일 변경")
				}
				j := success[mediaedit.Job](t, f.service.GetJob(t.Context(), mediaedit.Request{ID: id}))
				if j.State == "completed" {
					t.Fatal("충돌 파일을 결과로 등록")
				}
			} else {
				f.waitJob(t, id, "completed")
				var n int
				f.db.QueryRow("SELECT count(*) FROM media WHERE source_kind='edited'").Scan(&n)
				if n != 1 {
					t.Fatal("복구 중복 등록", n)
				}
			}
		})
	}
}

func TestValidationAndSourceChange(t *testing.T) {
	f := newFixture(t)
	bad := f.service.Prepare(t.Context(), mediaedit.PrepareRequest{RequestID: uuid.NewString(), FileID: f.fileID, Start: 1, End: 0, FPS: 10, Width: 160})
	if bad.OK || bad.Error.Code != "INVALID_ARGUMENT" {
		t.Fatal(bad)
	}
	v := f.prepare(t, "gif")
	if f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: v.ID, SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{Width: 80, Height: 60}}).OK {
		t.Fatal("편집 자료 ID와 출력 작업 ID 충돌 허용")
	}
	for _, req := range []mediaedit.CreateRequest{{RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{Width: 161, Height: 120}}, {RequestID: uuid.NewString(), SessionID: v.ID, Kind: "gif", Crop: engine.EditCrop{Width: 80, Height: 60}, Deleted: []int{1, 1}}} {
		if f.service.Create(context.Background(), req).OK {
			t.Fatal("잘못된 요청 허용")
		}
	}
	trim := f.prepare(t, "trim")
	if e := os.WriteFile(f.source, []byte(strings.Repeat("changed", 10)), 0600); e != nil {
		t.Fatal(e)
	}
	j := success[mediaedit.Job](t, f.service.Create(t.Context(), mediaedit.CreateRequest{RequestID: uuid.NewString(), SessionID: trim.ID, Kind: "trim"}))
	f.waitJob(t, j.ID, "failed")
}
