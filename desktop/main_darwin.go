//go:build darwin

// Command desktop is the native macOS shell for hosted Hikyo instances
// (ADR docs/adr/desktop-app.md). It shows each connection's own web app in a
// window whose requests the shell proxies with the session as a bearer, so
// page code never holds a credential.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/Hikyo-Org/hikyo/desktop/internal/shell"
	"github.com/Hikyo-Org/hikyo/internal/clientauth"
)

// version is the app version, set with -ldflags "-X main.version=...".
var version = "0.0.0-dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Hikyo Desktop:", err)
		os.Exit(1)
	}
}

// dataDir is where the connection list lives. Secrets are never stored there.
func dataDir() (string, error) {
	if dir := os.Getenv("HIKYO_DESKTOP_HOME"); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Hikyo Desktop"), nil
}

func run() error {
	dir, err := dataDir()
	if err != nil {
		return err
	}
	store, err := shell.OpenStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		return err
	}
	seeded := probeSeed(store)
	native := newMacNative()
	manager, err := shell.NewManager(store, keychain{}, probeNative(native), shell.Options{
		Version: version,
		// Development only: plain http to a loopback instance.
		AllowLoopbackHTTP: os.Getenv("HIKYO_DESKTOP_ALLOW_LOOPBACK_HTTP") == "1",
		OpenURL:           probeOpener(clientauth.OpenBrowser),
	})
	if err != nil {
		return err
	}
	native.manager = manager

	app := application.New(application.Options{
		Name: "Hikyo",
		// The shell is the outermost handler; the fallback never answers.
		Assets: application.AssetOptions{Handler: http.NotFoundHandler(), Middleware: probeMiddleware(manager.Middleware())},
		Mac:    application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	native.app = app

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go manager.WatchClipboard(ctx)
		if err := manager.OpenApp(); err != nil {
			log.Printf("opening the connection list: %v", err)
		}
		probeOpen(manager, seeded)
	})
	runErr := app.Run()
	cancel()
	manager.Close()
	return runErr
}
