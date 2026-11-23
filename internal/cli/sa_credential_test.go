package cli_test

import (
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

// mintCeremonyServer refuses a service-account mint, naming one environment
// per refusal, until the session holds a window over every environment in
// `reach`; each inline TOTP reauthentication opens one window and rotates the
// bearer. `stuck` names an environment whose window never satisfies the mint.
func mintCeremonyServer(t *testing.T, reach []string, stuck string) (http.Handler, *int, *int) {
	t.Helper()
	open := map[string]bool{}
	bearer := "test-token"
	mints, reauths := 0, 0
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer "+bearer {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthenticated","message":"authentication required"}}`))
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/reveal-window"):
			_ = jsonwire.Encode(w, apigen.RevealWindow{CanReveal: true, EffectiveWindowSeconds: 300, TotpOffered: true})
		case strings.HasSuffix(r.URL.Path, "/auth/totp"):
			_ = jsonwire.Encode(w, apigen.TotpStatus{Confirmed: true})
		case strings.HasSuffix(r.URL.Path, "/auth/reauth/totp"):
			var body apigen.TotpReauthRequest
			_ = json.UnmarshalRead(r.Body, &body)
			env, err := body.AsTotpEnvironmentReauthRequest()
			if err != nil || env.Code != "123456" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"code":"unauthenticated","message":"authentication required"}}`))
				return
			}
			reauths++
			if string(env.EnvironmentId) != stuck {
				open[string(env.EnvironmentId)] = true
			}
			bearer = "hik_1_cli_rotated-" + string(env.EnvironmentId)
			_ = jsonwire.Encode(w, apigen.ReauthResult{
				EnvironmentId: env.EnvironmentId, SessionId: "ses_rotated", SessionToken: &bearer,
				WindowExpires: apigen.Timestamp(time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)),
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/service-accounts/sa_1/credentials"):
			mints++
			for _, env := range reach {
				if !open[env] {
					w.WriteHeader(http.StatusConflict)
					_, _ = w.Write([]byte(`{"error":{"code":"conflict","message":"the current state of this resource refuses the request",` +
						`"detail":"reauthenticate over the environments this operation makes reachable, then retry (` + env + `)"}}`))
					return
				}
			}
			expires := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
			hint := "hik_1_au_ab"
			w.WriteHeader(http.StatusCreated)
			_ = jsonwire.Encode(w, apigen.MintCredentialResult{
				Value: "hik_1_au_minted-secret", ExpiresAt: &expires,
				Credential: apigen.MachineCredential{Id: "mcr_new", Kind: apigen.HikyoToken, Lifetime: apigen.Finite, ExpiresAt: &expires, PrefixHint: &hint},
			})
		default:
			http.NotFound(w, r)
		}
	}), &mints, &reauths
}

func mintArgs() []string {
	return []string{"sa", "credential", "mint", "--sa", "sa_1", "--lifetime", "720h", "--dangerously-print",
		"--instance", "local", "--org", "org_70", "--project", "prj_70"}
}

// Issue #1516: the server names one missing environment per refusal, so an
// account reaching two environments takes two inline ceremonies and one mint.
func TestServiceAccountMintReauthenticatesOverEveryNamedEnvironment(t *testing.T) {
	handler, mints, reauths := mintCeremonyServer(t, []string{"env_a", "env_b"}, "")
	ios, stdout, stderr := definitionsTestIO(t, handler)
	var prompted []string
	ios.ReadPassword = func(prompt string) (string, error) {
		prompted = append(prompted, prompt)
		return "123456", nil
	}
	if code := cli.Run(t.Context(), ios, mintArgs()); code != cli.ExitOK {
		t.Fatalf("exit %d, want ok; stderr=%s", code, stderr.String())
	}
	if *mints != 3 || *reauths != 2 || len(prompted) != 2 {
		t.Fatalf("mints=%d reauths=%d prompts=%d, want 3, 2, 2", *mints, *reauths, len(prompted))
	}
	if !strings.HasPrefix(prompted[0], "Minting a credential over env_a") || !strings.HasPrefix(prompted[1], "Minting a credential over env_b") {
		t.Errorf("prompts do not name the refused environments in order: %q", prompted)
	}
	if !strings.Contains(stdout.String(), "hik_1_au_minted-secret") {
		t.Errorf("the minted value did not reach the chosen destination: %q", stdout.String())
	}
}

// A window that does not satisfy the mint must not loop: the second refusal
// over the same environment is returned as the server's answer.
func TestServiceAccountMintStopsWhenAWindowDoesNotSatisfyTheMint(t *testing.T) {
	handler, mints, reauths := mintCeremonyServer(t, []string{"env_a"}, "env_a")
	ios, _, stderr := definitionsTestIO(t, handler)
	ios.ReadPassword = func(string) (string, error) { return "123456", nil }
	if code := cli.Run(t.Context(), ios, mintArgs()); code != cli.ExitRefused {
		t.Fatalf("exit %d, want refused; stderr=%s", code, stderr.String())
	}
	if *mints != 2 || *reauths != 1 {
		t.Fatalf("mints=%d reauths=%d, want 2 and 1", *mints, *reauths)
	}
	if !strings.Contains(stderr.String(), "reauthenticate over the environments") {
		t.Errorf("the server's refusal was replaced: %s", stderr.String())
	}
}

// Issue #1516: `expiring_soon` is false once the expiry has passed, so the
// status must read the expiry itself rather than fall through to `live`.
func TestServiceAccountCredentialListMarksExpired(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	past, soon, later := now.Add(-time.Hour), now.Add(72*time.Hour), now.Add(90*24*time.Hour)
	hint := "hik_1_au_ab"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = jsonwire.Encode(w, apigen.MachineCredentialList{Count: 4, Items: []apigen.MachineCredential{
			{Id: "mcr_expired", Kind: apigen.HikyoToken, Lifetime: apigen.Finite, ExpiresAt: &past, PrefixHint: &hint},
			{Id: "mcr_soon", Kind: apigen.HikyoToken, Lifetime: apigen.Finite, ExpiresAt: &soon, ExpiringSoon: true, PrefixHint: &hint},
			{Id: "mcr_live", Kind: apigen.HikyoToken, Lifetime: apigen.Finite, ExpiresAt: &later, PrefixHint: &hint},
			{Id: "mcr_revoked", Kind: apigen.HikyoToken, Lifetime: apigen.Finite, ExpiresAt: &past, RevokedAt: &past, PrefixHint: &hint},
		}})
	})
	ios, stdout, stderr := definitionsTestIO(t, handler)
	ios.Now = func() time.Time { return now }
	args := []string{"sa", "credential", "list", "--sa", "sa_1", "--instance", "local", "--org", "org_70", "--project", "prj_70"}
	if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK {
		t.Fatalf("exit %d; stderr=%s", code, stderr.String())
	}
	want := map[string]string{"mcr_expired": "expired", "mcr_soon": "expiring-soon", "mcr_live": "live", "mcr_revoked": "revoked"}
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if status, ok := want[fields[0]]; ok {
			if got := fields[len(fields)-1]; got != status {
				t.Errorf("%s status %q, want %q", fields[0], got, status)
			}
			delete(want, fields[0])
		}
	}
	if len(want) != 0 {
		t.Fatalf("rows missing from the listing: %v\n%s", want, stdout.String())
	}
}
