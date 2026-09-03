package main

import (
	"embed"
	"log"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// The release build embeds the Vite output, so the dashboard stays a single
// native application with no Node runtime requirement.
//
//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	app := application.New(application.Options{
		Name:        "Outrider",
		Description: "Local model serving dashboard",
		Services: []application.Service{
			application.NewService(NewDashboardService(loopbackEndpoint())),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory,
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "OutriderDashboard",
		Title:            "Outrider",
		Width:            1120,
		Height:           760,
		MinWidth:         820,
		MinHeight:        560,
		BackgroundColour: application.NewRGB(13, 15, 20),
		URL:              "/",
		Mac: application.MacWindow{
			Backdrop: application.MacBackdropTranslucent,
			TitleBar: application.MacTitleBarHiddenInset,
		},
	})
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		window.Hide()
		event.Cancel()
	})

	tray := app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(appIcon)
	} else {
		tray.SetIcon(appIcon)
	}
	tray.SetLabel("Outrider")
	tray.SetTooltip("Outrider model server")

	menu := app.Menu.New()
	menu.Add("Open Dashboard").OnClick(func(_ *application.Context) {
		tray.ShowWindow()
	})
	menu.AddSeparator()
	menu.Add("Quit Outrider").OnClick(func(_ *application.Context) {
		app.Quit()
	})
	tray.AttachWindow(window).WindowOffset(5).SetMenu(menu)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func loopbackEndpoint() string {
	port := os.Getenv("OUTRIDER_PORT")
	if port == "" {
		port = "11435"
	}
	return "http://127.0.0.1:" + port
}
