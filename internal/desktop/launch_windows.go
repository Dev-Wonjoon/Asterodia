package desktop

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// RelaunchAsterodia runs before opening the library. Old executable paths remain
// usable by shortcuts, but only the canonical process opens the UI and database.
// Updater helper mode MUST be handled before this function is called.
func RelaunchAsterodia() (bool, error) {
	self, err := os.Executable()
	if err != nil {
		return false, err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return false, err
	}
	if !legacyExecutableName(filepath.Base(self)) {
		return false, nil
	}
	root := filepath.Dir(self)
	unlockLaunch, err := AcquireLock(filepath.Join(root, ".asterodia-launch.lock"))
	if err != nil {
		return false, fmt.Errorf("Asterodia 실행 파일 전환이 이미 진행 중입니다: %w", err)
	}
	defer unlockLaunch()
	paths, err := ResolvePaths(DataDirectoryOverride())
	if err != nil {
		return false, err
	}
	if err = os.MkdirAll(paths.Data, 0700); err != nil {
		return false, err
	}
	unlockLibrary, err := AcquireLibraryLock(paths.Data)
	if err != nil {
		return false, err
	}
	target := filepath.Join(root, "Asterodia.exe")
	rollback, commit, err := prepareCanonicalExecutable(self, target, executableIdentity)
	// The child acquires these same locks itself. No DB/configuration was opened.
	unlockLibrary()
	if err != nil {
		return false, fmt.Errorf("Asterodia.exe로 전환하지 못했습니다: %w", err)
	}
	cmd := exec.Command(target, os.Args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if err = cmd.Start(); err != nil {
		if restoreErr := rollback(); restoreErr != nil {
			return false, fmt.Errorf("Asterodia 실행 실패: %v; 실행 파일 복원 실패: %w", err, restoreErr)
		}
		return false, fmt.Errorf("Asterodia 실행 실패: %w", err)
	}
	_ = cmd.Process.Release()
	commit()
	return true, nil
}

func legacyExecutableName(name string) bool {
	switch strings.ToLower(name) {
	case "storagebucket.exe", "storagebucket-windows-amd64.exe", "storagebucket-windows-arm64.exe":
		return true
	}
	return false
}

type executableInfo struct {
	product, company string
	version          uint64
}

// Read the embedded Windows identity, not an untrusted filename. Numeric file
// versions also preserve the ordering of letter and four-component releases.
func executableIdentity(path string) (executableInfo, error) {
	var zero windows.Handle
	size, err := windows.GetFileVersionInfoSize(path, &zero)
	if err != nil || size == 0 || size > 1<<20 {
		return executableInfo{}, fmt.Errorf("실행 파일 버전 정보를 확인할 수 없습니다: %s", path)
	}
	data := make([]byte, size)
	if err = windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&data[0])); err != nil {
		return executableInfo{}, err
	}
	var fixed *windows.VS_FIXEDFILEINFO
	var length uint32
	if err = windows.VerQueryValue(unsafe.Pointer(&data[0]), `\`, unsafe.Pointer(&fixed), &length); err != nil || length < uint32(unsafe.Sizeof(*fixed)) || fixed == nil || fixed.Signature != 0xfeef04bd {
		return executableInfo{}, fmt.Errorf("올바른 실행 파일 버전 정보가 아닙니다: %s", path)
	}
	readString := func(key string) string {
		var value *uint16
		var count uint32
		// Asterodia's build/windows/info.json uses the neutral 0000 locale.
		if windows.VerQueryValue(unsafe.Pointer(&data[0]), `\StringFileInfo\000004b0\`+key, unsafe.Pointer(&value), &count) != nil || value == nil || count == 0 {
			return ""
		}
		return windows.UTF16ToString(unsafe.Slice(value, count))
	}
	return executableInfo{product: readString("ProductName"), company: readString("CompanyName"), version: uint64(fixed.FileVersionMS)<<32 | uint64(fixed.FileVersionLS)}, nil
}

// Returns rollback/commit actions so a failed spawn can restore the old target.
// Source files, shortcuts and user data are never renamed or removed.
func prepareCanonicalExecutable(self, target string, identify func(string) (executableInfo, error)) (func() error, func(), error) {
	noop := func() {}
	noRollback := func() error { return nil }
	existing, err := os.Lstat(target)
	present := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	if present {
		if !existing.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("기존 Asterodia.exe가 일반 실행 파일이 아닙니다")
		}
		same, err := sameExecutable(self, target)
		if err != nil {
			return nil, nil, err
		}
		if same {
			return noRollback, noop, nil
		}
		sourceInfo, err := identify(self)
		if err != nil {
			return nil, nil, err
		}
		targetInfo, err := identify(target)
		if err != nil {
			return nil, nil, err
		}
		if sourceInfo.product != "Asterodia" || targetInfo.product != "Asterodia" || sourceInfo.company != "Dev-Wonjoon" || targetInfo.company != sourceInfo.company {
			return nil, nil, fmt.Errorf("같은 이름의 다른 파일을 보호하기 위해 전환을 중단했습니다: %s", target)
		}
		if targetInfo.version >= sourceInfo.version {
			return noRollback, noop, nil
		}
	}
	staged, err := stageExecutable(self, filepath.Dir(target))
	if err != nil {
		return nil, nil, err
	}
	defer os.Remove(staged)
	backup := ""
	if present {
		backup, err = stageExecutable(target, filepath.Dir(target))
		if err != nil {
			return nil, nil, err
		}
	}
	if err = moveExecutable(staged, target, present); err != nil {
		if backup != "" {
			_ = os.Remove(backup)
		}
		return nil, nil, err
	}
	rollback := func() error {
		if backup != "" {
			return moveExecutable(backup, target, true)
		}
		return os.Remove(target)
	}
	return rollback, func() {
		if backup != "" {
			_ = os.Remove(backup)
		}
	}, nil
}

func sameExecutable(left, right string) (bool, error) {
	hash := func(path string) ([32]byte, error) {
		f, err := os.Open(path)
		if err != nil {
			return [32]byte{}, err
		}
		defer f.Close()
		h := sha256.New()
		_, err = io.Copy(h, f)
		var value [32]byte
		copy(value[:], h.Sum(nil))
		return value, err
	}
	a, err := hash(left)
	if err != nil {
		return false, err
	}
	b, err := hash(right)
	return a == b, err
}

func stageExecutable(source, directory string) (string, error) {
	in, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.CreateTemp(directory, ".asterodia-launch-*.exe")
	if err != nil {
		return "", err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(out.Name())
		return "", err
	}
	return out.Name(), nil
}

func moveExecutable(source, target string, replace bool) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	return windows.MoveFileEx(from, to, flags)
}
