package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// The CLI half of the disclosure reauthentication ceremony (api-cli-surface
// ADR § Login and reauth transports: "local-account sessions may satisfy a
// window > 0 with inline terminal TOTP where #16 permits TOTP"; human-auth ADR
// § Assurance).
//
// A disclosure - `values get/list/diff/export --reveal`, a copy that opens
// secret material, `run --use-human-session` - is refused by the server until
// the acting session holds a live reauthentication window over the
// environment it discloses in. The window's policy is the server's: whether
// one is live, how long the environment's effective window is, and whether
// TOTP may open it (it cannot where the effective window is 0 - a protected
// environment, or the instance default - because TOTP cannot bind a challenge
// to an enumerated key set). The CLI reads that answer and performs the one
// transport it owns: an authenticator code typed at the controlling terminal,
// presented to `/auth/reauth/totp`, which rotates the session token. The
// rotated token MUST replace the stored one, or every later verb in the same
// shell answers "authentication required" - the failure the readiness audit
// reproduced.
//
// A 0-window environment has no terminal path by design: the ceremony there is
// the browser's purpose-bound passkey ceremony. The refusal names both ways
// out rather than pretending a code would do.

// revealWindowPath is the reveal guard's read surface for one environment.
func revealWindowPath(projectBase, env string) string {
	return projectBase + "/environments/" + url.PathEscape(env) + "/reveal-window"
}

// forbidden reports whether err is a refusal the reauthentication ceremony can
// cure: the server's uniform 403 (the shape every disclosure without a live
// window answers with), or the widening conjunct's 409 naming reauthentication
// (a grant that makes machine plaintext reachable consumes the grantor's window
// over the environments it reaches - machine-identities ADR). Other refusals
// (a missing grant answers 403 too, but `ensureRevealWindow` then finds
// `can_reveal` false and hands the original error back) are not retried.
func forbidden(err error) bool {
	var e *Error
	if !errors.As(err, &e) || e.Code != ExitRefused {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "not permitted") || strings.Contains(msg, "reauthenticate over the environments")
}

// disclosure names what a CLI disclosure is about to do, for the ceremony
// that may have to precede it: the purpose and the keys per environment.
type disclosure struct {
	// purpose is a non-adapter service.ReauthPurpose on the wire.
	purpose string
	// keys resolves the enumerated unit for one environment: the ids of the
	// secret keys the act will open there. It is consulted only when a
	// browser handoff is needed, so the common case costs no extra request.
	keys func(ctx context.Context, env string) ([]string, error)
	// protectedOnly skips the ceremony on an environment that is not protected.
	protectedOnly bool
	// window overrides the read-authorized reveal-window route for decisions
	// whose own operation is authorized by publish instead.
	window func(ctx context.Context, env string) (apigen.RevealWindow, error)
	// authorized means the server already checked the caller's own disclosure
	// authority and asked only for the window (a machine widening names
	// reveal or reveal-history by what it reaches), so can_reveal, which is
	// the current-plaintext question only, must not veto the ceremony.
	authorized bool
}

// withRevealCeremony runs a disclosure, and on the server's refusal opens the
// reauthentication window over each named environment and runs it exactly
// once more: inline TOTP where the environment's window allows it, the
// browser's purpose-bound passkey ceremony (handoff) where it does not. The
// attempt comes first so a live window costs no extra round trip and a
// config-only copy never prompts.
func withRevealCeremony(ctx context.Context, client *Client, st *State, ios IO, artifact AuthArtifact,
	projectBase string, envs []string, d disclosure, attempt func() error) error {
	err := attempt()
	if err == nil || !forbidden(err) {
		return err
	}
	session, sessionErr := requireHumanSession("CLI reauthentication", artifact)
	if sessionErr != nil {
		return sessionErr
	}
	for _, env := range envs {
		if cerr := ensureRevealWindow(ctx, client, st, ios, &session, projectBase, env, d, err); cerr != nil {
			return cerr
		}
	}
	return attempt()
}

// totpPromptSubject opens the TOTP prompt with what the code is for.
func totpPromptSubject(purpose, env string) string {
	switch purpose {
	case "mint":
		return fmt.Sprintf("Minting a credential over %s", env)
	case "":
		return fmt.Sprintf("Widening machine reveal access over %s", env)
	case "access":
		return fmt.Sprintf("Emergency access in %s", env)
	case "publish":
		return fmt.Sprintf("Publishing in %s", env)
	case "approve", "reject", "bypass":
		return fmt.Sprintf("An approval decision in %s", env)
	}
	return fmt.Sprintf("Disclosure in %s", env)
}

// ensureRevealWindow opens a live reauthentication window over env for the
// acting session, or returns an error naming why it cannot. `refusal` is the
// action's own error, returned unchanged when no window is live and CanReveal
// is false, except for approve, reject, bypass, and emergency-access decisions.
// Those decisions use their own authorization checks. Inline TOTP persists
// the rotated session and updates the client's bearer. Window lookup, ceremony,
// and session-persistence errors propagate. Emergency access has no browser
// handoff here; without inline TOTP it returns a CLI authentication error.
func ensureRevealWindow(ctx context.Context, client *Client, st *State, ios IO, artifact *SessionArtifact,
	projectBase, env string, d disclosure, refusal error) error {
	var window apigen.RevealWindow
	if d.window != nil {
		var err error
		window, err = d.window(ctx, env)
		if err != nil {
			return err
		}
	} else if err := client.Do(ctx, http.MethodGet, revealWindowPath(projectBase, env), nil, &window); err != nil {
		return err
	}
	// A successful disclosure spends a single-decision window, so one still live
	// here is stale and possibly bound to other keys. A new ceremony replaces it.
	if window.Live && !window.SingleDecision {
		return nil
	}
	// Approval votes and bypasses require publish authority, not reveal. Their
	// failed action is the authoritative capability check, so the reveal-window
	// affordance must not block their purpose-bound ceremony.
	// Emergency temporary access (#152) is likewise authorized by its own
	// operation, not by reveal.
	// A publish ceremony (declare, credential rotation) authorizes publish, not
	// reveal, so the built-in publisher and maintainer roles must reach it.
	// Declare owes it only on a protected environment; rotation's is unconditional.
	if d.protectedOnly && !window.Protected {
		return nil
	}
	exempt := d.purpose == "approve" || d.purpose == "reject" || d.purpose == "bypass" || d.purpose == "access" || d.purpose == "publish" || d.authorized
	if !window.CanReveal && !exempt {
		return refusal
	}
	// Dispatch by what the environment allows AND what the account can
	// present (api-cli-surface ADR: "by how the current session authenticated
	// and what is enrolled"): inline TOTP needs a window above 0 and an
	// enrolled authenticator; everything else is the browser's ceremony - the
	// passkey where the window is 0, a passkey or the browser's own code entry
	// where it slides but no authenticator stands on this account.
	inlineTOTP := window.EffectiveWindowSeconds > 0 && window.TotpOffered
	if inlineTOTP {
		var status apigen.TotpStatus
		if err := client.Do(ctx, http.MethodGet, api.PathPrefix+"/auth/totp", nil, &status); err != nil {
			return err
		}
		inlineTOTP = status.Confirmed
	}
	if !inlineTOTP {
		// remedy is the one step that can open a window here: a protected
		// environment has none, so raising its window would change nothing.
		why := "its reveal window is 0"
		remedy := fmt.Sprintf("raise the window with `hikyo project-settings set --env %s --reauth-window-seconds 300`", env)
		switch {
		case window.Protected:
			why, remedy = "it is a protected environment", ""
		case window.EffectiveWindowSeconds > 0:
			why, remedy = "no authenticator is enrolled on this account",
				fmt.Sprintf("enroll an authenticator with `hikyo account factor enrol-totp` or at %s", accountSecurityURL(client))
		}
		if d.purpose == "access" {
			return failf(ExitAuth, "emergency access in %s needs a reauthentication window and %s; open the window in the browser or enroll an authenticator", env, why)
		}
		if d.purpose == "mint" {
			// The unit of a mint is its environment, so it carries no keys (#1516).
			if ios.OpenURL == nil {
				return failf(ExitAuth, "a credential mint over %s needs a reauthentication window and %s: the ceremony is the browser's, "+
					"which this invocation cannot open; mint it in the browser's Machine access page%s", env, why, orRemedy(remedy))
			}
			_, _ = fmt.Fprintf(ios.Stderr, "credential mint over %s: %s, so the ceremony is the browser's. Opening it to authorize the mint...\n", env, why)
			return runCLIReauthHandoff(ctx, client, st, *artifact, d.purpose, "identity.credential-mint", []string{env}, nil, ios.OpenURL)
		}
		if d.authorized && d.purpose == "" {
			// The browser has no machine rule editor yet (issue 1541), so only a window helps.
			if remedy == "" {
				return failf(ExitAuth, "a rule change that widens a machine into the protected environment %s cannot be made yet: "+
					"a protected environment has no reauthentication window, and the browser cannot edit machine rules yet", env)
			}
			return failf(ExitAuth, "a rule change that widens a machine in %s needs a reauthentication window and %s; %s, then retry", env, why, remedy)
		}
		if d.keys == nil || d.purpose == "" || ios.OpenURL == nil {
			if d.purpose == "publish" {
				return failf(ExitAuth, "a publish in %s needs a reauthentication ceremony and %s: the ceremony is the browser's, "+
					"which this invocation cannot open; complete the publish in the browser", env, why)
			}
			return failf(ExitAuth, "a disclosure in %s needs a reauthentication window and %s: the ceremony is the browser's, "+
				"which this invocation cannot open; reveal it in the browser%s", env, why, orRemedy(remedy))
		}
		keyIDs, err := d.keys(ctx, env)
		if err != nil {
			return err
		}
		operation := map[string]string{
			"reveal":  "value.reveal",
			"copy":    "value.copy-source",
			"publish": "value.copy-destination",
			"approve": "approval.vote",
			"reject":  "approval.vote",
			"bypass":  "approval.bypass",
		}[d.purpose]
		if operation == "" {
			return failf(ExitInternal, "unknown reauthentication purpose %q", d.purpose)
		}
		fmt.Fprintf(ios.Stderr, "disclosure in %s: %s, so the ceremony is the browser's. Opening it to authorize %d key(s)...\n", env, why, len(keyIDs))
		return runCLIDisclosureReauth(ctx, client, st, *artifact, d.purpose, operation, env, keyIDs, ios.OpenURL)
	}
	code, err := ios.readPassword(fmt.Sprintf(
		"%s needs a reauthentication window (%ds). Enter the code from your authenticator: ",
		totpPromptSubject(d.purpose, env), window.EffectiveWindowSeconds))
	if err != nil {
		return err
	}
	envID := apigen.ID(env)
	var body apigen.TotpReauthRequest
	if err := body.FromTotpEnvironmentReauthRequest(apigen.TotpEnvironmentReauthRequest{Code: code, EnvironmentId: envID}); err != nil {
		return fmt.Errorf("encode TOTP reauthentication request: %w", err)
	}
	var opened apigen.ReauthResult
	if err := client.Do(ctx, http.MethodPost, api.PathPrefix+"/auth/reauth/totp",
		body, &opened); err != nil {
		return err
	}
	// The window opener rotates the session token. Persist it AND present it on
	// this client from here on; the old bearer is dead the moment the server
	// answered.
	if opened.SessionToken == nil || *opened.SessionToken == "" {
		return failf(ExitInternal, "the server opened a window but returned no rotated session token to a CLI caller")
	}
	artifact.Token = *opened.SessionToken
	artifact.SessionID = string(opened.SessionId)
	if err := st.PutSession(*artifact); err != nil {
		return err
	}
	client.Bearer = *opened.SessionToken
	fmt.Fprintf(ios.Stderr, "reauthentication window open over %s until %s\n", env, opened.WindowExpires.Format("15:04:05 MST"))
	return nil
}

// orRemedy appends a remedy as an alternative, or nothing when there is none.
func orRemedy(remedy string) string {
	if remedy == "" {
		return ""
	}
	return ", or " + remedy
}
