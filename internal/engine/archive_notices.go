package engine

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Dev-Wonjoon/StorageBucket/internal/licenses"
)

var engineNoticeName = regexp.MustCompile(`(?i)^(licenses?|licences?|copying|notices?|copyright|third[_-]?party)([._-]|$)`)

// Keep original archive notices without using archive-controlled filesystem paths.
func extractArchiveNotices(archive *zip.ReadCloser, directory string) error {
	if err := os.Mkdir(directory, 0700); err != nil {
		return err
	}
	type savedNotice struct{ OriginalPath, File, SHA256 string }
	var saved []savedNotice
	var total uint64
	for _, entry := range archive.File {
		isNotice := false
		for _, part := range strings.Split(entry.Name, "/") {
			isNotice = isNotice || engineNoticeName.MatchString(part)
		}
		if entry.FileInfo().IsDir() || !isNotice {
			continue
		}
		if !entry.Mode().IsRegular() || !fs.ValidPath(entry.Name) || strings.Contains(entry.Name, `\`) || strings.Contains(entry.Name, ":") {
			return fmt.Errorf("압축 고지 파일의 경로·형식이 올바르지 않습니다: %s", entry.Name)
		}
		total += entry.UncompressedSize64
		if entry.UncompressedSize64 > 2<<20 || total > 8<<20 || len(saved) >= 128 {
			return fmt.Errorf("압축 고지 파일 크기·개수 제한 초과")
		}
		reader, err := entry.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(reader, 2<<20+1))
		closeErr := reader.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if len(data) == 0 || len(data) > 2<<20 {
			return fmt.Errorf("압축 고지 파일이 비었거나 너무 큽니다")
		}
		filename := fmt.Sprintf("archive-%03d.txt", len(saved)+1)
		if err := os.WriteFile(filepath.Join(directory, filename), data, 0600); err != nil {
			return err
		}
		saved = append(saved, savedNotice{entry.Name, filename, fmt.Sprintf("%x", sha256.Sum256(data))})
	}
	if len(saved) == 0 {
		return fmt.Errorf("FFmpeg 배포 압축 파일에 라이선스 고지가 없습니다")
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "archive-notices.json"), data, 0600)
}

// Each verified binary gets its own immutable notice directory. Older notices
// remain available when a rolling upstream release changes between installs.
func (m *Manager) retainEngineNotices(staging, binary string, definition binaryDefinition, version string) error {
	directory := filepath.Join(staging, "notices")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	var reference strings.Builder
	for _, item := range licenses.List() {
		matches := item.Name == definition.name || (strings.HasPrefix(item.Name, "FFmpeg /") && (definition.name == "ffmpeg" || definition.name == "ffprobe"))
		if !matches {
			continue
		}
		fmt.Fprintf(&reference, "%s\n%s\n%s\n\n%s", item.Name, item.License, item.Notice, item.Text)
	}
	if reference.Len() == 0 {
		return fmt.Errorf("%s 참고 라이선스 고지가 없습니다", definition.name)
	}
	if err := os.WriteFile(filepath.Join(directory, "reference-notices.txt"), []byte(reference.String()), 0600); err != nil {
		return err
	}
	file, err := os.Open(binary)
	if err != nil {
		return err
	}
	digest := sha256.New()
	_, err = io.Copy(digest, file)
	file.Close()
	if err != nil {
		return err
	}
	checksum := fmt.Sprintf("%x", digest.Sum(nil))
	metadata := struct{ Engine, Version, BinarySHA256, DownloadURL string }{definition.name, version, checksum, definition.downloadURL}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, "installed-build.json"), data, 0600); err != nil {
		return err
	}
	root := filepath.Join(m.binDir, "licenses")
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("엔진 고지 경로가 일반 폴더가 아닙니다")
	}
	target, err := os.MkdirTemp(root, definition.name+"-"+checksum[:12]+"-")
	if err != nil {
		return err
	}
	if err := os.Rename(directory, filepath.Join(target, "notices")); err != nil {
		_ = os.Remove(target) // Only the empty directory just created above.
		return err
	}
	return nil
}
