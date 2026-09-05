package upgrade

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

func freshHierarchyFixture(t *testing.T, cfg Config) State {
	t.Helper()
	empty := emptyManifest(cfg.Engine)
	operation := operation(Source{Genesis: FreshGenesis}, empty)
	manifest, err := PinnedLegacyManifest(cfg.Engine)
	if err != nil {
		t.Fatal(err)
	}
	operation.TargetMigrationDigest, err = manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	operation.TargetSchemaDigest, err = PinnedLegacySchemaDigest(cfg.Engine)
	if err != nil {
		t.Fatal(err)
	}
	var state State
	err = WithLock(t.Context(), cfg, func(session *Session) error {
		var err error
		state, err = session.Bootstrap(t.Context(), empty, operation, Production)
		if err != nil {
			return err
		}
		state, err = session.Advance(t.Context(), state, SchemaWriteStarted)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateFixture(t, cfg); err != nil {
		t.Fatal(err)
	}
	err = WithLock(t.Context(), cfg, func(session *Session) error {
		var err error
		state, err = session.Advance(t.Context(), state, SchemaApplied)
		if err != nil {
			return err
		}
		return session.ReconcileFreshInstance(t.Context(), state)
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestFreshHierarchyIsAtomicAndCannotBeReinitialized(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		state := freshHierarchyFixture(t, cfg)
		root, err := crypto.GenerateRootKey()
		if err != nil {
			t.Fatal(err)
		}
		defer crypto.Zero(root)
		err = WithLock(t.Context(), cfg, func(session *Session) error {
			short := []byte{1, 2, 3}
			if err := session.InitializeFreshHierarchy(t.Context(), state, short); !errors.Is(err, crypto.ErrRootKeyFormat) || !bytes.Equal(short, make([]byte, len(short))) {
				return fmt.Errorf("invalid root refusal/zero: %v", err)
			}
			// A preexisting scope generation forces a real insert failure after
			// master and tier-3 rows were written inside this transaction.
			if _, err := session.conn.ExecContext(t.Context(), "INSERT INTO key_generations(scope,generation) VALUES('tier3:token::',1)"); err != nil {
				return err
			}
			if err := session.InitializeFreshHierarchy(t.Context(), state, slices.Clone(root)); err == nil {
				return errors.New("partial-insert failure accepted")
			}
			for _, table := range []string{"master_keys", "tier3_keys"} {
				var count int
				if err := session.conn.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
					return err
				}
				if count != 0 {
					return errors.New("mid-insert failure retained partial hierarchy")
				}
			}
			if _, err := session.conn.ExecContext(t.Context(), "DELETE FROM key_generations WHERE scope='tier3:token::'"); err != nil {
				return err
			}
			failed := errors.New("injected initialization precommit failure")
			session.beforeCommit = func() error { return failed }
			consumed := slices.Clone(root)
			if err := session.InitializeFreshHierarchy(t.Context(), state, consumed); !errors.Is(err, failed) || !bytes.Equal(consumed, make([]byte, crypto.KeySize)) {
				return fmt.Errorf("atomic failure/zero: %v", err)
			}
			session.beforeCommit = nil
			for _, table := range []string{"master_keys", "tier3_keys"} {
				var count int
				if err := session.conn.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
					return err
				}
				if count != 0 {
					return errors.New("failed initialization committed partial key hierarchy")
				}
			}
			var scopes int
			if err := session.conn.QueryRowContext(t.Context(), "SELECT count(*) FROM key_generations WHERE scope <> 'hierarchy'").Scan(&scopes); err != nil {
				return err
			}
			if scopes != 0 {
				return errors.New("failed initialization committed scope generations")
			}
			if err := session.InitializeFreshHierarchy(t.Context(), state, slices.Clone(root)); err != nil {
				return err
			}
			reader, err := session.CandidateKeys(t.Context(), state)
			if err != nil {
				return err
			}
			master, err := reader.ActiveMasterWrappers(t.Context())
			if err != nil || len(master) != 1 {
				return fmt.Errorf("actual master missing: %v", err)
			}
			tier3, err := reader.AllOpenableTier3(t.Context())
			if err != nil || len(tier3) != 3 {
				return fmt.Errorf("actual tier3 hierarchy missing: %v", err)
			}
			// Opening every real AEAD envelope proves wrappers were created together
			// under the supplied root; the read adapter cannot create missing keys.
			if err := crypto.InitializeFreshHierarchy(t.Context(), &initializedKeys{master: master, tier3: tier3}, slices.Clone(root)); err != nil {
				return err
			}
			if err := session.InitializeFreshHierarchy(t.Context(), state, slices.Clone(root)); !errors.Is(err, ErrConflict) {
				return fmt.Errorf("second initialization accepted: %v", err)
			}
			wrong := bytes.Repeat([]byte{99}, crypto.KeySize)
			if _, err := crypto.LoadKeyring(t.Context(), &initializedKeys{master: master, tier3: tier3}, wrong); !errors.Is(err, crypto.ErrRootKeyMismatch) {
				return fmt.Errorf("wrong root opened hierarchy: %v", err)
			}
			healthy, err := session.Advance(t.Context(), state, Healthy)
			if err != nil {
				return err
			}
			if err := session.InitializeFreshHierarchy(t.Context(), healthy, slices.Clone(root)); !errors.Is(err, ErrConflict) {
				return fmt.Errorf("healthy initialization accepted: %v", err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestFreshHierarchyRefusesPopulationAndLegacy(t *testing.T) {
	for _, population := range []string{"orgs", "principals", "legacy"} {
		t.Run(population, func(t *testing.T) {
			both(t, func(t *testing.T, cfg Config) {
				var state State
				if population == "legacy" {
					if err := migrateFixture(t, cfg); err != nil {
						t.Fatal(err)
					}
					manifest, err := PinnedLegacyManifest(cfg.Engine)
					if err != nil {
						t.Fatal(err)
					}
					err = WithLock(t.Context(), cfg, func(session *Session) error {
						var err error
						state, err = session.Bootstrap(t.Context(), manifest, legacyOperation(t, cfg, manifest), Production)
						return err
					})
					if err != nil {
						t.Fatal(err)
					}
				} else {
					state = freshHierarchyFixture(t, cfg)
				}
				err := WithLock(t.Context(), cfg, func(session *Session) error {
					if population == "orgs" {
						if _, err := session.conn.ExecContext(t.Context(), "INSERT INTO orgs(id,name,active,metadata,created_at) VALUES('org_test','Test',$1,'{}','2026-09-05T00:00:00Z')", true); err != nil {
							return err
						}
					}
					if population == "principals" {
						if _, err := session.conn.ExecContext(t.Context(), "INSERT INTO principals(id,kind,created_at) VALUES('usr_test','human','2026-09-05T00:00:00Z')"); err != nil {
							return err
						}
					}
					root := bytes.Repeat([]byte{42}, crypto.KeySize)
					if err := session.InitializeFreshHierarchy(t.Context(), state, root); !errors.Is(err, ErrConflict) {
						return fmt.Errorf("nonfresh population accepted: %v", err)
					}
					if !bytes.Equal(root, make([]byte, crypto.KeySize)) {
						return errors.New("refused root not consumed")
					}
					var count int
					if err := session.conn.QueryRowContext(t.Context(), "SELECT count(*) FROM master_keys").Scan(&count); err != nil {
						return err
					}
					if count != 0 {
						return errors.New("refusal wrote keys")
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

type initializedKeys struct{ master, tier3 []crypto.WrappedKey }

func (k *initializedKeys) ActiveMasterWrappers(context.Context) ([]crypto.WrappedKey, error) {
	return k.master, nil
}
func (k *initializedKeys) ActiveTier3(_ context.Context, p crypto.Purpose, org, project string) (crypto.WrappedKey, error) {
	for _, key := range k.tier3 {
		if key.Purpose == p && key.OrgID == org && key.ProjectID == project {
			return key, nil
		}
	}
	return crypto.WrappedKey{}, crypto.ErrNoKey
}
func (k *initializedKeys) Tier3Versions(ctx context.Context, p crypto.Purpose, org, project string) ([]crypto.WrappedKey, error) {
	key, err := k.ActiveTier3(ctx, p, org, project)
	if err != nil {
		return nil, err
	}
	return []crypto.WrappedKey{key}, nil
}
func (k *initializedKeys) AllOpenableTier3(context.Context) ([]crypto.WrappedKey, error) {
	return k.tier3, nil
}
func (*initializedKeys) CreateHierarchy(context.Context, crypto.WrappedKey, []crypto.WrappedKey) error {
	return errors.New("readonly fixture")
}
func (*initializedKeys) CreateTier3(context.Context, crypto.WrappedKey) error {
	return errors.New("readonly fixture")
}
