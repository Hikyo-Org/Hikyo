//go:build android

package main

import (
	"log"
	"os"
)

// The app's stderr goes nowhere, so the log is a file read with `adb shell run-as com.wails.app cat files/spike.log`.
// shortcut: hardcoded app id path, fine for the debug build; use the activity's files dir if the id changes.
func init() {
	f, err := os.OpenFile("/data/data/com.wails.app/files/spike.log", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		println("spike log:", err.Error()) // runtime println reaches logcat
		return
	}
	log.SetOutput(f)
}
