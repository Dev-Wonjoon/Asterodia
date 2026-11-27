package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/cookies"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

func managedStore(t *testing.T) (*cookies.Store, string) {
	t.Helper()
	dir, err := os.MkdirTemp(engineTestRoot(t, "engine-parsers"), "managed-cookies-")
	if err != nil {
		t.Fatal(err)
	}
	s, err := cookies.Open(filepath.Join(dir, "cookies.dat"))
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func managedImport(t *testing.T, s *cookies.Store, dir string) {
	t.Helper()
	filename := filepath.Join(dir, "source.txt")
	if err := os.WriteFile(filename, []byte("# Netscape HTTP Cookie File\n#HttpOnly_.berriz.in\tTRUE\t/\tTRUE\t0\tsession\toriginal-private\n.youtube.com\tTRUE\t/\tTRUE\t0\tunrelated\tmust-not-export\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.ImportFile(filename); err != nil {
		t.Fatal(err)
	}
}

// Deterministic subprocess for cookie import, scope and CLI write-back. It
// cannot reach a browser or a remote server, and only writes inside the test run.
func runManagedCookieFixture() bool {
	if os.Getenv("ASTERODIA_MANAGED_COOKIE_FIXTURE") != "1" || len(os.Args) < 2 || (os.Args[1] != "--ignore-config" && os.Args[1] != "--config-ignore") {
		return false
	}
	filename := ""
	browser := false
	for i, arg := range os.Args {
		if arg == "--cookies" && i+1 < len(os.Args) {
			filename = os.Args[i+1]
		}
		if arg == "--cookies-from-browser" {
			browser = true
		}
	}
	rel, err := filepath.Rel(os.Getenv("ASTERODIA_TEST_ROOT"), filename)
	if err != nil || !filepath.IsLocal(rel) || filename == "" {
		os.Exit(2)
	}
	if browser {
		body := "# Netscape HTTP Cookie File\n.berriz.in\tTRUE\t/\tTRUE\t0\tsession\tbrowser-current\n.youtube.com\tTRUE\t/\tTRUE\t0\tunrelated\tmust-not-export\n"
		if os.WriteFile(filename, []byte(body), 0600) != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	records, err := cookies.ReadFile(filename)
	if err != nil {
		os.Exit(2)
	}
	for _, r := range records {
		if cookies.Site(r.Domain) != "berriz.in" {
			fmt.Fprintln(os.Stderr, "unrelated cookie exported")
			os.Exit(2)
		}
	}
	for i := range records {
		records[i].Value = "updated-by-engine"
		records[i].HTTPOnly = false
	}
	records = append(records, cookies.Record{Domain: "evil.invalid", Path: "/", Name: "injected", Value: "must-not-import"})
	if os.WriteFile(filename, cookies.Encode(records), 0600) != nil {
		os.Exit(2)
	}
	if os.Args[1] == "--config-ignore" {
		fmt.Print(`[[2,{"category":"future","post_id":"123"}],[3,"https://cdn.invalid/file.jpg",{"media_id":"123","extension":"jpg"}]]`)
	} else {
		fmt.Print(`{"id":"123","extractor_key":"Generic","webpage_url":"https://berriz.in/page","format_id":"http","url":"https://cdn.invalid/file.mp4","ext":"mp4","vcodec":"h264","acodec":"aac"}`)
	}
	os.Exit(0)
	return true
}

func TestCookieManagedCLIExportsOnlySelectedSitesAndMerges(t *testing.T) {
	t.Setenv("ASTERODIA_MANAGED_COOKIE_FIXTURE", "1")
	for _, kind := range []string{"yt-dlp", "gallery-dl"} {
		t.Run(kind, func(t *testing.T) {
			s, dir := managedStore(t)
			managedImport(t, s, dir)
			work, err := os.MkdirTemp(dir, "job-")
			if err != nil {
				t.Fatal(err)
			}
			auth := &engine.ManagedAuth{Store: s, Browser: "firefox", Exporter: &engine.Ytdlp{Executable: "must-not-run.exe"}}
			if !auth.HasCookies(berrizOriginal) {
				t.Fatal("imported cookies not detected")
			}
			if kind == "yt-dlp" {
				_, err = (&engine.Ytdlp{Executable: os.Args[0], Auth: auth}).Discover(t.Context(), berrizOriginal, work)
			} else {
				_, err = (&engine.GalleryDL{Executable: os.Args[0], Auth: auth}).Discover(t.Context(), berrizOriginal, work)
			}
			if err != nil {
				t.Fatal(err)
			}
			records := s.Snapshot("berriz.in").Records
			if len(records) != 1 || records[0].Value != "updated-by-engine" || !records[0].HTTPOnly || !strings.HasPrefix(records[0].Source, "file:") {
				t.Fatal("CLI rotation lost metadata or source", records)
			}
			if len(s.Snapshot("evil.invalid").Records) != 0 || s.Snapshot("youtube.com").Records[0].Value != "must-not-export" {
				t.Fatal("unrelated credentials changed")
			}
			entries, err := os.ReadDir(work)
			if err != nil || len(entries) != 0 {
				t.Fatal("authentication file survived", entries, err)
			}
		})
	}
}

func TestCookieManagedBrowserRereadRespectsPartialDeletionAndSiteExclusion(t *testing.T) {
	t.Setenv("ASTERODIA_MANAGED_COOKIE_FIXTURE", "1")
	s, dir := managedStore(t)
	work, err := os.MkdirTemp(dir, "job-")
	if err != nil {
		t.Fatal(err)
	}
	auth := &engine.ManagedAuth{Store: s, Browser: "firefox", Exporter: &engine.Ytdlp{Executable: os.Args[0]}}
	y := &engine.Ytdlp{Auth: auth}
	address, _ := url.Parse("https://svc-api.berriz.in/service/resource")
	selected, err := y.Cookies(t.Context(), address, work)
	if err != nil || len(selected) != 1 || selected[0].Value != "browser-current" {
		t.Fatal(selected, err)
	}
	id := s.Snapshot("berriz.in").Records[0].ID()
	if err := s.Delete(id); err != nil {
		t.Fatal(err)
	}
	if _, err := y.Cookies(t.Context(), address, work); !errors.Is(err, engine.ErrAuthenticationRequired) {
		t.Fatal("browser reintroduced deleted cookie", err)
	}
	if err := s.SetEnabled("berriz.in", false); err != nil {
		t.Fatal(err)
	}
	auth.Exporter = &engine.Ytdlp{Executable: "must-not-run.exe"}
	if auth.HasCookies(berrizOriginal) {
		t.Fatal("excluded site still selects authenticated extraction")
	}
	if _, err := y.Cookies(context.Background(), address, work); !errors.Is(err, engine.ErrAuthenticationRequired) {
		t.Fatal(err)
	}
	groups, _ := json.Marshal(s.Groups())
	if strings.Contains(string(groups), "browser-current") {
		t.Fatal("cookie value reached UI metadata")
	}
}
