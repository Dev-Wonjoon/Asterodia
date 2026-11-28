package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	whatwgurl "github.com/nlnwa/whatwg-url/url"
)

type Ytdlp struct {
	Executable     string
	JSRuntime      string
	FFmpegLocation string
	Auth           *ManagedAuth
	CookieBrowser  string
	CookieFile     string
	NoPlaylist     bool
	runner         ProcessRunner
}

var ErrUnsupportedMediaFormat = errors.New("요청한 종류·화질에 맞는 MP4·M4A 포맷이 없습니다")

func ytdlpMaxHeight(quality string) (int, error) {
	height, ok := map[string]int{"": 0, "best": 0, "audio": 0, "480": 480, "720": 720, "1080": 1080, "2k": 1440, "4k": 2160, "8k": 4320}[quality]
	if !ok {
		return 0, fmt.Errorf("지원하지 않는 화질 옵션: %q", quality)
	}
	return height, nil
}

// ValidateDirectDownload checks each resolved entry before any target is staged.
// The identity parser remains independent of format availability for compatibility.
func (target YtdlpTarget) ValidateDirectDownload(options Options) error {
	if target.downloadInfo == "" {
		return ErrUnsupportedMediaFormat
	}
	var info ytdlpDownloadInfo
	if err := json.Unmarshal([]byte(target.downloadInfo), &info); err != nil {
		return fmt.Errorf("확정한 포맷 정보 해석 실패: %w", err)
	}
	height, err := ytdlpMaxHeight(options.Quality)
	if err != nil {
		return err
	}
	_, flexible, err := downloadOutputFormat(options)
	if err != nil {
		return err
	}
	_, _, err = selectYtdlpFormats(info.Formats, options.Type == "audio" || options.Quality == "audio", height, true, flexible)
	return err
}

// Discover는 Select에서 검증한 원본 URL을 받아 yt-dlp의 조회 결과를 반환한다.
func (y *Ytdlp) Discover(
	ctx context.Context,
	originalURL string,
	workDir string,
) (json.RawMessage, error) {
	args := []string{
		"--ignore-config",
		"--no-plugin-dirs",
		"--no-cache-dir",
		"--no-cookies",
		"--no-cookies-from-browser",
		"--no-js-runtimes",
		"--no-remote-components",
		"--encoding", "utf-8",
		"--abort-on-error",
		"--simulate",
		"--dump-single-json",
		"--no-quiet",
	}
	args = append(args, networkArguments(ctx)...)

	if y.JSRuntime != "" {
		args = append(args, "--js-runtimes", y.JSRuntime)
	}
	auth, finishAuth, err := prepareAuthentication(ctx, y.Auth, y.CookieBrowser, y.CookieFile, originalURL, workDir)
	if err != nil {
		return nil, err
	}
	defer finishAuth(false)
	args = append(args, auth...)
	if y.NoPlaylist {
		args = append(args, "--no-playlist")
	}
	args = append(args, "--", originalURL)

	var discoveryJSON string
	result, err := y.runner.Run(ctx, ProcessRequest{
		Executable: y.Executable,
		Arguments:  args,
		Directory:  workDir,
		Timeout:    2 * time.Minute,
		OnStdoutLine: func(line string) error {
			if strings.HasPrefix(strings.TrimSpace(line), "{") && json.Valid([]byte(line)) {
				discoveryJSON = line
			}
			return nil
		},
	})

	if err == nil {
		err = finishAuth(true)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrStopUnconfirmed) || errors.Is(err, ErrOutputLimit) {
			return nil, fmt.Errorf("yt-dlp 대상 조회 실패: %w", err)
		}
		if stderr := strings.TrimSpace(result.Stderr); stderr != "" {
			if errors.Is(galleryDLFailure("", stderr), ErrUnsupportedURL) {
				return nil, ErrUnsupportedURL
			}
			return nil, fmt.Errorf("yt-dlp 대상 조회 실패: %w\n%s", err, stderr)
		}
		return nil, fmt.Errorf("yt-dlp 대상 조회 실패: %w", err)
	}

	output := bytes.TrimSpace([]byte(discoveryJSON))
	if len(output) == 0 || output[0] != '{' || !json.Valid(output) {
		return nil, fmt.Errorf("yt-dlp 조회 결과가 올바른 JSON 객체가 아닙니다")
	}

	return json.RawMessage(output), nil
}

func selectYtdlpFormats(
	formats []ytdlpDirectFormat,
	audio bool,
	maxHeight int,
	canMerge bool,
	flexible bool,
) ([]ytdlpDirectFormat, string, error) {
	var videoOnly, audioOnly, combined *ytdlpDirectFormat
	for i := range formats {
		format := &formats[i]
		if format.VideoCodec == "none" {
			// MP3 extraction can use any supported audio source. Keep MP4
			// video merges on M4A to preserve their existing codec selection.
			if audio || flexible || format.Ext == "m4a" {
				audioOnly = format
			}
			continue
		}
		if !audio && !flexible && format.Ext != "mp4" {
			continue
		}
		if !audio && maxHeight > 0 &&
			(format.Height <= 0 || format.Height > maxHeight) {
			continue
		}
		if format.AudioCodec == "none" {
			videoOnly = format
		} else {
			combined = format
		}
	}

	switch {
	case audio && audioOnly != nil:
		return []ytdlpDirectFormat{*audioOnly}, "bestaudio", nil
	case !audio && videoOnly != nil && audioOnly != nil && canMerge:
		return []ytdlpDirectFormat{*videoOnly, *audioOnly}, "bestvideo+bestaudio", nil
	case combined != nil:
		return []ytdlpDirectFormat{*combined}, "best", nil
	case !audio && videoOnly != nil && audioOnly != nil:
		return nil, "", fmt.Errorf("분리 포맷을 병합하려면 FFmpeg 절대 경로가 필요합니다")
	case !audio && videoOnly != nil:
		return []ytdlpDirectFormat{*videoOnly}, "bestvideo", nil
	default:
		return nil, "", ErrUnsupportedMediaFormat
	}
}

func (y *Ytdlp) DownloadSingle(
	ctx context.Context,
	target YtdlpTarget,
	stageDir string,
	options Options,
) (string, error) {
	switch options.Type {
	case "", "video", "audio":
	default:
		return "", fmt.Errorf("지원하지 않는 다운로드 유형: %q", options.Type)
	}

	maxHeight, err := ytdlpMaxHeight(options.Quality)
	if err != nil {
		return "", err
	}

	audio := options.Type == "audio" || options.Quality == "audio"
	output, advanced, err := downloadOutputFormat(options)
	if err != nil {
		return "", err
	}
	if advanced {
		if err = (VideoEditor{FFmpeg: y.FFmpegLocation}).CheckFormat(ctx, output); err != nil {
			return "", err
		}
	}
	extension := "mp4"
	if audio {
		extension = "mp3"
		if !filepath.IsAbs(y.FFmpegLocation) {
			return "", fmt.Errorf("오디오 추출에는 FFmpeg 절대 경로가 필요합니다")
		}
	}
	if !audio && !advanced && output.Extension == "mkv" {
		extension = "mkv"
	}

	if target.downloadInfo == "" {
		return "", ErrUnsupportedMediaFormat
	}

	var info ytdlpDownloadInfo
	if err := json.Unmarshal([]byte(target.downloadInfo), &info); err != nil {
		return "", fmt.Errorf("확정한 포맷 정보 해석 실패: %w", err)
	}

	selected, selector, err := selectYtdlpFormats(
		info.Formats, audio, maxHeight, filepath.IsAbs(y.FFmpegLocation), advanced,
	)
	if err != nil {
		return "", err
	}

	if advanced {
		extension = selected[0].Ext
		if len(selected) > 1 {
			extension = "mkv"
		}
	}
	info.Formats = selected
	payload, err := json.Marshal(info)
	if err != nil {
		return "", fmt.Errorf("확정한 포맷 정보 구성 실패: %w", err)
	}
	expectedFormatID := selected[0].ID
	merge := len(selected) == 2
	streaming := false
	for _, format := range selected {
		streaming = streaming || format.Protocol == "m3u8" || format.Protocol == "m3u8_native" || format.Protocol == "http_dash_segments"
	}
	if streaming && !filepath.IsAbs(y.FFmpegLocation) {
		return "", fmt.Errorf("HLS·DASH 처리에는 FFmpeg 절대 경로가 필요합니다")
	}
	if merge {
		expectedFormatID += "+" + selected[1].ID
	}

	if !filepath.IsAbs(stageDir) {
		return "", fmt.Errorf("작업 폴더는 절대 경로여야 합니다")
	}
	entries, err := os.ReadDir(stageDir)
	if err != nil {
		return "", fmt.Errorf("작업 폴더 읽기 실패: %w", err)
	}
	if len(entries) != 0 {
		return "", fmt.Errorf("작업 폴더가 비어 있어야 합니다")
	}

	request := ProcessRequest{
		Executable:   y.Executable,
		Directory:    stageDir,
		Timeout:      10 * time.Minute,
		Stdin:        string(payload),
		StreamOutput: true,
		Arguments: []string{
			"--ignore-config", "--no-plugin-dirs", "--no-cache-dir",
			"--no-cookies", "--no-cookies-from-browser",
			"--no-js-runtimes", "--no-remote-components",
			"--encoding", "utf-8", "--abort-on-error",
			"--no-simulate", "--no-quiet", "--progress", "--newline", "--progress-delta", "0.5",
			"--progress-template", fmt.Sprintf(`download:__ASTERODIA_PROGRESS__:{"streams":%d,"formatId":%%(info.format_id)j,"download":%%(progress.{status,downloaded_bytes,total_bytes,total_bytes_estimate,speed,eta})j}`, len(selected)),
			"--abort-on-unavailable-fragments", "--fixup", "detect_or_warn",
			"--downloader", "m3u8:native",
			"--paths", "home:" + stageDir,
			"--paths", "temp:" + stageDir,
			"--output", "media.%(ext)s",
			"--format", selector,
			"--print", "after_move:" + resultPrefix + "%(.{id,format_id,filepath})j",
		},
	}

	request.Arguments = append(request.Arguments, networkArguments(ctx)...)
	if len(networkArguments(ctx)) > 0 {
		request.Arguments = append(request.Arguments, "--downloader-args", "ffmpeg_i:-protocol_whitelist file,pipe")
	}
	request.Arguments = append(request.Arguments, "--downloader", "dash,m3u8:native", "--max-filesize", "20G")
	if audio || merge || streaming {
		if err := verifyManagedExecutable(y.FFmpegLocation); err != nil {
			return "", err
		}
		if err := verifyManagedExecutable(filepath.Join(filepath.Dir(y.FFmpegLocation), "ffprobe.exe")); err != nil {
			return "", err
		}
		request.Arguments = append(request.Arguments,
			"--ffmpeg-location", y.FFmpegLocation,
		)
	}
	if audio && !advanced {
		request.Arguments = append(request.Arguments,
			"--extract-audio", "--audio-format", "mp3", "--no-keep-video",
		)
	}
	if merge {
		request.Arguments = append(request.Arguments, "--merge-output-format", extension)
	}
	if !audio && !advanced && extension == "mkv" && !merge {
		if !filepath.IsAbs(y.FFmpegLocation) {
			return "", fmt.Errorf("MKV 변환에는 FFmpeg 절대 경로가 필요합니다")
		}
		request.Arguments = append(request.Arguments, "--ffmpeg-location", y.FFmpegLocation, "--remux-video", "mkv")
	}

	auth, finishAuth, err := prepareAuthentication(ctx, y.Auth, y.CookieBrowser, y.CookieFile, target.WebpageURL, stageDir)
	if err != nil {
		return "", err
	}
	defer finishAuth(false)
	request.Arguments = append(request.Arguments, auth...)
	request.Arguments = append(request.Arguments, "--load-info-json", "-")
	var savedJSON string
	request.OnStdoutLine = func(line string) error {
		if strings.HasPrefix(line, resultPrefix) {
			savedJSON = strings.TrimPrefix(line, resultPrefix)
		}
		return nil
	}

	result, err := y.runner.Run(ctx, request)
	if err != nil {
		if stderr := strings.TrimSpace(result.Stderr); stderr != "" {
			return "", fmt.Errorf("yt-dlp 다운로드 실패: %w\n%s", err, stderr)
		}
		return "", fmt.Errorf("yt-dlp 다운로드 실패: %w", err)
	}
	if err = finishAuth(true); err != nil {
		return "", err
	}

	var saved struct {
		ID       string `json:"id"`
		FormatID string `json:"format_id"`
		Filepath string `json:"filepath"`
	}
	if err := json.Unmarshal([]byte(savedJSON), &saved); err != nil {
		return "", fmt.Errorf("yt-dlp 저장 결과 해석 실패: %w", err)
	}
	if saved.ID != target.ID || saved.FormatID != expectedFormatID {
		return "", fmt.Errorf("저장 결과가 확정한 영상 또는 포맷과 다릅니다")
	}

	filename := "media." + extension
	relative, err := filepath.Rel(stageDir, saved.Filepath)
	if err != nil || !filepath.IsAbs(saved.Filepath) || relative != filename {
		return "", fmt.Errorf("yt-dlp가 예상한 저장 경로를 반환하지 않았습니다")
	}

	path := filepath.Join(stageDir, filename)
	fileInfo, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("저장 파일 확인 실패: %w", err)
	}
	if !fileInfo.Mode().IsRegular() || fileInfo.Size() <= 0 {
		return "", fmt.Errorf("저장 결과가 크기 0 초과의 일반 파일이어야 합니다")
	}

	entries, err = os.ReadDir(stageDir)
	if err != nil {
		return "", fmt.Errorf("저장 결과 목록 확인 실패: %w", err)
	}
	if len(entries) != 1 || entries[0].Name() != filename {
		return "", fmt.Errorf("작업 폴더에는 %s 한 개만 있어야 합니다", filename)
	}

	if advanced {
		converted := filepath.Join(stageDir, "converted."+output.Extension)
		duration := 0.0
		if target.Duration != nil {
			duration = *target.Duration
		}
		if err = (VideoEditor{FFmpeg: y.FFmpegLocation}).Convert(ctx, path, converted, output, duration, nil); err != nil {
			return "", err
		}
		if err = os.Remove(path); err != nil {
			return "", err
		}
		path = filepath.Join(stageDir, "media."+output.Extension)
		if err = os.Rename(converted, path); err != nil {
			return "", err
		}
	}
	return path, nil
}

const resultPrefix = "__ASTERODIA_RESULT__:"

type YtdlpTarget struct {
	OwnerID      string   `json:"uploader_id"`
	OwnerName    string   `json:"uploader"`
	Duration     *float64 `json:"duration"`
	ID           string   `json:"id"`
	ExtractorKey string   `json:"extractor_key"`
	Title        string   `json:"title"`
	WebpageURL   string   `json:"webpage_url"`
	TargetKey    string   `json:"-"`
	downloadInfo string
}

type ytdlpDirectFormat struct {
	raw        map[string]json.RawMessage
	ID         string `json:"format_id"`
	URL        string `json:"url"`
	Protocol   string `json:"protocol"`
	Ext        string `json:"ext"`
	Height     int    `json:"height,omitempty"`
	VideoCodec string `json:"vcodec,omitempty"`
	AudioCodec string `json:"acodec,omitempty"`
	// The extractor can require a short delay before its signed URL is usable.
	// Let yt-dlp enforce this timestamp instead of immediately receiving a 403.
	AvailableAt       *float64                `json:"available_at,omitempty"`
	HTTPHeaders       map[string]string       `json:"http_headers,omitempty"`
	DownloaderOptions *ytdlpDownloaderOptions `json:"downloader_options,omitempty"`
}

type ytdlpDownloaderOptions struct {
	raw           map[string]json.RawMessage
	HTTPChunkSize int64 `json:"http_chunk_size,omitempty"`
}

type ytdlpDownloadInfo struct {
	raw          map[string]json.RawMessage
	Type         string              `json:"_type"`
	ID           string              `json:"id"`
	Title        string              `json:"title"`
	Extractor    string              `json:"extractor,omitempty"`
	ExtractorKey string              `json:"extractor_key"`
	Formats      []ytdlpDirectFormat `json:"formats"`
}

func buildYtdlpDownloadInfo(raw json.RawMessage, target YtdlpTarget) (string, error) {
	var source struct {
		Extractor      string            `json:"extractor"`
		Formats        []json.RawMessage `json:"formats"`
		SectionStart   *float64          `json:"section_start"`
		SectionEnd     *float64          `json:"section_end"`
		AdditionalURLs json.RawMessage   `json:"additional_urls"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return "", err
	}
	if source.SectionStart != nil || source.SectionEnd != nil ||
		(len(source.AdditionalURLs) > 0 && string(source.AdditionalURLs) != "null") {
		return "", nil
	}

	candidates := source.Formats
	if len(candidates) == 0 {
		// 단일 직접 URL 결과는 포맷 필드가 영상 객체 자체에 있을 수 있다.
		candidates = []json.RawMessage{raw}
	}

	// yt-dlp가 정렬한 목록의 뒤쪽부터 적합한 포맷 하나를 고른다.
	var formats []ytdlpDirectFormat
	type plainFormat ytdlpDirectFormat
	for i, rawFormat := range candidates {
		var candidate struct {
			plainFormat
			HasDRM    json.RawMessage   `json:"has_drm"`
			Fragments []json.RawMessage `json:"fragments"`
		}
		if err := json.Unmarshal(rawFormat, &candidate); err != nil {
			return "", fmt.Errorf("yt-dlp 포맷 %d 해석 실패: %w", i+1, err)
		}

		format := ytdlpDirectFormat(candidate.plainFormat)
		format.raw = decodeMetadata(rawFormat)
		drm := string(candidate.HasDRM)
		hasVideo := (format.Ext == "mp4" || format.Ext == "webm" || format.Ext == "mkv" || format.Ext == "mov") && format.VideoCodec != "none"
		audioOnly := false
		switch format.Ext {
		case "m4a", "mp4", "webm", "opus", "mp3", "ogg", "oga", "wav", "flac", "aac":
			audioOnly = format.VideoCodec == "none" && format.AudioCodec != "none"
		}

		if format.ID == "" || (!hasVideo && !audioOnly) ||
			(drm != "" && drm != "null" && drm != "false") {
			continue
		}

		mediaURL, err := url.Parse(format.URL)
		if err != nil || mediaURL.Hostname() == "" || mediaURL.User != nil ||
			(mediaURL.Scheme != "http" && mediaURL.Scheme != "https") {
			continue
		}
		if format.Protocol == "" {
			format.Protocol = mediaURL.Scheme
		}
		switch format.Protocol {
		case "http", "https", "m3u8", "m3u8_native", "http_dash_segments":
		default:
			continue
		}
		// Preserve the extractor's bounded Range requests when freezing formats.
		if len(candidate.Fragments) > 0 && (format.Protocol == "http" || format.Protocol == "https") {
			continue
		}
		if format.DownloaderOptions != nil && format.DownloaderOptions.HTTPChunkSize < 0 {
			format.DownloaderOptions.HTTPChunkSize = 0
		}

		format.HTTPHeaders = make(map[string]string)
		for name, value := range candidate.HTTPHeaders {
			if strings.ContainsAny(value, "\r\n\x00") {
				continue
			}
			if name != "" && !strings.ContainsAny(name, ":\r\n\x00 \t") {
				format.HTTPHeaders[name] = value
			}
		}

		formats = append(formats, format)
	}
	if len(formats) == 0 {
		return "", nil
	}

	payload, err := json.Marshal(ytdlpDownloadInfo{
		raw:          decodeMetadata(raw),
		Type:         "video",
		ID:           target.ID,
		Title:        target.Title,
		Extractor:    source.Extractor,
		ExtractorKey: target.ExtractorKey,
		Formats:      formats,
	})
	return string(payload), err
}

func ParseYtdlpTargets(
	raw json.RawMessage,
	originalURL string,
) ([]YtdlpTarget, error) {
	var targets []YtdlpTarget
	if err := collectYtdlpTargets(raw, 0, &targets); err != nil {
		return nil, err
	}

	// 기존 단일 영상의 URL 대체 규칙을 유지한다.
	if len(targets) == 1 && targets[0].WebpageURL == "" {
		var root struct {
			Entries    json.RawMessage `json:"entries"`
			WebpageURL *string         `json:"webpage_url"`
		}
		if err := json.Unmarshal(raw, &root); err != nil {
			return nil, err
		}
		if root.Entries == nil && root.WebpageURL == nil {
			targets[0].WebpageURL = originalURL
		}
	}

	seen := make(map[string]struct{}, len(targets))
	for i := range targets {
		key, err := ytdlpTargetKey(targets[i])
		if err != nil {
			return nil, fmt.Errorf("yt-dlp %d번째 대상 식별 실패: %w", i+1, err)
		}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("yt-dlp 대상 키가 중복됩니다: %s", key)
		}
		seen[key] = struct{}{}
		targets[i].TargetKey = key
	}

	return targets, nil
}

var youtubeVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// JSON.stringify는 U+2028과 U+2029를 그대로 쓴다.
// 리터럴 역슬래시는 쌍으로 보존한다.
var ytdlpJSONEscapes = strings.NewReplacer(
	`\\`, `\\`,
	`\u2028`, "\u2028",
	`\u2029`, "\u2029",
)

func ytdlpTargetKey(target YtdlpTarget) (string, error) {
	hasControl := func(r rune) bool { return r < 0x20 || r == 0x7f }
	if strings.ContainsFunc(target.ID, hasControl) ||
		strings.ContainsFunc(target.ExtractorKey, hasControl) {
		return "", fmt.Errorf("대상 식별 정보에 제어 문자가 있습니다")
	}

	if strings.ContainsFunc(target.WebpageURL, func(r rune) bool {
		return r <= 0x20 || r == 0x7f
	}) {
		return "", ErrInvalidURL
	}

	page, err := whatwgurl.Parse(target.WebpageURL)
	if err != nil || (page.Scheme() != "http" && page.Scheme() != "https") ||
		page.Host() == "" || page.Username() != "" || page.Password() != "" {
		return "", ErrInvalidURL
	}

	name := strings.ToLower(target.ExtractorKey)
	if name == "youtube" {
		if !youtubeVideoID.MatchString(target.ID) {
			return "", fmt.Errorf("YouTube 영상 ID가 올바르지 않습니다")
		}
		return "youtube:" + target.ID, nil
	}

	origin := page.Scheme() + "://" + page.Host()
	identity := [4]any{name, origin, target.ID, nil}
	if name == "generic" {
		identity[3] = page.Href(false)
	}

	var payload bytes.Buffer
	encoder := json.NewEncoder(&payload)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(identity); err != nil {
		return "", err
	}

	encoded := ytdlpJSONEscapes.Replace(strings.TrimSuffix(payload.String(), "\n"))
	return fmt.Sprintf("ytdlp:v1:%x", sha256.Sum256([]byte(encoded))), nil
}

func collectYtdlpTargets(
	raw json.RawMessage,
	depth int,
	targets *[]YtdlpTarget,
) error {
	if depth > 16 || len(*targets) >= 5000 {
		return fmt.Errorf("yt-dlp 조회 결과가 처리 한도를 초과했습니다")
	}

	var row struct {
		YtdlpTarget
		Type          string          `json:"_type"`
		Entries       json.RawMessage `json:"entries"`
		PlaylistCount *int            `json:"playlist_count"`
		EntryCount    *int            `json:"n_entries"`
		IsLive        bool            `json:"is_live"`
		IsUpcoming    bool            `json:"is_upcoming"`
		HasDRM        bool            `json:"has_drm"`
		LiveStatus    string          `json:"live_status"`
	}

	if err := json.Unmarshal(raw, &row); err != nil {
		return fmt.Errorf("yt-dlp 대상 해석 실패: %w", err)
	}

	switch row.Type {
	case "playlist", "multi_video":
		var entries []json.RawMessage
		if err := json.Unmarshal(row.Entries, &entries); err != nil {
			return fmt.Errorf("yt-dlp 대상 목록 해석 실패: %w", err)
		}
		if len(entries) == 0 {
			return fmt.Errorf("yt-dlp 대상 목록이 비어 있습니다")
		}

		for _, count := range []*int{row.PlaylistCount, row.EntryCount} {
			if count != nil && *count != len(entries) {
				return fmt.Errorf("yt-dlp 대상 목록이 불완전합니다")
			}
		}

		for i, entry := range entries {
			if err := collectYtdlpTargets(entry, depth+1, targets); err != nil {
				return fmt.Errorf("yt-dlp 목록의 %d번째 항목: %w", i+1, err)
			}
		}
		return nil

	case "", "video":
		if row.Entries != nil ||
			strings.TrimSpace(row.ID) == "" ||
			strings.TrimSpace(row.ExtractorKey) == "" {
			return fmt.Errorf("yt-dlp 대상의 식별 정보가 올바르지 않습니다")
		}
		if row.Title == "" {
			row.Title = row.ID
		}
		downloadInfo, err := buildYtdlpDownloadInfo(raw, row.YtdlpTarget)
		if err != nil {
			return fmt.Errorf("yt-dlp 다운로드 정보 구성 실패: %w", err)
		}
		if row.IsLive || row.IsUpcoming || row.HasDRM ||
			(row.LiveStatus != "" && row.LiveStatus != "not_live" && row.LiveStatus != "was_live") {
			if downloadInfo == "" {
				return fmt.Errorf("%w. 라이브·예약·보호된 영상도 현재 지원하지 않습니다", ErrUnsupportedMediaFormat)
			}
			return fmt.Errorf("현재 다운로드할 수 없는 yt-dlp 대상입니다")
		}

		row.downloadInfo = downloadInfo
		*targets = append(*targets, row.YtdlpTarget)
		return nil

	default:
		return fmt.Errorf("지원하지 않는 yt-dlp 결과 유형 %q", row.Type)
	}
}
