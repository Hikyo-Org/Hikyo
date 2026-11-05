//go:build !ios && !android

package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

func defaultInstances() []string { return nil }

// shortcut: reads the CLI's 0600 session file, so sign-in stays `hikyo login`; a real shell runs the handoff itself and uses the keychain.
func loadSessions() map[string]session {
	dir := os.Getenv("HIKYO_STATE_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		dir = filepath.Join(home, ".local", "state", "hikyo")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "sessions.json"))
	if err != nil {
		log.Fatal(err)
	}
	out := map[string]session{}
	if err := json.Unmarshal(raw, &out); err != nil {
		log.Fatalf("sessions.json: %v", err)
	}
	return out
}
