package mediaedit_test

import (
	"bufio"
	"fmt"
	"os"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"github.com/Dev-Wonjoon/Asterodia/internal/webserver"
)

// Opt-in browser fixture. Every file and database belongs to the isolated run.
func TestEditorBrowserFixture(t *testing.T) {
	if os.Getenv("ASTERODIA_EDITOR_FIXTURE") != "1" {
		t.Skip("브라우저 확인 전용")
	}
	var f *fixture
	if os.Getenv("ASTERODIA_EDITOR_GIF") == "1" {
		f = gifFixtureFrames(t, 10)
	} else {
		f = newFixture(t)
	}
	server := webserver.NewService(media.NewService(f.db), "127.0.0.1:0")
	server.ConfigureEditor(f.service)
	t.Cleanup(server.Close)
	state := success[webserver.Status](t, server.Start())
	if !server.SetEditing(true).OK {
		t.Fatal("편집 허용 실패")
	}
	fmt.Println("__EDITOR_URL__" + state.URL)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if scanner.Text() == "exit" {
			return
		}
	}
}
