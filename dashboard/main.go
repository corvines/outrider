package main

import (
	"context"
	"embed"
	"log"
	"os"
	"runtime"
	"time"

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

//go:embed assets/tray-template.png
var trayIcon []byte

func main() {
	endpoint := loopbackEndpoint()
	owner := newGatewayOwner(endpoint)
	startCtx, cancelStart := context.WithTimeout(context.Background(), 20*time.Second)
	if err := owner.Ensure(startCtx); err != nil {
		log.Printf("outrider: could not start the local server: %v", err)
	}
	cancelStart()
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := owner.Stop(stopCtx); err != nil {
			log.Printf("outrider: could not stop the local server: %v", err)
		}
	}()

	app := application.New(application.Options{
		Name:        "Outrider",
		Description: "Local model serving dashboard",
		Services: []application.Service{
			application.NewService(NewDashboardService(endpoint)),
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
			Backdrop:    application.MacBackdropTranslucent,
			TitleBar:    application.MacTitleBarDefault,
			WindowLevel: application.MacWindowLevelNormal,
		},
	})
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		window.Hide()
		event.Cancel()
	})

	tray := app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayIcon)
	} else {
		tray.SetIcon(trayIcon)
	}
	tray.SetTooltip("Outrider model server")

	menu := app.Menu.New()
	menu.Add("Open Dashboard").OnClick(func(_ *application.Context) {
		window.Show().Focus()
	})
	menu.AddSeparator()
	menu.Add("Quit Outrider").OnClick(func(_ *application.Context) {
		app.Quit()
	})
	// Keep the dashboard as a normal desktop window. Attaching it to the tray
	// turns it into a popup-menu window, which makes it float above other apps.
	tray.SetMenu(menu)

	if err := app.Run(); err != nil {
		log.Printf("outrider: %v", err)
	}
}

func loopbackEndpoint() string {
	port := os.Getenv("OUTRIDER_PORT")
	if port == "" {
		port = "11435"
	}
	return "http://127.0.0.1:" + port
}
