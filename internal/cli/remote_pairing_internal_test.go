package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func pairingTestIO(t *testing.T) IO {
	t.Helper()
	stateDir := t.TempDir()
	return IO{
		Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
		Workdir: t.TempDir(), Env: Env{Getenv: func(name string) string {
			if name == "HIKYO_STATE_DIR" {
				return stateDir
			}
			return ""
		}},
		TerminalError: errors.New("no controlling terminal"),
		ReadPassword: func(string) (string, error) {
			t.Fatal("the command requested secret input before validating its destination")
			return "", errors.New("unexpected prompt")
		},
	}
}

func TestRemotePairingReservesDisclosureBeforeAuthentication(t *testing.T) {
	for _, args := range [][]string{
		{"remote-pairing", "prepare"},
		{"remote-pairing", "prepare", "--custom-secret"},
		{"remote-invite", "create", "--label", "peer"},
	} {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			ios := pairingTestIO(t)
			if got := Run(t.Context(), ios, args); got != ExitRefused {
				t.Fatalf("exit %d, want disclosure refusal: %s", got, ios.Stderr)
			}
			if !strings.Contains(ios.Stderr.(*bytes.Buffer).String(), "nowhere to go") {
				t.Fatal("command reached authentication instead of refusing absent disclosure custody")
			}
		})
	}
}

func TestPairingContinuationRequiresPrivateBoundedUnexpiredJSON(t *testing.T) {
	const secret = "opaque-private-requester-state"
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	valid, err := json.Marshal(apigen.RemotePairingPrepared{
		Phrase: "not-a-real-generated-phrase", RequesterState: secret, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		raw  string
		mode os.FileMode
		want bool
	}{
		{"private continuation", string(valid), 0o600, true},
		{"trailing JSON", string(valid) + "{}", 0o600, false},
		{"unknown fields", `{"requester_state":"x","expires_at":"2026-10-20T13:00:00Z","extra":true}`, 0o600, false},
		{"oversize input", strings.Repeat("x", pairingRequestFileLimit+1), 0o600, false},
		{"shared readable", string(valid), 0o644, false},
		{"expired", `{"requester_state":"x","expires_at":"2026-10-20T11:00:00Z"}`, 0o600, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if runtime.GOOS == "windows" && tc.mode == 0o644 {
				t.Skip("POSIX mode refusal is covered on Unix")
			}
			path := filepath.Join(t.TempDir(), "request.json")
			if err := os.WriteFile(path, []byte(tc.raw), tc.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			ios := pairingTestIO(t)
			ios.Now = func() time.Time { return now }
			got, err := readPairingContinuation(ios, path)
			if tc.want {
				if err != nil || got != secret {
					t.Fatalf("valid private continuation was refused: %v", err)
				}
			} else if err == nil {
				t.Fatal("unsafe continuation accepted")
			} else if strings.Contains(err.Error(), secret) {
				t.Fatal("refusal disclosed requester state")
			}
		})
	}
}

func TestPairingInputRefusesSymbolicLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	dir := t.TempDir()
	target, link := filepath.Join(dir, "invitation"), filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("private-invitation-capability"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readPairingInput(pairingTestIO(t), link, "", 8192); err == nil {
		t.Fatal("symlink invitation accepted")
	}
}

func TestRemotePairingSecretsHaveNoArgumentFlags(t *testing.T) {
	for _, args := range [][]string{
		{"remote-pairing", "accept", "peer", "--requester-state", "secret"},
		{"remote-pairing", "accept", "peer", "--invite", "secret"},
		{"remote-invite", "create", "--phrase", "secret"},
		{"remote-pairing", "prepare", "--phrase", "secret"},
	} {
		if got := Run(t.Context(), pairingTestIO(t), args); got != ExitUsage {
			t.Fatalf("secret argv flag must be refused, exit %d", got)
		}
	}
}

func TestPairingContinuationAcceptsLargeEscapedSecretEnvelope(t *testing.T) {
	ios := pairingTestIO(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	ios.Now = func() time.Time { return now }
	state := strings.Repeat("x", pairingStateLimit)
	raw, err := json.Marshal(apigen.RemotePairingPrepared{
		Phrase: strings.Repeat("\x00", 4096), RequesterState: state, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readPairingContinuation(ios, path)
	if err != nil || got != state {
		t.Fatal("valid large continuation envelope was refused")
	}
}

func TestPairingInviteListReportsExpiredAtDeadline(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	list := apigen.RemotePairingInviteList{Items: []apigen.RemotePairingInvite{
		{Id: "expired", ExpiresAt: now},
		{Id: "pending", ExpiresAt: now.Add(time.Second)},
		{Id: "consumed", ExpiresAt: now, ConsumedAt: &now},
		{Id: "revoked", ExpiresAt: now, RevokedAt: &now},
	}}
	table := pairingInviteTable(list, now)
	for index, want := range []string{"expired", "pending", "consumed", "revoked"} {
		if table.Rows[index][3] != want {
			t.Fatalf("invitation %s has state %s, want %s", table.Rows[index][0], table.Rows[index][3], want)
		}
	}
}

func TestRemotePairingRefusesInvalidLengthOptionsBeforeInput(t *testing.T) {
	for _, args := range [][]string{
		{"remote-pairing", "prepare", "--words", "0"},
		{"remote-pairing", "prepare", "--words", "129"},
		{"remote-pairing", "prepare", "--words", "5", "--custom-secret"},
	} {
		if got := Run(t.Context(), pairingTestIO(t), args); got != ExitUsage {
			t.Fatalf("invalid preparation choices exit%d, want usage refusal", got)
		}
	}
}
