package engine_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

func TestYtdlpDownloadPlaylist(t *testing.T) {
	originalURL := os.Getenv("STORAGEBUCKET_YTDLP_TEST_PLAYLIST_URL")
	if originalURL == "" {
		t.Skip("재생목록 테스트 URL을 지정하세요")
	}

	wantIDs := strings.Split(os.Getenv("STORAGEBUCKET_YTDLP_TEST_PLAYLIST_IDS"), ",")
	if len(wantIDs) != 2 {
		t.Fatal("재생목록에 있는 영상 ID 두 개를 쉼표로 구분해 지정하세요")
	}
	for i := range wantIDs {
		wantIDs[i] = strings.TrimSpace(wantIDs[i])
		if len(wantIDs[i]) != 11 {
			t.Fatalf("%d번째 영상 ID가 올바르지 않습니다", i+1)
		}
	}

	ytdlpPath := os.Getenv("STORAGEBUCKET_TEST_YTDLP_EXE")
	nodePath := os.Getenv("STORAGEBUCKET_TEST_NODE_EXE")
	ffprobePath := os.Getenv("STORAGEBUCKET_TEST_FFPROBE_EXE")
	if !filepath.IsAbs(ytdlpPath) || !filepath.IsAbs(nodePath) || !filepath.IsAbs(ffprobePath) {
		t.Fatal("yt-dlp.exe, node.exe, ffprobe.exe의 절대 경로를 지정하세요")
	}

	root := ytdlpTestRoot(t)
	playlistDir, err := os.MkdirTemp(root, "playlist-")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 24*time.Minute)
	defer cancel()

	ytdlp := engine.Ytdlp{
		Executable:     ytdlpPath,
		JSRuntime:      "node:" + nodePath,
		FFmpegLocation: filepath.Dir(ffprobePath),
	}
	raw, err := ytdlp.Discover(ctx, originalURL, playlistDir)
	if err != nil {
		t.Fatalf("재생목록 조회 실패: %v", err)
	}
	targets, err := engine.ParseYtdlpTargets(raw, originalURL)
	if err != nil {
		t.Fatalf("재생목록 대상 확정 실패: %v", err)
	}
	if len(targets) != len(wantIDs) {
		t.Fatalf("대상 수: %d, 기대값: %d", len(targets), len(wantIDs))
	}

	for i, target := range targets {
		wantKey := "youtube:" + wantIDs[i]
		if target.TargetKey != wantKey {
			t.Fatalf("%d번째 대상 키: %q, 기대값: %q", i+1, target.TargetKey, wantKey)
		}
	}

	var runner engine.ProcessRunner
	for _, target := range targets {
		stageDir, err := os.MkdirTemp(playlistDir, "item-")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s 다운로드 시작 (480p 이하): %s", target.TargetKey, stageDir)
		path, err := ytdlp.DownloadSingle(ctx, target, stageDir, engine.Options{Quality: "480"})
		if err != nil {
			t.Fatalf("%s 저장 실패: %v", target.TargetKey, err)
		}

		result, err := runner.Run(ctx, engine.ProcessRequest{
			Executable: ffprobePath,
			Directory:  stageDir,
			Timeout:    30 * time.Second,
			Arguments: []string{
				"-v", "error", "-show_entries", "stream=codec_type",
				"-of", "json", "-i", path,
			},
		})
		if err != nil {
			t.Fatalf("%s ffprobe 실행 실패: %v\n%s", target.TargetKey, err, result.Stderr)
		}

		var media struct {
			Streams []struct {
				Type string `json:"codec_type"`
			} `json:"streams"`
		}
		if err := json.Unmarshal([]byte(result.Stdout), &media); err != nil {
			t.Fatalf("%s 스트림 결과 해석 실패: %v", target.TargetKey, err)
		}
		var hasVideo, hasAudio bool
		for _, stream := range media.Streams {
			hasVideo = hasVideo || stream.Type == "video"
			hasAudio = hasAudio || stream.Type == "audio"
		}
		if !hasVideo || !hasAudio {
			t.Fatalf("%s 필수 스트림 누락: video=%t audio=%t",
				target.TargetKey, hasVideo, hasAudio)
		}
		t.Logf("%s 저장 및 스트림 확인: %s", target.TargetKey, path)
	}
}
