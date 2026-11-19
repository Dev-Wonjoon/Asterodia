package engine

import (
	"context"
	"errors"
	"io"
	"time"
)

type Kind string

const (
	KindYtdlp     Kind = "yt-dlp"
	KindGalleryDL Kind = "gallery-dl"
)

type Options struct {
	Type      string `json:"type,omitempty"`
	Quality   string `json:"quality,omitempty"`
	Extension string `json:"extension,omitempty"`
	Playlist  *bool  `json:"playlist,omitempty"`
}

type BinaryStatus struct {
	Path    string `json:"path"`
	Present bool   `json:"present"`
}

type Status struct {
	Installed bool    `json:"installed"`
	Version   *string `json:"version"`
	Error     string  `json:"error,omitempty"`
}

type InstallProgress struct {
	InstallID  string `json:"installId"`
	Engine     string `json:"engine"`
	Phase      string `json:"phase"`
	Downloaded int64  `json:"downloaded"`
	Total      *int64 `json:"total"`
	Error      string `json:"error,omitempty"`
}

var ErrStopUnconfirmed = errors.New(
	"엔진 프로세스 종료를 확인하지 못했습니다",
)

var ErrOutputLimit = errors.New("엔진 출력 크기 제한을 초과했습니다")

type ProcessRequest struct {
	Executable string
	Arguments  []string
	Directory  string
	Timeout    time.Duration
	Stdin      string
	Input      io.Reader

	// 진행률 해석 등에 사용한다. 콜백은 빠르게 반환해야 한다.
	OnStdoutLine func(string) error
	OnStderrLine func(string) error
	// Streaming diagnostics keep only a bounded tail in ProcessResult.
	StreamOutput bool
}

type Output struct {
	Engine string
	Stream string
	Text   string
}
type outputKey struct{}

// WithOutputObserver attaches a fast diagnostic sink to this operation and
// child engine calls without coupling process execution to a database.
func WithOutputObserver(ctx context.Context, observer func(Output)) context.Context {
	return context.WithValue(ctx, outputKey{}, observer)
}

type ProcessResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}
