package netguard

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Proxy is scoped to one trusted engine operation. Its secret is never exposed
// by web APIs. CONNECT keeps end-to-end TLS certificate verification intact.
type Proxy struct {
	URL         string
	cancel      context.CancelFunc
	server      *http.Server
	connections sync.Map
	remaining   atomic.Int64
	once        sync.Once
	client      *http.Client
	dialer      Dialer
}

func StartProxy(ctx context.Context, budget int64) (*Proxy, error) {
	return startProxy(ctx, budget, DefaultDialer())
}
func startProxy(ctx context.Context, budget int64, dialer Dialer) (*Proxy, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		listener.Close()
		return nil, err
	}
	token := hex.EncodeToString(secret)
	ctx, cancel := context.WithCancel(ctx)
	p := &Proxy{URL: "http://engine:" + token + "@" + listener.Addr().String(), cancel: cancel, client: Client(dialer), dialer: dialer}
	p.remaining.Store(budget)
	capacity := make(chan struct{}, 32)
	p.server = &http.Server{ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	p.server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("engine:"+token))
		if subtle.ConstantTimeCompare([]byte(expected), []byte(r.Header.Get("Proxy-Authorization"))) != 1 {
			http.Error(w, "Proxy authentication required", 407)
			return
		}
		select {
		case capacity <- struct{}{}:
			defer func() { <-capacity }()
		default:
			http.Error(w, "Busy", 429)
			return
		}
		if ctx.Err() != nil || p.remaining.Load() <= 0 {
			http.Error(w, "Transfer limit reached", 403)
			return
		}
		if r.Method == http.MethodConnect {
			p.tunnel(w, r)
			return
		}
		if r.URL.Scheme != "http" || !ValidURL(r.URL) || (r.Method != "GET" && r.Method != "HEAD" && r.Method != "POST") {
			http.Error(w, "Destination denied", 403)
			return
		}
		if port := r.URL.Port(); port != "" && port != "80" {
			http.Error(w, "Port denied", 403)
			return
		}
		out := r.Clone(r.Context())
		out.RequestURI = ""
		out.Header = r.Header.Clone()
		stripHopHeaders(out.Header)
		out.Body = &budgetBody{ReadCloser: r.Body, proxy: p}
		response, err := p.client.Transport.RoundTrip(out)
		if err != nil {
			http.Error(w, "Upstream request failed", 502)
			return
		}
		defer response.Body.Close()
		stripHopHeaders(response.Header)
		for key, values := range response.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, &budgetReader{Reader: response.Body, proxy: p})
	})
	go func() { _ = p.server.Serve(listener) }()
	go func() { <-ctx.Done(); p.Close() }()
	return p, nil
}

func stripHopHeaders(h http.Header) {
	for _, value := range h.Values("Connection") {
		for _, key := range strings.Split(value, ",") {
			h.Del(strings.TrimSpace(key))
		}
	}
	for _, key := range []string{"Connection", "Proxy-Connection", "Proxy-Authenticate", "Proxy-Authorization", "Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(key)
	}
}

func (p *Proxy) tunnel(w http.ResponseWriter, r *http.Request) {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil || port != "443" || !ValidURL(&url.URL{Scheme: "https", Host: net.JoinHostPort(host, port)}) {
		http.Error(w, "Destination denied", 403)
		return
	}
	upstream, err := p.dialer.DialContext(r.Context(), "tcp", r.Host)
	if err != nil {
		http.Error(w, "Destination denied", 403)
		return
	}
	defer upstream.Close()
	p.connections.Store(upstream, struct{}{})
	defer p.connections.Delete(upstream)
	if r.Context().Err() != nil {
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Tunnel unavailable", 500)
		return
	}
	downstream, buffer, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer downstream.Close()
	p.connections.Store(downstream, struct{}{})
	defer p.connections.Delete(downstream)
	if r.Context().Err() != nil {
		return
	}
	_ = downstream.SetDeadline(time.Now().Add(10 * time.Minute))
	_, err = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	if err != nil {
		return
	}
	if buffer.Flush() != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(upstream, &budgetReader{Reader: buffer.Reader, proxy: p})
		upstream.Close()
		close(done)
	}()
	_, _ = io.Copy(downstream, &budgetReader{Reader: upstream, proxy: p})
	downstream.Close()
	upstream.Close()
	<-done
}

func (p *Proxy) Close() {
	p.once.Do(func() {
		p.cancel()
		_ = p.server.Close()
		p.connections.Range(func(key, value any) bool { _ = key.(net.Conn).Close(); return true })
		p.client.CloseIdleConnections()
	})
}

type budgetReader struct {
	io.Reader
	proxy *Proxy
}

func (r *budgetReader) Read(b []byte) (int, error) {
	left := r.proxy.remaining.Load()
	if left <= 0 {
		return 0, errors.New("transfer budget exceeded")
	}
	if int64(len(b)) > left {
		b = b[:left]
	}
	n, err := r.Reader.Read(b)
	if r.proxy.remaining.Add(-int64(n)) < 0 {
		return 0, errors.New("transfer budget exceeded")
	}
	return n, err
}

type budgetBody struct {
	io.ReadCloser
	proxy *Proxy
}

func (r *budgetBody) Read(b []byte) (int, error) {
	return (&budgetReader{Reader: r.ReadCloser, proxy: r.proxy}).Read(b)
}
