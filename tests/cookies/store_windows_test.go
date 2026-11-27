package cookies_test

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/cookies"
)

func cookieRoot(t *testing.T) string {
	t.Helper()
	root := os.Getenv("ASTERODIA_TEST_ROOT")
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(filepath.Join(repo, ".test-data", "cookie-storage"), root)
	if !filepath.IsAbs(root) || err != nil || rel == "." || !filepath.IsLocal(rel) {
		t.Fatal("cookie tests need an isolated .test-data/cookie-storage run directory")
	}
	dir, err := os.MkdirTemp(root, "store-")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
func importFile(t *testing.T, s *cookies.Store, dir, body string) {
	t.Helper()
	path := filepath.Join(dir, "source.txt")
	if err := os.WriteFile(path, []byte("# Netscape HTTP Cookie File\n"+body), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.ImportFile(path); err != nil {
		t.Fatal(err)
	}
}
func openStore(t *testing.T, dir string) *cookies.Store {
	t.Helper()
	s, err := cookies.Open(filepath.Join(dir, "cookies.dat"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCookieParseRoundTripAndScope(t *testing.T) {
	body := []byte("# Netscape HTTP Cookie File\r\n#HttpOnly_.berriz.in\tTRUE\t/\tTRUE\t\tsession\tsecret-one\r\nsvc-api.berriz.in\tFALSE\t/service\tTRUE\t4102444800.5\tsession\tsecret-two\r\nberriz.in\tFALSE\t/\tTRUE\t0\thost-only\tnot-for-api\r\n.berriz.in\tTRUE\t/service-other\tTRUE\t0\tpath-only\tnot-for-api\r\n.berriz.in\tTRUE\t/\tTRUE\t1\texpired\tnot-for-api\r\n")
	records, err := cookies.Parse(body)
	if err != nil || len(records) != 5 {
		t.Fatal(records, err)
	}
	round, err := cookies.Parse(cookies.Encode(records))
	if err != nil || len(round) != len(records) {
		t.Fatal(round, err)
	}
	for i, r := range round {
		if r != records[i] {
			t.Fatal("cookie attributes changed", r, records[i])
		}
	}
	address, _ := url.Parse("https://svc-api.berriz.in/service/v1/resource")
	selected := cookies.ForURL(records, address)
	if len(selected) != 2 || selected[0].Value != "secret-two" || selected[1].Value != "secret-one" {
		t.Fatal(selected)
	}
	address.Scheme = "http"
	if len(cookies.ForURL(records, address)) != 0 {
		t.Fatal("secure cookies sent over HTTP")
	}
	for _, bad := range []string{"{}", "# Netscape HTTP Cookie File\n.evil.invalid\tTRUE\t/\tTRUE\t0\tbad\x00name\tsecret\n", "# Netscape HTTP Cookie File\n.berriz.in\tTRUE\t/\tTRUE\tNaN\tx\tsecret\n"} {
		if _, err := cookies.Parse([]byte(bad)); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid input accepted or value leaked", err)
		}
	}
	if cookies.Site("sub.example.co.uk") != "example.co.uk" || cookies.Site("https://svc-api.berriz.in/path") != "berriz.in" {
		t.Fatal("site grouping ignored public suffixes")
	}
}

func TestCookieNetscapeValuesArePreservedWithoutHTTPHeaderValidation(t *testing.T) {
	for _, entry := range []struct{ name, value string }{
		{"quoted", `"quoted-token"`},
		{"json", `{"consent":"yes","path":"a\\b"}`},
		{"brackets[key]", "plain"},
		{"legacy name", "plain"},
		{"unicode", "합성 테스트"},
		{"semicolon", "one;two"},
		{"", `"bare-token"`},
	} {
		t.Run(entry.name, func(t *testing.T) {
			body := []byte("# Netscape HTTP Cookie File\n.example.com\tTRUE\t/\tTRUE\t0\t" + entry.name + "\t" + entry.value + "\n")
			records, err := cookies.Parse(body)
			if err != nil || len(records) != 1 {
				t.Fatal("Netscape entry rejected", err)
			}
			round, err := cookies.Parse(cookies.Encode(records))
			if err != nil || len(round) != 1 || round[0].Name != entry.name || round[0].Value != entry.value {
				t.Fatal("Netscape round trip changed the cookie")
			}
			address, _ := url.Parse("https://example.com/")
			selected := cookies.ForURL(records, address)
			if entry.name == "quoted" {
				if len(selected) != 1 || selected[0].String() != `quoted="quoted-token"; Path=/` {
					t.Fatal("native HTTP request lost outer quotes")
				}
			} else if len(selected) != 0 {
				t.Fatal("native HTTP serialization accepted an incompatible entry")
			}
		})
	}
	for _, value := range []string{"one\x00two", "one\x01two", "one\x7ftwo"} {
		if _, err := cookies.Parse([]byte("# Netscape HTTP Cookie File\n.example.com\tTRUE\t/\tTRUE\t0\tname\t" + value + "\n")); err == nil {
			t.Fatal("control character accepted")
		}
	}
}

func TestCookieImportPersistenceAndMetadataPrivacy(t *testing.T) {
	dir := cookieRoot(t)
	s := openStore(t, dir)
	importFile(t, s, dir, ".berriz.in\tTRUE\t/\tTRUE\t0\tsession\tvery-private-value\n.youtube.com\tTRUE\t/\tTRUE\t0\tyoutube\tkeep-youtube\n")
	importFile(t, s, dir, ".berriz.in\tTRUE\t/\tTRUE\t0\tsession\tupdated-private-value\n")
	if groups := s.Groups(); len(groups) != 2 {
		t.Fatal("import replaced an unrelated site", groups)
	}
	metadata, _ := json.Marshal(s.Groups())
	sealed, err := os.ReadFile(filepath.Join(dir, "cookies.dat"))
	if err != nil || bytes.Contains(sealed, []byte("updated-private-value")) || bytes.Contains(metadata, []byte("updated-private-value")) || bytes.Contains(metadata, []byte(`"value"`)) {
		t.Fatal("cookie values exposed", err)
	}
	again := openStore(t, dir)
	if !again.Imported() || again.Snapshot("berriz.in").Records[0].Value != "updated-private-value" {
		t.Fatal("cookies did not persist")
	}
	if err := again.DeleteSite("berriz.in"); err != nil {
		t.Fatal(err)
	}
	if again.Snapshot("berriz.in").Enabled || len(again.Snapshot("youtube.com").Records) != 1 {
		t.Fatal("site deletion changed unrelated authentication")
	}
	unchanged, err := os.ReadFile(filepath.Join(dir, "source.txt"))
	if err != nil || !bytes.Contains(unchanged, []byte("updated-private-value")) {
		t.Fatal("source file was modified", err)
	}
	if openStore(t, dir).Snapshot("berriz.in").Enabled {
		t.Fatal("site exclusion did not persist")
	}
}

func TestCookieDeletionAndImportInvalidateRunningTasks(t *testing.T) {
	for _, mode := range []string{"entry", "site", "disable", "import", "newer-task"} {
		t.Run(mode, func(t *testing.T) {
			dir := cookieRoot(t)
			s := openStore(t, dir)
			importFile(t, s, dir, ".berriz.in\tTRUE\t/\tTRUE\t0\tsession\toriginal\n")
			snap := s.Snapshot("berriz.in")
			update := append([]cookies.Record(nil), snap.Records...)
			update[0].Value = "stale-secret"
			switch mode {
			case "entry":
				if err := s.Delete(snap.Records[0].ID()); err != nil {
					t.Fatal(err)
				}
			case "site":
				if err := s.DeleteSite("berriz.in"); err != nil {
					t.Fatal(err)
				}
			case "disable":
				if err := s.SetEnabled("berriz.in", false); err != nil {
					t.Fatal(err)
				}
			case "import":
				importFile(t, s, dir, ".berriz.in\tTRUE\t/\tTRUE\t0\tsession\tnew-import\n")
			case "newer-task":
				fresh := append([]cookies.Record(nil), snap.Records...)
				fresh[0].Value = "newer-task"
				if err := s.Merge(snap, fresh); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Merge(snap, update); err != nil {
				t.Fatal(err)
			}
			for _, r := range s.Snapshot("berriz.in").Records {
				if r.Value == "stale-secret" {
					t.Fatal("stale task resurrected or overwrote a cookie")
				}
			}
			if mode == "entry" {
				fresh := openStore(t, dir).Snapshot("berriz.in")
				if len(fresh.Filter(update)) != 0 {
					t.Fatal("browser reread restored a deleted entry")
				}
			}
		})
	}
}

func TestCookiePolicyFiltersBrowserAndRelatedDomains(t *testing.T) {
	dir := cookieRoot(t)
	s := openStore(t, dir)
	if err := s.SetEnabled("google.com", false); err != nil {
		t.Fatal(err)
	}
	records := []cookies.Record{{Domain: "youtube.com", Path: "/", Name: "youtube", Value: "yes"}, {Domain: "google.com", Path: "/", Name: "google", Value: "blocked"}, {Domain: "berriz.in", Path: "/", Name: "unrelated", Value: "blocked"}}
	filtered := s.Snapshot("https://youtu.be/video").Filter(records)
	if len(filtered) != 1 || filtered[0].Name != "youtube" {
		t.Fatal("disabled or unrelated domain passed", filtered)
	}
	if err := s.SetEnabled("youtu.be", false); err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshot("youtu.be").Filter(records)) != 0 {
		t.Fatal("excluded short link still used cookies")
	}
}

func TestCookieBareTokensRemainAvailableForCLIExport(t *testing.T) {
	body := []byte("# Netscape HTTP Cookie File\n.example.com\tTRUE\t/\tFALSE\t0\t\tbare-one\n.example.com\tTRUE\t/\tFALSE\t0\t\tbare-two\n")
	records, err := cookies.Parse(body)
	if err != nil || len(records) != 2 {
		t.Fatal(records, err)
	}
	round, err := cookies.Parse(cookies.Encode(records))
	if err != nil || len(round) != 2 || round[0].Name != "" || round[1].Name != "" {
		t.Fatal(round, err)
	}
	address, _ := url.Parse("https://example.com")
	if len(cookies.ForURL(round, address)) != 0 {
		t.Fatal("unsupported native header was generated")
	}
}

func TestCookieGenericEmbedsRetainEnabledServiceCookies(t *testing.T) {
	dir := cookieRoot(t)
	s := openStore(t, dir)
	importFile(t, s, dir, ".youtube.com\tTRUE\t/\tTRUE\t0\tyoutube\tembed-auth\n.vimeo.com\tTRUE\t/\tTRUE\t0\tvimeo\texcluded\n")
	if err := s.SetEnabled("vimeo.com", false); err != nil {
		t.Fatal(err)
	}
	snap := s.TaskSnapshot("https://news.example/article")
	if !snap.All || len(snap.Records) != 1 || snap.Records[0].Name != "youtube" {
		t.Fatal("generic delegation lost an enabled service's cookies", snap)
	}
	if s.TaskSnapshot("https://berriz.in/ko/community/live/replay/id").All {
		t.Fatal("known site lost explicit scope")
	}
	if cookies.Site("127.0.0.1") != "127.0.0.1" || cookies.Site("https://[::1]/") != "::1" {
		t.Fatal("IP addresses were grouped as DNS names")
	}
}

func TestCookieSitePolicyAlsoAppliesToShortLinks(t *testing.T) {
	dir := cookieRoot(t)
	s := openStore(t, dir)
	importFile(t, s, dir, ".twitter.com\tTRUE\t/\tTRUE\t0\tlegacy\tlegacy-auth\n.x.com\tTRUE\t/\tTRUE\t0\tcurrent\tcurrent-auth\n")
	groups := s.Groups()
	if len(groups) != 1 || groups[0].Site != "x.com" || groups[0].Count != 2 {
		t.Fatal("alternate domains were not grouped together", groups)
	}
	if err := s.SetEnabled("twitter.com", false); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"https://x.com/user", "https://twitter.com/user", "https://t.co/link"} {
		if s.TaskSnapshot(address).Enabled {
			t.Fatal("alternate domain bypassed exclusion", address)
		}
	}
	if err := s.SetEnabled("youtube.com", false); err != nil {
		t.Fatal(err)
	}
	if s.TaskSnapshot("https://youtu.be/video").Enabled {
		t.Fatal("short link bypassed service exclusion")
	}
	if err := s.DeleteSite("twitter.com"); err != nil {
		t.Fatal(err)
	}
	for _, group := range s.Groups() {
		if group.Site == "x.com" && group.Count != 0 {
			t.Fatal("site deletion retained alternate-domain cookies")
		}
	}
}
