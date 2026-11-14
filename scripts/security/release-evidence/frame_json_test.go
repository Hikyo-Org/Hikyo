package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

// A decoded position is copied into the evidence receipt as encoding/json (v1)
// wrote it: only an absent one is omitted, never an empty object or null.
func TestScanFramePositionKeepsV1Bytes(t *testing.T) {
	for position, want := range map[string]string{
		"":     `{"module":"m"}`,
		"{}":   `{"module":"m","position":{}}`,
		"null": `{"module":"m","position":null}`,
	} {
		frame := scanFrame{Module: "m"}
		if position != "" {
			frame.Position = jsontext.Value(position)
		}
		got, err := json.Marshal(frame, jsonwire.Stable)
		if err != nil || string(got) != want {
			t.Errorf("position %q: got %s (%v), want %s", position, got, err, want)
		}
	}
}
