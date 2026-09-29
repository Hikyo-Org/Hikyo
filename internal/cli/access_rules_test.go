package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
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
}

func TestAccessRuleVerbs(t *testing.T) {
	var requests []string
	var posted apigen.CreateRuleRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == api.PathPrefix+"/meta" {
			_ = json.NewEncoder(w).Encode(apigen.Meta{ServerVersion: "fixture-current", ApiRevision: api.Revision})
			return
		}
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(apigen.Rule{Id: "rul_1", PrincipalId: posted.Principal, Capability: posted.Capability, Where: posted.Where})
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(apigen.RuleList{Items: []apigen.Rule{}})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	st, stateDir := machineState(t, srv.URL)
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

	// Syntax is refused before any request.
	requests = nil
	for _, args := range [][]string{
		{"access", "rule", "add", "--project", "prj_one", "--principal", "usr_carol"},
		{"access", "rule", "add", "--project", "prj_one", "--principal", "usr_carol", "--capability", "edit", "--only-env", "a", "--except-env", "b"},
		{"access", "rule", "add", "--project", "prj_one", "--principal", "usr_carol", "--capability", "edit", "--only-folder", "a", "--except-key", "b"},
		{"access", "rule", "remove"},
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == api.PathPrefix+"/meta" {
			_ = json.NewEncoder(w).Encode(apigen.Meta{ServerVersion: "fixture-current", ApiRevision: api.Revision})
			return
		}
		if r.Method != http.MethodGet {
			writes++
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(apigen.Rule{})
	}))
	defer srv.Close()
	_, stateDir := machineState(t, srv.URL)
	ios, _, stderr := composeIO(stateDir, t.TempDir(), "hik_1_wl_secret", nil)
	code := Run(t.Context(), ios, []string{"access", "rule", "add", "--instance", "local", "--org", "org_one", "--project", "prj_one",
		"--principal", "usr_carol", "--capability", "reveal"})
	if code != ExitRefused || writes != 0 || !strings.Contains(stderr.String(), "requires a human session") {
		t.Fatalf("exit=%d writes=%d stderr=%s", code, writes, stderr)
	}
}
