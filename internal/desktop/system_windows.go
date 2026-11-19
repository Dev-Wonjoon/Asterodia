package desktop

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
)

// AcquireLock precedes DB recovery, so a second instance cannot interrupt a
// currently running download in the first instance. Windows releases it on exit.
func AcquireLock(filename string) (func(), error) {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	var overlapped windows.Overlapped
	if err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped); err != nil {
		file.Close()
		return nil, fmt.Errorf("이 데이터 폴더를 사용하는 StorageBucket이 이미 실행 중입니다: %w", err)
	}
	return func() { _ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped); file.Close() }, nil
}

type SystemService struct{}

func (s *SystemService) GetDoubleClickInterval() *uint32 {
	value, _, _ := windows.NewLazySystemDLL("user32.dll").NewProc("GetDoubleClickTime").Call()
	v := uint32(value)
	if v == 0 || v > 5000 {
		return nil
	}
	return &v
}
