//go:build android

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// Intent.ACTION_VIEW; the app goes to the background while the browser is up.
func openURL(u string) error {
	application.Android.OpenURL(u)
	return nil
}
