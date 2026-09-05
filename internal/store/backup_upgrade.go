package store

import (
	"archive/tar"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/jackc/pgx/v5"
)

const UpgradeArchiveFormat = "hikyo-upgrade-backup/v2"

// UpgradeExportRequest carries authenticated route authority and public export
// policy. Its proposed legacy incarnation is explicitly separate from facts
// that bindSQLite/bindPostgres read inside the actual archived snapshot.
type UpgradeExportRequest struct {
	preparation    upgrade.PreparationAdmission
	Plan           upgradecompat.Plan
	Recipients     []string
	LegacyProposal *backupreceipt.LegacyProposal
	BackupID       backupreceipt.Nonce
	CreatedAt      time.Time
}

func ExportUpgrade(ctx context.Context, db *DB, w io.Writer, workDir string, request UpgradeExportRequest) (Manifest, error) {
	if db == nil || !request.Plan.Valid() || request.BackupID.Validate() != nil || request.CreatedAt.IsZero() {
		return Manifest{}, errors.New("upgrade export requires authenticated route and public identity")
	}
	request.Recipients = slices.Clone(request.Recipients)
	if request.LegacyProposal != nil {
		proposal := *request.LegacyProposal
		request.LegacyProposal = &proposal
	}
	if _, err := request.Plan.SourceManifest(releaseidentity.Engine(db.engine)); err != nil {
		return Manifest{}, err
	}
	return exportArchive(ctx, db, w, workDir, &request)
}

func (r UpgradeExportRequest) bindSQLite(ctx context.Context, q *sql.DB, m *Manifest) error {
	manifest, err := r.Plan.SourceManifest(releaseidentity.SQLite)
	if err != nil {
		return err
	}
	inspected, err := upgrade.InspectSQLiteSource(ctx, q, manifest)
	if err != nil {
		return err
	}
	return r.bind(inspected, m)
}
func (r UpgradeExportRequest) bindPostgres(ctx context.Context, q pgx.Tx, m *Manifest) error {
	manifest, err := r.Plan.SourceManifest(releaseidentity.Postgres)
	if err != nil {
		return err
	}
	inspected, err := upgrade.InspectPostgresSource(ctx, q, manifest)
	if err != nil {
		return err
	}
	return r.bind(inspected, m)
}
func (r UpgradeExportRequest) bind(inspected upgrade.InstalledSource, m *Manifest) error {
	manifest, err := r.Plan.SourceManifest(releaseidentity.Engine(m.Engine))
	if err != nil {
		return err
	}
	digest, err := manifest.Digest()
	if err != nil {
		return err
	}
	if inspected.Source != r.Plan.Source() || inspected.SchemaDigest != r.Plan.SourceSchemaDigest() || inspected.MigrationDigest != digest {
		return errors.New("actual backup snapshot differs from authenticated source")
	}
	snapshot := backupreceipt.Snapshot{BackupID: r.BackupID, InstanceID: inspected.InstanceID, Engine: releaseidentity.Engine(m.Engine), SourceIdentity: inspected.Source, SourceSchemaSHA256: inspected.SchemaDigest, MigrationSHA256: inspected.MigrationDigest, RestoreEpoch: inspected.RestoreEpoch, CreatedAt: r.CreatedAt, RecipientFingerprints: slices.Clone(r.Recipients)}
	if inspected.Ledger == nil {
		if inspected.Source.Genesis != releaseidentity.LegacyGenesisV1 || r.LegacyProposal == nil || r.LegacyProposal.Validate() != nil {
			return errors.New("populated pre-ledger export requires explicit legacy proposal")
		}
		snapshot.Authority = backupreceipt.LegacyProposalAuthority
		snapshot.RecoveryIncarnation = r.LegacyProposal.RecoveryIncarnation
		snapshot.RouteGeneration = 1
	} else {
		state := inspected.Ledger
		healthy := !state.Maintenance && state.Pending != nil && state.Pending.Phase == upgrade.Healthy && !state.Pending.Invalidated
		restored := r.preparation.Valid() && state.Maintenance && state.Pending != nil && state.Pending.Phase == upgrade.RestoreRequired && state.Pending.Invalidated
		if r.LegacyProposal != nil || (!healthy && !restored) {
			return errors.New("upgrade export requires healthy current source authority")
		}
		raw, err := state.RecoveryIncarnation.MarshalText()
		if err != nil {
			return err
		}
		snapshot.Authority = backupreceipt.LedgerAuthority
		snapshot.RecoveryIncarnation = backupreceipt.Nonce(raw)
		snapshot.SourceGeneration = state.Generation
		snapshot.RouteGeneration = state.Generation + 1
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}
	m.Format = UpgradeArchiveFormat
	m.CreatedAt = r.CreatedAt
	m.Upgrade = &snapshot
	return nil
}

// ManifestDigest hashes the exact encoding written by writeManifest. A reader
// must instead hash original member bytes with ReadManifestEvidence below.
func ManifestDigest(m Manifest) (releaseidentity.Digest, error) {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	return releaseidentity.Hash(raw), nil
}

// ReadManifestEvidence hashes the original bounded decrypted member, without
// re-encoding it. Full container authentication remains the caller's first step.
func ReadManifestEvidence(archive io.ReadSeeker) (Manifest, releaseidentity.Digest, error) {
	start, err := archive.Seek(0, io.SeekCurrent)
	if err != nil {
		return Manifest{}, "", err
	}
	m, err := ReadManifest(archive)
	if err != nil {
		return Manifest{}, "", err
	}
	if _, err := archive.Seek(start, io.SeekStart); err != nil {
		return Manifest{}, "", err
	}
	tr := tar.NewReader(archive)
	header, err := tr.Next()
	if err != nil || header.Name != manifestMember || header.Size > 1<<20 {
		return Manifest{}, "", ErrArchiveFormat
	}
	raw, err := io.ReadAll(io.LimitReader(tr, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return Manifest{}, "", ErrArchiveFormat
	}
	if _, err := archive.Seek(start, io.SeekStart); err != nil {
		return Manifest{}, "", err
	}
	return m, releaseidentity.Hash(raw), nil
}

// VerifyEmbeddedUpgradeSource checks the actual SQL bytes this operator binary
// will use for source-version scratch restoration, including the complete prefix.
func VerifyEmbeddedUpgradeSource(plan upgradecompat.Plan, engine Engine) error {
	expected, err := plan.SourceManifest(releaseidentity.Engine(engine))
	if err != nil {
		return err
	}
	embedded, err := releaseidentity.BuildMigrationManifest(MigrationsFS, "migrations/"+string(engine), releaseidentity.Engine(engine))
	if err != nil {
		return err
	}
	var maximum uint64
	if len(expected.Entries) > 0 {
		maximum = expected.Entries[len(expected.Entries)-1].Version
	}
	embedded.Entries = slices.DeleteFunc(embedded.Entries, func(m releaseidentity.Migration) bool { return m.Version > maximum })
	want, err := expected.Digest()
	if err != nil {
		return err
	}
	got, err := embedded.Digest()
	if err != nil {
		return err
	}
	if got != want {
		return errors.New("operator embedded SQL differs from authenticated source prefix")
	}
	return nil
}
