package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

type YtdlpEngine interface {
	Discover(context.Context, string, string) (json.RawMessage, error)
	DownloadSingle(context.Context, engine.YtdlpTarget, string, engine.Options) (string, error)
}

type GalleryDLDiscoverer interface {
	Discover(context.Context, string, string) (engine.GalleryDLDiscovery, error)
	Download(context.Context, string, engine.GalleryDLDiscovery, string) ([]string, error)
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
	galleryVideos map[int]engine.YtdlpTarget
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

	other := engine.KindGalleryDL
	if kind == engine.KindGalleryDL {
		other = engine.KindYtdlp
	}
	chain := engine.RunDiscoveryChain(ctx, r.discoveryHandler(kind, result), r.discoveryHandler(other, result))
	return chain.Value, chain.Err
}

func (r Runner) discoveryHandler(candidate engine.Kind, base AttemptDiscovery) engine.DiscoveryHandler[AttemptDiscovery] {
	return engine.DiscoveryHandler[AttemptDiscovery]{Name: string(candidate), Handle: func(ctx context.Context) engine.DiscoveryStepResult[AttemptDiscovery] {
		// Every handler starts from the request, never from a failed strategy's
		// partial targets or transfer plan.
		result := base
		result.Engine = candidate
		originalURL, stageDir, options := base.OriginalURL, base.stageDir, base.Options
		var err error
		switch candidate {
		case engine.KindYtdlp:
			if r.Ytdlp == nil {
				err = engine.ErrUnsupportedURL
				break
			}
			var raw json.RawMessage
			raw, err = r.Ytdlp.Discover(ctx, originalURL, stageDir)
			if err == nil {
				result.YtdlpTargets, err = engine.ParseYtdlpTargets(raw, originalURL)
			}
		case engine.KindGalleryDL:
			if r.GalleryDL == nil {
				err = engine.ErrUnsupportedURL
				break
			}
			result.GalleryResult, err = r.GalleryDL.Discover(ctx, originalURL, stageDir)
			if err == nil {
				result.galleryVideos, err = r.resolvePinterestVideos(ctx, result.GalleryResult, originalURL, stageDir, options)
				if err != nil {
					return engine.DiscoveryStepResult[AttemptDiscovery]{Decision: engine.DiscoveryStop, Value: result, Err: err}
				}
			}
		}
		decision := engine.DiscoveryHandled
		if err != nil {
			decision = engine.DiscoveryStop
			if errors.Is(err, engine.ErrUnsupportedURL) && !errors.Is(err, engine.ErrRateLimited) &&
				!errors.Is(err, engine.ErrOutputLimit) && !errors.Is(err, engine.ErrStopUnconfirmed) &&
				!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				decision = engine.DiscoveryNext
			}
		}
		return engine.DiscoveryStepResult[AttemptDiscovery]{Decision: decision, Value: result, Err: err}
	}}
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
		if a.Options.Type == "audio" || (a.Options.Quality != "" && a.Options.Quality != "best") || a.Options.Extension != "" {
			return nil, fmt.Errorf("%w: 선택된 엔진은 요청한 변환 옵션을 처리하지 못합니다", engine.ErrUnsupportedMediaFormat)
		}
		media, err := a.GalleryResult.Targets(a.OriginalURL)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnsupportedTargetDiscovery, err)
		}
		for _, target := range a.galleryVideos {
			if err := target.ValidateDirectDownload(a.Options); err != nil {
				return nil, err
			}
		}

		targets := make([]TargetInput, 0, len(media))
		for _, item := range media {
			targets = append(targets, TargetInput{TargetKey: item.TargetKey, SourceURL: item.SourceURL, Metadata: TargetMetadata{Kind: item.Kind, Title: item.Title, ExternalID: item.ExternalID, PlatformName: item.PlatformName, OwnerID: item.OwnerID, OwnerName: item.OwnerName}})
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
			transferFrom(ctx).beginFile(i+1, len(attempt.YtdlpTargets))
			path, err := r.Ytdlp.DownloadSingle(ctx, target, itemDir, attempt.Options)
			if err != nil {
				return nil, fmt.Errorf("yt-dlp 대상 %s 저장 실패: %w", target.TargetKey, err)
			}
			paths = append(paths, path)
		}

	case engine.KindGalleryDL:
		var err error
		if r.GalleryDL == nil {
			return nil, fmt.Errorf("gallery-dl 엔진이 연결되지 않았습니다")
		}
		stopProgress := watchGalleryTransfer(ctx, attempt.stageDir, len(attempt.GalleryResult.Files))
		defer stopProgress()
		if len(attempt.galleryVideos) > 0 {
			paths, err = r.stagePinterest(ctx, attempt)
		} else {
			paths, err = r.GalleryDL.Download(ctx, attempt.OriginalURL, attempt.GalleryResult, attempt.stageDir)
		}
		if err != nil {
			return nil, err
		}
		targets, e := attempt.GalleryResult.Targets(attempt.OriginalURL)
		if e != nil {
			return nil, e
		}
		if len(paths) != len(targets) {
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
