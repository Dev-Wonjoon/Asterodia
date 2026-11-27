package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/cookies"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

const berrizID = "01a17302-2c13-57bc-c315-ba00b1c61882"
const berrizOriginal = "https://berriz.in/ko/kiiikiii/live/replay/" + berrizID + "/?source=user"
const berrizManifest = "https://vod-edge.berriz.in/cdn/vod/fixture/master.m3u8?signature=keep%2Bexact"

type berrizCookies func(context.Context, *url.URL, string) ([]*http.Cookie, error)

func (f berrizCookies) Cookies(ctx context.Context, address *url.URL, dir string) ([]*http.Cookie, error) {
	return f(ctx, address, dir)
}

type berrizBackend struct {
	discover func(context.Context, string, string) (json.RawMessage, error)
	save     func(context.Context, engine.YtdlpTarget, string, engine.Options) (string, error)
}

func (b berrizBackend) Discover(ctx context.Context, address, dir string) (json.RawMessage, error) {
	return b.discover(ctx, address, dir)
}
func (b berrizBackend) DownloadSingle(ctx context.Context, target engine.YtdlpTarget, dir string, options engine.Options) (string, error) {
	if b.save == nil {
		return "", errors.New("unexpected download")
	}
	return b.save(ctx, target, dir, options)
}

func berrizPayload(t *testing.T, edit func(map[string]any)) []byte {
	t.Helper()
	replay := map[string]any{"duration": 1174, "isDrm": false, "hls": map[string]any{"playbackUrl": berrizManifest}, "dash": nil}
	if edit != nil {
		edit(replay)
	}
	raw, err := json.Marshal(map[string]any{"code": "0000", "data": map[string]any{
		"media":            map[string]any{"mediaId": berrizID, "mediaType": "LIVE", "title": "Berriz 다시보기", "live": map[string]any{"liveStatus": "REPLAY", "replay": replay}},
		"communityArtists": []any{map[string]any{"communityArtistId": 26, "name": "작성자"}},
		"settlement":       map[string]any{"mediaSettlementToken": "must-not-enter-download-plan"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func berrizFormats() json.RawMessage {
	return json.RawMessage(`{"id":"cdn-filename","extractor_key":"Generic","webpage_url":"https://vod-edge.berriz.in/master.m3u8","formats":[{"format_id":"480","height":480,"ext":"mp4","vcodec":"h264","acodec":"aac","protocol":"m3u8_native","url":"https://vod-edge.berriz.in/480.m3u8"},{"format_id":"720","height":720,"ext":"mp4","vcodec":"h264","acodec":"aac","protocol":"m3u8_native","url":"https://vod-edge.berriz.in/720.m3u8"},{"format_id":"1080","height":1080,"ext":"mp4","vcodec":"h264","acodec":"aac","protocol":"m3u8_native","url":"https://vod-edge.berriz.in/1080.m3u8"}]}`)
}

func berrizFixtureDeps(t *testing.T, body []byte, status int, backend berrizBackend) engine.ExtractorDependencies {
	t.Helper()
	return engine.ExtractorDependencies{Ytdlp: backend,
		Cookies: berrizCookies(func(ctx context.Context, address *url.URL, dir string) ([]*http.Cookie, error) {
			if address.Hostname() != "svc-api.berriz.in" {
				t.Fatal("unexpected cookie destination", address.Hostname())
			}
			return []*http.Cookie{{Name: "session", Value: "synthetic-session"}}, nil
		}),
		HTTPClient: &http.Client{Transport: instagramRoundTripper(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "https://svc-api.berriz.in/service/v1/medias/live/replay/"+berrizID+"/playback_area_context" || r.Header.Get("Cookie") != "session=synthetic-session" || r.Header.Get("Origin") != "https://berriz.in" {
				t.Error("wrong API request", r.URL.Hostname())
			}
			response := instagramResponse(r, status, body)
			if status == http.StatusFound {
				response.Header.Set("Location", "https://evil.invalid/capture")
			}
			return response, nil
		})},
	}
}

func TestBerrizIdentitySurvivesLanguageAndManifestChanges(t *testing.T) {
	for _, language := range []string{"ko", "en", "ja"} {
		body := berrizPayload(t, func(replay map[string]any) {
			replay["hls"] = map[string]string{"playbackUrl": berrizManifest + "&refresh=" + language}
		})
		deps := berrizFixtureDeps(t, body, 200, berrizBackend{discover: func(context.Context, string, string) (json.RawMessage, error) { return berrizFormats(), nil }})
		address := "https://berriz.in/" + language + "/kiiikiii/live/replay/" + berrizID + "/?ref=" + language
		plan, err := engine.NewBerrizExtractor(deps).Discover(t.Context(), engine.ExtractRequest{URL: address})
		if err != nil {
			t.Fatal(err)
		}
		targets, err := plan.MediaTargets()
		if err != nil || len(targets) != 1 || targets[0].TargetKey != "berriz:"+berrizID || targets[0].SourceURL != address {
			t.Fatal(targets, err)
		}
	}
}

func TestBerrizReplayDiscoveryAndBoundDownload(t *testing.T) {
	for _, quality := range []string{"best", "720", "480"} {
		t.Run(quality, func(t *testing.T) {
			stage, err := os.MkdirTemp(engineTestRoot(t, "engine-parsers"), "berriz-plan-")
			if err != nil {
				t.Fatal(err)
			}
			discoverCalls, saveCalls := 0, 0
			options := engine.Options{Type: "video", Quality: quality}
			backend := berrizBackend{discover: func(ctx context.Context, address, dir string) (json.RawMessage, error) {
				discoverCalls++
				if address != berrizManifest || dir != stage {
					t.Fatal("manifest changed", address, dir)
				}
				return berrizFormats(), nil
			}, save: func(ctx context.Context, target engine.YtdlpTarget, dir string, opts engine.Options) (string, error) {
				saveCalls++
				if opts.Type != options.Type || opts.Quality != quality || target.ID != berrizID || target.ExtractorKey != "Berriz" || target.WebpageURL != berrizOriginal {
					t.Fatal("bound metadata changed", target, opts)
				}
				file := filepath.Join(dir, "media.mp4")
				return file, os.WriteFile(file, []byte("fixture video"), 0600)
			}}
			deps := berrizFixtureDeps(t, berrizPayload(t, nil), 200, backend)
			plan, err := engine.DiscoverWithExtractors(t.Context(), engine.ExtractRequest{URL: berrizOriginal, WorkDir: stage, Options: options}, engine.DefaultExtractors(deps))
			if err != nil || plan.Extractor != "berriz" || plan.Transfer != engine.TransferYtdlp {
				t.Fatal(plan, err)
			}
			targets, err := plan.MediaTargets()
			if err != nil || len(targets) != 1 {
				t.Fatal(targets, err)
			}
			target := targets[0]
			if target.TargetKey != "berriz:"+berrizID || target.SourceURL != berrizOriginal || target.Title != "Berriz 다시보기" || target.ExternalID != berrizID || target.PlatformName != "Berriz" || target.OwnerID != "26" || target.OwnerName != "작성자" || target.Duration == nil || *target.Duration != 1174 {
				t.Fatal(target)
			}
			deps.Ytdlp = nil
			paths, err := plan.Download(t.Context(), stage, nil)
			if err != nil || len(paths) != 1 || discoverCalls != 1 || saveCalls != 1 {
				t.Fatal(paths, discoverCalls, saveCalls, err)
			}
		})
	}
}

func TestBerrizRejectsInvalidPlaybackBeforeBackend(t *testing.T) {
	for _, mode := range []string{"drm", "unknown-drm", "no-hls", "hostile-cdn", "http-cdn", "credentials", "not-manifest", "wrong-id", "not-replay", "unknown-type", "zero-duration", "invalid-json", "too-large", "unauthenticated", "denied", "missing", "rate-limit", "expired-session", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			body := berrizPayload(t, func(replay map[string]any) {
				switch mode {
				case "drm":
					replay["isDrm"] = true
				case "unknown-drm":
					delete(replay, "isDrm")
				case "no-hls":
					replay["hls"] = nil
				case "zero-duration":
					replay["duration"] = 0
				default:
					bad := map[string]string{"hostile-cdn": "https://vod-edge.berriz.in.evil.invalid/master.m3u8", "http-cdn": "http://vod-edge.berriz.in/master.m3u8", "credentials": "https://user:pass@vod-edge.berriz.in/master.m3u8", "not-manifest": "https://vod-edge.berriz.in/file.mp4"}[mode]
					if bad != "" {
						replay["hls"] = map[string]string{"playbackUrl": bad}
					}
				}
			})
			switch mode {
			case "wrong-id":
				body = []byte(strings.Replace(string(body), berrizID, "another-id", 1))
			case "not-replay":
				body = []byte(strings.Replace(string(body), `"liveStatus":"REPLAY"`, `"liveStatus":"ON_AIR"`, 1))
			case "unknown-type":
				body = []byte(strings.Replace(string(body), `"mediaType":"LIVE"`, `"mediaType":"VOD"`, 1))
			case "invalid-json":
				body = []byte(`<html>login required</html>`)
			case "too-large":
				body = []byte(strings.Repeat("x", (2<<20)+1))
			case "expired-session":
				body = []byte(`{"code":"FS_ER4020","message":"private-server-text"}`)
			}
			status := map[string]int{"unauthenticated": 401, "denied": 403, "missing": 404, "rate-limit": 429, "redirect": 302}[mode]
			if status == 0 {
				status = 200
			}
			calls := 0
			deps := berrizFixtureDeps(t, body, status, berrizBackend{discover: func(context.Context, string, string) (json.RawMessage, error) { calls++; return berrizFormats(), nil }})
			plan, err := engine.NewBerrizExtractor(deps).Discover(t.Context(), engine.ExtractRequest{URL: berrizOriginal})
			if err == nil || len(plan.YtdlpTargets) != 0 || calls != 0 {
				t.Fatal("invalid playback reached backend", plan, calls, err)
			}
			want := map[string]error{"drm": engine.ErrUnsupportedMediaFormat, "too-large": engine.ErrOutputLimit, "unauthenticated": engine.ErrAuthenticationRequired, "denied": engine.ErrAccessDenied, "missing": engine.ErrMediaUnavailable, "rate-limit": engine.ErrRateLimited, "expired-session": engine.ErrAuthenticationRequired}[mode]
			if want != nil && !errors.Is(err, want) {
				t.Fatal(err, want)
			}
			if strings.Contains(err.Error(), "private-server-text") {
				t.Fatal("server response leaked")
			}
		})
	}
}

func TestBerrizRejectsInvalidURLsAndCancellation(t *testing.T) {
	for _, address := range []string{"https://berriz.in/ko/kiiikiii/", "https://berriz.in/ko/kiiikiii/live/replay/not-a-uuid", "https://berriz.in.evil.invalid/ko/kiiikiii/live/replay/" + berrizID, "https://user:pass@berriz.in/ko/kiiikiii/live/replay/" + berrizID, "https://berriz.in:8443/ko/kiiikiii/live/replay/" + berrizID} {
		_, err := engine.NewBerrizExtractor(engine.ExtractorDependencies{}).Discover(t.Context(), engine.ExtractRequest{URL: address})
		if err == nil {
			t.Fatal("invalid address accepted", address)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := engine.NewBerrizExtractor(engine.ExtractorDependencies{}).Discover(ctx, engine.ExtractRequest{URL: berrizOriginal})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestBerrizCookieFilesAreScopedAndUnchanged(t *testing.T) {
	root := engineTestRoot(t, "engine-parsers")
	file := filepath.Join(root, "berriz-source-cookies.txt")
	const body = "# Netscape HTTP Cookie File\r\n#HttpOnly_.berriz.in\tTRUE\t/\tTRUE\t0\tsession\tsynthetic-session\r\nberriz.in\tFALSE\t/\tTRUE\t0\thost-only\twrong-host\r\n.evil.invalid\tTRUE\t/\tTRUE\t0\tunrelated\tprivate\r\n.berriz.in\tTRUE\t/service-other\tTRUE\t0\twrong-path\tprivate\r\n.berriz.in\tTRUE\t/\tTRUE\t1\texpired\tprivate\r\n"
	if err := os.WriteFile(file, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	address, _ := url.Parse("https://svc-api.berriz.in/service/v1/medias/live/replay/" + berrizID + "/playback_area_context")
	y := engine.Ytdlp{CookieBrowser: "invalid-browser", CookieFile: file}
	cookies, err := y.Cookies(t.Context(), address, "")
	if err != nil || len(cookies) != 1 || cookies[0].Name != "session" || cookies[0].Value != "synthetic-session" {
		t.Fatal(cookies, err)
	}
	unchanged, err := os.ReadFile(file)
	if err != nil || string(unchanged) != body {
		t.Fatal("source cookie file changed", err)
	}
	for _, invalid := range []string{"not a Netscape file", "# Netscape HTTP Cookie File\n.berriz.in\tTRUE\t/\tTRUE\tinvalid\tsession\tsynthetic\n", "# Netscape HTTP Cookie File\n.berriz.in\tTRUE\t/\tTRUE\t0\tbad name\tsynthetic\n"} {
		if err := os.WriteFile(file, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := y.Cookies(t.Context(), address, ""); err == nil {
			t.Fatal("malformed cookie file accepted")
		}
	}
}

// The regular process runner executes this test binary as an offline cookie
// exporter, exercising real process arguments, file lifetime and error handling.
func runBerrizCookieFixture() bool {
	mode := os.Getenv("ASTERODIA_BERRIZ_COOKIE_FIXTURE")
	if mode == "" || len(os.Args) < 2 || os.Args[1] != "--ignore-config" {
		return false
	}
	file := ""
	for i, arg := range os.Args {
		if arg == "--cookies" && i+1 < len(os.Args) {
			file = os.Args[i+1]
		}
	}
	rel, err := filepath.Rel(os.Getenv("ASTERODIA_TEST_ROOT"), file)
	if err != nil || !filepath.IsLocal(rel) || file == "" {
		os.Exit(2)
	}
	if mode != "failure" {
		if err := os.WriteFile(file, []byte("# Netscape HTTP Cookie File\n.berriz.in\tTRUE\t/\tTRUE\t0\tsession\tsynthetic-browser-session\n"), 0600); err != nil {
			os.Exit(2)
		}
	}
	fmt.Fprintln(os.Stderr, "synthetic-private-browser-output")
	if mode == "missing-url" || mode == "failure" {
		os.Exit(2)
	}
	os.Exit(0)
	return true
}

func TestBerrizBrowserCookieExportCleanup(t *testing.T) {
	for _, mode := range []string{"success", "missing-url", "failure"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("ASTERODIA_BERRIZ_COOKIE_FIXTURE", mode)
			dir, err := os.MkdirTemp(engineTestRoot(t, "engine-parsers"), "berriz-auth-")
			if err != nil {
				t.Fatal(err)
			}
			address, _ := url.Parse("https://svc-api.berriz.in/service/v1/medias/live/replay/" + berrizID + "/playback_area_context")
			observed := 0
			ctx := engine.WithOutputObserver(t.Context(), func(engine.Output) { observed++ })
			y := engine.Ytdlp{Executable: os.Args[0], CookieBrowser: "firefox"}
			cookies, err := y.Cookies(ctx, address, dir)
			if mode == "failure" {
				if !errors.Is(err, engine.ErrAuthenticationRequired) {
					t.Fatal(err)
				}
			} else if err != nil || len(cookies) != 1 || cookies[0].Value != "synthetic-browser-session" {
				t.Fatal(cookies, err)
			}
			entries, readErr := os.ReadDir(dir)
			if readErr != nil || len(entries) != 0 || observed != 0 {
				t.Fatal("export survived or leaked diagnostics", entries, readErr, observed)
			}
		})
	}
}

// A local HLS source exercises Berriz metadata projection with the real yt-dlp
// frozen-info downloader and FFmpeg, without using account data or remote media.
func TestBerrizHLSWithRealEngine(t *testing.T) {
	executable, probe := os.Getenv("ASTERODIA_TEST_YTDLP_EXE"), os.Getenv("ASTERODIA_TEST_FFPROBE_EXE")
	if executable == "" || probe == "" {
		t.Skip("yt-dlp and FFmpeg test executables required")
	}
	if !filepath.IsAbs(executable) || !filepath.IsAbs(probe) {
		t.Fatal("absolute engine paths required")
	}
	root := engineTestRoot(t, "engine-parsers")
	source, err := os.MkdirTemp(root, "berriz-hls-source-")
	if err != nil {
		t.Fatal(err)
	}
	var process engine.ProcessRunner
	generated, err := process.Run(t.Context(), engine.ProcessRequest{Executable: filepath.Join(filepath.Dir(probe), "ffmpeg.exe"), Directory: source, Timeout: 30 * time.Second,
		Arguments: []string{"-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=10:duration=2", "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-g", "5", "-c:a", "aac", "-shortest", "-f", "hls", "-hls_time", "0.5", "-hls_list_size", "0", "index.m3u8"}})
	if err != nil {
		t.Fatal("generate HLS", err, generated.Stderr)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(source)))
	defer server.Close()
	cookieFile := filepath.Join(root, "berriz-real-engine-cookies.txt")
	const cookieBody = "# Netscape HTTP Cookie File\n.berriz.in\tTRUE\t/\tTRUE\t0\tsession\tsynthetic-session\n"
	if err := os.WriteFile(cookieFile, []byte(cookieBody), 0600); err != nil {
		t.Fatal(err)
	}
	y := &engine.Ytdlp{Executable: executable, FFmpegLocation: filepath.Dir(probe), NoPlaylist: true, CookieFile: cookieFile}
	managed, err := cookies.Open(filepath.Join(root, "berriz-managed-cookies.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if err := managed.ImportFile(cookieFile); err != nil {
		t.Fatal(err)
	}
	y.Auth = &engine.ManagedAuth{Store: managed}
	backend := berrizBackend{discover: func(ctx context.Context, address, dir string) (json.RawMessage, error) {
		if address != berrizManifest {
			t.Fatal("lost signed manifest")
		}
		return y.Discover(ctx, server.URL+"/index.m3u8", dir)
	}, save: y.DownloadSingle}
	deps := berrizFixtureDeps(t, berrizPayload(t, nil), 200, backend)
	deps.Cookies = y
	stage, err := os.MkdirTemp(root, "berriz-hls-stage-")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	plan, err := engine.NewBerrizExtractor(deps).Discover(ctx, engine.ExtractRequest{URL: berrizOriginal, WorkDir: stage})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := plan.Download(ctx, stage, nil)
	if err != nil || len(paths) != 1 {
		t.Fatal("real HLS download failed", paths, err)
	}
	result, err := process.Run(ctx, engine.ProcessRequest{Executable: probe, Directory: stage, Timeout: 10 * time.Second, Arguments: []string{"-v", "error", "-show_entries", "stream=codec_type", "-of", "json", paths[0]}})
	if err != nil {
		t.Fatal(err, result.Stderr)
	}
	var media struct {
		Streams []struct {
			Kind string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &media); err != nil {
		t.Fatal(err)
	}
	video, audio := false, false
	for _, stream := range media.Streams {
		video = video || stream.Kind == "video"
		audio = audio || stream.Kind == "audio"
	}
	if !video || !audio {
		t.Fatal("missing media streams", result.Stdout)
	}
	unchanged, err := os.ReadFile(cookieFile)
	if err != nil || string(unchanged) != cookieBody {
		t.Fatal("configured cookie file changed", err)
	}
}

func TestBerrizAuthenticationFailureDoesNotStartDiscovery(t *testing.T) {
	for _, mode := range []string{"missing-source", "empty-source", "expired-source", "cancelled-source"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			deps := engine.ExtractorDependencies{Ytdlp: berrizBackend{discover: func(context.Context, string, string) (json.RawMessage, error) { calls++; return berrizFormats(), nil }},
				HTTPClient: &http.Client{Transport: instagramRoundTripper(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected API request") })}}
			if mode != "missing-source" {
				deps.Cookies = berrizCookies(func(context.Context, *url.URL, string) ([]*http.Cookie, error) {
					if mode == "cancelled-source" {
						return nil, context.Canceled
					}
					if mode == "expired-source" {
						return nil, engine.ErrAuthenticationRequired
					}
					return nil, nil
				})
			}
			_, err := engine.NewBerrizExtractor(deps).Discover(t.Context(), engine.ExtractRequest{URL: berrizOriginal})
			want := engine.ErrAuthenticationRequired
			if mode == "cancelled-source" {
				want = context.Canceled
			}
			if !errors.Is(err, want) || calls != 0 {
				t.Fatal(calls, err)
			}
		})
	}
}
