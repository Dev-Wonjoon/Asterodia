package update_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appupdate "github.com/Dev-Wonjoon/Asterodia/internal/update"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

func TestAssetSelection(t *testing.T) {
	assets := []github.ReleaseAsset{{Name: "StorageBucket-windows-amd64-installer.exe"}, {Name: "StorageBucket-windows-arm64.exe"}, {Name: "StorageBucket-windows-amd64.exe"}, {Name: "StorageBucket-windows-amd64.exe.sig"}}
	for _, tc := range []struct {
		platform, arch string
		want           int
	}{{"windows", "amd64", 2}, {"windows", "arm64", 1}, {"windows", "386", -1}, {"darwin", "arm64", -1}} {
		if got := appupdate.MatchAsset(updater.CheckRequest{Platform: tc.platform, Arch: tc.arch}, assets); got != tc.want {
			t.Fatalf("%s/%s: got %d", tc.platform, tc.arch, got)
		}
	}
	if appupdate.MatchAsset(updater.CheckRequest{Platform: "windows", Arch: "amd64"}, append(assets, assets[2])) != -1 {
		t.Fatal("ambiguous executable accepted")
	}
}

func TestAsterodiaAssetSelection(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			modern := github.ReleaseAsset{Name: "Asterodia-windows-" + arch + ".exe"}
			legacy := github.ReleaseAsset{Name: "StorageBucket-windows-" + arch + ".exe"}
			req := updater.CheckRequest{Platform: "windows", Arch: arch}
			for _, tc := range []struct {
				name   string
				assets []github.ReleaseAsset
				want   int
			}{
				{"new-only", []github.ReleaseAsset{modern}, 0},
				{"legacy-only", []github.ReleaseAsset{legacy}, 0},
				{"prefer-new", []github.ReleaseAsset{legacy, modern}, 1},
				{"prefer-new-reversed", []github.ReleaseAsset{modern, legacy}, 0},
				{"duplicate-new", []github.ReleaseAsset{modern, modern, legacy}, -1},
				{"duplicate-legacy", []github.ReleaseAsset{modern, legacy, legacy}, -1},
				{"installer-only", []github.ReleaseAsset{{Name: "Asterodia-windows-" + arch + "-installer.exe"}}, -1},
			} {
				if got := appupdate.MatchAsset(req, tc.assets); got != tc.want {
					t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
				}
			}
		})
	}
}

func TestAsterodiaReleaseChecksumAndDownload(t *testing.T) {
	payload := []byte("Asterodia executable")
	digest := sha256.Sum256(payload)
	for _, valid := range []bool{true, false} {
		sums := hex.EncodeToString(digest[:]) + "  StorageBucket-windows-amd64.exe\n"
		if valid {
			sums += hex.EncodeToString(digest[:]) + "  Asterodia-windows-amd64.exe\n"
		}
		provider := fixtureProvider(t, "v2.3.5", sums, payload, func(release map[string]any) {
			assets := release["assets"].([]map[string]any)
			modern := map[string]any{"id": 3, "name": "Asterodia-windows-amd64.exe", "size": len(payload), "browser_download_url": "https://updates.test/app"}
			release["assets"] = append(assets, modern)
		})
		release, err := provider.Check(t.Context(), updater.CheckRequest{CurrentVersion: "2.3.4", Platform: "windows", Arch: "amd64"})
		if !valid {
			if err == nil {
				t.Fatal("new asset accepted with only the legacy checksum")
			}
			continue
		}
		if err != nil || release == nil || release.Artifact.Filename != "Asterodia-windows-amd64.exe" {
			t.Fatalf("new release: %v, %v", release, err)
		}
		var output bytes.Buffer
		if err := provider.Download(t.Context(), release, &output, nil); err != nil || !bytes.Equal(output.Bytes(), payload) {
			t.Fatalf("new executable download: %v", err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixtureProvider(t *testing.T, version, sums string, payload []byte, configure ...func(map[string]any)) *appupdate.Provider {
	t.Helper()
	const baseURL = "https://updates.test"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/Dev-Wonjoon/Asterodia/releases/latest":
			if r.Header.Get("Authorization") != "" {
				t.Error("public update should not embed a token")
			}
			assets := []map[string]any{{"id": 1, "name": "StorageBucket-windows-amd64.exe", "size": len(payload), "browser_download_url": baseURL + "/app"}}
			if sums != "missing" {
				assets = append(assets, map[string]any{"id": 2, "name": "SHA256SUMS", "size": len(sums), "browser_download_url": baseURL + "/sums"})
			}
			release := map[string]any{"tag_name": version, "name": version, "body": "release notes", "prerelease": false, "draft": false, "assets": assets}
			for _, change := range configure {
				change(release)
			}
			_ = json.NewEncoder(w).Encode(release)
		case "/app":
			_, _ = w.Write(payload)
		case "/sums":
			_, _ = fmt.Fprint(w, sums)
		default:
			http.NotFound(w, r)
		}
	})
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		return recorder.Result(), nil
	})}
	p, err := appupdate.NewProvider(baseURL, client)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtendedReleaseUpdates(t *testing.T) {
	payload := []byte("hotfix executable")
	digest := sha256.Sum256(payload)
	sums := hex.EncodeToString(digest[:]) + "  StorageBucket-windows-amd64.exe\n"
	for _, pair := range [][2]string{{"2.2.5", "2.2.6"}, {"2.2.6", "2.2.6a"}, {"2.2.6a", "2.2.6b"}, {"2.2.6z", "2.2.6.1"}, {"2.2.6", "2.2.6.1"}, {"2.2.6.2", "2.2.6.10"}, {"2.2.6.2427", "2.2.7"}, {"2.2.7.2", "2.2.7.2a"}, {"2.2.7.2a", "2.2.7.2b"}, {"2.2.7.2z", "2.2.7.3"}} {
		provider := fixtureProvider(t, "v"+pair[1], sums, payload)
		release, err := provider.Check(t.Context(), updater.CheckRequest{CurrentVersion: pair[0], Platform: "windows", Arch: "amd64"})
		if err != nil || release == nil || release.Version != pair[1] || !bytes.Equal(release.Verification.Digest, digest[:]) {
			t.Fatalf("%s -> %s: release=%v error=%v", pair[0], pair[1], release, err)
		}
		var output bytes.Buffer
		if err = provider.Download(t.Context(), release, &output, nil); err != nil || !bytes.Equal(output.Bytes(), payload) {
			t.Fatalf("download %s: %v", pair[1], err)
		}
	}
	for _, pair := range [][2]string{{"2.2.6a", "2.2.6"}, {"2.2.6a", "2.2.6a"}, {"2.2.6.2", "2.2.6.1"}, {"2.2.7", "2.2.6.2427"}, {"2.2.7.2a", "2.2.7.2"}, {"2.2.7.2a", "2.2.7.2a"}, {"2.2.7.3", "2.2.7.2z"}} {
		provider := fixtureProvider(t, "v"+pair[1], "missing", payload)
		if release, err := provider.Check(t.Context(), updater.CheckRequest{CurrentVersion: pair[0], Platform: "windows", Arch: "amd64"}); err != nil || release != nil {
			t.Fatalf("downgrade/equal %s -> %s: %v %v", pair[0], pair[1], release, err)
		}
	}
	for _, flag := range []string{"draft", "prerelease"} {
		provider := fixtureProvider(t, "v2.2.6a", sums, payload, func(release map[string]any) { release[flag] = true })
		if release, err := provider.Check(t.Context(), updater.CheckRequest{CurrentVersion: "2.2.6", Platform: "windows", Arch: "amd64"}); err != nil || release != nil {
			t.Fatalf("unpublished release surfaced: %v %v", release, err)
		}
	}
	provider := fixtureProvider(t, "v2.2.6.1", sums+sums, payload)
	if _, err := provider.Check(t.Context(), updater.CheckRequest{CurrentVersion: "2.2.6", Platform: "windows", Arch: "amd64"}); err == nil {
		t.Fatal("ambiguous checksum accepted")
	}
}

func TestRequireChecksums(t *testing.T) {
	payload := []byte("release executable fixture")
	digest := sha256.Sum256(payload)
	valid := hex.EncodeToString(digest[:]) + "  StorageBucket-windows-amd64.exe\n"
	for _, tc := range []struct {
		name, sums string
		wantError  bool
	}{
		{"valid", valid, false}, {"absent-sidecar", "missing", true}, {"absent-entry", hex.EncodeToString(digest[:]) + "  installer.exe\n", true}, {"short-digest", "aabb  StorageBucket-windows-amd64.exe\n", true}, {"malformed", "xyz  StorageBucket-windows-amd64.exe\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := fixtureProvider(t, "v2.0.10", tc.sums, payload)
			r, err := p.Check(t.Context(), updater.CheckRequest{CurrentVersion: "2.0.9", Platform: "windows", Arch: "amd64"})
			if (err != nil) != tc.wantError {
				t.Fatalf("release=%v err=%v", r, err)
			}
			if !tc.wantError && (r == nil || r.Version != "2.0.10") {
				t.Fatalf("numeric version comparison failed: %v", r)
			}
		})
	}
	for _, version := range []string{"v2.0.0", "v2.0.1"} {
		p := fixtureProvider(t, version, "missing", payload)
		if r, err := p.Check(t.Context(), updater.CheckRequest{CurrentVersion: "2.0.1", Platform: "windows", Arch: "amd64"}); err != nil || r != nil {
			t.Fatalf("older/equal release: %v %v", r, err)
		}
	}
	p := fixtureProvider(t, "v3.0.0-beta.1", valid, payload)
	if _, err := p.Check(t.Context(), updater.CheckRequest{CurrentVersion: "2.0.1", Platform: "windows", Arch: "amd64"}); err == nil {
		t.Fatal("prerelease tag accepted on stable channel")
	}
}

type quietHost struct{}

func (quietHost) Emit(string, ...any) bool         { return true }
func (quietHost) OnEvent(string, func(any)) func() { return func() {} }
func (quietHost) OpenWindow(updater.WindowOptions) updater.WindowHandle {
	panic("test must not open update windows")
}
func (quietHost) Quit() { panic("test must not restart") }

func TestDownloadedBytesAreVerifiedBeforeReady(t *testing.T) {
	payload := []byte("downloaded executable")
	wrong := sha256.Sum256([]byte("different executable"))
	sums := hex.EncodeToString(wrong[:]) + "  StorageBucket-windows-amd64.exe\n"
	p := fixtureProvider(t, "v2.0.2", sums, payload)
	u := updater.New(quietHost{})
	if err := u.Init(updater.Config{CurrentVersion: "2.0.1", Platform: "windows", Arch: "amd64", Providers: []updater.Provider{p}, Window: updater.WindowNone}); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := u.DownloadAndInstall(t.Context()); err == nil {
		t.Fatal("tampered download accepted")
	}
	if u.State() == updater.StateReady || u.DownloadedPath() != "" {
		t.Fatal("invalid executable staged")
	}
}

func TestDownloadRejectsUnexpectedLength(t *testing.T) {
	payload := []byte("executable")
	digest := sha256.Sum256(payload)
	p := fixtureProvider(t, "v2.0.2", hex.EncodeToString(digest[:])+"  StorageBucket-windows-amd64.exe\n", payload)
	for _, size := range []int64{0, 1, int64(len(payload) + 1), 513 << 20} {
		release, err := p.Check(t.Context(), updater.CheckRequest{CurrentVersion: "2.0.1", Platform: "windows", Arch: "amd64"})
		if err != nil {
			t.Fatal(err)
		}
		release.Artifact.Size = size
		var output bytes.Buffer
		if err = p.Download(t.Context(), release, &output, nil); err == nil {
			t.Fatalf("accepted incorrect length: %d", size)
		}
	}
}

func TestValidDownloadStagesOnlyExecutable(t *testing.T) {
	payload := []byte("downloaded executable")
	digest := sha256.Sum256(payload)
	p := fixtureProvider(t, "v2.0.2", hex.EncodeToString(digest[:])+"  StorageBucket-windows-amd64.exe\n", payload)
	u := updater.New(quietHost{})
	if err := u.Init(updater.Config{CurrentVersion: "2.0.1", Platform: "windows", Arch: "amd64", Providers: []updater.Provider{p}, Window: updater.WindowNone}); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := u.DownloadAndInstall(t.Context()); err != nil {
		t.Fatal(err)
	}
	if u.State() != updater.StateReady {
		t.Fatal(u.State())
	}
	path := u.DownloadedPath()
	t.Cleanup(func() {
		dir := filepath.Dir(path)
		executable, err := os.Executable()
		if err != nil || filepath.Dir(dir) != filepath.Dir(executable) || !strings.HasPrefix(filepath.Base(dir), "wails-update-") {
			t.Errorf("unexpected updater staging directory: %s", dir)
			return
		}
		_ = os.RemoveAll(dir)
	})
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(payload) {
		t.Fatalf("%q %v", data, err)
	}
}
