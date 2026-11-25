package update

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

type Backend interface {
	Init(updater.Config) error
	State() updater.State
	Check(context.Context) (*updater.Release, error)
	DownloadAndInstall(context.Context) error
	Restart(context.Context) error
}

type Status struct {
	CurrentVersion string `json:"currentVersion"`
	State          string `json:"state"`
	Version        string `json:"version"`
	Notes          string `json:"notes"`
	Size           int64  `json:"size"`
	Written        int64  `json:"written"`
	Error          string `json:"error"`
	LastChecked    int64  `json:"lastChecked"`
	Supported      bool   `json:"supported"`
}

type Service struct {
	mu         sync.Mutex
	op         sync.Mutex
	backend    Backend
	gate       *api.UpdateGate
	db         *sql.DB
	status     Status
	cancel     context.CancelFunc
	done       chan struct{}
	restarting bool
}

func NewService(version string, gate *api.UpdateGate) *Service {
	if gate == nil {
		gate = &api.UpdateGate{}
	}
	return &Service{gate: gate, status: Status{CurrentVersion: version, State: "idle", Supported: runtime.GOOS == "windows" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") && validVersion(version)}}
}

//wails:ignore
func (s *Service) Configure(backend Backend, provider *Provider) error {
	if err := backend.Init(updater.Config{CurrentVersion: s.status.CurrentVersion, Providers: []updater.Provider{provider}, Window: updater.WindowNone}); err != nil {
		return err
	}
	provider.Progress = func(written, _ int64) { s.mu.Lock(); s.status.Written = written; s.mu.Unlock() }
	s.backend = backend
	return nil
}

//wails:ignore
func (s *Service) SetDatabase(db *sql.DB) { s.db = db }

//wails:ignore
func (s *Service) Start() {
	s.mu.Lock()
	if s.done != nil || !s.status.Supported {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.done = cancel, make(chan struct{})
	done := s.done
	s.mu.Unlock()
	go func() {
		defer close(done)
		// Check immediately in the background after updater initialization.
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				_ = s.Check(ctx)
				timer.Reset(6 * time.Hour)
			}
		}
	}()
}

//wails:ignore
func (s *Service) Close() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *Service) snapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status
	if s.restarting {
		status.State = "restarting"
	} else if s.backend != nil {
		status.State = string(s.backend.State())
	}
	if !status.Supported {
		status.State = "unsupported"
	}
	return status
}

func (s *Service) Status() api.Result { return api.Success(s.snapshot()) }

func (s *Service) Check(ctx context.Context) api.Result {
	if !s.op.TryLock() {
		return s.Status()
	}
	defer s.op.Unlock()
	status := s.snapshot()
	if !status.Supported || s.backend == nil {
		return api.Failure("UPDATE_UNAVAILABLE", "현재 실행 환경에서는 자동 업데이트를 사용할 수 없습니다")
	}
	if status.State == "ready" || status.State == "restarting" {
		return s.Status()
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	release, err := s.backend.Check(ctx)
	s.mu.Lock()
	s.status.LastChecked = time.Now().UnixMilli()
	s.status.Error = ""
	if err != nil {
		s.status.Error = "업데이트를 확인하지 못했습니다. " + err.Error()
	}
	if release == nil {
		s.status.Version, s.status.Notes, s.status.Size, s.status.Written = "", "", 0, 0
	} else {
		s.status.Version, s.status.Notes, s.status.Size, s.status.Written = release.Version, displayReleaseNotes(release.Notes), release.Artifact.Size, 0
	}
	s.mu.Unlock()
	return s.Status()
}

func displayReleaseNotes(notes string) string {
	lines := strings.Split(strings.TrimSpace(notes), "\n")
	if len(lines) < 2 {
		return notes
	}
	opening := strings.TrimSpace(lines[0])
	width := len(opening) - len(strings.TrimLeft(opening, "`"))
	if width < 3 {
		return notes
	}
	language := strings.TrimSpace(opening[width:])
	if language != "" && !strings.EqualFold(language, "markdown") && !strings.EqualFold(language, "md") {
		return notes
	}
	// Unwrap only a Markdown fence surrounding the entire release note.
	// Longer outer fences may contain shorter fences used by code examples.
	for index, line := range lines[1:] {
		closing := strings.TrimSpace(line)
		if len(closing) >= width && strings.Trim(closing, "`") == "" {
			if index+1 == len(lines)-1 {
				return strings.TrimSuffix(strings.Join(lines[1:len(lines)-1], "\n"), "\r")
			}
			break
		}
	}
	return notes
}

func (s *Service) Download(ctx context.Context) api.Result {
	if !s.op.TryLock() {
		return api.Failure("UPDATE_BUSY", "업데이트 요청을 처리하고 있습니다")
	}
	defer s.op.Unlock()
	status := s.snapshot()
	if !status.Supported || s.backend == nil || status.Version == "" || status.State == "restarting" {
		return api.Failure("UPDATE_NOT_READY", "먼저 새 버전을 확인하세요")
	}
	if status.State == "ready" {
		return s.Status()
	}
	s.mu.Lock()
	s.status.Error, s.status.Written = "", 0
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if err := s.backend.DownloadAndInstall(ctx); err != nil {
		s.mu.Lock()
		s.status.Error = "업데이트를 다운로드하거나 검증하지 못했습니다. " + err.Error()
		s.mu.Unlock()
	}
	return s.Status()
}

func (s *Service) Restart(ctx context.Context) api.Result {
	if !s.op.TryLock() {
		return api.Failure("UPDATE_BUSY", "업데이트 요청을 처리하고 있습니다")
	}
	defer s.op.Unlock()
	if s.backend == nil || s.snapshot().State != "ready" {
		return api.Failure("UPDATE_NOT_READY", "업데이트 다운로드와 검증을 먼저 완료하세요")
	}
	if !s.gate.Freeze() {
		return api.Failure("UPDATE_WORK_ACTIVE", "작업을 처리하고 있습니다. 다운로드·가져오기·편집·엔진 설치를 마친 뒤 다시 시도하세요")
	}
	if err := CheckIdle(ctx, s.db); err != nil {
		s.gate.Thaw()
		return api.Failure("UPDATE_WORK_ACTIVE", err.Error())
	}
	s.mu.Lock()
	s.restarting = true
	s.status.Error = ""
	s.mu.Unlock()
	if err := s.backend.Restart(ctx); err != nil {
		s.mu.Lock()
		s.restarting = false
		s.status.Error = "업데이트를 적용하지 못했습니다. " + err.Error()
		s.mu.Unlock()
		s.gate.Thaw()
		return api.Failure("UPDATE_RESTART_FAILED", "업데이트를 적용하지 못했습니다. "+err.Error())
	}
	// Keep the gate frozen until process exit; no new desktop/mobile work may start.
	return s.Status()
}

// CheckIdle covers durable work after submission. Thumbnail cache generation is
// restartable: Close joins its worker and Start requeues any interrupted cache.
func CheckIdle(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("진행 중인 작업 상태를 확인하지 못했습니다")
	}
	var active int
	err := db.QueryRowContext(ctx, `SELECT
	 (SELECT count(*) FROM download_queue WHERE state IN ('pending','running') OR cleanup_required=1)
	+(SELECT count(*) FROM local_import_job WHERE state IN ('scanning','pending','running') OR cleanup_required=1)
	+(SELECT count(*) FROM file_move_job WHERE state IN ('running','pausing'))
	+(SELECT count(*) FROM file_move_item WHERE state IN ('copying','verified','committed','cleanup_pending'))
	+(SELECT count(*) FROM maintenance_task WHERE state IN ('pending','running') AND coalesce(kind,'')<>'preview_generation')
	+(SELECT count(*) FROM media_edit_session WHERE state IN ('pending','preparing','stop_unconfirmed'))
	+(SELECT count(*) FROM media_edit_job WHERE state IN ('pending','running','verifying','publishing','stop_unconfirmed'))
	+(SELECT count(*) FROM media_edit_page WHERE state IN ('pending','running'))`).Scan(&active)
	if err != nil {
		return fmt.Errorf("진행 중인 작업 상태를 확인하지 못했습니다: %w", err)
	}
	if active > 0 {
		return fmt.Errorf("진행 중인 다운로드·가져오기·파일 이동·편집·정리 작업을 마친 뒤 업데이트를 적용하세요")
	}
	return nil
}
