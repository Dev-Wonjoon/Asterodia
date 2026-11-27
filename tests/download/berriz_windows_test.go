package download_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/download"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

type berrizCookieSource struct{}

func (berrizCookieSource) Cookies(context.Context, *url.URL, string) ([]*http.Cookie, error) {
	return []*http.Cookie{{Name: "session", Value: "fixture"}}, nil
}

func TestRunnerBerrizFreezesOriginalMetadataAndKeepsTransfer(t *testing.T) {
	const id = "01a17302-2c13-57bc-c315-ba00b1c61882"
	const original = "https://berriz.in/ko/kiiikiii/live/replay/" + id + "/?ref=user"
	const manifest = "https://vod-edge.berriz.in/cdn/master.m3u8?signature=exact%2Btoken"
	for _, mode := range []string{"success", "freeze-fails", "transfer-fails"} {
		t.Run(mode, func(t *testing.T) {
			db := openStoreTestDB(t)
			store := download.NewStore(db)
			root := downloadTestRoot(t)
			job, err := store.CreateInitialAttempt(t.Context(), download.InitialAttemptInput{OriginalURL: original, StorageRoot: root})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "freeze-fails" {
				execStoreTestSQL(t, db, `CREATE TRIGGER reject_berriz BEFORE INSERT ON download_target BEGIN SELECT RAISE(ABORT,'berriz fixture'); END`)
			}
			apiCalls, discoverCalls, saveCalls := 0, 0, 0
			options := engine.Options{Type: "video", Quality: "720"}
			client := &http.Client{Transport: extractorTransport(func(r *http.Request) (*http.Response, error) {
				apiCalls++
				if r.Header.Get("Cookie") != "session=fixture" {
					t.Error("API credentials missing")
				}
				raw, _ := json.Marshal(map[string]any{"code": "0000", "data": map[string]any{
					"media":            map[string]any{"mediaId": id, "mediaType": "LIVE", "title": "Berriz 테스트 영상", "live": map[string]any{"liveStatus": "REPLAY", "replay": map[string]any{"duration": 1174, "isDrm": false, "hls": map[string]string{"playbackUrl": manifest}}}},
					"communityArtists": []any{map[string]any{"communityArtistId": 26, "name": "작성자"}},
				}})
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw)), ContentLength: int64(len(raw)), Request: r}, nil
			})}
			backend := stagingYtdlp{ytdlpDiscoverFunc: func(ctx context.Context, address, dir string) (json.RawMessage, error) {
				discoverCalls++
				if address != manifest {
					t.Fatal("changed signed manifest", address)
				}
				return json.RawMessage(`{"id":"cdn-id","extractor_key":"Generic","webpage_url":"https://vod-edge.berriz.in/master.m3u8","formats":[{"format_id":"hls-720","protocol":"m3u8_native","url":"https://vod-edge.berriz.in/720.m3u8","height":720,"ext":"mp4","vcodec":"h264","acodec":"aac"}]}`), nil
			}, save: func(target engine.YtdlpTarget, dir string) (string, error) {
				saveCalls++
				var key, source, platform, title, external, owner string
				var duration float64
				err := db.QueryRow(`SELECT target_key,source_url,json_extract(metadata_json,'$.platformName'),json_extract(metadata_json,'$.title'),json_extract(metadata_json,'$.externalId'),json_extract(metadata_json,'$.ownerName'),json_extract(metadata_json,'$.duration') FROM download_target WHERE attempt_id=? AND position=0 AND state='pending'`, job.AttemptID).Scan(&key, &source, &platform, &title, &external, &owner, &duration)
				if err != nil || key != "berriz:"+id || source != original || platform != "Berriz" || title != "Berriz 테스트 영상" || external != id || owner != "작성자" || duration != 1174 {
					t.Fatal("wrong metadata before transfer", key, source, platform, title, external, owner, duration, err)
				}
				if err := target.ValidateDirectDownload(options); err != nil {
					t.Fatal(err)
				}
				if mode == "transfer-fails" {
					return "", engine.ErrMediaUnavailable
				}
				file := filepath.Join(dir, "media.mp4")
				return file, os.WriteFile(file, []byte("fixture media"), 0600)
			}}
			runner := download.Runner{Storage: download.Storage{Root: root}, Extractors: engine.DefaultExtractors(engine.ExtractorDependencies{Ytdlp: backend, Cookies: berrizCookieSource{}, HTTPClient: client})}
			attempt, err := runner.DiscoverAttempt(t.Context(), job.JobID, job.AttemptID, original, options)
			if err != nil || attempt.Extractor != "berriz" {
				t.Fatal(attempt, err)
			}
			runner.Extractors, runner.Ytdlp, runner.GalleryDL = nil, nil, nil
			files, err := runner.FreezeAndStageAttempt(t.Context(), store, attempt)
			if mode == "freeze-fails" {
				if err == nil || saveCalls != 0 || len(files) != 0 {
					t.Fatal(files, saveCalls, err)
				}
				return
			}
			if mode == "transfer-fails" {
				if !errors.Is(err, engine.ErrMediaUnavailable) || len(files) != 0 || saveCalls != 1 || apiCalls != 1 || discoverCalls != 1 {
					t.Fatal("failed transfer reselected extraction", files, apiCalls, discoverCalls, saveCalls, err)
				}
				return
			}
			if err != nil || len(files) != 1 || saveCalls != 1 || apiCalls != 1 || discoverCalls != 1 {
				t.Fatal(files, apiCalls, discoverCalls, saveCalls, err)
			}
			var count int
			if err := db.QueryRow(`SELECT count(*) FROM download_target WHERE attempt_id=? AND state='downloaded'`, job.AttemptID).Scan(&count); err != nil || count != 1 {
				t.Fatal(count, err)
			}
		})
	}
}
