package cli_test

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

// fakeTTY is a controlling terminal: it swallows writes and reads a scripted
// answer for the y/N confirmation.
type fakeTTY struct {
	r          io.Reader
	closeCount int
}

func (f *fakeTTY) Read(p []byte) (int, error)  { return f.r.Read(p) }
func (f *fakeTTY) Write(p []byte) (int, error) { return len(p), nil }
func (f *fakeTTY) Close() error {
	f.closeCount++
	return nil
}

func terminalSession(t *testing.T, answer string) (*disclose.TerminalSession, *fakeTTY) {
	t.Helper()
	tty := &fakeTTY{r: strings.NewReader(answer)}
	session, err := disclose.NewTerminalSession(tty)
	if err != nil {
		t.Fatal(err)
	}
	return session, tty
}

func runHumanServer(t *testing.T, windowLive bool) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/meta"):
			_ = jsonwire.Encode(w, apigen.Meta{ServerVersion: "fixture", ApiRevision: api.Revision})
		case strings.HasSuffix(r.URL.Path, "/personal-values"):
			_ = jsonwire.Encode(w, apigen.PersonalValueList{Items: []apigen.PersonalValueEntry{}})
		case strings.HasSuffix(r.URL.Path, "/reveal-window"):
			_ = jsonwire.Encode(w, apigen.RevealWindow{Live: windowLive})
		case strings.HasSuffix(r.URL.Path, "/revisions/latest"):
			_ = jsonwire.Encode(w, apigen.RevisionDetail{
				Revision: 7,
				Keys: []apigen.SnapshotKey{
					{Name: "LOG_LEVEL", Classification: "config", KeyId: "key_1"},
					{Name: "API_TOKEN", Classification: "secret", KeyId: "key_2", Sensitive: true},
					{Name: "DECLASSIFIED", Classification: "config", KeyId: "key_3", Sensitive: true},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/values/export"):
			var request apigen.ExportValuesRequest
			if err := json.UnmarshalRead(r.Body, &request); err != nil {
				t.Error(err)
			}
			config, secret := "info", "plaintext"
			items := []apigen.ExportedValue{{Name: "LOG_LEVEL", Classification: "config", Revealed: true, Value: &config}}
			if request.Reveal != nil && *request.Reveal {
				items = append(items,
					apigen.ExportedValue{Name: "API_TOKEN", Classification: "secret", Revealed: true, Value: &secret},
					apigen.ExportedValue{Name: "DECLASSIFIED", Classification: "secret", Revealed: true, Value: &secret})
			}
			_ = jsonwire.Encode(w, apigen.ExportedValues{Revision: 7, Items: items, Count: len(items)})
		default:
			http.NotFound(w, r)
		}
	})
}

func runArgs(extra ...string) []string {
	base := []string{"run", "--use-human-session"}
	base = append(base, extra...)
	return append(base, "--instance", "local", "--org", "org_70", "--project", "prj_70", "--env", "env_70", "--", "true")
}

func TestRunHumanSessionRefusedWithoutTTY(t *testing.T) {
	// testIO injects no TerminalSession, so the refusal needs no
	// server and comes before any session lookup.
	ios, _, stderr := definitionsTestIO(t, runHumanServer(t, true))
	if code := cli.Run(t.Context(), ios, runArgs()); code != cli.ExitRefused {
		t.Fatalf("exit %d, want refused; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "controlling terminal") {
		t.Errorf("refusal does not name the missing terminal: %s", stderr.String())
	}
}

func TestRunHumanSessionRefusedWhenStderrIsNotATerminal(t *testing.T) {
	// A controlling terminal exists but stderr is captured: the locked condition
	// "stderr-is-a-TTY" refuses before any session lookup.
	ios, _, stderr := definitionsTestIO(t, runHumanServer(t, true))
	var tty *fakeTTY
	ios.TerminalSession, tty = terminalSession(t, "y\n")
	ios.StderrIsTerminal = func() bool { return false }
	if code := cli.Run(t.Context(), ios, runArgs()); code != cli.ExitRefused {
		t.Fatalf("exit %d, want refused; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "stderr to be a terminal") {
		t.Errorf("refusal does not name stderr: %s", stderr.String())
	}
	if tty.closeCount != 1 {
		t.Errorf("terminal close count = %d, want 1", tty.closeCount)
	}
}

func TestRunHumanSessionRefusedWhenDeclined(t *testing.T) {
	ios, _, stderr := definitionsTestIO(t, runHumanServer(t, true))
	ios.StderrIsTerminal = func() bool { return true }
	var tty *fakeTTY
	ios.TerminalSession, tty = terminalSession(t, "n\n")
	if code := cli.Run(t.Context(), ios, runArgs()); code != cli.ExitRefused {
		t.Fatalf("exit %d, want refused; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "declined") {
		t.Errorf("refusal does not report the decline: %s", stderr.String())
	}
	if tty.closeCount != 1 {
		t.Errorf("terminal close count = %d, want 1", tty.closeCount)
	}
}

func TestRunHumanSessionRequiresLiveWindow(t *testing.T) {
	ios, _, stderr := definitionsTestIO(t, runHumanServer(t, false))
	ios.StderrIsTerminal = func() bool { return true }
	ios.TerminalSession, _ = terminalSession(t, "y\n")
	if code := cli.Run(t.Context(), ios, runArgs()); code != cli.ExitAuth {
		t.Fatalf("exit %d, want ExitAuth; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "live disclosure window is required") {
		t.Errorf("refusal is not the disclosure-window message: %s", stderr.String())
	}
}

func TestRunHumanSessionConfigOnlyStillNeedsTheWindow(t *testing.T) {
	// --config-only carries no secrets, but the exception's four conditions are
	// locked as a set: with a dead window the run is refused, with a live one
	// and a "yes" confirmation it reaches exec (captured here).
	dead, _, stderr := definitionsTestIO(t, runHumanServer(t, false))
	dead.StderrIsTerminal = func() bool { return true }
	dead.TerminalSession, _ = terminalSession(t, "y\n")
	if code := cli.Run(t.Context(), dead, runArgs("--config-only")); code != cli.ExitAuth {
		t.Fatalf("config-only with a dead window: exit %d, want ExitAuth; stderr=%s", code, stderr.String())
	}
	live, _, stderr2 := definitionsTestIO(t, runHumanServer(t, true))
	live.StderrIsTerminal = func() bool { return true }
	var liveTTY *fakeTTY
	live.TerminalSession, liveTTY = terminalSession(t, "y\n")
	var execed bool
	live.Exec = func(argv0 string, argv, env []string) error { execed = true; return nil }
	if code := cli.Run(t.Context(), live, runArgs("--config-only")); code != cli.ExitOK {
		t.Fatalf("exit %d, want ok; stderr=%s", code, stderr2.String())
	}
	if !execed {
		t.Error("config-only human-session run did not reach exec")
	}
	if liveTTY.closeCount != 1 {
		t.Errorf("terminal close count = %d, want 1", liveTTY.closeCount)
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func TestRunHumanSessionConfirmsThenReauthenticatesThenExports(t *testing.T) {
	var order []string
	var exportBearer string
	var exported apigen.ExportValuesRequest
	exports := 0
	fixture := runHumanServer(t, true)
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/reveal-window"):
			_ = jsonwire.Encode(w, apigen.RevealWindow{CanReveal: true, EffectiveWindowSeconds: 300, TotpOffered: true})
		case strings.HasSuffix(r.URL.Path, "/auth/totp"):
			_ = jsonwire.Encode(w, apigen.TotpStatus{Confirmed: true})
		case strings.HasSuffix(r.URL.Path, "/auth/reauth/totp"):
			order = append(order, "reauth")
			rotated := "hses_rotated"
			_ = jsonwire.Encode(w, apigen.ReauthResult{SessionToken: &rotated, SessionId: "ses_rotated"})
		case strings.HasSuffix(r.URL.Path, "/values/export"):
			order = append(order, "export")
			exports++
			exportBearer = r.Header.Get("Authorization")
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			if err := json.Unmarshal(body, &exported); err != nil {
				t.Error(err)
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			fixture.ServeHTTP(w, r)
		default:
			fixture.ServeHTTP(w, r)
		}
	}))
	ios.StderrIsTerminal = func() bool { return true }
	answer := strings.NewReader("y\n")
	session, err := disclose.NewTerminalSession(&fakeTTY{r: readerFunc(func(p []byte) (int, error) {
		order = append(order, "confirm")
		return answer.Read(p)
	})})
	if err != nil {
		t.Fatal(err)
	}
	ios.TerminalSession = session
	ios.ReadPassword = func(string) (string, error) {
		order = append(order, "code")
		return "123456", nil
	}
	var injected []string
	ios.Exec = func(_ string, _, env []string) error { injected = env; return nil }
	if code := cli.Run(t.Context(), ios, runArgs()); code != cli.ExitOK {
		t.Fatalf("exit %d, want ok; stderr=%s", code, stderr.String())
	}
	if got := strings.Join(slices.Compact(order), ","); got != "confirm,code,reauth,export" {
		t.Errorf("order = %s, want confirm,code,reauth,export", got)
	}
	if exports != 1 {
		t.Errorf("exports = %d, want 1 pinned to the confirmed revision", exports)
	}
	if exportBearer != "Bearer hses_rotated" {
		t.Errorf("export presented %q, want the rotated session", exportBearer)
	}
	if exported.Revision == nil || *exported.Revision != 7 {
		t.Errorf("export pinned revision %v, want the confirmed 7", exported.Revision)
	}
	if !slices.Contains(injected, "LOG_LEVEL=info") {
		t.Error("child environment lacks LOG_LEVEL=info")
	}
}

func TestRunHumanSessionRefusesLoaderControlBeforeConfirming(t *testing.T) {
	exports := 0
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/values/export") {
			exports++
		}
		_ = jsonwire.Encode(w, apigen.RevisionDetail{
			Revision: 7, Keys: []apigen.SnapshotKey{{Name: "LD_PRELOAD", Classification: "config", KeyId: "key_1"}},
		})
	}))
	ios.StderrIsTerminal = func() bool { return true }
	session, err := disclose.NewTerminalSession(&fakeTTY{r: readerFunc(func([]byte) (int, error) {
		t.Error("prompted for a run that is refused anyway")
		return 0, io.EOF
	})})
	if err != nil {
		t.Fatal(err)
	}
	ios.TerminalSession = session
	if code := cli.Run(t.Context(), ios, runArgs()); code != cli.ExitRefused || exports != 0 {
		t.Fatalf("exit %d exports=%d, want refused without an export; stderr=%s", code, exports, stderr.String())
	}
	if !strings.Contains(stderr.String(), "LD_PRELOAD") {
		t.Errorf("refusal does not name the key: %s", stderr.String())
	}
}

func TestRunHumanSessionRefusesAnUndeliveredConfirmedKey(t *testing.T) {
	fixture := runHumanServer(t, true)
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/values/export") {
			fixture.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = jsonwire.Encode(w, apigen.ExportedValues{
			Revision: 7, Count: 1, Items: []apigen.ExportedValue{{Name: "API_TOKEN", Classification: "secret"}},
		})
	}))
	ios.StderrIsTerminal = func() bool { return true }
	ios.TerminalSession, _ = terminalSession(t, "y\n")
	execed := false
	ios.Exec = func(string, []string, []string) error { execed = true; return nil }
	if code := cli.Run(t.Context(), ios, runArgs()); code != cli.ExitRefused || execed {
		t.Fatalf("exit %d exec=%v, want refused without exec; stderr=%s", code, execed, stderr.String())
	}
	if !strings.Contains(stderr.String(), "did not deliver confirmed key(s) API_TOKEN") {
		t.Errorf("refusal does not name the secret: %s", stderr.String())
	}
}

// A key the caller holds a personal value or notice for is refused before the
// confirmation and before any export, with or without --config-only.
func TestRunHumanSessionRefusesAHeldPersonalKey(t *testing.T) {
	for _, extra := range [][]string{nil, {"--config-only"}} {
		fixture := runHumanServer(t, true)
		exports, listed := 0, 0
		ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/environments/env_70/personal-values"):
				listed++
				w.Header().Set("Content-Type", "application/json")
				_ = jsonwire.Encode(w, apigen.PersonalValueList{Items: []apigen.PersonalValueEntry{{Name: "LOG_LEVEL", Set: true}}})
			case strings.HasSuffix(r.URL.Path, "/values/export"):
				exports++
				fixture.ServeHTTP(w, r)
			default:
				fixture.ServeHTTP(w, r)
			}
		}))
		ios.StderrIsTerminal = func() bool { return true }
		// A decline: a refusal placed after the confirmation reports "declined" instead.
		ios.TerminalSession, _ = terminalSession(t, "n\n")
		execed := false
		ios.Exec = func(string, []string, []string) error { execed = true; return nil }
		code := cli.Run(t.Context(), ios, runArgs(extra...))
		if code != cli.ExitRefused || execed || exports != 0 || listed != 1 || !strings.Contains(stderr.String(), "refusing key(s) LOG_LEVEL") {
			t.Fatalf("%v: exit=%d exec=%v exports=%d listed=%d stderr=%s", extra, code, execed, exports, listed, stderr.String())
		}
	}
}

// A 0-window environment: the full projection binds the browser ceremony to the
// server's disclosure unit, and --config-only opens no ceremony it could not spend.
// Above 0 the ceremony opens a sliding window, which config-only can use.
func TestRunHumanSessionBrowserCeremonyUnit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		extra  []string
		window apigen.RevealWindow
		want   string
	}{
		{"full projection", nil, apigen.RevealWindow{CanReveal: true}, `["key_2","key_3"]`},
		{"stale single-decision window", nil, apigen.RevealWindow{CanReveal: true, Live: true, SingleDecision: true}, `["key_2","key_3"]`},
		{"config only at zero", []string{"--config-only"}, apigen.RevealWindow{CanReveal: true}, ""},
		{"config only above zero", []string{"--config-only"}, apigen.RevealWindow{CanReveal: true, EffectiveWindowSeconds: 300}, `["key_1"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var handoff map[string]any
			exports := 0
			fixture := runHumanServer(t, false)
			ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/reveal-window"):
					w.Header().Set("Content-Type", "application/json")
					_ = jsonwire.Encode(w, tc.window)
				case strings.HasSuffix(r.URL.Path, "/auth/totp"):
					w.Header().Set("Content-Type", "application/json")
					_ = jsonwire.Encode(w, apigen.TotpStatus{})
				case strings.Contains(r.URL.Path, "/cli-reauth"):
					if err := json.UnmarshalRead(r.Body, &handoff); err != nil {
						t.Error(err)
					}
					http.Error(w, "stop here", http.StatusConflict)
				case strings.HasSuffix(r.URL.Path, "/values/export"):
					exports++
					fixture.ServeHTTP(w, r)
				default:
					fixture.ServeHTTP(w, r)
				}
			}))
			ios.StderrIsTerminal = func() bool { return true }
			ios.TerminalSession, _ = terminalSession(t, "y\n")
			ios.OpenURL = func(string) error { return nil }
			if code := cli.Run(t.Context(), ios, runArgs(tc.extra...)); code == cli.ExitOK {
				t.Fatalf("exit ok, want a refusal; stderr=%s", stderr.String())
			}
			if exports != 0 {
				t.Errorf("exported %d times without a window", exports)
			}
			if tc.want == "" {
				if handoff != nil {
					t.Errorf("opened a browser ceremony it could not spend: %v", handoff)
				}
				if !strings.Contains(stderr.String(), "no disclosure to spend it on") {
					t.Errorf("refusal does not explain itself: %s", stderr.String())
				}
				return
			}
			got, err := json.Marshal(handoff["key_ids"], jsonwire.Stable)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("ceremony unit = %s (request %v), want %s", got, handoff, tc.want)
			}
		})
	}
}

// Only a refusal the server answered, with a publish behind it, is reported as
// the race. Everything else keeps its own error and exit code.
func TestRunHumanSessionExportFailureReporting(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		latest   int64
		wantExit int
		want     string
	}{
		{"refused after a publish", http.StatusNotFound, 8, cli.ExitRefused, "published to during this run"},
		{"refused with no publish", http.StatusNotFound, 7, cli.ExitNotFound, "server returned 404"},
		{"rate limited after a publish", http.StatusTooManyRequests, 8, cli.ExitRateLimited, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exported := false
			fixture := runHumanServer(t, true)
			ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/values/export"):
					exported = true
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Retry-After", "60")
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(`{"error":{"code":"refused","message":"refused"}}`))
				case exported && strings.HasSuffix(r.URL.Path, "/revisions/latest"):
					w.Header().Set("Content-Type", "application/json")
					_ = jsonwire.Encode(w, apigen.RevisionDetail{Revision: tc.latest})
				default:
					fixture.ServeHTTP(w, r)
				}
			}))
			ios.StderrIsTerminal = func() bool { return true }
			ios.TerminalSession, _ = terminalSession(t, "y\n")
			code := cli.Run(t.Context(), ios, runArgs())
			if code != tc.wantExit || !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("exit %d, want %d naming %q; stderr=%s", code, tc.wantExit, tc.want, stderr.String())
			}
			if tc.status != http.StatusNotFound && strings.Contains(stderr.String(), "published to during this run") {
				t.Fatalf("relabelled a failure that proves nothing: %s", stderr.String())
			}
		})
	}
}

func TestRunHumanSessionBindsComposeInstanceBeforeNetwork(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		for _, match := range []bool{false, true} {
			// The matched case runs again with an equivalent spelling (leading
			// zero in the port) of the stored origin.
			for _, respelled := range []bool{false, true} {
				if respelled && !match {
					continue
				}
				name := "sole-trusted"
				if explicit {
					name = "explicit"
				}
				if match {
					name += "/matched-normalized"
				} else {
					name += "/mismatched"
				}
				if respelled {
					name += "/respelled"
				}
				t.Run(name, func(t *testing.T) {
					requests := 0
					fixture := runHumanServer(t, true)
					ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests++
						fixture.ServeHTTP(w, r)
					}))
					state, err := cli.NewState(ios.Env)
					if err != nil {
						t.Fatal(err)
					}
					entry, err := state.Trust().Lookup("local")
					if err != nil {
						t.Fatal(err)
					}
					origin := "https://repository-instance.example"
					if match {
						origin = entry.Origin + "/"
					}
					if respelled {
						origin = strings.Replace(entry.Origin, "127.0.0.1:", "127.0.0.1:0", 1)
					}
					config := "version: 1\ninstance: " + origin + "\norg: org_70\nproject: prj_70\nenvironment: env_70\nslug: acme\ntargets:\n  api:\n    keys: [LOG_LEVEL]\n    services: [api]\n"
					if err := os.WriteFile(filepath.Join(ios.Workdir, "hikyo-compose.yaml"), []byte(config), 0o600); err != nil {
						t.Fatal(err)
					}
					ios.StderrIsTerminal = func() bool { return true }
					ios.TerminalSession, _ = terminalSession(t, "y\n")
					execed := false
					ios.Exec = func(string, []string, []string) error { execed = true; return nil }
					args := []string{"run", "--use-human-session", "--config-only"}
					if explicit {
						args = append(args, "--instance", "local")
					}
					args = append(args, "--", "true")
					code := cli.Run(t.Context(), ios, args)
					if match {
						if code != cli.ExitOK || !execed || requests == 0 {
							t.Fatalf("matched instance: exit=%d exec=%v requests=%d stderr=%s", code, execed, requests, stderr)
						}
					} else if code != cli.ExitUsage || execed || requests != 0 || !strings.Contains(stderr.String(), "refusing rather than picking one") {
						t.Fatalf("mismatched instance: exit=%d exec=%v requests=%d stderr=%s", code, execed, requests, stderr)
					}
				})
			}
		}
	}
}
