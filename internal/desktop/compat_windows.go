package desktop

import (
	"os"
	"path/filepath"
)

// Keep the old lock as a migration guard: a pre-rename executable must not run
// against the same media/configuration while Asterodia is using the library.
func AcquireLibraryLock(directory string) (func(), error) {
	legacyUnlock, err := AcquireLock(filepath.Join(directory, "storagebucket.lock"))
	if err != nil {
		return nil, err
	}
	unlock, err := AcquireLock(filepath.Join(directory, "asterodia.lock"))
	if err != nil {
		legacyUnlock()
		return nil, err
	}
	return func() { unlock(); legacyUnlock() }, nil
}

func DataDirectoryOverride() string {
	if value := os.Getenv("ASTERODIA_DATA_DIR"); value != "" {
		return value
	}
	return os.Getenv("STORAGEBUCKET_DATA_DIR")
}
