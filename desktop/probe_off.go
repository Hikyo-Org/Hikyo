//go:build darwin && !probe

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Hikyo-Org/hikyo/desktop/internal/shell"
)

// The probe hooks are no-ops in release builds.
func installProbe(*application.WebviewWindow) {}
func probeSeed(*shell.Store) string           { return "" }
func probeOpen(*shell.Manager, string)        {}
