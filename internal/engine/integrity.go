package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type enginePin struct{ URL, ArchiveSHA, BinarySHA string }

// Digests are fixed in the app, not downloaded beside the binary at runtime.
// Sources: official release SHA2-256SUMS/SHA256SUMS and the verified BtbN ZIP.
var enginePins = map[string]enginePin{
	"yt-dlp":      {"https://github.com/yt-dlp/yt-dlp/releases/download/2026.10.26/yt-dlp.exe", "f5389d3698ecde4ef06f4e6b1ce0294fe9ac1396228c8990b8d3416d6d177a7d", "f5389d3698ecde4ef06f4e6b1ce0294fe9ac1396228c8990b8d3416d6d177a7d"},
	"gallery-dl":  {"https://codeberg.org/mikf/gallery-dl/releases/download/v1.32.20/gallery-dl.exe", "e14e498e0d95a216b3019b4f899d813ebb322868052f4f0ecfa3a0600deb84b0", "e14e498e0d95a216b3019b4f899d813ebb322868052f4f0ecfa3a0600deb84b0"},
	"ffmpeg":      {"https://github.com/FFmpeg-Builds/FFmpeg-Builds/releases/download/autobuild-2026-11-24-12-41/ffmpeg-N-129267-g9ec5cf1aac-win64-gpl.zip", "32cbd8dbacb8ab625f33799a12ca500ba3ef6161a8f9989d158fde9c4d0a71d7", "60040c633742e8c0d3b697ab5e86ad804ec5ca1b287e1851aa2a9684c612d216"},
	"ffprobe":     {"https://github.com/FFmpeg-Builds/FFmpeg-Builds/releases/download/autobuild-2026-11-24-12-41/ffmpeg-N-129267-g9ec5cf1aac-win64-gpl.zip", "32cbd8dbacb8ab625f33799a12ca500ba3ef6161a8f9989d158fde9c4d0a71d7", "2136f4e6de45e53c5d3c05b1e3eed2a8376e62a6d3597604dd9a18bfcfd59d53"},
	"imagemagick": {"https://github.com/ImageMagick/ImageMagick/releases/download/7.1.2-34/ImageMagick-7.1.2-34-portable-Q16-x64.7z", "52cc6a441c7884f5ff4274b410c9b90c4b7d54245dcdf4d32988bb4e4df4e5d1", "1232979c05fa7c4bd28a7a1d0101568b10afeb0c31b8a04414a463b4c0e007d0"},
}

// Official Visual Studio 2022 release catalog, Microsoft CRT x64 redist VSIX.
// Only its release OpenMP DLL is deployed; debug_nonredist files are excluded.
var imageRuntimePin = enginePin{
	URL:        "https://download.visualstudio.microsoft.com/download/pr/45d3b8dd-bced-4b37-9974-142f748d710c/4aaf54db0bfc9435f7c3660e1a00237a4b556042bfeea64bde44c2e0194e6ee5/Microsoft.VC.14.44.17.14.CRT.Redist.X64.base.vsix",
	ArchiveSHA: "4aaf54db0bfc9435f7c3660e1a00237a4b556042bfeea64bde44c2e0194e6ee5",
	BinarySHA:  "55aba23cdcd6484fbb06f4155b8ca75adfce7a881f10afd0c49457165e677164",
}

var integrityRoot struct {
	sync.RWMutex
	path string
}

// ConfigureIntegrityRoot is called once by the desktop app before services start.
func ConfigureIntegrityRoot(path string) {
	integrityRoot.Lock()
	defer integrityRoot.Unlock()
	if path == "" {
		integrityRoot.path = ""
		return
	}
	integrityRoot.path = filepath.Clean(path)
}

func verifyEngineDigest(path, digest string) error {
	if len(digest) != 64 {
		return fmt.Errorf("엔진 무결성 정보가 없습니다")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxEngineFileSize {
		return fmt.Errorf("엔진은 일반 파일이어야 합니다")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return fmt.Errorf("엔진 파일이 변경됐습니다")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return fmt.Errorf("엔진 SHA-256 검증에 실패했습니다. 설정에서 다시 설치하세요")
	}
	return nil
}

func verifyManagedExecutable(path string) error {
	integrityRoot.RLock()
	root := integrityRoot.path
	integrityRoot.RUnlock()
	if root == "" {
		return nil
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) {
		return nil
	}
	for _, definition := range binaryDefinitions() {
		candidate := rel
		// Install probes execute before an atomic commit, inside Manager's
		// staging directory. They must pass the same pinned-file checks.
		if directory, staged, ok := strings.Cut(rel, string(filepath.Separator)); ok && strings.HasPrefix(directory, ".install-"+definition.name+"-") {
			candidate = staged
		}
		if filepath.Clean(candidate) == filepath.Clean(definition.filename) {
			if err := verifyEngineDigest(path, enginePins[definition.name].BinarySHA); err != nil {
				return err
			}
			if definition.name == "imagemagick" {
				return verifyImageRuntime(filepath.Dir(path))
			}
			return nil
		}
	}
	return fmt.Errorf("관리 대상 엔진 실행 파일이 아닙니다")
}
