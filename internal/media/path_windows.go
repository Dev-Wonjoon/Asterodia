package media

import (
	"fmt"
	"golang.org/x/sys/windows"
	"path/filepath"
	"strings"
	"unsafe"
)

// CanonicalPath returns the resolved path and versioned, case-aware Windows key.
func CanonicalPath(path string) (string, string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", "", err
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", "", err
	}
	defer windows.CloseHandle(h)
	buffer := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(h, &buffer[0], uint32(len(buffer)), 0)
	if err != nil || n >= uint32(len(buffer)) {
		return "", "", fmt.Errorf("실제 경로 확인 실패: %v", err)
	}
	actual := windows.UTF16ToString(buffer[:n])
	if strings.HasPrefix(actual, `\\?\UNC\`) {
		actual = `\\` + actual[8:]
	} else {
		actual = strings.TrimPrefix(actual, `\\?\`)
	}
	actual = filepath.Clean(actual)
	// Each component inherits its name comparison from its parent directory.
	// A case-sensitive ancestor must stay distinct even below an insensitive one.
	volume := filepath.VolumeName(actual)
	parts := strings.Split(strings.TrimPrefix(actual, volume+string(filepath.Separator)), string(filepath.Separator))
	prefix := volume + string(filepath.Separator)
	keyParts := []string{strings.ToLower(prefix)}
	for _, part := range parts {
		if part == "" {
			continue
		}
		dir, e := windows.UTF16PtrFromString(prefix)
		if e != nil {
			return "", "", e
		}
		parent, err := windows.CreateFile(dir, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if err != nil {
			return "", "", err
		}
		var flags uint32
		err = windows.GetFileInformationByHandleEx(parent, 23, (*byte)(unsafe.Pointer(&flags)), 4)
		windows.CloseHandle(parent)
		name := part
		if err != nil || flags&1 == 0 {
			name = strings.ToLower(name)
		}
		keyParts = append(keyParts, name)
		prefix = filepath.Join(prefix, part)
	}
	return actual, "winpath-v1:" + filepath.Join(keyParts...), nil
}
