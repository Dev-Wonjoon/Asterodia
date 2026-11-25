package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func init() {
	if os.Getenv("STORAGEBUCKET_TEST_IMAGE_MISSING_DLL_CHILD") == "1" {
		code := uint32(0xc0000135)
		os.Exit(int(code))
	}
}

func TestImageRuntimeMissingDLLExitExplainsExecutionFailure(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows loader exit status")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("STORAGEBUCKET_TEST_IMAGE_MISSING_DLL_CHILD", "1")
	_, err = new(ProcessRunner).ReadVersion(t.Context(), "imagemagick", executable)
	if err == nil || !strings.Contains(err.Error(), "실행 검증 실패") || !strings.Contains(err.Error(), "DLL") || !strings.Contains(err.Error(), "0xc0000135") {
		t.Fatal("missing DLL was not explained as an execution verification failure", err)
	}
}

func runtimeDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func runtimeArchive(t *testing.T, entries []string, dll []byte, mode os.FileMode) (string, enginePin) {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range entries {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(mode)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write(dll); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "runtime.vsix")
	if err := os.WriteFile(file, buffer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return file, enginePin{ArchiveSHA: runtimeDigest(buffer.Bytes()), BinarySHA: runtimeDigest(dll)}
}

func TestImageRuntimeExtractsOnlyPinnedReleaseDLL(t *testing.T) {
	archive, pin := runtimeArchive(t, []string{imageRuntimeEntry, "../outside.dll", "debug_nonredist/vcomp140d.dll"}, []byte("verified-release-dll"), 0600)
	destination := t.TempDir()
	if err := extractImageRuntime(t.Context(), archive, destination, pin); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 1 || entries[0].Name() != imageRuntimeName {
		t.Fatal("unexpected VSIX files extracted", entries, err)
	}
	if err := verifyEngineDigest(filepath.Join(destination, imageRuntimeName), pin.BinarySHA); err != nil {
		t.Fatal(err)
	}
}

func TestImageRuntimeRejectsUntrustedPayloadWithoutReplacingOldDLL(t *testing.T) {
	for _, mode := range []string{"archive-checksum", "dll-checksum", "missing-release", "duplicate", "symlink", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			entries, fileMode := []string{imageRuntimeEntry}, os.FileMode(0600)
			if mode == "missing-release" {
				entries = []string{"debug_nonredist/vcomp140d.dll"}
			}
			if mode == "duplicate" {
				entries = append(entries, imageRuntimeEntry)
			}
			if mode == "symlink" {
				fileMode |= os.ModeSymlink
			}
			archive, pin := runtimeArchive(t, entries, []byte("new-dll"), fileMode)
			if mode == "archive-checksum" {
				pin.ArchiveSHA = runtimeDigest([]byte("different-package"))
			}
			if mode == "dll-checksum" {
				pin.BinarySHA = runtimeDigest([]byte("different-dll"))
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			directory := t.TempDir()
			target := filepath.Join(directory, imageRuntimeName)
			if err := os.WriteFile(target, []byte("old-working-dll"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := extractImageRuntime(ctx, archive, directory, pin); err == nil {
				t.Fatal("untrusted runtime accepted")
			}
			got, err := os.ReadFile(target)
			if err != nil || string(got) != "old-working-dll" {
				t.Fatal("previous DLL was replaced", string(got), err)
			}
			files, err := os.ReadDir(directory)
			if err != nil || len(files) != 1 {
				t.Fatal("temporary extraction files leaked", files, err)
			}
		})
	}
}

func TestImageRuntimeIntegrityAlsoProtectsInstallProbes(t *testing.T) {
	root := t.TempDir()
	integrityRoot.RLock()
	previousRoot := integrityRoot.path
	integrityRoot.RUnlock()
	previousImage, previousRuntime := enginePins["imagemagick"], imageRuntimePin
	t.Cleanup(func() {
		enginePins["imagemagick"], imageRuntimePin = previousImage, previousRuntime
		integrityRoot.Lock()
		integrityRoot.path = previousRoot
		integrityRoot.Unlock()
	})
	ConfigureIntegrityRoot(root)
	imagePin := previousImage
	imagePin.BinarySHA = runtimeDigest([]byte("trusted-exe"))
	enginePins["imagemagick"] = imagePin
	imageRuntimePin.BinarySHA = runtimeDigest([]byte("trusted-dll"))
	for _, folder := range []string{"", ".install-imagemagick-fixture"} {
		directory := filepath.Join(root, folder, "imagemagick")
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		executable := filepath.Join(directory, "magick.exe")
		if err := os.WriteFile(executable, []byte("trusted-exe"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := verifyManagedExecutable(executable); err == nil || !strings.Contains(err.Error(), "런타임") {
			t.Fatal("missing runtime accepted", err)
		}
		if err := os.WriteFile(filepath.Join(directory, imageRuntimeName), []byte("trusted-dll"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := verifyManagedExecutable(executable); err != nil {
			t.Fatal("verified installation probe rejected", err)
		}
		if err := os.WriteFile(filepath.Join(directory, imageRuntimeName), []byte("tampered-dll"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := verifyManagedExecutable(executable); err == nil {
			t.Fatal("tampered local DLL accepted")
		}
	}
}
