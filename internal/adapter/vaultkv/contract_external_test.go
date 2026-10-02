package vaultkv

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/netip"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

// The contract runs the real adapter against current Vault and OpenBao dev
// servers. Each server is configured by three variables:
//
//	HIKYO_TEST_VAULT_ADDR / HIKYO_TEST_VAULT_TOKEN / HIKYO_TEST_VAULT_CACERT
//	HIKYO_TEST_OPENBAO_ADDR / HIKYO_TEST_OPENBAO_TOKEN / HIKYO_TEST_OPENBAO_CACERT
//
// The token is a harness root token used only to build the fixture; the
// adapter itself runs on an AppRole whose policy DENIES every read of the data
// path, so a passing contract proves the adapter needs no value read. Locally
// an absent server skips loudly; with HIKYO_TEST_KV_REQUIRED=1 (CI) it fails.
func TestVaultContract(t *testing.T)   { runKVContract(t, "VAULT") }
func TestOpenBaoContract(t *testing.T) { runKVContract(t, "OPENBAO") }

type kvHarness struct {
	t      *testing.T
	addr   string
	token  string
	caPEM  string
	client *http.Client
	ns     string
}

func loadKVHarness(t *testing.T, name string) (*kvHarness, bool) {
	t.Helper()
	addr, token, caPath := os.Getenv("HIKYO_TEST_"+name+"_ADDR"), os.Getenv("HIKYO_TEST_"+name+"_TOKEN"), os.Getenv("HIKYO_TEST_"+name+"_CACERT")
	if addr == "" || token == "" || caPath == "" {
		if os.Getenv("HIKYO_TEST_KV_REQUIRED") == "1" {
			t.Fatalf("HIKYO_TEST_KV_REQUIRED=1 but HIKYO_TEST_%s_ADDR, _TOKEN and _CACERT are not all set", name)
		}
		t.Skipf("EXTERNAL %s KV v2 contract SKIPPED LOUDLY: set HIKYO_TEST_%s_ADDR, _TOKEN and _CACERT", name, name)
		return nil, false
	}
	ca, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatalf("read %s CA: %v", name, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		t.Fatalf("%s CA holds no certificate", name)
	}
	return &kvHarness{t: t, addr: strings.TrimSuffix(addr, "/"), token: token, caPEM: string(ca), client: &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}},
	}}, true
}

func (h *kvHarness) do(method, path string, body any) (int, map[string]any) {
	h.t.Helper()
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshal harness request: %v", err)
		}
		input = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, h.addr+path, input)
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("X-Vault-Token", h.token)
	if h.ns != "" {
		req.Header.Set("X-Vault-Namespace", h.ns)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatalf("harness %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func (h *kvHarness) must(method, path string, body any) map[string]any {
	h.t.Helper()
	status, out := h.do(method, path, body)
	if status < 200 || status >= 300 {
		h.t.Fatalf("harness %s %s = %d %v", method, path, status, out["errors"])
	}
	return out
}

func (h *kvHarness) readValue(mount, path string) (string, bool) {
	h.t.Helper()
	status, out := h.do(http.MethodGet, "/v1/"+mount+"/data/"+path, nil)
	if status == http.StatusNotFound {
		return "", false
	}
	if status != http.StatusOK {
		h.t.Fatalf("harness read %s = %d", path, status)
	}
	data, _ := out["data"].(map[string]any)
	values, _ := data["data"].(map[string]any)
	if values == nil {
		return "", false
	}
	value, _ := values["value"].(string)
	return value, true
}

func randomSuffix(t *testing.T) string {
	raw := make([]byte, 5)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(raw)
}

// fixture mounts a fresh KV v2 engine and an AppRole whose policy can write,
// patch, and soft-delete but never read data. It returns the adapter
// credential JSON.
func (h *kvHarness) fixture(mount string) string {
	h.t.Helper()
	h.must(http.MethodPost, "/v1/sys/mounts/"+mount, map[string]any{"type": "kv", "options": map[string]string{"version": "2"}})
	h.t.Cleanup(func() { h.do(http.MethodDelete, "/v1/sys/mounts/"+mount, nil) })
	policy := fmt.Sprintf(`
path "%[1]s/data/*"     { capabilities = ["create", "update"] }
path "%[1]s/delete/*"     { capabilities = ["update"] }
path "%[1]s/metadata/*" { capabilities = ["patch", "read"] }
path "sys/internal/ui/mounts/%[1]s" { capabilities = ["read"] }
`, mount)
	policyName := "hikyo-" + mount
	h.must(http.MethodPut, "/v1/sys/policies/acl/"+policyName, map[string]string{"policy": policy})
	h.t.Cleanup(func() { h.do(http.MethodDelete, "/v1/sys/policies/acl/"+policyName, nil) })
	authMount := "approle-" + mount
	h.must(http.MethodPost, "/v1/sys/auth/"+authMount, map[string]string{"type": "approle"})
	h.t.Cleanup(func() { h.do(http.MethodDelete, "/v1/sys/auth/"+authMount, nil) })
	h.must(http.MethodPost, "/v1/auth/"+authMount+"/role/hikyo", map[string]any{"token_policies": []string{policyName}, "token_ttl": "10m", "token_max_ttl": "30m"})
	roleID := h.must(http.MethodGet, "/v1/auth/"+authMount+"/role/hikyo/role-id", nil)["data"].(map[string]any)["role_id"].(string)
	secretID := h.must(http.MethodPost, "/v1/auth/"+authMount+"/role/hikyo/secret-id", map[string]any{})["data"].(map[string]any)["secret_id"].(string)
	credential, err := json.Marshal(map[string]string{"method": "approle", "role_id": roleID, "secret_id": secretID, "mount": authMount, "ca_pem": h.caPEM})
	if err != nil {
		h.t.Fatalf("marshal harness credential: %v", err)
	}
	return string(credential)
}

func (h *kvHarness) adapterClient(credential string) *Client {
	h.t.Helper()
	origin := h.addr
	if h.ns != "" {
		origin += "/" + h.ns
	}
	client, err := NewClient(ClientConfig{
		Origin: origin, Credential: credential, Deadline: 10 * time.Second,
		AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
	})
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(client.Forget)
	return client
}

// ambiguousKV applies one write and then reports a transport failure.
type ambiguousKV struct {
	API
	path string
}

func (a *ambiguousKV) WriteCAS(ctx context.Context, mount, path, value string, cas int64) (int64, error) {
	version, err := a.API.WriteCAS(ctx, mount, path, value, cas)
	if err == nil && path == a.path {
		a.path = ""
		return 0, errors.New("connection reset after apply")
	}
	return version, err
}

func runKVContract(t *testing.T, name string) {
	h, ok := loadKVHarness(t, name)
	if !ok {
		return
	}
	mount := "hikyo-e2e-" + randomSuffix(t)
	client := h.adapterClient(h.fixture(mount))
	module := &Module{API: client}
	gate := func(context.Context) error { return nil }

	// The adapter's own token cannot read data: the server itself enforces the
	// no-value-read closure, not just the client.
	token, err := client.authorize(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	probe := &kvHarness{t: t, addr: h.addr, token: token, client: h.client}
	h.must(http.MethodPost, "/v1/"+mount+"/data/probe", map[string]any{"data": map[string]string{"value": "probe"}})
	if status, _ := probe.do(http.MethodGet, "/v1/"+mount+"/data/probe", nil); status != http.StatusForbidden {
		t.Fatalf("adapter token data read = %d, want 403 from the least-privilege policy", status)
	}

	destination := adapter.Destination{Kind: adapter.Repository, Owner: mount, Name: "apps/pay"}
	connection, err := module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: destination, Gate: gate})
	if err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if connection.Version == "" || connection.DestinationID <= 0 {
		t.Fatalf("connection = %+v", connection)
	}
	destination.NumericID = connection.DestinationID
	target := adapter.Target{ID: "tgt_contract", Destination: destination, Generation: 1}
	journal := newFakeJournal()
	entries := []adapter.ManifestEntry{
		{KeyID: "key_db", CanonicalName: "DATABASE_URL", Classification: adapter.SecretClassification, Value: "postgres://one\nline two ✓"},
		{KeyID: "key_log", CanonicalName: "LOG_LEVEL", Classification: adapter.ConfigClassification, Value: "debug"},
	}

	// Create.
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: entries}, journal); err != nil {
		t.Fatalf("create sync: %v", err)
	}
	for path, want := range map[string]string{"apps/pay/DATABASE_URL": entries[0].Value, "apps/pay/LOG_LEVEL": "debug", "apps/pay/MANAGED_BY_HIKYO": adapter.SentinelName} {
		if got, ok := h.readValue(mount, path); !ok || got != want {
			t.Fatalf("%s = %q, %v; want byte-exact %q", path, got, ok, want)
		}
	}

	// Plan is value-blind and sees everything owned.
	plan, err := module.Plan(t.Context(), adapter.PlanRequest{Target: target, Manifest: entries, Ledger: journal.ledger(), Gate: gate})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range plan.Changes {
		if change.Disposition != adapter.Update {
			t.Fatalf("plan after create = %+v", plan.Changes)
		}
	}

	// Ambiguous outcome: the write applies but the response is lost. Matching
	// pending metadata does not establish its writer: sync and teardown must
	// retain custody and stop until a fresh version-bound operator decision.
	entries[0].Value = "postgres://two"
	ambiguous := &Module{API: &ambiguousKV{API: client, path: "apps/pay/DATABASE_URL"}}
	if _, err := ambiguous.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: entries, Ledger: journal.ledger()}, journal); !errors.Is(err, adapter.ErrIndeterminate) || !errors.Is(err, adapter.ErrOperatorReview) {
		t.Fatalf("ambiguous sync = %v, want indeterminate and operator review", err)
	}
	if journal.states["secret:DATABASE_URL"] != adapter.Dispatched {
		t.Fatal("unknown response did not retain dispatched custody")
	}
	paths := []string{"apps/pay/DATABASE_URL", "apps/pay/LOG_LEVEL", "apps/pay/MANAGED_BY_HIKYO"}
	metadataBefore := make(map[string]Metadata, len(paths))
	valuesBefore := make(map[string]string, len(paths))
	for _, path := range paths {
		metadata, err := client.ReadMetadata(t.Context(), mount, path)
		if err != nil {
			t.Fatal(err)
		}
		metadataBefore[path] = metadata
		value, exists := h.readValue(mount, path)
		if !exists {
			t.Fatalf("acknowledged fixture path %s missing", path)
		}
		valuesBefore[path] = value
	}
	if valuesBefore[paths[0]] != "postgres://two" || metadataBefore[paths[0]].CustomMetadata[PendingKey] == "" {
		t.Fatal("fixture lacks the applied but unacknowledged pending write")
	}
	journalBefore := *journal
	journalBefore.states = maps.Clone(journal.states)
	journalBefore.missing = maps.Clone(journal.missing)
	journalBefore.releases = maps.Clone(journal.releases)
	journalBefore.conflicts = slices.Clone(journal.conflicts)
	journalBefore.outcomes = slices.Clone(journal.outcomes)
	for _, teardown := range []bool{false, true} {
		if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: entries, Ledger: journal.ledger(), Teardown: teardown}, journal); !errors.Is(err, adapter.ErrOperatorReview) {
			t.Fatalf("held replay teardown=%v: %v; want operator review", teardown, err)
		}
		for _, path := range paths {
			metadata, err := client.ReadMetadata(t.Context(), mount, path)
			if err != nil {
				t.Fatal(err)
			}
			value, exists := h.readValue(mount, path)
			if !exists || value != valuesBefore[path] || !reflect.DeepEqual(metadata, metadataBefore[path]) {
				t.Fatalf("held replay changed value/version/history/markers of %s", path)
			}
		}
		if !reflect.DeepEqual(*journal, journalBefore) {
			t.Fatal("held replay changed journal outcomes or custody")
		}
	}

	plan, err = module.Plan(t.Context(), adapter.PlanRequest{Target: target, Manifest: entries, Ledger: journal.ledger(), Gate: gate})
	if err != nil {
		t.Fatalf("fresh recovery plan: %v", err)
	}
	var witness int64
	for _, change := range plan.Changes {
		if change.EffectiveName == "DATABASE_URL" {
			if change.Disposition != adapter.Conflict || change.ObservedProviderVersion == nil || *change.ObservedProviderVersion <= 0 {
				t.Fatalf("held path lacks positive version-bound conflict: %+v", change)
			}
			witness = *change.ObservedProviderVersion
		}
	}
	if witness != metadataBefore[paths[0]].CurrentVersion {
		t.Fatalf("recovery plan witness=%d; want exact observed current version", witness)
	}
	// Model the sanctioned adoption seam: explicit operator consent changes
	// only this held name to Owned, binds the positive plan witness, and advances
	// the target generation. It does not prove who produced the old version.
	journal.states["secret:DATABASE_URL"] = adapter.Owned
	target.Generation++
	adoptedLedger := journal.ledger()
	for i := range adoptedLedger {
		if adoptedLedger[i].EffectiveName == "DATABASE_URL" {
			adoptedLedger[i].AdoptionPending = true
			adoptedLedger[i].AdoptionVersion = &witness
		}
	}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: entries, Ledger: adoptedLedger}, journal); err != nil {
		t.Fatalf("explicit fresh version-bound recovery: %v", err)
	}
	recovered, err := client.ReadMetadata(t.Context(), mount, paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if recovered.CurrentVersion != witness+1 || recovered.CustomMetadata[VersionKey] != fmt.Sprint(witness+1) || recovered.CustomMetadata[PendingKey] != "" || journal.states["secret:DATABASE_URL"] != adapter.Owned {
		t.Fatal("recovery did not perform a new acknowledged CAS and retain ownership")
	}
	if got, ok := h.readValue(mount, paths[0]); !ok || got != "postgres://two" {
		t.Fatal("explicit recovery changed delivered plaintext")
	}
	// Acknowledged custody remains eligible for normal convergence. As the
	// accepted no-value-read contract requires, re-delivery creates a new version.
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: entries, Ledger: journal.ledger()}, journal); err != nil {
		t.Fatalf("acknowledged replay: %v", err)
	}
	acknowledged, err := client.ReadMetadata(t.Context(), mount, paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if acknowledged.CurrentVersion != recovered.CurrentVersion+1 || acknowledged.CustomMetadata[VersionKey] != fmt.Sprint(acknowledged.CurrentVersion) || acknowledged.CustomMetadata[PendingKey] != "" || journal.states["secret:DATABASE_URL"] != adapter.Owned {
		t.Fatal("acknowledged replay lost version/custody")
	}
	if got, ok := h.readValue(mount, paths[0]); !ok || got != "postgres://two" {
		t.Fatal("acknowledged replay changed delivered plaintext")
	}

	// External movement is a CAS conflict, never an overwrite.
	h.must(http.MethodPost, "/v1/"+mount+"/data/apps/pay/LOG_LEVEL", map[string]any{"data": map[string]string{"value": "hand-edited"}})
	result, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: entries, Ledger: journal.ledger()}, journal)
	if !errors.Is(err, adapter.ErrConflict) || len(result.Conflicts) != 1 {
		t.Fatalf("sync over external edit = %+v, %v; want conflict", result, err)
	}
	if got, _ := h.readValue(mount, "apps/pay/LOG_LEVEL"); got != "hand-edited" {
		t.Fatalf("external edit overwritten with %q", got)
	}

	// An unowned pre-existing path refuses without mutation.
	h.must(http.MethodPost, "/v1/"+mount+"/data/apps/pay/FOREIGN", map[string]any{"data": map[string]string{"value": "theirs"}})
	foreign := []adapter.ManifestEntry{{KeyID: "key_f", CanonicalName: "FOREIGN", Classification: adapter.SecretClassification, Value: "ours"}}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: append(slices.Clone(entries[:1]), foreign...), Ledger: journal.ledger()}, journal); !errors.Is(err, adapter.ErrConflict) {
		t.Fatalf("sync over unowned path = %v", err)
	}
	if got, _ := h.readValue(mount, "apps/pay/FOREIGN"); got != "theirs" {
		t.Fatalf("unowned path overwritten with %q", got)
	}
	meta := h.must(http.MethodGet, "/v1/"+mount+"/metadata/apps/pay/FOREIGN", nil)["data"].(map[string]any)
	if custom, _ := meta["custom_metadata"].(map[string]any); custom[MarkerKey] != nil {
		t.Fatalf("unowned path was marked: %v", custom)
	}

	// Retention versus destruction: pruning soft-deletes the current version;
	// history and metadata survive and the value is recoverable.
	// LOG_LEVEL moved externally: dropping it from the selection releases
	// custody with a conflict and a warning instead of deleting the edit.
	pruned, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: entries[:1], Ledger: journal.ledger()}, journal)
	if err != nil || len(pruned.Warnings) != 1 || len(pruned.Conflicts) != 1 {
		t.Fatalf("prune of moved path = %+v, %v; want release with warning", pruned, err)
	}
	if got, _ := h.readValue(mount, "apps/pay/LOG_LEVEL"); got != "hand-edited" {
		t.Fatalf("moved path pruned: %q", got)
	}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Ledger: journal.ledger(), Teardown: true}, journal); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	for _, path := range []string{"apps/pay/DATABASE_URL", "apps/pay/MANAGED_BY_HIKYO"} {
		if _, ok := h.readValue(mount, path); ok {
			t.Fatalf("%s still readable after prune", path)
		}
		meta := h.must(http.MethodGet, "/v1/"+mount+"/metadata/"+path, nil)["data"].(map[string]any)
		versions, _ := meta["versions"].(map[string]any)
		current := fmt.Sprint(meta["current_version"])
		version, _ := versions[current].(map[string]any)
		if version["destroyed"] != false || version["deletion_time"] == "" {
			t.Fatalf("%s prune = %v, want soft-deleted and not destroyed", path, version)
		}
	}
	current := h.must(http.MethodGet, "/v1/"+mount+"/metadata/apps/pay/DATABASE_URL", nil)["data"].(map[string]any)["current_version"]
	h.must(http.MethodPost, "/v1/"+mount+"/undelete/apps/pay/DATABASE_URL", map[string]any{"versions": []any{current}})
	if got, ok := h.readValue(mount, "apps/pay/DATABASE_URL"); !ok || got != "postgres://two" {
		t.Fatalf("soft-deleted value not recoverable: %q, %v", got, ok)
	}

	// Mount movement: the same path re-enabled is a different mount identity.
	h.must(http.MethodDelete, "/v1/sys/mounts/"+mount, nil)
	h.must(http.MethodPost, "/v1/sys/mounts/"+mount, map[string]any{"type": "kv", "options": map[string]string{"version": "2"}})
	if _, err := module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: destination, Gate: gate}); !errors.Is(err, adapter.ErrDestinationID) {
		t.Fatalf("TestConnection after remount = %v, want destination identity refusal", err)
	}

	runNamespaceContract(t, h)
}

func runNamespaceContract(t *testing.T, h *kvHarness) {
	t.Run("namespace", func(t *testing.T) {
		namespace := "hikyo-ns-" + randomSuffix(t)
		status, _ := h.do(http.MethodPost, "/v1/sys/namespaces/"+namespace, map[string]any{})
		if status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusBadRequest {
			t.Skipf("namespaces SKIPPED: this server does not serve sys/namespaces (status %d); Vault namespaces are Enterprise-only", status)
		}
		if status < 200 || status >= 300 {
			t.Fatalf("create namespace = %d", status)
		}
		t.Cleanup(func() { h.do(http.MethodDelete, "/v1/sys/namespaces/"+namespace, nil) })
		scoped := &kvHarness{t: t, addr: h.addr, token: h.token, caPEM: h.caPEM, client: h.client, ns: namespace}
		mount := "kv"
		client := scoped.adapterClient(scoped.fixture(mount))
		module := &Module{API: client}
		destination := adapter.Destination{Kind: adapter.Repository, Owner: mount, Name: "svc"}
		connection, err := module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: destination, Gate: func(context.Context) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		destination.NumericID = connection.DestinationID
		journal := newFakeJournal()
		target := adapter.Target{ID: "tgt_ns", Destination: destination, Generation: 1}
		if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: []adapter.ManifestEntry{{KeyID: "k", CanonicalName: "TOKEN", Classification: adapter.SecretClassification, Value: "in-namespace"}}}, journal); err != nil {
			t.Fatal(err)
		}
		if got, ok := scoped.readValue(mount, "svc/TOKEN"); !ok || got != "in-namespace" {
			t.Fatalf("namespaced value = %q, %v", got, ok)
		}
		if _, ok := h.readValue(mount, "svc/TOKEN"); ok {
			t.Fatal("namespaced write leaked into the root namespace")
		}
		if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Ledger: journal.ledger(), Teardown: true}, journal); err != nil {
			t.Fatal(err)
		}
	})
}
