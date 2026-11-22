package webserver_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

func TestGalleryExpansionAcrossAlbumsAndPages(t *testing.T) {
	f := newFixture(t)
	older, _, _ := f.seed(t, "older", "image", ".png")
	album, _, path := f.seed(t, "album", "image", ".png")
	newer, _, _ := f.seed(t, "newer", "image", ".png")
	// Gaps in stored positions must not become gaps in the visible page offsets.
	for i := 1; i < 53; i++ {
		if _, err := f.db.Exec("INSERT INTO media_file(media_id,file_key,storage_kind,filepath,kind,position,created_at) VALUES(?,?,'unverified_legacy',?,'image',?,0)", album, fmt.Sprint(i), path, i*3); err != nil {
			t.Fatal(err)
		}
	}
	catalog := media.NewService(f.db)
	read := func(req media.ListRequest) media.Object {
		t.Helper()
		result := catalog.List(t.Context(), req)
		if !result.OK {
			t.Fatalf("gallery: %+v", result.Error)
		}
		return result.Data.(map[string]any)
	}
	first := read(media.ListRequest{Gallery: true, Limit: 24})
	if first["total"] != int64(3) || len(first["data"].([]media.Object)) != 3 {
		t.Fatal(first)
	}
	expanded := media.ListRequest{Gallery: true, Limit: 24, ExpandedMediaIDs: []int64{album, album}}
	first = read(expanded)
	rows := first["data"].([]media.Object)
	if first["total"] != int64(55) || len(rows) != 24 || rows[0]["id"] != newer || rows[1]["galleryFileOffset"] != 0 {
		t.Fatal(first)
	}
	expanded.Offset = 24
	middle := read(expanded)
	rows = middle["data"].([]media.Object)
	if rows[0]["galleryFileOffset"] != 23 || rows[23]["galleryFileOffset"] != 46 {
		t.Fatal(middle)
	}
	expanded.Offset = 48
	last := read(expanded)
	rows = last["data"].([]media.Object)
	if len(rows) != 7 || rows[5]["galleryFileOffset"] != 52 || rows[6]["id"] != older || last["hasNextPage"] != false {
		t.Fatal(last)
	}
	collapsed := read(media.ListRequest{Gallery: true, Limit: 24, Offset: 48, AnchorMediaID: album})
	if collapsed["offset"] != 0 || collapsed["total"] != int64(3) {
		t.Fatal(collapsed)
	}
	clamped := read(media.ListRequest{Gallery: true, Limit: 24, Offset: 999, ExpandedMediaIDs: []int64{album}})
	if clamped["offset"] != 48 {
		t.Fatal(clamped)
	}
	filtered := read(media.ListRequest{Gallery: true, Limit: 24, ExpandedMediaIDs: []int64{album}, Filter: media.Filter{Keyword: "older"}})
	if filtered["total"] != int64(1) {
		t.Fatal(filtered)
	}
	if _, err := f.db.Exec("INSERT INTO favorite(media_id,created_at) VALUES(?,0)", album); err != nil {
		t.Fatal(err)
	}
	favorites := read(media.ListRequest{Gallery: true, FavoritesOnly: true, Limit: 24, ExpandedMediaIDs: []int64{album}})
	if favorites["total"] != int64(53) {
		t.Fatal(favorites)
	}
	empty := read(media.ListRequest{Gallery: true, Limit: 24, Offset: 100, Filter: media.Filter{Keyword: "absent"}})
	if empty["total"] != int64(0) || empty["offset"] != 0 || len(empty["data"].([]media.Object)) != 0 {
		t.Fatal(empty)
	}
	invalid := catalog.List(t.Context(), media.ListRequest{Gallery: true, ExpandedMediaIDs: []int64{-1}})
	if invalid.OK || invalid.Error.Code != "INVALID_ARGUMENT" {
		t.Fatal(invalid)
	}
	// Preserve the old album-only List contract for other app consumers.
	legacy := read(media.ListRequest{Limit: 24, ExpandedMediaIDs: []int64{album}})
	if legacy["total"] != int64(3) {
		t.Fatal(legacy)
	}

	status(t, f.server.Start())
	code, _, data := f.request(t, "GET", fmt.Sprintf("/api/library?gallery=1&expanded=%d&limit=24&offset=24", album), nil)
	if code != 200 || strings.Contains(string(data), path) || strings.Contains(string(data), "filepath") {
		t.Fatalf("public gallery: %d %s", code, data)
	}
	var public struct {
		Data []struct {
			GalleryFile *struct {
				ID int64 `json:"id"`
			} `json:"galleryFile"`
			Offset int `json:"galleryFileOffset"`
		} `json:"data"`
		Total  int `json:"total"`
		Offset int `json:"offset"`
	}
	if err := json.Unmarshal(data, &public); err != nil {
		t.Fatal(err)
	}
	if public.Total != 55 || public.Offset != 24 || len(public.Data) != 24 || public.Data[0].GalleryFile == nil || public.Data[0].Offset != 23 {
		t.Fatal(string(data))
	}
	for _, asset := range []string{"/player.js", "/player.css", "/album-state.js"} {
		if code, _, _ := f.request(t, "GET", asset, nil); code != 200 {
			t.Fatal(asset, code)
		}
	}
	for _, query := range []string{"expanded=bad", "expanded=-1", "anchor=-1"} {
		if code, _, _ := f.request(t, "GET", "/api/library?gallery=1&"+query, nil); code != 400 {
			t.Fatal(query, code)
		}
	}
}

func TestUserAlbumViewerAPIsKeepPathsPrivate(t *testing.T) {
	f := newFixture(t)
	_, one, path := f.seed(t, "one", "image", ".png")
	_, two, _ := f.seed(t, "two", "audio", ".mp3")
	title := "collected"
	catalog := media.NewService(f.db)
	result := catalog.SaveAlbum(t.Context(), media.AlbumRequest{Title: &title, AddFileIDs: []int64{two, one}})
	if !result.OK {
		t.Fatal(result.Error)
	}
	id := result.Data.(map[string]any)["id"].(int64)
	status(t, f.server.Start())
	for _, url := range []string{fmt.Sprintf("/api/library/%d/sequence", id), fmt.Sprintf("/api/files/%d/info", one), fmt.Sprintf("/api/library/%d", id), fmt.Sprintf("/api/library?gallery=1&expanded=%d", id)} {
		code, _, body := f.request(t, "GET", url, nil)
		if code != 200 || strings.Contains(string(body), path) || strings.Contains(string(body), "filepath") || strings.Contains(string(body), "directoryPath") {
			t.Fatalf("%s: %d %s", url, code, body)
		}
	}
	code, _, body := f.request(t, "GET", fmt.Sprintf("/api/library/%d/sequence", id), nil)
	var sequence struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.Unmarshal(body, &sequence); err != nil {
		t.Fatal(err)
	}
	if code != 200 || fmt.Sprint(sequence.IDs) != fmt.Sprint([]int64{two, one}) {
		t.Fatal(string(body))
	}
}
