package main

import (
	"embed"
	"path/filepath"

	"log"

	"github.com/Dev-Wonjoon/Asterodia/internal/api"
	"github.com/Dev-Wonjoon/Asterodia/internal/desktop"
	"github.com/Dev-Wonjoon/Asterodia/internal/update"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// The swap helper must run before acquiring the data lock or opening the DB.
	updater.HandleHelperMode()
	paths, err := desktop.ResolvePaths(desktop.DataDirectoryOverride())
	if err != nil {
		log.Fatal(err)
	}

	updateGate := &api.UpdateGate{}
	updates := update.NewService(update.Version, updateGate)
	services, middleware, closeAll, err := applicationServices(paths, updates, updateGate)
	if err != nil {
		log.Fatal(err)
	}
	defer closeAll()
	app := application.New(application.Options{
		Name:        "Asterodia",
		Description: "미디어 다운로드 및 로컬 라이브러리 관리",
		Services:    services,
		OnShutdown:  closeAll,
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: middleware,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath: filepath.Join(paths.Cache, "webview"),
		},
	})
	provider, err := update.NewProvider("", nil)
	if err != nil {
		log.Fatal(err)
	}
	if err = updates.Configure(app.Updater, provider); err != nil {
		log.Fatal(err)
	}
	updates.Start()

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Asterodia",
		// Window sized to the golden ratio (1000 / 618 ≈ 1.618).
		Width:  1000,
		Height: 618,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})

	// Run the application. This blocks until the application has been exited.
	err = app.Run()

	// If an error occurred while running the application, log it and exit.
	if err != nil {
		log.Fatal(err)
	}
}
