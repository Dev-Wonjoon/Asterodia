package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyExecutableNames(t *testing.T) {
	for _, name := range []string{"StorageBucket.exe", "STORAGEBUCKET.EXE", "StorageBucket-windows-amd64.exe", "StorageBucket-windows-arm64.exe"} {
		if !legacyExecutableName(name) {
			t.Fatal(name)
		}
	}
	for _, name := range []string{"Asterodia.exe", "Asterodia-windows-amd64.exe", "StorageBucket.exe.bak", "other.exe"} {
		if legacyExecutableName(name) {
			t.Fatal(name)
		}
	}
}

func TestCanonicalExecutablePreservesVersionsAndRollsBack(t *testing.T) {
	for _, test := range []struct {
		name, product string
		version       uint64
		wantSource    bool
		wantError     bool
	}{{"older", "Asterodia", 9, true, false}, {"same version", "Asterodia", 10, false, false}, {"newer", "Asterodia", 11, false, false}, {"unrelated", "Other app", 9, false, true}} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source, target := filepath.Join(root, "StorageBucket.exe"), filepath.Join(root, "Asterodia.exe")
			for name, value := range map[string]string{source: "new executable", target: "existing executable", filepath.Join(root, "config.json"): "settings", filepath.Join(root, "asterodia.db"): "library"} {
				if err := os.WriteFile(name, []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			identify := func(path string) (executableInfo, error) {
				if path == source {
					return executableInfo{"Asterodia", "Dev-Wonjoon", 10}, nil
				}
				return executableInfo{test.product, "Dev-Wonjoon", test.version}, nil
			}
			rollback, _, err := prepareCanonicalExecutable(source, target, identify)
			if (err != nil) != test.wantError {
				t.Fatal(err)
			}
			actual, _ := os.ReadFile(target)
			want := "existing executable"
			if test.wantSource {
				want = "new executable"
			}
			if string(actual) != want {
				t.Fatal("wrong executable selected", string(actual))
			}
			if err == nil {
				if err = rollback(); err != nil {
					t.Fatal(err)
				}
			}
			for name, want := range map[string]string{source: "new executable", target: "existing executable", filepath.Join(root, "config.json"): "settings", filepath.Join(root, "asterodia.db"): "library"} {
				actual, err := os.ReadFile(name)
				if err != nil || string(actual) != want {
					t.Fatal("rollback or migration changed existing files", name, string(actual), err)
				}
			}
			leftovers, _ := filepath.Glob(filepath.Join(root, ".asterodia-launch-*.exe"))
			if len(leftovers) != 0 {
				t.Fatal("temporary executable leaked", leftovers)
			}
		})
	}
}

func TestCanonicalExecutableFirstLaunchAndCommit(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "StorageBucket.exe"), filepath.Join(root, "Asterodia.exe")
	if err := os.WriteFile(source, []byte("running executable"), 0600); err != nil {
		t.Fatal(err)
	}
	unknown := func(string) (executableInfo, error) { return executableInfo{}, errors.New("missing version") }
	rollback, _, err := prepareCanonicalExecutable(source, target, unknown)
	if err != nil {
		t.Fatal(err)
	}
	if same, err := sameExecutable(source, target); !same || err != nil {
		t.Fatal(same, err)
	}
	if err = rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("first-launch rollback left a target", err)
	}
	_, commit, err := prepareCanonicalExecutable(source, target, unknown)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	// A second launch through the old shortcut needs no copy or version read.
	if _, _, err = prepareCanonicalExecutable(source, target, unknown); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, []byte("unknown executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = prepareCanonicalExecutable(source, target, unknown); err == nil {
		t.Fatal("unverifiable executable was overwritten")
	}
}

func TestBuiltExecutableIdentity(t *testing.T) {
	path := os.Getenv("ASTERODIA_IDENTITY_FIXTURE")
	if path == "" {
		t.Skip("native build verification supplies an executable fixture")
	}
	info, err := executableIdentity(path)
	if err != nil || info.product != "Asterodia" || info.company != "Dev-Wonjoon" || info.version == 0 {
		t.Fatal(info, err)
	}
}
