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

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"github.com/Dev-Wonjoon/StorageBucket/internal/mediaedit"
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
	base, e := filepath.EvalSymlinks(os.Getenv("STORAGEBUCKET_TEST_ROOT"))
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
	f.service = mediaedit.NewService(f.db, f.cache, engine.VideoEditor{FFmpeg: ffmpeg, FFprobe: f.probe}, func() string { return f.output }, nil)
	if e := f.service.Start(); e != nil {
		t.Fatal(e)
	}
	svc := f.service
	t.Cleanup(svc.Close)
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
