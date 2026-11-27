package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/netguard"
)

var berrizReplayPath = regexp.MustCompile(`^/[A-Za-z]{2}(?:-[A-Za-z]{2,4})?/[A-Za-z0-9_-]+/live/replay/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})/?$`)

type BerrizExtractor struct{ deps ExtractorDependencies }

func NewBerrizExtractor(deps ExtractorDependencies) *BerrizExtractor {
	return &BerrizExtractor{deps: deps}
}
func (*BerrizExtractor) Name() string                { return "berriz" }
func (*BerrizExtractor) Match(address *url.URL) bool { return siteHost(address, "berriz.in") }

type berrizPlayback struct {
	Duration float64 `json:"duration"`
	IsDRM    *bool   `json:"isDrm"`
	HLS      *struct {
		URL string `json:"playbackUrl"`
	} `json:"hls"`
}

type berrizReplayResponse struct {
	Code string `json:"code"`
	Data struct {
		Media struct {
			ID    string `json:"mediaId"`
			Type  string `json:"mediaType"`
			Title string `json:"title"`
			Live  *struct {
				Status string          `json:"liveStatus"`
				Replay *berrizPlayback `json:"replay"`
			} `json:"live"`
		} `json:"media"`
		Artists []struct {
			ID   json.Number `json:"communityArtistId"`
			Name string      `json:"name"`
		} `json:"communityArtists"`
	} `json:"data"`
}

func berrizReplayID(raw string) (string, error) {
	if _, err := Select(raw, Options{}); err != nil {
		return "", err
	}
	address, _ := url.Parse(raw)
	match := berrizReplayPath.FindStringSubmatch(address.EscapedPath())
	if !siteHost(address, "berriz.in") || (address.Port() != "" && address.Port() != "443") || len(match) == 0 {
		return "", fmt.Errorf("%w: Berriz LIVE 다시보기 링크를 입력하세요", ErrUnsupportedURL)
	}
	return strings.ToLower(match[1]), nil
}

func (b *BerrizExtractor) Discover(ctx context.Context, req ExtractRequest) (DownloadPlan, error) {
	if err := ctx.Err(); err != nil {
		return DownloadPlan{}, err
	}
	id, err := berrizReplayID(req.URL)
	if err != nil {
		return DownloadPlan{}, err
	}
	if b.deps.Ytdlp == nil {
		return DownloadPlan{}, fmt.Errorf("%w: Berriz 다시보기에는 yt-dlp 엔진이 필요합니다", ErrUnsupportedURL)
	}
	data, err := b.replay(ctx, req, id)
	if err != nil {
		return DownloadPlan{}, err
	}
	media := data.Data.Media
	replay := media.Live.Replay
	if replay.IsDRM == nil {
		return DownloadPlan{}, fmt.Errorf("Berriz 다시보기의 DRM 여부를 확인할 수 없습니다")
	}
	if *replay.IsDRM {
		return DownloadPlan{}, fmt.Errorf("%w: DRM으로 보호된 Berriz 영상은 저장할 수 없습니다", ErrUnsupportedMediaFormat)
	}
	if replay.Duration <= 0 || math.IsNaN(replay.Duration) || math.IsInf(replay.Duration, 0) {
		return DownloadPlan{}, fmt.Errorf("Berriz 다시보기의 길이가 올바르지 않습니다")
	}
	if replay.HLS == nil || !berrizHLSAllowed(replay.HLS.URL) {
		return DownloadPlan{}, fmt.Errorf("Berriz 다시보기에 유효한 HLS 재생 주소가 없습니다")
	}
	// The backend resolves formats only. Identity and library metadata belong to
	// the original replay, never to a CDN filename or expiring manifest URL.
	raw, err := b.deps.Ytdlp.Discover(ctx, replay.HLS.URL, req.WorkDir)
	if err != nil {
		return DownloadPlan{}, err
	}
	var projected map[string]json.RawMessage
	if json.Unmarshal(raw, &projected) != nil || projected == nil {
		return DownloadPlan{}, fmt.Errorf("Berriz HLS 조회 결과가 올바르지 않습니다")
	}
	if entries := projected["entries"]; len(entries) != 0 && string(entries) != "null" {
		return DownloadPlan{}, fmt.Errorf("Berriz 다시보기 한 건의 재생 정보가 필요합니다")
	}
	set := func(key string, value any) { projected[key], _ = json.Marshal(value) }
	set("id", id)
	set("extractor", "berriz")
	set("extractor_key", "Berriz")
	set("webpage_url", req.URL)
	title := strings.TrimSpace(media.Title)
	if title == "" {
		title = "Berriz " + id
	}
	set("title", title)
	set("duration", replay.Duration)
	set("uploader_id", "")
	set("uploader", "")
	if len(data.Data.Artists) > 0 {
		artist := data.Data.Artists[0]
		set("uploader_id", artist.ID.String())
		set("uploader", artist.Name)
	}
	raw, err = json.Marshal(projected)
	if err != nil {
		return DownloadPlan{}, err
	}
	targets, err := ParseYtdlpTargets(raw, req.URL)
	if err != nil {
		return DownloadPlan{}, err
	}
	if len(targets) != 1 {
		return DownloadPlan{}, fmt.Errorf("Berriz 다시보기 한 건의 다운로드 대상이 필요합니다")
	}
	targets[0].TargetKey = "berriz:" + id
	plan := ytdlpDownloadPlan(req, targets, b.deps.Ytdlp)
	if _, err := plan.MediaTargets(); err != nil {
		return DownloadPlan{}, err
	}
	return plan, nil
}

func berrizHLSAllowed(raw string) bool {
	address, err := url.Parse(raw)
	return err == nil && address.Scheme == "https" && address.User == nil && address.Fragment == "" &&
		strings.EqualFold(address.Hostname(), "vod-edge.berriz.in") && (address.Port() == "" || address.Port() == "443") &&
		strings.EqualFold(path.Ext(address.EscapedPath()), ".m3u8")
}

func (b *BerrizExtractor) replay(ctx context.Context, req ExtractRequest, id string) (berrizReplayResponse, error) {
	var data berrizReplayResponse
	address, _ := url.Parse("https://svc-api.berriz.in/service/v1/medias/live/replay/" + id + "/playback_area_context")
	source := b.deps.Cookies
	if source == nil {
		source, _ = b.deps.Ytdlp.(CookieSource)
	}
	if source == nil {
		return data, ErrAuthenticationRequired
	}
	cookies, err := source.Cookies(ctx, address, req.WorkDir)
	if err != nil {
		return data, err
	}
	if len(cookies) == 0 {
		return data, ErrAuthenticationRequired
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
	if err != nil {
		return data, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Origin", "https://berriz.in")
	request.Header.Set("Referer", "https://berriz.in/")
	for _, cookie := range cookies {
		if cookie == nil || cookie.Valid() != nil {
			return data, ErrAuthenticationRequired
		}
		request.AddCookie(cookie)
	}
	client := netguard.NewClient()
	if b.deps.HTTPClient != nil {
		copy := *b.deps.HTTPClient
		client = &copy
	} else {
		defer client.CloseIdleConnections()
	}
	client.Jar = nil
	// Cookies are scoped to the fixed API origin and must never follow a redirect.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		if wrapped, ok := err.(*url.Error); ok {
			err = wrapped.Err
		}
		return data, fmt.Errorf("Berriz 재생 정보 조회 실패: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if classified := galleryDLFailure("", "HTTP "+strconv.Itoa(response.StatusCode)); classified != nil {
			return data, classified
		}
		return data, fmt.Errorf("Berriz 재생 정보 조회 실패: HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil {
		return data, err
	}
	if len(body) > 2<<20 {
		return data, ErrOutputLimit
	}
	if json.Unmarshal(body, &data) != nil {
		return data, fmt.Errorf("Berriz 재생 정보 JSON을 해석하지 못했습니다")
	}
	if data.Code != "0000" {
		switch data.Code {
		case "FS_ER4020":
			return data, ErrAuthenticationRequired
		default:
			return data, fmt.Errorf("Berriz 재생 정보를 조회하지 못했습니다")
		}
	}
	media := data.Data.Media
	if !strings.EqualFold(media.ID, id) || media.Type != "LIVE" || media.Live == nil || media.Live.Status != "REPLAY" || media.Live.Replay == nil {
		return data, fmt.Errorf("Berriz 응답이 요청한 다시보기와 일치하지 않습니다")
	}
	if err := ctx.Err(); err != nil {
		return data, err
	}
	return data, nil
}
