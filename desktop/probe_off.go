//go:build darwin && !probe

package main

import (
	"net/http"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Hikyo-Org/hikyo/desktop/internal/shell"
)

// The probe hooks are no-ops in release builds.
func installProbe(*application.WebviewWindow, string) {}

func probeNative(n shell.Native) shell.Native                { return n }
func probeOpener(open func(string) error) func(string) error { return open }
func probeMiddleware(next func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return next
}
func probeSeed(*shell.Store) string    { return "" }
func probeOpen(*shell.Manager, string) {}

// Release builds ignore the development environment variables: a same-user
// process must not be able to move the connection list or allow plain http by
// setting them.
func devDataDir() string         { return "" }
func devAllowLoopbackHTTP() bool { return false }
