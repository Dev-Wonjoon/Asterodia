package mediaedit

import (
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

var memoryStatusProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
var diskSpaceProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

func systemResources(path string) (available, total, disk uint64, memoryKnown, diskKnown bool) {
	var m struct {
		Length, Load                                                                     uint32
		TotalPhys, AvailPhys, TotalPage, AvailPage, TotalVirtual, AvailVirtual, Extended uint64
	}
	m.Length = uint32(unsafe.Sizeof(m))
	ok, _, _ := memoryStatusProc.Call(uintptr(unsafe.Pointer(&m)))
	available, total, memoryKnown = m.AvailPhys, m.TotalPhys, ok != 0
	for filepath.IsAbs(path) {
		if _, err := os.Stat(path); err == nil {
			break
		}
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	if p, err := windows.UTF16PtrFromString(path); err == nil && filepath.IsAbs(path) {
		var totalBytes, totalFree uint64
		ok, _, _ = diskSpaceProc.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&disk)), uintptr(unsafe.Pointer(&totalBytes)), uintptr(unsafe.Pointer(&totalFree)))
		diskKnown = ok != 0
	}
	return
}
