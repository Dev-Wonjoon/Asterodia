package engine_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

// Opt-in public URL diagnosis. Normal regression runs use local HTTP fixtures.
func TestYtdlpLiveShorts(t *testing.T) {
	url := os.Getenv("STORAGEBUCKET_LIVE_SHORTS_URL")
	if url == "" {
		t.Skip("공개 Shorts 재현 URL을 지정해야 합니다")
	}
	root := ytdlpTestRoot(t)
	stage, err := os.MkdirTemp(root, "live-shorts-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage)
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	executable := os.Getenv("STORAGEBUCKET_TEST_YTDLP_EXE")
	ffmpeg := os.Getenv("STORAGEBUCKET_TEST_FFMPEG_EXE")
	if !filepath.IsAbs(executable) || !filepath.IsAbs(ffmpeg) {
		t.Fatal("격리된 실제 엔진 경로가 필요합니다")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	var mu sync.Mutex
	var lines []string
	ctx = engine.WithOutputObserver(ctx, func(output engine.Output) {
		if output.Stream == "stdout" && (json.Valid([]byte(output.Text)) || strings.HasPrefix(output.Text, "__STORAGEBUCKET_RESULT__:")) {
			return
		}
		text := output.Text
		if len(text) > 1500 {
			text = text[:1500] + "…"
		}
		mu.Lock()
		lines = append(lines, "["+output.Engine+"/"+output.Stream+"] "+text)
		mu.Unlock()
	})
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, line := range lines {
			t.Log(line)
		}
	}()
	y := engine.Ytdlp{Executable: executable, FFmpegLocation: ffmpeg, JSRuntime: "node:" + node, NoPlaylist: true}
	started := time.Now()
	raw, err := y.Discover(ctx, url, stage)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := engine.ParseYtdlpTargets(raw, url)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("대상 수: %d", len(targets))
	}
	discovered := time.Now()
	file, err := y.DownloadSingle(ctx, targets[0], stage, engine.Options{Quality: "best"})
	if err != nil {
		t.Fatal(err)
	}
	downloaded := time.Now()
	if err = engine.VerifyMedia(ctx, ffmpeg, file, "video"); err != nil {
		t.Fatal(err)
	}
	t.Logf("소요 시간: 조회 %.3f초 · 다운로드/병합 %.3f초 · 검증 %.3f초 · 전체 %.3f초", discovered.Sub(started).Seconds(), downloaded.Sub(discovered).Seconds(), time.Since(downloaded).Seconds(), time.Since(started).Seconds())
	t.Log("공개 Shorts 조회·다운로드·미디어 검증 성공")
}
