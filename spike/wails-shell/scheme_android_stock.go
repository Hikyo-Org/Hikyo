//go:build android && stockhost

package main

// The stock Wails host serves one fixed origin, so only one connection fits.
const pageScheme = "https"

func newHost() string { return "wails.localhost" }

// shortcut: the stock host sends x-wails-window-id only on /wails/runtime calls, so the window check is off here; the patched host sends it.
const windowHeaderSent = false
