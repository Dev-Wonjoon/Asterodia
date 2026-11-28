package engine

import (
	"bytes"
	"context"
	"fmt"
	"github.com/Dev-Wonjoon/Asterodia/internal/netguard"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const publicMediaHTMLLimit = 8 << 20

type TransferKind string

const (
	TransferYtdlp     TransferKind = "yt-dlp"
	TransferGalleryDL TransferKind = "gallery-dl"
	TransferDirect    TransferKind = "direct"
)

// DownloadPlan carries both the discovered targets and their bound executor.
// Download never selects an extractor again or falls back after targets freeze.
type DownloadPlan struct {
	GalleryDLDiscovery
	Extractor    string
	Engine       Kind
	Transfer     TransferKind
	OriginalURL  string
	Options      Options
	YtdlpTargets []YtdlpTarget
	download     func(context.Context, string, func(int, int)) ([]string, error)
}

func (p DownloadPlan) Download(ctx context.Context, stageDir string, beforeFile func(int, int)) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.download == nil || !filepath.IsAbs(stageDir) {
		return nil, fmt.Errorf("조회 결과에 연결된 다운로드 계획과 절대 작업 경로가 필요합니다")
	}
	if _, err := p.MediaTargets(); err != nil {
		return nil, err
	}
	return p.download(ctx, stageDir, beforeFile)
}

func (p DownloadPlan) MediaTargets() ([]MediaTarget, error) {
	if p.Engine == KindYtdlp {
		kind := "video"
		if p.Options.Type == "audio" || p.Options.Quality == "audio" {
			kind = "audio"
		}
		targets := make([]MediaTarget, 0, len(p.YtdlpTargets))
		for _, target := range p.YtdlpTargets {
			if err := target.ValidateDirectDownload(p.Options); err != nil {
				return nil, err
			}
			targets = append(targets, MediaTarget{TargetKey: target.TargetKey, SourceURL: target.WebpageURL,
				Kind: kind, Title: target.Title, ExternalID: target.ID, PlatformName: target.ExtractorKey,
				OwnerID: target.OwnerID, OwnerName: target.OwnerName, Duration: target.Duration})
		}
		if len(targets) == 0 {
			return nil, fmt.Errorf("확정할 다운로드 대상이 없습니다")
		}
		return targets, nil
	}
	if p.Engine != KindGalleryDL {
		return nil, ErrUnsupportedURL
	}
	if p.Options.Type == "audio" || p.Options.Quality == "audio" ||
		(p.Options.Quality != "" && p.Options.Quality != "best") || p.Options.Extension != "" || p.Options.VideoCodec != "" || p.Options.AudioCodec != "" {
		return nil, fmt.Errorf("%w: 선택된 추출기는 요청한 변환 옵션을 처리하지 못합니다", ErrUnsupportedMediaFormat)
	}
	return p.GalleryDLDiscovery.Targets(p.OriginalURL)
}

func galleryDownloadPlan(req ExtractRequest, data GalleryDLDiscovery, backend GalleryDLBackend) DownloadPlan {
	return DownloadPlan{GalleryDLDiscovery: data, Engine: KindGalleryDL, Transfer: TransferGalleryDL,
		OriginalURL: req.URL, Options: req.Options,
		download: func(ctx context.Context, dir string, _ func(int, int)) ([]string, error) {
			return backend.Download(ctx, req.URL, data, dir)
		}}
}

func directDownloadPlan(req ExtractRequest, data GalleryDLDiscovery, download func(context.Context, string, GalleryDLDiscovery, string) ([]string, error)) DownloadPlan {
	return DownloadPlan{GalleryDLDiscovery: data, Engine: KindGalleryDL, Transfer: TransferDirect,
		OriginalURL: req.URL, Options: req.Options,
		download: func(ctx context.Context, dir string, _ func(int, int)) ([]string, error) {
			return download(ctx, req.URL, data, dir)
		}}
}

func anonymousMediaGET(ctx context.Context, supplied *http.Client, address string, media bool, agent, label string, allowed func(*url.URL, bool) bool) (*http.Response, error) {
	parsed, err := url.Parse(address)
	if err != nil || !allowed(parsed, media) {
		return nil, fmt.Errorf("%s 응답에 허용되지 않은 주소가 있습니다", label)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", agent)
	request.Header.Set("Accept", "*/*")
	if !media {
		request.Header.Set("Accept", "text/html")
	}
	client := *netguard.NewClient()
	client.Timeout = 0
	defer client.CloseIdleConnections()
	if supplied != nil {
		client = *supplied
	}
	client.Jar = nil
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if strings.HasPrefix(next.URL.Path, "/accounts/login") {
			return ErrAuthenticationRequired
		}
		if len(via) >= 5 || !allowed(next.URL, media) {
			return fmt.Errorf("%s 리다이렉트 주소가 잘못되었습니다", label)
		}
		if previousRedirect != nil {
			return previousRedirect(next, via)
		}
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		// Avoid including signed CDN URLs in diagnostic messages.
		if e, ok := err.(*url.Error); ok {
			err = e.Err
		}
		return nil, fmt.Errorf("%s 요청 실패: %w", label, err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		if classified := galleryDLFailure("", fmt.Sprintf("HTTP %d", response.StatusCode)); classified != nil {
			return nil, classified
		}
		return nil, fmt.Errorf("%s 요청 실패: HTTP %d", label, response.StatusCode)
	}
	return response, nil
}

func downloadDirectMedia(ctx context.Context, originalURL string, expected GalleryDLDiscovery, stageDir string, label string, save func(context.Context, string, string, io.Writer) error) (paths []string, resultErr error) {
	targets, err := expected.Targets(originalURL)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(stageDir) {
		return nil, fmt.Errorf("작업 폴더는 절대 경로여야 합니다")
	}
	entries, err := os.ReadDir(stageDir)
	if err != nil {
		return nil, err
	}
	if len(entries) != 0 {
		return nil, fmt.Errorf("작업 폴더가 비어 있어야 합니다")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var createdFiles, createdDirs []string
	defer func() {
		if resultErr != nil {
			// Remove only files/directories created by this operation, never the stage itself.
			for _, path := range createdFiles {
				_ = os.Remove(path)
			}
			for _, dir := range createdDirs {
				_ = os.Remove(dir)
			}
		}
	}()
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file := expected.Files[target.FileIndex]
		metadata := decodeMetadata(file.Metadata)
		dir := filepath.Join(stageDir, fmt.Sprintf("item-%04d", i+1))
		if err := os.Mkdir(dir, 0700); err != nil {
			return nil, err
		}
		createdDirs = append(createdDirs, dir)
		path := filepath.Join(dir, "media."+metadataText(metadata, "extension"))
		partial := path + ".part"
		out, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		createdFiles = append(createdFiles, partial)
		err = save(ctx, file.URL, metadataText(metadata, "extension"), out)
		closeErr := out.Close()
		if err != nil {
			return nil, fmt.Errorf("%s %d번째 파일 저장 실패: %w", label, i+1, err)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if err := os.Rename(partial, path); err != nil {
			return nil, err
		}
		createdFiles = append(createdFiles, path)
		paths = append(paths, path)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return paths, nil
}

func saveAnonymousMedia(ctx context.Context, address, extension string, out io.Writer, get func(context.Context, string, bool) (*http.Response, error), label string) error {
	response, err := get(ctx, address, true)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	prefix := make([]byte, 512)
	n, err := io.ReadFull(response.Body, prefix)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return err
	}
	prefix = prefix[:n]
	typeName := http.DetectContentType(prefix)
	wantType := map[string]string{"jpg": "image/jpeg", "jpeg": "image/jpeg", "png": "image/png", "mp4": "video/mp4"}[extension]
	if n == 0 || wantType == "" || typeName != wantType {
		return fmt.Errorf("%s 응답이 요청한 사진·영상 파일이 아닙니다", label)
	}
	written, err := io.Copy(out, io.MultiReader(bytes.NewReader(prefix), response.Body))
	if err != nil {
		return err
	}
	if response.ContentLength >= 0 && written != response.ContentLength {
		return io.ErrUnexpectedEOF
	}
	return ctx.Err()
}

func ytdlpDownloadPlan(req ExtractRequest, targets []YtdlpTarget, backend YtdlpBackend) DownloadPlan {
	plan := DownloadPlan{Engine: KindYtdlp, Transfer: TransferYtdlp, OriginalURL: req.URL, Options: req.Options, YtdlpTargets: targets}
	plan.download = func(ctx context.Context, dir string, before func(int, int)) ([]string, error) {
		var paths []string
		for i, target := range targets {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			item := filepath.Join(dir, fmt.Sprintf("item-%04d", i+1))
			if err := os.Mkdir(item, 0700); err != nil {
				return nil, err
			}
			if before != nil {
				before(i+1, len(targets))
			}
			path, err := backend.DownloadSingle(ctx, target, item, req.Options)
			if err != nil {
				return nil, fmt.Errorf("yt-dlp 대상 %s 저장 실패: %w", target.TargetKey, err)
			}
			paths = append(paths, path)
		}
		return paths, nil
	}
	return plan
}
