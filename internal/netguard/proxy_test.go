package netguard

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func proxyClient(t *testing.T, p *Proxy, authenticate bool) *http.Client {
	t.Helper()
	u, err := url.Parse(p.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !authenticate {
		u.User = nil
	}
	transport := &http.Transport{Proxy: http.ProxyURL(u)}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 3 * time.Second}
}

func TestProxyRequiresSecretAndBlocksPrivateDestinations(t *testing.T) {
	var hits atomic.Int32
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); io.WriteString(w, "private") }))
	defer local.Close()
	p, err := StartProxy(t.Context(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, authenticated := range []bool{false, true} {
		client := proxyClient(t, p, authenticated)
		response, err := client.Get(local.URL)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		want := http.StatusForbidden
		if !authenticated {
			want = 407
		}
		if response.StatusCode != want {
			t.Fatalf("proxy returned %d, want %d", response.StatusCode, want)
		}
	}
	response, err := proxyClient(t, p, true).Get("https://127.0.0.1/private")
	if response != nil {
		response.Body.Close()
	}
	if err == nil {
		t.Fatal("private CONNECT was accepted")
	}
	if hits.Load() != 0 {
		t.Fatal("proxy accessed private endpoint")
	}
}

func TestProxyPinsPublicDestinationsStripsSecretsAndBoundsTransfers(t *testing.T) {
	var secretSeen atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			secretSeen.Store(true)
		}
		io.WriteString(w, strings.Repeat("x", 100))
	}))
	defer upstream.Close()
	dialer := Dialer{Lookup: func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}, Connect: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "93.184.216.34:80" {
			t.Errorf("unvalidated address %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}}
	p, err := startProxy(t.Context(), 8, dialer)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	response, err := proxyClient(t, p, true).Get("http://image.example/")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if len(payload) > 8 || p.remaining.Load() > 0 || secretSeen.Load() {
		t.Fatalf("unsafe proxy transfer: bytes=%d, remaining=%d, secret=%v", len(payload), p.remaining.Load(), secretSeen.Load())
	}
}
