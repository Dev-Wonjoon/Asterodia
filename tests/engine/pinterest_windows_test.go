package engine_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

func TestPinterestDiscovery(t *testing.T) {
	metadata := map[string]any{"category": "pinterest", "subcategory": "pin", "id": "664281013778109217", "num": 1, "filename": "video", "extension": "mp4", "_ytdl_manifest": "hls", "grid_title": "테스트 영상", "pinner": map[string]string{"id": "42", "full_name": "작성자", "username": "artist"}}
	const address = "ytdl:https://v.pinimg.com/videos/one.m3u8?token=fixture%2Bone"
	raw, _ := json.Marshal([][]any{{2, metadata}, {3, address, metadata}})
	d, err := engine.ParseGalleryDLDiscovery(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.PinterestVideoURLs()[0]; got != "https://v.pinimg.com/videos/one.m3u8?token=fixture%2Bone" {
		t.Fatal(got)
	}
	first, err := d.Targets("https://www.pinterest.com/pin/664281013778109217/")
	if err != nil {
		t.Fatal(err)
	}
	short, err := d.Targets("https://pin.it/fixture")
	if err != nil || first[0].TargetKey != short[0].TargetKey || first[0].PostKey != "pinterest:664281013778109217" || first[0].Kind != "video" || first[0].Title != "테스트 영상" || first[0].OwnerID != "42" {
		t.Fatal(first, short, err)
	}
	for _, mode := range []string{"other-site", "credentials", "http", "wrong-extension", "not-hls", "not-manifest"} {
		t.Run(mode, func(t *testing.T) {
			data := make(map[string]any)
			for key, value := range metadata {
				data[key] = value
			}
			url := address
			switch mode {
			case "other-site":
				data["category"] = "twitter"
			case "credentials":
				url = "ytdl:https://name:password@v.pinimg.com/video.m3u8"
			case "http":
				url = "ytdl:http://v.pinimg.com/video.m3u8"
			case "wrong-extension":
				data["extension"] = "exe"
			case "not-hls":
				delete(data, "_ytdl_manifest")
			case "not-manifest":
				url = "ytdl:https://v.pinimg.com/video.mp4"
			}
			raw, _ := json.Marshal([][]any{{2, data}, {3, url, data}})
			if _, err := engine.ParseGalleryDLDiscovery(raw); err == nil {
				t.Fatal("invalid request accepted", mode)
			}
		})
	}
}

func TestPinterestRouting(t *testing.T) {
	for _, address := range []string{"https://www.pinterest.com/pin/123/", "https://kr.pinterest.com/pin/123/", "https://www.pinterest.co.kr/pin/123/", "https://www.pinterest.ca/pin/123/", "https://pin.it/fixture"} {
		kind, err := engine.Select(address, engine.Options{})
		if err != nil || kind != engine.KindGalleryDL {
			t.Fatal(address, kind, err)
		}
	}
	for _, address := range []string{"https://pinterest.com.evil.test/pin/123/", "https://notpinterest.com/pin/123/"} {
		kind, err := engine.Select(address, engine.Options{})
		if err != nil || kind != engine.KindYtdlp {
			t.Fatal(address, kind, err)
		}
	}
}

func TestPinterestDirectDownload(t *testing.T) {
	root := engineTestRoot(t, "gallerydl-download")
	stage, err := os.MkdirTemp(root, "pinterest-")
	if err != nil {
		t.Fatal(err)
	}
	metadata := map[string]any{"category": "pinterest", "subcategory": "pin", "id": "664281013778109217", "filename": "photo", "extension": "png", "num": 3}
	raw, _ := json.Marshal([][]any{{2, metadata}, {3, "https://i.pinimg.com/originals/photo.png", metadata}})
	d, err := engine.ParseGalleryDLDiscovery(raw)
	if err != nil {
		t.Fatal(err)
	}
	records, _ := json.Marshal([]map[string]any{{"Name": "photo.png", "Metadata": metadata, "Payload": "image fixture"}})
	t.Setenv("ASTERODIA_GALLERYDL_FIXTURE", "1")
	t.Setenv("ASTERODIA_GALLERYDL_EXPECT_FILTER", "num in [3]")
	t.Setenv("ASTERODIA_GALLERYDL_DOWNLOAD_RESULTS", string(records))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	paths, err := (&engine.GalleryDL{Executable: exe}).Download(t.Context(), "https://pin.it/fixture", d, stage)
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil || string(data) != "image fixture" {
		t.Fatal(string(data), err)
	}
}

func TestPinterestLegacyImageMetadata(t *testing.T) {
	root := engineTestRoot(t, "gallerydl-download")
	stage, err := os.MkdirTemp(root, "pinterest-legacy-")
	if err != nil {
		t.Fatal(err)
	}
	metadata := map[string]any{"category": "pinterest", "post_id": "664281013778109217", "filename": "asset", "extension": "png"}
	raw, _ := json.Marshal([][]any{{2, metadata}, {3, "https://i.pinimg.com/originals/photo.png", metadata}})
	discovery, err := engine.ParseGalleryDLDiscovery(raw)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := discovery.Targets("https://www.pinterest.com/pin/664281013778109217/")
	if err != nil || targets[0].PostKey != "pinterest:664281013778109217" || targets[0].Kind != "image" {
		t.Fatal(targets, err)
	}
	records, _ := json.Marshal([]map[string]any{{"Name": "photo.png", "Metadata": metadata, "Payload": "legacy image fixture"}})
	t.Setenv("ASTERODIA_GALLERYDL_FIXTURE", "1")
	t.Setenv("ASTERODIA_GALLERYDL_DOWNLOAD_RESULTS", string(records))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	paths, err := (&engine.GalleryDL{Executable: exe}).Download(t.Context(), "https://www.pinterest.com/pin/664281013778109217/", discovery, stage)
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil || string(data) != "legacy image fixture" {
		t.Fatal(string(data), err)
	}
}

func TestPinterestNativeImageStillRequiresFileNumbers(t *testing.T) {
	root := engineTestRoot(t, "gallerydl-download")
	for _, number := range []any{nil, 0, "invalid"} {
		metadata := map[string]any{"category": "pinterest", "id": "664281013778109217", "post_id": "664281013778109217", "filename": "asset", "extension": "png"}
		if number != nil {
			metadata["num"] = number
		}
		raw, _ := json.Marshal([][]any{{2, metadata}, {3, "https://i.pinimg.com/originals/photo.png", metadata}})
		discovery, err := engine.ParseGalleryDLDiscovery(raw)
		if err != nil {
			t.Fatal(err)
		}
		stage, err := os.MkdirTemp(root, "pinterest-invalid-order-")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = (&engine.GalleryDL{}).Download(t.Context(), "https://pin.it/fixture", discovery, stage); err == nil {
			t.Fatal("native pin without valid file order was accepted", number)
		}
	}
}
