package upgrade

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
)

// InitializeFreshHierarchy closes the crash window between first migration and
// healthy admission. Only the first schema-applied empty genesis may initialize
// keys, in one transaction on the connection owning physical exclusion. Root is
// consumed on every return; no runtime store or keyring escapes this operation.
func (s *Session) InitializeFreshHierarchy(ctx context.Context, expected State, root []byte) error {
	defer crypto.Zero(root)
	if len(root) != crypto.KeySize {
		return crypto.ErrRootKeyFormat
	}
	if expected.Validate() != nil || expected.Applied.Genesis != FreshGenesis || expected.Generation != 1 || !expected.Maintenance || expected.Pending.Invalidated || expected.Pending.Phase != SchemaApplied || expected.Pending.Hop != 0 || expected.Pending.Source.Genesis != FreshGenesis || expected.Pending.RouteSource.Genesis != FreshGenesis || expected.RestoreEpoch != 0 {
		return ErrConflict
	}
	return s.transaction(ctx, func() error {
		current, err := s.Resume(ctx, expected)
		if err != nil {
			return err
		}
		catalog, err := s.DomainCatalog(ctx)
		if err != nil {
			return err
		}
		if catalog.Digest() != current.Pending.TargetSchemaDigest {
			return ErrConflict
		}
		var instance string
		var credential, restored int64
		if err := s.conn.QueryRowContext(ctx, `SELECT identity FROM instance_identity WHERE id=1`).Scan(&instance); err != nil {
			return err
		}
		if err := s.conn.QueryRowContext(ctx, `SELECT credential_epoch,restore_epoch FROM auth_instance_state WHERE id=1`).Scan(&credential, &restored); err != nil {
			return err
		}
		if instance != current.InstanceID || credential != 1 || restored != 0 {
			return ErrConflict
		}
		for _, table := range []string{"orgs", "principals", "master_keys", "tier3_keys"} {
			var count int64
			if err := s.conn.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return ErrConflict
			}
		}
		// This adapter remains lexically inside the owned SQL transaction. All
		// noninitialization KeyStore operations explicitly refuse.
		return crypto.InitializeFreshHierarchy(ctx, &freshKeyStore{session: s}, root)
	})
}

type freshKeyStore struct {
	session *Session
	created bool
}

var _ crypto.KeyStore = (*freshKeyStore)(nil)

func (f *freshKeyStore) ActiveMasterWrappers(context.Context) ([]crypto.WrappedKey, error) {
	if f.created {
		return nil, ErrConflict
	}
	return nil, nil // The enclosing transaction proved every key table empty.
}
func (*freshKeyStore) ActiveTier3(context.Context, crypto.Purpose, string, string) (crypto.WrappedKey, error) {
	return crypto.WrappedKey{}, ErrConflict
}
func (*freshKeyStore) Tier3Versions(context.Context, crypto.Purpose, string, string) ([]crypto.WrappedKey, error) {
	return nil, ErrConflict
}
func (*freshKeyStore) AllOpenableTier3(context.Context) ([]crypto.WrappedKey, error) {
	return nil, ErrConflict
}
func (*freshKeyStore) CreateTier3(context.Context, crypto.WrappedKey) error { return ErrConflict }
func (f *freshKeyStore) CreateHierarchy(ctx context.Context, master crypto.WrappedKey, tier3 []crypto.WrappedKey) error {
	if f.created || master.Version != 1 || master.RootKeyEpoch != 1 || len(master.Blob) == 0 || len(tier3) != 3 {
		return ErrConflict
	}
	seen := map[crypto.Purpose]bool{}
	for _, key := range tier3 {
		if key.Purpose != crypto.PurposeInstance && key.Purpose != crypto.PurposeToken && key.Purpose != crypto.PurposeScanning {
			return ErrConflict
		}
		if seen[key.Purpose] || key.OrgID != "" || key.ProjectID != "" || key.Version != 1 || key.MasterKeyVersion != 1 || key.ID == "" || len(key.Blob) == 0 {
			return ErrConflict
		}
		seen[key.Purpose] = true
	}
	f.created = true
	now := time.Now().UTC().Truncate(time.Millisecond)
	if f.session.engine == releaseidentity.SQLite {
		queries := sqlitegen.New(f.session.conn)
		if _, err := queries.AcquireHierarchyGeneration(ctx); err != nil {
			return err
		}
		if err := queries.InsertMasterKey(ctx, sqlitegen.InsertMasterKeyParams{Version: 1, RootKeyEpoch: 1, Blob: master.Blob, CreatedAt: now.Format(time.RFC3339Nano)}); err != nil {
			return err
		}
		for _, key := range tier3 {
			if err := queries.InsertTier3Key(ctx, sqlitegen.InsertTier3KeyParams{ID: key.ID, Purpose: string(key.Purpose), Version: 1, MasterKeyVersion: 1, Blob: key.Blob, CreatedAt: now.Format(time.RFC3339Nano)}); err != nil {
				return err
			}
			if err := queries.InsertKeyGeneration(ctx, "tier3:"+string(key.Purpose)+"::"); err != nil {
				return err
			}
		}
		return nil
	}
	return f.session.conn.Raw(func(value any) error {
		connection, ok := value.(*stdlib.Conn)
		if !ok {
			return errors.New("upgrade: unexpected fresh PostgreSQL driver")
		}
		queries := pggen.New(connection.Conn())
		if _, err := queries.AcquireHierarchyGeneration(ctx); err != nil {
			return err
		}
		stamp := pgtype.Timestamptz{Time: now, Valid: true}
		if err := queries.InsertMasterKey(ctx, pggen.InsertMasterKeyParams{Version: 1, RootKeyEpoch: 1, Blob: master.Blob, CreatedAt: stamp}); err != nil {
			return err
		}
		for _, key := range tier3 {
			if err := queries.InsertTier3Key(ctx, pggen.InsertTier3KeyParams{ID: key.ID, Purpose: string(key.Purpose), Version: 1, MasterKeyVersion: 1, Blob: key.Blob, CreatedAt: stamp}); err != nil {
				return err
			}
			if err := queries.InsertKeyGeneration(ctx, "tier3:"+string(key.Purpose)+"::"); err != nil {
				return err
			}
		}
		return nil
	})
}
