package isolation

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
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
		}, nil))
		defer httpServer.Close()
		admin.auth.ExternalOrigin = httpServer.URL
		input, err := json.Marshal(map[string]any{
			"origin": httpServer.URL, "username": "factor-admin", "password": admin.password,
			"elevated": elevated.SessionToken, "principal": string(admin.boot.PrincipalID),
			"codes":         []string{totpCode(t, uri, clk), totpCode(t, uri, clk.Add(30*time.Second))},
			"weak_username": "skew-unenrolled",
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
