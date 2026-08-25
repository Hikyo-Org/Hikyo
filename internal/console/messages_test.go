package console

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestFullArtworkMatchesSuppliedLogo(t *testing.T) {
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(FullArtwork())))
	want := "50bfd2923a7d8a44dd3e559acd2bc811c28bb2b93a8cd3ca747d4fea3307f633"
	if got != want {
		t.Fatalf("full artwork SHA-256 = %s, want %s", got, want)
	}
}

func TestAboutAndWelcomeMessagesUseFullArtwork(t *testing.T) {
	info := VersionInfo{Version: "1.2.3", Commit: "abcdef12", BuildDate: "2026-08-24T08:00:00Z"}

	wantAbout := FullArtwork() + "\n" + VersionMessage(info) +
		"\nValidated secrets and configuration across environments.\n"
	if got := AboutMessage(info); got != wantAbout {
		t.Fatalf("AboutMessage() = %q, want %q", got, wantAbout)
	}

	wantWelcome := FullArtwork() + "\nWelcome to Hikyo 1.2.3\n" +
		"Run hikyo to see available commands.\n"
	if got := WelcomeMessage(info); got != wantWelcome {
		t.Fatalf("WelcomeMessage() = %q, want %q", got, wantWelcome)
	}
}

func TestVersionMessageNamesBuildMetadata(t *testing.T) {
	got := VersionMessage(VersionInfo{
		Version:   "0.2.0-rc.1",
		Commit:    "abcdef12",
		BuildDate: "2026-08-24T08:00:00Z",
	})
	want := "Hikyo 0.2.0-rc.1\n" +
		"  Commit  abcdef12\n" +
		"  Built   2026-08-24T08:00:00Z\n"
	if got != want {
		t.Fatalf("VersionMessage() = %q, want %q", got, want)
	}
}

func TestDevelopmentVersionMessageStaysCompact(t *testing.T) {
	if got, want := VersionMessage(VersionInfo{Version: "dev", Commit: "unknown", BuildDate: "unknown"}), "Hikyo dev\n"; got != want {
		t.Fatalf("VersionMessage() = %q, want %q", got, want)
	}
}

func TestServerReadyMessageShowsUserAndOperatorEndpoints(t *testing.T) {
	info := ServerInfo{
		Version:        "0.2.0",
		AppURL:         "https://hikyo.example.com",
		ListenAddress:  "0.0.0.0:8443",
		OperationalURL: "http://127.0.0.1:8081",
		Mode:           "production",
	}
	got := ServerReadyMessage(info)
	want := "Hikyo server is ready\n" +
		"  Version     0.2.0\n" +
		"  App         https://hikyo.example.com\n" +
		"  Listen      0.0.0.0:8443\n" +
		"  Operations  http://127.0.0.1:8081\n" +
		"  Mode        production\n"
	if got != want {
		t.Fatalf("ServerReadyMessage() = %q, want %q", got, want)
	}
	if got := ServerStartupMessage(info, false); got != "" {
		t.Fatalf("non-interactive ServerStartupMessage() = %q, want no diagnostic on stdout", got)
	}
	if got := ServerStartupMessage(info, true); got != want {
		t.Fatalf("interactive ServerStartupMessage() = %q, want %q", got, want)
	}
}

func TestUpdateMessagesExplainOutcome(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "checking",
			got:  UpdateCheckMessage("stable"),
			want: "Checking for updates on stable...\n",
		},
		{
			name: "current",
			got:  UpdateCurrentMessage(UpdateInfo{Current: "1.0.0", Channel: "stable"}),
			want: "Hikyo is up to date\n  Version  1.0.0\n  Channel  stable\n",
		},
		{
			name: "available",
			got: UpdateAvailableMessage(UpdateInfo{
				Current: "1.0.0", Latest: "1.0.1", Channel: "stable",
				ReleaseURL: "https://example.com/hikyo/v1.0.1",
			}),
			want: "Hikyo update available\n" +
				"  Installed  1.0.0\n" +
				"  Latest     1.0.1\n" +
				"  Channel    stable\n" +
				"  Release    https://example.com/hikyo/v1.0.1\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("message = %q, want %q", test.got, test.want)
			}
		})
	}
}
