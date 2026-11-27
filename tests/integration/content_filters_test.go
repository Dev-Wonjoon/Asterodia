package integration_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/media"
	"github.com/Dev-Wonjoon/Asterodia/internal/settings"
)

func TestContentFiltersPersistAndRejectInvalidChanges(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.root, "content-settings.json")
	config, err := settings.New(path, filepath.Join(f.root, "downloads"), settings.Dialogs{})
	if err != nil {
		t.Fatal(err)
	}
	wanted := media.ContentFilters{HiddenPlatforms: []string{"site"}, HiddenTags: []string{"blocked"}}
	saved, err := config.SetContentFilters(media.ContentFilters{HiddenPlatforms: []string{" site ", "site"}, HiddenTags: []string{"blocked"}})
	if err != nil || !reflect.DeepEqual(saved, wanted) {
		t.Fatal(saved, err)
	}
	reloaded, err := settings.New(path, "unused", settings.Dialogs{})
	if err != nil || !reflect.DeepEqual(reloaded.GetContentFilters(), wanted) {
		t.Fatal(reloaded, err)
	}
	saved.HiddenTags[0] = "mutated"
	if !reflect.DeepEqual(config.GetContentFilters(), wanted) {
		t.Fatal("settings shared mutable arrays")
	}
	for _, invalid := range []media.ContentFilters{{HiddenTags: []string{" "}}, {HiddenPlatforms: make([]string, 101)}, {HiddenTags: []string{"bad\x00tag"}}} {
		if _, err := config.SetContentFilters(invalid); err == nil {
			t.Fatal("invalid preferences accepted")
		}
		if !reflect.DeepEqual(config.GetContentFilters(), wanted) {
			t.Fatal("invalid update changed settings")
		}
	}
	if _, err := config.SetContentFilters(media.ContentFilters{}); err != nil {
		t.Fatal(err)
	}
	if len(config.GetContentFilters().HiddenTags) != 0 {
		t.Fatal("unhide did not clear the list")
	}
}

func TestContentFiltersHideTagsSitesAndRelatedAlbumsWithoutDeleting(t *testing.T) {
	db, service := searchDB(t)
	for id := 1; id <= 5; id++ {
		searchExec(t, db, "INSERT INTO media(id,source_kind,source_title,created_at) VALUES(?,'legacy',?,?)", id, []string{"", "blocked tag item", "blocked site item", "visible item", "mixed album", "safe album"}[id], id)
	}
	searchExec(t, db, "INSERT INTO tag(id,name) VALUES(1,'blocked')")
	searchExec(t, db, "INSERT INTO media_tag(media_id,tag_id) VALUES(1,1)")
	searchExec(t, db, "INSERT INTO platform(id,name) VALUES(1,'excluded-site'),(2,'visible-site')")
	for id := 1; id <= 3; id++ {
		searchExec(t, db, "INSERT INTO media_file(id,media_id,file_key,storage_kind,filepath,kind,position,platform_id,created_at) VALUES(?,?,?,'unverified_legacy',?,'image',0,?,0)", id, id, id, "fixture", []int{0, 2, 1, 2}[id])
	}
	searchExec(t, db, "UPDATE media SET source_kind='local' WHERE id IN (4,5)")
	searchExec(t, db, "INSERT INTO user_album(media_id) VALUES(4),(5)")
	searchExec(t, db, "INSERT INTO user_album_file(album_id,file_id,position) VALUES(4,2,0),(4,3,1),(5,3,0)")
	searchExec(t, db, "INSERT INTO favorite(media_id,created_at) SELECT id,0 FROM media")
	preferences := media.ContentFilters{HiddenTags: []string{"blocked"}, HiddenPlatforms: []string{"excluded-site"}}
	service.ConfigureContentFilters(func() media.ContentFilters { return preferences })
	for _, gallery := range []bool{false, true} {
		for _, favorite := range []bool{false, true} {
			page := success(t, service.List(t.Context(), media.ListRequest{Limit: 1, Gallery: gallery, FavoritesOnly: favorite})).(media.Object)
			if page["total"] != int64(2) || len(page["data"].([]media.Object)) != 1 || page["hasNextPage"] != true {
				t.Fatal(page)
			}
			all := success(t, service.List(t.Context(), media.ListRequest{Limit: 20, Gallery: gallery, FavoritesOnly: favorite, Filter: media.Filter{ShowHidden: true}})).(media.Object)
			if all["total"] != int64(5) {
				t.Fatal(all)
			}
		}
	}
	// The exclusion remains active for both the LIKE fallback and indexed search.
	for pass := 0; pass < 2; pass++ {
		filtered := success(t, service.List(t.Context(), media.ListRequest{Limit: 20, Filter: media.Filter{Keyword: "blocked"}})).(media.Object)
		if filtered["total"] != int64(0) {
			t.Fatal("keyword search exposed hidden records", filtered)
		}
		included := success(t, service.List(t.Context(), media.ListRequest{Limit: 20, Filter: media.Filter{Tags: []string{"blocked"}}})).(media.Object)
		if included["total"] != int64(0) {
			t.Fatal("include filter bypassed exclusion", included)
		}
		for _, text := range []string{"blocked", "mixed"} {
			page := searchPage(t, service, media.SearchSuggestRequest{Text: text, Kind: "title"})
			if len(page["data"].([]media.Object)) != 0 {
				t.Fatal(page)
			}
			page = searchPage(t, service, media.SearchSuggestRequest{Text: text, Kind: "title", Filter: media.Filter{ShowHidden: true}})
			if len(page["data"].([]media.Object)) == 0 {
				t.Fatal(page)
			}
		}
		drainSearch(t, service)
	}
	preferences = media.ContentFilters{}
	all := success(t, service.List(t.Context(), media.ListRequest{Limit: 20})).(media.Object)
	if all["total"] != int64(5) || queryInt(t, db, "SELECT count(*) FROM media_file") != 3 {
		t.Fatal("hidden originals or registrations were deleted", all)
	}
}
