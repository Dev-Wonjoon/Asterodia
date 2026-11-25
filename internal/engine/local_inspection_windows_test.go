package engine

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalInspectionThreadLimitWithOfficialEngines(t *testing.T) {
	root := os.Getenv("STORAGEBUCKET_SECURITY_ENGINE_ROOT")
	if root == "" {
		t.Skip("verified official artifacts not configured")
	}
	bin := t.TempDir()
	archive := filepath.Join(root, "ffmpeg.zip")
	if err := verifyEngineDigest(archive, enginePins["ffmpeg"].ArchiveSHA); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		directory := filepath.Join(bin, name)
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, name+".exe")
		if err := extractEngineBinary(archive, path, name+".exe"); err != nil {
			t.Fatal(err)
		}
		if err := verifyEngineDigest(path, enginePins[name].BinarySHA); err != nil {
			t.Fatal(err)
		}
	}
	magick := filepath.Join(bin, "image")
	if err := extractImageEngine(t.Context(), filepath.Join(root, "imagemagick.7z"), magick, enginePins["imagemagick"].ArchiveSHA); err != nil {
		t.Fatal(err)
	}
	magick = filepath.Join(magick, "magick.exe")
	if err := verifyEngineDigest(magick, enginePins["imagemagick"].BinarySHA); err != nil {
		t.Fatal(err)
	}
	inspector := LocalInspector{FFmpeg: filepath.Join(bin, "ffmpeg", "ffmpeg.exe"), FFprobe: filepath.Join(bin, "ffprobe", "ffprobe.exe"), ImageMagick: magick, Cache: filepath.Join(bin, "cache"), InspectionThreads: 1}
	picture := filepath.Join(bin, "picture.png")
	f, err := os.Create(picture)
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 16, 12)))
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind, name, source string
		codec              []string
	}{
		{kind: "video", name: "video.mp4", source: "color=c=blue:s=32x24:d=0.2", codec: []string{"-c:v", "libx264", "-pix_fmt", "yuv420p"}},
		{kind: "audio", name: "audio.mp3", source: "sine=frequency=440:duration=0.2", codec: []string{"-c:a", "libmp3lame"}},
	} {
		path := filepath.Join(bin, tc.name)
		args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-filter_threads", "1", "-f", "lavfi", "-i", tc.source}
		args = append(args, tc.codec...)
		args = append(args, "-threads", "1", path)
		result, err := new(ProcessRunner).Run(t.Context(), ProcessRequest{Executable: inspector.FFmpeg, Directory: bin, Timeout: 30 * time.Second, Arguments: args})
		if err != nil {
			t.Fatalf("generate %s: %v %s", tc.kind, err, result.Stderr)
		}
		metadata, err := inspector.Inspect(t.Context(), path, tc.kind)
		if err != nil || metadata.Kind != tc.kind || metadata.Duration == nil || *metadata.Duration <= 0 {
			t.Fatalf("inspect %s with one thread: %+v %v", tc.kind, metadata, err)
		}
	}
	metadata, err := inspector.Inspect(t.Context(), picture, "image")
	if err != nil || metadata.Width == nil || *metadata.Width != 16 || metadata.Height == nil || *metadata.Height != 12 {
		t.Fatalf("inspect image with one thread: %+v %v", metadata, err)
	}
}
