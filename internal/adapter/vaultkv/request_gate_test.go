package vaultkv

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

type revocableKVJournal struct {
	*fakeJournal
	revoked *atomic.Bool
}

func (j revocableKVJournal) Gate(ctx context.Context, effect adapter.Effect) error {
	if j.revoked.Load() {
		return adapter.ErrUnauthorized
	}
	return j.fakeJournal.Gate(ctx, effect)
}

func requestGateClient(t *testing.T, srv *httptest.Server, credential map[string]string) *Client {
	t.Helper()
	credential["ca_pem"] = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	raw, err := json.Marshal(credential)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(ClientConfig{Origin: srv.URL, Credential: string(raw), Deadline: time.Second, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Forget)
	return client
}

func TestRevocationDuringMetadataPatchRefusesFollowingValueRequest(t *testing.T) {
	var revoked atomic.Bool
	var writes, patches atomic.Int32
	kv := newFakeKV()
	target := testTarget(t, kv)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/sys/internal/ui/mounts/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"type": "kv", "uuid": kv.mount.UUID, "options": map[string]string{"version": "2"}}})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/metadata/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"current_version": 1, "custom_metadata": map[string]string{MarkerKey: target.ID, VersionKey: "1"}, "versions": map[string]any{"1": map[string]any{"destroyed": false, "deletion_time": ""}}}})
		case r.Method == http.MethodPatch:
			patches.Add(1)
			revoked.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/data/"):
			writes.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]int{"version": 2}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	client := requestGateClient(t, srv, map[string]string{"method": "token", "token": "synthetic-static"})
	journal := revocableKVJournal{fakeJournal: newFakeJournal(), revoked: &revoked}
	journal.states["secret:MANAGED_BY_HIKYO"], journal.states["secret:DATABASE_URL"] = adapter.Owned, adapter.Owned
	_, err := (&Module{API: client}).Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest[:1], Ledger: journal.ledger(), Completed: []adapter.Change{{Surface: adapter.Secret, EffectiveName: adapter.SentinelName, Disposition: adapter.Update}}}, journal)
	if err == nil || patches.Load() != 1 || writes.Load() != 0 {
		t.Fatalf("authority loss allowed fresh provider work: err=%v patches=%d value requests=%d", err, patches.Load(), writes.Load())
	}
	if journal.states["secret:DATABASE_URL"] != adapter.Dispatched {
		t.Fatal("refused value request released unresolved metadata custody")
	}
}

func TestConnectionRechecksAuthorityAfterAppRoleLoginAndRenewal(t *testing.T) {
	for _, phase := range []string{"login", "renew"} {
		t.Run(phase, func(t *testing.T) {
			var revoked, armed atomic.Bool
			var mounts, revokes atomic.Int32
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/v1/sys/health":
					_ = json.NewEncoder(w).Encode(map[string]any{"initialized": true, "sealed": false, "version": "synthetic"})
				case strings.HasSuffix(r.URL.Path, "/login"), strings.HasSuffix(r.URL.Path, "/renew-self"):
					if armed.Load() {
						revoked.Store(true)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"auth": map[string]any{"client_token": "synthetic-login", "lease_duration": 600, "renewable": true}})
				case strings.HasSuffix(r.URL.Path, "/revoke-self"):
					revokes.Add(1)
					w.WriteHeader(http.StatusNoContent)
				case strings.Contains(r.URL.Path, "/sys/internal/ui/mounts/"):
					mounts.Add(1)
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"type": "kv", "uuid": "test-mount", "options": map[string]string{"version": "2"}}})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(srv.Close)
			client := requestGateClient(t, srv, map[string]string{"method": "approle", "role_id": "synthetic-role", "secret_id": "synthetic-secret"})
			if phase == "renew" {
				if _, err := client.authorize(t.Context()); err != nil {
					t.Fatal(err)
				}
				now := client.renewAt.Add(time.Second)
				client.now = func() time.Time { return now }
			}
			armed.Store(true)
			gate := func(context.Context) error {
				if revoked.Load() {
					return adapter.ErrUnauthorized
				}
				return nil
			}
			_, err := (&Module{API: client}).TestConnection(t.Context(), adapter.ConnectionRequest{Destination: adapter.Destination{Kind: adapter.Repository, Owner: "secret", Name: "apps"}, Gate: gate})
			if !errors.Is(err, adapter.ErrUnauthorized) || mounts.Load() != 0 {
				t.Fatalf("authority lost during %s: err=%v subsequent mount requests=%d", phase, err, mounts.Load())
			}
			client.Forget()
			if revokes.Load() != 1 || client.token != "" || client.credential.SecretID != "" {
				t.Fatal("refused attempt retained its minted token or credential")
			}
		})
	}
}
