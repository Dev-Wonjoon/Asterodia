package engine

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"golang.org/x/sys/windows"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

func TestEngineIntegrityRejectsChangedFilesBeforeExecution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine.exe")
	original := []byte("MZfixture")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(original)
	digest := hex.EncodeToString(h[:])
	if err := verifyEngineDigest(path, digest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("MZtampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if verifyEngineDigest(path, digest) == nil {
		t.Fatal("tampered binary accepted")
	}
	for name, pin := range enginePins {
		if len(pin.BinarySHA) != 64 || len(pin.ArchiveSHA) != 64 || strings.Contains(pin.URL, "/latest/") {
			t.Fatalf("unfixed engine definition: %s", name)
		}
	}
}

func TestManagerReportsUnverifiedBinariesWithoutVersionProbe(t *testing.T) {
	root := t.TempDir()
	for _, definition := range binaryDefinitions() {
		path := filepath.Join(root, definition.filename)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("MZunverified"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	manager, err := NewManager(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	status, err := manager.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for name, state := range status {
		if state.Installed || !strings.Contains(state.Error, "SHA-256") {
			t.Fatalf("unverified %s reached version probing: %+v", name, state)
		}
	}
}

func TestPinnedOfficialArtifactsMatchBeforeVersionProbe(t *testing.T) {
	root := os.Getenv("ASTERODIA_SECURITY_ENGINE_ROOT")
	if root == "" {
		t.Skip("verified official artifacts not configured")
	}
	for _, name := range []string{"ffmpeg", "ffprobe", "imagemagick", "yt-dlp", "gallery-dl"} {
		t.Run(name, func(t *testing.T) {
			definition, err := lookupBinary(name)
			if err != nil {
				t.Fatal(err)
			}
			pin := enginePins[name]
			target := filepath.Join(t.TempDir(), definition.filename)
			if definition.archive {
				archive := filepath.Join(root, "ffmpeg.zip")
				if name == "imagemagick" {
					archive = filepath.Join(root, "imagemagick.7z")
				}
				if err = verifyEngineDigest(archive, pin.ArchiveSHA); err != nil {
					t.Fatal(err)
				}
				if name == "imagemagick" {
					err = extractImageEngine(t.Context(), archive, filepath.Dir(target), pin.ArchiveSHA)
				} else {
					err = extractEngineBinary(archive, target, definition.filename)
				}
				if err != nil {
					t.Fatal(err)
				}
			} else {
				target = filepath.Join(root, definition.filename)
			}
			if err = verifyEngineDigest(target, pin.BinarySHA); err != nil {
				t.Fatal(err)
			}
			if _, err = new(ProcessRunner).ReadVersion(t.Context(), name, target); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEngineJobHasMemoryAndTerminationLimits(t *testing.T) {
	job, err := newWindowsJob()
	if err != nil {
		t.Fatal(err)
	}
	defer job.close()
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if err = windows.QueryInformationJobObject(job.handle, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)), nil); err != nil {
		t.Fatal(err)
	}
	if limits.JobMemoryLimit != 2<<30 || limits.BasicLimitInformation.LimitFlags&windows.JOB_OBJECT_LIMIT_JOB_MEMORY == 0 || limits.BasicLimitInformation.LimitFlags&windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE == 0 {
		t.Fatal("engine job has incomplete security limits")
	}
}

func TestOfficialEnginesUseConfiguredProxy(t *testing.T) {
	t.Setenv("NO_PROXY", "*")
	root := os.Getenv("ASTERODIA_SECURITY_ENGINE_ROOT")
	if root == "" {
		t.Skip("verified official test binaries not configured")
	}
	for _, name := range []string{"yt-dlp", "gallery-dl"} {
		t.Run(name, func(t *testing.T) {
			binary := filepath.Join(root, name+".exe")
			if err := verifyEngineDigest(binary, enginePins[name].BinarySHA); err != nil {
				t.Fatal(err)
			}
			var hits atomic.Int32
			expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("engine:fixture-secret"))
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Proxy-Authorization") != expectedAuth {
					http.Error(w, "Proxy authentication required", 407)
					return
				}
				hits.Add(1)
				http.Error(w, "Denied by test proxy", 403)
			}))
			defer proxy.Close()
			ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), networkKey{}, strings.Replace(proxy.URL, "http://", "http://engine:fixture-secret@", 1)), 20*time.Second)
			defer cancel()
			var err error
			if name == "yt-dlp" {
				_, err = (&Ytdlp{Executable: binary, NoPlaylist: true}).Discover(ctx, "https://www.youtube.com/watch?v=BaW_jenozKc", t.TempDir())
			} else {
				_, err = (&GalleryDL{Executable: binary}).discoverGalleryDL(ctx, "https://www.instagram.com/p/LocalFixture/", t.TempDir())
			}
			if err == nil || hits.Load() == 0 {
				t.Fatalf("engine did not use configured proxy: requests=%d, err=%v", hits.Load(), err)
			}
		})
	}
}

func TestOfficialYtdlpDoesNotBypassProxyForNonHTTPRedirect(t *testing.T) {
	t.Setenv("NO_PROXY", "*")
	root := os.Getenv("ASTERODIA_SECURITY_ENGINE_ROOT")
	if root == "" {
		t.Skip("verified official test binary not configured")
	}
	binary := filepath.Join(root, "yt-dlp.exe")
	if err := verifyEngineDigest(binary, enginePins["yt-dlp"].BinarySHA); err != nil {
		t.Fatal(err)
	}
	private, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer private.Close()
	var privateHits, proxyHits atomic.Int32
	go func() {
		conn, err := private.Accept()
		if err == nil {
			privateHits.Add(1)
			conn.Close()
		}
	}()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		if r.URL.Scheme == "http" {
			http.Redirect(w, r, "ftp://"+private.Addr().String()+"/private", 302)
		} else {
			http.Error(w, "Unsupported protocol", 403)
		}
	}))
	defer proxy.Close()
	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), networkKey{}, proxy.URL), 15*time.Second)
	defer cancel()
	_, err = (&Ytdlp{Executable: binary, NoPlaylist: true}).Discover(ctx, "http://public.example/media", t.TempDir())
	if err == nil || proxyHits.Load() == 0 || privateHits.Load() != 0 {
		t.Fatalf("non-HTTP redirect escaped proxy: proxy=%d private=%d err=%v", proxyHits.Load(), privateHits.Load(), err)
	}
}

func TestSecureEnvironmentCannotBypassDestinationPolicy(t *testing.T) {
	values := secureEnvironment([]string{"PATH=keep", "SystemRoot=C:\\Windows", "http_proxy=http://untrusted", "HTTPS_PROXY=http://other", "No_PrOxY=*", "ALL_PROXY=socks5://other"}, "http://engine:secret@127.0.0.1:1234")
	seen := map[string]string{}
	for _, entry := range values {
		key, value, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if _, exists := seen[key]; exists {
			t.Fatal("duplicate environment key", key)
		}
		seen[key] = value
	}
	if seen["NO_PROXY"] != "" || seen["PATH"] != "keep" || seen["HTTP_PROXY"] != seen["HTTPS_PROXY"] || seen["ALL_PROXY"] != seen["HTTPS_PROXY"] || !strings.Contains(seen["HTTP_PROXY"], "engine:secret") {
		t.Fatal("unsafe engine environment")
	}
}
