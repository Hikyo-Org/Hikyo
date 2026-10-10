package isolation

import (
	"context"
	"errors"
	"testing"

	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestDeveloperUnknownCredentialFloodBoundsDurableRefusals(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		limiter, err := admission.New(admission.Config{ArgonMemoryKiB: 64 * 1024})
		if err != nil {
			t.Fatal(err)
		}
		const source = "192.0.2.81"
		before := queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.artifact_class_refused'`)
		for i := 0; i < admission.MetaPerIPPerMinute+15; i++ {
			token, _, err := crypto.NewArtifact(crypto.ArtifactDeveloper)
			if err != nil {
				t.Fatal(err)
			}
			ctx := audit.WithContext(t.Context(), audit.Context{SourceIP: source, Origin: audit.OriginAPI})
			ctx = operation.WithPreauthenticationAdmission(ctx, func() error { return limiter.AdmitDiscovery(source) })
			err = tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
				_, err := az.AuthenticateCaller(ctx, token, time.Now().UTC())
				return err
			})
			if i < admission.MetaPerIPPerMinute {
				if !errors.Is(err, domain.ErrUnauthenticated) {
					t.Fatalf("admitted request %d: %v", i, err)
				}
			} else if !errors.Is(err, admission.ErrOverloaded) {
				t.Fatalf("flood request %d: %v", i, err)
			}
		}
		after := queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.artifact_class_refused'`)
		if after-before != admission.MetaPerIPPerMinute {
			t.Fatalf("durable refusal count %d, want admitted allowance %d", after-before, admission.MetaPerIPPerMinute)
		}
	})
}
