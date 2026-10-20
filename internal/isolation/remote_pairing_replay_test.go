package isolation

import (
	"crypto/ed25519"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/pairing"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// One authorized peer must not spend another peer's replay admission. Populate
// the durable replay state directly so this tests the isolation boundary without
// an expensive request flood or a timing-dependent throughput requirement.
func TestRemotePairingReplayAdmissionIsolatesPeers(t *testing.T) {
	db := seededDB(t, openSQLite)
	now := time.Now().UTC()
	i := newPairingInstance(t, db, func() time.Time { return now })
	enroll := func() (string, ed25519.PrivateKey) {
		const phrase = "independent peers"
		created := i.invite(t, phrase)
		key, public := pairingTestKey(t)
		started, err := i.service.Start(t.Context(), decodePairingBundle(t, created).Capability, public)
		if err != nil {
			t.Fatal(err)
		}
		message, proof := pairingFinishFixture(t, phrase, started, key, now)
		finished, err := i.service.Finish(t.Context(), started.Exchange, message, proof)
		if err != nil {
			t.Fatal(err)
		}
		return finished.Credential, key
	}
	attackerToken, attackerKey := enroll()
	healthyToken, healthyKey := enroll()
	public, err := pairing.PublicJWK(attackerKey)
	if err != nil {
		t.Fatal(err)
	}
	thumbprint, err := pairing.PublicKeyThumbprint(public)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.SQLiteWrite().ExecContext(t.Context(), `
		WITH RECURSIVE entries(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM entries WHERE n<?)
		INSERT INTO remote_pairing_replays(thumbprint,jti_verifier,expires_at)
		SELECT ?,randomblob(32),? FROM entries`, authz.ReplayKeyLimit, thumbprint, now.Add(pairing.MaxProofAge+pairing.MaxFutureSkew).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	makeProof := func(token string, key ed25519.PrivateKey) string {
		proof, err := pairing.GenerateDPoP(key, pairing.DPoPClaims{Method: http.MethodGet,
			URL: i.server.URL + "/api/v1/instance/directory", AccessToken: token, IssuedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		return proof
	}
	if _, err := i.workspace.Serve(t.Context(), service.PairedConnectionActor(attackerToken, makeProof(attackerToken, attackerKey))); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("saturated peer admission = %v, want limit exceeded", err)
	}
	proof := makeProof(healthyToken, healthyKey)
	if _, err := i.workspace.Serve(t.Context(), service.PairedConnectionActor(healthyToken, proof)); err != nil {
		t.Fatalf("one peer's replay history denied an unrelated healthy peer: %v", err)
	}
	if _, err := i.workspace.Serve(t.Context(), service.PairedConnectionActor(healthyToken, proof)); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("previously admitted proof = %v, want replay refusal", err)
	}
	// Many independent keys can still reach the installation's hard ceiling.
	// Its fail-closed behavior must survive the new per-key isolation boundary.
	_, err = db.SQLiteWrite().ExecContext(t.Context(), `
		WITH RECURSIVE entries(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM entries WHERE n<10000)
		INSERT INTO remote_pairing_replays(thumbprint,jti_verifier,expires_at)
		SELECT 'other-key-'||n,randomblob(32),? FROM entries
		WHERE n<=10000-(SELECT COUNT(*) FROM remote_pairing_replays)`, now.Add(pairing.MaxProofAge+pairing.MaxFutureSkew).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := i.workspace.Serve(t.Context(), service.PairedConnectionActor(healthyToken, makeProof(healthyToken, healthyKey))); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("global replay ceiling = %v, want limit exceeded", err)
	}
	// Expired records free admission; no live record is evicted to do so.
	now = now.Add(71 * time.Second)
	if _, err := i.workspace.Serve(t.Context(), service.PairedConnectionActor(attackerToken, makeProof(attackerToken, attackerKey))); err != nil {
		t.Fatalf("expired replay history still denied its peer: %v", err)
	}
}
