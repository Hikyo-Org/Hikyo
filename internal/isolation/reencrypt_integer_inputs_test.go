package isolation

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestReencryptGeneratedIntegerInputsRefuseNarrowing(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		for _, input := range []struct {
			name     string
			limit    int
			dek, row uint32
		}{
			{"valid", 10, 1, 1}, {"limit", math.MaxInt32 + 1, 1, 1}, {"dek", 10, math.MaxUint32, 1}, {"row", 10, 1, math.MaxUint32},
		} {
			t.Run(input.name, func(t *testing.T) {
				err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
					proof, err := az.Authorize(ctx, authz.Identity{Principal: root}, authz.OpReencryptInstance, domain.Scope{})
					if err != nil {
						return err
					}
					if input.name == "limit" {
						_, err = repos.Reencrypt().ListPasswordCredsForReencrypt(ctx, proof, "", input.limit)
						return err
					}
					_, err = repos.Reencrypt().ReencryptSelfConfigSeedInput(ctx, proof, "absent-input", []byte("fixture"), []byte("old fixture"), input.dek, input.row)
					return err
				})
				if db.Engine() == store.EnginePostgres && input.name != "valid" {
					if err == nil || !strings.Contains(err.Error(), "PostgreSQL integer out of range") {
						t.Fatalf("wide %s input did not fail before query: %v", input.name, err)
					}
				} else if err != nil {
					t.Fatalf("representable %s input failed: %v", input.name, err)
				}
			})
		}
	})
}
