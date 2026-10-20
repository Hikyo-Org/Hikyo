package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func TestProjectCapacityGrammar(t *testing.T) {
	for _, args := range [][]string{
		{"set"}, {"set", "--inherit", "--max-keys", "5000"},
		{"set", "--inherit", "--max-keys", "0"}, {"set", "--max-keys", "0"},
		{"set", "--max-keys", "-1"}, {"set", "--max-keys", "10001"},
		{"set", "--max-keys", "five"}, {"get", "--max-keys", "5000"},
		{"get", "unexpected"}, {"get", "-o", "invalid"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			ios := IO{Stdout: &stdout, Stderr: &stderr, Workdir: t.TempDir(), Env: Env{Getenv: func(string) string { return "" }}}
			if got := Run(t.Context(), ios, append([]string{"project", "capacity"}, args...)); got != ExitUsage {
				t.Fatalf("exit=%d want=%d stderr=%s", got, ExitUsage, stderr.String())
			}
		})
	}
}

func TestProjectCapacityWire(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		put      bool
		inherit  bool
		conflict bool
		readFail bool
		table    bool
		capped   bool
	}{
		{name: "read JSON", args: []string{"get"}},
		{name: "read table", args: []string{"get"}, table: true},
		{name: "read retained capped override", args: []string{"get"}, capped: true},
		{name: "custom override", args: []string{"set", "--max-keys", "5000"}, put: true},
		{name: "reset inheritance", args: []string{"set", "--inherit"}, put: true, inherit: true},
		{name: "stale policy is not retried", args: []string{"set", "--max-keys", "5000"}, put: true, conflict: true},
		{name: "failed read prevents write", args: []string{"set", "--max-keys", "5000"}, readFail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := api.PathPrefix + "/orgs/org_a/projects/prj_a/capacity"
			requests := []string{}
			current := apigen.ProjectCapacity{InstanceDefault: 1000, InstanceMaximum: 10000, EffectiveLimit: 1000,
				Inherited: true, KeyCount: 23, EnvironmentCount: 4, Revision: 7}
			if tc.capped {
				saved := 5000
				current.KeyLimitOverride = &saved
				current.InstanceMaximum = 2500
				current.EffectiveLimit = 2500
				current.Inherited = false
				current.Capped = true
			}
			server := newRevisionAwareFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method)
				if r.URL.Path != path || r.Header.Get("Authorization") != "Bearer capacity-test-token" {
					t.Errorf("unexpected target or authentication: method=%s path=%s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				switch r.Method {
				case http.MethodGet:
					if tc.readFail {
						w.WriteHeader(http.StatusNotFound)
						_, _ = w.Write([]byte(`{"error":"not_found"}`))
						return
					}
				case http.MethodPut:
					var raw map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					encoded, err := json.Marshal(raw)
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					var body apigen.SetProjectCapacityRequest
					if err := json.Unmarshal(encoded, &body); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if body.ExpectedRevision != 7 || body.ExpectedInstanceDefault != 1000 || body.ExpectedInstanceMaximum != 10000 {
						t.Errorf("write did not bind the read policy: %+v", body)
					}
					if tc.inherit {
						if string(raw["key_limit_override"]) != "null" {
							t.Errorf("inherit must explicitly send null, got %s", raw["key_limit_override"])
						}
					} else if body.KeyLimitOverride == nil || *body.KeyLimitOverride != 5000 {
						t.Errorf("override missing or changed: %+v", body)
					}
					if tc.conflict {
						w.WriteHeader(http.StatusConflict)
						_, _ = w.Write([]byte(`{"error":"conflict","message":"capacity policy changed"}`))
						return
					}
					current.Revision++
					current.KeyLimitOverride = body.KeyLimitOverride
					current.Inherited = tc.inherit
					if body.KeyLimitOverride != nil {
						current.EffectiveLimit = *body.KeyLimitOverride
					}
				default:
					t.Errorf("unexpected method %s", r.Method)
				}
				_ = json.NewEncoder(w).Encode(current)
			}))
			t.Cleanup(server.Close)
			stateDir := t.TempDir()
			st := &State{dir: stateDir}
			if err := st.Trust().Put(pinnedTestEntry("local", server)); err != nil {
				t.Fatal(err)
			}
			if err := st.PutSession(SessionArtifact{Instance: "local", Origin: server.URL, Token: "capacity-test-token",
				SessionID: "ses_capacity", Principal: "usr_capacity", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			ios := IO{Stdout: &stdout, Stderr: &stderr, Workdir: t.TempDir(), Env: Env{Getenv: func(key string) string {
				if key == "HIKYO_STATE_DIR" {
					return stateDir
				}
				return ""
			}}}
			args := append([]string{"project", "capacity"}, tc.args...)
			args = append(args, "--instance", "local", "--org", "org_a", "--project", "prj_a")
			if !tc.table {
				args = append(args, "-o", "json")
			}
			wantExit := ExitOK
			if tc.conflict {
				wantExit = ExitRefused
			}
			if tc.readFail {
				wantExit = ExitNotFound
			}
			if code := Run(t.Context(), ios, args); code != wantExit {
				t.Fatalf("exit=%d want=%d stderr=%s", code, wantExit, stderr.String())
			}
			wantMethods := "GET"
			if tc.put {
				wantMethods += ",PUT"
			}
			if got := strings.Join(requests, ","); got != wantMethods {
				t.Fatalf("requests=%s want=%s", got, wantMethods)
			}
			if tc.conflict || tc.readFail {
				if stdout.Len() != 0 {
					t.Fatalf("failed operation printed success: %s", stdout.String())
				}
			} else if tc.table {
				if !strings.Contains(stdout.String(), "EFFECTIVE LIMIT") || !strings.Contains(stdout.String(), "ENVIRONMENTS") {
					t.Fatalf("table omits capacity/usage: %s", stdout.String())
				}
			} else {
				var got apigen.ProjectCapacity
				if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.Revision != current.Revision || got.EffectiveLimit != current.EffectiveLimit || got.KeyCount != 23 || got.EnvironmentCount != 4 || got.Inherited != current.Inherited || got.Capped != current.Capped {
					t.Fatalf("output lost server policy: %+v", got)
				}
				if current.KeyLimitOverride != nil && (got.KeyLimitOverride == nil || *got.KeyLimitOverride != *current.KeyLimitOverride) {
					t.Fatalf("output lost saved override: %+v", got)
				}
			}
			if strings.Contains(stdout.String()+stderr.String(), "capacity-test-token") {
				t.Fatal("command leaked authentication token")
			}
		})
	}
}
