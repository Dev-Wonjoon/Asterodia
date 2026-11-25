package mediaedit_test

import (
	"encoding/json"
	"github.com/Dev-Wonjoon/StorageBucket/internal/webserver"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

func connectMobileClient(t *testing.T, client *http.Client, server *webserver.Service) {
	t.Helper()
	state := server.Status().Data.(webserver.Status)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar
	body, _ := json.Marshal(map[string]string{"code": state.PairCode})
	r, err := http.NewRequest("POST", state.URL+"/api/access/connect", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Origin", state.URL)
	r.Header.Set("Content-Type", "application/json")
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	io.Copy(io.Discard, response.Body)
	if response.StatusCode != 200 {
		t.Fatal("mobile test connection failed", response.StatusCode)
	}
}
