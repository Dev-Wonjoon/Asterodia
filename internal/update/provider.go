package update

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

var stableVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// Provider requires the exact portable executable and a SHA-256 checksum.
// The framework's provider alone permits releases without a checksum sidecar.
type Provider struct {
	source   *github.Provider
	Progress func(int64, int64)
}

func NewProvider(baseURL string, client *http.Client) (*Provider, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	source, err := github.New(github.Config{
		Repository: Repository, BaseURL: baseURL, HTTPClient: client,
		ChecksumAsset: "SHA256SUMS", AssetMatcher: MatchAsset,
	})
	if err != nil {
		return nil, err
	}
	return &Provider{source: source}, nil
}

func MatchAsset(req updater.CheckRequest, assets []github.ReleaseAsset) int {
	if req.Platform != "windows" || (req.Arch != "amd64" && req.Arch != "arm64") {
		return -1
	}
	want := "StorageBucket-windows-" + req.Arch + ".exe"
	matched := -1
	for i, asset := range assets {
		if asset.Name == want {
			if matched != -1 {
				return -1
			}
			matched = i
		}
	}
	return matched
}

func (p *Provider) Name() string { return p.source.Name() }

func (p *Provider) Check(ctx context.Context, req updater.CheckRequest) (*updater.Release, error) {
	release, err := p.source.Check(ctx, req)
	if err != nil || release == nil {
		return release, err
	}
	if release.Channel != "stable" || !stableVersion.MatchString(release.Version) {
		return nil, fmt.Errorf("정식 버전 형식의 릴리스만 업데이트할 수 있습니다")
	}
	if release.Verification == nil || release.Verification.DigestAlgo != "sha256" || len(release.Verification.Digest) != sha256.Size {
		return nil, fmt.Errorf("업데이트 파일의 SHA256SUMS가 없거나 올바르지 않습니다")
	}
	if release.Artifact.Size <= 0 || release.Artifact.Size > 512<<20 {
		return nil, fmt.Errorf("업데이트 파일 크기가 올바르지 않습니다")
	}
	return release, nil
}

func (p *Provider) Download(ctx context.Context, release *updater.Release, dst io.Writer, progress func(int64, int64)) error {
	if release == nil || release.Artifact.Size <= 0 || release.Artifact.Size > 512<<20 {
		return fmt.Errorf("업데이트 파일 크기가 올바르지 않습니다")
	}
	bounded := &assetWriter{dst: dst, remaining: release.Artifact.Size}
	err := p.source.Download(ctx, release, bounded, func(written, total int64) {
		if progress != nil {
			progress(written, total)
		}
		if p.Progress != nil {
			p.Progress(written, total)
		}
	})
	if err != nil {
		return err
	}
	if bounded.remaining != 0 {
		return fmt.Errorf("업데이트 파일이 끝까지 다운로드되지 않았습니다")
	}
	return nil
}

type assetWriter struct {
	dst       io.Writer
	remaining int64
}

func (w *assetWriter) Write(bytes []byte) (int, error) {
	if int64(len(bytes)) > w.remaining {
		return 0, fmt.Errorf("업데이트 파일이 명시된 크기를 초과했습니다")
	}
	n, err := w.dst.Write(bytes)
	w.remaining -= int64(n)
	return n, err
}
