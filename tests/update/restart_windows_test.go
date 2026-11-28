package update_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/desktop"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

// The copied test executable acts as both the old app and the new app. Its
// helper entry mirrors main.go: helper mode precedes the single-instance lock.
func init() {
	root := os.Getenv("ASTERODIA_UPDATE_PROBE_ROOT")
	if root == "" {
		return
	}
	updater.HandleHelperMode()
	if relaunched, err := desktop.RelaunchAsterodia(); err != nil {
		_ = os.WriteFile(filepath.Join(root, "probe-error.txt"), []byte(err.Error()), 0600)
		os.Exit(1)
	} else if relaunched {
		os.Exit(0)
	}
	if err := runUpdateProbe(root); err != nil {
		_ = os.WriteFile(filepath.Join(root, "probe-error.txt"), []byte(err.Error()), 0600)
		os.Exit(1)
	}
	os.Exit(0)
}

type probeHost struct{ quietHost }

func (probeHost) Quit() { os.Exit(0) }

type probeProvider struct{ bytes []byte }

func (p probeProvider) Name() string { return "isolated-test" }
func (p probeProvider) Check(context.Context, updater.CheckRequest) (*updater.Release, error) {
	digest := sha256.Sum256(p.bytes)
	return &updater.Release{Version: "2.0.2", Artifact: updater.Artifact{Filename: "StorageBucket-windows-amd64.exe", Filetype: "exe", Size: int64(len(p.bytes))}, Verification: &updater.Verification{DigestAlgo: "sha256", Digest: digest[:]}}, nil
}
func (p probeProvider) Download(_ context.Context, _ *updater.Release, dst io.Writer, progress func(int64, int64)) error {
	n, err := dst.Write(p.bytes)
	if progress != nil {
		progress(int64(n), int64(len(p.bytes)))
	}
	return err
}

func runUpdateProbe(root string) error {
	if err := os.MkdirAll(filepath.Join(root, "data"), 0700); err != nil {
		return err
	}
	unlock, err := desktop.AcquireLibraryLock(filepath.Join(root, "data"))
	if err != nil {
		return err
	}
	defer unlock()
	marker := filepath.Join(root, "swap-started")
	if _, err = os.Stat(marker); err == nil {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, "restarted"), []byte(executable), 0600)
	}
	arguments, err := json.Marshal(os.Args[1:])
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(root, "launch-args.json"), arguments, 0600); err != nil {
		return err
	}
	bytes, err := os.ReadFile(filepath.Join(root, "payload.exe"))
	if err != nil {
		return err
	}
	u := updater.New(probeHost{})
	if err = u.Init(updater.Config{CurrentVersion: "2.0.1", Providers: []updater.Provider{probeProvider{bytes}}, Window: updater.WindowNone}); err != nil {
		return err
	}
	if _, err = u.Check(context.Background()); err != nil {
		return err
	}
	if err = u.DownloadAndInstall(context.Background()); err != nil {
		return err
	}
	if err = os.WriteFile(marker, []byte("pending"), 0600); err != nil {
		return err
	}
	return u.Restart(context.Background())
}

func TestWindowsExecutableSwapAndRelaunchPreservesData(t *testing.T) {
	for _, name := range []string{"Asterodia.exe", "StorageBucket.exe", "StorageBucket-windows-amd64.exe"} {
		t.Run(name, func(t *testing.T) { testWindowsExecutableSwapAndRelaunch(t, name) })
	}
}

func testWindowsExecutableSwapAndRelaunch(t *testing.T, entryName string) {
	root := filepath.Join(t.TempDir(), "portable library")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	updated := append(append([]byte(nil), original...), []byte("\nAsterodia update probe\n")...)
	target := filepath.Join(root, "Asterodia.exe")
	for file, data := range map[string][]byte{entryName: original, "payload.exe": updated, "asterodia.db": []byte("existing library"), "config.json": []byte("existing settings")} {
		if err = os.WriteFile(filepath.Join(root, file), data, 0700); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(root, entryName), "--launch-probe", "value with spaces")
	cmd.Env = append(os.Environ(), "ASTERODIA_UPDATE_PROBE_ROOT="+root, "ASTERODIA_DATA_DIR="+root)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	if output, err := cmd.CombinedOutput(); err != nil {
		failure, _ := os.ReadFile(filepath.Join(root, "probe-error.txt"))
		t.Fatalf("old app failed: %v %s %s", err, output, failure)
	}
	for {
		if _, err = os.Stat(filepath.Join(root, "restarted")); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			failure, _ := os.ReadFile(filepath.Join(root, "probe-error.txt"))
			t.Fatalf("new app did not relaunch: %s", failure)
		case <-time.After(25 * time.Millisecond):
		}
	}
	restarted, _ := os.ReadFile(filepath.Join(root, "restarted"))
	if string(restarted) != target {
		t.Fatalf("restart retained the legacy executable name: %s", restarted)
	}
	arguments, _ := os.ReadFile(filepath.Join(root, "launch-args.json"))
	if string(arguments) != `["--launch-probe","value with spaces"]` {
		t.Fatalf("legacy launch lost arguments: %s", arguments)
	}
	actual, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(actual) != sha256.Sum256(updated) {
		t.Fatal("executable was not replaced")
	}
	for file, want := range map[string]string{"asterodia.db": "existing library", "config.json": "existing settings"} {
		actual, err := os.ReadFile(filepath.Join(root, file))
		if err != nil || string(actual) != want {
			t.Fatal(fmt.Sprintf("user data changed: %s: %q %v", file, actual, err))
		}
	}
}
