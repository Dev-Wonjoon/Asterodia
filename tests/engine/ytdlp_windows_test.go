package engine_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

func TestYtdlpDownloadSingle(t *testing.T) {
	t.Run("mp4", func(t *testing.T) {
		testYtdlpDownloadSingle(t, false)
	})
	t.Run("mp3", func(t *testing.T) {
		testYtdlpDownloadSingle(t, true)
	})
}

func testYtdlpDownloadSingle(t *testing.T, audio bool) {
	t.Helper()
	originalURL := os.Getenv("ASTERODIA_YTDLP_TEST_URL")
	if originalURL == "" {
		t.Skip("실제 다운로드를 검증하려면 ASTERODIA_YTDLP_TEST_URL을 지정하세요")
	}

	ytdlpPath := os.Getenv("ASTERODIA_TEST_YTDLP_EXE")
	ffprobePath := os.Getenv("ASTERODIA_TEST_FFPROBE_EXE")
	if !filepath.IsAbs(ytdlpPath) || !filepath.IsAbs(ffprobePath) {
		t.Fatal("yt-dlp.exe와 ffprobe.exe의 절대 경로를 지정하세요")
	}

	root := ytdlpTestRoot(t)

	stageDir, err := os.MkdirTemp(root, "stage-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("작업 폴더: %s", stageDir)

	ctx, cancel := context.WithTimeout(context.Background(), 13*time.Minute)
	defer cancel()

	ytdlp := engine.Ytdlp{Executable: ytdlpPath}
	if nodePath := os.Getenv("ASTERODIA_TEST_NODE_EXE"); nodePath != "" {
		if !filepath.IsAbs(nodePath) {
			t.Fatal("ASTERODIA_TEST_NODE_EXE에 node.exe의 절대 경로를 지정하세요")
		}
		ytdlp.JSRuntime = "node:" + nodePath
		ytdlp.FFmpegLocation = filepath.Dir(ffprobePath)
	}
	raw, err := ytdlp.Discover(ctx, originalURL, stageDir)
	if err != nil {
		t.Fatalf("조회 실패: %v", err)
	}
	targets, err := engine.ParseYtdlpTargets(raw, originalURL)
	if err != nil {
		t.Fatalf("대상 확정 실패: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("대상 수: %d, 기대값: 1", len(targets))
	}

	options := engine.Options{}
	if audio {
		options.Type = "audio"
		ytdlp.FFmpegLocation = filepath.Dir(ffprobePath)
	}
	path, err := ytdlp.DownloadSingle(ctx, targets[0], stageDir, options)

	if err != nil {
		t.Fatalf("저장 실패: %v", err)
	}

	var runner engine.ProcessRunner
	result, err := runner.Run(ctx, engine.ProcessRequest{
		Executable: ffprobePath,
		Directory:  stageDir,
		Timeout:    30 * time.Second,
		Arguments: []string{
			"-v", "error", "-show_streams",
			"-show_entries", "stream=codec_type", "-of", "json", "-i", path,
		},
	})
	if err != nil {
		t.Fatalf("ffprobe 실행 실패: %v", err)
	}

	var media struct {
		Streams []struct {
			Type string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &media); err != nil {
		t.Fatalf("ffprobe 결과 해석 실패: %v", err)
	}
	var hasVideo, hasAudio bool
	for _, stream := range media.Streams {
		hasVideo = hasVideo || stream.Type == "video"
		hasAudio = hasAudio || stream.Type == "audio"
	}
	wantVideo := !audio
	if !hasAudio || hasVideo != wantVideo {
		t.Fatalf("스트림 불일치: video=%t (기대 %t), audio=%t (기대 true)",
			hasVideo, wantVideo, hasAudio)
	}
	t.Logf("저장 및 스트림 확인 완료: %s", path)
}

func ytdlpTestRoot(t *testing.T) string {
	t.Helper()
	return engineTestRoot(t, "ytdlp-download")
}

func engineTestRoot(t *testing.T, categoryName string) string {
	t.Helper()

	configuredRoot := os.Getenv("ASTERODIA_TEST_ROOT")
	if !filepath.IsAbs(configuredRoot) {
		t.Fatal("ASTERODIA_TEST_ROOT에 절대 경로를 지정하세요")
	}
	root, err := filepath.EvalSymlinks(configuredRoot)
	if err != nil {
		t.Fatal(err)
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(source) {
		t.Fatal("테스트 소스 경로를 확인할 수 없습니다")
	}
	repo, err := filepath.EvalSymlinks(filepath.Join(filepath.Dir(source), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	category := filepath.Join(repo, ".test-data", categoryName)
	if !processPathInside(category, root) || strings.EqualFold(category, root) {
		t.Fatalf("테스트 루트는 .test-data/%s/ 아래의 실행별 폴더여야 합니다", categoryName)
	}
	return root
}
