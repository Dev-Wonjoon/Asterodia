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

	appupdate "github.com/Dev-Wonjoon/StorageBucket/internal/update"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixtureProvider(t *testing.T, version, sums string, payload []byte) *appupdate.Provider {
	t.Helper()
	const baseURL = "https://updates.test"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/Dev-Wonjoon/StorageBucket/releases/latest":
			if r.Header.Get("Authorization") != "" {
				t.Error("public update should not embed a token")
			}
			assets := []map[string]any{{"id": 1, "name": "StorageBucket-windows-amd64.exe", "size": len(payload), "browser_download_url": baseURL + "/app"}}
			if sums != "missing" {
				assets = append(assets, map[string]any{"id": 2, "name": "SHA256SUMS", "size": len(sums), "browser_download_url": baseURL + "/sums"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": version, "name": version, "body": "release notes", "prerelease": false, "draft": false, "assets": assets})
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
