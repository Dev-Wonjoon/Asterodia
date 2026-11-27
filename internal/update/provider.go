package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// Provider requires the exact portable executable and a SHA-256 checksum.
// The framework's provider alone permits releases without a checksum sidecar.
type Provider struct {
	source   *github.Provider
	client   *http.Client
	baseURL  string
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
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	return &Provider{source: source, client: client, baseURL: strings.TrimRight(baseURL, "/")}, nil
}

func MatchAsset(req updater.CheckRequest, assets []github.ReleaseAsset) int {
	if req.Platform != "windows" || (req.Arch != "amd64" && req.Arch != "arm64") {
		return -1
	}
	// New releases include an identical legacy alias for pre-rename clients.
	// Prefer the Asterodia asset, while still accepting pre-rename releases.
	selected := -1
	for _, name := range []string{"Asterodia", "StorageBucket"} {
		want := name + "-windows-" + req.Arch + ".exe"
		matched := -1
		for i, asset := range assets {
			if asset.Name == want {
				if matched != -1 {
					return -1
				}
				matched = i
			}
		}
		if selected == -1 {
			selected = matched
		}
	}
	return selected
}

func (p *Provider) Name() string { return p.source.Name() }

func (p *Provider) Check(ctx context.Context, req updater.CheckRequest) (*updater.Release, error) {
	// Resolve and compare here: the framework's SemVer parser rejects both
	// four-component versions and post-release letter suffixes.
	body, status, err := p.fetch(ctx, p.baseURL+"/repos/"+Repository+"/releases/latest", 1<<20)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("GitHub update API returned HTTP %d", status)
	}
	var published struct {
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		Body        string    `json:"body"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
		Draft       bool      `json:"draft"`
		Prerelease  bool      `json:"prerelease"`
		Assets      []struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			Size        int64  `json:"size"`
			URL         string `json:"browser_download_url"`
			ContentType string `json:"content_type"`
		} `json:"assets"`
	}
	if err = json.Unmarshal(body, &published); err != nil {
		return nil, err
	}
	if published.Draft || published.Prerelease {
		return nil, nil
	}
	version := published.TagName
	if strings.HasPrefix(version, "v") || strings.HasPrefix(version, "V") {
		version = version[1:]
	}
	if !validVersion(version) {
		return nil, fmt.Errorf("정식 버전 형식의 릴리스만 업데이트할 수 있습니다")
	}
	comparison, err := CompareVersions(version, req.CurrentVersion)
	if err != nil {
		return nil, err
	}
	if comparison <= 0 {
		return nil, nil
	}
	assets := make([]github.ReleaseAsset, len(published.Assets))
	checksum := -1
	for n, asset := range published.Assets {
		assets[n].Name = asset.Name
		if asset.Name == "SHA256SUMS" {
			if checksum != -1 {
				return nil, fmt.Errorf("업데이트 체크섬 파일이 중복되었습니다")
			}
			checksum = n
		}
	}
	selected := MatchAsset(req, assets)
	if selected < 0 || checksum < 0 {
		return nil, fmt.Errorf("업데이트 실행 파일 또는 SHA256SUMS가 없거나 올바르지 않습니다")
	}
	asset := published.Assets[selected]
	if asset.Size <= 0 || asset.Size > 512<<20 || asset.URL == "" {
		return nil, fmt.Errorf("업데이트 파일 크기가 올바르지 않습니다")
	}
	sums, status, err := p.fetch(ctx, published.Assets[checksum].URL, 1<<20)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("업데이트 체크섬을 읽지 못했습니다: HTTP %d", status)
	}
	var digest []byte
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != asset.Name {
			continue
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size || digest != nil {
			return nil, fmt.Errorf("업데이트 파일의 SHA256SUMS가 없거나 올바르지 않습니다")
		}
		digest = decoded
	}
	if digest == nil {
		return nil, fmt.Errorf("업데이트 파일의 SHA256SUMS가 없거나 올바르지 않습니다")
	}
	return &updater.Release{
		Version: version, Channel: "stable", Name: published.Name, Notes: published.Body, PublishedAt: published.PublishedAt,
		Artifact:     updater.Artifact{Filename: asset.Name, Filetype: "exe", Size: asset.Size, Platform: req.Platform, Arch: req.Arch},
		Verification: &updater.Verification{DigestAlgo: "sha256", Digest: digest},
		Metadata:     map[string]any{"github.asset.id": asset.ID, "github.asset.url": asset.URL, "github.asset.contentType": asset.ContentType, "github.release.tag": published.TagName, "github.release.htmlURL": published.HTMLURL},
	}, nil
}

func (p *Provider) fetch(ctx context.Context, url string, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, response.StatusCode, err
	}
	if int64(len(body)) > limit {
		return nil, response.StatusCode, fmt.Errorf("업데이트 메타데이터가 너무 큽니다")
	}
	return body, response.StatusCode, nil
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
