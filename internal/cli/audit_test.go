package cli_test

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/cli"
)

func TestAuditQueryDispatch(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags []string
		path  string
	}{
		{"instance", []string{"--instance-scope"}, "/api/v1/instance/audit"},
		{"org", []string{"--org", "acme"}, "/api/v1/orgs/acme/audit"},
		{"project", []string{"--org", "acme", "--project", "app"}, "/api/v1/orgs/acme/projects/app/audit"},
		{"environment", []string{"--org", "acme", "--project", "app", "--env", "prod"}, "/api/v1/orgs/acme/projects/app/environments/prod/audit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			ios, stdout, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != tc.path || r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("request = %s %s, auth %q", r.Method, r.URL, r.Header.Get("Authorization"))
				}
				q := r.URL.Query()
				if q.Get("limit") != "3" || q.Get("after_seq") != "7" || q.Get("to_seq") != "19" || q.Get("operation") != "auth.*" || len(q["outcomes"]) != 2 {
					t.Errorf("filters = %v", q)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"items":[],"count":0,"next_after_seq":19,"upper_seq":19,"exhausted":true}`))
			}))
			args := append([]string{"audit", "query", "--instance", "local", "-o", "json", "--limit", "3", "--after-seq", "7", "--to-seq", "19", "--operation", "auth.*", "--outcome", "denied", "--outcome", "failure", "--outcome", "denied"}, tc.flags...)
			if code := cli.Run(t.Context(), ios, args); code != cli.ExitOK || calls != 1 {
				t.Fatalf("exit=%d calls=%d stdout=%s stderr=%s", code, calls, stdout, strings.SplitN(stderr.String(), "\n", 2)[0])
			}
		})
	}
}

func TestAuditIncompleteExportDoesNotPublishStaging(t *testing.T) {
	staging := t.TempDir()
	t.Setenv("TMPDIR", staging)
	ios, stdout, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/instance/audit/export" || r.URL.Query().Get("receipt") != "v1" {
			t.Errorf("export request = %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Hikyo-Audit-Receipt", "v1")
		w.Header().Set("Hikyo-Audit-Export-ID", "started-fixture")
		w.Header().Set("Hikyo-Audit-Snapshot-Time", "2026-10-28T00:00:00Z")
		_, _ = w.Write([]byte(`{"seq":1,"id":"evt_fixture","type":"auth.login","schema_version":1,"occurred_at":"2026-10-28T00:00:00Z","occurred_asserted":false,"recorded_at":"2026-10-28T00:00:00Z","actor_class":"unauthenticated","scope_class":"instance","outcome":"failure","origin":"api","payload":{}}` + "\n"))
	}))
	if code := cli.Run(t.Context(), ios, []string{"audit", "export", "--instance", "local", "--instance-scope"}); code != cli.ExitUnavailable || stdout.Len() != 0 {
		t.Fatalf("export exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	files, err := os.ReadDir(staging)
	if err != nil || len(files) != 0 {
		t.Fatalf("staging leaked: %v, err=%v", files, err)
	}
}

func TestAuditInstanceRefusesMachineBeforeRequest(t *testing.T) {
	ios, stdout, stderr := definitionsTestIO(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("instance collector made a network request") }))
	get := ios.Env.Getenv
	ios.Env.Getenv = func(key string) string {
		if key == "HIKYO_TOKEN" {
			return "synthetic-machine-token"
		}
		return get(key)
	}
	for _, sub := range []string{"query", "export"} {
		stdout.Reset()
		stderr.Reset()
		if code := cli.Run(t.Context(), ios, []string{"audit", sub, "--instance", "local", "--instance-scope", "--auth", "machine"}); code != cli.ExitRefused || stdout.Len() != 0 {
			t.Fatalf("%s exit=%d stdout=%s stderr=%s", sub, code, stdout, stderr)
		}
	}
}

func TestAuditRejectsInvalidScopeAndFiltersBeforeNetwork(t *testing.T) {
	for _, flags := range [][]string{
		{"--instance-scope", "--org", "acme"}, {"--instance-scope", "--env", "prod"},
		{"--instance-scope", "--limit", "0"}, {"--instance-scope", "--limit", "1001"},
		{"--instance-scope", "--after-seq", "-1"}, {"--instance-scope", "--outcome", "made-up"},
		{"--instance-scope", "--from", "yesterday"}, {"--instance-scope", "stray"},
	} {
		t.Run("invalid", func(t *testing.T) {
			ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected request") }))
			if code := cli.Run(t.Context(), ios, append([]string{"audit", "query"}, flags...)); code != cli.ExitUsage {
				t.Fatalf("exit=%d stderr=%s", code, stderr)
			}
		})
	}
}
