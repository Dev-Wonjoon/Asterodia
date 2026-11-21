package engine_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

type noticeTransport func(*http.Request) (*http.Response, error)

func (f noticeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEngineNoticesInstallAndFailures(t *testing.T) {
	root := engineTestRoot(t, "engine-notices")
	source := filepath.Join(root, "fixture.go")
	executable := filepath.Join(root, "fixture.exe")
	if err := os.WriteFile(source, []byte(`package main
import ("fmt"; "os"; "strings")
func main() { name := "ffmpeg"; if strings.Contains(os.Args[0], "ffprobe") { name = "ffprobe" }; fmt.Println(name + " version notice-fixture") }
`), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "build", "-o", executable, source)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("compile isolated fixture: %v %s", err, output)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	type archiveEntry struct {
		name, text string
		symlink    bool
	}
	makeArchive := func(entries ...archiveEntry) []byte {
		t.Helper()
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		for _, entry := range append([]archiveEntry{{name: "bundle/bin/ffmpeg.exe", text: string(binary)}, {name: "bundle/bin/ffprobe.exe", text: string(binary)}}, entries...) {
			header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
			if entry.symlink {
				header.SetMode(os.ModeSymlink | 0700)
			}
			file, err := writer.CreateHeader(header)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(file, entry.text); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes()
	}
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	var payload []byte
	requests := 0
	http.DefaultTransport = noticeTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Scheme != "https" || r.URL.Host != "github.com" {
			return nil, fmt.Errorf("unexpected network request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(payload)), ContentLength: int64(len(payload)), Header: make(http.Header)}, nil
	})
	original := "Copyright Example\r\nOriginal license terms\r\n"
	payload = makeArchive(archiveEntry{name: "bundle/LICENSE.txt", text: original}, archiveEntry{name: "bundle/licenses/component.txt", text: "Additional original notice"})
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		t.Run(name, func(t *testing.T) {
			bin, err := os.MkdirTemp(root, "success-")
			if err != nil {
				t.Fatal(err)
			}
			manager, err := engine.NewManager(bin, nil)
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := manager.Install(t.Context(), name, "fixture"); err != nil || !ok {
				t.Fatalf("install: %v", err)
			}
			files, err := filepath.Glob(filepath.Join(bin, "licenses", name+"-*", "notices", "archive-001.txt"))
			if err != nil || len(files) != 1 {
				t.Fatalf("original notices missing: %v %v", files, err)
			}
			data, err := os.ReadFile(files[0])
			if err != nil || string(data) != original {
				t.Fatal("original license bytes changed")
			}
			dir := filepath.Dir(files[0])
			additional, err := os.ReadFile(filepath.Join(dir, "archive-002.txt"))
			if err != nil || string(additional) != "Additional original notice" {
				t.Fatal("nested component notice was discarded")
			}
			metadata, err := os.ReadFile(filepath.Join(dir, "installed-build.json"))
			if err != nil {
				t.Fatal(err)
			}
			var info map[string]string
			if err := json.Unmarshal(metadata, &info); err != nil {
				t.Fatal(err)
			}
			if info["BinarySHA256"] != fmt.Sprintf("%x", sha256.Sum256(binary)) || !strings.Contains(info["Version"], "notice-fixture") {
				t.Fatalf("incorrect build attribution: %s", metadata)
			}
			if _, err := os.Stat(filepath.Join(dir, "reference-notices.txt")); err != nil {
				t.Fatal(err)
			}
			before := requests
			if ok, err := manager.Install(t.Context(), name, "reuse"); err != nil || !ok || requests != before {
				t.Fatal("valid installation was unnecessarily replaced", err)
			}
		})
	}
	for _, scenario := range []struct {
		name    string
		entries []archiveEntry
	}{
		{"missing", nil}, {"empty", []archiveEntry{{name: "bundle/LICENSE.txt"}}},
		{"traversal", []archiveEntry{{name: "../LICENSE.txt", text: "unsafe"}}},
		{"symlink", []archiveEntry{{name: "bundle/LICENSE.txt", text: "link", symlink: true}}},
		{"oversized", []archiveEntry{{name: "bundle/NOTICE.txt", text: strings.Repeat("x", (2<<20)+1)}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			payload = makeArchive(scenario.entries...)
			bin, err := os.MkdirTemp(root, "failure-")
			if err != nil {
				t.Fatal(err)
			}
			old := []byte("MZexisting invalid fixture")
			if err := os.WriteFile(filepath.Join(bin, "ffmpeg.exe"), old, 0600); err != nil {
				t.Fatal(err)
			}
			manager, err := engine.NewManager(bin, nil)
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := manager.Install(t.Context(), "ffmpeg", "failure"); err == nil || ok {
				t.Fatal("invalid archive accepted")
			}
			data, err := os.ReadFile(filepath.Join(bin, "ffmpeg.exe"))
			if err != nil || !bytes.Equal(data, old) {
				t.Fatal("failed extraction changed existing engine")
			}
			staging, _ := filepath.Glob(filepath.Join(bin, ".install-*"))
			if len(staging) != 0 {
				t.Fatal("temporary installation was not cleaned up")
			}
		})
	}
}
