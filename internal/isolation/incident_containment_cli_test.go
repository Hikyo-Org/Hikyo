package isolation

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Exercise the runbook's existing CLI spellings through the real TLS router,
// authorization and persistence. No production profile, terminal or provider.
func TestManualIncidentContainmentCLI(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		e := newAccessWireEnv(t, db)
		grants := &service.Grants{DB: db}
		grantInstanceCapability(t, db, root, domain.CapCredentialReset)
		// Preserve an independent manager at this exact org scope before
		// removing the creating administrator's last-manager grant.
		if _, err := grants.Create(ctx, service.LocalPrincipal(root), service.GrantSpec{Target: root, Capability: domain.CapManageMembers, Scope: domain.Scope{Org: domain.OrgID(e.org)}}); err != nil {
			t.Fatal(err)
		}
		if _, err := grants.Create(ctx, service.LocalPrincipal(root), service.GrantSpec{Target: alice, Capability: domain.CapCredentialReset, Scope: domain.Scope{Org: domain.OrgID(e.org)}}); err != nil {
			t.Fatal(err)
		}
		orgOperator := sessionWithWindows(t, db, alice)
		operator := sessionWithWindows(t, db, root)
		rules := &service.Rules{DB: db}
		rule, err := rules.Create(ctx, service.Bearer(operator), service.RuleSpec{Target: e.admin, Capability: domain.CapRead, Org: domain.OrgID(e.org), Where: domain.Where{Projects: []domain.ProjectID{domain.ProjectID(e.project)}, EnvMode: domain.AxisAll, KeyMode: domain.AxisAll}})
		if err != nil {
			t.Fatal(err)
		}
		targetSession := sessionWithWindows(t, db, e.admin)
		delegation := seedSecurityDeveloper(t, db, targetSession, domain.Scope{Org: domain.OrgID(e.org), Project: domain.ProjectID(e.project), Env: domain.EnvID(e.env)}, "incident-cli")
		srv := httptest.NewTLSServer(server.New(&server.API{
			Auth: e.auth, Grants: grants, Rules: rules,
			Workspace:            &service.Workspace{DB: db, Version: "incident-fixture"},
			DeveloperCredentials: &service.DeveloperCredentials{DB: db, Auth: e.auth},
			Version:              "incident-fixture",
		}, nil))
		t.Cleanup(srv.Close)
		e.srv = srv
		stateDir, workdir := t.TempDir(), t.TempDir()
		env := cli.Env{Getenv: func(key string) string {
			if key == "HIKYO_STATE_DIR" {
				return stateDir
			}
			return ""
		}}
		state, err := cli.NewState(env)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.Trust().Put(cli.TrustEntry{Name: "clean", Origin: srv.URL, SPKIPin: cli.SPKIFingerprint(srv.Certificate())}); err != nil {
			t.Fatal(err)
		}
		caller := authenticateCaller(t, db, operator)
		if err := state.PutSession(cli.SessionArtifact{Instance: "clean", Origin: srv.URL, Token: operator, SessionID: caller.SessionID, Principal: string(root), ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}); err != nil {
			t.Fatal(err)
		}
		run := func(args ...string) []byte {
			t.Helper()
			var stdout, stderr bytes.Buffer
			code := cli.Run(ctx, cli.IO{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Env: env, Workdir: workdir}, append(args, "--instance", "clean"))
			if code != cli.ExitOK {
				t.Fatalf("CLI %v: exit %d: %s", args, code, stderr.String())
			}
			return stdout.Bytes()
		}
		for _, dimensions := range [][]string{{"--instance-scope"}, {"--org", e.org}} {
			args := append([]string{"access", "grant", "list", "-o", "json"}, dimensions...)
			var list apigen.GrantList
			if err := json.Unmarshal(run(args...), &list); err != nil {
				t.Fatal(err)
			}
			for _, row := range list.Items {
				if row.PrincipalId != string(e.admin) {
					continue
				}
				remove := []string{"access", "grant", "remove", "--principal", row.PrincipalId, "--capability", string(row.Capability)}
				if row.Scope.OrgId == nil {
					remove = append(remove, "--instance-scope")
				} else {
					remove = append(remove, "--org", *row.Scope.OrgId)
					if row.Scope.ProjectId != nil {
						remove = append(remove, "--project", *row.Scope.ProjectId)
					}
					if row.Scope.EnvironmentId != nil {
						remove = append(remove, "--env", *row.Scope.EnvironmentId)
					}
				}
				run(remove...)
			}
		}
		var ruleList apigen.RuleList
		if err := json.Unmarshal(run("access", "rule", "list", "--org", e.org, "-o", "json"), &ruleList); err != nil || len(ruleList.Items) != 1 {
			t.Fatalf("rule inventory: %+v, %v", ruleList, err)
		}
		run("access", "rule", "remove", rule.ID, "--org", e.org)
		// A zero-grant target is no longer org-bounded. The final network
		// reset requires the existing instance holder, not a widened org grant.
		if status, _ := e.callAs(t, orgOperator, http.MethodPost, "/api/v1/accounts/"+string(e.admin)+"/credential-reset", nil); status != http.StatusUnauthorized {
			t.Fatalf("org-only reset holder reached a fully swept target: HTTP %d", status)
		}
		liveAtReset := sessionWithWindows(t, db, e.admin)
		if authenticateCaller(t, db, liveAtReset).Principal != e.admin {
			t.Fatal("reset-time negative control needs a live target session after the sweep")
		}
		authorityFile := filepath.Join(t.TempDir(), "authority")
		out := run("account", "reset-credential", string(e.admin), "--output-file", authorityFile)
		if authenticateCaller(t, db, liveAtReset).Principal != "" {
			t.Fatal("CLI reset retained a live session before credential establishment")
		}
		delivered, err := os.ReadFile(authorityFile)
		if err != nil {
			t.Fatalf("read private reset authority: %v", err)
		}
		authority := strings.TrimSpace(string(delivered))
		if err := crypto.ParseArtifact(authority, crypto.ArtifactBootstrap); err != nil {
			t.Fatal("CLI reset did not deliver a valid establishment authority")
		}
		info, err := os.Stat(authorityFile)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("CLI reset authority file must have private permissions: %v", err)
		}
		if bytes.Contains(out, []byte(authority)) || bytes.Contains(out, []byte("hik_1_")) {
			t.Fatal("CLI reset printed the establishment authority")
		}
		if err := e.auth.EstablishCredential(ctx, authority, "independently delivered CLI replacement"); err != nil {
			t.Fatalf("consume the private-file authority: %v", err)
		}
		for _, bearer := range []string{targetSession, delegation, liveAtReset} {
			if authenticateCaller(t, db, bearer).Principal != "" {
				t.Fatal("CLI workflow left an affected artifact live")
			}
		}

	})
}
