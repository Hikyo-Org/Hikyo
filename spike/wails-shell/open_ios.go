//go:build ios

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// The app goes to the background while Safari is up.
func openURL(u string) error {
	application.IOS.OpenURL(u)
	return nil
}
