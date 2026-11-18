package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type LocalMetadata struct {
	Kind     string   `json:"kind"`
	Format   string   `json:"format"`
	Width    *int     `json:"width"`
	Height   *int     `json:"height"`
	Duration *float64 `json:"duration"`
}
type LocalInspector struct{ FFprobe, FFmpeg, ImageMagick, Cache string }

func LocalKind(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".avif", ".tif", ".tiff", ".heic", ".heif":
		return "image"
	case ".mp4", ".m4v", ".mov", ".mkv", ".webm", ".avi", ".wmv":
		return "video"
	case ".mp3", ".m4a", ".aac", ".wav", ".flac", ".ogg", ".opus", ".wma":
		return "audio"
	}
	return ""
}
func imageCoder(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "JPEG"
	case ".png":
		return "PNG"
	case ".webp":
		return "WEBP"
	case ".gif":
		return "GIF"
	case ".bmp":
		return "BMP"
	case ".avif":
		return "AVIF"
	case ".tif", ".tiff":
		return "TIFF"
	case ".heic", ".heif":
		return "HEIC"
	}
	return ""
}
func imageArguments(work string) []string {
	return []string{"-regard-warnings", "-limit", "memory", "256MiB", "-limit", "map", "0", "-limit", "disk", "256MiB", "-limit", "thread", "2", "-limit", "time", "30", "-limit", "width", "20000", "-limit", "height", "20000", "-limit", "area", "40MP", "-define", "registry:temporary-path=" + work}
}
func (i LocalInspector) image(ctx context.Context, path string, extra []string) (string, error) {
	if !filepath.IsAbs(i.ImageMagick) || !filepath.IsAbs(i.Cache) {
		return "", fmt.Errorf("설정에서 ImageMagick을 설치하세요")
	}
	info, err := os.Stat(i.ImageMagick)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("설정에서 ImageMagick을 설치하세요")
	}
	work := filepath.Join(i.Cache, "image-work", uuid.NewString())
	if err = os.MkdirAll(work, 0700); err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	args := append(imageArguments(work), imageCoder(path)+":-[0]")
	args = append(args, extra...)
	result, err := new(ProcessRunner).Run(ctx, ProcessRequest{Executable: i.ImageMagick, Directory: work, Timeout: 45 * time.Second, Arguments: args, Input: f})
	if err != nil {
		return "", fmt.Errorf("이미지 디코딩 실패 또는 지원 코덱·처리 한도 초과: %w %s", err, result.Stderr)
	}
	return strings.TrimSpace(result.Stdout), nil
}
func (i LocalInspector) Inspect(ctx context.Context, path, kind string) (LocalMetadata, error) {
	metadata := LocalMetadata{Kind: kind}
	if kind == "image" {
		out, err := i.image(ctx, path, []string{"-format", "%m %w %h", "info:"})
		if err != nil {
			return metadata, err
		}
		fields := strings.Fields(out)
		if len(fields) != 3 {
			return metadata, fmt.Errorf("이미지 검사 응답이 올바르지 않습니다")
		}
		w, e1 := strconv.Atoi(fields[1])
		h, e2 := strconv.Atoi(fields[2])
		if e1 != nil || e2 != nil || w <= 0 || h <= 0 || int64(w)*int64(h) > 40_000_000 {
			return metadata, fmt.Errorf("이미지 크기가 처리 한도를 초과했습니다")
		}
		metadata.Format = strings.ToLower(fields[0])
		metadata.Width = &w
		metadata.Height = &h
		return metadata, nil
	}
	if kind != "video" && kind != "audio" {
		return metadata, fmt.Errorf("지원하지 않는 미디어입니다")
	}
	if !filepath.IsAbs(i.FFprobe) || !filepath.IsAbs(i.FFmpeg) {
		return metadata, fmt.Errorf("설정에서 FFmpeg와 FFprobe를 설치하세요")
	}
	demuxer := ""
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".m4v", ".mov", ".m4a":
		demuxer = "mov"
	case ".mkv", ".webm":
		demuxer = "matroska"
	case ".avi":
		demuxer = "avi"
	case ".wmv", ".wma":
		demuxer = "asf"
	case ".mp3":
		demuxer = "mp3"
	case ".aac":
		demuxer = "aac"
	case ".wav":
		demuxer = "wav"
	case ".flac":
		demuxer = "flac"
	case ".ogg", ".opus":
		demuxer = "ogg"
	}
	if demuxer == "" {
		return metadata, fmt.Errorf("지원하지 않는 파일 형식입니다")
	}
	input := []string{"-protocol_whitelist", "file,pipe", "-f", demuxer, "-i", path}
	args := []string{"-hide_banner", "-v", "error", "-show_entries", "format=format_name,duration:stream=codec_type,codec_name,width,height,duration:stream_disposition=attached_pic", "-of", "json"}
	args = append(args, input...)
	result, err := new(ProcessRunner).Run(ctx, ProcessRequest{Executable: i.FFprobe, Directory: filepath.Dir(path), Timeout: 30 * time.Second, Arguments: args})
	if err != nil {
		return metadata, fmt.Errorf("미디어 정보 검사 실패: %w %s", err, result.Stderr)
	}
	var probe struct {
		Streams []struct {
			CodecType     string `json:"codec_type"`
			Width, Height int
			Duration      string
			Disposition   struct {
				AttachedPic int `json:"attached_pic"`
			}
		}
		Format struct {
			FormatName string `json:"format_name"`
			Duration   string
		}
	}
	if err = json.Unmarshal([]byte(result.Stdout), &probe); err != nil {
		return metadata, err
	}
	duration, _ := strconv.ParseFloat(probe.Format.Duration, 64)
	found := false
	for _, stream := range probe.Streams {
		if stream.CodecType == kind && (kind != "video" || stream.Disposition.AttachedPic == 0) {
			found = true
			if stream.Width > 0 {
				w, h := stream.Width, stream.Height
				metadata.Width = &w
				metadata.Height = &h
			}
			if duration <= 0 {
				duration, _ = strconv.ParseFloat(stream.Duration, 64)
			}
			break
		}
	}
	if !found || duration < 0 || math.IsNaN(duration) || math.IsInf(duration, 0) || (kind == "video" && (metadata.Width == nil || metadata.Height == nil)) {
		return metadata, fmt.Errorf("요청한 종류의 정상 미디어 스트림이 없습니다")
	}
	metadata.Duration = &duration
	metadata.Format = probe.Format.FormatName
	stream := "0:V:0"
	if kind == "audio" {
		stream = "0:a:0"
	}
	args = []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-xerror"}
	args = append(args, input...)
	args = append(args, "-map", stream, "-t", "0.1", "-f", "null", "-")
	result, err = new(ProcessRunner).Run(ctx, ProcessRequest{Executable: i.FFmpeg, Directory: filepath.Dir(path), Timeout: 45 * time.Second, Arguments: args})
	if err != nil {
		return metadata, fmt.Errorf("미디어 디코딩 검증 실패: %w %s", err, result.Stderr)
	}
	return metadata, nil
}
func (i LocalInspector) ImagePreview(ctx context.Context, path, destination string) error {
	_, err := i.image(ctx, path, []string{"-auto-orient", "-thumbnail", "480x480>", "-background", "white", "-alpha", "remove", "-alpha", "off", "-colorspace", "sRGB", "-quality", "85", "JPEG:" + destination})
	return err
}
