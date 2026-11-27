package localimport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"golang.org/x/sys/windows"
)

type fileState struct {
	Path     string `json:"path"`
	Key      string `json:"key"`
	Identity string `json:"identity"`
	Size     int64  `json:"size"`
	Mtime    string `json:"mtime"`
}

func canonical(path string) (string, string, error) { return media.CanonicalPath(path) }

func checkDirectory(root, path, key string) error {
	actual, current, err := canonical(path)
	if err != nil {
		return err
	}
	rel, e := filepath.Rel(root, actual)
	if e != nil || (rel != "." && !filepath.IsLocal(rel)) || actual != path || current != key {
		return fmt.Errorf("폴더의 실제 경로가 변경되었습니다")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	skip, err := hiddenOrLink(path, info)
	if err != nil {
		return err
	}
	if !info.IsDir() || (path != root && skip) {
		return fmt.Errorf("링크나 숨김 폴더로 바뀐 경로는 탐색할 수 없습니다")
	}
	return nil
}

func hiddenOrLink(path string, info os.FileInfo) (bool, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attributes, err := windows.GetFileAttributes(p)
	return info.Mode()&os.ModeSymlink != 0 || attributes&(windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0, err
}

func snapshot(root, path string) (fileState, error) {
	var state fileState
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return state, fmt.Errorf("선택한 폴더 내부의 파일이 필요합니다")
	}
	parts := strings.Split(rel, string(filepath.Separator))
	for i := 1; i <= len(parts); i++ {
		item := filepath.Join(root, filepath.Join(parts[:i]...))
		info, e := os.Lstat(item)
		if e != nil {
			return state, e
		}
		p, e := windows.UTF16PtrFromString(item)
		if e != nil {
			return state, e
		}
		attr, e := windows.GetFileAttributes(p)
		if e != nil {
			return state, e
		}
		if attr&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return state, fmt.Errorf("링크나 정션으로 바뀐 경로는 등록할 수 없습니다")
		}
		if i < len(parts) && !info.IsDir() {
			return state, fmt.Errorf("상위 폴더가 변경되었습니다")
		}
	}
	actual, key, err := canonical(path)
	if err != nil {
		return state, err
	}
	if rel, err = filepath.Rel(root, actual); err != nil || !filepath.IsLocal(rel) {
		return state, fmt.Errorf("선택 범위를 벗어난 원본입니다")
	}
	f, err := os.Open(actual)
	if err != nil {
		return state, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return state, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return state, fmt.Errorf("비어 있지 않은 일반 미디어 파일이 필요합니다")
	}
	var native windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &native); err != nil {
		return state, err
	}
	return fileState{actual, key, fmt.Sprintf("%x:%x:%x", native.VolumeSerialNumber, native.FileIndexHigh, native.FileIndexLow), info.Size(), fmt.Sprint(info.ModTime().UnixNano())}, nil
}
