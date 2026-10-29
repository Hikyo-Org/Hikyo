package isolation

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/vaultkv"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
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

func loadKVServer(t *testing.T, name string) (kvServer, bool) {
	t.Helper()
	addr, token, caPath := os.Getenv("HIKYO_TEST_"+name+"_ADDR"), os.Getenv("HIKYO_TEST_"+name+"_TOKEN"), os.Getenv("HIKYO_TEST_"+name+"_CACERT")
	if addr == "" || token == "" || caPath == "" {
		if os.Getenv("HIKYO_TEST_KV_REQUIRED") == "1" {
			t.Fatalf("HIKYO_TEST_KV_REQUIRED=1 but HIKYO_TEST_%s_ADDR, _TOKEN and _CACERT are not all set", name)
		}
		t.Skipf("EXTERNAL %s KV v2 end-to-end SKIPPED LOUDLY: set HIKYO_TEST_%s_ADDR, _TOKEN and _CACERT", name, name)
		return kvServer{}, false
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
	if method == http.MethodPatch {
		req.Header.Set("Content-Type", "application/merge-patch+json")
	}
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
		Request:  adapter.SyncRequest{Config: adapter.Config{Origin: material.Origin}, Target: material.Target, Manifest: manifest, Ledger: material.Ledger, Completed: material.Completed},
		Revision: material.Revision,
		Release:  release,
	}, nil
}

func runVaultKVEndToEnd(t *testing.T, db *store.DB, server kvServer) {
	ctx := tctx(t)
	seedComposeCatalogue(t, db)
	mount := fmt.Sprintf("hikyo-e2e-%d", time.Now().UnixNano())
	server.must(t, http.MethodPost, "/v1/sys/mounts/"+mount, map[string]any{"type": "kv", "options": map[string]string{"version": "2"}})
	t.Cleanup(func() { server.do(t, http.MethodDelete, "/v1/sys/mounts/"+mount, nil) })
	policy := fmt.Sprintf(`path "%[1]s/data/*" { capabilities = ["create", "update"] }
path "%[1]s/delete/*" { capabilities = ["update"] }
path "%[1]s/metadata/*" { capabilities = ["patch", "read"] }
path "sys/internal/ui/mounts/%[1]s" { capabilities = ["read"] }`, mount)
	server.must(t, http.MethodPut, "/v1/sys/policies/acl/"+mount, map[string]string{"policy": policy})
	t.Cleanup(func() { server.do(t, http.MethodDelete, "/v1/sys/policies/acl/"+mount, nil) })
	auth := server.must(t, http.MethodPost, "/v1/auth/token/create", map[string]any{"policies": []string{mount}, "ttl": "30m", "no_default_policy": true})["auth"].(map[string]any)
	t.Cleanup(func() {
		server.must(t, http.MethodPost, "/v1/auth/token/revoke", map[string]any{"token": auth["client_token"]})
	})
	credential, _ := json.Marshal(map[string]string{"method": "token", "token": auth["client_token"].(string), "ca_pem": string(server.ca)})

	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_kv_manage','usr_alice','manage-adapters','`+cOrg+`','`+cPrj+`',NULL,`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_kv_reveal','usr_alice','reveal','`+cOrg+`','`+cPrj+`','`+cEnv+`',`+ts+`)`)
	kr := probeKeyring(t, db)
	var loseWriteResponse, failFinalize, failMetadata bool
	var metadataReads int
	build := func(origin, credential string) (adapter.Module, func(), error) {
		client, err := vaultkv.NewClient(vaultkv.ClientConfig{Origin: origin, Credential: credential, Deadline: 10 * time.Second,
			AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}})
		if err != nil {
			return nil, nil, err
		}
		return &vaultkv.Module{API: &kvFailureClient{API: client, loseResponse: &loseWriteResponse, failFinalize: &failFinalize, failMetadata: &failMetadata, metadataReads: &metadataReads}}, client.Forget, nil
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
	scope := domain.Scope{Org: cOrg, Project: cPrj}
	created, err := svc.Create(ctx, operator, scope, service.CreateAdapterRequest{
		Provider: string(adapter.VaultKVProvider), Origin: server.addr, Credential: credential,
		Target: service.AdapterTargetInput{
			EnvironmentID: cEnv, DestinationKind: string(adapter.Repository),
			DestinationOwner: mount, DestinationName: "hikyo/prod",
			KeySelection: &service.AdapterKeySelection{Include: []string{"DATABASE_URL"}},
		},
	})
	if err != nil {
		t.Fatalf("create vault-kv adapter: %v", err)
	}
	target := created.Targets[0]

	publisher := service.LocalPrincipal(domain.PrincipalID(cAdmin))
	envScope := domain.Scope{Org: cOrg, Project: cPrj, Env: cEnv}
	values := &service.Values{DB: db, Keyring: kr}
	revisions := &service.Revisions{DB: db, Keyring: kr}
	publish := func(plaintext string) {
		t.Helper()
		staged, err := values.Set(ctx, publisher, envScope, "DATABASE_URL", plaintext, nil)
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
	clock := time.Now().UTC()
	newWorker := func(id string) *adapter.Worker {
		runtime := store.NewAdapterRuntime(db, authorizePush)
		return &adapter.Worker{
			Store: runtime, ID: id,
			Loader: kvLoader{runtime: runtime, keyring: kr, build: func(credential string) (adapter.Module, func(), error) {
				return build(server.addr, credential)
			}},
			Now: func() time.Time { return clock }, Jitter: func(time.Duration) time.Duration { return 0 },
		}
	}
	worker := newWorker("kv-node-1")
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
	if got, ok := server.value(t, mount, "hikyo/prod/DATABASE_URL"); !ok || got != kvSecretOne {
		t.Fatalf("delivered %q, %v; want %q", got, ok, kvSecretOne)
	}
	if got, ok := server.value(t, mount, "hikyo/prod/MANAGED_BY_HIKYO"); !ok || got != adapter.SentinelName {
		t.Fatalf("sentinel = %q, %v", got, ok)
	}
	assertKVCLI(t, db, svc, target.ID, mount, auth["client_token"].(string))

	// Pause gates delivery. Resume catches up through a fresh worker instance.
	if _, err := svc.PauseTarget(ctx, operator, scope, target.ID); err != nil {
		t.Fatal(err)
	}
	publish(kvSecretTwo)
	drain()
	if got, _ := server.value(t, mount, "hikyo/prod/DATABASE_URL"); got != kvSecretOne {
		t.Fatal("paused target delivered a revision")
	}
	if _, err := svc.ResumeTarget(ctx, operator, scope, target.ID); err != nil {
		t.Fatal(err)
	}
	worker = newWorker("kv-node-2")
	drain()
	if got, _ := server.value(t, mount, "hikyo/prod/DATABASE_URL"); got != kvSecretTwo {
		t.Fatal("resumed target failed to catch up after worker restart")
	}

	// Lose an acknowledged provider response after the real CAS applies. The
	// durable outbox must hold custody across restart and explicit resync.
	loseWriteResponse = true
	publish(kvSecretOne)
	drain()
	if got := inspect(); got.LastErrorClass != adapter.ErrorClassProviderAmbiguous {
		t.Fatalf("lost response class = %q, want operator review", got.LastErrorClass)
	}
	metadata := func() map[string]any {
		return server.must(t, http.MethodGet, "/v1/"+mount+"/metadata/hikyo/prod/DATABASE_URL", nil)["data"].(map[string]any)
	}
	before := metadata()
	if before["custom_metadata"].(map[string]any)[vaultkv.PendingKey] == nil {
		t.Fatal("ambiguous write lacks pending metadata")
	}
	worker = newWorker("kv-node-3")
	if _, err := svc.SyncTarget(ctx, operator, scope, target.ID); err != nil {
		t.Fatal(err)
	}
	drain()
	if !reflect.DeepEqual(before, metadata()) {
		t.Fatal("restarted outbox inferred an unknown writer or changed held metadata")
	}
	if got := inspect(); got.LastErrorClass != adapter.ErrorClassProviderAmbiguous {
		t.Fatalf("restarted held target class = %q, want provider ambiguity needing attention", got.LastErrorClass)
	}
	recoverHeld := func() {
		t.Helper()
		beforeRecovery := metadata()
		plan, err := svc.Plan(ctx, operator, scope, target.ID)
		if err != nil {
			t.Fatal(err)
		}
		current := inspect()
		adopt := service.AdoptAdapterRequest{TargetID: target.ID, ArtifactID: plan.ArtifactID,
			ExpectedGeneration: current.Generation, ExpectedDestinationID: current.DestinationID,
			ExpectedRepositoryID: current.RepositoryID, Entries: []store.AdapterConflictEntry{{Surface: "variable", EffectiveName: "DATABASE_URL"}}}
		if _, err := svc.Adopt(ctx, operator, scope, adopt); err != nil {
			t.Fatalf("fresh version-bound operator recovery: %v", err)
		}
		drain()
		if got := inspect(); got.Health() != adapter.HealthConverged {
			t.Fatalf("adopted recovery: health %q class %q", got.Health(), got.LastErrorClass)
		}
		if metadata()["current_version"].(float64) != beforeRecovery["current_version"].(float64)+1 {
			t.Fatal("adoption did not create exactly one new CAS version")
		}
		if _, err := svc.Adopt(ctx, operator, scope, adopt); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("consumed adoption artifact replay = %v", err)
		}
	}
	recoverHeld()

	// Legacy Owned+pending metadata must stop all paths before mutation, not
	// only when the worker eventually reaches this key after the sentinel.
	pending := metadata()["current_version"].(float64) + 1
	server.must(t, http.MethodPatch, "/v1/"+mount+"/metadata/hikyo/prod/DATABASE_URL", map[string]any{"custom_metadata": map[string]string{vaultkv.PendingKey: fmt.Sprintf("%.0f", pending)}})
	assertHeldRetry := func() {
		t.Helper()
		beforeKey := metadata()
		beforeSentinel := server.must(t, http.MethodGet, "/v1/"+mount+"/metadata/hikyo/prod/MANAGED_BY_HIKYO", nil)["data"]
		worker = newWorker("kv-node-held-retry")
		if _, err := svc.SyncTarget(ctx, operator, scope, target.ID); err != nil {
			t.Fatal(err)
		}
		drain()
		afterSentinel := server.must(t, http.MethodGet, "/v1/"+mount+"/metadata/hikyo/prod/MANAGED_BY_HIKYO", nil)["data"]
		if got := inspect(); got.LastErrorClass != adapter.ErrorClassProviderAmbiguous || !reflect.DeepEqual(beforeKey, metadata()) || !reflect.DeepEqual(beforeSentinel, afterSentinel) {
			t.Fatal("held target retry changed metadata/history or lost attention status")
		}
	}
	assertHeldRetry()
	recoverHeld()
	for _, variant := range []string{"advanced", "malformed", "missing-marker"} {
		pending := metadata()["current_version"].(float64) + 1
		markers := map[string]any{vaultkv.PendingKey: fmt.Sprintf("%.0f", pending)}
		if variant == "malformed" {
			markers[vaultkv.PendingKey] = "unknown"
		}
		if variant == "missing-marker" {
			markers[vaultkv.MarkerKey] = nil
		}
		server.must(t, http.MethodPatch, "/v1/"+mount+"/metadata/hikyo/prod/DATABASE_URL", map[string]any{"custom_metadata": markers})
		if variant == "advanced" {
			// Another writer advances beyond the pending version. Neither
			// movement nor a malformed/removed marker resolves writer custody.
			for range 2 {
				server.must(t, http.MethodPost, "/v1/"+mount+"/data/hikyo/prod/DATABASE_URL", map[string]any{"data": map[string]string{"value": kvSecretTwo}})
			}
		}
		assertHeldRetry()
		recoverHeld()
	}

	// An acknowledged value write with refused metadata finalization keeps a
	// successful receipt and Dispatched custody. A new worker/job must hold
	// every path, including the already delivered sentinel, until adoption.
	failFinalize = true
	if _, err := svc.SyncTarget(ctx, operator, scope, target.ID); err != nil {
		t.Fatal(err)
	}
	drain()
	if outcome := queryString(t, db, `SELECT outcome FROM adapter_effects WHERE target_id='`+target.ID+`' AND effective_name='DATABASE_URL' ORDER BY created_at DESC,id DESC LIMIT 1`); outcome != "success" {
		t.Fatalf("acknowledged value receipt = %q", outcome)
	}
	if state := queryString(t, db, `SELECT state FROM adapter_ledger WHERE target_id='`+target.ID+`' AND normalized_name='DATABASE_URL'`); state != "dispatched" {
		t.Fatalf("incomplete metadata custody = %q", state)
	}
	assertHeldRetry()
	recoverHeld()

	// Fail the later key's inspection after the sentinel's acknowledged
	// write. A recreated runtime must resume this same job/input revision
	// from its durable successful prefix without re-delivering the sentinel.
	failMetadata, metadataReads = true, 0
	publish(kvSecretTwo)
	clock = clock.Add(time.Second)
	if worked, err := worker.RunOnce(ctx); !worked || err != nil {
		t.Fatalf("partial attempt: worked=%v err=%v", worked, err)
	}
	if failMetadata || inspect().LastErrorClass != adapter.ErrorClassNetwork {
		t.Fatal("partial fixture did not stop at the later metadata failure")
	}
	prefixVersion := server.must(t, http.MethodGet, "/v1/"+mount+"/metadata/hikyo/prod/MANAGED_BY_HIKYO", nil)["data"].(map[string]any)["current_version"]
	worker = newWorker("kv-node-partial-resume")
	drain()
	if got := inspect(); got.Health() != adapter.HealthConverged {
		t.Fatalf("partial resume did not converge: %q", got.LastErrorClass)
	}
	if after := server.must(t, http.MethodGet, "/v1/"+mount+"/metadata/hikyo/prod/MANAGED_BY_HIKYO", nil)["data"].(map[string]any)["current_version"]; after != prefixVersion {
		t.Fatal("same-job restart re-delivered an acknowledged prefix")
	}
	if value, _ := server.value(t, mount, "hikyo/prod/DATABASE_URL"); value != kvSecretTwo {
		t.Fatal("partial resume did not deliver the held input revision")
	}

	// A second target exercises retain through the service/outbox seam. It
	// releases custody without changing a single remote version or marker.
	retained, err := svc.AddTarget(ctx, operator, scope, created.Adapter.ID, service.AdapterTargetInput{
		EnvironmentID: cEnv, DestinationKind: string(adapter.Repository), DestinationOwner: mount,
		DestinationName: "hikyo/retain", KeySelection: &service.AdapterKeySelection{Include: []string{"DATABASE_URL"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SyncTarget(ctx, operator, scope, retained.ID); err != nil {
		t.Fatal(err)
	}
	drain()

	// An edit outside Hikyo: the next converge is a conflict, never an overwrite.
	server.must(t, http.MethodPost, "/v1/"+mount+"/data/hikyo/prod/DATABASE_URL", map[string]any{"data": map[string]string{"value": "hand-edited"}})
	publish(kvSecretOne)
	// A conflict retries with backoff until an operator acts, so run one
	// attempt rather than draining.
	clock = clock.Add(time.Second)
	if worked, err := worker.RunOnce(ctx); !worked || err != nil {
		t.Fatalf("conflicting attempt: worked=%v err=%v", worked, err)
	}
	if got := inspect(); got.LastErrorClass != adapter.ErrorClassConflict || got.Health() == adapter.HealthConverged {
		t.Fatalf("after external edit: health %q class %q, want a conflict", got.Health(), got.LastErrorClass)
	}
	if got, _ := server.value(t, mount, "hikyo/prod/DATABASE_URL"); got != "hand-edited" {
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
	if got, _ := server.value(t, mount, "hikyo/prod/DATABASE_URL"); got != "hand-edited" {
		t.Fatalf("removal pruned a path that moved outside Hikyo: %q", got)
	}
	meta := server.must(t, http.MethodGet, "/v1/"+mount+"/metadata/hikyo/prod/MANAGED_BY_HIKYO", nil)["data"].(map[string]any)
	if custom, _ := meta["custom_metadata"].(map[string]any); custom[vaultkv.MarkerKey] != target.ID {
		t.Fatalf("removal deleted metadata: %v", meta)
	}

	retainedMetadata := server.must(t, http.MethodGet, "/v1/"+mount+"/metadata/hikyo/retain/DATABASE_URL", nil)
	removed, err := svc.RemoveTarget(ctx, operator, scope, retained.ID, true)
	if err != nil || len(removed.Orphaned) == 0 {
		t.Fatalf("retain failed to name orphaned custody: %v", err)
	}
	drain()
	if after := server.must(t, http.MethodGet, "/v1/"+mount+"/metadata/hikyo/retain/DATABASE_URL", nil); !reflect.DeepEqual(retainedMetadata["data"], after["data"]) {
		t.Fatal("retain mutated provider version or metadata")
	}
	if got, ok := server.value(t, mount, "hikyo/retain/DATABASE_URL"); !ok || got != kvSecretOne {
		t.Fatal("retain removed the delivered value")
	}

	for _, plaintext := range []string{kvSecretOne, kvSecretTwo, auth["client_token"].(string)} {
		if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM audit_tenant_events WHERE payload LIKE '%%%s%%'`, plaintext)); got != 0 {
			t.Fatal("audit payload carries a delivered value or provider credential")
		}
	}
}

func TestAdapterVaultKVEndToEnd(t *testing.T) {
	for _, name := range []string{"VAULT", "OPENBAO"} {
		t.Run(name, func(t *testing.T) {
			server, ok := loadKVServer(t, name)
			if !ok {
				return
			}
			forEngines(t, func(t *testing.T, db *store.DB) { runVaultKVEndToEnd(t, db, server) })
		})
	}
}

type kvFailureClient struct {
	vaultkv.API
	loseResponse  *bool
	failFinalize  *bool
	failMetadata  *bool
	metadataReads *int
}

// Drive the actual CLI through the TLS API and its real authorization and
// service owners, against the target delivered to each real provider above.
func assertKVCLI(t *testing.T, db *store.DB, svc *service.Adapters, targetID, mount, providerToken string) {
	t.Helper()
	srv := httptest.NewTLSServer(server.New(&server.API{Auth: authService(t, db), Adapters: svc, Version: "kv-cli-fixture"}, nil))
	t.Cleanup(srv.Close)
	stateDir, workDir := t.TempDir(), t.TempDir()
	env := cli.Env{Getenv: func(key string) string {
		if key == "HIKYO_STATE_DIR" {
			return stateDir
		}
		return ""
	}}
	state, err := cli.NewState(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Trust().Put(cli.TrustEntry{Name: "kv-fixture", Origin: srv.URL, SPKIPin: cli.SPKIFingerprint(srv.Certificate())}); err != nil {
		t.Fatal(err)
	}
	if err := state.PutSession(cli.SessionArtifact{Instance: "kv-fixture", Origin: srv.URL, Token: sessionWithWindows(t, db, alice), Principal: string(alice)}); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{
		{"adapter", "target", "show", targetID, "--format", "workflow"},
		{"adapter", "target", "show", targetID, "-o", "json"},
		{"adapter", "plan", "--target", targetID, "-o", "json"},
	} {
		var stdout, stderr bytes.Buffer
		args := append(command, "--org", cOrg, "--project", cPrj, "--instance", "kv-fixture")
		code := cli.Run(t.Context(), cli.IO{Stdout: &stdout, Stderr: &stderr, Workdir: workDir, Env: env}, args)
		if code != cli.ExitOK {
			t.Fatalf("Vault CLI %s: exit %d: %s", command[1], code, stderr.String())
		}
		output := stdout.String() + stderr.String()
		for _, sensitive := range []string{kvSecretOne, kvSecretTwo, providerToken} {
			if strings.Contains(output, sensitive) {
				t.Fatal("Vault CLI exposed a value or provider credential")
			}
		}
		if command[len(command)-1] == "workflow" && !strings.Contains(output, mount+"/hikyo/prod/DATABASE_URL") {
			t.Fatal("Vault CLI omitted the names-only destination mapping")
		}
	}
}

func (c *kvFailureClient) WriteCAS(ctx context.Context, mount, path, value string, cas int64) (int64, error) {
	version, err := c.API.WriteCAS(ctx, mount, path, value, cas)
	if err == nil && *c.loseResponse && strings.HasSuffix(path, "/DATABASE_URL") {
		*c.loseResponse = false
		return 0, errors.New("synthetic response loss after real provider apply")
	}
	return version, err
}

func (c *kvFailureClient) PatchCustomMetadata(ctx context.Context, mount, path string, custom map[string]*string) error {
	if *c.failFinalize && custom[vaultkv.VersionKey] != nil && strings.HasSuffix(path, "/DATABASE_URL") {
		*c.failFinalize = false
		return &vaultkv.ResponseError{Status: http.StatusForbidden}
	}
	return c.API.PatchCustomMetadata(ctx, mount, path, custom)
}

func (c *kvFailureClient) ReadMetadata(ctx context.Context, mount, path string) (vaultkv.Metadata, error) {
	if *c.failMetadata && strings.HasSuffix(path, "/DATABASE_URL") {
		*c.metadataReads++
		if *c.metadataReads == 2 {
			*c.failMetadata = false
			return vaultkv.Metadata{}, &vaultkv.ResponseError{Status: http.StatusServiceUnavailable}
		}
	}
	return c.API.ReadMetadata(ctx, mount, path)
}
