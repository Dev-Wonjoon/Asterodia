package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/settings"
)

func TestCookieSettingsMigrateOnceAndSupportPartialDeletion(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.root, "cookie-settings")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "original.txt")
	body := "# Netscape HTTP Cookie File\n.berriz.in\tTRUE\t/\tTRUE\t0\tone\tsynthetic-private-one\n.berriz.in\tTRUE\t/service\tTRUE\t0\ttwo\tsynthetic-private-two\n.youtube.com\tTRUE\t/\tTRUE\t0\tyoutube\tprivate-other-site\n"
	if err := os.WriteFile(source, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(dir, "config.json")
	configJSON, _ := json.Marshal(map[string]string{"cookieFilePath": source, "cookieBrowser": "chrome"})
	if err := os.WriteFile(filename, configJSON, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := settings.New(filename, f.root, settings.Dialogs{CookieFile: func() (string, error) { return source, nil }})
	if err != nil {
		t.Fatal(err)
	}
	state := s.GetCookies()
	if len(state.Groups) != 2 {
		t.Fatal(state)
	}
	raw, _ := json.Marshal(state)
	if strings.Contains(string(raw), "synthetic-private") {
		t.Fatal("credential values reached UI API")
	}
	id := state.Groups[0].Entries[0].ID
	if _, err := s.DeleteCookie(id); err != nil {
		t.Fatal(err)
	}
	again, err := settings.New(filename, f.root, settings.Dialogs{})
	if err != nil {
		t.Fatal(err)
	}
	if again.GetCookies().Groups[0].Count != 1 {
		t.Fatal("legacy source was reimported after deletion")
	}
	if _, err := again.DeleteCookieSite("berriz.in"); err != nil {
		t.Fatal(err)
	}
	state = again.GetCookies()
	if state.Groups[0].Enabled || state.Groups[0].Count != 0 || state.Groups[1].Count != 1 {
		t.Fatal(state)
	}
	if _, err := again.SetCookieSiteEnabled("berriz.in", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportCookies(); err != nil {
		t.Fatal(err)
	}
	if s.GetCookies().Groups[0].Count != 2 {
		t.Fatal("explicit import did not restore selected site")
	}
	unchanged, err := os.ReadFile(source)
	if err != nil || string(unchanged) != body {
		t.Fatal("original file changed", err)
	}
}

func TestUnreadableCookieVaultPreservesOtherSettingsAndOriginalBytes(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.root, "unreadable-cookie-settings")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	vault := filepath.Join(dir, "cookies.dat")
	original := []byte("invalid encrypted cookie store")
	if err := os.WriteFile(vault, original, 0600); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(dir, "config.json")
	config, err := settings.New(filename, f.root, settings.Dialogs{})
	if err != nil {
		t.Fatal("cookie store blocked unrelated app settings", err)
	}
	if config.GetCookies().Warning != "store-unavailable" || config.GetDownloadPath() != f.root {
		t.Fatal(config.GetCookies())
	}
	if _, err := config.SetCookieSiteEnabled("example.com", false); err == nil {
		t.Fatal("unreadable store was overwritten")
	}
	unchanged, err := os.ReadFile(vault)
	if err != nil || string(unchanged) != string(original) {
		t.Fatal("encrypted source was changed", err)
	}
}
