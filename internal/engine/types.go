package engine

import (
	"errors"
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

	// 진행률 해석 등에 사용한다. 콜백은 빠르게 반환해야 한다.
	OnStdoutLine func(string) error
}

type ProcessResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}
