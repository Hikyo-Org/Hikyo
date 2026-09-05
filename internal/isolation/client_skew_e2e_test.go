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
		elevated := enrolTOTPAndStepUp(t, admin.auth, t.Context(), base, &clk, admin.password)
		// The clock stops changing before HTTP workers start.
		httpServer := httptest.NewServer(server.New(&service.System{DB: db}, &server.API{
			Auth: admin.auth, Orgs: &service.Orgs{DB: db}, Version: "current-server-skew-fixture",
		}, nil))
		defer httpServer.Close()
		input, err := json.Marshal(map[string]string{
			"origin": httpServer.URL, "username": "factor-admin", "password": admin.password,
			"elevated": elevated, "principal": string(admin.boot.PrincipalID),
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
		}
	})
}
