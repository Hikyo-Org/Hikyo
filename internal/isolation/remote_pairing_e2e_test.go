package isolation

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/pairing"
	"github.com/Hikyo-Org/hikyo/internal/remotefetch"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// This harness serves the real router, authorizer, datastore and directory.
// It deliberately does not reuse fakeRemote's nonempty-Authorization shortcut.
type pairingInstance struct {
	db        *store.DB
	service   *service.RemotePairings
	remotes   *service.Remotes
	workspace *service.Workspace
	server    *httptest.Server
	pin       string
	requests  atomic.Int64
}

func newPairingInstance(t *testing.T, db *store.DB, clock func() time.Time) *pairingInstance {
	t.Helper()
	auth := authService(t, db)
	auth.Now = clock
	fetch, err := remotefetch.New(remotefetch.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	i := &pairingInstance{db: db}
	i.remotes = &service.Remotes{DB: db, Keyring: auth.Keyring, Fetch: fetch, Now: clock}
	i.workspace = &service.Workspace{DB: db, Version: "pairing-e2e", Now: clock}
	i.service = &service.RemotePairings{DB: db, Keyring: auth.Keyring, Remotes: i.remotes, Now: clock}
	execRaw(t, db, `INSERT INTO grants(id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES('g_pairing_directory','usr_root','instance-directory',NULL,NULL,NULL,`+ts+`)`)
	handler := server.New(&server.API{Auth: auth, Remotes: i.remotes, Workspace: i.workspace, Pairings: i.service,
		Version: "pairing-e2e", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, nil)
	i.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i.requests.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(i.server.Close)
	i.pin = remotefetch.SPKIFingerprint(i.server.Certificate())
	i.service.Origin = i.server.URL
	i.service.ResolveSPKIPin = func(context.Context) (string, error) { return i.pin, nil }
	return i
}

func secondPairingDB(t *testing.T, engine store.Engine) *store.DB {
	t.Helper()
	if engine == store.EngineSQLite {
		return seededDB(t, openSQLite)
	}
	return seededDB(t, func(t *testing.T) *store.DB {
		dsn := derivedDatabase(t, postgresTestDSN(t), "_isolation_pairing_viewer")
		pre, err := pgx.Connect(t.Context(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pre.Exec(t.Context(), "DROP SCHEMA public CASCADE; CREATE SCHEMA public")
		pre.Close(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		db, err := openIsolationFixture(t, store.Config{Engine: store.EnginePostgres, DSN: dsn})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	})
}

func (i *pairingInstance) invite(t *testing.T, phrase string) service.PairingInviteCreated {
	t.Helper()
	created, err := i.service.CreateInvite(t.Context(), service.LocalPrincipal(root), service.CreatePairingInvite{
		Phrase: phrase, Label: "viewing-instance",
		InviteLifetime: time.Hour, CredentialLifetime: 24 * time.Hour})
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	return created
}

func (i *pairingInstance) request(t *testing.T, method, path string, headers http.Header, body any) (int, []byte) {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal("encode request")
		}
	}
	req, err := http.NewRequestWithContext(t.Context(), method, i.server.URL+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatal("construct request")
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := i.server.Client().Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32769))
	if err != nil || len(raw) > 32768 {
		t.Fatal("read bounded response")
	}
	return resp.StatusCode, raw
}

type pairingBundleFixture struct {
	ID         string `json:"id"`
	Capability string `json:"capability"`
}

func decodePairingBundle(t *testing.T, created service.PairingInviteCreated) pairingBundleFixture {
	t.Helper()
	raw, err := base64.RawURLEncoding.Strict().DecodeString(created.InviteBundle)
	if err != nil {
		t.Fatal("decode invitation")
	}
	var bundle pairingBundleFixture
	if json.Unmarshal(raw, &bundle) != nil || bundle.ID == "" || bundle.Capability == "" {
		t.Fatal("invalid invitation shape")
	}
	return bundle
}

func pairedStoredCredential(t *testing.T, i *pairingInstance, id string) (string, string, ed25519.PrivateKey) {
	t.Helper()
	var sealed []byte
	var err error
	if i.db.Engine() == store.EnginePostgres {
		err = i.db.PG().QueryRow(t.Context(), "SELECT credential_sealed FROM remotes WHERE id=$1", id).Scan(&sealed)
	} else {
		err = i.db.SQLiteRead().QueryRowContext(t.Context(), "SELECT credential_sealed FROM remotes WHERE id=?", id).Scan(&sealed)
	}
	if err != nil {
		t.Fatal("read sealed stored credential")
	}
	plain, err := i.service.Keyring.ForInstance().OpenField(crypto.InstanceFieldAAD{OwnerTable: "remotes", OwnerRowID: id, FieldTag: "credential"}, sealed)
	if err != nil {
		t.Fatal("open stored credential")
	}
	encoded, ok := strings.CutPrefix(string(plain), "hikyo-paired-v1:")
	if !ok {
		t.Fatal("paired credential lost its reserved storage type")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		t.Fatal("decode stored credential")
	}
	var stored struct {
		Token      string `json:"token"`
		PrivateKey []byte `json:"private_key"`
	}
	if json.Unmarshal(raw, &stored) != nil || stored.Token == "" || len(stored.PrivateKey) != ed25519.PrivateKeySize {
		t.Fatal("invalid stored key-bound credential")
	}
	return string(plain), stored.Token, ed25519.PrivateKey(stored.PrivateKey)
}

func TestRemotePairingTwoInstancesAndProofBoundDirectory(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		serving := newPairingInstance(t, db, nil)
		viewing := newPairingInstance(t, secondPairingDB(t, db.Engine()), nil)
		before := remotefetch.Dials()
		prepared, err := viewing.service.Prepare(t.Context(), service.LocalPrincipal(root), service.PreparePairingRequest{})
		if err != nil {
			t.Fatalf("local preparation: %v", err)
		}
		if remotefetch.Dials() != before || serving.requests.Load() != 0 {
			t.Fatal("preparation contacted a remote before invitation creation")
		}
		created := serving.invite(t, prepared.Phrase)
		raw, err := base64.RawURLEncoding.Strict().DecodeString(created.InviteBundle)
		if err != nil {
			t.Fatal("decode invitation")
		}
		if bytes.Contains(raw, []byte(prepared.Phrase)) || bytes.Contains(raw, []byte("private_key")) {
			t.Fatal("invitation discloses requester secret material")
		}
		var altered map[string]json.RawMessage
		if json.Unmarshal(raw, &altered) != nil {
			t.Fatal("decode invitation fields")
		}
		altered["spki_pin"], err = json.Marshal(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
		if err != nil {
			t.Fatal("encode altered pin")
		}
		badBundle, err := json.Marshal(altered)
		if err != nil {
			t.Fatal("encode altered invitation")
		}
		if _, err := viewing.service.Pair(t.Context(), service.LocalPrincipal(root), service.PairRemoteRequest{
			Name: "wrong-pin", RequesterState: prepared.RequesterState, InviteBundle: base64.RawURLEncoding.EncodeToString(badBundle),
		}); err == nil {
			t.Fatal("pairing accepted the wrong TLS pin")
		}
		if serving.requests.Load() != 0 {
			t.Fatal("pairing sent HTTP bytes before accepting the remote TLS pin")
		}
		if n := queryInt(t, db, "SELECT attempts FROM remote_pairing_invites"); n != 0 {
			t.Fatal("wrong TLS pin reached invitation redemption")
		}
		view, err := viewing.service.Pair(t.Context(), service.LocalPrincipal(root), service.PairRemoteRequest{Name: "paired-serving", RequesterState: prepared.RequesterState, InviteBundle: created.InviteBundle})
		if err != nil {
			t.Fatalf("pair against real TLS router: %v", err)
		}
		if view.State != string(remotefetch.OutcomeOK) || view.Identity == "" || view.OrgCount != 2 || view.ProjectCount != 3 {
			t.Fatal("pairing did not fetch the real serving directory")
		}
		if serving.requests.Load() < 3 {
			t.Fatal("pairing did not traverse start, finish and authenticated directory")
		}
		stored, token, private := pairedStoredCredential(t, viewing, view.ID)
		listing, outcome, err := viewing.remotes.Fetch.Directory(t.Context(), serving.server.URL, serving.pin, stored)
		if err != nil || outcome != remotefetch.OutcomeOK || listing.Identity != view.Identity {
			t.Fatal("subsequent directory fetch did not prove the stored private key")
		}
		const directory = "/api/v1/instance/directory"
		makeProof := func(key ed25519.PrivateKey) string {
			t.Helper()
			proof, err := pairing.GenerateDPoP(key, pairing.DPoPClaims{Method: http.MethodGet, URL: serving.server.URL + directory, AccessToken: token, IssuedAt: time.Now().UTC()})
			if err != nil {
				t.Fatal("make directory proof")
			}
			return proof
		}
		proof := makeProof(private)
		headers := http.Header{"Authorization": {"DPoP " + token}, "DPoP": {proof}}
		if status, _ := serving.request(t, http.MethodGet, directory, headers, nil); status != http.StatusOK {
			t.Fatalf("valid directory proof status%d", status)
		}
		if status, _ := serving.request(t, http.MethodGet, directory, headers, nil); status != http.StatusUnauthorized {
			t.Fatalf("replayed directory proof status%d", status)
		}
		otherKey, err := pairing.NewSigningKey()
		if err != nil {
			t.Fatal(err)
		}
		for name, headers := range map[string]http.Header{
			"missing-proof":    {"Authorization": {"DPoP " + token}},
			"bearer-downgrade": {"Authorization": {"Bearer " + token}, "DPoP": {makeProof(private)}},
			"wrong-key":        {"Authorization": {"DPoP " + token}, "DPoP": {makeProof(otherKey)}},
			"duplicate-proof":  {"Authorization": {"DPoP " + token}, "DPoP": {makeProof(private), makeProof(private)}},
		} {
			t.Run(name, func(t *testing.T) {
				if status, _ := serving.request(t, http.MethodGet, directory, headers, nil); status != http.StatusUnauthorized {
					t.Fatalf("invalid directory presentation status%d", status)
				}
			})
		}
		if _, err := serving.workspace.ListOrigins(t.Context(), service.PairedConnectionActor(token, makeProof(private))); err == nil {
			t.Fatal("directory credential reached workspace origin administration")
		}
		if _, err := serving.workspace.ListConnections(t.Context(), service.Bearer(token)); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("paired token reached ordinary bearer authentication: %v", err)
		}
		if _, err := viewing.service.Pair(t.Context(), service.LocalPrincipal(root), service.PairRemoteRequest{Name: "second-use", RequesterState: prepared.RequesterState, InviteBundle: created.InviteBundle}); err == nil {
			t.Fatal("consumed invitation paired twice")
		}
	})
}

func pairingTestKey(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	key, err := pairing.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	public, err := pairing.PublicJWK(key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(public)
	if err != nil {
		t.Fatal("encode public key")
	}
	return key, string(raw)
}

func pairingFinishFixture(t *testing.T, phrase string, started service.PairingStarted, key ed25519.PrivateKey, now time.Time) (string, string) {
	t.Helper()
	var challenge struct {
		Context         pairing.Context `json:"context"`
		ServerEphemeral []byte          `json:"server_ephemeral"`
		Nonce           string          `json:"nonce"`
	}
	if json.Unmarshal([]byte(started.ServerMessage), &challenge) != nil {
		t.Fatal("decode PAKE challenge")
	}
	proofs, err := pairing.ClientProof(phrase, challenge.Context, challenge.ServerEphemeral)
	if err != nil {
		t.Fatal("create PAKE client proof")
	}
	message, err := json.Marshal(struct {
		ClientEphemeral []byte `json:"client_ephemeral"`
		ClientProof     []byte `json:"client_proof"`
	}{proofs.ClientEphemeral, proofs.ClientProof})
	if err != nil {
		t.Fatal("encode PAKE proof")
	}
	proof, err := pairing.GenerateDPoP(key, pairing.DPoPClaims{Method: http.MethodPost, URL: challenge.Context.Origin + "/api/v1/auth/pairing/finish", AccessToken: started.Exchange, Nonce: challenge.Nonce, IssuedAt: now})
	if err != nil {
		t.Fatal("create enrollment possession proof")
	}
	return string(message), proof
}

func TestRemotePairingInvitationAuthorityAndLifecycle(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		now := time.Now().UTC()
		clock := func() time.Time { return now }
		i := newPairingInstance(t, db, clock)
		ctx := t.Context()
		prepared, err := i.service.Prepare(ctx, service.LocalPrincipal(root), service.PreparePairingRequest{})
		if err != nil {
			t.Fatal(err)
		}
		req := service.CreatePairingInvite{Phrase: prepared.Phrase, Label: "authority-probe", InviteLifetime: time.Hour, CredentialLifetime: 24 * time.Hour}
		for name, actor := range map[string]service.Actor{"anonymous": {}, "unprivileged": service.LocalPrincipal(nobody)} {
			t.Run(name, func(t *testing.T) {
				if _, err := i.service.CreateInvite(ctx, actor, req); err == nil {
					t.Fatal("unprivileged invitation creation succeeded")
				}
				if _, err := i.service.ListInvites(ctx, actor); err == nil {
					t.Fatal("unprivileged invitation listing succeeded")
				}
				if _, err := i.service.Prepare(ctx, actor, service.PreparePairingRequest{}); err == nil {
					t.Fatal("unprivileged requester preparation succeeded")
				}
			})
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM remote_pairing_invites"); n != 0 {
			t.Fatal("refused administrators created invitation state")
		}
		if status, _ := i.request(t, http.MethodGet, "/api/v1/instance/pairing-invites", nil, nil); status != http.StatusUnauthorized {
			t.Fatalf("anonymous invitation listing status%d", status)
		}
		if status, _ := i.request(t, http.MethodPost, "/api/v1/instance/pairing-invites", nil, map[string]any{
			"phrase": prepared.Phrase, "label": "anonymous-create",
			"invite_lifetime_seconds": 3600, "credential_lifetime_seconds": 86400,
		}); status != http.StatusUnauthorized {
			t.Fatalf("anonymous invitation creation status%d", status)
		}
		_, public := pairingTestKey(t)
		unknown, _, err := crypto.NewArtifact(crypto.ArtifactHandoffCode)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := i.service.Start(ctx, unknown, public); !errors.Is(err, service.ErrPairingInvalid) {
			t.Fatalf("unknown capability result: %v", err)
		}
		for _, kind := range []string{"revoked", "expired", "epoch"} {
			t.Run(kind, func(t *testing.T) {
				created := i.invite(t, prepared.Phrase)
				bundle := decodePairingBundle(t, created)
				switch kind {
				case "revoked":
					if err := i.service.RevokeInvite(ctx, service.LocalPrincipal(root), created.Invite.ID); err != nil {
						t.Fatal(err)
					}
				case "expired":
					now = now.Add(time.Hour + time.Second)
				case "epoch":
					execRaw(t, db, "UPDATE auth_instance_state SET credential_epoch=credential_epoch+1")
				}
				if _, err := i.service.Start(ctx, bundle.Capability, public); !errors.Is(err, service.ErrPairingInvalid) {
					t.Fatalf("unusable invitation result: %v", err)
				}
			})
		}
	})
}

func TestRemotePairingAutomaticIdentityAuthorization(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		i := newPairingInstance(t, db, nil)
		ctx := t.Context()
		var resolutions atomic.Int64
		i.service.ResolveSPKIPin = func(context.Context) (string, error) {
			resolutions.Add(1)
			return i.pin, nil
		}
		req := service.CreatePairingInvite{Phrase: "Ab4!", Label: "automatic-identity", InviteLifetime: time.Hour, CredentialLifetime: 24 * time.Hour}
		for _, actor := range []service.Actor{{}, service.LocalPrincipal(nobody)} {
			if _, err := i.service.CreateInvite(ctx, actor, req); err == nil {
				t.Fatal("unauthorized actor created invitation")
			}
			if _, err := i.service.ListInvites(ctx, actor); err == nil {
				t.Fatal("unauthorized actor listed invitation configuration")
			}
		}
		if status, _ := i.request(t, http.MethodGet, "/api/v1/instance/pairing-invites", nil, nil); status != http.StatusUnauthorized {
			t.Fatalf("anonymous invitation listing status%d", status)
		}
		if status, _ := i.request(t, http.MethodPost, "/api/v1/instance/pairing-invites", nil, map[string]any{
			"phrase": req.Phrase, "label": req.Label, "invite_lifetime_seconds": 3600, "credential_lifetime_seconds": 86400,
		}); status != http.StatusUnauthorized {
			t.Fatalf("anonymous invitation creation status%d", status)
		}
		if resolutions.Load() != 0 {
			t.Fatal("unauthorized actor invoked TLS identity resolution")
		}
		listed, err := i.service.ListInvites(ctx, service.LocalPrincipal(root))
		if err != nil || listed.Origin != i.server.URL || listed.ConfigurationError != "" {
			t.Fatal("authorized listing omitted configured pairing identity")
		}
		adminToken := mintReauthedSession(t, db, root, "")
		headers := http.Header{"Authorization": {"Bearer " + adminToken}}
		if status, _ := i.request(t, http.MethodGet, "/api/v1/instance/pairing-invites", headers, nil); status != http.StatusOK {
			t.Fatalf("authorized invitation listing status%d", status)
		}
		if resolutions.Load() != 0 {
			t.Fatal("listing configuration invoked TLS identity resolution")
		}
		i.service.ConfigurationError = "The public HTTPS address is not configured."
		listed, err = i.service.ListInvites(ctx, service.LocalPrincipal(root))
		if err != nil || listed.ConfigurationError != i.service.ConfigurationError {
			t.Fatal("listing did not expose the static configuration refusal")
		}
		if _, err := i.service.CreateInvite(ctx, service.LocalPrincipal(root), req); err == nil {
			t.Fatal("invalid serving configuration created invitation")
		}
		if resolutions.Load() != 0 || queryInt(t, db, "SELECT COUNT(*) FROM remote_pairing_invites") != 0 {
			t.Fatal("invalid configuration resolved TLS or created invitation state")
		}
		i.service.ConfigurationError = ""
		created, err := i.service.CreateInvite(ctx, service.LocalPrincipal(root), req)
		if err != nil || resolutions.Load() != 1 {
			t.Fatal("authorized creation did not resolve TLS identity exactly once")
		}
		if created.Invite.Origin != i.server.URL || created.Invite.SPKIPin != i.pin {
			t.Fatal("invitation did not use server-owned identity")
		}
		raw, err := base64.RawURLEncoding.Strict().DecodeString(created.InviteBundle)
		if err != nil {
			t.Fatal("decode server-owned invitation")
		}
		var bundle struct {
			Origin string `json:"origin"`
			Pin    string `json:"spki_pin"`
		}
		if json.Unmarshal(raw, &bundle) != nil || bundle.Origin != i.server.URL || bundle.Pin != i.pin {
			t.Fatal("invitation bundle identity differs from server configuration")
		}
		for _, injected := range []string{"origin", "spki_pin"} {
			body := map[string]any{"phrase": req.Phrase, "label": "injected-identity", "invite_lifetime_seconds": 3600, "credential_lifetime_seconds": 86400, injected: "https://attacker.invalid"}
			if status, _ := i.request(t, http.MethodPost, "/api/v1/instance/pairing-invites", headers, body); status != http.StatusBadRequest {
				t.Fatalf("HTTP request accepted removed identity field %s: status%d", injected, status)
			}
		}
		if resolutions.Load() != 1 || queryInt(t, db, "SELECT COUNT(*) FROM remote_pairing_invites") != 1 {
			t.Fatal("rejected identity injection resolved TLS or created invitation state")
		}
		i.service.ResolveSPKIPin = func(context.Context) (string, error) {
			resolutions.Add(1)
			return "", errors.New("private TLS resolver diagnostic")
		}
		if _, err := i.service.CreateInvite(ctx, service.LocalPrincipal(root), req); err == nil {
			t.Fatal("failed TLS resolution created invitation")
		}
		if resolutions.Load() != 2 || queryInt(t, db, "SELECT COUNT(*) FROM remote_pairing_invites") != 1 {
			t.Fatal("failed TLS resolution persisted invitation state")
		}
		// The callback mutates authority while no service transaction is held.
		// Creation must resolve the caller again before writing the invitation.
		execRaw(t, db, `INSERT INTO grants(id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES('g_pairing_late_admin','usr_nobody','instance-config',NULL,NULL,NULL,`+ts+`)`)
		i.service.ResolveSPKIPin = func(context.Context) (string, error) {
			resolutions.Add(1)
			execRaw(t, db, `DELETE FROM grants WHERE id='g_pairing_late_admin'`)
			return i.pin, nil
		}
		if _, err := i.service.CreateInvite(ctx, service.LocalPrincipal(nobody), req); err == nil {
			t.Fatal("administrator revoked during TLS resolution created invitation")
		}
		if resolutions.Load() != 3 || queryInt(t, db, "SELECT COUNT(*) FROM remote_pairing_invites") != 1 {
			t.Fatal("creation failed to reauthorize after TLS identity resolution")
		}
	})
}

func TestRemotePairingAttemptBudgetSurvivesExchangeExpiry(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		now := time.Now().UTC()
		i := newPairingInstance(t, db, func() time.Time { return now })
		prepared, err := i.service.Prepare(t.Context(), service.LocalPrincipal(root), service.PreparePairingRequest{})
		if err != nil {
			t.Fatal(err)
		}
		created := i.invite(t, prepared.Phrase)
		bundle := decodePairingBundle(t, created)
		_, public := pairingTestKey(t)
		for attempt := range 8 {
			if _, err := i.service.Start(t.Context(), bundle.Capability, public); err != nil {
				t.Fatalf("admitted attempt %d failed: %v", attempt+1, err)
			}
			now = now.Add(3 * time.Minute)
		}
		// A separately loaded service must see the durable lifetime counter even
		// after all previous exchange rows have expired and been pruned.
		reloaded := &service.RemotePairings{DB: db, Keyring: reloadProbeKeyring(t, db), Remotes: i.remotes, Now: func() time.Time { return now }}
		if _, err := reloaded.Start(t.Context(), bundle.Capability, public); !errors.Is(err, service.ErrPairingInvalid) {
			t.Fatalf("ninth attempt after expiry result: %v", err)
		}
		if n := queryInt(t, db, "SELECT attempts FROM remote_pairing_invites"); n != 8 {
			t.Fatalf("durable attempts=%d, want8", n)
		}
	})
}

func TestRemotePairingWrongProofConsumesExchange(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		i := newPairingInstance(t, db, nil)
		prepared, err := i.service.Prepare(t.Context(), service.LocalPrincipal(root), service.PreparePairingRequest{})
		if err != nil {
			t.Fatal(err)
		}
		created := i.invite(t, prepared.Phrase)
		bundle := decodePairingBundle(t, created)
		key, public := pairingTestKey(t)
		for _, kind := range []string{"wrong-phrase", "missing-possession", "wrong-key"} {
			t.Run(kind, func(t *testing.T) {
				started, err := i.service.Start(t.Context(), bundle.Capability, public)
				if err != nil {
					t.Fatal(err)
				}
				phrase := prepared.Phrase
				if kind == "wrong-phrase" {
					phrase, err = pairing.GeneratePhrase()
					if err != nil {
						t.Fatal(err)
					}
					if phrase == prepared.Phrase {
						t.Fatal("independent random phrase unexpectedly identical")
					}
				}
				signingKey := key
				if kind == "wrong-key" {
					signingKey, _ = pairingTestKey(t)
				}
				message, proof := pairingFinishFixture(t, phrase, started, signingKey, time.Now().UTC())
				if kind == "missing-possession" {
					proof = ""
				}
				if _, err := i.service.Finish(t.Context(), started.Exchange, message, proof); !errors.Is(err, service.ErrPairingInvalid) {
					t.Fatalf("invalid finish result: %v", err)
				}
				// Retry with both valid proofs: this fails only if the prior
				// invalid attempt's consumption survived its error return.
				message, proof = pairingFinishFixture(t, prepared.Phrase, started, key, time.Now().UTC())
				if _, err := i.service.Finish(t.Context(), started.Exchange, message, proof); !errors.Is(err, service.ErrPairingInvalid) {
					t.Fatalf("consumed exchange reused: %v", err)
				}
			})
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM instance_connections"); n != 0 {
			t.Fatal("failed proofs minted a connection")
		}
	})
}

func TestRemotePairingConcurrentFinishMintsOnce(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		i := newPairingInstance(t, db, nil)
		prepared, err := i.service.Prepare(t.Context(), service.LocalPrincipal(root), service.PreparePairingRequest{})
		if err != nil {
			t.Fatal(err)
		}
		created := i.invite(t, prepared.Phrase)
		bundle := decodePairingBundle(t, created)
		type finishInput struct{ exchange, message, proof string }
		var inputs [2]finishInput
		for n := range inputs {
			key, public := pairingTestKey(t)
			started, err := i.service.Start(t.Context(), bundle.Capability, public)
			if err != nil {
				t.Fatal(err)
			}
			message, proof := pairingFinishFixture(t, prepared.Phrase, started, key, time.Now().UTC())
			inputs[n] = finishInput{started.Exchange, message, proof}
		}
		// Model two nodes, each loading keyring custody independently.
		peer := &service.RemotePairings{DB: db, Keyring: reloadProbeKeyring(t, db), Remotes: i.remotes}
		gate := make(chan struct{})
		results := make(chan error, 2)
		ctx := t.Context()
		for n, svc := range []*service.RemotePairings{i.service, peer} {
			go func(svc *service.RemotePairings, input finishInput) {
				<-gate
				_, err := svc.Finish(ctx, input.exchange, input.message, input.proof)
				results <- err
			}(svc, inputs[n])
		}
		close(gate)
		success := 0
		var unexpected error
		for range inputs {
			err := <-results
			if err == nil {
				success++
			} else if !errors.Is(err, service.ErrPairingInvalid) {
				unexpected = err
			}
		}
		if unexpected != nil {
			t.Fatalf("concurrent finish returned unexpected error: %v", unexpected)
		}
		if success != 1 {
			t.Fatalf("concurrent finish successes=%d, want1", success)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM instance_connections"); n != 1 {
			t.Fatalf("connection count=%d, want1", n)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM remote_paired_connections"); n != 1 {
			t.Fatalf("key binding count=%d, want1", n)
		}
	})
}

func TestRemotePairingSupportedRestoreRetiresOldInvitationAndExchange(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		now := time.Now().UTC()
		i := newPairingInstance(t, db, func() time.Time { return now })
		prepared, err := i.service.Prepare(t.Context(), service.LocalPrincipal(root), service.PreparePairingRequest{})
		if err != nil {
			t.Fatal(err)
		}
		created := i.invite(t, prepared.Phrase)
		bundle := decodePairingBundle(t, created)
		key, public := pairingTestKey(t)
		started, err := i.service.Start(t.Context(), bundle.Capability, public)
		if err != nil {
			t.Fatal(err)
		}
		message, proof := pairingFinishFixture(t, prepared.Phrase, started, key, now)
		// Freeze time across real archive restore and recovery. A refusal must
		// come from restored authority retirement, not accidental expiration.
		db = restoreIsolationFixture(t, db)
		if _, err := (&service.Restore{DB: db}).Reconcile(t.Context(), root); err != nil {
			t.Fatal(err)
		}
		restored := &service.RemotePairings{DB: db, Keyring: authService(t, db).Keyring, Now: func() time.Time { return now }, Origin: i.server.URL, ResolveSPKIPin: i.service.ResolveSPKIPin}
		if _, err := restored.Start(t.Context(), bundle.Capability, public); !errors.Is(err, service.ErrPairingInvalid) {
			t.Fatalf("pre-restore invitation result: %v", err)
		}
		if _, err := restored.Finish(t.Context(), started.Exchange, message, proof); !errors.Is(err, service.ErrPairingInvalid) {
			t.Fatalf("pre-restore exchange result: %v", err)
		}
		fresh, err := restored.CreateInvite(t.Context(), service.LocalPrincipal(root), service.CreatePairingInvite{
			Phrase: prepared.Phrase, Label: "fresh-after-restore",
			InviteLifetime: time.Hour, CredentialLifetime: 24 * time.Hour,
		})
		if err != nil {
			t.Fatalf("fresh restored invitation: %v", err)
		}
		freshBundle := decodePairingBundle(t, fresh)
		freshStarted, err := restored.Start(t.Context(), freshBundle.Capability, public)
		if err != nil {
			t.Fatal(err)
		}
		message, proof = pairingFinishFixture(t, prepared.Phrase, freshStarted, key, now)
		if _, err := restored.Finish(t.Context(), freshStarted.Exchange, message, proof); err != nil {
			t.Fatalf("fresh restored exchange: %v", err)
		}
	})
}

func TestRemotePairingSealedStateSurvivesInstanceReencryption(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		i := newPairingInstance(t, db, nil)
		prepared, err := i.service.Prepare(t.Context(), service.LocalPrincipal(root), service.PreparePairingRequest{})
		if err != nil {
			t.Fatal(err)
		}
		created := i.invite(t, prepared.Phrase)
		bundle := decodePairingBundle(t, created)
		key, public := pairingTestKey(t)
		started, err := i.service.Start(t.Context(), bundle.Capability, public)
		if err != nil {
			t.Fatal(err)
		}
		message, proof := pairingFinishFixture(t, prepared.Phrase, started, key, time.Now().UTC())
		rotation := &service.Rotation{DB: db, Keyring: i.service.Keyring}
		rotated, err := rotation.RotateDEK(t.Context(), service.LocalPrincipal(root), service.DEKScope{Instance: true})
		if err != nil {
			t.Fatal(err)
		}
		reencrypt := &service.Reencrypt{DB: db, Keyring: i.service.Keyring, ChunkSize: 1, ChunkPause: -1}
		if _, err := reencrypt.ReencryptInstance(t.Context(), service.LocalPrincipal(root)); err != nil {
			t.Fatal(err)
		}
		for _, tableColumn := range [][2]string{{"remote_pairing_invites", "pake_sealed"}, {"remote_pairing_exchanges", "state_sealed"}} {
			var sealed []byte
			query := "SELECT " + tableColumn[1] + " FROM " + tableColumn[0]
			if db.Engine() == store.EnginePostgres {
				err = db.PG().QueryRow(t.Context(), query).Scan(&sealed)
			} else {
				err = db.SQLiteRead().QueryRowContext(t.Context(), query).Scan(&sealed)
			}
			if err != nil {
				t.Fatal("read reencrypted pairing state")
			}
			version, err := crypto.RecordKeyVersion(sealed)
			if err != nil || version != rotated.Version {
				t.Fatal("pairing secret remained under retiring instance key")
			}
		}
		peer := &service.RemotePairings{DB: db, Keyring: reloadProbeKeyring(t, db)}
		if _, err := peer.Finish(t.Context(), started.Exchange, message, proof); err != nil {
			t.Fatalf("reencrypted cross-process exchange continuation: %v", err)
		}
	})
}

func TestRemotePairingRestoreEpochExceedsArchivedPairingStamps(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		i := newPairingInstance(t, db, nil)
		prepared, err := i.service.Prepare(t.Context(), service.LocalPrincipal(root), service.PreparePairingRequest{})
		if err != nil {
			t.Fatal(err)
		}
		created := i.invite(t, prepared.Phrase)
		bundle := decodePairingBundle(t, created)
		_, public := pairingTestKey(t)
		if _, err := i.service.Start(t.Context(), bundle.Capability, public); err != nil {
			t.Fatal(err)
		}
		// Archive stamps are attacker-controlled; advancing merely from the
		// live counter could activate a forged future-stamped invitation.
		execRaw(t, db, "UPDATE remote_pairing_invites SET credential_epoch=40")
		execRaw(t, db, "UPDATE remote_pairing_exchanges SET credential_epoch=80")
		if err := tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			if err := az.AdvanceRestoreEpoch(ctx, time.Now().UTC()); err != nil {
				return err
			}
			_, err := az.RestoreState(ctx)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, "SELECT credential_epoch FROM auth_instance_state"); n != 81 {
			t.Fatalf("restored credential epoch=%d, want81", n)
		}
	})
}

func TestRemotePairingRestoreEpochSupportsHistoricalAbsentTables(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		for _, table := range []string{"remote_pairing_replays", "remote_paired_connections", "remote_pairing_exchanges", "remote_pairing_invites"} {
			execRaw(t, db, "DROP TABLE "+table)
		}
		if err := tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			if err := az.AdvanceRestoreEpoch(ctx, time.Now().UTC()); err != nil {
				return err
			}
			// On PostgreSQL this proves no undefined-table query was attempted
			// and swallowed, which would leave the transaction aborted.
			_, err := az.RestoreState(ctx)
			return err
		}); err != nil {
			t.Fatalf("pre-pairing schema restore epoch: %v", err)
		}
	})
}

func TestRemotePairingRequesterExpiryDuringNetwork(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		// Each stage gets a fresh viewer. A successful remote Finish remains
		// consumed when later local expiry prevents storing its remote card.
		for _, stage := range []string{"after-start", "during-directory"} {
			t.Run(stage, func(t *testing.T) {
				// Wire without the shared constructor's one-time fixture grant.
				auth := authService(t, db)
				fetch, err := remotefetch.New(remotefetch.DefaultConfig())
				if err != nil {
					t.Fatal(err)
				}
				remoteSvc := &service.Remotes{DB: db, Keyring: auth.Keyring, Fetch: fetch}
				pairSvc := &service.RemotePairings{DB: db, Keyring: auth.Keyring, Remotes: remoteSvc}
				ws := &service.Workspace{DB: db, Version: "pairing-expiry-e2e"}
				var clock atomic.Int64
				now := time.Now().UTC()
				clock.Store(now.UnixNano())
				handler := server.New(&server.API{Auth: auth, Remotes: remoteSvc, Workspace: ws, Pairings: pairSvc, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, nil)
				tlsPeer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if stage == "after-start" && r.URL.Path == "/api/v1/auth/pairing/start" || stage == "during-directory" && r.URL.Path == "/api/v1/instance/directory" {
						clock.Store(now.Add(2 * time.Hour).UnixNano())
					}
					handler.ServeHTTP(w, r)
				}))
				t.Cleanup(tlsPeer.Close)
				pairSvc.Origin = tlsPeer.URL
				pairSvc.ResolveSPKIPin = func(context.Context) (string, error) {
					return remotefetch.SPKIFingerprint(tlsPeer.Certificate()), nil
				}
				viewer := newPairingInstance(t, secondPairingDB(t, db.Engine()), func() time.Time { return time.Unix(0, clock.Load()).UTC() })
				prepared, err := viewer.service.Prepare(t.Context(), service.LocalPrincipal(root), service.PreparePairingRequest{})
				if err != nil {
					t.Fatal(err)
				}
				created, err := pairSvc.CreateInvite(t.Context(), service.LocalPrincipal(root), service.CreatePairingInvite{Phrase: prepared.Phrase, Label: "expiry-" + stage, InviteLifetime: time.Hour, CredentialLifetime: 24 * time.Hour})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := viewer.service.Pair(t.Context(), service.LocalPrincipal(root), service.PairRemoteRequest{Name: "expired-request", RequesterState: prepared.RequesterState, InviteBundle: created.InviteBundle}); !errors.Is(err, service.ErrPairingInvalid) {
					t.Fatalf("expired network continuation result: %v", err)
				}
				if n := queryInt(t, viewer.db, "SELECT COUNT(*) FROM remotes"); n != 0 {
					t.Fatal("expired requester continuation committed a remote card")
				}
			})
		}
	})
}

func TestRemotePairingFinishDeadlineCrossesVerification(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		now := time.Now().UTC()
		var finishing atomic.Bool
		var checks atomic.Int64
		i := newPairingInstance(t, db, func() time.Time {
			if finishing.Load() && checks.Add(1) > 1 {
				return now.Add(3 * time.Minute)
			}
			return now
		})
		prepared, err := i.service.Prepare(t.Context(), service.LocalPrincipal(root), service.PreparePairingRequest{})
		if err != nil {
			t.Fatal(err)
		}
		created := i.invite(t, prepared.Phrase)
		bundle := decodePairingBundle(t, created)
		key, public := pairingTestKey(t)
		started, err := i.service.Start(t.Context(), bundle.Capability, public)
		if err != nil {
			t.Fatal(err)
		}
		message, proof := pairingFinishFixture(t, prepared.Phrase, started, key, now)
		principals := queryInt(t, db, "SELECT COUNT(*) FROM principals")
		finishing.Store(true)
		status, _ := i.request(t, http.MethodPost, "/api/v1/auth/pairing/finish", nil, map[string]string{
			"exchange": started.Exchange, "client_message": message, "proof": proof,
		})
		if status != http.StatusUnauthorized {
			t.Fatalf("exchange expired during verification status%d", status)
		}
		if checks.Load() < 2 {
			t.Fatal("finish did not reach fresh post-verification deadline check")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM principals"); n != principals {
			t.Fatal("expired exchange minted a principal")
		}
		for _, table := range []string{"instance_connections", "remote_paired_connections"} {
			if n := queryInt(t, db, "SELECT COUNT(*) FROM "+table); n != 0 {
				t.Fatal("expired exchange minted directory authority")
			}
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM remote_pairing_exchanges WHERE consumed_at IS NOT NULL"); n != 1 {
			t.Fatal("deadline failure rolled back single-use exchange consumption")
		}
		// Return to the original valid time to distinguish durable consumption
		// from a retry that merely fails because its clock remains expired.
		finishing.Store(false)
		if _, err := i.service.Finish(t.Context(), started.Exchange, message, proof); !errors.Is(err, service.ErrPairingInvalid) {
			t.Fatalf("deadline-rejected exchange reused within original window: %v", err)
		}
	})
}

// runRemotePairingAuditLifecycle is invoked by the canonical remote lifecycle
// on that lifecycle's own database. Its TLS identity comes from the existing
// peer fixture, but authentication is performed by the real PAKE and authorizer,
// never the fake peer's nonempty-header shortcut. The two-instance test above
// separately verifies the full HTTP transport.
func runRemotePairingAuditLifecycle(t *testing.T, db *store.DB, remotes *service.Remotes, workspace *service.Workspace, origin, pin string) {
	t.Helper()
	ctx := t.Context()
	admin := service.LocalPrincipal(root)
	svc := &service.RemotePairings{DB: db, Keyring: remotes.Keyring, Remotes: remotes, Origin: origin, ResolveSPKIPin: func(context.Context) (string, error) { return pin, nil }}
	prepared, err := svc.Prepare(ctx, admin, service.PreparePairingRequest{})
	if err != nil {
		t.Fatalf("pairing audit preparation: %v", err)
	}
	create := func(label string) service.PairingInviteCreated {
		t.Helper()
		created, err := svc.CreateInvite(ctx, admin, service.CreatePairingInvite{
			Phrase: prepared.Phrase, Label: label,
			InviteLifetime: time.Hour, CredentialLifetime: 24 * time.Hour,
		})
		if err != nil {
			t.Fatalf("pairing audit invitation: %v", err)
		}
		return created
	}
	revocable := create("audit-revocable")
	created := create("audit-pairing")
	listed, err := svc.ListInvites(ctx, admin)
	if err != nil {
		t.Fatalf("pairing audit listing: %v", err)
	}
	seen := map[string]bool{}
	for _, invite := range listed.Items {
		seen[invite.ID] = true
	}
	if !seen[revocable.Invite.ID] || !seen[created.Invite.ID] {
		t.Fatal("pairing audit listing omitted actual invitations")
	}
	if err := svc.RevokeInvite(ctx, admin, revocable.Invite.ID); err != nil {
		t.Fatalf("pairing audit revocation: %v", err)
	}
	key, public := pairingTestKey(t)
	revokedBundle := decodePairingBundle(t, revocable)
	if _, err := svc.Start(ctx, revokedBundle.Capability, public); !errors.Is(err, service.ErrPairingInvalid) {
		t.Fatalf("revoked pairing audit invitation accepted: %v", err)
	}
	bundle := decodePairingBundle(t, created)
	failedStart, err := svc.Start(ctx, bundle.Capability, public)
	if err != nil {
		t.Fatalf("pairing audit failure admission: %v", err)
	}
	message, _ := pairingFinishFixture(t, prepared.Phrase, failedStart, key, time.Now().UTC())
	if _, err := svc.Finish(ctx, failedStart.Exchange, message, ""); !errors.Is(err, service.ErrPairingInvalid) {
		t.Fatalf("missing possession proof did not emit pairing failure: %v", err)
	}
	started, err := svc.Start(ctx, bundle.Capability, public)
	if err != nil {
		t.Fatalf("pairing audit admission: %v", err)
	}
	message, proof := pairingFinishFixture(t, prepared.Phrase, started, key, time.Now().UTC())
	finished, err := svc.Finish(ctx, started.Exchange, message, proof)
	if err != nil {
		t.Fatalf("pairing audit redemption: %v", err)
	}
	if finished.Connection.ID == "" || finished.Credential == "" {
		t.Fatal("pairing audit redemption did not mint directory authority")
	}
	proof, err = pairing.GenerateDPoP(key, pairing.DPoPClaims{
		Method: http.MethodGet, URL: origin + "/api/v1/instance/directory",
		AccessToken: finished.Credential, IssuedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal("make pairing audit directory proof")
	}
	if _, err := workspace.Serve(ctx, service.PairedConnectionActor(finished.Credential, proof)); err != nil {
		t.Fatalf("paired audit credential did not serve metadata: %v", err)
	}
	if err := workspace.RevokeConnection(ctx, admin, finished.Connection.ID); err != nil {
		t.Fatalf("paired audit credential revocation: %v", err)
	}
}

func TestRemotePairingChosenSecretsAndVariableWordCounts(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		serving := newPairingInstance(t, db, nil)
		viewing := newPairingInstance(t, secondPairingDB(t, db.Engine()), nil)
		longPrefix := strings.Repeat("p", 100)
		cases := []struct {
			name       string
			custom     string
			wordCount  int
			mismatches []string
		}{
			{name: "four-characters", custom: "Ab1!"},
			{name: "five-characters", custom: "a B!?"},
			{name: "exact-case-and-whitespace", custom: " \tPaß phrase\n ", mismatches: []string{" \tpaß phrase\n ", "Paß phrase"}},
			{name: "one-generated-word", wordCount: 1},
			{name: "128-generated-words", wordCount: 128},
			{name: "long-suffix-significant", custom: longPrefix + "雪A", mismatches: []string{longPrefix + "雪B"}},
			{name: "4096-nul-bytes", custom: strings.Repeat("\x00", 4096)},
			{name: "4096-utf8-bytes", custom: strings.Repeat("界", 1365) + "x"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := service.PreparePairingRequest{}
				if tc.wordCount != 0 {
					count := tc.wordCount
					req.WordCount = &count
				} else {
					secret := tc.custom
					req.Phrase = &secret
				}
				prepared, err := viewing.service.Prepare(t.Context(), service.LocalPrincipal(root), req)
				if err != nil {
					t.Fatalf("prepare chosen secret: %v", err)
				}
				if tc.wordCount != 0 {
					if len(strings.Fields(prepared.Phrase)) != tc.wordCount {
						t.Fatal("generated phrase changed the selected word count")
					}
				} else if prepared.Phrase != tc.custom {
					t.Fatal("preparation changed the exact chosen secret bytes")
				}
				for _, mismatch := range tc.mismatches {
					connections := queryInt(t, db, "SELECT COUNT(*) FROM instance_connections")
					wrong := serving.invite(t, mismatch)
					if _, err := viewing.service.Pair(t.Context(), service.LocalPrincipal(root), service.PairRemoteRequest{
						Name: "chosen-secret", RequesterState: prepared.RequesterState, InviteBundle: wrong.InviteBundle,
					}); err == nil {
						t.Fatal("different case, whitespace or long suffix authenticated as the chosen secret")
					}
					if n := queryInt(t, db, "SELECT COUNT(*) FROM instance_connections"); n != connections {
						t.Fatal("wrong chosen secret minted a remote connection")
					}
					if n := queryInt(t, viewing.db, "SELECT COUNT(*) FROM remotes"); n != 0 {
						t.Fatal("wrong chosen secret committed a local remote")
					}
				}
				created := serving.invite(t, prepared.Phrase)
				view, err := viewing.service.Pair(t.Context(), service.LocalPrincipal(root), service.PairRemoteRequest{
					Name: "chosen-secret", RequesterState: prepared.RequesterState, InviteBundle: created.InviteBundle,
				})
				if err != nil {
					t.Fatalf("pair exact chosen secret over real TLS: %v", err)
				}
				if view.State != string(remotefetch.OutcomeOK) || view.OrgCount != 2 || view.ProjectCount != 3 {
					t.Fatal("chosen secret did not unlock the real metadata directory")
				}
				if err := viewing.remotes.RemoveRemote(t.Context(), service.LocalPrincipal(root), view.Name); err != nil {
					t.Fatalf("remove completed case's local remote: %v", err)
				}
			})
		}
	})
}

func TestRemotePairingPreparationRejectsInvalidSecretOptions(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		i := newPairingInstance(t, db, nil)
		empty, oversized, invalidUTF8, short := "", strings.Repeat("x", 4097), string([]byte{0xff}), "abcd"
		zero, over, six := 0, 129, 6
		for name, req := range map[string]service.PreparePairingRequest{
			"explicit-empty":      {Phrase: &empty},
			"oversized-bytes":     {Phrase: &oversized},
			"invalid-utf8":        {Phrase: &invalidUTF8},
			"zero-words":          {WordCount: &zero},
			"too-many-words":      {WordCount: &over},
			"conflicting-options": {Phrase: &short, WordCount: &six},
		} {
			t.Run(name, func(t *testing.T) {
				if _, err := i.service.Prepare(t.Context(), service.LocalPrincipal(root), req); !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("invalid secret options result: %v", err)
				}
			})
		}
	})
}
