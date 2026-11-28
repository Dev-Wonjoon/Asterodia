package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// VerifyMedia decodes a frame/sample with the expected demuxer. Restrict protocols
// so a downloaded file cannot turn validation into a remote playlist request.
func VerifyMedia(ctx context.Context, ffmpeg, path, kind string) error {
	demuxer := ""
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".mov", ".m4a":
		demuxer = "mov"
	case ".mkv", ".webm":
		demuxer = "matroska"
	case ".mp3":
		demuxer = "mp3"
	case ".ogg", ".opus":
		demuxer = "ogg"
	case ".flac":
		demuxer = "flac"
	case ".wav":
		demuxer = "wav"
	default:
		return fmt.Errorf("검증을 지원하지 않는 미디어 형식입니다")
	}
	stream := "0:v:0"
	if kind == "audio" {
		stream = "0:a:0"
	}
	_, err := new(ProcessRunner).Run(ctx, ProcessRequest{Executable: ffmpeg, Directory: filepath.Dir(path), Timeout: time.Minute, Arguments: []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-xerror", "-protocol_whitelist", "file,pipe", "-f", demuxer, "-i", path, "-map", stream, "-t", "0.1", "-f", "null", "-"}})
	return err
}
