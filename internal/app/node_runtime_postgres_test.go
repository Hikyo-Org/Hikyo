package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func nodePostgresConfig(t *testing.T) *config.Config {
	t.Helper()
	database := upgradeDrillDatabase(t, store.EnginePostgres)
	cfg := devConfig(t)
	cfg.Store = config.Datastore{Engine: config.EnginePostgres, DSN: database.DSN, PostgresPoolMax: 7}
	cfg.Upgrade.StateDirectory = t.TempDir()
	stateDirectory, err := filepath.EvalSymlinks(cfg.Upgrade.StateDirectory)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Upgrade.StateDirectory = stateDirectory
	if err := os.Chmod(cfg.Upgrade.StateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestNodeRuntimePostgresPoolAppliesWithoutReplacingIdentity(t *testing.T) {
	srv, err := Boot(t.Context(), nodePostgresConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	startOwnerServer(t, srv)
	db, keyring, coordination := srv.db, srv.keyring, srv.db.Coordination()
	if got := db.ConnectionPoolLimits().Primary; got != 7 {
		t.Fatalf("initial pool = %d", got)
	}
	prepared, err := srv.owner.Prepare(t.Context(), nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) { node["HIKYO_PG_POOL_MAX"] = "3" }))
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if got := db.ConnectionPoolLimits().Primary; got != 7 {
		t.Fatalf("preparation changed pool = %d", got)
	}
	if err := prepared.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := db.ConnectionPoolLimits().Primary; got != 3 {
		t.Fatalf("installed pool = %d", got)
	}
	if srv.db != db || srv.keyring != keyring {
		t.Fatal("pool change replaced admitted instance identity")
	}
	if _, err := coordination.Now(t.Context()); err != nil {
		t.Fatalf("existing coordination handle lost pool: %v", err)
	}
	if got := ownerHTTPStatus(t, srv, http.MethodGet, "/api/v1/meta"); got != http.StatusOK {
		t.Fatalf("HTTP after pool swap = %d", got)
	}
}

func TestNodeRuntimeMissingHANodeBootsOnlyAdministrativeRecovery(t *testing.T) {
	cfg := nodePostgresConfig(t)
	cfg.HA, cfg.NodeID = true, "node-a"
	first, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := first.owner.current.graph.auth.BootstrapAdmin(t.Context(), "operator", "Operator", "stdout")
	if err != nil {
		_ = first.Close()
		t.Fatal(err)
	}
	if err := first.selfConfig.LoadRuntime(t.Context()); err != nil {
		_ = first.Close()
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	joining := *cfg
	joining.NodeID = "node-b"
	srv, err := Boot(t.Context(), &joining, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.selfConfig.Capture(t.Context()); err == nil {
		_ = srv.Close()
		t.Fatal("unconfigured node acknowledged business runtime")
	}
	artifact, verifier, err := crypto.NewArtifact(crypto.ArtifactBrowserSession)
	if err != nil {
		_ = srv.Close()
		t.Fatal(err)
	}
	now := time.Now()
	err = tx.Write(t.Context(), srv.db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		generation, err := az.PrincipalGeneration(ctx, bootstrap.PrincipalID)
		if err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		return az.MintSession(ctx, authz.NewSession{ID: "ses_node_repair", PrincipalID: bootstrap.PrincipalID, Verifier: verifier, Artifact: "browser", SessionGeneration: generation, CredentialEpoch: epoch, AuthMethod: "local-passkey", Factors: `["webauthn"]`, AuthenticatedAt: now, CreatedAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour), SourceIP: "127.0.0.1", UserAgent: "node-repair-test"})
	})
	if err != nil {
		_ = srv.Close()
		t.Fatal(err)
	}
	startOwnerServer(t, srv)
	for _, check := range []struct {
		path string
		want int
	}{
		{"/api/v1/auth/whoami", http.StatusOK},
		{"/api/v1/instance/config", http.StatusOK},
		{"/api/v1/orgs/org_00000000-0000-7000-8000-000000000001/projects", http.StatusServiceUnavailable},
	} {
		req, err := http.NewRequest(http.MethodGet, "http://"+srv.Addr+check.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+artifact)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != check.want {
			t.Fatalf("%s = %d, want %d", check.path, res.StatusCode, check.want)
		}
	}
	res, err := http.Get("http://" + srv.OperationalAddr + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured node readiness = %d", res.StatusCode)
	}
	if _, err := srv.selfConfig.Status(t.Context(), service.Bearer(artifact)); err != nil {
		t.Fatal(err)
	}
}
