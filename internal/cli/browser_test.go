package cli

import (
	"slices"
	"strings"
	"testing"
)

func TestBrowserCommandRejectsUnsafeTargets(t *testing.T) {
	for _, target := range []string{
		"", "--help", "/tmp/report.html", "//example.com/path",
		"file:///tmp/report.html", "javascript:alert(1)", "custom://example.com",
		"https:opaque", "https:///path", "http://:80/path", "https://example.com/%zz",
		"https://user:secret@example.com/", "https://example.com/\nsecret",
	} {
		for _, platform := range []string{"darwin", "windows", "linux"} {
			command, err := browserCommand(target, platform)
			if err == nil || command != nil {
				t.Fatalf("%s accepted %q", platform, target)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("browser refusal disclosed URL credentials")
			}
		}
	}
}

func TestBrowserCommandPreservesHTTPHandoff(t *testing.T) {
	for _, target := range []string{
		"https://example.com/reauth/cli?transaction=a%2Bb&other=value#fragment",
		"http://127.0.0.1:8080/reauth/cli?transaction=state",
		"http://[::1]:8080/reauth/cli?transaction=state",
	} {
		for _, test := range []struct {
			platform string
			prefix   []string
		}{
			{"darwin", []string{"open"}},
			{"windows", []string{"rundll32", "url.dll,FileProtocolHandler"}},
			{"linux", []string{"xdg-open"}},
		} {
			command, err := browserCommand(target, test.platform)
			if err != nil {
				t.Fatal(err)
			}
			want := append(slices.Clone(test.prefix), target)
			if !slices.Equal(command.Args, want) {
				t.Fatalf("args = %q, want %q", command.Args, want)
			}
		}
	}
}
