package cli

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/disclose"
)

type countedTrustListener struct {
	net.Listener
	accepted atomic.Int32
}

func (l *countedTrustListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.accepted.Add(1)
	}
	return conn, err
}

func firstContactServer(t *testing.T) (*httptest.Server, *countedTrustListener, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(http.StatusNotFound) }))
	listener := &countedTrustListener{Listener: server.Listener}
	server.Listener = listener
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, listener, &requests
}

func firstContactIO(t *testing.T, answers string) (IO, *answeringTTY) {
	t.Helper()
	ios, _, _ := composeIO(t.TempDir(), t.TempDir(), "", nil)
	tty := &answeringTTY{in: strings.NewReader(answers)}
	session, err := disclose.NewTerminalSession(tty)
	if err != nil {
		t.Fatal(err)
	}
	ios.TerminalSession = session
	return ios, tty
}

func TestNewHTTPSInteractiveEstablishmentRequiresExplicitTLSModeBeforeNetwork(t *testing.T) {
	for _, verb := range []string{"login", "context"} {
		t.Run(verb, func(t *testing.T) {
			server, listener, requests := firstContactServer(t)
			ios, _ := firstContactIO(t, "y\n")
			args := []string{"context", "create", "ci", "--instance", server.URL}
			if verb == "login" {
				args = []string{"login", server.URL, "--local", "--as", "user"}
			}
			code := Run(t.Context(), ios, args)
			if code != ExitRefused {
				t.Errorf("omitted TLS mode exit=%d, want refusal", code)
			}
			if listener.accepted.Load() != 0 || requests.Load() != 0 {
				t.Errorf("omitted TLS mode contacted server: accepted=%d requests=%d", listener.accepted.Load(), requests.Load())
			}
			state, err := NewState(ios.Env)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := state.Trust().Load()
			if err != nil || len(entries) != 0 {
				t.Errorf("omitted TLS mode recorded trust: %v %v", entries, err)
			}
		})
	}
}

func TestRemoteAddRequiresConsentBeforeUnverifiedTLSProbe(t *testing.T) {
	for _, tc := range []struct {
		name, answers   string
		wantConnections int32
	}{
		{"no terminal", "", 0},
		{"inspection declined", "n\n", 0},
		{"fingerprint declined", "y\nn\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, listener, requests := firstContactServer(t)
			ios, tty := firstContactIO(t, tc.answers)
			if tc.name == "no terminal" {
				ios.TerminalSession = nil
			}
			state, err := NewState(ios.Env)
			if err != nil {
				t.Fatal(err)
			}
			err = addRemote(t.Context(), ios, state, commonFlags{}, FormatTable, "peer", server.URL)
			if err == nil {
				t.Fatal("remote without inspection and fingerprint consent succeeded")
			}
			if listener.accepted.Load() != tc.wantConnections || requests.Load() != 0 {
				t.Errorf("unconsented remote access: accepted=%d want=%d requests=%d", listener.accepted.Load(), tc.wantConnections, requests.Load())
			}
			if tc.name != "no terminal" {
				for _, text := range []string{server.URL, "SPKI", "no CA", "validity", "hostname", "No application request or credential"} {
					if !strings.Contains(tty.out.String(), text) {
						t.Errorf("inspection prompt omitted %q: %s", text, tty.out.String())
					}
				}
			}
		})
	}
}

func TestTLSIdentityInspectionRejectsEmptyOrUnknownPolicyBeforeDial(t *testing.T) {
	for _, policy := range []TLSMode{"", "unknown"} {
		t.Run(string(policy), func(t *testing.T) {
			server, listener, requests := firstContactServer(t)
			if _, err := inspectTLSIdentity(t.Context(), server.URL, time.Second, policy); err == nil {
				t.Error("invalid inspection policy accepted")
			}
			if listener.accepted.Load() != 0 || requests.Load() != 0 {
				t.Errorf("invalid policy dialed: accepted=%d requests=%d", listener.accepted.Load(), requests.Load())
			}
		})
	}
}

func TestExplicitSPKIEstablishmentRequiresFingerprintConsent(t *testing.T) {
	for _, answer := range []string{"y\n", "n\n"} {
		t.Run(answer, func(t *testing.T) {
			server, listener, requests := firstContactServer(t)
			ios, tty := firstContactIO(t, answer)
			state, err := NewState(ios.Env)
			if err != nil {
				t.Fatal(err)
			}
			entry, err := establishWithTLSMode(ios, state, server.URL, "ci", "", "", TLSModeSPKI)
			if (err == nil) != (answer == "y\n") {
				t.Fatalf("fingerprint consent entry=%+v err=%v", entry, err)
			}
			entries, loadErr := state.Trust().Load()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if answer == "y\n" && (len(entries) != 1 || entry.SPKIPin != SPKIFingerprint(server.Certificate())) {
				t.Fatal("explicit SPKI did not record confirmed key")
			}
			if answer == "n\n" && len(entries) != 0 {
				t.Fatal("declined fingerprint recorded trust")
			}
			if listener.accepted.Load() != 1 || requests.Load() != 0 {
				t.Fatalf("SPKI probe sent an application request or no TLS probe: accepted=%d requests=%d", listener.accepted.Load(), requests.Load())
			}
			for _, text := range []string{"spki", "no CA", "validity", "hostname"} {
				if !strings.Contains(tty.out.String(), text) {
					t.Errorf("SPKI confirmation omitted %q: %s", text, tty.out.String())
				}
			}
		})
	}
}

func TestExistingAndProvisionedSPKITrustDoesNotRequireModeFlag(t *testing.T) {
	server, listener, requests := firstContactServer(t)
	for _, provisioned := range []bool{false, true} {
		t.Run(map[bool]string{false: "stored", true: "provisioned"}[provisioned], func(t *testing.T) {
			ios := IO{Stdout: io.Discard, Stderr: io.Discard, Env: Env{Getenv: func(string) string { return "" }}}
			state := &State{dir: t.TempDir()}
			entry := TrustEntry{Name: "ci", Origin: server.URL, SPKIPin: SPKIFingerprint(server.Certificate())}
			bundlePath := ""
			if provisioned {
				raw, err := json.Marshal(entry)
				if err != nil {
					t.Fatal(err)
				}
				bundlePath = filepath.Join(t.TempDir(), "trust.json")
				if err := os.WriteFile(bundlePath, raw, 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := state.Trust().Put(entry); err != nil {
				t.Fatal(err)
			}
			got, err := establishWithTLSMode(ios, state, "ci", "", bundlePath, "", "")
			if err != nil || got.SPKIPin != entry.SPKIPin {
				t.Fatalf("legacy trust without flag=%+v %v", got, err)
			}
			if listener.accepted.Load() != 0 || requests.Load() != 0 {
				t.Fatal("existing or provisioned trust required a new probe")
			}
		})
	}
}

func TestContextCreateExplicitSPKIFlagEstablishesConfirmedSelfSignedKey(t *testing.T) {
	server, listener, requests := firstContactServer(t)
	ios, _ := firstContactIO(t, "y\n")
	if code := Run(t.Context(), ios, []string{"context", "create", "ci", "--instance", server.URL, "--tls-mode", "spki"}); code != 0 {
		t.Fatalf("explicit SPKI context creation exit=%d", code)
	}
	state, err := NewState(ios.Env)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := state.Trust().Load()
	if err != nil || len(entries) != 1 {
		t.Fatalf("confirmed trust=%v err=%v", entries, err)
	}
	for _, entry := range entries {
		if entry.TLSMode != TLSModeSPKI || entry.SPKIPin != SPKIFingerprint(server.Certificate()) {
			t.Fatalf("recorded wrong explicit SPKI trust: %+v", entry)
		}
	}
	if listener.accepted.Load() != 1 || requests.Load() != 0 {
		t.Fatalf("probe sent application bytes: connections=%d requests=%d", listener.accepted.Load(), requests.Load())
	}
}
