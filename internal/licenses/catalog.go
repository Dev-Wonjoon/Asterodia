// Package licenses provides the offline notices shipped with StorageBucket.
package licenses

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed catalog.json texts/*.txt
var assets embed.FS

type Entry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	License   string `json:"license"`
	Scope     string `json:"scope"`
	URL       string `json:"url"`
	SourceURL string `json:"sourceURL"`
	Notice    string `json:"notice"`
	Text      string `json:"text"`
}

var catalog = load()

func load() []Entry {
	data, err := assets.ReadFile("catalog.json")
	if err != nil {
		panic(err)
	}
	var manifest struct {
		Entries []struct {
			Entry
			Documents []struct{ Title, File, URL, SHA256 string }
		}
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		panic(err)
	}
	entries := make([]Entry, 0, len(manifest.Entries))
	seen := make(map[string]bool)
	for _, item := range manifest.Entries {
		if item.ID == "" || seen[item.ID] || len(item.Documents) == 0 {
			panic("invalid embedded license entry: " + item.ID)
		}
		seen[item.ID] = true
		var text strings.Builder
		for _, document := range item.Documents {
			original, err := assets.ReadFile(document.File)
			if err != nil || len(original) == 0 || fmt.Sprintf("%x", sha256.Sum256(original)) != document.SHA256 {
				panic("invalid embedded license text: " + document.File)
			}
			fmt.Fprintf(&text, "===== %s =====\nSource: %s\n\n", document.Title, document.URL)
			text.Write(original)
			text.WriteString("\n\n")
		}
		item.Text = text.String()
		entries = append(entries, item.Entry)
	}
	return entries
}

// List returns a copy so callers cannot change the shared embedded catalog.
func List() []Entry { return append([]Entry(nil), catalog...) }

// Text returns the full notices for offline viewing or saving as plain text.
func Text() string {
	var text strings.Builder
	text.WriteString("StorageBucket — Third-Party Software Notices\n\n")
	for _, item := range catalog {
		fmt.Fprintf(&text, "%s | %s | %s | %s\n%s\n%s\n\n%s\n", item.Name, item.Version, item.Scope, item.License, item.URL, item.Notice, item.Text)
	}
	return text.String()
}
