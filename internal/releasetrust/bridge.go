package releasetrust

import (
	"errors"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
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
	case "hikyo.dev/recovery-bridge/v1":
		if statement.SourceGenesis != "" || statement.Source.Validate() != nil || statement.Target.Sequence <= statement.Source.Sequence || statement.SourcePolicySHA256.Validate() != nil {
			return VerifiedBridge{}, errors.New("invalid recovery bridge source")
		}
	case "hikyo.dev/legacy-nightly-bridge/v1":
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
