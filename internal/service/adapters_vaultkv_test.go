package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/vaultkv"
)

// connectionOnlyKV serves the value-free connection probe of a KV v2 mount.
type connectionOnlyKV struct{ vaultkv.API }

func (connectionOnlyKV) Health(context.Context) (vaultkv.Health, error) {
	return vaultkv.Health{Initialized: true, Version: "2.1.1"}, nil
}
func (connectionOnlyKV) MountInfo(context.Context, string) (vaultkv.Mount, error) {
	return vaultkv.Mount{Type: "kv", Version: "2", UUID: "mount-uuid"}, nil
}
func (connectionOnlyKV) LookupSelf(context.Context) (vaultkv.TokenInfo, error) {
	return vaultkv.TokenInfo{}, nil
}

func TestVaultKVAdapterCreatePersistsMountIdentityAndMapsPaths(t *testing.T) {
	db := adapterServiceDB(t)
	seedKey(t, db, "key_vault", "API_TOKEN")
	kr := adapterKeyring(t, db)
	svc := &Adapters{DB: db, Keyring: kr, ModuleFactory: testModuleFactory(func(provider adapter.Provider, _, _ string) (adapter.Module, func(), error) {
		if provider != adapter.VaultKVProvider {
			t.Fatalf("provider = %q, want vault-kv", provider)
		}
		return &vaultkv.Module{API: connectionOnlyKV{}}, nil, nil
	})}
	request := CreateAdapterRequest{
		Provider: "vault-kv", Origin: "https://VAULT.example:08200/team-a", Credential: []byte("hvs.static"),
		Target: AdapterTargetInput{EnvironmentID: "env_one", DestinationKind: "repository", DestinationOwner: "secret", DestinationName: "apps/pay", NamePrefix: "PROD_", KeyIDs: []string{"key_vault"}},
	}
	for _, origin := range []string{"https://vault.example:8200/team-a/", "https://vault.example:8200/team%2Da", "https://vault.example:8200/team-a?unsafe=1"} {
		invalid := request
		invalid.Origin = origin
		if _, err := svc.Create(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, invalid); err == nil {
			t.Fatalf("unsafe origin %q accepted", origin)
		}
	}
	request.Target.DestinationKind = "organization"
	if _, err := svc.Create(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, request); err == nil {
		t.Fatal("organization destination accepted for vault-kv")
	}
	request.Target.DestinationKind = "repository"
	view, err := svc.Create(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, request)
	if err != nil {
		t.Fatal(err)
	}
	want, err := vaultkv.DestinationID(vaultkv.Mount{UUID: "mount-uuid"}, "apps/pay")
	if err != nil {
		t.Fatal(err)
	}
	if view.Adapter.Provider != "vault-kv" || view.Adapter.Origin != "https://vault.example:8200/team-a" || view.Targets[0].DestinationID != want {
		t.Fatalf("Create() = %+v, want vault-kv with mount-bound destination id %d", view, want)
	}
	for _, statement := range []string{
		`INSERT INTO snapshots (id,org_id,project_id,environment_id,revision,schema_revision,published_by,published_at) VALUES ('snp_vault','org_adapter','prj_adapter','env_one',1,1,'usr_adapter','2026-08-17T00:00:00Z')`,
		`INSERT INTO snapshot_entries (id,org_id,project_id,environment_id,snapshot_id,key_id,key_name,classification,ciphertext,value_entry_id) VALUES ('sen_vault','org_adapter','prj_adapter','env_one','snp_vault','key_vault','API_TOKEN','secret',X'00','val_vault')`,
	} {
		if _, err := db.SQLiteWrite().ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	inspected, err := svc.InspectTarget(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, view.Targets[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inspected.Workflow, "API_TOKEN: secret/apps/pay/PROD_API_TOKEN#value") || strings.Contains(inspected.Workflow, "secrets.") {
		t.Fatalf("workflow = %q, want KV paths", inspected.Workflow)
	}
}
