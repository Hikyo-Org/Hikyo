package isolation

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/vaultkv"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Vault/OpenBao KV v2 delivery (#162), end to end through the real service,
// store, outbox runtime, worker and HTTP client against a real dev server:
// publish delivers byte-exact plaintext with check-and-set, an edit made
// outside Hikyo turns the next converge into a conflict instead of an
// overwrite, removal soft-deletes without destroying history, and no audit
// payload carries plaintext. The server comes from the same variables as the
// adapter contract (HIKYO_TEST_VAULT_* or HIKYO_TEST_OPENBAO_*).

const (
	kvSecretOne = "kv-plaintext-revision-one"
	kvSecretTwo = "kv-plaintext-revision-two"
)

type kvServer struct {
	addr, token string
	ca          []byte
	http        *http.Client
}

func loadKVServer(t *testing.T) (kvServer, bool) {
	t.Helper()
	for _, name := range []string{"VAULT", "OPENBAO"} {
		addr, token, caPath := os.Getenv("HIKYO_TEST_"+name+"_ADDR"), os.Getenv("HIKYO_TEST_"+name+"_TOKEN"), os.Getenv("HIKYO_TEST_"+name+"_CACERT")
		if addr == "" || token == "" || caPath == "" {
			continue
		}
		ca, err := os.ReadFile(caPath)
		if err != nil {
			t.Fatal(err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			t.Fatalf("%s CA holds no certificate", name)
		}
		return kvServer{addr: strings.TrimSuffix(addr, "/"), token: token, ca: ca, http: &http.Client{
			Timeout:   10 * time.Second,
			Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}},
		}}, true
	}
	if os.Getenv("HIKYO_TEST_KV_REQUIRED") == "1" {
		t.Fatal("HIKYO_TEST_KV_REQUIRED=1 but no HIKYO_TEST_VAULT_* or HIKYO_TEST_OPENBAO_* server is configured")
	}
	t.Skip("EXTERNAL KV v2 end-to-end SKIPPED LOUDLY: set HIKYO_TEST_VAULT_ADDR, _TOKEN and _CACERT")
	return kvServer{}, false
}

func (s kvServer) do(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var input io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		input = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, s.addr+path, input)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Vault-Token", s.token)
	resp, err := s.http.Do(req)
	if err != nil {
		t.Fatalf("harness %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func (s kvServer) must(t *testing.T, method, path string, body any) map[string]any {
	t.Helper()
	status, out := s.do(t, method, path, body)
	if status < 200 || status >= 300 {
		t.Fatalf("harness %s %s = %d %v", method, path, status, out["errors"])
	}
	return out
}

func (s kvServer) value(t *testing.T, mount, path string) (string, bool) {
	t.Helper()
	status, out := s.do(t, http.MethodGet, "/v1/"+mount+"/data/"+path, nil)
	if status == http.StatusNotFound {
		return "", false
	}
	data, _ := out["data"].(map[string]any)
	values, _ := data["data"].(map[string]any)
	if values == nil {
		return "", false
	}
	value, ok := values["value"].(string)
	return value, ok
}

// kvLoader mirrors the production loader: it opens the credential and each
// snapshot value with the project keyring and hands plaintext to the module
// only for the lifetime of one Sync.
type kvLoader struct {
	runtime *store.AdapterRuntime
	keyring *crypto.Keyring
	build   func(credential string) (adapter.Module, func(), error)
}

func (l kvLoader) Load(ctx context.Context, job adapter.Job, journal adapter.Journal) (adapter.LoadedSync, error) {
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
	module, release, err := l.build(string(credential))
	crypto.Zero(credential)
	if err != nil {
		return adapter.LoadedSync{}, err
	}
	manifest := make([]adapter.ManifestEntry, 0, len(material.Entries))
	for _, row := range material.Entries {
		plain, err := sealer.OpenField(crypto.ProjectFieldAAD{
			OrgID: job.OrgID, ProjectID: job.ProjectID,
			OwnerTable: "snapshot_entries", OwnerRowID: row.ID, FieldTag: "snapshot_value",
			EnvironmentID: job.EnvironmentID, KeyID: row.KeyID, SnapshotID: row.SnapshotID,
		}, row.Ciphertext)
		if err != nil {
			release()
			return adapter.LoadedSync{}, err
		}
		manifest = append(manifest, adapter.ManifestEntry{KeyID: row.KeyID, CanonicalName: row.KeyName, Classification: adapter.Classification(row.Classification), Value: string(plain)})
		crypto.Zero(plain)
	}
	return adapter.LoadedSync{
		Module:   module,
		Request:  adapter.SyncRequest{Config: adapter.Config{Origin: material.Origin}, Target: material.Target, Manifest: manifest, Ledger: material.Ledger},
		Revision: material.Revision,
		Release:  release,
	}, nil
}

func runVaultKVEndToEnd(t *testing.T, db *store.DB) {
	server, ok := loadKVServer(t)
	if !ok {
		return
	}
	ctx := tctx(t)
	mount := fmt.Sprintf("hikyo-e2e-%d", time.Now().UnixNano())
	server.must(t, http.MethodPost, "/v1/sys/mounts/"+mount, map[string]any{"type": "kv", "options": map[string]string{"version": "2"}})
	t.Cleanup(func() { server.do(t, http.MethodDelete, "/v1/sys/mounts/"+mount, nil) })
	policy := fmt.Sprintf(`path "%[1]s/data/*" { capabilities = ["create", "update", "delete"] }
path "%[1]s/metadata/*" { capabilities = ["patch", "read"] }
path "sys/internal/ui/mounts/%[1]s" { capabilities = ["read"] }`, mount)
	server.must(t, http.MethodPut, "/v1/sys/policies/acl/"+mount, map[string]string{"policy": policy})
	t.Cleanup(func() { server.do(t, http.MethodDelete, "/v1/sys/policies/acl/"+mount, nil) })
	auth := server.must(t, http.MethodPost, "/v1/auth/token/create", map[string]any{"policies": []string{mount}, "ttl": "30m", "no_default_policy": true})["auth"].(map[string]any)
	credential, _ := json.Marshal(map[string]string{"method": "token", "token": auth["client_token"].(string), "ca_pem": string(server.ca)})

	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_kv_manage','usr_alice','manage-adapters','org_a','prj_a1',NULL,`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_kv_reveal','usr_alice','reveal','org_a','prj_a1','env_a1',`+ts+`)`)
	kr := probeKeyring(t, db)
	build := func(origin, credential string) (adapter.Module, func(), error) {
		client, err := vaultkv.NewClient(vaultkv.ClientConfig{Origin: origin, Credential: credential, Deadline: 10 * time.Second,
			AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}})
		if err != nil {
			return nil, nil, err
		}
		return &vaultkv.Module{API: client}, client.Forget, nil
	}
	svc := &service.Adapters{DB: db, Keyring: kr, ModuleFactory: func(provider adapter.Provider, config adapter.Config, credential string) (*adapter.ModuleLease, error) {
		if provider != adapter.VaultKVProvider {
			return nil, fmt.Errorf("unexpected provider %s", provider)
		}
		module, release, err := build(config.Origin, credential)
		if err != nil {
			return nil, err
		}
		return adapter.NewModuleLease(module, release)
	}}
	operator := service.LocalPrincipal(alice)
	scope := domain.Scope{Org: orgA, Project: prjA1}
	created, err := svc.Create(ctx, operator, scope, service.CreateAdapterRequest{
		Provider: string(adapter.VaultKVProvider), Origin: server.addr, Credential: credential,
		Target: service.AdapterTargetInput{
			EnvironmentID: string(envA1), DestinationKind: string(adapter.Repository),
			DestinationOwner: mount, DestinationName: "hikyo/prod",
			KeySelection: &service.AdapterKeySelection{Include: []string{"SHARED_*"}},
		},
	})
	if err != nil {
		t.Fatalf("create vault-kv adapter: %v", err)
	}
	target := created.Targets[0]

	publisher := service.LocalPrincipal(custodian)
	envScope := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}
	values := &service.Values{DB: db, Keyring: kr}
	revisions := &service.Revisions{DB: db, Keyring: kr}
	publish := func(plaintext string) {
		t.Helper()
		staged, err := values.Set(ctx, publisher, envScope, "SHARED_KEY", plaintext, nil)
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		if _, err := revisions.PublishPlanned(ctx, publisher, envScope, service.PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	authorizePush := func(ctx context.Context, job adapter.Job, _ adapter.Effect) error {
		return tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			_, err := az.Authorize(ctx, authz.Identity{Principal: domain.PrincipalID(job.AuthorityPrincipal), Class: domain.ClassHuman}, authz.OpAdapterPush, domain.Scope{
				Org: domain.OrgID(job.OrgID), Project: domain.ProjectID(job.ProjectID), Env: domain.EnvID(job.EnvironmentID),
			})
			return err
		})
	}
	runtime := store.NewAdapterRuntime(db, authorizePush)
	clock := time.Now().UTC()
	worker := &adapter.Worker{
		Store: runtime, ID: "kv-node-1",
		Loader: kvLoader{runtime: runtime, keyring: kr, build: func(credential string) (adapter.Module, func(), error) {
			return build(server.addr, credential)
		}},
		Now: func() time.Time { return clock }, Jitter: func(time.Duration) time.Duration { return 0 },
	}
	drain := func() {
		t.Helper()
		for range 8 {
			clock = clock.Add(time.Second)
			worked, err := worker.RunOnce(ctx)
			if err != nil {
				t.Fatalf("worker: %v", err)
			}
			if !worked {
				return
			}
		}
		t.Fatal("worker never drained the outbox")
	}
	inspect := func() store.AdapterTarget {
		t.Helper()
		view, err := svc.InspectTarget(ctx, operator, scope, target.ID)
		if err != nil {
			t.Fatal(err)
		}
		return view.Target
	}

	publish(kvSecretOne)
	drain()
	if got := inspect(); got.Health() != adapter.HealthConverged || got.ConvergedRevision == nil {
		t.Fatalf("after first publish: health %q class %q", got.Health(), got.LastErrorClass)
	}
	if got, ok := server.value(t, mount, "hikyo/prod/SHARED_KEY"); !ok || got != kvSecretOne {
		t.Fatalf("delivered %q, %v; want %q", got, ok, kvSecretOne)
	}
	if got, ok := server.value(t, mount, "hikyo/prod/MANAGED_BY_HIKYO"); !ok || got != adapter.SentinelName {
		t.Fatalf("sentinel = %q, %v", got, ok)
	}

	// An edit outside Hikyo: the next converge is a conflict, never an overwrite.
	server.must(t, http.MethodPost, "/v1/"+mount+"/data/hikyo/prod/SHARED_KEY", map[string]any{"data": map[string]string{"value": "hand-edited"}})
	publish(kvSecretTwo)
	// A conflict retries with backoff until an operator acts, so run one
	// attempt rather than draining.
	clock = clock.Add(time.Second)
	if worked, err := worker.RunOnce(ctx); !worked || err != nil {
		t.Fatalf("conflicting attempt: worked=%v err=%v", worked, err)
	}
	if got := inspect(); got.LastErrorClass != adapter.ErrorClassConflict || got.Health() == adapter.HealthConverged {
		t.Fatalf("after external edit: health %q class %q, want a conflict", got.Health(), got.LastErrorClass)
	}
	if got, _ := server.value(t, mount, "hikyo/prod/SHARED_KEY"); got != "hand-edited" {
		t.Fatalf("external edit overwritten with %q", got)
	}

	// Removal soft-deletes Hikyo's sentinel, releases the moved key without
	// touching it, and never destroys history or metadata.
	if _, err := svc.RemoveTarget(ctx, operator, scope, target.ID, false); err != nil {
		t.Fatalf("remove target: %v", err)
	}
	drain()
	if _, ok := server.value(t, mount, "hikyo/prod/MANAGED_BY_HIKYO"); ok {
		t.Fatal("sentinel still readable after removal")
	}
	if got, _ := server.value(t, mount, "hikyo/prod/SHARED_KEY"); got != "hand-edited" {
		t.Fatalf("removal pruned a path that moved outside Hikyo: %q", got)
	}
	meta := server.must(t, http.MethodGet, "/v1/"+mount+"/metadata/hikyo/prod/MANAGED_BY_HIKYO", nil)["data"].(map[string]any)
	if custom, _ := meta["custom_metadata"].(map[string]any); custom[vaultkv.MarkerKey] != target.ID {
		t.Fatalf("removal deleted metadata: %v", meta)
	}

	for _, plaintext := range []string{kvSecretOne, kvSecretTwo} {
		if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM audit_tenant_events WHERE payload LIKE '%%%s%%'`, plaintext)); got != 0 {
			t.Fatalf("audit payload carries value plaintext %q", plaintext)
		}
	}
}

func TestAdapterVaultKVEndToEnd(t *testing.T) {
	forEngines(t, runVaultKVEndToEnd)
}
