package webserver_test

import (
	"encoding/json"
	"fmt"
	"github.com/Dev-Wonjoon/StorageBucket/internal/webserver"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Existing functional tests connect through the same public authentication API.
// Security-boundary tests use an unauthenticated transport explicitly.
type connectedTransport struct {
	mu     sync.Mutex
	base   http.RoundTripper
	server func() *webserver.Service
	cookie *http.Cookie
	url    string
}

func (c *connectedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.mu.Lock()
	state := c.server().Status().Data.(webserver.Status)
	if c.cookie == nil || c.url != state.URL {
		body, _ := json.Marshal(map[string]string{"code": state.PairCode})
		login, err := http.NewRequest("POST", state.URL+"/api/access/connect", strings.NewReader(string(body)))
		if err != nil {
			c.mu.Unlock()
			return nil, err
		}
		login.Header.Set("Origin", state.URL)
		login.Header.Set("Content-Type", "application/json")
		response, err := c.base.RoundTrip(login)
		if err != nil {
			c.mu.Unlock()
			return nil, err
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || len(response.Cookies()) != 1 {
			c.mu.Unlock()
			return nil, fmt.Errorf("test connection failed: HTTP %d", response.StatusCode)
		}
		c.cookie = response.Cookies()[0]
		c.url = state.URL
	}
	cookie := *c.cookie
	c.mu.Unlock()
	copy := r.Clone(r.Context())
	copy.Header = r.Header.Clone()
	copy.AddCookie(&cookie)
	return c.base.RoundTrip(copy)
}
