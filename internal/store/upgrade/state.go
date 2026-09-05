// Package upgrade stores upgrade state. Its values are claims, not release
// verification or permission to migrate/serve. The shared application gate must
// independently supply trust, exclusion, backup proof and bounded health checks.
package upgrade

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

var (
	ErrAbsent   = errors.New("upgrade: control state absent")
	ErrCorrupt  = errors.New("upgrade: corrupt control state")
	ErrConflict = errors.New("upgrade: stale or changed operation")
	ErrGenesis  = errors.New("upgrade: unrecognized genesis")
)

type TrustDomain string

const (
	Production       TrustDomain = "production"
	LocalDevelopment TrustDomain = "local-development"
)

func (d TrustDomain) Validate() error {
	if d != Production && d != LocalDevelopment {
		return ErrCorrupt
	}
	return nil
}

type Phase string

const (
	Prepared           Phase = "prepared"
	SchemaWriteStarted Phase = "schema-write-started"
	SchemaApplied      Phase = "schema-applied"
	Healthy            Phase = "healthy"
	RestoreRequired    Phase = "restore-required"
	FreshGenesis             = releaseidentity.FreshGenesisV1
	LegacyGenesis            = releaseidentity.LegacyGenesisV1
)

// Source is either a globally verified release CLAIM or an explicit genesis.
// Genesis never masquerades as a released version or trusted envelope.
type Source = releaseidentity.Source

// Incarnation changes on supported restore, never on process restart or upgrade.
// JSON uses a closed fixed-width lowercase hex encoding, not an integer array.
type Incarnation [32]byte

func (i Incarnation) MarshalText() ([]byte, error) { return []byte(hex.EncodeToString(i[:])), nil }
func (i *Incarnation) UnmarshalText(b []byte) error {
	if len(b) != 64 {
		return ErrCorrupt
	}
	decoded, err := hex.DecodeString(string(b))
	if err != nil || hex.EncodeToString(decoded) != string(b) {
		return ErrCorrupt
	}
	copy(i[:], decoded)
	if *i == (Incarnation{}) {
		return ErrCorrupt
	}
	return nil
}

func newIncarnation() (Incarnation, error) {
	var result Incarnation
	if _, err := rand.Read(result[:]); err != nil {
		return result, err
	}
	if result == (Incarnation{}) {
		return result, ErrCorrupt
	}
	return result, nil
}

type OperationKind string

const (
	UpgradeOperation  OperationKind = "upgrade"
	RecoveryOperation OperationKind = "recovery"
)

type Operation struct {
	Kind                  OperationKind            `json:"kind"`
	SourceSchemaDigest    releaseidentity.Digest   `json:"source_schema_digest"`
	TargetSchemaDigest    releaseidentity.Digest   `json:"target_schema_digest"`
	Acceptance            Acceptance               `json:"acceptance"`
	RouteSource           Source                   `json:"route_source"`
	Source                Source                   `json:"source"`
	Target                releaseidentity.Identity `json:"target"`
	SourceMigrationDigest releaseidentity.Digest   `json:"source_migration_digest"`
	TargetMigrationDigest releaseidentity.Digest   `json:"target_migration_digest"`
	RouteDigest           releaseidentity.Digest   `json:"route_digest"`
	Hop                   int64                    `json:"hop"`
	RouteLength           int64                    `json:"route_length"`
	Generation            int64                    `json:"generation"`
	RecoveryIncarnation   Incarnation              `json:"recovery_incarnation"`
	BackupID              string                   `json:"backup_id"`
	Phase                 Phase                    `json:"phase"`
	// Invalidated marks restored historical evidence. It can never resume.
	Invalidated bool `json:"invalidated"`
}

type State struct {
	SchemaDigest        releaseidentity.Digest        `json:"schema_digest"`
	Floor               releaseidentity.SnapshotFloor `json:"floor"`
	ReleaseRootDigest   releaseidentity.Digest        `json:"release_root_digest"`
	TrustDomain         TrustDomain                   `json:"trust_domain"`
	InstanceID          string                        `json:"instance_id"`
	Applied             Source                        `json:"applied"`
	MigrationDigest     releaseidentity.Digest        `json:"migration_digest"`
	RestoreEpoch        int64                         `json:"restore_epoch"`
	RecoveryIncarnation Incarnation                   `json:"recovery_incarnation"`
	Generation          int64                         `json:"generation"`
	Maintenance         bool                          `json:"maintenance"`
	Pending             *Operation                    `json:"pending"`
}

func (s State) Validate() error {
	if err := s.TrustDomain.Validate(); err != nil {
		return err
	}
	if s.InstanceID == "" || len(s.InstanceID) > 256 || s.RestoreEpoch < 0 || s.Generation < 1 || s.RecoveryIncarnation == (Incarnation{}) {
		return ErrCorrupt
	}
	if err := s.Applied.Validate(); err != nil {
		return fmt.Errorf("%w: applied identity", ErrCorrupt)
	}
	if err := s.SchemaDigest.Validate(); err != nil {
		return ErrCorrupt
	}
	if err := s.MigrationDigest.Validate(); err != nil {
		return fmt.Errorf("%w: migration digest", ErrCorrupt)
	}
	if err := s.Floor.Validate(); err != nil || s.Floor.MetadataSequence <= 0 || s.ReleaseRootDigest.Validate() != nil {
		return ErrCorrupt
	}
	p := s.Pending
	if p == nil {
		return ErrCorrupt
	}
	if p.Kind != UpgradeOperation && p.Kind != RecoveryOperation {
		return ErrCorrupt
	}
	if p.Kind == RecoveryOperation {
		if !p.Source.IsRelease() || p.Source.Release != p.Target || p.RouteSource != p.Source || p.Hop != 0 || p.RouteLength != 1 || p.SourceMigrationDigest != p.TargetMigrationDigest || p.SourceSchemaDigest != p.TargetSchemaDigest || p.Phase == SchemaWriteStarted {
			return ErrCorrupt
		}
	}
	if err := p.RouteSource.Validate(); err != nil {
		return ErrCorrupt
	}
	if p.Hop == 0 && p.RouteSource != p.Source {
		return ErrCorrupt
	}
	if err := p.Acceptance.Validate(); err != nil {
		return err
	}
	if p.RouteSource.Genesis != FreshGenesis && p.Acceptance.Attestation == nil {
		return ErrCorrupt
	}
	if s.Floor != p.Acceptance.Floor || s.ReleaseRootDigest != p.Acceptance.ReleaseRootDigest {
		return ErrCorrupt
	}
	if err := p.Source.Validate(); err != nil {
		return ErrCorrupt
	}
	if err := p.Target.Validate(); err != nil {
		return ErrCorrupt
	}
	for _, d := range []releaseidentity.Digest{p.SourceMigrationDigest, p.TargetMigrationDigest, p.RouteDigest, p.SourceSchemaDigest, p.TargetSchemaDigest} {
		if err := d.Validate(); err != nil {
			return ErrCorrupt
		}
	}
	if p.RouteLength < 1 || p.RouteLength > 32 || p.Hop < 0 || p.Hop >= p.RouteLength || p.Generation < 1 || p.RecoveryIncarnation == (Incarnation{}) || len(p.BackupID) > 256 {
		return ErrCorrupt
	}
	if p.RouteSource.Genesis != FreshGenesis && p.BackupID == "" {
		return ErrCorrupt
	}
	if p.Kind == UpgradeOperation && p.Source.Genesis == "" && p.Target.Sequence <= p.Source.Release.Sequence {
		return ErrCorrupt
	}
	if p.Invalidated {
		if !s.Maintenance || p.Phase != RestoreRequired || p.RecoveryIncarnation == s.RecoveryIncarnation {
			return ErrCorrupt
		}
		return nil
	}
	if p.Generation != s.Generation || p.RecoveryIncarnation != s.RecoveryIncarnation {
		return ErrCorrupt
	}
	switch p.Phase {
	case Prepared, SchemaWriteStarted, SchemaApplied, RestoreRequired:
		if !s.Maintenance || s.Applied != p.Source || s.MigrationDigest != p.SourceMigrationDigest || s.SchemaDigest != p.SourceSchemaDigest {
			return ErrCorrupt
		}
	case Healthy:
		if s.Maintenance != (p.Hop+1 < p.RouteLength) || s.Applied != (Source{Release: p.Target}) || s.MigrationDigest != p.TargetMigrationDigest || s.SchemaDigest != p.TargetSchemaDigest {
			return ErrCorrupt
		}
	default:
		return ErrCorrupt
	}
	return nil
}

func nextGeneration(g int64) (int64, error) {
	if g < 0 || g == math.MaxInt64 {
		return 0, ErrConflict
	}
	return g + 1, nil
}
