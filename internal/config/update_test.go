package config

import (
	"strings"
	"testing"
)

func TestUpdateChannelDefaultsStableAndParsesConfiguredTrack(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want string
	}{
		{raw: "", want: "stable"},
		{raw: "nightly", want: "nightly"},
		{raw: "off", want: "off"},
	} {
		cfg, _, err := Load("server", []string{"--dev"}, func(name string) string {
			if name == "HIKYO_UPDATE_CHANNEL" {
				return test.raw
			}
			return ""
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.UpdateChannel != test.want {
			t.Fatalf("raw %q: channel = %q, want %q", test.raw, cfg.UpdateChannel, test.want)
		}
	}
}

func TestUpdateChannelRefusesUnknownTrack(t *testing.T) {
	_, _, err := Load("server", []string{"--dev"}, func(name string) string {
		if name == "HIKYO_UPDATE_CHANNEL" {
			return "preview"
		}
		return ""
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "HIKYO_UPDATE_CHANNEL") {
		t.Fatalf("error = %v, want named channel refusal", err)
	}
}
