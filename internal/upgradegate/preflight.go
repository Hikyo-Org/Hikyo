package upgradegate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// ConfigurationProof contains advisory digests, never admission or evidence
// consumption authority. StateDigest is rechecked before signing submit;
// MaterialDigest follows the selected artifacts across replacement/restart.
type ConfigurationProof struct {
	MaterialDigest string
	StateDigest    string
}

type ConfigurationPreflight struct {
	Request          Request
	InstalledCustody os.FileInfo
	TargetManifest   string
	CiphertextPath   string
}

// PreflightConfiguration checks a proposed source without opening runtime
// stores, locks, pin copies or migrations. The serving process must already be
// healthy on this exact binary. Execution repeats authority checks in Run.
func PreflightConfiguration(ctx context.Context, input ConfigurationPreflight) (ConfigurationProof, error) {
	raw, _, err := buildcompat.Current()
	if err != nil {
		return ConfigurationProof{}, err
	}
	return preflightConfiguration(ctx, input, raw, buildcompat.Verify, true)
}

// CheckStartupConfiguration authenticates the already selected artifacts before
// Run. Historical evidence is checked at its signed issuance time here: Run
// alone decides if new evidence is needed and enforces real-time expiry under
// migration exclusion. This check cannot revive or consume historical evidence.
func CheckStartupConfiguration(ctx context.Context, input ConfigurationPreflight) (ConfigurationProof, error) {
	raw, _, err := buildcompat.Current()
	if err != nil {
		return ConfigurationProof{}, err
	}
	return preflightConfiguration(ctx, input, raw, buildcompat.Verify, false)
}

func preflightConfiguration(ctx context.Context, input ConfigurationPreflight, build []byte, verifyBuild func(upgradecompat.VerifiedNode) error, preparing bool) (ConfigurationProof, error) {
	refusal := func(err error) (ConfigurationProof, error) { return ConfigurationProof{}, err }
	request := input.Request
	if err := ctx.Err(); err != nil {
		return refusal(err)
	}
	state, err := upgrade.InspectControl(ctx, request.Store)
	if err != nil {
		return refusal(err)
	}
	if state.TrustDomain != upgrade.Production || state.ReleaseRootDigest != releaseidentity.Hash(request.Pinned.Root) {
		return refusal(errors.New("upgrade configuration differs from installed production trust"))
	}
	bundle, err := upgradebundle.Load(ctx, request.BundleDirectory, request.Pinned, state.Floor)
	if err != nil {
		return refusal(err)
	}
	node, err := bundle.MatchBuild(build)
	if err != nil {
		return refusal(err)
	}
	if err := verifyBuild(node); err != nil {
		return refusal(err)
	}
	manifest, err := releaseidentity.BuildMigrationManifest(request.Migrations, request.MigrationDirectory, request.Store.Engine)
	if err != nil {
		return refusal(err)
	}
	declared, err := node.Manifest(request.Store.Engine)
	if err != nil {
		return refusal(err)
	}
	actualDigest, _ := manifest.Digest()
	declaredDigest, _ := declared.Digest()
	if actualDigest != declaredDigest {
		return refusal(errors.New("embedded migration bytes differ from verified build"))
	}
	custody, custodyRaw, err := inspectOperatorCustody(request.StateDirectory, input.InstalledCustody)
	if err != nil {
		return refusal(err)
	}
	if err := custody.check(state); err != nil {
		return refusal(err)
	}
	operator, err := custody.pin(state.InstanceID)
	if err != nil {
		return refusal(err)
	}
	configured, err := backupreceipt.PinOperator(state.InstanceID, request.InitialOperatorPublicKey)
	if err != nil || configured.KeyID() != operator.KeyID() {
		return refusal(errors.New("configured operator key differs from installed custody; use explicit operator rotation"))
	}
	target := node.Identity()
	if input.TargetManifest != "" {
		target = releaseidentity.Identity{}
		digest := releaseidentity.Digest(input.TargetManifest)
		if digest.Validate() != nil {
			return refusal(errors.New("invalid upgrade target manifest digest"))
		}
		for _, candidate := range bundle.Sources(request.Store.Engine) {
			if candidate.Identity.IsRelease() && candidate.Identity.Release.ManifestSHA256 == digest {
				target = candidate.Identity.Release
			}
		}
		if target == (releaseidentity.Identity{}) {
			return refusal(errors.New("upgrade target absent from authenticated bundle"))
		}
	}
	if preparing {
		if state.Maintenance || state.Pending == nil || state.Pending.Invalidated || state.Pending.Phase != upgrade.Healthy || state.Applied != (releaseidentity.Source{Release: node.Identity()}) {
			return refusal(errors.New("upgrade configuration apply requires the healthy installed executing build"))
		}
		observed, source, err := inspectSource(ctx, request.Store, bundle)
		if err != nil {
			return refusal(err)
		}
		if observed.Ledger == nil || observed.Ledger.Generation != state.Generation || observed.Ledger.RecoveryIncarnation != state.RecoveryIncarnation || observed.Source != state.Applied || observed.SchemaDigest != state.SchemaDigest {
			return refusal(upgrade.ErrConflict)
		}
		if _, err := bundle.Plan(source, target); err != nil {
			return refusal(err)
		}
	}
	material := struct {
		Bundle      releaseidentity.Digest
		Operator    releaseidentity.Digest
		Receipt     releaseidentity.Digest
		Attestation releaseidentity.Digest
		Signature   releaseidentity.Digest
		Ciphertext  releaseidentity.Digest
	}{Bundle: bundle.MaterialDigest(), Operator: releaseidentity.Hash(request.InitialOperatorPublicKey)}

	evidence := request.Evidence
	if len(evidence.Receipt) > 0 || len(evidence.Attestation) > 0 || len(evidence.Signature) > 0 || input.CiphertextPath != "" {
		receipt, err := backupreceipt.ParseReceipt(evidence.Receipt)
		if err != nil {
			return refusal(err)
		}
		var source upgradecompat.InstalledSource
		for _, candidate := range bundle.Sources(request.Store.Engine) {
			if candidate.Identity == receipt.Snapshot.SourceIdentity && candidate.SchemaSHA256 == receipt.Snapshot.SourceSchemaSHA256 {
				source = candidate
				break
			}
		}
		statement, err := backupreceipt.ParseAttestation(evidence.Attestation)
		if err != nil {
			return refusal(err)
		}
		evidenceTarget := target
		if input.TargetManifest == "" {
			evidenceTarget = statement.TargetIdentity
		}
		plan, err := bundle.Plan(source, evidenceTarget)
		if err != nil {
			return refusal(err)
		}
		at := time.Now()
		if !preparing {
			at = statement.IssuedAt
		} else {
			snapshot := receipt.Snapshot
			incarnation, _ := state.RecoveryIncarnation.MarshalText()
			if snapshot.Authority != backupreceipt.LedgerAuthority || snapshot.InstanceID != state.InstanceID || snapshot.Engine != request.Store.Engine || snapshot.SourceIdentity != state.Applied || snapshot.SourceSchemaSHA256 != state.SchemaDigest || snapshot.MigrationSHA256 != state.MigrationDigest || snapshot.RestoreEpoch != state.RestoreEpoch || snapshot.RecoveryIncarnation != backupreceipt.Nonce(incarnation) || snapshot.SourceGeneration != state.Generation {
				return refusal(errors.New("upgrade evidence differs from current installed authority"))
			}
		}
		if err := backupreceipt.CheckEvidenceArtifacts(ctx, operator, plan, input.CiphertextPath, evidence, at); err != nil {
			return refusal(err)
		}
		material.Receipt = releaseidentity.Hash(evidence.Receipt)
		material.Attestation = releaseidentity.Hash(evidence.Attestation)
		material.Signature = releaseidentity.Hash(evidence.Signature)
		material.Ciphertext = receipt.CiphertextSHA256
	} else if request.LegacyWritersStopped {
		return refusal(errors.New("legacy writer assertion requires separate signed evidence"))
	}
	encoded, err := json.Marshal(material)
	if err != nil {
		return refusal(err)
	}
	rawState, err := json.Marshal(struct {
		State   upgrade.State
		Build   releaseidentity.Digest
		Target  releaseidentity.Identity
		Custody releaseidentity.Digest
	}{State: state, Build: releaseidentity.Hash(build), Target: target, Custody: releaseidentity.Hash(custodyRaw)})
	if err != nil {
		return refusal(err)
	}
	if err := ctx.Err(); err != nil {
		return refusal(err)
	}
	return ConfigurationProof{MaterialDigest: string(releaseidentity.Hash(encoded)), StateDigest: string(releaseidentity.Hash(rawState))}, nil
}
