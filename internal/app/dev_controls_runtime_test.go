package app

import (
	"context"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/config"
)

func TestDevelopmentNodeControlsChangeActualRuntimeAfterActivation(t *testing.T) {
	cfg := devConfig(t)
	cfg.DevAdmissionPerIPPerMinute = 100
	srv, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	old := srv.owner.current.graph
	for range 100 {
		if !old.limiter.AllowDiscovery("192.0.2.8") {
			t.Fatal("fixture admission refused early")
		}
	}
	if old.limiter.AllowDiscovery("192.0.2.8") {
		t.Fatal("fixture admission limit not enforced")
	}
	budget := srv.owner.budget
	if !budget.Enabled() || srv.selfConfig.Budget != budget {
		t.Fatal("fixture budget capture missing")
	}
	bundle := nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) {
		node["HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE"] = "200"
		node["HIKYO_DEV_SERVICE_BUDGETS_DISABLED"] = "true"
		node["HIKYO_DEV_ADAPTER_FAKE_PROVIDER"] = "true"
	})
	prepared, err := srv.owner.Prepare(t.Context(), bundle)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if !budget.Enabled() || old.limiter.AllowDiscovery("192.0.2.8") {
		t.Fatal("preparation changed serving policy")
	}
	if err := prepared.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	next := srv.owner.current.graph
	for range 100 {
		if !next.limiter.AllowDiscovery("192.0.2.8") {
			t.Fatal("new admission limit refused before 200 total requests")
		}
	}
	if next.limiter.AllowDiscovery("192.0.2.8") {
		t.Fatal("new limit did not preserve old request history")
	}
	if budget.Enabled() || srv.owner.budget != budget || srv.selfConfig.Budget != budget {
		t.Fatal("actual shared budget did not change enforcement")
	}
	lease := developmentModule(t, srv)
	defer lease.Release()
	connection, err := lease.Module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: adapter.Destination{Kind: adapter.Repository, Owner: "dev", Name: "repo"}})
	if err != nil || connection.Version != "dev-fake" {
		t.Fatalf("installed provider did not use fake: %+v, %v", connection, err)
	}
	activateNode(t, srv, nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) {
		delete(node, "HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE")
		delete(node, "HIKYO_DEV_SERVICE_BUDGETS_DISABLED")
		delete(node, "HIKYO_DEV_ADAPTER_FAKE_PROVIDER")
	}))
	if !budget.Enabled() || srv.owner.current.graph.cfg.DevAdapterFakeProvider || srv.owner.current.graph.cfg.DevAdmissionPerIPPerMinute != 0 {
		t.Fatal("removed overrides did not restore real defaults")
	}
}

func developmentModule(t *testing.T, srv *Server) *adapter.ModuleLease {
	t.Helper()
	loader, ok := srv.owner.current.graph.adapterWorker.Loader.(*adapterLoader)
	if !ok {
		t.Fatal("missing adapter loader")
	}
	lease, err := loader.moduleFactory(adapter.ForgejoProvider, adapter.Config{Origin: "https://provider.example"}, "development-credential")
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

func TestDevelopmentProviderPreservesRemoteStateAcrossOrdinaryReloadAndRefusesSwitch(t *testing.T) {
	cfg := devConfig(t)
	cfg.DevAdapterFakeProvider = true
	srv, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	destination := adapter.Destination{Kind: adapter.Repository, Owner: "dev", Name: "repo"}
	provider := srv.owner.fakeProvider
	provider.store("https://provider.example", destination).secrets["TOKEN"] = "existing simulated remote value"
	activateNode(t, srv, nodeCandidate(t, srv, func(owner map[string]string, _ map[string]string) { owner["HIKYO_ARGON2_TIME"] = "4" }))
	lease := developmentModule(t, srv)
	defer lease.Release()
	plan, err := lease.Module.Plan(t.Context(), adapter.PlanRequest{Target: adapter.Target{Destination: destination}, Manifest: []adapter.ManifestEntry{{KeyID: "token", CanonicalName: "TOKEN", Classification: adapter.SecretClassification, Value: "replacement"}}})
	if err != nil {
		t.Fatal(err)
	}
	conflict := false
	for _, change := range plan.Changes {
		conflict = conflict || change.EffectiveName == "TOKEN" && change.Disposition == adapter.Conflict
	}
	if !conflict {
		t.Fatal("ordinary reload erased simulated remote state")
	}
	before := srv.owner.current.graph
	if _, err := srv.owner.Prepare(t.Context(), nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) { node["HIKYO_DEV_ADAPTER_FAKE_PROVIDER"] = "false" })); err == nil {
		t.Fatal("switch abandoned simulated provider contents")
	}
	if srv.owner.current.graph != before {
		t.Fatal("refused switch replaced graph")
	}
}

func TestDevelopmentProviderSwitchRechecksConfigurationAfterPreparation(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		name := "sqlite"
		if postgres {
			name = "postgres"
		}
		t.Run(name, func(t *testing.T) {
			cfg := devConfig(t)
			if postgres {
				cfg = nodePostgresConfig(t)
			}
			srv, err := Boot(t.Context(), cfg, testLogger())
			if err != nil {
				t.Fatal(err)
			}
			defer srv.Close()
			bundle := nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) {
				node["HIKYO_DEV_ADAPTER_FAKE_PROVIDER"] = "true"
				node["HIKYO_DEV_SERVICE_BUDGETS_DISABLED"] = "true"
			})
			prepared, err := srv.owner.Prepare(t.Context(), bundle)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			seedDevelopmentConfigureFence(t, srv)
			if err := prepared.Activate(t.Context()); err == nil {
				t.Fatal("configuration created after preparation switched provider")
			}
			if srv.owner.current.graph.cfg.DevAdapterFakeProvider || !srv.owner.budget.Enabled() {
				t.Fatal("failed activation changed provider or budget")
			}
			if _, err := srv.owner.Prepare(t.Context(), bundle); err == nil {
				t.Fatal("pending configuration admitted another preparation")
			}
		})
	}
}

func TestDevelopmentControlsCannotChangeDeploymentTrustContext(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	bundle := nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) { node["HIKYO_DEV_SERVICE_BUDGETS_DISABLED"] = "true" })
	srv.owner.base.Dev = false
	if _, err := srv.owner.Prepare(t.Context(), bundle); err == nil {
		t.Fatal("production context accepted managed development controls")
	}
	srv.owner.base.Dev = true
	srv.owner.base.HA = true
	if err := srv.owner.checkDevelopmentProviderSwitch(context.Background(), &config.Config{Dev: true, HA: true}); err == nil {
		t.Fatal("HA node changed provider mode independently")
	}
}
