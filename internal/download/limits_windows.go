package download

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"
)

const MaxQueuedDownloads = 20
const MaxDownloadBytes int64 = 20 << 30
const MinFreeDownloadBytes uint64 = 5 << 30

var ErrResourceLimit = errors.New("다운로드 자원 제한을 초과했습니다. 작업 수·용량과 저장 공간을 확인하세요")
var diskFreeProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

func downloadFreeBytes(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available, total, free uint64
	ok, _, e := diskFreeProc.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&available)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if ok == 0 {
		return 0, e
	}
	return available, nil
}

func checkDownloadResources(root, stage string) error {
	return checkDownloadUsage(root, stage, MaxDownloadBytes, MinFreeDownloadBytes, downloadFreeBytes)
}
func checkDownloadUsage(root, stage string, maxBytes int64, minFree uint64, freeBytes func(string) (uint64, error)) error {
	free, err := freeBytes(root)
	if err != nil {
		return fmt.Errorf("저장 공간 확인 실패: %w", err)
	}
	if free < minFree {
		return ErrResourceLimit
	}
	if stage == "" {
		return nil
	}
	var size int64
	var entries int
	err = filepath.WalkDir(stage, func(path string, d fs.DirEntry, e error) error {
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		entries++
		if entries > 100000 {
			return ErrResourceLimit
		}
		if d.Type()&os.ModeSymlink != 0 {
			return ErrResourceLimit
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return ErrResourceLimit
		}
		size += info.Size()
		if size > maxBytes {
			return ErrResourceLimit
		}
		return nil
	})
	return err
}

func watchDownloadResources(ctx context.Context, cancel context.CancelFunc, root, stage string) func() error {
	done := make(chan struct{})
	stop := make(chan struct{})
	var once sync.Once
	var failure error
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				if err := checkDownloadResources(root, stage); err != nil {
					failure = err
					cancel()
					return
				}
			}
		}
	}()
	return func() error { once.Do(func() { close(stop) }); <-done; return failure }
}

func (s *Service) downloadQueueFull(ctx context.Context) (bool, error) {
	var outstanding int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM download_queue WHERE state IN ('pending','running')").Scan(&outstanding); err != nil {
		return false, err
	}
	executing := 0
	s.executing.Range(func(key, value any) bool { executing++; return true })
	return outstanding-executing >= MaxQueuedDownloads, nil
}
