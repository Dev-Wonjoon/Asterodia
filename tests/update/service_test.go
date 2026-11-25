package update_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/desktop"
	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/filemove"
	"github.com/Dev-Wonjoon/StorageBucket/internal/localimport"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"github.com/Dev-Wonjoon/StorageBucket/internal/mediaedit"
	appupdate "github.com/Dev-Wonjoon/StorageBucket/internal/update"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

type fakeBackend struct {
	mu                          sync.Mutex
	state                       updater.State
	release                     *updater.Release
	checkErr, errorRestart      error
	checks, downloads, restarts int
	checking                    chan struct{}
	resume                      chan struct{}
}

func (b *fakeBackend) Init(updater.Config) error { b.set(updater.StateIdle); return nil }
func (b *fakeBackend) set(state updater.State)   { b.mu.Lock(); b.state = state; b.mu.Unlock() }
func (b *fakeBackend) State() updater.State      { b.mu.Lock(); defer b.mu.Unlock(); return b.state }
func (b *fakeBackend) Check(ctx context.Context) (*updater.Release, error) {
	b.mu.Lock()
	b.checks++
	b.state = updater.StateChecking
	b.mu.Unlock()
	if b.checking != nil {
		close(b.checking)
		select {
		case <-b.resume:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if b.checkErr != nil {
		b.set(updater.StateError)
		return nil, b.checkErr
	}
	if b.release != nil {
		b.set(updater.StateAvailable)
	} else {
		b.set(updater.StateUpToDate)
	}
	return b.release, nil
}
func (b *fakeBackend) DownloadAndInstall(context.Context) error {
	b.downloads++
	b.set(updater.StateReady)
	return nil
}
func (b *fakeBackend) Restart(context.Context) error { b.restarts++; return b.errorRestart }

func setup(t *testing.T) (*appupdate.Service, *fakeBackend, *api.UpdateGate, *sql.DB) {
	t.Helper()
	gate := &api.UpdateGate{}
	s := appupdate.NewService("2.0.1", gate)
	b := &fakeBackend{release: &updater.Release{Version: "2.0.2", Notes: "notes", Artifact: updater.Artifact{Size: 100}}}
	p, err := appupdate.NewProvider("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Configure(b, p); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(t.Context(), filepath.Join(t.TempDir(), "updates.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); db.Close() })
	s.SetDatabase(db)
	return s, b, gate, db
}

func TestCheckDownloadRestart(t *testing.T) {
	s, b, gate, _ := setup(t)
	if result := s.Restart(t.Context()); result.OK {
		t.Fatal("restart without verified download")
	}
	if result := s.Check(t.Context()); !result.OK {
		t.Fatal(result.Error)
	}
	if status := s.Status().Data.(appupdate.Status); status.Version != "2.0.2" || status.Notes != "notes" {
		t.Fatal(status)
	}
	if result := s.Download(t.Context()); !result.OK {
		t.Fatal(result.Error)
	}
	// A periodic/manual check must preserve a verified staged update.
	_ = s.Check(t.Context())
	if b.checks != 1 {
		t.Fatal("ready update was rechecked")
	}
	leave, ok := gate.Enter()
	if !ok {
		t.Fatal("gate frozen before restart")
	}
	if result := s.Restart(t.Context()); result.OK || b.restarts != 0 {
		t.Fatal("in-flight work was interrupted")
	}
	leave()
	if result := s.Restart(t.Context()); !result.OK {
		t.Fatal(result.Error)
	}
	if _, ok := gate.Enter(); ok {
		t.Fatal("new work allowed during executable swap")
	}
	if b.restarts != 1 || s.Status().Data.(appupdate.Status).State != "restarting" {
		t.Fatal("restart not recorded")
	}
	gate.Thaw()
}

func TestReleaseNotesDisplayWithoutOuterMarkdownFence(t *testing.T) {
	s, b, _, _ := setup(t)
	for _, tc := range []struct {
		name, notes, want string
	}{
		{"markdown", "```markdown\n## 한국어\n- 썸네일 개선\n\n## English\n- Improve thumbnails\n```", "## 한국어\n- 썸네일 개선\n\n## English\n- Improve thumbnails"},
		{"md", "```md\n- Changes\n```", "- Changes"},
		{"unlabeled", "```\n- Changes\n```", "- Changes"},
		{"windows-newlines", "\r\n```Markdown\r\n## 한국어\r\n- 변경 사항\r\n```\r\n", "## 한국어\r\n- 변경 사항"},
		{"code-example", "````markdown\n## Example\n```go\nfmt.Println(1)\n```\n````", "## Example\n```go\nfmt.Println(1)\n```"},
		{"plain", "  ## Changes\n- Keep `inline code`\n", "  ## Changes\n- Keep `inline code`\n"},
		{"embedded-example", "## Example\n```markdown\n# Sample\n```", "## Example\n```markdown\n# Sample\n```"},
		{"other-language", "```go\nfmt.Println(1)\n```", "```go\nfmt.Println(1)\n```"},
		{"multiple-blocks", "```markdown\nFirst\n```\nSecond\n```markdown\nThird\n```", "```markdown\nFirst\n```\nSecond\n```markdown\nThird\n```"},
		{"unclosed", "```markdown\n- Changes", "```markdown\n- Changes"},
		{"empty-wrapper", "```markdown\n```", ""},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b.release.Notes = tc.notes
			result := s.Check(t.Context())
			if !result.OK {
				t.Fatal(result.Error)
			}
			if status := result.Data.(appupdate.Status); status.Notes != tc.want || status.Version != "2.0.2" {
				t.Fatalf("display notes = %q, want %q", status.Notes, tc.want)
			}
			if got := s.Status().Data.(appupdate.Status).Notes; got != tc.want {
				t.Fatalf("polled notes = %q, want %q", got, tc.want)
			}
			if b.release.Notes != tc.notes {
				t.Fatal("original release notes were modified")
			}
		})
	}
}

func TestFailedRestartReopensWorkGate(t *testing.T) {
	s, b, gate, _ := setup(t)
	_ = s.Check(t.Context())
	_ = s.Download(t.Context())
	b.errorRestart = errors.New("helper failed")
	if r := s.Restart(t.Context()); r.OK {
		t.Fatal("failure hidden")
	}
	leave, ok := gate.Enter()
	if !ok {
		t.Fatal("work blocked after failed restart")
	}
	leave()
	b.errorRestart = nil
	if r := s.Restart(t.Context()); !r.OK {
		t.Fatal(r.Error)
	}
	gate.Thaw()
}

func TestConcurrentChecksAreCoalesced(t *testing.T) {
	s, b, _, _ := setup(t)
	b.checking = make(chan struct{})
	b.resume = make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); _ = s.Check(context.Background()) }()
	<-b.checking
	if r := s.Check(t.Context()); !r.OK {
		t.Fatal(r.Error)
	}
	if r := s.Download(t.Context()); r.OK {
		t.Fatal("download allowed during check")
	}
	close(b.resume)
	<-done
	if b.checks != 1 {
		t.Fatal(b.checks)
	}
}

func TestCheckFailureCanBeRetried(t *testing.T) {
	s, b, _, _ := setup(t)
	b.checkErr = errors.New("offline")
	_ = s.Check(t.Context())
	status := s.Status().Data.(appupdate.Status)
	if status.State != "error" || status.Error == "" || status.Version != "" {
		t.Fatal(status)
	}
	b.checkErr = nil
	_ = s.Check(t.Context())
	if status = s.Status().Data.(appupdate.Status); status.State != "available" || status.Error != "" {
		t.Fatal(status)
	}
}

func TestChecksAutomaticallyWithoutDownloading(t *testing.T) {
	s, b, _, _ := setup(t)
	s.Start()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(20 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("startup update check was not performed promptly")
		case <-poll.C:
			if s.Status().Data.(appupdate.Status).State == "available" {
				s.Close()
				if b.downloads != 0 || b.restarts != 0 {
					t.Fatal("startup downloaded or restarted without a user action")
				}
				return
			}
		}
	}
}

func TestIdleCheckFailsClosed(t *testing.T) {
	_, _, _, db := setup(t)
	if err := appupdate.CheckIdle(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err := appupdate.CheckIdle(t.Context(), nil); err == nil {
		t.Fatal("unknown work state accepted")
	}
	if _, err := db.Exec("DROP TABLE media_edit_page"); err != nil {
		t.Fatal(err)
	}
	if err := appupdate.CheckIdle(t.Context(), db); err == nil {
		t.Fatal("unreadable work state accepted")
	}
}

func TestBackgroundWorkBlocksRestart(t *testing.T) {
	for _, table := range []string{"download_queue", "local_import_job", "file_move_job", "file_move_item", "maintenance_task", "media_edit_session", "media_edit_job", "media_edit_page"} {
		t.Run(table, func(t *testing.T) {
			s, b, gate, _ := setup(t)
			memory, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			memory.SetMaxOpenConns(1)
			for _, name := range []string{"download_queue", "local_import_job", "file_move_job", "file_move_item", "maintenance_task", "media_edit_session", "media_edit_job", "media_edit_page"} {
				if _, err = memory.Exec("CREATE TABLE " + name + " (state TEXT, cleanup_required INTEGER DEFAULT 0, kind TEXT DEFAULT 'deletion_cleanup')"); err != nil {
					t.Fatal(err)
				}
			}
			state := "running"
			if table == "file_move_item" {
				state = "copying"
			}
			if table == "media_edit_session" {
				state = "preparing"
			}
			if _, err = memory.Exec("INSERT INTO "+table+" (state) VALUES (?)", state); err != nil {
				t.Fatal(err)
			}
			s.SetDatabase(memory)
			_ = s.Check(t.Context())
			_ = s.Download(t.Context())
			if result := s.Restart(t.Context()); result.OK || b.restarts != 0 {
				t.Fatal("background work was interrupted")
			}
			leave, ok := gate.Enter()
			if !ok {
				t.Fatal("gate not reopened after busy check")
			}
			leave()
		})
	}
}

func TestRestartAllowsThumbnailCacheAfterImportStops(t *testing.T) {
	for _, state := range []string{"paused", "cancelled", "completed", "failed"} {
		t.Run(state, func(t *testing.T) {
			s, backend, _, _ := setup(t)
			memory, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			memory.SetMaxOpenConns(1)
			for _, table := range []string{"download_queue", "local_import_job", "file_move_job", "file_move_item", "maintenance_task", "media_edit_session", "media_edit_job", "media_edit_page"} {
				if _, err = memory.Exec("CREATE TABLE " + table + " (state TEXT, cleanup_required INTEGER DEFAULT 0, kind TEXT)"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = memory.Exec("INSERT INTO maintenance_task(kind,state) VALUES ('preview_generation','pending'),('preview_generation','running'); INSERT INTO local_import_job(state) VALUES (?)", state); err != nil {
				t.Fatal(err)
			}
			s.SetDatabase(memory)
			if err = appupdate.CheckIdle(t.Context(), memory); err != nil {
				t.Fatal(err)
			}
			// Cancellation must still finish source-file cleanup before restart.
			if _, err = memory.Exec("UPDATE local_import_job SET cleanup_required=1"); err != nil {
				t.Fatal(err)
			}
			if err = appupdate.CheckIdle(t.Context(), memory); err == nil {
				t.Fatal("unfinished import cleanup accepted")
			}
			if _, err = memory.Exec("UPDATE local_import_job SET cleanup_required=0"); err != nil {
				t.Fatal(err)
			}
			_ = s.Check(t.Context())
			_ = s.Download(t.Context())
			if result := s.Restart(t.Context()); !result.OK || backend.restarts != 1 {
				t.Fatal(result)
			}
		})
	}
}

func TestFrozenGateRejectsDesktopAndMobileMutations(t *testing.T) {
	gate := &api.UpdateGate{}
	if !gate.Freeze() {
		t.Fatal("freeze failed")
	}
	defer gate.Thaw()
	ctx := t.Context()
	results := []api.Result{
		(&download.Service{UpdateGate: gate}).Enqueue(ctx, download.EnqueueRequest{}),
		(&download.Service{UpdateGate: gate}).Run(ctx, download.RunRequest{}),
		(&filemove.Service{UpdateGate: gate}).Create(ctx, filemove.CreateRequest{}),
		(&localimport.Service{UpdateGate: gate}).Choose(ctx),
		(&mediaedit.Service{UpdateGate: gate}).Prepare(ctx, mediaedit.PrepareRequest{}),
		(&mediaedit.Service{UpdateGate: gate}).Create(ctx, mediaedit.CreateRequest{}),
		(&mediaedit.Service{UpdateGate: gate}).Frames(ctx, mediaedit.FramesRequest{}),
		(&desktop.Service{UpdateGate: gate}).ImportDatabase(ctx),
		(&media.Service{UpdateGate: gate}).Edit(ctx, media.EditRequest{}),
	}
	for i, r := range results {
		if r.OK || r.Error.Code != "UPDATE_RESTARTING" {
			t.Fatalf("mutation %d: %+v", i, r)
		}
	}
	if _, err := (&engine.Manager{UpdateGate: gate}).Install(ctx, "ffmpeg", "test"); err == nil {
		t.Fatal("engine installation allowed")
	}
}
