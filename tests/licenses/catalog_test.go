package licenses_test

import (
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/licenses"
	"github.com/Dev-Wonjoon/Asterodia/internal/settings"
)

func TestOfflineNoticesAndSettings(t *testing.T) {
	entries := (&settings.Service{}).GetEngineLicenses()
	seen := make(map[string]bool)
	plain := licenses.Text()
	want := map[string]string{
		"Go standard library and runtime": "Sun Microsystems",
		"npm: lucide-react":               "Feather", "npm: react": "MIT License",
		"github.com/wailsapp/wails/v3": "John Chadwick", "modernc.org/sqlite": "SQLite",
		"Inter font": "SIL OPEN FONT LICENSE", "yt-dlp": "THIRD-PARTY LICENSES", "ImageMagick": "NOTICE",
		"gallery-dl": "Mike F", "FFmpeg / ffprobe (BtbN GPL build)": "GNU GENERAL PUBLIC LICENSE",
	}
	for _, item := range entries {
		if seen[item.ID] || item.ID == "" || item.License == "" || item.Version == "" || item.Text == "" || !strings.HasPrefix(item.URL, "https://") {
			t.Fatalf("incomplete notice: %s", item.Name)
		}
		seen[item.ID] = true
		if item.Scope != "bundled" && item.Scope != "external" && item.Scope != "build" {
			t.Fatalf("invalid scope: %s", item.Scope)
		}
		if content, ok := want[item.Name]; ok {
			if !strings.Contains(item.Notice+item.Text, content) {
				t.Fatalf("%s missing original notice: %s", item.Name, content)
			}
			delete(want, item.Name)
		}
		if !strings.Contains(plain, item.Text) {
			t.Fatalf("plain text export omitted %s", item.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing components: %v", want)
	}
	original := entries[0].Name
	entries[0].Name = "modified"
	if licenses.List()[0].Name != original {
		t.Fatal("caller modified shared notices")
	}
}
