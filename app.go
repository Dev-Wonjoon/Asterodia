package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
	"github.com/Dev-Wonjoon/StorageBucket/internal/desktop"
	"github.com/Dev-Wonjoon/StorageBucket/internal/download"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/filemove"
	"github.com/Dev-Wonjoon/StorageBucket/internal/localimport"
	"github.com/Dev-Wonjoon/StorageBucket/internal/maintenance"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"github.com/Dev-Wonjoon/StorageBucket/internal/mediaedit"
	"github.com/Dev-Wonjoon/StorageBucket/internal/settings"
	"github.com/Dev-Wonjoon/StorageBucket/internal/thumbnail"
	"github.com/Dev-Wonjoon/StorageBucket/internal/update"
	"github.com/Dev-Wonjoon/StorageBucket/internal/webserver"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func applicationServices(
	paths desktop.Paths,
	updates *update.Service,
	updateGate *api.UpdateGate,
) ([]application.Service, func(http.Handler) http.Handler, func(), error) {
	engine.ConfigureIntegrityRoot(paths.Bin)
	if err := os.MkdirAll(paths.Data, 0700); err != nil {
		return nil, nil, nil, err
	}
	unlock, err := desktop.AcquireLock(filepath.Join(paths.Data, "storagebucket.lock"))
	if err != nil {
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
		unlock()
		return nil, nil, nil, err
	}
	databasePath := filepath.Join(paths.Root, "storagebucket.db")
	db, err := database.OpenWithOptions(context.Background(), databasePath, database.MigrationOptions{BackupDirectory: filepath.Join(paths.Root, "backups")})
	if err != nil {
		unlock()
		return nil, nil, nil, err
	}
	databaseImporter := database.NewImporter(db, databasePath, filepath.Join(paths.Root, "backups"))
	catalog := media.NewService(db)
	catalog.ConfigureContentFilters(config.GetContentFilters)
	catalog.UpdateGate = updateGate
	updates.SetDatabase(db)
	catalog.StartSearchIndex()
	web := webserver.NewService(catalog, webserver.DefaultAddress)
	web.ConfigureAccessCode(config.GetWebRequireCode(), config.SetWebRequireCode)
	web.ConfigureContentFilters(config.SetContentFilters)
	thumb := thumbnail.NewService(db, filepath.Join(paths.Cache, "thumbnails"), filepath.Join(paths.Bin, "ffmpeg.exe"), func(id int64) {
		if app := application.Get(); app != nil {
			app.Event.Emit("media:thumbnail-updated", id)
		}
	})
	inspector := engine.LocalInspector{FFprobe: filepath.Join(paths.Bin, "ffprobe.exe"), FFmpeg: filepath.Join(paths.Bin, "ffmpeg.exe"), ImageMagick: filepath.Join(paths.Bin, "imagemagick", "magick.exe"), Cache: paths.Cache}
	thumb.ConfigureImages(inspector)
	importInspector := inspector
	importInspector.InspectionThreads = 1
	localImporter := localimport.NewService(db, importInspector, func() (string, error) {
		return application.Get().Dialog.OpenFile().CanChooseFiles(false).CanChooseDirectories(true).SetTitle("라이브러리에 가져올 폴더 선택").PromptForSingleSelection()
	}, func(id int64) {
		thumb.ScheduleMedia(id)
		if app := application.Get(); app != nil {
			app.Event.Emit("media:thumbnail-updated", id)
		}
	})
	thumb.ConfigurePreview(func(ctx context.Context, url, dir string) (json.RawMessage, error) {
		ctx, closeNetwork, err := engine.SecureContext(ctx)
		if err != nil {
			return nil, err
		}
		defer closeNetwork()
		snapshot := config.Snapshot()
		kind, err := engine.Select(url, engine.Options{})
		if err != nil || kind != engine.KindYtdlp {
			return nil, err
		}
		return (&engine.Ytdlp{Executable: filepath.Join(paths.Bin, "yt-dlp.exe"), CookieBrowser: snapshot.CookieBrowser, CookieFile: snapshot.CookieFilePath, NoPlaylist: true}).Discover(ctx, url, dir)
	})
	localImporter.UpdateGate = updateGate
	downloader := download.NewService(db, config.GetDownloadPath, func(root string, options engine.Options) download.Runner {
		snapshot := config.Snapshot()
		jsRuntime := ""
		if node, err := exec.LookPath("node"); err == nil {
			jsRuntime = "node:" + node
		}
		return download.Runner{SecureNetwork: true, Storage: download.Storage{Root: root}, Ytdlp: &engine.Ytdlp{Executable: filepath.Join(paths.Bin, "yt-dlp.exe"), JSRuntime: jsRuntime, FFmpegLocation: filepath.Join(paths.Bin, "ffmpeg.exe"), CookieBrowser: snapshot.CookieBrowser, CookieFile: snapshot.CookieFilePath, NoPlaylist: options.Playlist == nil || !*options.Playlist}, GalleryDL: &engine.GalleryDL{Executable: filepath.Join(paths.Bin, "gallery-dl.exe"), CookieBrowser: snapshot.CookieBrowser, CookieFile: snapshot.CookieFilePath}}
	}, thumb.ScheduleMedia)
	downloader.ConfigureImages(inspector)
	web.ConfigureDownloader(downloader)
	downloader.UpdateGate = updateGate
	downloader.ConfigurePreviewLookup(thumb.CachedDownloadPreview)
	downloader.ConfigurePreviewCleanup(func(ctx context.Context, url string) {
		_ = thumb.ReleaseDownloadPreview(ctx, url)
	})
	downloader.ConfigureVerification(func(ctx context.Context, path, kind string) error {
		return engine.VerifyMedia(ctx, filepath.Join(paths.Bin, "ffmpeg.exe"), path, kind)
	})
	mover := filemove.NewService(db, func(id int64) {
		thumb.ScheduleMedia(id)
		if app := application.Get(); app != nil {
			app.Event.Emit("media:thumbnail-updated", id)
		}
	})
	mover.ConfigureInspector(inspector)
	mover.UpdateGate = updateGate
	editor := mediaedit.NewService(db, filepath.Join(paths.Cache, "mediaedit"), engine.VideoEditor{FFmpeg: filepath.Join(paths.Bin, "ffmpeg.exe"), FFprobe: filepath.Join(paths.Bin, "ffprobe.exe")}, config.GetEditOutputDirectory, func(id int64) {
		thumb.ScheduleMedia(id)
		if app := application.Get(); app != nil {
			app.Event.Emit("media:thumbnail-updated", id)
		}
	})
	web.ConfigureEditor(editor)
	editor.UpdateGate = updateGate
	var once sync.Once
	cleaner := maintenance.NewCleaner(db)
	closeAll := func() {
		once.Do(func() {
			updates.Close()
			web.Close()
			editor.Close()
			mover.Close()
			localImporter.Close()
			downloader.Close()
			thumb.Close()
			cleaner.Close()
			catalog.Close()
			db.Close()
			unlock()
		})
	}
	if err = mover.Recover(context.Background()); err != nil {
		closeAll()
		return nil, nil, nil, err
	}
	if err = localImporter.Recover(context.Background()); err != nil {
		closeAll()
		return nil, nil, nil, err
	}
	if err = downloader.Recover(context.Background()); err != nil {
		closeAll()
		return nil, nil, nil, err
	}
	if err = editor.Start(); err != nil {
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
			if progress.Phase == "completed" && (progress.Engine == "ffmpeg" || progress.Engine == "ffprobe" || progress.Engine == "imagemagick") {
				thumb.ScheduleMissing()
			}
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
	manager.UpdateGate = updateGate
	desktopService := desktop.NewService(databaseImporter, func() (string, error) {
		return application.Get().Dialog.OpenFile().CanChooseFiles(true).CanChooseDirectories(false).AddFilter("StorageBucket 데이터베이스", "*.db;*.sqlite;*.sqlite3").SetTitle("병합할 데이터베이스 선택").PromptForSingleSelection()
	}, func() {
		thumb.ScheduleMissing()
		if app := application.Get(); app != nil {
			app.Event.Emit("media:thumbnail-updated", int64(0))
		}
	})
	desktopService.UpdateGate = updateGate

	return []application.Service{
		application.NewService(updates),
		application.NewService(editor),
		application.NewService(mover),
		application.NewService(localImporter),
		application.NewService(downloader),
		application.NewService(manager),
		application.NewService(catalog),
		application.NewService(web),
		application.NewService(config),
		application.NewService(thumb),
		application.NewService(&desktop.SystemService{}),
		application.NewService(desktopService),
	}, func(next http.Handler) http.Handler {
		mediaHandler := desktop.MediaMiddleware(db, filepath.Join(paths.Cache, "previews"))(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/edit-assets/") {
				editor.Assets().ServeHTTP(w, r)
				return
			}
			mediaHandler.ServeHTTP(w, r)
		})
	}, closeAll, nil
}

func init() {
	application.RegisterEvent[engine.InstallProgress](
		"engine:install-progress",
	)
	application.RegisterEvent[int64]("media:thumbnail-updated")
}
