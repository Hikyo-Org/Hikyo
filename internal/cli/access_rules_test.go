package cli

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func TestRuleRequestBindsItemsToProjects(t *testing.T) {
	one := []string{"prj_a"}
	two := []string{"prj_a", "prj_b"}

	req, err := ruleRequest("usr_x", "edit", one,
		ruleAxisFlags{except: stringList{"env_prod"}}, ruleAxisFlags{except: stringList{"db"}}, ruleAxisFlags{})
	if err != nil {
		t.Fatal(err)
	}
	w := req.Where
	if w.Environments.Mode != apigen.RuleAxisModeAll || len(w.Environments.Items) != 1 || w.Environments.Items[0].Project != "prj_a" {
		t.Fatalf("environments = %+v", w.Environments)
	}
	if w.Keys.Mode != apigen.RuleAxisModeAll || len(w.Keys.Items) != 1 || *w.Keys.Items[0].Folder != "db" {
		t.Fatalf("keys = %+v", w.Keys)
	}

	// An unqualified folder covers every project of the rule; a qualified one
	// covers its own. An unqualified environment is ambiguous across projects.
	req, err = ruleRequest("usr_x", "reveal", two,
		ruleAxisFlags{only: stringList{"prj_b:env_dev"}}, ruleAxisFlags{only: stringList{"db", "prj_b:stripe"}}, ruleAxisFlags{only: stringList{"prj_a:key_1"}})
	if err != nil {
		t.Fatal(err)
	}
	w = req.Where
	if w.Environments.Mode != apigen.RuleAxisModeOnly || w.Environments.Items[0] != (apigen.RuleEnvironmentItem{Project: "prj_b", Environment: "env_dev"}) {
		t.Fatalf("environments = %+v", w.Environments)
	}
	var got []string
	for _, it := range w.Keys.Items {
		if it.Folder != nil {
			got = append(got, it.Project+"/folder:"+*it.Folder)
		} else {
			got = append(got, it.Project+"/key:"+*it.Key)
		}
	}
	if w.Keys.Mode != apigen.RuleAxisModeOnly || strings.Join(got, " ") != "prj_a/folder:db prj_b/folder:db prj_b/folder:stripe prj_a/key:key_1" {
		t.Fatalf("keys = %v", got)
	}
	if _, err := ruleRequest("usr_x", "edit", two, ruleAxisFlags{except: stringList{"env_prod"}}, ruleAxisFlags{}, ruleAxisFlags{}); err == nil {
		t.Fatal("an unqualified environment on a two-project rule was accepted")
	}
	if _, err := ruleRequest("usr_x", "edit", one, ruleAxisFlags{}, ruleAxisFlags{}, ruleAxisFlags{only: stringList{"prj_z:key_1"}}); err == nil {
		t.Fatal("a key qualified with a project outside the rule was accepted")
	}
	if _, err := ruleRequest("usr_x", "edit", one, ruleAxisFlags{}, ruleAxisFlags{except: stringList{"prj_typo:secrets"}}, ruleAxisFlags{}); err == nil {
		t.Fatal("an except folder qualified with a project outside the rule was accepted as a literal folder")
	}
}

func TestAccessRuleVerbs(t *testing.T) {
	var requests []string
	var posted apigen.CreateRuleRequest
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == api.PathPrefix+"/meta" {
			_ = jsonwire.Encode(w, apigen.Meta{ServerVersion: "fixture-current", ApiRevision: api.Revision})
			return
		}
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodPost:
			if err := json.UnmarshalRead(r.Body, &posted); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusCreated)
			_ = jsonwire.Encode(w, apigen.Rule{Id: "rul_1", PrincipalId: posted.Principal, Capability: posted.Capability, Where: posted.Where})
		case http.MethodGet:
			if r.URL.Path == api.PathPrefix+"/orgs/org_one/projects" {
				_ = jsonwire.Encode(w, apigen.ProjectList{Items: []apigen.Project{{Id: "prj_one"}, {Id: "prj_two"}}, Count: 2})
				return
			}
			_ = jsonwire.Encode(w, apigen.RuleList{Items: []apigen.Rule{}})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	st, stateDir := machineState(t, srv.URL, SPKIFingerprint(srv.Certificate()))
	if err := st.PutSession(SessionArtifact{Instance: "local", Origin: srv.URL, Token: "fixture-human", SessionID: "ses_fixture", Principal: "usr_fixture", ExpiresAt: "2030-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string) {
		ios, stdout, stderr := composeIO(stateDir, t.TempDir(), "", nil)
		code := Run(t.Context(), ios, append(args, "--instance", "local", "--org", "org_one"))
		return code, stdout.String() + stderr.String()
	}

	if code, out := run("access", "rule", "add", "--project", "prj_one", "--principal", "usr_carol", "--capability", "reveal",
		"--only-env", "env_dev", "--only-folder", "db"); code != ExitOK {
		t.Fatalf("add = %d %s", code, out)
	}
	if posted.Where.Projects[0] != "prj_one" || posted.Where.Environments.Mode != apigen.RuleAxisModeOnly || posted.Where.Keys.Mode != apigen.RuleAxisModeOnly {
		t.Fatalf("posted %+v", posted)
	}
	if code, out := run("access", "rule", "list", "--project", "prj_one"); code != ExitOK {
		t.Fatalf("list = %d %s", code, out)
	}
	if code, out := run("access", "rule", "remove", "rul_1"); code != ExitOK {
		t.Fatalf("remove = %d %s", code, out)
	}
	want := []string{
		"POST " + api.PathPrefix + "/orgs/org_one/rules",
		"GET " + api.PathPrefix + "/orgs/org_one/projects/prj_one/rules",
		"DELETE " + api.PathPrefix + "/orgs/org_one/rules/rul_1",
	}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(requests, "\n"), strings.Join(want, "\n"))
	}

	// --all-projects lists the org's projects first and sends exactly them
	// with project_mode all, each folder repeated under every project (R2).
	requests = nil
	if code, out := run("access", "rule", "add", "--principal", "usr_carol", "--capability", "edit",
		"--all-projects", "--except-folder", "secret"); code != ExitOK {
		t.Fatalf("add --all-projects = %d %s", code, out)
	}
	pw := posted.Where
	if pw.ProjectMode == nil || *pw.ProjectMode != apigen.RuleProjectModeAll || strings.Join(pw.Projects, ",") != "prj_one,prj_two" ||
		pw.Environments.Mode != apigen.RuleAxisModeAll || len(pw.Environments.Items) != 0 || pw.Keys.Mode != apigen.RuleAxisModeAll ||
		len(pw.Keys.Items) != 2 || pw.Keys.Items[0].Project != "prj_one" || pw.Keys.Items[1].Project != "prj_two" || *pw.Keys.Items[1].Folder != "secret" {
		t.Fatalf("posted %+v", pw)
	}
	want = []string{"GET " + api.PathPrefix + "/orgs/org_one/projects", "POST " + api.PathPrefix + "/orgs/org_one/rules"}
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("all-projects requests %v, want %v", requests, want)
	}

	// Syntax is refused before any request.
	requests = nil
	for _, args := range [][]string{
		{"access", "rule", "add", "--project", "prj_one", "--principal", "usr_carol"},
		{"access", "rule", "add", "--project", "prj_one", "--principal", "usr_carol", "--capability", "edit", "--only-env", "a", "--except-env", "b"},
		{"access", "rule", "add", "--project", "prj_one", "--principal", "usr_carol", "--capability", "edit", "--only-folder", "a", "--except-key", "b"},
		{"access", "rule", "remove"},
		{"access", "rule", "add", "--principal", "usr_carol", "--capability", "edit", "--all-projects", "--in-project", "prj_one"},
		{"access", "rule", "add", "--principal", "usr_carol", "--capability", "edit", "--all-projects", "--except-env", "env_prod"},
		{"access", "rule", "add", "--principal", "usr_carol", "--capability", "edit", "--all-projects", "--only-key", "key_1"},
		{"access", "rule", "add", "--principal", "usr_carol", "--capability", "edit", "--all-projects", "--except-folder", "prj_one:db"},
	} {
		if code, out := run(args...); code != ExitUsage {
			t.Errorf("%v = %d %s, want usage", args, code, out)
		}
	}
	if len(requests) != 0 {
		t.Fatalf("a usage error reached the server: %v", requests)
	}
}

// Rules, like grants, are administered from a stored human session only: a
// machine credential is refused before any request, reveal or not.
func TestAccessRuleVerbsNeedHumanSession(t *testing.T) {
	writes := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == api.PathPrefix+"/meta" {
			_ = jsonwire.Encode(w, apigen.Meta{ServerVersion: "fixture-current", ApiRevision: api.Revision})
			return
		}
		if r.Method != http.MethodGet {
			writes++
		}
		w.WriteHeader(http.StatusCreated)
		_ = jsonwire.Encode(w, apigen.Rule{})
	}))
	defer srv.Close()
	_, stateDir := machineState(t, srv.URL, SPKIFingerprint(srv.Certificate()))
	ios, _, stderr := composeIO(stateDir, t.TempDir(), "hik_1_wl_secret", nil)
	code := Run(t.Context(), ios, []string{"access", "rule", "add", "--instance", "local", "--org", "org_one", "--project", "prj_one",
		"--principal", "usr_carol", "--capability", "reveal"})
	if code != ExitRefused || writes != 0 || !strings.Contains(stderr.String(), "requires a human session") {
		t.Fatalf("exit=%d writes=%d stderr=%s", code, writes, stderr)
	}
}

// A machine rule that widens plaintext reach is refused once per environment
// without a window that satisfies it. The verb opens the window the refusal
// names and sends the rule again, and stops when the server names the same
// environment twice. A live window there is bound to another operation, or the
// rule would not have been refused, so it does not skip the ceremony.
func TestAccessRuleAddReauthenticatesMachineWidening(t *testing.T) {
	slidingTOTP := apigen.RevealWindow{EffectiveWindowSeconds: 300, TotpOffered: true}
	for _, tc := range []struct {
		name     string
		refusals int
		wantCode int
		wantPost int
		noReveal bool
		totp     bool
		window   apigen.RevealWindow
		wantErr  string
	}{
		{"retries_after_the_window_opens", 1, ExitOK, 2, false, true, slidingTOTP, ""},
		{"stops_on_a_repeated_environment", 2, ExitRefused, 2, false, true, slidingTOTP, ""},
		// A history-only widening needs reveal-history, not reveal, so the
		// window's can_reveal being false must not stop the ceremony.
		{"retries_for_an_actor_without_current_reveal", 1, ExitOK, 2, true, true, slidingTOTP, ""},
		{"reauthenticates_over_a_live_window_bound_elsewhere", 1, ExitOK, 2, false, true,
			apigen.RevealWindow{Live: true, EffectiveWindowSeconds: 300, TotpOffered: true}, ""},
		// With no window and no inline TOTP, the CLI names the one step that can
		// open a window for this cause, never a reveal or a browser rule editor.
		{"asks_to_raise_a_zero_window", 1, ExitAuth, 1, false, false, apigen.RevealWindow{}, "--reauth-window-seconds 300`, then retry"},
		{"asks_to_enroll_without_an_authenticator", 1, ExitAuth, 1, false, false, slidingTOTP, "enroll an authenticator with `hikyo account factor enrol-totp`"},
		{"says_a_protected_environment_cannot_widen_yet", 1, ExitAuth, 1, false, false, apigen.RevealWindow{Protected: true}, "cannot be made yet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts, windows, reauths := 0, 0, 0
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == api.PathPrefix+"/meta":
					_ = jsonwire.Encode(w, apigen.Meta{ServerVersion: "fixture-current", ApiRevision: api.Revision})
				case r.URL.Path == api.PathPrefix+"/orgs/org_one/projects/prj_one/environments/env_dev/reveal-window":
					windows++
					window := tc.window
					window.CanReveal = !tc.noReveal
					_ = jsonwire.Encode(w, window)
				case r.URL.Path == api.PathPrefix+"/auth/totp":
					_ = jsonwire.Encode(w, apigen.TotpStatus{Confirmed: tc.totp})
				case r.URL.Path == api.PathPrefix+"/auth/reauth/totp":
					reauths++
					token := "hik_1_cli_rotated-rule"
					_ = jsonwire.Encode(w, apigen.ReauthResult{
						EnvironmentId: "env_dev", SessionId: "ses_rotated", SessionToken: &token,
						WindowExpires: apigen.Timestamp(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)),
					})
				case r.Method == http.MethodPost:
					posts++
					if posts <= tc.refusals {
						detail := "reauthenticate over the environments this operation makes reachable, then retry (env_dev)"
						var body apigen.Error
						body.Error.Code = apigen.ErrorCodeConflict
						body.Error.Message = "conflict"
						body.Error.Detail = &detail
						w.WriteHeader(http.StatusConflict)
						_ = jsonwire.Encode(w, body)
						return
					}
					w.WriteHeader(http.StatusCreated)
					_ = jsonwire.Encode(w, apigen.Rule{Id: "rul_1", PrincipalId: "mch_one", Capability: "read"})
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			st, stateDir := machineState(t, srv.URL, SPKIFingerprint(srv.Certificate()))
			if err := st.PutSession(SessionArtifact{Instance: "local", Origin: srv.URL, Token: "fixture-human", SessionID: "ses_fixture", Principal: "usr_fixture", ExpiresAt: "2030-01-01T00:00:00Z"}); err != nil {
				t.Fatal(err)
			}
			ios, stdout, stderr := composeIO(stateDir, t.TempDir(), "", nil)
			ios.ReadPassword = func(string) (string, error) { return "123456", nil }
			code := Run(t.Context(), ios, []string{"access", "rule", "add", "--instance", "local", "--org", "org_one", "--project", "prj_one",
				"--principal", "mch_one", "--capability", "read", "--only-env", "env_dev"})
			if code != tc.wantCode || posts != tc.wantPost || windows != 1 {
				t.Fatalf("exit=%d posts=%d windows=%d, want exit=%d posts=%d windows=1; out=%s%s",
					code, posts, windows, tc.wantCode, tc.wantPost, stdout, stderr)
			}
			if !strings.Contains(stderr.String(), tc.wantErr) {
				t.Fatalf("stderr = %q, want %q", stderr, tc.wantErr)
			}
			if wantReauths := map[bool]int{true: 1}[tc.totp]; reauths != wantReauths {
				t.Fatalf("reauths=%d, want %d: the inline ceremony must run once per refused environment", reauths, wantReauths)
			}
		})
	}
}

// serverConflict is the CLI error for a 409 the server answers, built by the
// client's own response conversion rather than by hand.
func serverConflict(t *testing.T, code apigen.ErrorCode, detail string) error {
	t.Helper()
	var body apigen.Error
	body.Error.Code = code
	body.Error.Message = "conflict"
	if detail != "" {
		body.Error.Detail = &detail
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return errorFromResponse(http.StatusConflict, http.Header{}, payload)
}

func TestWideningEnvReadsOnlyTheWideningRefusal(t *testing.T) {
	if got := wideningEnv(serverConflict(t, apigen.ErrorCodeConflict, fmt.Sprintf(service.WideningReauthDetail, "env_a"))); got != "env_a" {
		t.Fatalf("wideningEnv over the service wording = %q, want env_a", got)
	}
	for _, err := range []error{
		nil,
		failf(ExitRefused, "not permitted"),
		serverConflict(t, apigen.ErrorCodeConflict, ""),
		serverConflict(t, apigen.ErrorCodeConflict, "another conflict (env_a)"),
		serverConflict(t, apigen.ErrorCodeUnauthenticated, fmt.Sprintf(service.WideningReauthDetail, "env_a")),
		// The text alone, with no structured detail, is not the refusal.
		failf(ExitRefused, "conflict (%s)", fmt.Sprintf(service.WideningReauthDetail, "env_a")),
	} {
		if got := wideningEnv(err); got != "" {
			t.Errorf("wideningEnv(%v) = %q, want empty", err, got)
		}
	}
}

// An all-projects rule reads back expanded over the org's current projects
// (#1446); the table shows it once, as all projects, with unqualified folders.
func TestRuleTableShowsAllProjectsOnce(t *testing.T) {
	all := apigen.RuleProjectModeAll
	secret := "secret"
	got := ruleTable(apigen.RuleList{Count: 1, Items: []apigen.Rule{{Id: "rul_1", PrincipalId: "usr_1", Capability: apigen.RuleCapabilityEdit,
		Where: apigen.RuleWhere{ProjectMode: &all, Projects: []apigen.ID{"prj_a", "prj_b"},
			Environments: apigen.RuleEnvironmentAxis{Mode: apigen.RuleAxisModeAll, Items: []apigen.RuleEnvironmentItem{}},
			Keys: apigen.RuleKeyAxis{Mode: apigen.RuleAxisModeAll, Items: []apigen.RuleKeyItem{
				{Project: "prj_a", Folder: &secret}, {Project: "prj_b", Folder: &secret},
			}}}}}})
	row := got.Rows[0]
	if row[3] != "all projects (now prj_a, prj_b)" || strings.Count(row[5], "folder:secret") != 1 || strings.Contains(row[5], "prj_") {
		t.Fatalf("row %q, want all projects once with one unqualified folder", row)
	}
}
