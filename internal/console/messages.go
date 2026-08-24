// Package console renders calm, human-readable messages for Hikyo's terminal
// surfaces. Structured operational events remain the responsibility of slog.
package console

import (
	"fmt"
	"strings"
)

// VersionInfo describes one Hikyo binary build.
type VersionInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

// VersionMessage renders the human-facing output of `hikyo version`.
func VersionMessage(info VersionInfo) string {
	if info.Version == "dev" {
		return "Hikyo dev\n"
	}
	var message strings.Builder
	fmt.Fprintf(&message, "Hikyo %s\n", info.Version)
	fmt.Fprintf(&message, "  Commit  %s\n", info.Commit)
	fmt.Fprintf(&message, "  Built   %s\n", info.BuildDate)
	return message.String()
}

// AboutMessage renders the explicit product-information screen.
func AboutMessage(info VersionInfo) string {
	return FullArtwork() + "\n" + VersionMessage(info) +
		"\nValidated secrets and configuration across environments.\n"
}

// WelcomeMessage renders the explicit first-contact screen.
func WelcomeMessage(info VersionInfo) string {
	return FullArtwork() + "\n" + fmt.Sprintf("Welcome to Hikyo %s\n", info.Version) +
		"Run hikyo to see available commands.\n"
}

// ServerInfo describes the endpoints and mode of a ready server process.
type ServerInfo struct {
	Version        string
	AppURL         string
	ListenAddress  string
	OperationalURL string
	Mode           string
}

// ServerReadyMessage renders the summary shown after both listeners bind.
func ServerReadyMessage(info ServerInfo) string {
	var message strings.Builder
	message.WriteString("Hikyo server is ready\n")
	fmt.Fprintf(&message, "  Version     %s\n", info.Version)
	fmt.Fprintf(&message, "  App         %s\n", info.AppURL)
	fmt.Fprintf(&message, "  Listen      %s\n", info.ListenAddress)
	fmt.Fprintf(&message, "  Operations  %s\n", info.OperationalURL)
	fmt.Fprintf(&message, "  Mode        %s\n", info.Mode)
	return message.String()
}

// ServerStartupMessage keeps readiness diagnostics on interactive terminals.
func ServerStartupMessage(info ServerInfo, interactive bool) string {
	if !interactive {
		return ""
	}
	return ServerReadyMessage(info)
}

// UpdateInfo describes the installed and selected Hikyo release.
type UpdateInfo struct {
	Current    string
	Latest     string
	Channel    string
	ReleaseURL string
}

// UpdateCheckMessage renders progress before the release source is queried.
func UpdateCheckMessage(channel string) string {
	return fmt.Sprintf("Checking for updates on %s...\n", channel)
}

// UpdateCurrentMessage renders a successful check with no newer release.
func UpdateCurrentMessage(info UpdateInfo) string {
	return fmt.Sprintf("Hikyo is up to date\n  Version  %s\n  Channel  %s\n", info.Current, info.Channel)
}

// UpdateAvailableMessage renders a new release and the action target.
func UpdateAvailableMessage(info UpdateInfo) string {
	return fmt.Sprintf("Hikyo update available\n  Installed  %s\n  Latest     %s\n  Channel    %s\n  Release    %s\n",
		info.Current, info.Latest, info.Channel, info.ReleaseURL)
}
