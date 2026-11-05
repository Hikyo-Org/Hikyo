//go:build ios

package main

import (
	_ "embed"
	"encoding/json"
	"log"
	"maps"
	"slices"
)

// ios-dev-config.json is gitignored and must exist to build: {"name": {"origin": "...", "token": "..."}}.
// shortcut: the token is baked into the binary, so the .app is a secret; a real shell keeps it in the keychain (IOS.SecureSet) after an in-app sign-in.
//
//go:embed ios-dev-config.json
var devConfig []byte

func defaultInstances() []string {
	return slices.Sorted(maps.Keys(loadSessions()))
}

func loadSessions() map[string]session {
	out := map[string]session{}
	if err := json.Unmarshal(devConfig, &out); err != nil {
		log.Fatalf("ios-dev-config.json: %v", err)
	}
	return out
}
