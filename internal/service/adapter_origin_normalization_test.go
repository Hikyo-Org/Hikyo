package service

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestAdapterCreateNormalizesOriginBeforeProbeAndPersistence(t *testing.T) {
	db := adapterServiceDB(t)
	seedKey(t, db, "key_origin", "API_TOKEN")
	gates := 0
	svc := &Adapters{DB: db, Keyring: adapterKeyring(t, db), ModuleFactory: providerBlindTestModuleFactory(func(origin, credential string) (adapter.Module, func(), error) {
		if origin != "https://new.example" {
			t.Fatalf("noncanonical origin reached provider:%q", origin)
		}
		return fakeAdapterConfigureModule{gates: &gates}, nil, nil
	})}
	input := CreateAdapterRequest{Origin: "https://NEW.example:00443/", Credential: []byte("provider-token"), Target: AdapterTargetInput{EnvironmentID: "env_one", DestinationKind: "repository", DestinationOwner: "acme", DestinationName: "new", NamePrefix: "PROD_", KeyIDs: []string{"key_origin"}}}
	view, err := svc.Create(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, input)
	if err != nil {
		t.Fatal(err)
	}
	if view.Adapter.Origin != "https://new.example" {
		t.Fatalf("persisted origin=%q", view.Adapter.Origin)
	}
	input.Origin = "https://new.example/"
	if _, err := svc.Create(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, input); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate endpoint alias accepted:%v", err)
	}
}
