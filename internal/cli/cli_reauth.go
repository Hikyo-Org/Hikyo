package cli

import (
	"cmp"
	"context"
	"fmt"
	"runtime"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/clientauth"
)

// runCLIAdapterReauth binds the callback before opening the browser, then
// silently rotates the local bearer after an exact state + PKCE exchange.
func runCLIAdapterReauth(ctx context.Context, client *Client, state *State, artifact SessionArtifact, operation string, environmentIDs []string, openURL func(string) error) error {
	return runCLIReauthHandoff(ctx, client, state, artifact, "adapter", operation, environmentIDs, nil, openURL)
}

// runCLIDisclosureReauth is the browser handoff for a disclosure the terminal
// cannot satisfy itself: a 0-window or protected environment, where only the
// purpose-bound, enumerated-key-set passkey ceremony the UI runs may open a
// window (api-cli-surface ADR § Login and reauth transports). The key set is
// the unit the browser decision covers and the unit the disclosure consumes.
func runCLIDisclosureReauth(ctx context.Context, client *Client, state *State, artifact SessionArtifact, purpose, operation, environmentID string, keyIDs []string, openURL func(string) error) error {
	if len(keyIDs) == 0 {
		return failf(ExitRefused, "a disclosure ceremony needs the keys it covers, and none resolved")
	}
	return runCLIReauthHandoff(ctx, client, state, artifact, purpose, operation, []string{environmentID}, keyIDs, openURL)
}

func runCLIReauthHandoff(ctx context.Context, client *Client, state *State, artifact SessionArtifact, purpose, operation string, environmentIDs, keyIDs []string, openURL func(string) error) error {
	return runCLIReauthHandoffTarget(ctx, client, state, artifact, purpose, operation, environmentIDs, keyIDs, nil, openURL)
}

func runCLIReauthHandoffTarget(ctx context.Context, client *Client, state *State, artifact SessionArtifact, purpose, operation string, environmentIDs, keyIDs []string, selfConfig *apigen.SelfConfigReauthIntent, openURL func(string) error) error {
	return runCLIReauthHandoffIntents(ctx, client, state, artifact, purpose, operation, environmentIDs, keyIDs, selfConfig, nil, openURL)
}

func runCLIReauthHandoffIntents(ctx context.Context, client *Client, state *State, artifact SessionArtifact, purpose, operation string, environmentIDs, keyIDs []string, selfConfig *apigen.SelfConfigReauthIntent, developer *apigen.DeveloperCredentialReauthIntent, openURL func(string) error) error {
	rotated, err := clientauth.Reauth(ctx, authTransport{client}, clientauth.ReauthRequest{
		Purpose: purpose, Operation: operation, EnvironmentIDs: environmentIDs, KeyIDs: keyIDs,
		SelfConfig: selfConfig, DeveloperCredential: developer,
	}, openURL)
	if err != nil {
		return err
	}
	return storeRotatedSession(client, state, artifact, rotated)
}

// OpenBrowser launches the platform browser for the CLI front channel.
func OpenBrowser(target string) error { return clientauth.OpenBrowser(target) }

// noGraphicalSession reports a host where a browser cannot be shown: any OS but
// macOS and Windows with none of DISPLAY, WAYLAND_DISPLAY and BROWSER. Launchers such as xdg-open start
// fine there and then never show anything, so the handoff would wait out its
// five minutes. macOS and Windows are not checked: `open` over SSH still targets
// the GUI session. A set BROWSER is honoured by xdg-open and VS Code Remote, so
// a host with it can complete the handoff without a display.
func noGraphicalSession(goos string, getenv func(string) string) bool {
	switch goos {
	case "darwin", "windows":
		return false
	}
	return getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "" && getenv("BROWSER") == ""
}

// browserChoice says which control made the browser the ceremony and what undoes
// it. The remedy is conditional because a window above 0 only offers a code;
// it does not prove an authenticator is enrolled. Every chosen-browser message
// is built from it.
func (s *State) browserChoice(instance string) (cause, remedy string, err error) {
	m, err := s.ReauthMethodFor(instance)
	if err != nil {
		return "", "", err
	}
	setting := m == ReauthBrowser
	config := "`hikyo config set reauth.method auto --instance " + instance + "`"
	switch {
	case s.forceBrowser && setting:
		return "--approve-in-browser and reauth.method", "drop --approve-in-browser and run " + config, nil
	case setting:
		return "reauth.method", "run " + config, nil
	default:
		return "--approve-in-browser", "drop --approve-in-browser", nil
	}
}

// chosenBrowserOpener is the opener for a ceremony the account picked with
// reauth.method or --approve-in-browser although a typed code was offered. It
// refuses before the handoff starts on a host with no graphical session, says so
// before the handoff waits, and names the remedy when no browser can be opened.
// The handoff URL is not printed: it carries the opaque transaction state (see
// OpenBrowser).
func chosenBrowserOpener(ios IO, st *State, instance string) (func(string) error, error) {
	cause, remedy, err := st.browserChoice(instance)
	if err != nil {
		return nil, err
	}
	tail := remedy + " to enter an authenticator code here, if one is enrolled"
	if noGraphicalSession(cmp.Or(ios.HostOS, runtime.GOOS), ios.Env.Getenv) {
		return nil, failf(ExitAuth, "you chose the browser approval (%s), but this host has no graphical session (DISPLAY, WAYLAND_DISPLAY and BROWSER are all empty); %s", cause, tail)
	}
	open := ios.OpenURL
	if open == nil {
		open = OpenBrowser
	}
	return func(target string) error {
		_, _ = fmt.Fprintf(ios.Stderr, "You chose the browser approval (%s); waiting for it. With no browser here, press Ctrl-C and %s.\n", cause, tail)
		if err := open(target); err != nil {
			return fmt.Errorf("%w; you chose the browser approval (%s), so no code prompt was offered: %s", err, cause, tail)
		}
		return nil
	}, nil
}

// redeemCLIReauth completes a browser-approved handoff and replaces the local
// bearer without returning it to any rendering or logging path.
func redeemCLIReauth(ctx context.Context, client *Client, state *State, artifact SessionArtifact, code string, verifier []byte) error {
	rotated, err := clientauth.RedeemReauth(ctx, authTransport{client}, code, verifier)
	if err != nil {
		return err
	}
	return storeRotatedSession(client, state, artifact, rotated)
}

func storeRotatedSession(client *Client, state *State, artifact SessionArtifact, rotated clientauth.RotatedSession) error {
	artifact.Token = rotated.Token
	artifact.SessionID = rotated.SessionID
	if err := state.PutSession(artifact); err != nil {
		return err
	}
	client.Bearer = rotated.Token
	return nil
}
