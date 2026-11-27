package download

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

func (r Runner) resolvePinterestVideos(ctx context.Context, discovery engine.GalleryDLDiscovery, originalURL, dir string, options engine.Options) (map[int]engine.YtdlpTarget, error) {
	requests := discovery.PinterestVideoURLs()
	if len(requests) == 0 {
		return nil, nil
	}
	if _, err := discovery.Targets(originalURL); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedTargetDiscovery, err)
	}
	if r.Ytdlp == nil {
		return nil, fmt.Errorf("Pinterest 동영상 다운로드에는 yt-dlp 엔진이 필요합니다")
	}
	resolved := make(map[int]engine.YtdlpTarget, len(requests))
	// Keep extraction order deterministic, including in diagnostics and tests.
	for index := range discovery.Files {
		address, ok := requests[index]
		if !ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := r.Ytdlp.Discover(ctx, address, dir)
		if err != nil {
			return nil, fmt.Errorf("Pinterest %d번째 동영상 조회 실패: %w", index+1, err)
		}
		targets, err := engine.ParseYtdlpTargets(raw, address)
		if err != nil {
			return nil, err
		}
		if len(targets) != 1 {
			return nil, fmt.Errorf("Pinterest 동영상 한 파일의 스트림이 필요합니다")
		}
		if err = targets[0].ValidateDirectDownload(options); err != nil {
			return nil, err
		}
		resolved[index] = targets[0]
	}
	return resolved, nil
}

func (r Runner) stagePinterest(ctx context.Context, attempt AttemptDiscovery) ([]string, error) {
	requests := attempt.GalleryDLDiscovery.PinterestVideoURLs()
	if r.Ytdlp == nil || len(requests) != len(attempt.galleryVideos) {
		return nil, fmt.Errorf("Pinterest 영상 확정 정보가 일치하지 않습니다")
	}
	paths := make([]string, len(attempt.GalleryDLDiscovery.Files))
	direct := engine.GalleryDLDiscovery{Directories: attempt.GalleryDLDiscovery.Directories}
	var positions []int
	for i, file := range attempt.GalleryDLDiscovery.Files {
		if _, video := requests[i]; video {
			target, ok := attempt.galleryVideos[i]
			if !ok {
				return nil, fmt.Errorf("Pinterest 영상 확정 정보가 누락되었습니다")
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			dir, err := prepareStageItem(attempt.stageDir, i)
			if err != nil {
				return nil, err
			}
			paths[i], err = r.Ytdlp.DownloadSingle(ctx, target, dir, attempt.Options)
			if err != nil {
				return nil, err
			}
		} else {
			direct.Files = append(direct.Files, file)
			positions = append(positions, i)
		}
	}
	if len(positions) == 0 {
		return paths, nil
	}
	dir := filepath.Join(attempt.stageDir, "pinterest-direct")
	if err := os.Mkdir(dir, 0700); err != nil {
		return nil, err
	}
	saved, err := r.GalleryDL.Download(ctx, attempt.OriginalURL, direct, dir)
	if err != nil {
		return nil, err
	}
	if len(saved) != len(positions) {
		return nil, fmt.Errorf("Pinterest 사진·영상의 저장 결과 수가 다릅니다")
	}
	for i, source := range saved {
		if err := verifyStagedFile(dir, i, source); err != nil {
			return nil, err
		}
		destination, err := prepareStageItem(attempt.stageDir, positions[i])
		if err != nil {
			return nil, err
		}
		paths[positions[i]] = filepath.Join(destination, filepath.Base(source))
		if err = os.Rename(source, paths[positions[i]]); err != nil {
			return nil, err
		}
		if err = os.Remove(filepath.Dir(source)); err != nil {
			return nil, err
		}
	}
	if err = os.Remove(dir); err != nil {
		return nil, err
	}
	return paths, nil
}
