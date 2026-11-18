package localimport_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/localimport"
	"github.com/Dev-Wonjoon/StorageBucket/internal/thumbnail"
)

func TestNativeImageVideoAudioImport(t *testing.T) {
	probe, magick := os.Getenv("STORAGEBUCKET_TEST_FFPROBE_EXE"), os.Getenv("STORAGEBUCKET_TEST_IMAGE_EXE")
	if !filepath.IsAbs(probe) || !filepath.IsAbs(magick) {
		t.Skip("실제 미디어 검사에는 FFprobe와 ImageMagick 절대 경로가 필요합니다")
	}
	ffmpeg := filepath.Join(filepath.Dir(probe), "ffmpeg.exe")
	f := newFixture(t)
	process := new(engine.ProcessRunner)
	for _, ext := range []string{"jpg", "png", "gif", "webp", "bmp", "tiff", "avif"} {
		path := filepath.Join(f.source, "한글 #이름[1]."+ext)
		result, err := process.Run(t.Context(), engine.ProcessRequest{Executable: magick, Directory: f.root, Timeout: 30 * time.Second, Arguments: []string{"-size", "16x12", "xc:red", path}})
		if err != nil {
			t.Fatalf("합성 %s: %s %v", ext, result.Stderr, err)
		}
	}
	if heic := os.Getenv("STORAGEBUCKET_TEST_HEIC_FILE"); heic != "" {
		data, err := os.ReadFile(heic)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != "7f8b363e4936c0666a25f64f3a92fda10bd8e5453be4592530b65a55dd98f3f2" {
			t.Fatal("HEIC 공식 예제 해시 불일치")
		}
		write(t, filepath.Join(f.source, "example.heic"), data)
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"video.mp4", []string{"-f", "lavfi", "-i", "color=c=blue:s=32x24:d=0.2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart"}},
		{"audio.mp3", []string{"-f", "lavfi", "-i", "sine=frequency=440:duration=0.2", "-c:a", "libmp3lame"}},
		{"audio.flac", []string{"-f", "lavfi", "-i", "sine=frequency=440:duration=0.2", "-c:a", "flac"}},
	} {
		args := append([]string{"-nostdin", "-hide_banner", "-loglevel", "error"}, tc.args...)
		args = append(args, filepath.Join(f.source, tc.name))
		result, err := process.Run(t.Context(), engine.ProcessRequest{Executable: ffmpeg, Directory: f.root, Timeout: 30 * time.Second, Arguments: args})
		if err != nil {
			t.Fatalf("미디어 생성: %v %s", err, result.Stderr)
		}
	}
	inspector := engine.LocalInspector{FFmpeg: ffmpeg, FFprobe: probe, ImageMagick: magick, Cache: filepath.Join(f.root, "cache")}
	var updates atomic.Int32
	thumbs := thumbnail.NewService(f.db, filepath.Join(f.root, "cache", "thumbnails"), ffmpeg, func(int64) { updates.Add(1) })
	thumbs.ConfigureImages(inspector)
	if err := thumbs.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer thumbs.Close()
	f.service.Close()
	f.service = localimport.NewService(f.db, inspector, func() (string, error) { return f.source, nil }, thumbs.ScheduleMedia)
	defer f.service.Close()
	p := run(t, f, scan(t, f))
	want := 10
	if os.Getenv("STORAGEBUCKET_TEST_HEIC_FILE") != "" {
		want++
	}
	if p.Job.Counts.Added != want || p.Job.Counts.FailedFolders != 0 {
		t.Fatalf("미디어 등록: %+v %+v", p.Job.Counts, p.Folders)
	}
	deadline := time.Now().Add(30 * time.Second)
	for updates.Load() < int32(want-2) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := count(t, f, "SELECT count(*) FROM media_file WHERE kind IN ('image','video') AND thumbnail_path IS NOT NULL"); n != int64(want-2) {
		rows, _ := f.db.Query("SELECT last_error FROM maintenance_task WHERE state='failed'")
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var err string
				rows.Scan(&err)
				t.Log(err)
			}
		}
		t.Fatalf("표시용 캐시 생성: %d/%d", n, want-2)
	}
	// Decode failure must not quietly register an extension-only candidate.
	write(t, filepath.Join(f.source, "broken.mp4"), []byte("not a video"))
	p = run(t, f, scan(t, f))
	if p.Job.Counts.FailedFolders != 1 || count(t, f, "SELECT count(*) FROM media_file") != int64(want) {
		t.Fatalf("손상 파일 검사: %+v", p.Job.Counts)
	}
}

func TestCancelKeepsCompletedFolders(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.source, "a", "first.png"), pngBytes(t, 8))
	write(t, filepath.Join(f.source, "b", "second.png"), pngBytes(t, 8))
	started := make(chan struct{})
	original := f.inspect
	f.inspect = func(ctx context.Context, path, kind string) (engine.LocalMetadata, error) {
		if strings.Contains(path, "second") {
			close(started)
			<-ctx.Done()
			return engine.LocalMetadata{}, ctx.Err()
		}
		return original(ctx, path, kind)
	}
	p := scan(t, f)
	ok(t, f.service.Run(t.Context(), localimport.Request{JobID: p.Job.ID}))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("두 번째 폴더 검사 대기 실패")
	}
	ok(t, f.service.Cancel(t.Context(), localimport.Request{JobID: p.Job.ID}))
	p = wait(t, f.service, p.Job.ID, "cancelled")
	if p.Job.Counts.CompletedFolders != 1 || p.Job.Counts.Added != 1 || count(t, f, "SELECT count(*) FROM media_file") != 1 {
		t.Fatal("취소가 완료된 폴더를 되돌렸습니다")
	}
}
