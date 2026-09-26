package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// #161: the cloudflare provider and its two destination kinds are accepted by
// both engines' CHECK constraints, while cross-provider kinds and malformed
// Pages environments are refused before any row is written.
func runCloudflareTargets(t *testing.T, db *store.DB) {
	seedAdapterBase(t, db)
	ctx := t.Context()
	scope := domain.Scope{Org: "org_744", Project: "prj_744"}
	const account = "0123456789abcdef0123456789abcdef"

	create := func(adapterID, provider, origin string, target store.AdapterTargetMutation) (store.AdapterTarget, error) {
		var out store.AdapterTarget
		err := storetx.Write(ctx, db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
			p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_744"}, authz.OpAdapterConfigure, scope)
			if err != nil {
				return err
			}
			target.AdapterID, target.EnvironmentID, target.KeyIDs = adapterID, "env_744", []string{"key_744"}
			_, out, err = repos.Adapters().Create(ctx, p, store.AdapterCreate{
				ID: adapterID, Provider: provider, Origin: origin,
				CredentialCiphertext: []byte("provider-token"), AuthorityPrincipalID: "usr_744",
				Target: target, At: time.Now().UTC(),
			})
			return err
		})
		return out, err
	}

	refusals := map[string]struct {
		provider string
		target   store.AdapterTargetMutation
	}{
		"github kind on cloudflare":  {"cloudflare", store.AdapterTargetMutation{ID: "tgt_r1", DestinationKind: "repository", DestinationOwner: account, DestinationName: "api", DestinationID: 7}},
		"cloudflare kind on forgejo": {"forgejo", store.AdapterTargetMutation{ID: "tgt_r2", DestinationKind: "workers-script", DestinationOwner: account, DestinationName: "api", DestinationID: 7}},
		"pages without environment":  {"cloudflare", store.AdapterTargetMutation{ID: "tgt_r3", DestinationKind: "pages-project", DestinationOwner: account, DestinationName: "site", DestinationID: 7}},
		"workers with environment":   {"cloudflare", store.AdapterTargetMutation{ID: "tgt_r4", DestinationKind: "workers-script", DestinationOwner: account, DestinationName: "api", DestinationEnvironment: "production", DestinationID: 7}},
		"unknown provider":           {"cloudflare-pages", store.AdapterTargetMutation{ID: "tgt_r5", DestinationKind: "workers-script", DestinationOwner: account, DestinationName: "api", DestinationID: 7}},
	}
	for name, tt := range refusals {
		if _, err := create("adp_"+name[:4], tt.provider, "https://refused."+name[:4], tt.target); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: Create() = %v, want ErrInvalid", name, err)
		}
	}

	target, err := create("adp_cf", "cloudflare", "https://api.cloudflare.com", store.AdapterTargetMutation{
		ID: "tgt_pages", DestinationKind: "pages-project", DestinationOwner: account,
		DestinationName: "site", DestinationEnvironment: "preview", DestinationID: 99, NamePrefix: "",
	})
	if err != nil {
		t.Fatalf("create Pages target: %v", err)
	}
	if target.Provider != "cloudflare" || target.DestinationKind != "pages-project" || target.DestinationEnvironment != "preview" {
		t.Fatalf("target = %+v", target)
	}
}

func TestCloudflareTargetsSQLite(t *testing.T) {
	runCloudflareTargets(t, openKeyTestDB(t, store.Config{Engine: store.EngineSQLite, Path: t.TempDir() + "/cloudflare.db"}))
}

func TestCloudflareTargetsPostgres(t *testing.T) {
	runCloudflareTargets(t, postgresTestDB(t))
}
