package upgrade

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

//go:embed genesis/*.json
var genesisFS embed.FS

// GenesisDeclaration is pinned source evidence, not a claimed released binary.
type GenesisDeclaration struct {
	ID           string                            `json:"id"`
	SourceCommit string                            `json:"source_commit"`
	Catalog      Catalog                           `json:"catalog"`
	Migrations   releaseidentity.MigrationManifest `json:"migrations"`
}

func legacyDeclaration(engine releaseidentity.Engine) (GenesisDeclaration, error) {
	raw, err := genesisFS.ReadFile("genesis/" + string(engine) + ".json")
	if err != nil {
		return GenesisDeclaration{}, err
	}
	var declaration GenesisDeclaration
	if err := json.Unmarshal(raw, &declaration); err != nil {
		return GenesisDeclaration{}, err
	}
	if declaration.ID != LegacyGenesis || declaration.SourceCommit != "373910cca63eb8a2e2e6b5a079cc285b64a2ae95" || declaration.Catalog.Engine != engine || declaration.Migrations.Engine != engine || declaration.Catalog.Format != "hikyo-schema/v1" {
		return GenesisDeclaration{}, ErrGenesis
	}
	if err := declaration.Migrations.Validate(); err != nil {
		return GenesisDeclaration{}, err
	}
	return declaration, nil
}

type Inspection struct {
	Genesis            string                 `json:"genesis"`
	CatalogDigest      releaseidentity.Digest `json:"catalog_digest"`
	MigrationDigest    releaseidentity.Digest `json:"migration_digest"`
	RequiresLegacyStop bool                   `json:"requires_legacy_stop"`
	RequiresBackup     bool                   `json:"requires_backup"`
}

func validateGenesis(c Catalog, manifest releaseidentity.MigrationManifest) (Inspection, error) {
	if manifest.Engine != c.Engine {
		return Inspection{}, ErrGenesis
	}
	if err := manifest.Validate(); err != nil {
		return Inspection{}, err
	}
	if controlPresent(c) {
		return Inspection{}, fmt.Errorf("%w: existing or partial control schema", ErrGenesis)
	}
	fresh := len(c.Objects) == 0
	if c.Engine == releaseidentity.Postgres {
		fresh = len(c.Objects) == 1 && c.Objects[0] == `["schema", "public"]`
	}
	if fresh && len(c.Applied) == 0 {
		if len(manifest.Entries) != 0 {
			return Inspection{}, ErrGenesis
		}
		digest, _ := manifest.Digest()
		return Inspection{Genesis: FreshGenesis, CatalogDigest: c.Digest(), MigrationDigest: digest}, nil
	}
	declaration, err := legacyDeclaration(c.Engine)
	if err != nil {
		return Inspection{}, err
	}
	if !reflect.DeepEqual(c, declaration.Catalog) || !reflect.DeepEqual(manifest, declaration.Migrations) {
		return Inspection{}, ErrGenesis
	}
	digest, _ := manifest.Digest()
	return Inspection{Genesis: LegacyGenesis, CatalogDigest: c.Digest(), MigrationDigest: digest, RequiresLegacyStop: true, RequiresBackup: true}, nil
}

// Inspect is strictly read-only. In particular it never calls goose, sets WAL,
// creates a missing SQLite database, or installs the control schema. A successful
// legacy result reports mandatory stop/backup prerequisites, not their proof.
func Inspect(ctx context.Context, cfg Config, manifest releaseidentity.MigrationManifest) (Inspection, error) {
	if err := cfg.Engine.Validate(); err != nil {
		return Inspection{}, err
	}
	if cfg.Engine == releaseidentity.SQLite {
		path, err := canonicalSQLite(cfg.Path)
		if err != nil {
			return Inspection{}, err
		}
		cfg.Path = path
		if _, err := checkedFile(path); errors.Is(err, os.ErrNotExist) {
			return validateGenesis(Catalog{Format: "hikyo-schema/v1", Engine: cfg.Engine, Objects: []string{}, Applied: []int64{}}, manifest)
		} else if err != nil {
			return Inspection{}, err
		}
	}
	db, err := open(cfg, true)
	if err != nil {
		return Inspection{}, err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return Inspection{}, err
	}
	defer conn.Close()
	begin := "BEGIN"
	if cfg.Engine == releaseidentity.Postgres {
		begin = "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY"
	}
	if _, err := conn.ExecContext(ctx, begin); err != nil {
		return Inspection{}, err
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
	catalog, err := inspectCatalog(ctx, conn, cfg.Engine)
	if err != nil {
		return Inspection{}, err
	}
	return validateGenesis(catalog, manifest)
}

// Bootstrap inspects first, then creates the schema and complete pending row in
// one transaction under the existing engine lock. No ordinary migration runs.
// Fresh genesis generates its incarnation internally. Legacy requires an explicit
// operator-generated proposal, authenticated by F4/F5 before this transaction;
// this storage primitive does not establish the proposal's trust.
func (s *Session) Bootstrap(ctx context.Context, manifest releaseidentity.MigrationManifest, operation Operation, domain TrustDomain) (State, error) {
	var state State
	if operation.Kind == "" {
		operation.Kind = UpgradeOperation
	}
	if operation.Kind != UpgradeOperation {
		return State{}, ErrConflict
	}
	if err := domain.Validate(); err != nil {
		return State{}, err
	}
	err := s.transaction(ctx, func() error {
		catalog, err := inspectCatalog(ctx, s.conn, s.engine)
		if err != nil {
			return err
		}
		report, err := validateGenesis(catalog, manifest)
		if err != nil {
			return err
		}
		if report.Genesis == LegacyGenesis && domain != Production {
			return ErrGenesis
		}
		if operation.Source != (Source{Genesis: report.Genesis}) || operation.Hop != 0 || operation.SourceSchemaDigest != report.CatalogDigest || operation.SourceMigrationDigest != report.MigrationDigest || operation.Phase != Prepared || operation.Generation != 1 || operation.Invalidated {
			return ErrConflict
		}
		incarnation := operation.RecoveryIncarnation
		if report.Genesis == FreshGenesis {
			if incarnation != (Incarnation{}) {
				return ErrConflict
			}
			incarnation, err = newIncarnation()
			if err != nil {
				return err
			}
		} else if incarnation == (Incarnation{}) {
			return ErrConflict
		}
		operation.RecoveryIncarnation = incarnation
		var instance string
		var epoch int64
		if report.Genesis == FreshGenesis {
			var id [16]byte
			if _, err := rand.Read(id[:]); err != nil {
				return err
			}
			instance = "ins_" + hex.EncodeToString(id[:])
		} else {
			if err := s.conn.QueryRowContext(ctx, `SELECT identity FROM instance_identity WHERE id=1`).Scan(&instance); err != nil {
				return err
			}
			var credential, restored int64
			if err := s.conn.QueryRowContext(ctx, `SELECT credential_epoch,restore_epoch FROM auth_instance_state WHERE id=1`).Scan(&credential, &restored); err != nil {
				return err
			}
			epoch = max(credential, restored)
		}
		state = State{SchemaDigest: report.CatalogDigest, Floor: operation.Acceptance.Floor, ReleaseRootDigest: operation.Acceptance.ReleaseRootDigest, TrustDomain: domain, InstanceID: instance, Applied: operation.Source, MigrationDigest: report.MigrationDigest, RestoreEpoch: epoch, RecoveryIncarnation: incarnation, Generation: 1, Maintenance: true, Pending: &operation}
		if err := state.Validate(); err != nil {
			return err
		}
		if _, err := s.conn.ExecContext(ctx, controlDDL); err != nil {
			return err
		}
		if _, err := s.conn.ExecContext(ctx, pendingDDL); err != nil {
			return err
		}
		if _, err := s.conn.ExecContext(ctx, nonceDDL); err != nil {
			return err
		}
		if err := s.accept(ctx, nil, &state); err != nil {
			return err
		}
		return s.persist(ctx, state, true)
	})
	if err != nil {
		return State{}, err
	}
	return state, nil
}

// PinnedLegacyManifest returns an owned copy for tooling/inspection. It conveys
// no installed-state or release authority; Bootstrap still checks the catalog.
func PinnedLegacyManifest(engine releaseidentity.Engine) (releaseidentity.MigrationManifest, error) {
	d, err := legacyDeclaration(engine)
	if err != nil {
		return releaseidentity.MigrationManifest{}, err
	}
	return d.Migrations.Clone(), nil
}

// PinnedLegacySchemaDigest exposes the exact approved source fingerprint to
// release tooling. It is a structural declaration, never proof of live state.
func PinnedLegacySchemaDigest(engine releaseidentity.Engine) (releaseidentity.Digest, error) {
	d, err := legacyDeclaration(engine)
	if err != nil {
		return "", err
	}
	return d.Catalog.Digest(), nil
}

// MarshalInspection emits only public structural prerequisites, never raw rows.
func MarshalInspection(report Inspection) ([]byte, error) { return json.Marshal(report) }
