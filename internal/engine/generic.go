package engine

import (
	"context"
	"errors"
	"net/url"
)

type GenericExtractor struct{ deps ExtractorDependencies }

func NewGenericExtractor(deps ExtractorDependencies) *GenericExtractor {
	return &GenericExtractor{deps: deps}
}
func (*GenericExtractor) Name() string                { return "generic" }
func (*GenericExtractor) Match(address *url.URL) bool { return address != nil }

func (g *GenericExtractor) Discover(ctx context.Context, req ExtractRequest) (DownloadPlan, error) {
	first, err := Select(req.URL, req.Options)
	if err != nil {
		return DownloadPlan{}, err
	}
	other := KindGalleryDL
	if first == KindGalleryDL {
		other = KindYtdlp
	}
	chain := RunDiscoveryChain(ctx, g.handler(first, req), g.handler(other, req))
	return chain.Value, chain.Err
}

func discoveryMustStop(err error) bool {
	return errors.Is(err, ErrRateLimited) || errors.Is(err, ErrOutputLimit) || errors.Is(err, ErrStopUnconfirmed)
}

func (g *GenericExtractor) handler(kind Kind, req ExtractRequest) DiscoveryHandler[DownloadPlan] {
	return DiscoveryHandler[DownloadPlan]{Name: string(kind), Handle: func(ctx context.Context) DiscoveryStepResult[DownloadPlan] {
		var plan DownloadPlan
		err := ErrUnsupportedURL
		switch kind {
		case KindYtdlp:
			if backend := g.deps.Ytdlp; backend != nil {
				raw, e := backend.Discover(ctx, req.URL, req.WorkDir)
				err = e
				if err == nil {
					var targets []YtdlpTarget
					targets, err = ParseYtdlpTargets(raw, req.URL)
					plan = ytdlpDownloadPlan(req, targets, backend)
				}
			}
		case KindGalleryDL:
			if backend := g.deps.GalleryDL; backend != nil {
				data, e := backend.Discover(ctx, req.URL, req.WorkDir)
				err = e
				if err == nil {
					plan = galleryDownloadPlan(req, data, backend)
				}
			}
		}
		decision := DiscoveryHandled
		if err != nil {
			decision = DiscoveryStop
			if errors.Is(err, ErrUnsupportedURL) && !discoveryMustStop(err) &&
				!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				decision = DiscoveryNext
			}
			plan = DownloadPlan{}
		}
		return DiscoveryStepResult[DownloadPlan]{Decision: decision, Value: plan, Err: err}
	}}
}
