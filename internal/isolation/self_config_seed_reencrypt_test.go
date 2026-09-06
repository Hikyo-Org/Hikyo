package isolation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func seedReencryptInput(t *testing.T, db *store.DB, ciphertext []byte) {
	t.Helper()
	reencExec(t, db, tctx(t),
		`INSERT INTO self_config_seed_attestations(node_id,schema_version,fingerprint,heartbeat_at) VALUES('seed-node',2,'seed-fingerprint','2026-09-01T00:00:00Z')`,
		`INSERT INTO self_config_seed_attestations(node_id,schema_version,fingerprint,heartbeat_at) VALUES('seed-node',2,'seed-fingerprint','2026-09-01T00:00:00Z')`)
	insertReencryptInput(t, db, ciphertext)
}

func insertReencryptInput(t *testing.T, db *store.DB, ciphertext []byte) {
	t.Helper()
	reencExec(t, db, tctx(t),
		`INSERT INTO self_config_seed_inputs(node_id,owner_instance_id,incarnation,fingerprint,ciphertext,dek_version) VALUES('seed-node','seed-owner','seed-incarnation','seed-fingerprint',?,1)`,
		`INSERT INTO self_config_seed_inputs(node_id,owner_instance_id,incarnation,fingerprint,ciphertext,dek_version) VALUES('seed-node','seed-owner','seed-incarnation','seed-fingerprint',$1,1)`, ciphertext)
}

func TestSelfConfigSeedReencryptRoundTrip(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *store.DB{"sqlite": openSQLite, "postgres": openPostgres} {
		t.Run(name, func(t *testing.T) {
			db := seededDB(t, open)
			kr := probeKeyring(t, db)
			ctx := tctx(t)
			aad := crypto.InstanceFieldAAD{OwnerTable: "self_config_seed_inputs", OwnerRowID: "seed-node", FieldTag: "inputs"}
			ct, err := kr.ForInstance().SealField(aad, []byte(`{"HIKYO_SMTP_PASSWORD":"seed-value"}`))
			if err != nil {
				t.Fatal(err)
			}
			seedReencryptInput(t, db, ct)
			rotation := &service.Rotation{DB: db, Keyring: kr, RootKey: probeRootSource{db: db}}
			if _, err := rotation.RotateDEK(ctx, service.LocalPrincipal(root), service.DEKScope{Instance: true}); err != nil {
				t.Fatal(err)
			}
			re := &service.Reencrypt{DB: db, Keyring: kr, ChunkSize: 1, ChunkPause: -1, AfterChunk: injectRetryOnce("self_config_seed_inputs")}
			res, err := re.ReencryptInstance(ctx, service.LocalPrincipal(root))
			if err != nil {
				t.Fatal(err)
			}
			if res.RowsMoved != 1 {
				t.Fatalf("moved %d, want one seed", res.RowsMoved)
			}
			reencCompletedRowsMoved(t, db, ctx, "instance", 1)
			stored := reencReadBlob(t, db, ctx, "SELECT ciphertext FROM self_config_seed_inputs WHERE node_id='seed-node'")
			if v, err := crypto.RecordKeyVersion(stored); err != nil || v != 2 {
				t.Fatalf("seed version %d: %v", v, err)
			}
			if pt, err := kr.ForInstance().OpenField(aad, stored); err != nil || string(pt) != `{"HIKYO_SMTP_PASSWORD":"seed-value"}` {
				t.Fatal("seed plaintext changed", err)
			}
			for _, other := range []crypto.InstanceFieldAAD{
				{OwnerTable: "self_config_seed_inputs", OwnerRowID: "other-node", FieldTag: "inputs"},
				{OwnerTable: "self_config_seed_inputs", OwnerRowID: "seed-node", FieldTag: "value"},
				{OwnerTable: "remotes", OwnerRowID: "seed-node", FieldTag: "inputs"},
			} {
				if _, err := kr.ForInstance().OpenField(other, stored); err == nil {
					t.Fatal("seed opened across AAD boundary")
				}
			}
			got := reencQueryStrings(t, db, ctx, "SELECT owner_instance_id || ':' || incarnation || ':' || fingerprint || ':' || dek_version || ':' || row_version FROM self_config_seed_inputs WHERE node_id='seed-node'")
			if len(got) != 1 || got[0] != "seed-owner:seed-incarnation:seed-fingerprint:2:2" {
				t.Fatalf("seed metadata changed: %v", got)
			}
			states, err := queryTier3StatesPurpose(db, ctx, "instance", "", "")
			if err != nil || states[1] != "retired" {
				t.Fatalf("old seed DEK not retired: %v %v", states, err)
			}
		})
	}
}

func TestSelfConfigSeedReencryptCAS(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *store.DB{"sqlite": openSQLite, "postgres": openPostgres} {
		t.Run(name, func(t *testing.T) {
			db := seededDB(t, open)
			seedReencryptInput(t, db, []byte("old-ciphertext"))
			ctx := tctx(t)
			cas := func(old []byte, rowVersion uint32, want bool) {
				t.Helper()
				err := storetx.Write(ctx, db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
					p, err := az.Authorize(ctx, authz.Identity{Principal: root, Class: domain.ClassHuman}, authz.OpReencryptInstance, domain.Scope{})
					if err != nil {
						return err
					}
					did, err := r.Reencrypt().ReencryptSelfConfigSeedInput(ctx, p, "seed-node", []byte("new-ciphertext"), old, 2, rowVersion)
					if err != nil {
						return err
					}
					if did != want {
						return fmt.Errorf("CAS changed=%v, want %v", did, want)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			cas([]byte("old-ciphertext"), 2, false)
			cas([]byte("other-ciphertext"), 1, false)
			cas([]byte("old-ciphertext"), 1, true)
			cas([]byte("old-ciphertext"), 1, false)
			reencExec(t, db, ctx, "DELETE FROM self_config_seed_inputs WHERE node_id='seed-node'", "DELETE FROM self_config_seed_inputs WHERE node_id='seed-node'")
			cas([]byte("new-ciphertext"), 2, false)
			if got := reencQueryStrings(t, db, ctx, "SELECT node_id FROM self_config_seed_inputs"); len(got) != 0 {
				t.Fatal("deleted input resurrected")
			}
			insertReencryptInput(t, db, []byte("replacement-ciphertext"))
			cas([]byte("old-ciphertext"), 1, false)
			if got := reencReadBlob(t, db, ctx, "SELECT ciphertext FROM self_config_seed_inputs WHERE node_id='seed-node'"); string(got) != "replacement-ciphertext" {
				t.Fatal("recreated input overwritten by stale walk")
			}
		})
	}
}

func TestSelfConfigSeedReencryptRetireFence(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *store.DB{"sqlite": openSQLite, "postgres": openPostgres} {
		for _, mode := range []string{"straggler", "demoted"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				db := seededDB(t, open)
				kr := probeKeyring(t, db)
				ctx := tctx(t)
				aad := crypto.InstanceFieldAAD{OwnerTable: "self_config_seed_inputs", OwnerRowID: "seed-node", FieldTag: "inputs"}
				ct, err := kr.ForInstance().SealField(aad, []byte("seed-value"))
				if err != nil {
					t.Fatal(err)
				}
				seedReencryptInput(t, db, ct)
				rotation := &service.Rotation{DB: db, Keyring: kr, RootKey: probeRootSource{db: db}}
				if _, err := rotation.RotateDEK(ctx, service.LocalPrincipal(root), service.DEKScope{Instance: true}); err != nil {
					t.Fatal(err)
				}
				re := &service.Reencrypt{DB: db, Keyring: kr, ChunkSize: 1, ChunkPause: -1}
				re.BeforeRetire = func(ctx context.Context) error {
					if mode == "demoted" {
						_, err := rotation.RotateDEK(ctx, service.LocalPrincipal(root), service.DEKScope{Instance: true})
						return err
					}
					reencExec(t, db, ctx,
						"UPDATE self_config_seed_inputs SET ciphertext=?,dek_version=1,row_version=row_version+1 WHERE node_id='seed-node'",
						"UPDATE self_config_seed_inputs SET ciphertext=$1,dek_version=1,row_version=row_version+1 WHERE node_id='seed-node'", ct)
					return nil
				}
				if _, err := re.ReencryptInstance(ctx, service.LocalPrincipal(root)); !errors.Is(err, domain.ErrConflict) {
					t.Fatalf("retire ignored %s: %v", mode, err)
				}
				states, err := queryTier3StatesPurpose(db, ctx, "instance", "", "")
				if err != nil || states[1] == "retired" {
					t.Fatalf("retired seed DEK after failed gate: %v %v", states, err)
				}
			})
		}
	}
}
