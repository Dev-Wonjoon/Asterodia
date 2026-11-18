package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

type YtdlpEngine interface {
	Discover(context.Context, string, string) (json.RawMessage, error)
	DownloadSingle(context.Context, engine.YtdlpTarget, string, engine.Options) (string, error)
}

type GalleryDLDiscoverer interface {
	Discover(context.Context, string, string) (engine.GalleryDLDiscovery, error)
}

type Runner struct {
	Storage   Storage
	Ytdlp     YtdlpEngine
	GalleryDL GalleryDLDiscoverer
}

type AttemptDiscovery struct {
	Engine        engine.Kind
	OriginalURL   string
	Options       engine.Options
	stageDir      string
	jobID         string
	attemptID     string
	YtdlpTargets  []engine.YtdlpTarget
	GalleryResult engine.GalleryDLDiscovery
}

func (r Runner) DiscoverAttempt(
	ctx context.Context,
	jobID, attemptID, originalURL string,
	options engine.Options,
) (AttemptDiscovery, error) {
	if err := ctx.Err(); err != nil {
		return AttemptDiscovery{}, err
	}
	kind, err := engine.Select(originalURL, options)
	if err != nil {
		return AttemptDiscovery{}, err
	}
	if (kind == engine.KindYtdlp && r.Ytdlp == nil) ||
		(kind == engine.KindGalleryDL && r.GalleryDL == nil) {
		return AttemptDiscovery{}, fmt.Errorf("%s 조회 엔진이 연결되지 않았습니다", kind)
	}

	stageDir, err := r.Storage.PrepareStage(jobID, attemptID)
	if err != nil {
		return AttemptDiscovery{}, err
	}
	result := AttemptDiscovery{
		Engine:      kind,
		OriginalURL: originalURL,
		Options:     options,
		stageDir:    stageDir,
		jobID:       jobID,
		attemptID:   attemptID,
	}

	switch kind {
	case engine.KindYtdlp:
		raw, err := r.Ytdlp.Discover(ctx, originalURL, stageDir)
		if err != nil {
			return result, fmt.Errorf("yt-dlp 조회 실패: %w", err)
		}
		targets, err := engine.ParseYtdlpTargets(raw, originalURL)
		if err != nil {
			return result, fmt.Errorf("yt-dlp 대상 확정 실패: %w", err)
		}
		result.YtdlpTargets = targets

	case engine.KindGalleryDL:
		discovery, err := r.GalleryDL.Discover(ctx, originalURL, stageDir)
		if err != nil {
			return result, fmt.Errorf("gallery-dl 조회 실패: %w", err)
		}
		result.GalleryResult = discovery
	}

	return result, nil
}

func (a AttemptDiscovery) StageDir() string {
	return a.stageDir
}

var ErrUnsupportedTargetDiscovery = errors.New("다운로드 대상 식별을 지원하지 않습니다")

func (r Runner) FreezeAndStageAttempt(
	ctx context.Context,
	store *Store,
	attempt AttemptDiscovery,
) ([]StagedFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("다운로드 Store가 연결되지 않았습니다")
	}
	if !filepath.IsAbs(attempt.stageDir) || attempt.jobID == "" || attempt.attemptID == "" {
		return nil, errors.New("작업과 시도에 연결된 조회 결과가 필요합니다")
	}

	targets, err := attempt.targetInputs()
	if err != nil {
		return nil, err
	}
	if err := store.FreezeTargets(ctx, attempt.jobID, attempt.attemptID, targets); err != nil {
		return nil, fmt.Errorf("다운로드 대상 확정 실패: %w", err)
	}

	files, err := r.StageAttempt(ctx, attempt)
	if err != nil {
		return nil, err
	}

	results := make([]StagedResult, len(files))
	for i, file := range files {
		if file.Position < 0 || file.Position >= len(targets) {
			return nil, fmt.Errorf("확정 대상 범위를 벗어난 저장 결과 순번: %d", file.Position)
		}
		results[i] = StagedResult{
			Position:  file.Position,
			TargetKey: targets[file.Position].TargetKey,
			Path:      file.Path,
		}
	}
	if err := store.RecordStagedResults(ctx, attempt.jobID, attempt.attemptID, results); err != nil {
		return nil, fmt.Errorf("작업 파일 저장 결과 기록 실패: %w", err)
	}
	return files, nil
}

func (a AttemptDiscovery) targetInputs() ([]TargetInput, error) {
	switch a.Engine {
	case engine.KindYtdlp:
		kind := "video"
		if a.Options.Type == "audio" || a.Options.Quality == "audio" {
			kind = "audio"
		}

		targets := make([]TargetInput, 0, len(a.YtdlpTargets))
		for _, target := range a.YtdlpTargets {
			// Generic can delegate HTML pages to an embedded-media extractor.
			// Validate the entire resolved list before freezing or saving any entry.
			if err := target.ValidateDirectDownload(a.Options); err != nil {
				return nil, err
			}
			targets = append(targets, TargetInput{
				TargetKey: target.TargetKey,
				SourceURL: target.WebpageURL,
				Metadata: TargetMetadata{
					Kind:         kind,
					Title:        target.Title,
					ExternalID:   target.ID,
					PlatformName: target.ExtractorKey,
					OwnerID:      target.OwnerID,
					OwnerName:    target.OwnerName,
					Duration:     target.Duration,
				},
			})
		}
		return targets, nil

	case engine.KindGalleryDL:
		media, err := a.GalleryResult.InstagramTargets(a.OriginalURL)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnsupportedTargetDiscovery, err)
		}

		targets := make([]TargetInput, 0, len(media))
		for _, item := range media {
			targets = append(targets, TargetInput{TargetKey: item.TargetKey, SourceURL: a.OriginalURL, Metadata: TargetMetadata{Kind: item.Kind, Title: item.Title, ExternalID: item.MediaID, PlatformName: "Instagram", OwnerID: item.OwnerID, OwnerName: item.OwnerName}})
		}
		return targets, nil

	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedTargetDiscovery, a.Engine)
	}
}

type StagedFile struct {
	Position int
	Path     string
}

func (r Runner) StageAttempt(ctx context.Context, attempt AttemptDiscovery) ([]StagedFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(attempt.stageDir) {
		return nil, fmt.Errorf("조회 시도에 작업 폴더가 없습니다")
	}

	var paths []string
	switch attempt.Engine {
	case engine.KindYtdlp:
		if r.Ytdlp == nil || len(attempt.YtdlpTargets) == 0 {
			return nil, fmt.Errorf("yt-dlp 엔진과 확정 대상이 필요합니다")
		}
		for i, target := range attempt.YtdlpTargets {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			itemDir, err := prepareStageItem(attempt.stageDir, i)
			if err != nil {
				return nil, fmt.Errorf("yt-dlp %d번째 대상: %w", i+1, err)
			}
			path, err := r.Ytdlp.DownloadSingle(ctx, target, itemDir, attempt.Options)
			if err != nil {
				return nil, fmt.Errorf("yt-dlp 대상 %s 저장 실패: %w", target.TargetKey, err)
			}
			paths = append(paths, path)
		}

	case engine.KindGalleryDL:
		var err error
		discovery := attempt.GalleryResult
		if parsed, e := url.Parse(attempt.OriginalURL); e == nil && (strings.EqualFold(parsed.Hostname(), "instagram.com") || strings.EqualFold(parsed.Hostname(), "www.instagram.com")) {
			targets, e := discovery.InstagramTargets(attempt.OriginalURL)
			if e != nil {
				return nil, e
			}
			files := make([]engine.GalleryDLFile, 0, len(targets))
			for _, target := range targets {
				files = append(files, discovery.Files[target.FileIndex])
			}
			discovery.Files = files
		}
		paths, err = discovery.SaveMedia(ctx, attempt.stageDir)
		if err != nil {
			return nil, err
		}
		if len(paths) != len(discovery.Files) {
			return nil, fmt.Errorf("gallery-dl 저장 결과 수가 조회 대상 수와 다릅니다")
		}

	default:
		return nil, fmt.Errorf("지원하지 않는 엔진: %q", attempt.Engine)
	}

	files := make([]StagedFile, 0, len(paths))
	for i, path := range paths {
		if err := verifyStagedFile(attempt.stageDir, i, path); err != nil {
			return nil, fmt.Errorf("%d번째 저장 결과 검증 실패: %w", i+1, err)
		}
		files = append(files, StagedFile{Position: i, Path: path})
	}
	return files, nil
}
