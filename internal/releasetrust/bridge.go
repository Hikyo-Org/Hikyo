package releasetrust

import (
	"errors"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Masterminds/semver/v3"
)

type BridgeStatement struct {
	Schema             string                            `json:"schema"`
	Source             releaseidentity.Identity          `json:"source,omitzero"`
	SourceGenesis      string                            `json:"source_genesis,omitempty"`
	Target             releaseidentity.Identity          `json:"target"`
	SourcePolicySHA256 releaseidentity.Digest            `json:"source_policy_sha256,omitempty"`
	TargetPolicySHA256 releaseidentity.Digest            `json:"target_policy_sha256"`
	SourceMigrations   releaseidentity.MigrationManifest `json:"source_migrations"`
	TargetMigrations   releaseidentity.MigrationManifest `json:"target_migrations"`
	SourceSchemaSHA256 releaseidentity.Digest            `json:"source_schema_sha256"`
	TargetSchemaSHA256 releaseidentity.Digest            `json:"target_schema_sha256"`
	Mode               string                            `json:"mode"`
	// NotAfter is the canonical UTC instant (RFC 3339, "Z") after which a bridge is
	// no longer usable. Every runtime-valid bridge carries one: the recovery v2 and
	// legacy nightly v2 schemas require it, and the expiry-free v1 forms are refused.
	NotAfter string `json:"not_after,omitempty"`
}

// BridgeSchemaV2 adds a signed expiry to the recovery bridge.
const BridgeSchemaV2 = "hikyo.dev/recovery-bridge/v2"

// LegacyNightlyBridgeSchemaV2 adds the same signed expiry to the legacy nightly
// bridge. Its v1 form (no expiry) is refused at runtime: the owner re-signs a v2
// statement offline at the ceremony, choosing not_after; agents never sign one.
const LegacyNightlyBridgeSchemaV2 = "hikyo.dev/legacy-nightly-bridge/v2"

// notAfter parses the canonical expiry; only the exact "Z" form is accepted so
// the signed bytes have one spelling.
func notAfter(value string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339, value)
	if err != nil || at.UTC().Format(time.RFC3339) != value {
		return time.Time{}, errors.New("recovery bridge not_after must be canonical UTC RFC 3339")
	}
	return at, nil
}

// Expired reports whether the bridge may no longer be used at now. The host clock
// is the only clock consulted. A statement without a valid expiry (which
// VerifyBridge never returns) counts as expired.
func (b VerifiedBridge) Expired(now time.Time) bool {
	if b.state == nil {
		return true
	}
	at, err := notAfter(b.state.statement.NotAfter)
	return err != nil || !now.Before(at)
}

// SourceIdentity preserves the inspected legacy genesis without inventing a
// signed release identity or policy for a pre-ledger database.
func (s BridgeStatement) SourceIdentity() releaseidentity.Source {
	return releaseidentity.Source{Genesis: s.SourceGenesis, Release: s.Source}
}

type BridgeMaterial struct{ Statement, Signature []byte }
type bridgeState struct {
	statement BridgeStatement
	digest    releaseidentity.Digest
	snapshot  releaseidentity.Digest
}

// VerifiedBridge authorizes only the exact exceptional edge. Target release
// authenticity and the independent instance attestation remain mandatory.
type VerifiedBridge struct{ state *bridgeState }

func (b VerifiedBridge) Valid() bool { return b.state != nil }
func (b VerifiedBridge) Digest() releaseidentity.Digest {
	if b.state == nil {
		return ""
	}
	return b.state.digest
}
func (b VerifiedBridge) SnapshotDigest() releaseidentity.Digest {
	if b.state == nil {
		return ""
	}
	return b.state.snapshot
}
func (b VerifiedBridge) Statement() BridgeStatement {
	if b.state == nil {
		return BridgeStatement{}
	}
	statement := b.state.statement
	statement.SourceMigrations = statement.SourceMigrations.Clone()
	statement.TargetMigrations = statement.TargetMigrations.Clone()
	return statement
}

func VerifyBridge(snapshot Snapshot, material BridgeMaterial) (VerifiedBridge, error) {
	if !snapshot.Valid() {
		return VerifiedBridge{}, errors.New("unverified trust snapshot")
	}
	digest := releaseidentity.Hash(material.Statement)
	if !slices.Contains(snapshot.state.catalog.Bridges, digest) {
		return VerifiedBridge{}, errors.New("bridge absent from current recovery authorization")
	}
	if err := VerifyKeySignature(snapshot.state.recoveryKey, material.Signature, material.Statement); err != nil {
		return VerifiedBridge{}, err
	}
	var statement BridgeStatement
	if err := decodeDocument(material.Statement, &statement); err != nil {
		return VerifiedBridge{}, err
	}
	if statement.Mode != "maintenance" || statement.Target.Validate() != nil || statement.TargetPolicySHA256.Validate() != nil {
		return VerifiedBridge{}, errors.New("invalid recovery bridge identity or mode")
	}
	switch statement.Schema {
	case BridgeSchemaV2:
		// The unbounded v1 recovery statement is refused: every recovery bridge expires.
		if _, err := notAfter(statement.NotAfter); err != nil {
			return VerifiedBridge{}, err
		}
		if statement.SourceGenesis != "" || statement.Source.Validate() != nil || statement.Target.Sequence <= statement.Source.Sequence || statement.SourcePolicySHA256.Validate() != nil {
			return VerifiedBridge{}, errors.New("invalid recovery bridge source")
		}
		if statement.Source.Profile == releaseidentity.NightlyV1 && statement.Target.Profile == releaseidentity.StableV1 {
			source, _ := semver.StrictNewVersion(statement.Source.Version) // identities validated above
			target, _ := semver.StrictNewVersion(statement.Target.Version)
			if releaseidentity.CompareUpdateVersions(target, source) <= 0 {
				return VerifiedBridge{}, errors.New("stable target must be newer than the nightly base version")
			}
		}
	case "hikyo.dev/legacy-nightly-bridge/v1":
		return VerifiedBridge{}, errors.New("legacy nightly bridge v1 has no expiry and is refused; it needs a v2 statement signed by the owner")
	case LegacyNightlyBridgeSchemaV2:
		if _, err := notAfter(statement.NotAfter); err != nil {
			return VerifiedBridge{}, err
		}
		if statement.SourceGenesis != releaseidentity.LegacyGenesisV1 || statement.Source != (releaseidentity.Identity{}) || statement.SourcePolicySHA256 != "" || statement.Target.Profile != releaseidentity.NightlyV1 || len(statement.SourceMigrations.Entries) == 0 {
			return VerifiedBridge{}, errors.New("invalid legacy nightly bridge source or target")
		}
	default:
		return VerifiedBridge{}, errors.New("unknown recovery bridge schema")
	}
	if statement.SourceMigrations.Validate() != nil || statement.TargetMigrations.Validate() != nil || statement.SourceMigrations.Engine != statement.TargetMigrations.Engine || statement.SourceSchemaSHA256.Validate() != nil || statement.TargetSchemaSHA256.Validate() != nil {
		return VerifiedBridge{}, errors.New("invalid recovery bridge migration binding")
	}
	return VerifiedBridge{state: &bridgeState{statement: statement, digest: digest, snapshot: snapshot.Digest()}}, nil
}
