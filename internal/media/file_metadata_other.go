//go:build !windows

package media

// Do not label modification time as creation time on unsupported filesystems.
func fileBirthTime(path string) *int64 { return nil }
