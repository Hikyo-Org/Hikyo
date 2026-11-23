package cli

import (
	"os"
	"strings"
	"testing"
)

func TestNoGraphicalSessionByPlatform(t *testing.T) {
	none := func(string) string { return "" }
	wayland := func(k string) string {
		if k == "WAYLAND_DISPLAY" {
			return "wayland-0"
		}
		return ""
	}
	for _, tc := range []struct {
		goos string
		env  func(string) string
		want bool
	}{
		{"linux", none, true},
		{"freebsd", none, true},
		{"linux", wayland, false},
		{"linux", func(k string) string { return map[string]string{"DISPLAY": ":0"}[k] }, false},
		// xdg-open and VS Code Remote honour BROWSER, so a host with it can complete the handoff.
		{"linux", func(k string) string { return map[string]string{"BROWSER": "/usr/bin/true"}[k] }, false},
		// `open` over SSH still targets the GUI session; do not refuse there.
		{"darwin", none, false},
		{"windows", none, false},
	} {
		if got := noGraphicalSession(tc.goos, tc.env); got != tc.want {
			t.Errorf("%s: noGraphicalSession = %v, want %v", tc.goos, got, tc.want)
		}
	}
}

// The remedy names the control that applied, with the instance, from one place.
func TestBrowserChoiceNamesTheCauseThatApplied(t *testing.T) {
	st := &State{dir: t.TempDir()}
	if err := st.SetReauthMethod("prod", ReauthBrowser); err != nil {
		t.Fatal(err)
	}
	if cause, remedy, _ := st.browserChoice("prod"); cause != "reauth.method" || remedy != "run `hikyo config set reauth.method auto --instance prod`" {
		t.Errorf("setting only: %q / %q", cause, remedy)
	}
	st.forceBrowser = true
	if cause, remedy, _ := st.browserChoice("prod"); cause != "--approve-in-browser and reauth.method" ||
		!strings.Contains(remedy, "drop --approve-in-browser and run `hikyo config set reauth.method auto --instance prod`") {
		t.Errorf("both: %q / %q", cause, remedy)
	}
	other := &State{dir: t.TempDir(), forceBrowser: true}
	if cause, remedy, _ := other.browserChoice("prod"); cause != "--approve-in-browser" || remedy != "drop --approve-in-browser" {
		t.Errorf("flag only: %q / %q", cause, remedy)
	}
}

// A damaged settings file is an error, never silently the default.
func TestBrowserChoiceReportsAnUnreadableSettingsFile(t *testing.T) {
	st := &State{dir: t.TempDir(), forceBrowser: true}
	if err := os.WriteFile(st.settingsPath(), []byte(`{"prod":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.browserChoice("prod"); err == nil {
		t.Fatal("browserChoice swallowed an unreadable settings file")
	}
	if _, err := st.approveInBrowser("prod"); err == nil {
		t.Fatal("the flag bypassed settings validation")
	}
}
