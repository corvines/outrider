package main

import (
	"context"
	"embed"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
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
	service := NewDashboardService(endpoint)
	service.owner = owner
	// Quit reaches us through OnShutdown, not through a deferred call: Quit
	// terminates the application without unwinding main.
	var stopOnce sync.Once
	stopGateway := func() {
		stopOnce.Do(func() {
			service.CancelChat()
			stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := owner.Stop(stopCtx); err != nil {
				log.Printf("outrider: could not stop the local server: %v", err)
			}
		})
	}
	defer stopGateway()

	var quitApproved atomic.Bool
	var requestQuit func()
	app := application.New(application.Options{
		Name:        "Outrider",
		Description: "Local model serving dashboard",
		ShouldQuit: func() bool {
			if quitApproved.Load() {
				return true
			}
			go requestQuit()
			return false
		},
		Services: []application.Service{
			application.NewService(service),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyRegular,
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	app.OnShutdown(stopGateway)
	service.quit = func() {
		quitApproved.Store(true)
		app.Quit()
	}

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
	requestQuit = func() {
		snapshot := service.QuitAndStopServer()
		if snapshot.ServerError != "" {
			window.Show().Focus()
			app.Dialog.Error().SetTitle("Could not stop server").SetMessage(snapshot.ServerError + "\n\nOutrider is still open. Retry from the dashboard.").Show()
		}
	}

	tray := app.SystemTray.New()
	menuIcon, err := paddedTrayIcon(trayIcon)
	if err != nil {
		log.Fatalf("outrider: prepare menu bar icon: %v", err)
	}
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(menuIcon)
	} else {
		tray.SetIcon(menuIcon)
	}
	tray.SetTooltip("Outrider model server")

	menu := app.Menu.New()
	menu.Add("Open Dashboard").OnClick(func(_ *application.Context) {
		window.Show().Focus()
	})
	menu.Add("Install Command Line Tool").OnClick(func(_ *application.Context) {
		go installCommandLineTool(app, owner)
	})
	menu.AddSeparator()
	menu.Add("Quit Outrider").OnClick(func(_ *application.Context) {
		go requestQuit()
	})
	// Keep the dashboard as a normal desktop window. Attaching it to the tray
	// turns it into a popup-menu window, which makes it float above other apps.
	tray.SetMenu(menu)

	// The app owns the server for as long as it is open: it starts one on
	// launch and stops it on quit. An already healthy gateway is adopted
	// rather than restarted, which keeps a loaded model in memory.
	go service.StartServer()

	if err := app.Run(); err != nil {
		log.Printf("outrider: %v", err)
	}
}

func installCommandLineTool(app *application.App, owner *gatewayOwner) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	target, err := owner.InstallCommandLineTool(ctx)
	if err != nil {
		log.Printf("outrider: could not install the command line tool: %v", err)
		app.Dialog.Error().
			SetTitle("Outrider").
			SetMessage("Could not install the command line tool.\n\n" + err.Error()).
			Show()
		return
	}
	app.Dialog.Info().
		SetTitle("Outrider").
		SetMessage("Installed the outrider command at " + target +
			".\n\nAdd " + filepath.Dir(target) + " to PATH if it is not there already.").
		Show()
}

func loopbackEndpoint() string {
	port := os.Getenv("OUTRIDER_PORT")
	if port == "" {
		port = "11435"
	}
	return "http://127.0.0.1:" + port
}
