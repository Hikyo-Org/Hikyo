package cli

import (
	"context"

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
