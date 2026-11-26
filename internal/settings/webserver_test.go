package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWebCodePreferenceDefaultsAndPersists(t *testing.T) {
	name := filepath.Join(t.TempDir(), "config.json")
	s, err := New(name, "downloads", Dialogs{})
	if err != nil {
		t.Fatal(err)
	}
	if !s.GetWebRequireCode() {
		t.Fatal("code requirement must default to on")
	}
	for _, required := range []bool{false, true} {
		if err := s.SetWebRequireCode(required); err != nil {
			t.Fatal(err)
		}
		s, err = New(name, "downloads", Dialogs{})
		if err != nil {
			t.Fatal(err)
		}
		if s.GetWebRequireCode() != required {
			t.Fatal("preference was not restored", required)
		}
	}
}

func TestWebCodePreferenceRollsBackOnSaveFailure(t *testing.T) {
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "config.json"), "downloads", Dialogs{})
	if err != nil {
		t.Fatal(err)
	}
	// A regular file cannot contain a settings file.
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	s.filename = filepath.Join(blocked, "config.json")
	if s.SetWebRequireCode(false) == nil || !s.GetWebRequireCode() {
		t.Fatal("failed save changed preference")
	}
}
