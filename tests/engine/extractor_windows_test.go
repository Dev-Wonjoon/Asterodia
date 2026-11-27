package engine_test

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

type fixtureExtractor struct {
	name    string
	match   bool
	resolve func(context.Context, engine.ExtractRequest) (engine.DownloadPlan, error)
}

func (f fixtureExtractor) Name() string        { return f.name }
func (f fixtureExtractor) Match(*url.URL) bool { return f.match }
func (f fixtureExtractor) Discover(ctx context.Context, req engine.ExtractRequest) (engine.DownloadPlan, error) {
	return f.resolve(ctx, req)
}

func TestExtractorSelectionAndTerminalErrors(t *testing.T) {
	for _, failure := range []error{nil, engine.ErrUnsupportedURL, engine.ErrRateLimited} {
		t.Run(stringError(failure), func(t *testing.T) {
			var calls []string
			extractors := []engine.Extractor{
				fixtureExtractor{name: "skip", match: false, resolve: func(context.Context, engine.ExtractRequest) (engine.DownloadPlan, error) {
					t.Fatal("unmatched extractor ran")
					return engine.DownloadPlan{}, nil
				}},
				fixtureExtractor{name: "site", match: true, resolve: func(context.Context, engine.ExtractRequest) (engine.DownloadPlan, error) {
					calls = append(calls, "site")
					return engine.DownloadPlan{}, failure
				}},
				fixtureExtractor{name: "generic", match: true, resolve: func(context.Context, engine.ExtractRequest) (engine.DownloadPlan, error) {
					calls = append(calls, "generic")
					return engine.DownloadPlan{}, nil
				}},
			}
			plan, err := engine.DiscoverWithExtractors(t.Context(), engine.ExtractRequest{URL: "https://example.invalid/post/1"}, extractors)
			if err != failure || !reflect.DeepEqual(calls, []string{"site"}) || (failure == nil && plan.Extractor != "site") {
				t.Fatal(plan, calls, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	_, err := engine.DiscoverWithExtractors(ctx, engine.ExtractRequest{URL: "https://example.invalid"}, []engine.Extractor{
		fixtureExtractor{match: true, resolve: func(context.Context, engine.ExtractRequest) (engine.DownloadPlan, error) {
			cancel()
			return engine.DownloadPlan{}, nil
		}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("late result survived cancellation", err)
	}
	if _, err := engine.DiscoverWithExtractors(t.Context(), engine.ExtractRequest{URL: "file:///secret"}, nil); !errors.Is(err, engine.ErrInvalidURL) {
		t.Fatal(err)
	}
	if _, err := engine.DiscoverWithExtractors(t.Context(), engine.ExtractRequest{URL: "https://example.invalid"}, nil); !errors.Is(err, engine.ErrUnsupportedURL) {
		t.Fatal(err)
	}
}

func stringError(err error) string {
	if err == nil {
		return "success"
	}
	return err.Error()
}

func TestExtractorSiteMatching(t *testing.T) {
	extractors := engine.DefaultExtractors(engine.ExtractorDependencies{})
	for _, tc := range []struct{ url, want string }{
		{"https://www.instagram.com/p/Fixture/", "instagram"},
		{"https://instagram.com/p/Fixture/", "instagram"},
		{"https://www.threads.com/@user/post/Fixture", "threads"},
		{"https://threads.net/t/Fixture", "threads"},
		{"https://threads.com.evil.invalid/post/1", "generic"},
		{"https://fakeinstagram.com/post/1", "generic"},
		{"https://x.com/user/status/1", "generic"},
		{"https://berriz.in/ko/kiiikiii/live/replay/01a17302-2c13-57bc-c315-ba00b1c61882/", "berriz"},
		{"https://berriz.in.evil.invalid/ko/user/live/replay/id", "generic"},
	} {
		address, _ := url.Parse(tc.url)
		found := ""
		for _, extractor := range extractors {
			if extractor.Match(address) {
				found = extractor.Name()
				break
			}
		}
		if found != tc.want {
			t.Errorf("%s: got %s, want %s", tc.url, found, tc.want)
		}
	}
}
