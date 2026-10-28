package isolation

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/gitlab"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// This gate is set only by chart-network-policy.sh, in its disposable cluster.
// The test runs in a pod selected by the actual chart policy, on both engines.
// It exercises real service, sealed values, durable outbox/journal and GitLab
// wire code. Only the provider is synthetic; this is not GitLab acceptance.
func TestNetworkPolicySyntheticProviderCleanup(t *testing.T) {
	if os.Getenv("HIKYO_NETWORK_POLICY_FIXTURE") != "isolated" {
		t.Skip("run through scripts/ci/chart-kind.sh in its owned enforcing cluster")
	}
	cfg := gitLabE2EConfig(t)
	origin, err := url.Parse(cfg.origin)
	if err != nil || origin.Scheme != "https" || !strings.HasSuffix(origin.Hostname(), ".svc") || origin.Port() != "8443" || cfg.token != "synthetic-network-policy" {
		t.Fatal("network policy lifecycle requires the cluster-local synthetic provider")
	}
	forEngines(t, func(t *testing.T, db *store.DB) {
		execRaw(t, db, "INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('np_manage','usr_alice','manage-adapters','org_a','prj_a1',NULL,"+ts+")")
		execRaw(t, db, "INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('np_reveal','usr_alice','reveal','org_a','prj_a1','env_a1',"+ts+")")
		keyring := probeKeyring(t, db)
		build := func(config adapter.Config, credential string) (*adapter.ModuleLease, error) {
			client, err := gitlab.NewClient(gitlab.ClientConfig{Origin: config.Origin, Credential: credential, AllowedCIDRs: cfg.allowed, Deadline: 5 * time.Second, CABundlePEM: cfg.caBundle})
			if err != nil {
				return nil, err
			}
			return adapter.NewModuleLease(&gitlab.Module{API: client}, client.Forget)
		}
		svc := &service.Adapters{DB: db, Keyring: keyring, ModuleFactory: func(provider adapter.Provider, config adapter.Config, credential string) (*adapter.ModuleLease, error) {
			if provider != adapter.GitLabProvider {
				return nil, fmt.Errorf("unexpected provider %s", provider)
			}
			return build(config, credential)
		}}
		scope := domain.Scope{Org: orgA, Project: prjA1}
		created, err := svc.Create(t.Context(), service.LocalPrincipal(alice), scope, service.CreateAdapterRequest{
			Provider: string(adapter.GitLabProvider), Origin: cfg.origin, Credential: []byte(cfg.token), CABundlePEM: cfg.caBundle,
			Target: service.AdapterTargetInput{EnvironmentID: string(envA1), DestinationKind: string(adapter.Repository), DestinationOwner: "fixture", DestinationName: "project", NamePrefix: "NP_", KeyIDs: []string{keyA1}},
		})
		if err != nil {
			t.Fatal(err)
		}
		target := created.Targets[0]
		runtime := store.NewAdapterRuntime(db, func(ctx context.Context, job adapter.Job, _ adapter.Effect) error {
			return tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
				_, err := az.Authorize(ctx, authz.Identity{Principal: domain.PrincipalID(job.AuthorityPrincipal), Class: domain.ClassHuman}, authz.OpAdapterPush, domain.Scope{Org: domain.OrgID(job.OrgID), Project: domain.ProjectID(job.ProjectID), Env: domain.EnvID(job.EnvironmentID)})
				return err
			})
		})
		worker := &adapter.Worker{Store: runtime, Loader: networkPolicyLoader{runtime: runtime, keyring: keyring, build: build}, ID: "network-policy-worker", Jitter: func(time.Duration) time.Duration { return 0 }}
		drain := func() {
			t.Helper()
			for range 8 {
				worked, err := worker.RunOnce(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if !worked {
					return
				}
			}
			t.Fatal("synthetic provider outbox did not drain")
		}
		values := &service.Values{DB: db, Keyring: keyring}
		envScope := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}
		staged, err := values.Set(t.Context(), service.LocalPrincipal(custodian), envScope, "SHARED_KEY", "synthetic-owned-value", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := (&service.Revisions{DB: db, Keyring: keyring}).PublishPlanned(t.Context(), service.LocalPrincipal(custodian), envScope, service.PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
			t.Fatal(err)
		}
		drain()
		destination := adapter.Destination{Kind: adapter.Repository, NumericID: 42, Owner: "fixture", Name: "project"}
		if observed := gitLabObserve(t, cfg, destination, "NP_SHARED_KEY", "*"); observed.Value != "synthetic-owned-value" {
			t.Fatal("provider readback did not match published value")
		}
		if got := queryInt(t, db, fmt.Sprintf("SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state='owned'", target.ID)); got != 1 {
			t.Fatalf("owned ledger entries = %d", got)
		}
		// Scrub while the provider credential and destination remain usable.
		// Revoking that credential first would strand provider-side cleanup.
		if _, err := svc.RemoveTarget(t.Context(), service.LocalPrincipal(alice), scope, target.ID, false); err != nil {
			t.Fatal(err)
		}
		drain()
		if status, _ := gitLabGet(t, cfg, destination, "NP_SHARED_KEY", "*"); status != http.StatusNotFound {
			t.Fatalf("cleanup provider readback returned HTTP %d, want 404", status)
		}
		if got := queryInt(t, db, fmt.Sprintf("SELECT COUNT(*) FROM adapter_outbox WHERE target_id='%s' AND kind='scrub' AND state='succeeded'", target.ID)); got != 1 {
			t.Fatalf("successful cleanup jobs = %d", got)
		}
		if got := queryInt(t, db, fmt.Sprintf("SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state<>'released'", target.ID)); got != 0 {
			t.Fatal("cleanup retained provider custody")
		}
		if _, err := svc.RevokeCredential(t.Context(), service.LocalPrincipal(alice), scope, target.AdapterID); err != nil {
			t.Fatal(err)
		}
		if got := queryInt(t, db, fmt.Sprintf("SELECT COUNT(*) FROM adapters WHERE id='%s' AND credential_ciphertext IS NULL", target.AdapterID)); got != 1 {
			t.Fatal("provider credential was not retired")
		}
		t.Log("published value read back; cleanup deleted provider value and released durable custody; credential revoked")
	})
}

type networkPolicyLoader struct {
	runtime *store.AdapterRuntime
	keyring *crypto.Keyring
	build   func(adapter.Config, string) (*adapter.ModuleLease, error)
}

func (l networkPolicyLoader) Load(ctx context.Context, job adapter.Job, journal adapter.Journal) (adapter.LoadedSync, error) {
	if err := journal.Gate(ctx, adapter.Effect{Surface: adapter.Secret, EffectiveName: "manifest", Disposition: adapter.Update}); err != nil {
		return adapter.LoadedSync{}, err
	}
	material, err := l.runtime.LoadExecution(ctx, job)
	if err != nil {
		return adapter.LoadedSync{}, err
	}
	sealer, err := l.keyring.ForProject(ctx, job.OrgID, job.ProjectID)
	if err != nil {
		return adapter.LoadedSync{}, err
	}
	credential, err := sealer.OpenField(adapter.CredentialAAD(job.OrgID, job.ProjectID, material.CredentialOwnerID), material.CredentialCiphertext)
	if err != nil {
		return adapter.LoadedSync{}, err
	}
	lease, err := l.build(adapter.Config{Origin: material.Origin}, string(credential))
	crypto.Zero(credential)
	if err != nil {
		return adapter.LoadedSync{}, err
	}
	manifest := make([]adapter.ManifestEntry, 0, len(material.Entries))
	for _, row := range material.Entries {
		plain, err := sealer.OpenField(crypto.ProjectFieldAAD{OrgID: job.OrgID, ProjectID: job.ProjectID, OwnerTable: "snapshot_entries", OwnerRowID: row.ID, FieldTag: "snapshot_value", EnvironmentID: job.EnvironmentID, KeyID: row.KeyID, SnapshotID: row.SnapshotID}, row.Ciphertext)
		if err != nil {
			lease.Release()
			return adapter.LoadedSync{}, err
		}
		manifest = append(manifest, adapter.ManifestEntry{KeyID: row.KeyID, CanonicalName: row.KeyName, Classification: adapter.Classification(row.Classification), Value: string(plain)})
		crypto.Zero(plain)
	}
	return adapter.LoadedSync{Module: lease.Module, Revision: material.Revision, Release: lease.Release, Request: adapter.SyncRequest{Config: adapter.Config{Origin: material.Origin}, Target: material.Target, Manifest: manifest, Ledger: material.Ledger}}, nil
}
