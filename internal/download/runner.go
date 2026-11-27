package download

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

type YtdlpEngine = engine.YtdlpBackend
type GalleryDLDiscoverer = engine.GalleryDLBackend

type Runner struct {
	SecureNetwork bool
	Storage       Storage
	Ytdlp         YtdlpEngine
	GalleryDL     GalleryDLDiscoverer
	Extractors    []engine.Extractor
}

type AttemptDiscovery struct {
	engine.DownloadPlan
	stageDir      string
	jobID         string
	attemptID     string
	galleryVideos map[int]engine.YtdlpTarget
	stage         func(context.Context, string, func(int, int)) ([]string, error)
}

func (r Runner) DiscoverAttempt(
	ctx context.Context,
	jobID, attemptID, originalURL string,
	options engine.Options,
) (AttemptDiscovery, error) {
	if err := ctx.Err(); err != nil {
		return AttemptDiscovery{}, err
	}
	if _, err := engine.Select(originalURL, options); err != nil {
		return AttemptDiscovery{}, err
	}
	stageDir, err := r.Storage.PrepareStage(jobID, attemptID)
	if err != nil {
		return AttemptDiscovery{}, err
	}
	extractors := r.Extractors
	if extractors == nil {
		deps := engine.ExtractorDependencies{Ytdlp: r.Ytdlp, GalleryDL: r.GalleryDL}
		// Backend-only callers retain the generic flow. The app explicitly
		// registers site extractors with its HTTP/authentication dependencies.
		extractors = []engine.Extractor{engine.NewGenericExtractor(deps)}
	}
	plan, err := engine.DiscoverWithExtractors(ctx, engine.ExtractRequest{URL: originalURL, WorkDir: stageDir, Options: options}, extractors)
	if err != nil {
		return AttemptDiscovery{}, err
	}
	result := AttemptDiscovery{DownloadPlan: plan, stageDir: stageDir, jobID: jobID, attemptID: attemptID, stage: plan.Download}
	if plan.Engine == engine.KindGalleryDL {
		result.galleryVideos, err = r.resolvePinterestVideos(ctx, plan.GalleryDLDiscovery, originalURL, stageDir, options)
		if err != nil {
			return AttemptDiscovery{}, err
		}
		if len(result.galleryVideos) > 0 {
			// Bind the composite executor now as well; later Runner changes must
			// not change the tools used by this attempt.
			snapshot := result
			result.stage = func(ctx context.Context, _ string, _ func(int, int)) ([]string, error) {
				return r.stagePinterest(ctx, snapshot)
			}
		}
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
	media, err := a.MediaTargets()
	if err != nil {
		if errors.Is(err, engine.ErrUnsupportedMediaFormat) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedTargetDiscovery, err)
	}
	for _, target := range a.galleryVideos {
		if err := target.ValidateDirectDownload(a.Options); err != nil {
			return nil, err
		}
	}
	targets := make([]TargetInput, 0, len(media))
	for _, item := range media {
		targets = append(targets, TargetInput{TargetKey: item.TargetKey, SourceURL: item.SourceURL,
			Metadata: TargetMetadata{Kind: item.Kind, Title: item.Title, ExternalID: item.ExternalID,
				PlatformName: item.PlatformName, OwnerID: item.OwnerID, OwnerName: item.OwnerName, Duration: item.Duration}})
	}
	return targets, nil
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

	if attempt.stage == nil {
		return nil, fmt.Errorf("조회 결과에 연결된 다운로드 실행기가 필요합니다")
	}
	targets, err := attempt.MediaTargets()
	if err != nil {
		return nil, err
	}
	if attempt.Engine == engine.KindGalleryDL {
		stopProgress := watchGalleryTransfer(ctx, attempt.stageDir, len(attempt.Files))
		defer stopProgress()
	}
	paths, err := attempt.stage(ctx, attempt.stageDir, func(position, total int) {
		transferFrom(ctx).beginFile(position, total)
	})
	if err != nil {
		return nil, err
	}
	if len(paths) != len(targets) {
		return nil, fmt.Errorf("저장 결과 수가 조회 대상 수와 다릅니다")
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
