package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// Extractors own site selection and discovery policy. Backends only execute tools.
type Extractor interface {
	Name() string
	Match(*url.URL) bool
	Discover(context.Context, ExtractRequest) (DownloadPlan, error)
}

type ExtractRequest struct {
	URL     string
	WorkDir string
	Options Options
}

type YtdlpBackend interface {
	Discover(context.Context, string, string) (json.RawMessage, error)
	DownloadSingle(context.Context, YtdlpTarget, string, Options) (string, error)
}

type GalleryDLBackend interface {
	Discover(context.Context, string, string) (GalleryDLDiscovery, error)
	Download(context.Context, string, GalleryDLDiscovery, string) ([]string, error)
}

// CookieSource returns only the cookies applicable to one requested URL.
type CookieSource interface {
	Cookies(context.Context, *url.URL, string) ([]*http.Cookie, error)
}

type ExtractorDependencies struct {
	Ytdlp            YtdlpBackend
	GalleryDL        GalleryDLBackend
	HTTPClient       *http.Client
	AuthenticatedFor func(string) bool
	Authenticated    bool
	Cookies          CookieSource
}

// DefaultExtractors is an explicit ordered registration list, built per runner.
// The generic matcher must remain last.
func DefaultExtractors(deps ExtractorDependencies) []Extractor {
	return []Extractor{
		NewInstagramExtractor(deps),
		NewThreadsExtractor(deps),
		NewBerrizExtractor(deps),
		NewGenericExtractor(deps),
	}
}

func DiscoverWithExtractors(ctx context.Context, req ExtractRequest, extractors []Extractor) (DownloadPlan, error) {
	if err := ctx.Err(); err != nil {
		return DownloadPlan{}, err
	}
	if _, err := Select(req.URL, req.Options); err != nil {
		return DownloadPlan{}, err
	}
	address, _ := url.Parse(req.URL)
	for _, extractor := range extractors {
		if extractor != nil && extractor.Match(address) {
			plan, err := extractor.Discover(ctx, req)
			if ctx.Err() != nil {
				return DownloadPlan{}, ctx.Err()
			}
			if err != nil {
				return DownloadPlan{}, err
			}
			plan.Extractor = extractor.Name()
			return plan, nil
		}
	}
	return DownloadPlan{}, ErrUnsupportedURL
}

func siteHost(address *url.URL, domains ...string) bool {
	if address == nil {
		return false
	}
	host := strings.ToLower(address.Hostname())
	for _, domain := range domains {
		if host == domain || host == "www."+domain {
			return true
		}
	}
	return false
}

// Public strategies may fall through on their own timeout. Request cancellation
// and hard resource/rate limits are handled before trying another strategy.
func publicDiscoveryHandler(name string, resolve func(context.Context) (DownloadPlan, error)) DiscoveryHandler[DownloadPlan] {
	return DiscoveryHandler[DownloadPlan]{Name: name, Handle: func(ctx context.Context) DiscoveryStepResult[DownloadPlan] {
		plan, err := resolve(ctx)
		decision := DiscoveryHandled
		if err != nil {
			decision = DiscoveryNext
			if discoveryMustStop(err) {
				decision = DiscoveryStop
			}
		}
		return DiscoveryStepResult[DownloadPlan]{Decision: decision, Value: plan, Err: err}
	}}
}
