//go:build !android

package main

// pageScheme and newHost name the origin a window loads: wails://<random label> on desktop and iOS.
const pageScheme = "wails"

func newHost() string { return randomLabel() }
