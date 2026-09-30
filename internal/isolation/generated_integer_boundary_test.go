package isolation

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// PostgreSQL INTEGER must refuse wide values before generated int32 inputs
// wrap; SQLite retains its existing wider integer behavior.
func TestGeneratedIntegerWritesPreserveEngineRange(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		now := time.Now().UTC()
		issuerVersion := int64(1<<32 + 1)
		err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
			proof, err := az.Authorize(ctx, authz.Identity{Principal: root}, authz.OpPKIIssuerCreate, domain.Scope{})
			if err != nil {
				return err
			}
			return repos.PKI().CreateIssuer(ctx, proof, store.PKIIssuerCreate{ID: "pki_range", Name: "range", Version: issuerVersion, Kind: "root", Origin: "generated", State: "pending", KeyAlgorithm: "ed25519", KeyFingerprint: "range", EncryptedPrivateKey: []byte("sealed fixture"), DEKVersion: 1, CSRDER: []byte("csr fixture"), SubjectCN: "range", CreatedBy: string(root), At: now})
		})
		if db.Engine() == store.EnginePostgres {
			if err == nil || queryInt(t, db, "SELECT COUNT(*) FROM pki_issuers WHERE id='pki_range'") != 0 {
				t.Fatalf("wide issuer version wrapped or wrote: %v", err)
			}
		} else if err != nil || int64(queryInt(t, db, "SELECT version FROM pki_issuers WHERE id='pki_range'")) != issuerVersion {
			t.Fatalf("SQLite issuer range changed: %v", err)
		}
		svc, _ := transitEnv(t, db, "range")
		key := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "range-key", Algorithm: "xchacha20-poly1305"})
		execRaw(t, db, fmt.Sprintf("UPDATE transit_keys SET latest_version=%d WHERE id='%s'", math.MaxInt32, key.ID))
		err = storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
			proof, err := az.Authorize(ctx, authz.Identity{Principal: alice}, authz.OpTransitKeyRotate, transitScope)
			if err != nil {
				return err
			}
			return repos.Transit().AppendVersion(ctx, proof, key.ID, math.MaxInt32, 3, store.TransitVersionCreate{ID: "tkv_range", Version: math.MaxInt32 + 1, Sealed: []byte("sealed fixture"), At: now})
		})
		if db.Engine() == store.EnginePostgres {
			if err == nil || queryInt(t, db, "SELECT COUNT(*) FROM transit_key_versions WHERE id='tkv_range'") != 0 || queryInt(t, db, fmt.Sprintf("SELECT latest_version FROM transit_keys WHERE id='%s'", key.ID)) != math.MaxInt32 {
				t.Fatalf("wide transit version wrapped or wrote: %v", err)
			}
		} else if err != nil || queryInt(t, db, "SELECT version FROM transit_key_versions WHERE id='tkv_range'") != math.MaxInt32+1 {
			t.Fatalf("SQLite transit range changed: %v", err)
		}
	})
}
