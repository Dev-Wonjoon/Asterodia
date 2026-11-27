package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Download lets gallery-dl perform extraction, authentication, retries and file
// transfer. Successful per-file sidecars are reconciled with the frozen plan.
func (g *GalleryDL) Download(ctx context.Context, originalURL string, expected GalleryDLDiscovery, stageDir string) ([]string, error) {
	wanted, err := expected.Targets(originalURL)
	if err != nil {
		return nil, err
	}
	if len(expected.PinterestVideoURLs()) > 0 {
		return nil, fmt.Errorf("Pinterest 스트리밍 영상은 yt-dlp 엔진으로 저장해야 합니다")
	}
	filter, err := expected.pinterestFileFilter()
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
	out := filepath.Join(stageDir, "engine-output")
	if err = os.Mkdir(out, 0700); err != nil {
		return nil, err
	}
	auth, finishAuth, err := prepareAuthentication(ctx, g.Auth, g.CookieBrowser, g.CookieFile, originalURL, stageDir)
	if err != nil {
		return nil, err
	}
	defer finishAuth(false)
	args := g.arguments(originalURL)
	args = append(args, networkArguments(ctx)...)
	args = append(args, "-o", "extractor.proxy-env=false", "-o", "extractor.ytdl.enabled=false")
	if filter != "" {
		args = append(args, "--filter", filter)
	}
	args = append(args, auth...)
	args = append(args, "--windows-filenames", "-D", out, "-o", `extractor.postprocessors=[{"name":"metadata","mode":"json","filename":"{_path.filename}.info.json","event":"after","private":false}]`, "--", originalURL)
	result, err := g.runner.Run(ctx, ProcessRequest{Executable: g.Executable, Arguments: args, Directory: stageDir, Timeout: 10 * time.Minute, StreamOutput: true})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrStopUnconfirmed) || errors.Is(err, ErrOutputLimit) {
			return nil, fmt.Errorf("gallery-dl 다운로드 실패: %w", err)
		}
		if classified := galleryDLFailure("", result.Stderr); classified != nil {
			return nil, classified
		}
		return nil, fmt.Errorf("gallery-dl 다운로드 실패: %w\n%s", err, strings.TrimSpace(result.Stderr))
	}
	if err = finishAuth(true); err != nil {
		return nil, err
	}

	entries, err = os.ReadDir(out)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(out)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	actual := make(map[string]string, len(wanted))
	allowed := make(map[string]bool, len(wanted)*2)
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".info.json") {
			continue
		}
		info, e := entry.Info()
		if e != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
			return nil, fmt.Errorf("엔진 완료 메타데이터 파일이 잘못되었습니다")
		}
		raw, e := root.ReadFile(entry.Name())
		if e != nil {
			return nil, e
		}
		data := decodeMetadata(raw)
		name := strings.TrimSuffix(entry.Name(), ".info.json")
		fileInfo, e := root.Stat(name)
		if e != nil || !fileInfo.Mode().IsRegular() || fileInfo.Size() <= 0 || !strings.EqualFold(mediaExtension(name), metadataText(data, "extension")) {
			return nil, fmt.Errorf("완료 메타데이터와 실제 미디어 파일이 일치하지 않습니다")
		}
		// Each sidecar includes the resolved directory and media metadata.
		one := GalleryDLDiscovery{Directories: []json.RawMessage{raw}, Files: []GalleryDLFile{{Metadata: raw}}}
		// Instagram validates complete post counts, so use the entire actual set below.
		if metadataText(data, "category") == "instagram" {
			continue
		}
		targets, e := one.Targets(originalURL)
		if e != nil {
			return nil, e
		}
		key := targets[0].TargetKey
		if _, duplicate := actual[key]; duplicate {
			return nil, fmt.Errorf("엔진 완료 결과에 중복 파일이 있습니다")
		}
		actual[key] = name
		allowed[name], allowed[entry.Name()] = true, true
	}
	if len(wanted) > 0 && wanted[0].PlatformName == "Instagram" {
		actual, allowed, err = g.instagramOutputs(root, entries, originalURL)
		if err != nil {
			return nil, err
		}
	}
	if len(actual) != len(wanted) || len(entries) != len(allowed) {
		return nil, fmt.Errorf("조회 결과와 다운로드 완료 파일 수가 다릅니다")
	}
	paths := make([]string, len(wanted))
	for i, target := range wanted {
		name, exists := actual[target.TargetKey]
		if !exists {
			return nil, fmt.Errorf("조회 결과와 다운로드 완료 파일 식별 정보가 다릅니다")
		}
		dir := filepath.Join(stageDir, fmt.Sprintf("item-%04d", i+1))
		if err = os.Mkdir(dir, 0700); err != nil {
			return nil, err
		}
		paths[i] = filepath.Join(dir, "media"+filepath.Ext(name))
		if err = os.Rename(filepath.Join(out, name), paths[i]); err != nil {
			return nil, err
		}
	}
	for _, entry := range entries {
		if err = root.Remove(entry.Name()); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	if err = root.Close(); err != nil {
		return nil, err
	}
	if err = os.Remove(out); err != nil {
		return nil, err
	}
	return paths, nil
}

func (g *GalleryDL) instagramOutputs(root *os.Root, entries []os.DirEntry, originalURL string) (map[string]string, map[string]bool, error) {
	var d GalleryDLDiscovery
	var names []string
	type output struct {
		raw      json.RawMessage
		name     string
		position int
	}
	var outputs []output
	allowed := map[string]bool{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".info.json") {
			continue
		}
		raw, err := root.ReadFile(entry.Name())
		if err != nil {
			return nil, nil, err
		}
		position, _ := strconv.Atoi(metadataText(decodeMetadata(raw), "num"))
		name := strings.TrimSuffix(entry.Name(), ".info.json")
		outputs = append(outputs, output{raw, name, position})
		allowed[entry.Name()], allowed[name] = true, true
	}
	// Restore extractor order independently of arbitrary output filenames.
	sort.SliceStable(outputs, func(i, j int) bool { return outputs[i].position < outputs[j].position })
	for _, output := range outputs {
		data := decodeMetadata(output.raw)
		if len(d.Directories) == 0 && metadataText(data, "audio_url") == "" {
			d.Directories = []json.RawMessage{output.raw}
		}
		d.Files = append(d.Files, GalleryDLFile{Metadata: output.raw, URL: metadataText(data, "audio_url")})
		names = append(names, output.name)
	}
	targets, err := d.Targets(originalURL)
	if err != nil {
		return nil, nil, err
	}
	actual := map[string]string{}
	for _, target := range targets {
		actual[target.TargetKey] = names[target.FileIndex]
	}
	return actual, allowed, nil
}
