package integration_test

import (
	"github.com/Dev-Wonjoon/StorageBucket/internal/settings"
	"path/filepath"
	"testing"
)

func TestWorkingDirectorySettingsKeepDownloadLocationAndPersistChoice(t *testing.T) {
	f := newFixture(t)
	downloads := filepath.Join(f.root, "downloads")
	chosen := filepath.Join(f.root, "working")
	filename := filepath.Join(f.root, "settings.json")
	dialogs := settings.Dialogs{Directory: func(initial string) (string, error) { return chosen, nil }}
	config, err := settings.New(filename, downloads, dialogs)
	if err != nil {
		t.Fatal(err)
	}
	if config.GetWorkingDirectory() != "" || config.GetEditOutputDirectory() != filepath.Join(downloads, "edits") {
		t.Fatal("legacy fallback changed")
	}
	if value, err := config.ChooseEditOutputDirectory(); err != nil || value == nil || *value != chosen {
		t.Fatal(value, err)
	}
	if config.GetWorkingDirectory() != "" {
		t.Fatal("per-job chooser changed defaults")
	}
	if _, err = config.SetWorkingDirectory(); err != nil {
		t.Fatal(err)
	}
	again, err := settings.New(filename, downloads, dialogs)
	if err != nil {
		t.Fatal(err)
	}
	if again.GetWorkingDirectory() != chosen || again.GetEditOutputDirectory() != chosen || again.GetDownloadPath() != downloads {
		t.Fatal("working folder not independent")
	}
	if _, err = again.ClearWorkingDirectory(); err != nil {
		t.Fatal(err)
	}
	if again.GetEditOutputDirectory() != filepath.Join(downloads, "edits") {
		t.Fatal("fallback not restored")
	}
	cancel, err := settings.New(filename, downloads, settings.Dialogs{Directory: func(string) (string, error) { return "", nil }})
	if err != nil {
		t.Fatal(err)
	}
	if value, err := cancel.SetWorkingDirectory(); err != nil || value != nil {
		t.Fatal("cancel changed setting", value, err)
	}
}
