package update

import (
	"fmt"
	"regexp"
	"strconv"
)

// Version is injected from the root VERSION file by production builds.
var Version = "dev"

const Repository = "Dev-Wonjoon/StorageBucket"

var stableVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:\.([1-9][0-9]*)|([a-z]))?$`)

// Letters are post-release fixes, not prereleases. The final numeric component
// reserves 1..26 for a..z; numbered fixes use 27*n (n <= 2427).
func versionParts(value string) ([4]uint64, error) {
	var parts [4]uint64
	match := stableVersion.FindStringSubmatch(value)
	if match == nil {
		return parts, fmt.Errorf("invalid release version: %q", value)
	}
	for n := 0; n < 3; n++ {
		component, err := strconv.ParseUint(match[n+1], 10, 16)
		if err != nil {
			return parts, fmt.Errorf("invalid release version: %q", value)
		}
		parts[n] = component
	}
	if match[4] != "" {
		revision, err := strconv.ParseUint(match[4], 10, 16)
		if err != nil || revision > 65535/27 {
			return parts, fmt.Errorf("invalid release revision: %q", value)
		}
		parts[3] = revision * 27
	} else if match[5] != "" {
		parts[3] = uint64(match[5][0]-'a') + 1
	}
	return parts, nil
}

func validVersion(value string) bool {
	_, err := versionParts(value)
	return err == nil
}

// CompareVersions returns -1, 0, or 1 for supported release versions.
func CompareVersions(left, right string) (int, error) {
	a, err := versionParts(left)
	if err != nil {
		return 0, err
	}
	b, err := versionParts(right)
	if err != nil {
		return 0, err
	}
	for n := range a {
		if a[n] < b[n] {
			return -1, nil
		}
		if a[n] > b[n] {
			return 1, nil
		}
	}
	return 0, nil
}
