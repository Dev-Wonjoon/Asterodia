package thumbnail

import (
	"context"
	"github.com/Dev-Wonjoon/Asterodia/internal/netguard"
	"net"
	"net/http"
	"net/netip"
	"net/url"
)

var errPreviewDestination = netguard.ErrDestination

func publicPreviewIP(ip netip.Addr) bool { return netguard.PublicIP(ip) }
func validPreviewURL(u *url.URL) bool    { return netguard.ValidURL(u) }

type previewDialer struct {
	lookup  func(context.Context, string, string) ([]netip.Addr, error)
	connect func(context.Context, string, string) (net.Conn, error)
}

func (d previewDialer) guard() netguard.Dialer {
	return netguard.Dialer{Lookup: d.lookup, Connect: d.connect}
}
func (d previewDialer) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d.guard().DialContext(ctx, network, address)
}
func newPreviewHTTPClient() *http.Client             { return netguard.NewClient() }
func previewHTTPClient(d previewDialer) *http.Client { return netguard.Client(d.guard()) }
