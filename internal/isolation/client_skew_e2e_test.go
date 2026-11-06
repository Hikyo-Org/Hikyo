package isolation

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/transit"
)

// The client source comes from the immutable freeze tag, never regeneration
// against this server. The CI wrapper sets it explicitly and requires both
// engines; ordinary Go-only runs have no JavaScript dependency.
func TestFrozenGeneratedClientAgainstCurrentServer(t *testing.T) {
	clientRoot := os.Getenv("HIKYO_FROZEN_CLIENT_ROOT")
	if clientRoot == "" {
		t.Skip("run scripts/ci/check-client-skew.sh for the generated-client lane")
	}
	if !filepath.IsAbs(clientRoot) {
		t.Fatal("frozen client root must be absolute")
	}
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := bootstrapAdmin(t, db, adminOpts{
			username: "factor-admin", displayName: "Skew Admin",
			password: "correct horse battery staple client skew",
		})
		// Transit and PKI for the frozen client to read. Everything is seeded
		// before the first login so no later grant change stales a session.
		// The environment holds one mldsa65 key: the closed v1.0.0 Zod enum must
		// reject the whole list (the one-time skew break of the 2026-11-04
		// api-cli-surface amendment). PKI ML-DSA stays behind service.PKI.MLDSAEnabled.
		// A real hierarchy, because the fixture's hand-written ids (org_a) do not
		// satisfy the Id grammar the frozen Zod schemas enforce.
		operator := service.LocalPrincipal(root)
		keyring := probeKeyring(t, db)
		org, err := (&service.Orgs{DB: db}).Create(t.Context(), operator, "skew-transit-org", true, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		project, err := (&service.Projects{DB: db}).Create(t.Context(), operator, domain.OrgID(org.ID), "skew-project")
		if err != nil {
			t.Fatal(err)
		}
		projectScope := domain.Scope{Org: domain.OrgID(org.ID), Project: domain.ProjectID(project.ID)}
		env, err := (&service.Environments{DB: db, Keyring: keyring}).Create(t.Context(), operator, projectScope, "skew-env", nil)
		if err != nil {
			t.Fatal(err)
		}
		envScope := domain.Scope{Org: projectScope.Org, Project: projectScope.Project, Env: domain.EnvID(env.ID)}
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_skew_admin_read','`+string(admin.boot.PrincipalID)+`','read','`+org.ID+`','`+project.ID+`','`+env.ID+`',`+ts+`)`)
		// crypto-use and crypto-manage let the frozen SDK run the mldsa65
		// lifecycle (create, sign, verify, rotate) against this server.
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_skew_admin_manage','`+string(admin.boot.PrincipalID)+`','crypto-manage','`+org.ID+`','`+project.ID+`',NULL,`+ts+`)`)
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_skew_admin_use','`+string(admin.boot.PrincipalID)+`','crypto-use','`+org.ID+`','`+project.ID+`','`+env.ID+`',`+ts+`)`)
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_skew_root_crypto','`+string(root)+`','crypto-manage','`+org.ID+`','`+project.ID+`',NULL,`+ts+`)`)
		transitSvc := &service.Transit{DB: db, Keyring: keyring, Custody: transit.NewRegistry(&transit.Software{Keyring: keyring})}
		for _, req := range []service.CreateTransitKeyRequest{
			{Name: "skew-signing", Algorithm: "ed25519", AllowedOperations: []string{"sign", "verify"}},
			{Name: "skew-sealing", Algorithm: "xchacha20-poly1305", AllowedOperations: []string{"encrypt", "decrypt"}},
			{Name: "skew-pq", Algorithm: "mldsa65", AllowedOperations: []string{"sign", "verify"}},
		} {
			if _, err := transitSvc.CreateKey(t.Context(), operator, envScope, req); err != nil {
				t.Fatal(err)
			}
		}
		pkiClk := &pkiClock{now: time.Now().UTC().Truncate(time.Second)}
		pkiService := pkiSvc(t, db, pkiClk)
		if _, err := pkiService.CreateIssuer(t.Context(), service.LocalPrincipal(root), service.PKIIssuerRequest{Mode: "root", Name: "skew-root", CommonName: "Skew Root"}); err != nil {
			t.Fatal(err)
		}
		policy := pkiWebPolicy()
		policy.AllowedIssuers = []string{"skew-root"}
		if _, err := pkiService.CreateProfile(t.Context(), service.LocalPrincipal(root), "skew-web", policy); err != nil {
			t.Fatal(err)
		}
		base := time.Now().UTC()
		clk := base
		admin.auth.Now = func() time.Time { return clk }
		initial, err := admin.auth.LocalLogin(t.Context(), "factor-admin", admin.password, service.ArtifactCLI)
		if err != nil {
			t.Fatal(err)
		}
		uri, err := admin.auth.EnrolTOTPStart(t.Context(), initial.SessionToken, admin.password)
		if err != nil {
			t.Fatal(err)
		}
		confirmed, err := admin.auth.EnrolTOTPConfirm(t.Context(), initial.SessionToken, totpCode(t, uri, clk))
		if err != nil {
			t.Fatal(err)
		}
		clk = clk.Add(30 * time.Second)
		elevated, err := admin.auth.StepUpTOTP(t.Context(), confirmed.SessionToken, totpCode(t, uri, clk))
		if err != nil {
			t.Fatal(err)
		}
		// Keep the original under-assured instance-list refusal as a separate
		// optional-policy, unenrolled operator control.
		grants := &service.Grants{DB: db, Auth: admin.auth, Now: func() time.Time { return clk }}
		weak, err := grants.InviteMember(t.Context(), service.Bearer(elevated.SessionToken), service.InviteSpec{
			Username: "skew-unenrolled", Template: domain.TemplateOperator, Delivery: "response",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := admin.auth.EstablishCredential(t.Context(), weak.Authority, admin.password); err != nil {
			t.Fatal(err)
		}
		// Two unspent steps fit the production skew allowance at this fixed
		// clock. HTTP workers never mutate the clock or the factor ledger.
		clk = clk.Add(30 * time.Second)
		// The clock stops changing before HTTP workers start.
		httpServer := httptest.NewServer(server.New(&server.API{
			Auth: admin.auth, Orgs: &service.Orgs{DB: db}, Version: "current-server-skew-fixture",
			Transit: transitSvc, PKI: pkiService,
		}, nil))
		defer httpServer.Close()
		admin.auth.ExternalOrigin = httpServer.URL
		// The current Go side decodes the same list: the generated enum type is a
		// plain string, so mldsa65 is not a decode failure for the CLI either.
		listReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, httpServer.URL+"/api/v1/orgs/"+org.ID+"/projects/"+project.ID+"/environments/"+env.ID+"/transit-keys", nil)
		if err != nil {
			t.Fatal(err)
		}
		listReq.Header.Set("Authorization", "Bearer "+elevated.SessionToken)
		listRes, err := http.DefaultClient.Do(listReq)
		if err != nil {
			t.Fatal(err)
		}
		var goList apigen.TransitKeyList
		decodeErr := json.NewDecoder(listRes.Body).Decode(&goList)
		_ = listRes.Body.Close()
		if listRes.StatusCode != http.StatusOK || decodeErr != nil || len(goList.Items) != 3 {
			t.Fatalf("go decode of the transit list: status %d, %d items, err %v", listRes.StatusCode, len(goList.Items), decodeErr)
		}
		if !slices.ContainsFunc(goList.Items, func(k apigen.TransitKey) bool { return k.Algorithm == apigen.TransitAlgorithm("mldsa65") }) {
			t.Fatalf("go decode lost the mldsa65 key: %+v", goList.Items)
		}
		input, err := json.Marshal(map[string]any{
			"origin": httpServer.URL, "username": "factor-admin", "password": admin.password,
			"elevated": elevated.SessionToken, "principal": string(admin.boot.PrincipalID),
			"codes":         []string{totpCode(t, uri, clk), totpCode(t, uri, clk.Add(30*time.Second))},
			"weak_username": "skew-unenrolled",
			"scope":         map[string]string{"org": org.ID, "project": project.ID, "environment": env.ID},
		})
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), "node", "../../scripts/ci/frozen-client.mjs", clientRoot)
		cmd.Stdin = bytes.NewReader(input)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("frozen generated client failed: %v\n%s", err, output)
		} else {
			t.Log(string(output))
			if queryInt(t, db, "SELECT COUNT(*) FROM reauth_windows") != 0 {
				t.Fatal("login handoff created operation proof")
			}
		}
	})
}
