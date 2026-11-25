package thumbnail

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
)

func TestPreviewBlocksNonPublicAddresses(t *testing.T) {
	for _, address := range []string{
		"0.1.2.3", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.169.254",
		"172.16.0.1", "192.168.1.1", "192.0.0.8", "192.0.2.1", "192.88.99.1",
		"198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "255.255.255.255",
		"::", "::1", "::ffff:127.0.0.1", "::ffff:192.168.1.1", "::ffff:100.64.0.1",
		"fc00::1", "fd7a:115c:a1e0::1", "fe80::1", "fe80::1%eth0", "ff02::1",
		"64:ff9b::a00:1", "64:ff9b:1::1", "100::1", "2001::1", "2001:db8::1", "2002:7f00:1::1", "3fff::1",
	} {
		t.Run(address, func(t *testing.T) {
			var lookups, connections int
			d := previewDialer{
				lookup: func(context.Context, string, string) ([]netip.Addr, error) {
					lookups++
					return nil, errors.New("literal must not be resolved")
				},
				connect: func(context.Context, string, string) (net.Conn, error) {
					connections++
					return nil, errors.New("blocked destination must not be dialed")
				},
			}
			_, err := d.dialContext(t.Context(), "tcp", net.JoinHostPort(address, "80"))
			if !errors.Is(err, errPreviewDestination) || lookups != 0 || connections != 0 {
				t.Fatalf("blocked address reached DNS/dial: %v, %d, %d", err, lookups, connections)
			}
		})
	}
}

func TestPreviewValidatesAllDNSAnswersBeforeDialing(t *testing.T) {
	for _, answers := range [][]netip.Addr{
		nil,
		{netip.MustParseAddr("127.0.0.1")},
		{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.1")},
		{netip.MustParseAddr("2606:4700:4700::1111"), netip.MustParseAddr("::ffff:169.254.169.254")},
	} {
		connections := 0
		d := previewDialer{
			lookup: func(context.Context, string, string) ([]netip.Addr, error) { return answers, nil },
			connect: func(context.Context, string, string) (net.Conn, error) {
				connections++
				return nil, errors.New("should not dial any mixed DNS answer")
			},
		}
		_, err := d.dialContext(t.Context(), "tcp", "image.example:80")
		if !errors.Is(err, errPreviewDestination) || connections != 0 {
			t.Fatalf("unsafe DNS answer accepted: %v, %v", answers, err)
		}
	}
}

func TestPreviewPinsPublicDNSAnswersIncludingFallbacks(t *testing.T) {
	var lookups int
	var dialed []string
	d := previewDialer{
		lookup: func(context.Context, string, string) ([]netip.Addr, error) {
			lookups++
			if lookups > 1 {
				return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
			}
			return []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111"), netip.MustParseAddr("93.184.216.34")}, nil
		},
		connect: func(_ context.Context, _, address string) (net.Conn, error) {
			dialed = append(dialed, address)
			if len(dialed) == 1 {
				return nil, errors.New("first public address unavailable")
			}
			conn, peer := net.Pipe()
			peer.Close()
			return conn, nil
		},
	}
	conn, err := d.dialContext(t.Context(), "tcp", "image.example:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if lookups != 1 || len(dialed) != 2 || dialed[0] != "[2606:4700:4700::1111]:443" || dialed[1] != "93.184.216.34:443" {
		t.Fatalf("DNS was not pinned through fallback: %d, %v", lookups, dialed)
	}
}

// Public DNS and dialing are injected only into this package-private client.
// The real client always uses its checked dialer; tests never access the internet.
func localPreviewClient(t *testing.T, server *httptest.Server, lookup func(string) []netip.Addr) (*http.Client, *atomic.Int32) {
	t.Helper()
	dials := &atomic.Int32{}
	client := previewHTTPClient(previewDialer{
		lookup: func(_ context.Context, _, host string) ([]netip.Addr, error) { return lookup(host), nil },
		connect: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "93.184.216.34:80" && address != "93.184.216.34:443" {
				return nil, errors.New("attempted to connect without a pinned public IP")
			}
			dials.Add(1)
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
	})
	t.Cleanup(client.CloseIdleConnections)
	return client, dials
}

func publicPreviewAnswer(string) []netip.Addr {
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}
}

func TestPreviewRechecksRedirectDestinations(t *testing.T) {
	for _, destination := range []string{
		"http://127.0.0.1/private", "http://[::1]/private", "http://100.64.0.1/private",
		"http://internal.example/private", "http://user:secret@image.example/private", "file:///private",
	} {
		t.Run(destination, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				http.Redirect(w, r, destination, http.StatusFound)
			}))
			defer server.Close()
			client, dials := localPreviewClient(t, server, func(host string) []netip.Addr {
				if host == "image.example" {
					return publicPreviewAnswer(host)
				}
				return []netip.Addr{netip.MustParseAddr("10.0.0.1")}
			})
			response, err := client.Get("http://image.example/start")
			if response != nil {
				response.Body.Close()
			}
			if err == nil || hits.Load() != 1 || dials.Load() != 1 {
				t.Fatalf("unsafe redirect was followed: %v, hits=%d, dials=%d", err, hits.Load(), dials.Load())
			}
		})
	}
}

func TestPreviewRejectsRebindingOnNewConnection(t *testing.T) {
	var hits, lookups atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Connection", "close")
		http.Redirect(w, r, "/after", http.StatusFound)
	}))
	defer server.Close()
	client, dials := localPreviewClient(t, server, func(string) []netip.Addr {
		if lookups.Add(1) == 1 {
			return publicPreviewAnswer("")
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	})
	response, err := client.Get("http://image.example/start")
	if response != nil {
		response.Body.Close()
	}
	if err == nil || lookups.Load() != 2 || hits.Load() != 1 || dials.Load() != 1 {
		t.Fatalf("rebinding escaped validation: %v, lookups=%d, hits=%d, dials=%d", err, lookups.Load(), hits.Load(), dials.Load())
	}
}

func TestPreviewPreservesHTTPSHostnameAndCertificateVerification(t *testing.T) {
	observed := make(chan [2]string, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- [2]string{r.Host, r.TLS.ServerName}
		io.WriteString(w, "verified")
	}))
	defer server.Close()
	name := server.Certificate().DNSNames[0]
	client, _ := localPreviewClient(t, server, publicPreviewAnswer)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: pool}
	response, err := client.Get("https://" + name + "/image")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	hostname := <-observed
	host, serverName := hostname[0], hostname[1]
	if host != name || serverName != name {
		t.Fatalf("pinning replaced HTTP Host or TLS SNI: %q, %q", host, serverName)
	}
	// Trusting the test CA must not disable certificate hostname checks.
	response, err = client.Get("https://wrong.example/image")
	if response != nil {
		response.Body.Close()
	}
	if err == nil {
		t.Fatal("HTTPS accepted a mismatched certificate")
	}
}

func TestPublicPreviewFetchesAndCachesWithoutAmbientProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	var payload bytes.Buffer
	if err := png.Encode(&payload, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "http://image.example/image", http.StatusFound)
			return
		}
		w.Write(payload.Bytes())
	}))
	defer server.Close()
	client, _ := localPreviewClient(t, server, publicPreviewAnswer)
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err = database.Initialize(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	const source = "https://source.example/post"
	if _, err = db.Exec("INSERT INTO download_urls(url,normalized_url,normalization_version,created_at) VALUES(?,?,1,0)", source, source); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO download_queue(id,request_url_id,original_url,mode,state,options_json) SELECT 'preview',id,?,'create','cancelled','{}' FROM download_urls WHERE url=?", source, source); err != nil {
		t.Fatal(err)
	}
	s := NewService(db, filepath.Join(t.TempDir(), "thumbnails"), "", nil)
	t.Cleanup(s.Close)
	discoveries := 0
	s.ConfigurePreview(func(context.Context, string, string) (json.RawMessage, error) {
		discoveries++
		return json.Marshal(map[string]string{"thumbnail": "http://start.example/start"})
	})
	for range 2 {
		path, err := s.getDownloadPreview(t.Context(), source, func() *http.Client { return client })
		if err != nil || path == nil {
			t.Fatalf("public preview missing: %v, %v", path, err)
		}
		data, err := os.ReadFile(*path)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width != 3 || cfg.Height != 2 || !strings.HasSuffix(*path, ".jpg") {
			t.Fatalf("invalid cached preview: %v, %v", cfg, err)
		}
	}
	if hits.Load() != 2 || discoveries != 1 {
		t.Fatalf("preview cache was not reused: hits=%d, discoveries=%d", hits.Load(), discoveries)
	}
}
