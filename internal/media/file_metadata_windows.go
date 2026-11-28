package media

import (
	"os"
	"syscall"
)

func fileBirthTime(path string) *int64 {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || data.CreationTime.HighDateTime == 0 && data.CreationTime.LowDateTime == 0 {
		return nil
	}
	value := data.CreationTime.Nanoseconds() / 1_000_000
	return &value
}
