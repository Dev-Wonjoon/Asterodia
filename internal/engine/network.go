package engine

import (
	"context"
	"github.com/Dev-Wonjoon/Asterodia/internal/netguard"
	"strings"
)

type networkKey struct{}

// SecureContext scopes a public-network proxy to one trusted app operation.
func SecureContext(ctx context.Context) (context.Context, func(), error) {
	p, err := netguard.StartProxy(ctx, 20<<30)
	if err != nil {
		return ctx, func() {}, err
	}
	return context.WithValue(ctx, networkKey{}, p.URL), p.Close, nil
}

func networkArguments(ctx context.Context) []string {
	proxy, _ := ctx.Value(networkKey{}).(string)
	if proxy == "" {
		return nil
	}
	return []string{"--proxy", proxy}
}

func secureEnvironment(parent []string, proxy string) []string {
	environment := make([]string, 0, len(parent)+4)
	for _, entry := range parent {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, "HTTP_PROXY="+proxy, "HTTPS_PROXY="+proxy, "ALL_PROXY="+proxy, "NO_PROXY=")
}
