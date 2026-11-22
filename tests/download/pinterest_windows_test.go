package download_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

type pinterestFixture struct {
	discovery engine.GalleryDLDiscovery
	save      func(engine.GalleryDLDiscovery, string) ([]string, error)
}

func TestRunnerPinterestLive(t *testing.T) {
	page := os.Getenv("STORAGEBUCKET_PINTEREST_LIVE_URL")
	if page == "" {
		t.Skip("explicit public Pinterest fixture URL required")
	}
	root := downloadTestRoot(t)
	ytdlp, gallery, ffmpeg, ffprobe := os.Getenv("STORAGEBUCKET_TEST_YTDLP_EXE"), os.Getenv("STORAGEBUCKET_TEST_GALLERYDL_EXE"), os.Getenv("STORAGEBUCKET_TEST_FFMPEG_EXE"), os.Getenv("STORAGEBUCKET_TEST_FFPROBE_EXE")
	for _, path := range []string{ytdlp, gallery, ffmpeg, ffprobe} {
		if !filepath.IsAbs(path) {
			t.Fatal("absolute engine path required", path)
		}
	}
	db := openStoreTestDB(t)
	store := download.NewStore(db)
	job, err := store.CreateInitialAttempt(t.Context(), download.InitialAttemptInput{OriginalURL: page, StorageRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	runner := download.Runner{Storage: download.Storage{Root: root}, Ytdlp: &engine.Ytdlp{Executable: ytdlp, FFmpegLocation: ffmpeg, NoPlaylist: true}, GalleryDL: &engine.GalleryDL{Executable: gallery}}
	attempt, err := runner.DiscoverAttempt(t.Context(), job.JobID, job.AttemptID, page, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	files, err := runner.FreezeAndStageAttempt(t.Context(), store, attempt)
	if err != nil || len(files) == 0 {
		t.Fatal(files, err)
	}
	videoCount := 0
	for _, file := range files {
		if filepath.Ext(file.Path) != ".mp4" {
			continue
		}
		videoCount++
		output, err := exec.CommandContext(t.Context(), ffprobe, "-v", "error", "-show_entries", "stream=codec_type:format=duration", "-of", "json", file.Path).Output()
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Streams []struct {
				Kind string `json:"codec_type"`
			} `json:"streams"`
			Format struct {
				Duration string `json:"duration"`
			} `json:"format"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatal(err)
		}
		hasVideo := false
		for _, stream := range result.Streams {
			hasVideo = hasVideo || stream.Kind == "video"
		}
		if !hasVideo || result.Format.Duration == "" {
			t.Fatal("invalid video", string(output))
		}
		t.Logf("Pinterest video verified: %s duration=%ss", file.Path, result.Format.Duration)
	}
	if videoCount == 0 {
		t.Fatal("pin contained no downloaded video")
	}
}

func (f pinterestFixture) Discover(context.Context, string, string) (engine.GalleryDLDiscovery, error) {
	return f.discovery, nil
}
func (f pinterestFixture) Download(_ context.Context, _ string, d engine.GalleryDLDiscovery, dir string) ([]string, error) {
	return f.save(d, dir)
}

func TestRunnerPinterestMixed(t *testing.T) {
	root := downloadTestRoot(t)
	for _, mode := range []string{"mixed", "second-video-invalid", "freeze-fails", "missing-video-engine", "stream-unsupported"} {
		t.Run(mode, func(t *testing.T) {
			db := openStoreTestDB(t)
			store := download.NewStore(db)
			page := "https://pin.it/fixture-" + mode
			job, err := store.CreateInitialAttempt(t.Context(), download.InitialAttemptInput{OriginalURL: page, StorageRoot: root})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "freeze-fails" {
				execStoreTestSQL(t, db, `CREATE TRIGGER reject_pinterest BEFORE INSERT ON download_target WHEN NEW.position=1 BEGIN SELECT RAISE(ABORT,'pinterest fixture'); END`)
			}
			base := map[string]any{"category": "pinterest", "subcategory": "pin", "id": "664281013778109217", "title": "핀", "count": 4}
			messages := [][]any{{2, base}}
			for i, ext := range []string{"png", "mp4", "mp4", "png"} {
				data := map[string]any{"category": "pinterest", "id": "664281013778109217", "filename": fmt.Sprint(i), "extension": ext, "num": i + 1}
				address := fmt.Sprintf("https://i.pinimg.com/photo%d.png", i)
				if ext == "mp4" {
					address = fmt.Sprintf("ytdl:https://v.pinimg.com/video%d.m3u8?signature=one%%2Btwo", i)
					data["_ytdl_manifest"] = "hls"
				}
				messages = append(messages, []any{3, address, data})
			}
			raw, _ := json.Marshal(messages)
			discovery, err := engine.ParseGalleryDLDiscovery(raw)
			if err != nil {
				t.Fatal(err)
			}
			stageCalls := 0
			checkFrozen := func() error {
				var count int
				err := db.QueryRow("SELECT count(*) FROM download_target WHERE attempt_id=?", job.AttemptID).Scan(&count)
				if err != nil || count != 4 {
					return fmt.Errorf("partial freeze: %d %v", count, err)
				}
				stageCalls++
				return nil
			}
			runner := download.Runner{Storage: download.Storage{Root: root}, GalleryDL: pinterestFixture{discovery, func(d engine.GalleryDLDiscovery, dir string) ([]string, error) {
				if err := checkFrozen(); err != nil {
					return nil, err
				}
				if len(d.Files) != 2 {
					return nil, fmt.Errorf("stream leaked to gallery engine")
				}
				paths := []string{}
				for i, position := range []int{0, 3} {
					item := filepath.Join(dir, fmt.Sprintf("item-%04d", i+1))
					if err := os.Mkdir(item, 0700); err != nil {
						return nil, err
					}
					path := filepath.Join(item, "media.png")
					if err := os.WriteFile(path, []byte(fmt.Sprint(position)), 0600); err != nil {
						return nil, err
					}
					paths = append(paths, path)
				}
				return paths, nil
			}}}
			if mode != "missing-video-engine" {
				runner.Ytdlp = stagingYtdlp{ytdlpDiscoverFunc: func(_ context.Context, address, _ string) (json.RawMessage, error) {
					if mode == "stream-unsupported" {
						if address == page {
							t.Error("mixed pin was retried as a single video")
						}
						return nil, engine.ErrUnsupportedURL
					}
					id := "1"
					if address == "https://v.pinimg.com/video2.m3u8?signature=one%2Btwo" {
						id = "2"
					} else if address != "https://v.pinimg.com/video1.m3u8?signature=one%2Btwo" {
						return nil, fmt.Errorf("signed stream changed: %s", address)
					}
					data := map[string]any{"id": id, "extractor_key": "Generic", "webpage_url": address, "format_id": "hls", "url": address, "protocol": "m3u8_native", "ext": "mp4", "vcodec": "h264", "acodec": "aac"}
					if mode == "second-video-invalid" && id == "2" {
						delete(data, "format_id")
					}
					raw, _ := json.Marshal(data)
					return raw, nil
				}, save: func(target engine.YtdlpTarget, dir string) (string, error) {
					if err := checkFrozen(); err != nil {
						return "", err
					}
					path := filepath.Join(dir, "media.mp4")
					return path, os.WriteFile(path, []byte(target.ID), 0600)
				}}
			}
			attempt, err := runner.DiscoverAttempt(t.Context(), job.JobID, job.AttemptID, page, engine.Options{})
			if err == nil {
				var files []download.StagedFile
				files, err = runner.FreezeAndStageAttempt(t.Context(), store, attempt)
				if mode == "mixed" {
					if err != nil || len(files) != 4 || stageCalls != 3 {
						t.Fatal(files, stageCalls, err)
					}
					for i, file := range files {
						data, e := os.ReadFile(file.Path)
						if e != nil || string(data) != fmt.Sprint(i) {
							t.Fatal(i, string(data), e)
						}
						if filepath.Base(filepath.Dir(file.Path)) != fmt.Sprintf("item-%04d", i+1) {
							t.Fatal(file.Path)
						}
					}
					return
				}
			}
			if err == nil || stageCalls != 0 {
				t.Fatal("failed preflight wrote files", stageCalls, err)
			}
			var count int
			if err := db.QueryRow("SELECT count(*) FROM download_target WHERE attempt_id=?", job.AttemptID).Scan(&count); err != nil || count != 0 {
				t.Fatal("partial targets", count, err)
			}
		})
	}
}
