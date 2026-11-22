package engine

import (
	"archive/zip"

	"context"
	"crypto/sha256"
	"fmt"
	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	UpdateGate        *api.UpdateGate
	binDir            string
	runner            ProcessRunner
	installMu         sync.Mutex
	onInstallProgress func(InstallProgress)
}

type binaryDefinition struct {
	name            string
	filename        string
	expectedVersion string
	versionArgs     []string
	downloadURL     string
	archive         bool
	sevenZip        bool
	sha256          string
}

func binaryDefinitions() []binaryDefinition {
	return []binaryDefinition{
		{name: "ffprobe", filename: "ffprobe.exe", versionArgs: []string{"-version"}, downloadURL: "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-win64-gpl.zip", archive: true},
		{name: "imagemagick", filename: "imagemagick/magick.exe", versionArgs: []string{"-version"}, downloadURL: "https://github.com/ImageMagick/ImageMagick/releases/download/7.1.2-34/ImageMagick-7.1.2-34-portable-Q16-x64.7z", archive: true, sevenZip: true, sha256: "52cc6a441c7884f5ff4274b410c9b90c4b7d54245dcdf4d32988bb4e4df4e5d1"},
		{
			name:            "yt-dlp",
			filename:        "yt-dlp.exe",
			expectedVersion: "2026.10.26",
			versionArgs:     []string{"--ignore-config", "--version"},
			downloadURL: "https://github.com/yt-dlp/yt-dlp/" +
				"releases/download/2026.10.26/yt-dlp.exe",
		},
		{
			name:            "gallery-dl",
			filename:        "gallery-dl.exe",
			expectedVersion: "1.32.20",
			versionArgs:     []string{"--config-ignore", "--version"},
			downloadURL: "https://codeberg.org/mikf/gallery-dl/" +
				"releases/download/v1.32.20/gallery-dl.exe",
		},
		{
			name:        "ffmpeg",
			filename:    "ffmpeg.exe",
			versionArgs: []string{"-version"},
			downloadURL: "https://github.com/BtbN/FFmpeg-Builds/" +
				"releases/download/latest/ffmpeg-master-latest-win64-gpl.zip",
			archive: true,
		},
	}
}

func lookupBinary(name string) (binaryDefinition, error) {
	for _, definition := range binaryDefinitions() {
		if definition.name == name {
			return definition, nil
		}
	}

	return binaryDefinition{}, fmt.Errorf("알 수 없는 엔진: %s", name)
}

func NewManager(
	binDir string,
	onInstallProgress func(InstallProgress),
) (*Manager, error) {
	if !filepath.IsAbs(binDir) {
		return nil, fmt.Errorf(
			"엔진 디렉터리는 절대 경로여야 합니다: %s",
			binDir,
		)
	}

	return &Manager{
		binDir:            filepath.Clean(binDir),
		onInstallProgress: onInstallProgress,
	}, nil
}

// Inspect는 Windows용 엔진 파일을 조회한다.
// 폴더 생성, 파일 변경, 외부 프로세스 실행은 하지 않는다.
func (m *Manager) Inspect() (map[string]BinaryStatus, error) {
	binaries := binaryDefinitions()

	result := make(map[string]BinaryStatus, len(binaries))

	for _, binary := range binaries {
		path := filepath.Join(m.binDir, binary.filename)
		status := BinaryStatus{Path: path}

		info, err := os.Lstat(path)

		switch {
		case os.IsNotExist(err):
			// 파일이나 bin 폴더가 아직 없으면 미설치 상태로 반환한다.
		case err != nil:
			return nil, fmt.Errorf(
				"%s 파일 확인: %w",
				binary.name,
				err,
			)
		default:
			status.Present = info.Mode().IsRegular() && info.Size() > 0
		}

		result[binary.name] = status
	}

	return result, nil
}

// Status는 각 엔진의 버전 조회 성공 여부를 반환한다.
func (m *Manager) Status(
	ctx context.Context,
) (map[string]Status, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	binaries, err := m.Inspect()
	if err != nil {
		return nil, err
	}

	type probeResult struct {
		name   string
		status Status
	}

	results := make(chan probeResult, len(binaries))

	for name, binary := range binaries {
		go func(name string, binary BinaryStatus) {
			status := Status{}

			if !binary.Present {
				status.Error = "엔진 실행 파일이 없거나 올바른 파일이 아닙니다."
			} else {
				version, err := m.runner.ReadVersion(
					ctx,
					name,
					binary.Path,
				)

				if err != nil {
					status.Error = err.Error()
				} else {
					status.Installed = true
					status.Version = &version
				}
			}

			results <- probeResult{
				name:   name,
				status: status,
			}
		}(name, binary)
	}

	statuses := make(map[string]Status, len(binaries))

	for range binaries {
		result := <-results
		statuses[result.name] = result.status
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return statuses, nil
}

const maxEngineFileSize int64 = 512 << 20

func (m *Manager) Install(
	ctx context.Context,
	name string,
	installID string,
) (success bool, installErr error) {
	leaveUpdate, updateAllowed := m.UpdateGate.Enter()
	if !updateAllowed {
		return false, fmt.Errorf("%s", api.UpdateBlocked)
	}
	defer leaveUpdate()
	if installID == "" {
		return false, fmt.Errorf("설치 요청 식별자가 없습니다")
	}

	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return false, fmt.Errorf("현재 엔진 설치는 Windows x64만 지원합니다")
	}

	definition, err := lookupBinary(name)
	if err != nil {
		return false, err
	}

	if !m.installMu.TryLock() {
		return false, fmt.Errorf("다른 엔진을 설치하고 있습니다")
	}
	defer m.installMu.Unlock()

	report := func(
		phase string,
		downloaded int64,
		total *int64,
		message string,
	) {
		if m.onInstallProgress == nil {
			return
		}

		m.onInstallProgress(InstallProgress{
			InstallID:  installID,
			Engine:     name,
			Phase:      phase,
			Downloaded: downloaded,
			Total:      total,
			Error:      message,
		})
	}

	defer func() {
		if installErr != nil {
			report("failed", 0, nil, installErr.Error())
		} else if success {
			report("completed", 0, nil, "")
		}
	}()

	report("preparing", 0, nil, "")

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	if err := ctx.Err(); err != nil {
		return false, err
	}

	destination := filepath.Join(m.binDir, definition.filename)

	// 정상 동작하는 기존 엔진은 재사용한다.
	info, err := os.Lstat(destination)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf(
				"엔진 경로에 일반 파일이 아닌 항목이 있습니다: %s",
				destination,
			)
		}

		report("verifying", 0, nil, "")
		if _, err := m.runner.ReadVersion(
			ctx, name, destination,
		); err == nil {
			return true, nil
		}

	case !os.IsNotExist(err):
		return false, fmt.Errorf("기존 엔진 확인: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return false, err
	}

	if err := os.MkdirAll(m.binDir, 0o755); err != nil {
		return false, fmt.Errorf("엔진 폴더 생성: %w", err)
	}

	staging, err := os.MkdirTemp(m.binDir, ".install-"+name+"-")
	if err != nil {
		return false, fmt.Errorf("임시 설치 폴더 생성: %w", err)
	}
	defer os.RemoveAll(staging)

	stagedBinary := filepath.Join(staging, definition.filename)
	downloadPath := stagedBinary

	if definition.archive {
		downloadPath = filepath.Join(staging, "payload.zip")
	}

	report("downloading", 0, nil, "")

	if err := downloadEngineAsset(
		ctx,
		definition.downloadURL,
		downloadPath,
		func(downloaded int64, total *int64) {
			report("downloading", downloaded, total, "")
		},
	); err != nil {
		return false, fmt.Errorf("%s 다운로드: %w", name, err)
	}

	if definition.archive {
		report("extracting", 0, nil, "")
		if definition.sevenZip {
			if err := extractImageEngine(ctx, downloadPath, filepath.Dir(stagedBinary), definition.sha256); err != nil {
				return false, err
			}
		} else if err := extractEngineBinary(
			downloadPath,
			stagedBinary,
			definition.filename,
		); err != nil {
			return false, fmt.Errorf("%s 압축 해제: %w", name, err)
		}
	}

	report("verifying", 0, nil, "")
	if err := validateEngineBinary(stagedBinary); err != nil {
		return false, fmt.Errorf("%s 파일 검증: %w", name, err)
	}

	version, err := m.runner.ReadVersion(
		ctx, name, stagedBinary,
	)
	if err != nil {
		return false, err
	}

	if err := ctx.Err(); err != nil {
		return false, err
	}

	report("committing", 0, nil, "")
	if definition.sevenZip {
		current := filepath.Dir(destination)
		previous := filepath.Join(staging, "previous")
		if info, e := os.Lstat(current); e == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return false, fmt.Errorf("이미지 엔진 경로가 일반 폴더가 아닙니다")
			}
			if e = os.Rename(current, previous); e != nil {
				return false, e
			}
		} else if !os.IsNotExist(e) {
			return false, e
		}
		if e := os.Rename(filepath.Dir(stagedBinary), current); e != nil {
			_ = os.Rename(previous, current)
			return false, e
		}
		return true, nil
	}
	if err := m.retainEngineNotices(staging, stagedBinary, definition, version); err != nil {
		return false, fmt.Errorf("%s 라이선스 고지 보존: %w", name, err)
	}
	if err := replaceEngineBinary(
		stagedBinary, destination,
	); err != nil {
		return false, fmt.Errorf("%s 설치 파일 반영: %w", name, err)
	}

	return true, nil
}

func extractImageEngine(ctx context.Context, archive, destination, digest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, err = io.Copy(hash, f)
	f.Close()
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != digest {
		return fmt.Errorf("ImageMagick 배포 파일의 SHA-256이 다릅니다")
	}
	if err = os.MkdirAll(destination, 0700); err != nil {
		return err
	}
	tar := filepath.Join(os.Getenv("SystemRoot"), "System32", "tar.exe")
	result, err := new(ProcessRunner).Run(ctx, ProcessRequest{Executable: tar, Directory: destination, Timeout: time.Minute, Arguments: []string{"-tf", archive}})
	if err != nil {
		return fmt.Errorf("Windows 압축 도구 확인 실패: %w", err)
	}
	for _, name := range strings.Split(result.Stdout, "\n") {
		name = strings.TrimSpace(name)
		if name != "" && !filepath.IsLocal(filepath.FromSlash(name)) {
			return fmt.Errorf("이미지 엔진 압축 경로가 올바르지 않습니다")
		}
	}
	_, err = new(ProcessRunner).Run(ctx, ProcessRequest{Executable: tar, Directory: destination, Timeout: time.Minute, Arguments: []string{"-xf", archive, "-C", destination}})
	return err
}

func downloadEngineAsset(
	ctx context.Context,
	address string,
	destination string,
	onProgress func(downloaded int64, total *int64),
) error {
	request, err := http.NewRequestWithContext(
		ctx, http.MethodGet, address, nil,
	)
	if err != nil {
		return err
	}
	if request.URL.Scheme != "https" {
		return fmt.Errorf("HTTPS 다운로드 주소가 필요합니다")
	}

	request.Header.Set("User-Agent", "StorageBucket")

	client := &http.Client{
		CheckRedirect: func(
			request *http.Request,
			via []*http.Request,
		) error {
			if len(via) >= 10 {
				return fmt.Errorf("리다이렉트가 너무 많습니다")
			}
			if request.URL.Scheme != "https" {
				return fmt.Errorf("HTTPS 이외의 리다이렉트는 지원하지 않습니다")
			}
			return nil
		},
	}

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxEngineFileSize {
		return fmt.Errorf("다운로드 파일이 너무 큽니다")
	}

	var total *int64
	if response.ContentLength > 0 {
		length := response.ContentLength
		total = &length
	}

	progress := &downloadProgressWriter{
		total:   total,
		publish: onProgress,
	}
	progress.send()

	source := io.TeeReader(response.Body, progress)

	if err := writeEngineFile(destination, source); err != nil {
		return err
	}

	progress.send()
	return nil
}

func writeEngineFile(
	destination string,
	source io.Reader,
) error {
	file, err := os.OpenFile(
		destination,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if err != nil {
		return err
	}
	defer file.Close()

	size, err := io.Copy(
		file,
		io.LimitReader(source, maxEngineFileSize+1),
	)
	if err != nil {
		return err
	}
	if size == 0 {
		return fmt.Errorf("파일이 비어 있습니다")
	}
	if size > maxEngineFileSize {
		return fmt.Errorf("파일 크기 제한을 초과했습니다")
	}

	if err := file.Sync(); err != nil {
		return err
	}

	return file.Close()
}

func extractEngineBinary(
	archivePath string,
	destination string,
	filename string,
) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()

	var selected *zip.File

	for _, entry := range archive.File {
		if path.Base(entry.Name) != filename {
			continue
		}

		if selected != nil {
			return fmt.Errorf("압축 파일에 %s가 여러 개 있습니다", filename)
		}
		if !entry.FileInfo().Mode().IsRegular() {
			return fmt.Errorf("%s가 일반 파일이 아닙니다", filename)
		}

		selected = entry
	}

	if selected == nil {
		return fmt.Errorf("압축 파일에 %s가 없습니다", filename)
	}
	if selected.UncompressedSize64 > uint64(maxEngineFileSize) {
		return fmt.Errorf("압축 해제할 파일이 너무 큽니다")
	}

	reader, err := selected.Open()
	if err != nil {
		return err
	}
	defer reader.Close()

	// 압축 내부 경로 대신 지정한 목적지에 파일 하나만 기록한다.
	if err := writeEngineFile(destination, reader); err != nil {
		return err
	}
	return extractArchiveNotices(archive, filepath.Join(filepath.Dir(destination), "notices"))
}

func validateEngineBinary(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	var signature [2]byte
	if _, err := io.ReadFull(file, signature[:]); err != nil {
		return err
	}

	if string(signature[:]) != "MZ" {
		return fmt.Errorf("Windows 실행 파일 형식이 아닙니다")
	}

	return nil
}

type downloadProgressWriter struct {
	downloaded int64
	total      *int64
	last       time.Time
	publish    func(downloaded int64, total *int64)
}

func (p *downloadProgressWriter) Write(data []byte) (int, error) {
	p.downloaded += int64(len(data))

	if time.Since(p.last) >= 150*time.Millisecond {
		p.send()
	}

	return len(data), nil
}

func (p *downloadProgressWriter) send() {
	p.last = time.Now()

	if p.publish != nil {
		p.publish(p.downloaded, p.total)
	}
}
