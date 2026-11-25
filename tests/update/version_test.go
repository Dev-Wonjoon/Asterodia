package update_test

import (
	"runtime"
	"testing"

	appupdate "github.com/Dev-Wonjoon/StorageBucket/internal/update"
)

func TestReleaseVersionOrdering(t *testing.T) {
	versions := []string{"2.2.5", "2.2.6", "2.2.6a", "2.2.6b", "2.2.6z", "2.2.6.1", "2.2.6.1a", "2.2.6.1z", "2.2.6.2", "2.2.6.2a", "2.2.6.10", "2.2.6.2427", "2.2.6.2427f", "2.2.7", "2.10.0", "3.0.0", "65535.65535.65535"}
	for a, left := range versions {
		for b, right := range versions {
			want := 0
			if a < b {
				want = -1
			} else if a > b {
				want = 1
			}
			got, err := appupdate.CompareVersions(left, right)
			if err != nil || got != want {
				t.Fatalf("compare %s %s: got %d (%v), want %d", left, right, got, err, want)
			}
		}
	}
}

func TestExtendedVersionUpdateSupport(t *testing.T) {
	platformSupported := runtime.GOOS == "windows" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")
	for _, version := range []string{"2.2.6", "2.2.6a", "2.2.6.1", "2.2.7.2a", "2.2.6.2427f"} {
		service := appupdate.NewService(version, nil)
		status := service.Status().Data.(appupdate.Status)
		if status.Supported != platformSupported || status.CurrentVersion != version {
			t.Fatalf("version %s: %+v", version, status)
		}
	}
	if status := appupdate.NewService("2.2.6-beta", nil).Status().Data.(appupdate.Status); status.Supported {
		t.Fatal("unsupported version format enabled automatic updates")
	}
}

func TestReleaseVersionRejectsUnsupportedFormats(t *testing.T) {
	for _, version := range []string{"", "dev", "v2.2.6", "2.2", "02.2.6", "2.2.6.0", "2.2.6.01", "2.2.6.2428", "2.2.6.2427g", "2.2.6aa", "2.2.6A", "2.2.6.1aa", "2.2.6.0a", "2.2.6.01a", "2.2.6-beta.1", "2.2.6+build.1", "65536.0.0", "2.2.6\n2.2.7"} {
		if _, err := appupdate.CompareVersions(version, "2.2.6"); err == nil {
			t.Errorf("invalid version accepted: %q", version)
		}
		if _, err := appupdate.CompareVersions("2.2.6", version); err == nil {
			t.Errorf("invalid current version accepted: %q", version)
		}
	}
}
