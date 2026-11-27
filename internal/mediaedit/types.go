package mediaedit

import "github.com/Dev-Wonjoon/Asterodia/internal/engine"

type PrepareRequest struct {
	QuickVideo     bool    `json:"quickVideo,omitempty"`
	OriginalFrames bool    `json:"originalFrames,omitempty"`
	FullSource     bool    `json:"fullSource,omitempty"`
	NativeVideo    bool    `json:"nativeVideo,omitempty"`
	NativeGIF      bool    `json:"nativeGIF,omitempty"`
	NativeWebP     bool    `json:"nativeWebP,omitempty"`
	RequestID      string  `json:"requestId"`
	FileID         int64   `json:"fileId"`
	Start          float64 `json:"start"`
	End            float64 `json:"end"`
	FPS            int     `json:"fps"`
	Width          int     `json:"width"`
	Kind           string  `json:"kind"`
}
type CreateRequest struct {
	OutputDirectory string                  `json:"outputDirectory,omitempty"`
	AlbumIDs        []int64                 `json:"albumIds,omitempty"`
	TagNames        []string                `json:"tagNames,omitempty"`
	TrimStart       *float64                `json:"trimStart,omitempty"`
	TrimEnd         *float64                `json:"trimEnd,omitempty"`
	RangeStart      int                     `json:"rangeStart"`
	RangeEnd        int                     `json:"rangeEnd"`
	RequestID       string                  `json:"requestId"`
	SessionID       string                  `json:"sessionId"`
	Kind            string                  `json:"kind"`
	Crop            engine.EditCrop         `json:"crop"`
	Deleted         []int                   `json:"deleted"`
	Mute            bool                    `json:"mute"`
	Quality         engine.AnimationQuality `json:"quality"`
	SelectionID     string                  `json:"selectionId"`
	Preview         bool                    `json:"preview"`
	PreviewStart    int                     `json:"previewStart"`
}
type Request struct {
	ID string `json:"id"`
}
type Session struct {
	ID            string         `json:"id"`
	FileID        *int64         `json:"fileId"`
	Title         string         `json:"title"`
	Options       PrepareRequest `json:"options"`
	State         string         `json:"state"`
	Width         int            `json:"width"`
	Height        int            `json:"height"`
	FrameCount    int            `json:"frameCount"`
	Progress      int            `json:"progress"`
	Error         string         `json:"error"`
	Cancelling    bool           `json:"cancelling"`
	CreatedAt     int64          `json:"createdAt"`
	RenderVersion int            `json:"renderVersion"`
}
type Job struct {
	OutputNotice string        `json:"outputNotice,omitempty"`
	ID           string        `json:"id"`
	SessionID    string        `json:"sessionId"`
	Options      CreateRequest `json:"options"`
	State        string        `json:"state"`
	Progress     int           `json:"progress"`
	ResultFileID *int64        `json:"resultFileId"`
	Error        string        `json:"error"`
	Cancelling   bool          `json:"cancelling"`
	CreatedAt    int64         `json:"createdAt"`
	Preview      bool          `json:"preview"`
	Resources    Usage         `json:"resources"`
}

type FramesRequest struct {
	Retry  bool   `json:"retry"`
	ID     string `json:"id"`
	Offset int    `json:"offset"`
}
type FramePage struct {
	Times     []float64 `json:"times,omitempty"`
	Durations []float64 `json:"durations,omitempty"`
	Offset    int       `json:"offset"`
	Count     int       `json:"count"`
	State     string    `json:"state"`
	Error     string    `json:"error"`
}
type SelectionRequest struct {
	ID        string              `json:"id"`
	SessionID string              `json:"sessionId"`
	Offset    int                 `json:"offset"`
	Ranges    []engine.FrameRange `json:"ranges"`
	Final     bool                `json:"final"`
}
