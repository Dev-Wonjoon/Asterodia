package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/desktop"
	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/maintenance"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"github.com/Dev-Wonjoon/StorageBucket/internal/settings"
	"github.com/Dev-Wonjoon/StorageBucket/internal/thumbnail"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func applicationServices(
	paths desktop.Paths,
) ([]application.Service, func(http.Handler) http.Handler, func(), error) {
	if err := os.MkdirAll(paths.Data, 0700); err != nil {
		return nil, nil, nil, err
	}
	unlock, err := desktop.AcquireLock(filepath.Join(paths.Data, "storagebucket.lock"))
	if err != nil {
		return nil, nil, nil, err
	}
	db, err := database.Open(context.Background(), filepath.Join(paths.Root, "storagebucket.db"))
	if err != nil {
		unlock()
		return nil, nil, nil, err
	}
	config, err := settings.New(filepath.Join(paths.Data, "config.json"), paths.Downloads, settings.Dialogs{
		Directory: func(current string) (string, error) {
			return application.Get().Dialog.OpenFile().CanChooseFiles(false).CanChooseDirectories(true).CanCreateDirectories(true).SetDirectory(current).SetTitle("다운로드 폴더 선택").PromptForSingleSelection()
		},
		CookieFile: func() (string, error) {
			return application.Get().Dialog.OpenFile().AddFilter("쿠키 파일", "*.txt").SetTitle("Netscape 쿠키 파일 선택").PromptForSingleSelection()
		},
		Reveal: func(path string) error { return application.Get().Env.OpenFileManager(path, true) },
	})
	if err != nil {
		db.Close()
		unlock()
		return nil, nil, nil, err
	}
	thumb := thumbnail.NewService(db, filepath.Join(paths.Cache, "thumbnails"), filepath.Join(paths.Bin, "ffmpeg.exe"), func(id int64) {
		if app := application.Get(); app != nil {
			app.Event.Emit("media:thumbnail-updated", id)
		}
	})
	thumb.ConfigurePreview(func(ctx context.Context, url, dir string) (json.RawMessage, error) {
		snapshot := config.Snapshot()
		kind, err := engine.Select(url, engine.Options{})
		if err != nil || kind != engine.KindYtdlp {
			return nil, err
		}
		return (&engine.Ytdlp{Executable: filepath.Join(paths.Bin, "yt-dlp.exe"), CookieBrowser: snapshot.CookieBrowser, CookieFile: snapshot.CookieFilePath, NoPlaylist: true}).Discover(ctx, url, dir)
	})
	downloader := download.NewService(db, config.GetDownloadPath, func(root string, options engine.Options) download.Runner {
		snapshot := config.Snapshot()
		jsRuntime := ""
		if node, err := exec.LookPath("node"); err == nil {
			jsRuntime = "node:" + node
		}
		return download.Runner{Storage: download.Storage{Root: root}, Ytdlp: &engine.Ytdlp{Executable: filepath.Join(paths.Bin, "yt-dlp.exe"), JSRuntime: jsRuntime, FFmpegLocation: filepath.Join(paths.Bin, "ffmpeg.exe"), CookieBrowser: snapshot.CookieBrowser, CookieFile: snapshot.CookieFilePath, NoPlaylist: options.Playlist == nil || !*options.Playlist}, GalleryDL: &engine.GalleryDL{Executable: filepath.Join(paths.Bin, "gallery-dl.exe"), CookieBrowser: snapshot.CookieBrowser, CookieFile: snapshot.CookieFilePath}}
	}, thumb.ScheduleMedia)
	downloader.ConfigureVerification(func(ctx context.Context, path, kind string) error {
		return engine.VerifyMedia(ctx, filepath.Join(paths.Bin, "ffmpeg.exe"), path, kind)
	})
	var once sync.Once
	cleaner := maintenance.NewCleaner(db)
	closeAll := func() { once.Do(func() { downloader.Close(); thumb.Close(); cleaner.Close(); db.Close(); unlock() }) }
	if err = downloader.Recover(context.Background()); err != nil {
		closeAll()
		return nil, nil, nil, err
	}
	if err = thumb.Start(context.Background()); err != nil {
		closeAll()
		return nil, nil, nil, err
	}
	manager, err := engine.NewManager(

		paths.Bin,
		func(progress engine.InstallProgress) {
			if app := application.Get(); app != nil {
				app.Event.Emit("engine:install-progress", progress)
			}
		},
	)
	cleaner.Start()

	if err != nil {
		closeAll()
		return nil, nil, nil, err
	}

	return []application.Service{
		application.NewService(downloader),
		application.NewService(manager),
		application.NewService(media.NewService(db)),
		application.NewService(config),
		application.NewService(thumb),
		application.NewService(&desktop.SystemService{}),
	}, desktop.MediaMiddleware(db, filepath.Join(paths.Cache, "previews")), closeAll, nil
}

func init() {
	application.RegisterEvent[engine.InstallProgress](
		"engine:install-progress",
	)
	application.RegisterEvent[int64]("media:thumbnail-updated")
}
