package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
)

// Unlike definitionsTestIO, this fixture counts metadata reads too. Malformed
// arguments must be refused before contacting the server at all.
func dekRefusalIO(t *testing.T) (cli.IO, *bytes.Buffer, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	ios, _, stderr := testIO(t, nil)
	state, err := cli.NewState(ios.Env)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Trust().Put(cli.TrustEntry{Name: "local", Origin: server.URL, SPKIPin: cli.SPKIFingerprint(server.Certificate())}); err != nil {
		t.Fatal(err)
	}
	if err := state.PutSession(cli.SessionArtifact{Instance: "local", Origin: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	return ios, stderr, &calls
}

type dekRequests struct {
	mu       sync.Mutex
	requests []string
}

func (r *dekRequests) record(request string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, request)
}

func (r *dekRequests) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.requests
	r.requests = nil
	return out
}

// dekServer answers rotate-dek and both reencrypt routes, echoing the
// addressed scope, and records every request's method, path and body.
func dekServer(t *testing.T) (http.Handler, *dekRequests) {
	t.Helper()
	var seen dekRequests
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing human session bearer")
		}
		seen.record(r.Method + " " + r.URL.Path + " " + string(body))
		w.Header().Set("Content-Type", "application/json")
		org, project := "org_70", "prj_70"
		switch {
		case r.URL.Path == "/api/v1/instance/rotate-dek":
			var req apigen.RotateDEKRequest
			if err := json.Unmarshal(body, &req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			out := apigen.DEKRotation{Scope: apigen.DEKRotationScope(req.Scope), KeyVersion: 2, Org: req.Org, Project: req.Project}
			_ = json.NewEncoder(w).Encode(out)
		case r.URL.Path == "/api/v1/instance/reencrypt":
			_ = json.NewEncoder(w).Encode(apigen.ReencryptResult{Scope: apigen.ReencryptResultScopeInstance, RowsMoved: 3})
		case r.URL.Path == "/api/v1/orgs/org_70/projects/prj_70/reencrypt":
			_ = json.NewEncoder(w).Encode(apigen.ReencryptResult{Scope: apigen.ReencryptResultScopeProject, RowsMoved: 5, Org: &org, Project: &project})
		default:
			http.NotFound(w, r)
		}
	}), &seen
}

// Both verbs dispatch at both scopes. --instance stays the shared instance
// reference; --instance-scope selects the instance DEK.
func TestDEKRotationDispatch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		request string
		body    string
		row     string
	}{
		{"rotate-dek instance", []string{"rotate-dek", "--instance-scope", "--yes"},
			"POST /api/v1/instance/rotate-dek ", `{"scope":"instance"}`, "instance"},
		{"rotate-dek project", []string{"rotate-dek", "--org", "org_70", "--project", "prj_70", "--yes"},
			"POST /api/v1/instance/rotate-dek ", `{"org":"org_70","project":"prj_70","scope":"project"}`, "org_70"},
		{"reencrypt instance", []string{"reencrypt", "--instance-scope"},
			"POST /api/v1/instance/reencrypt ", "", "3"},
		{"reencrypt project", []string{"reencrypt", "--org", "org_70", "--project", "prj_70"},
			"POST /api/v1/orgs/org_70/projects/prj_70/reencrypt ", "", "org_70"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, seen := dekServer(t)
			ios, stdout, stderr := definitionsTestIO(t, handler)
			// Project flags override inherited tenant dimensions; explicit instance
			// scope ignores them. Neither changes the selected instance.
			getenv := ios.Env.Getenv
			ios.Env.Getenv = func(key string) string {
				switch key {
				case "HIKYO_ORG":
					return "org_other"
				case "HIKYO_PROJECT":
					return "prj_other"
				case "HIKYO_ENV":
					return "env_other"
				default:
					return getenv(key)
				}
			}
			if code := cli.Run(t.Context(), ios, append(tc.args, "--instance", "local", "-o", "json")); code != cli.ExitOK {
				t.Fatalf("exit %d; stderr=%s", code, stderr)
			}
			if requests := seen.take(); len(requests) != 1 || requests[0] != tc.request+tc.body {
				t.Fatalf("requests = %v", requests)
			}
			if !strings.Contains(stdout.String(), tc.row) {
				t.Fatalf("stdout = %q", stdout)
			}
			golden(t, strings.ReplaceAll(tc.name, " ", "-")+".json", stdout.Bytes())
		})
	}
}

// Conflicting and incomplete scopes are usage errors that reach no server.
func TestDEKRotationScopeRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"rotate-dek instance scope with org", []string{"rotate-dek", "--instance-scope", "--org", "org_70", "--yes"}},
		{"rotate-dek instance scope with project", []string{"rotate-dek", "--instance-scope", "--project", "prj_70", "--yes"}},
		{"rotate-dek instance scope with env", []string{"rotate-dek", "--instance-scope", "--env", "env_70", "--yes"}},
		{"rotate-dek org without project", []string{"rotate-dek", "--org", "org_70", "--yes"}},
		{"rotate-dek no scope", []string{"rotate-dek", "--yes"}},
		{"reencrypt instance scope with org", []string{"reencrypt", "--instance-scope", "--org", "org_70", "--project", "prj_70"}},
		{"reencrypt project without org", []string{"reencrypt", "--project", "prj_70"}},
		{"reencrypt no scope", []string{"reencrypt"}},
		{"rotate-dek project without org", []string{"rotate-dek", "--project", "prj_70", "--yes"}},
		{"reencrypt org without project", []string{"reencrypt", "--org", "org_70"}},
		{"reencrypt instance scope with project", []string{"reencrypt", "--instance-scope", "--project", "prj_70"}},
		{"reencrypt instance scope with env", []string{"reencrypt", "--instance-scope", "--env", "env_70"}},
		{"rotate-dek positional", []string{"rotate-dek", "--instance-scope", "--yes", "stray"}},
		{"reencrypt positional", []string{"reencrypt", "--instance-scope", "stray"}},
		{"rotate-dek unknown flag", []string{"rotate-dek", "--unknown"}},
		{"reencrypt unknown flag", []string{"reencrypt", "--unknown"}},
		{"rotate-dek conflict before context lookup", []string{"rotate-dek", "--instance-scope", "--org", "org_70", "--context", "missing", "--yes"}},
		{"reencrypt conflict before context lookup", []string{"reencrypt", "--instance-scope", "--project", "prj_70", "--context", "missing"}},
		{"rotate-dek environment is not a DEK scope", []string{"rotate-dek", "--org", "org_70", "--project", "prj_70", "--env", "env_70", "--yes"}},
		{"reencrypt environment is not a DEK scope", []string{"reencrypt", "--org", "org_70", "--project", "prj_70", "--env", "env_70"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ios, stderr, calls := dekRefusalIO(t)
			if code := cli.Run(t.Context(), ios, append(tc.args, "--instance", "local")); code != cli.ExitUsage {
				t.Fatalf("exit %d, want usage; stderr=%s", code, stderr)
			}
			if calls.Load() != 0 {
				t.Fatalf("a refused rotation reached the server %d times", calls.Load())
			}
		})
	}
}

// Usage diagnostics must win over later failures in local target or auth state.
// Exercise both callers so extraction cannot change which error users see.
func TestDEKRotationPreparationErrorPrecedence(t *testing.T) {
	for _, verb := range []string{"rotate-dek", "reencrypt"} {
		for _, tc := range []struct {
			name       string
			args       []string
			invalidPin bool
			message    string
		}{
			{"format before positionals", []string{"-o", "yaml", "--env", "env_70", "stray"}, false,
				`unknown output format "yaml": use table or json`},
			{"positionals before selectors", []string{"--env", "env_70", "stray"}, false,
				"usage: hikyo " + verb + " takes no positional arguments, got: stray"},
			{"environment before context", []string{"--env=", "--context", "missing"}, false,
				"hikyo " + verb + ": --env cannot select a DEK; use --org/--project for the entire project or --instance-scope"},
			{"empty selector before context", []string{"--instance=", "--context", "missing"}, false,
				"hikyo " + verb + ": --instance must not be empty; omit it to use inherited selection"},
			{"scope conflict before pin", []string{"--instance-scope", "--org", "org_70"}, true,
				"hikyo " + verb + ": --instance-scope and --org/--project name two different scopes; choose one"},
			{"missing org before authentication", []string{"--instance", "untrusted", "--project", "prj_70"}, false,
				"no org resolved."},
			{"missing project before authentication", []string{"--instance", "untrusted", "--org", "org_70"}, false,
				"no project resolved."},
		} {
			t.Run(verb+"/"+tc.name, func(t *testing.T) {
				ios, stdout, stderr := testIO(t, nil)
				if tc.invalidPin {
					if err := os.WriteFile(filepath.Join(ios.Workdir, cli.PinFileName), []byte("{"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if code := cli.Run(t.Context(), ios, append([]string{verb}, tc.args...)); code != cli.ExitUsage {
					t.Fatalf("exit %d, want usage; stderr=%s", code, stderr)
				}
				if !strings.Contains(stderr.String(), tc.message) {
					t.Fatalf("stderr=%q, want %q", stderr, tc.message)
				}
				if stdout.Len() != 0 || strings.Contains(stderr.String(), "target:") || strings.Contains(stderr.String(), "rotate-dek appends") {
					t.Fatalf("failed preparation produced output: stdout=%q stderr=%q", stdout, stderr)
				}
			})
		}
	}
}

// An empty explicit selector cannot silently inherit a target or broader scope.
func TestDEKRotationEmptyExplicitSelectors(t *testing.T) {
	for _, verb := range []string{"rotate-dek", "reencrypt"} {
		for _, scope := range []string{"instance", "project"} {
			for _, selector := range []string{"instance", "context", "org", "project", "env"} {
				for _, form := range []string{"separate", "equals"} {
					t.Run(verb+"/"+scope+"/"+selector+"/"+form, func(t *testing.T) {
						ios, stderr, calls := dekRefusalIO(t)
						getenv := ios.Env.Getenv
						ios.Env.Getenv = func(key string) string {
							switch key {
							case "HIKYO_INSTANCE":
								return "local"
							case "HIKYO_ORG":
								return "org_70"
							case "HIKYO_PROJECT":
								return "prj_70"
							case "HIKYO_ENV":
								return "env_70"
							default:
								return getenv(key)
							}
						}
						args := []string{verb}
						if verb == "rotate-dek" {
							args = append(args, "--yes")
						}
						if scope == "instance" {
							args = append(args, "--instance-scope")
						}
						if form == "equals" {
							args = append(args, "--"+selector+"=")
						} else {
							args = append(args, "--"+selector, "")
						}
						if code := cli.Run(t.Context(), ios, args); code != cli.ExitUsage {
							t.Errorf("exit %d, want usage; stderr=%s", code, stderr)
						}
						if calls.Load() != 0 {
							t.Fatalf("empty explicit selector fell back to ambient scope: %d HTTP requests", calls.Load())
						}
					})
				}
			}
		}
	}
}

// An explicit instance must win over another trusted instance selected by the
// environment. A typo must fail closed rather than falling back to either one.
func TestDEKRotationInstanceTarget(t *testing.T) {
	for _, verb := range []string{"rotate-dek", "reencrypt"} {
		for _, instance := range []string{"local", "typo"} {
			t.Run(verb+"/"+instance, func(t *testing.T) {
				handler, seen := dekServer(t)
				ios, _, stderr := definitionsTestIO(t, handler)
				var otherCalls atomic.Int32
				other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					otherCalls.Add(1)
					http.Error(w, "wrong instance", http.StatusForbidden)
				}))
				t.Cleanup(other.Close)
				state, err := cli.NewState(ios.Env)
				if err != nil {
					t.Fatal(err)
				}
				if err := state.Trust().Put(cli.TrustEntry{Name: "other", Origin: other.URL, SPKIPin: cli.SPKIFingerprint(other.Certificate())}); err != nil {
					t.Fatal(err)
				}
				getenv := ios.Env.Getenv
				ios.Env.Getenv = func(key string) string {
					if key == "HIKYO_INSTANCE" {
						return "other"
					}
					return getenv(key)
				}
				for _, selectors := range [][]string{{"--instance-scope"}, {"--org", "org_70", "--project", "prj_70"}} {
					args := append([]string{verb, "--instance", instance}, selectors...)
					if verb == "rotate-dek" {
						args = append(args, "--yes")
					}
					want := cli.ExitOK
					if instance == "typo" {
						want = cli.ExitRefused
					}
					if code := cli.Run(t.Context(), ios, args); code != want {
						t.Fatalf("exit %d, want %d: %s", code, want, stderr)
					}
					if otherCalls.Load() != 0 {
						t.Fatalf("contacted wrong instance %d times", otherCalls.Load())
					}
					wantCalls := 0
					if instance == "local" {
						wantCalls = 1
					}
					if requests := seen.take(); len(requests) != wantCalls {
						t.Fatalf("requests = %v", requests)
					}
				}
			})
		}
	}
}

func TestDEKRotationAuthenticationAndConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		status int
		want   int
		calls  int
	}{
		{"confirmation", []string{"rotate-dek", "--instance-scope"}, 200, cli.ExitRefused, 0},
		{"rotation reauthentication", []string{"rotate-dek", "--instance-scope", "--yes"}, 401, cli.ExitAuth, 1},
		{"reencryption reauthentication", []string{"reencrypt", "--instance-scope"}, 401, cli.ExitAuth, 1},
		{"rotation capability", []string{"rotate-dek", "--instance-scope", "--yes"}, 403, cli.ExitRefused, 1},
		{"reencryption capability", []string{"reencrypt", "--instance-scope"}, 403, cli.ExitRefused, 1},
		{"incomplete walk", []string{"reencrypt", "--instance-scope"}, 409, cli.ExitRefused, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			ios, stdout, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("unexpected request: %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]map[string]string{"error": {"code": "refused", "message": "fixture refusal"}})
			}))
			if code := cli.Run(t.Context(), ios, append(tc.args, "--instance", "local")); code != tc.want {
				t.Fatalf("exit %d, want %d: %s", code, tc.want, stderr)
			}
			if calls.Load() != int32(tc.calls) || stdout.Len() != 0 {
				t.Fatalf("calls=%d stdout=%q", calls.Load(), stdout)
			}
		})
	}
}

func TestDEKRotationCredentialRefusals(t *testing.T) {
	for _, verb := range []string{"rotate-dek", "reencrypt"} {
		for _, refusal := range []string{"logged out", "machine token", "origin changed"} {
			t.Run(verb+"/"+refusal, func(t *testing.T) {
				handler, seen := dekServer(t)
				ios, _, stderr := definitionsTestIO(t, handler)
				state, err := cli.NewState(ios.Env)
				if err != nil {
					t.Fatal(err)
				}
				want := cli.ExitRefused
				switch refusal {
				case "logged out", "machine token":
					if err := state.DeleteSession("local"); err != nil {
						t.Fatal(err)
					}
					want = cli.ExitAuth
					if refusal == "machine token" {
						getenv := ios.Env.Getenv
						ios.Env.Getenv = func(key string) string {
							if key == "HIKYO_TOKEN" {
								return "synthetic-machine-token"
							}
							return getenv(key)
						}
						want = cli.ExitRefused
					}
				case "origin changed":
					if err := state.PutSession(cli.SessionArtifact{Instance: "local", Origin: "https://different.example", Token: "test-token"}); err != nil {
						t.Fatal(err)
					}
				}
				args := []string{verb, "--instance", "local", "--instance-scope"}
				if verb == "rotate-dek" {
					args = append(args, "--yes")
				}
				if code := cli.Run(t.Context(), ios, args); code != want {
					t.Fatalf("exit %d, want %d: %s", code, want, stderr)
				}
				if requests := seen.take(); len(requests) != 0 {
					t.Fatalf("refused credential reached server: %v", requests)
				}
			})
		}
	}
}
