package isolation

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestSelfConfigSetupCreatesAnOrdinaryProtectedProject(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		auth := authService(t, db)
		managed := &service.SelfConfig{DB: db, Keyring: auth.Keyring, Auth: auth, NodeID: "local", Seed: func() (map[string]string, error) { return map[string]string{"HIKYO_UPDATE_CHANNEL": "off"}, nil }}
		auth.SelfConfig = managed
		boot, err := auth.BootstrapAdmin(t.Context(), "operator", "Operator", "file")
		if err != nil {
			t.Fatal(err)
		}
		actor := service.LocalPrincipal(boot.PrincipalID)
		status, err := managed.Status(t.Context(), actor)
		if err != nil {
			t.Fatal(err)
		}
		if !status.Managed || status.Binding == nil || status.DesiredRevision == nil || *status.DesiredRevision != 1 {
			t.Fatalf("setup did not bind its initial revision: %+v", status)
		}
		scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
		values := &service.Values{DB: db, Keyring: auth.Keyring, Auth: auth}
		cell, err := values.Get(t.Context(), actor, scope, "HIKYO_UPDATE_CHANNEL", false)
		if err != nil {
			t.Fatal(err)
		}
		if !cell.Set || cell.Value != "off" {
			t.Fatalf("normal matrix did not read the seed: %+v", cell)
		}
		orgs := &service.Orgs{DB: db}
		if err := orgs.Delete(t.Context(), actor, scope.Org); err == nil {
			t.Fatal("system organization could be deleted")
		}
	})
}
