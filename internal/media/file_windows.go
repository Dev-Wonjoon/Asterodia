package media

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Open relative to the directory handle so junction changes cannot escape Root.
// Denying write/delete sharing keeps the same name and contents until close.
func OpenOwnedFile(root *os.Root, path string) (*os.File, error) {
	parent, e := root.Open(filepath.Dir(path))
	if e != nil {
		return nil, e
	}
	defer parent.Close()
	name, e := windows.NewNTUnicodeString(filepath.Base(path))
	if e != nil {
		return nil, e
	}
	attributes := windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(parent.Fd()), ObjectName: name, Attributes: windows.OBJ_CASE_INSENSITIVE}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	e = windows.NtCreateFile(&handle, windows.FILE_GENERIC_READ|windows.DELETE, &attributes, &status, nil, 0, windows.FILE_SHARE_READ, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if e != nil {
		if errors.Is(e, windows.STATUS_OBJECT_NAME_NOT_FOUND) || errors.Is(e, windows.STATUS_OBJECT_PATH_NOT_FOUND) {
			return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
		}
		return nil, e
	}
	f := os.NewFile(uintptr(handle), path)
	var info windows.ByHandleFileInformation
	if e = windows.GetFileInformationByHandle(handle, &info); e != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		f.Close()
		if e != nil {
			return nil, e
		}
		return nil, fmt.Errorf("일반 미디어 파일만 배치·정리할 수 있습니다")
	}
	return f, nil
}

func FileIdentity(f *os.File) (string, error) {
	var info windows.ByHandleFileInformation
	if e := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); e != nil {
		return "", e
	}
	return fmt.Sprintf("%08x:%08x%08x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}

func VerifyOwnedFile(f *os.File, entry FileProof) error {
	return VerifyOwnedFileContext(context.Background(), f, entry)
}
func VerifyOwnedFileContext(ctx context.Context, f *os.File, entry FileProof) error {
	identity, e := FileIdentity(f)
	if e != nil {
		return e
	}
	info, e := f.Stat()
	if e != nil {
		return e
	}
	if identity != entry.Identity || !info.Mode().IsRegular() || info.Size() != entry.Size {
		return fmt.Errorf("파일 소유권 또는 크기가 변경되어 원본을 보존했습니다")
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return e
	}
	hash := sha256.New()
	if _, e = io.Copy(hash, cancelReader{ctx: ctx, reader: f}); e != nil {
		return e
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != entry.Hash {
		return fmt.Errorf("파일 내용이 변경되어 원본을 보존했습니다")
	}
	return nil
}

func RenameOwnedFile(root *os.Root, source *os.File, path string) error {
	parent, e := root.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer parent.Close()
	name, e := windows.UTF16FromString(filepath.Base(path))
	if e != nil {
		return e
	}
	type renameInfo struct {
		Flags  uint32
		Root   windows.Handle
		Length uint32
		Name   [1]uint16
	}
	var shape renameInfo
	buffer := make([]byte, int(unsafe.Offsetof(shape.Name))+len(name)*2)
	info := (*renameInfo)(unsafe.Pointer(&buffer[0]))
	info.Root = windows.Handle(parent.Fd())
	info.Length = uint32((len(name) - 1) * 2)
	copy(unsafe.Slice(&info.Name[0], len(name)), name)
	// Flags=0 forbids replacement even if another process creates the name now.
	var status windows.IO_STATUS_BLOCK
	e = windows.NtSetInformationFile(windows.Handle(source.Fd()), &status, &buffer[0], uint32(len(buffer)), windows.FileRenameInformation)
	if errors.Is(e, windows.ERROR_ALREADY_EXISTS) || errors.Is(e, windows.ERROR_FILE_EXISTS) || errors.Is(e, windows.STATUS_OBJECT_NAME_COLLISION) {
		return fmt.Errorf("%w: %v", os.ErrExist, e)
	}
	if e != nil {
		if _, statErr := root.Lstat(path); statErr == nil {
			return fmt.Errorf("%w: %v", os.ErrExist, e)
		}
	}
	return e
}

func DeleteOwnedFile(f *os.File) error {
	pending := byte(1)
	return windows.SetFileInformationByHandle(windows.Handle(f.Fd()), windows.FileDispositionInfo, &pending, 1)
}

// CreateOwnedFile creates one basename exclusively, denying replacement or writes
// by other handles until the caller has persisted ownership and committed.
func CreateOwnedFile(root *os.Root, path string) (*os.File, error) {
	if filepath.Base(path) != path || path == "." || path == ".." {
		return nil, fmt.Errorf("파일명만 허용됩니다")
	}
	parent, e := root.Open(".")
	if e != nil {
		return nil, e
	}
	defer parent.Close()
	name, e := windows.NewNTUnicodeString(path)
	if e != nil {
		return nil, e
	}
	attr := windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(parent.Fd()), ObjectName: name, Attributes: windows.OBJ_CASE_INSENSITIVE}
	attr.Length = uint32(unsafe.Sizeof(attr))
	var h windows.Handle
	var status windows.IO_STATUS_BLOCK
	e = windows.NtCreateFile(&h, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE, &attr, &status, nil, 0, windows.FILE_SHARE_READ, windows.FILE_CREATE, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if errors.Is(e, windows.STATUS_OBJECT_NAME_COLLISION) {
		return nil, os.ErrExist
	}
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(h), path), nil
}

type FileProof struct {
	Path     string `json:"path"`
	Identity string `json:"identity"`
	Hash     string `json:"sha256"`
	Size     int64  `json:"size"`
}

// LockDirectory keeps the selected directory from being renamed or replaced
// while its os.Root and the file handles are used for an atomic DB commit.
func LockDirectory(path string) (*os.File, error) {
	name, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	h, e := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(h), path)
	var info windows.ByHandleFileInformation
	if e = windows.GetFileInformationByHandle(h, &info); e != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		f.Close()
		return nil, fmt.Errorf("일반 대상 폴더만 허용됩니다")
	}
	return f, nil
}

type cancelReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r cancelReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
