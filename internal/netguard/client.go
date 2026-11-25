package netguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

var ErrDestination = errors.New("미리보기 주소는 공개 인터넷 주소여야 합니다")

// Also exclude special-purpose ranges that IsGlobalUnicast/IsPrivate do not
// exclude, including Tailscale/CGNAT and IPv6 transition mechanisms.
var previewSpecialNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

var previewIPv6Internet = netip.MustParsePrefix("2000::/3")

func PublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() ||
		(ip.Is6() && !previewIPv6Internet.Contains(ip)) {
		return false
	}
	for _, network := range previewSpecialNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

func ValidURL(address *url.URL) bool {
	if address == nil || address.User != nil || address.Hostname() == "" ||
		(address.Scheme != "http" && address.Scheme != "https") {
		return false
	}
	if ip, err := netip.ParseAddr(address.Hostname()); err == nil {
		return PublicIP(ip)
	}
	return true
}

type Dialer struct {
	Lookup  func(context.Context, string, string) ([]netip.Addr, error)
	Connect func(context.Context, string, string) (net.Conn, error)
}

func (d Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrDestination
	}
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else {
		ips, err = d.Lookup(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
	}
	if len(ips) == 0 {
		return nil, ErrDestination
	}
	// Reject mixed public/private answers before connecting to any address.
	for _, ip := range ips {
		if !PublicIP(ip) {
			return nil, ErrDestination
		}
	}
	var lastErr error
	for _, ip := range ips {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Dial the checked IP directly: a second DNS Lookup would permit rebinding.
		conn, err := d.Connect(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func NewClient() *http.Client {
	return Client(DefaultDialer())
}

func Client(dialer Dialer) *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			// Ambient proxies resolve destinations elsewhere and bypass IP checks.
			Proxy: nil, DialContext: dialer.DialContext, ForceAttemptHTTP2: true,
			TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 10 * time.Second,
			IdleConnTimeout: 30 * time.Second, MaxIdleConns: 8, MaxIdleConnsPerHost: 2,
			MaxResponseHeaderBytes: 32 << 10,
		},
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) > 5 || !ValidURL(next.URL) {
				return ErrDestination
			}
			return nil
		},
	}
}

func DefaultDialer() Dialer {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return Dialer{Lookup: net.DefaultResolver.LookupNetIP, Connect: d.DialContext}
}
