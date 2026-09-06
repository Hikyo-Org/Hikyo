package backupreceipt

import (
	"context"
	"encoding/binary"
	"errors"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// EvidenceMaterial is untrusted public input. Private operator custody is never
// accepted by a server admission verifier.
type EvidenceMaterial struct{ Receipt, Attestation, Signature []byte }

// VerifiedEvidence proves exact ciphertext, a real operator signature, an
// authenticated route and the observed authority domain at verification time.
// It is not reusable admission: F2 must recheck expiry and consume its nonce in
// the same transaction that installs the complete pending operation.
type VerifiedEvidence struct {
	valid     bool
	receipt   Receipt
	statement Attestation
	digest    releaseidentity.Digest
}

func (e VerifiedEvidence) Valid() bool                    { return e.valid }
func (e VerifiedEvidence) Digest() releaseidentity.Digest { return e.digest }
func (e VerifiedEvidence) Receipt() Receipt {
	r := e.receipt
	r.Snapshot = r.Snapshot.Clone()
	return r
}
func (e VerifiedEvidence) Statement() Attestation {
	a := e.statement
	a.BridgeSHA256 = slices.Clone(a.BridgeSHA256)
	return a
}

// VerifyEvidence must receive authority inspected under the authoritative
// transaction, never Snapshot fields copied out of the receipt itself.
func VerifyEvidence(ctx context.Context, pin PinnedOperator, plan upgradecompat.Plan, ciphertext *Ciphertext, material EvidenceMaterial, live LiveSource, now time.Time) (VerifiedEvidence, error) {
	if live.Validate() != nil {
		return VerifiedEvidence{}, errors.New("invalid current ledger authority")
	}
	receipt, err := ParseReceipt(material.Receipt)
	if err != nil {
		return VerifiedEvidence{}, err
	}
	s := receipt.Snapshot
	if s.Authority != LedgerAuthority || s.InstanceID != live.InstanceID || s.Engine != live.Engine || s.SourceIdentity != live.Source || s.SourceSchemaSHA256 != live.SourceSchemaSHA256 || s.MigrationSHA256 != live.MigrationSHA256 || s.RestoreEpoch != live.RestoreEpoch || s.RecoveryIncarnation != live.RecoveryIncarnation || s.SourceGeneration != live.Generation {
		return VerifiedEvidence{}, errors.New("backup receipt differs from current ledger authority")
	}
	return verifyBoundEvidence(ctx, pin, plan, ciphertext, material, receipt, now)
}

// VerifyLegacyEvidence matches actual pre-ledger facts separately from an
// explicitly proposed incarnation. It never manufactures or returns live state.
func VerifyLegacyEvidence(ctx context.Context, pin PinnedOperator, plan upgradecompat.Plan, ciphertext *Ciphertext, material EvidenceMaterial, inspected LegacyInspection, proposal LegacyProposal, now time.Time) (VerifiedEvidence, error) {
	if inspected.Validate() != nil || proposal.Validate() != nil {
		return VerifiedEvidence{}, errors.New("invalid legacy inspection or bootstrap proposal")
	}
	receipt, err := ParseReceipt(material.Receipt)
	if err != nil {
		return VerifiedEvidence{}, err
	}
	s := receipt.Snapshot
	if s.Authority != LegacyProposalAuthority || s.SourceIdentity.Genesis != releaseidentity.LegacyGenesisV1 || s.InstanceID != inspected.InstanceID || s.Engine != inspected.Engine || s.SourceSchemaSHA256 != inspected.SchemaSHA256 || s.MigrationSHA256 != inspected.MigrationSHA256 || s.RestoreEpoch != inspected.RestoreEpoch || s.RecoveryIncarnation != proposal.RecoveryIncarnation || s.SourceGeneration != 0 || s.RouteGeneration != 1 {
		return VerifiedEvidence{}, errors.New("backup receipt differs from inspected legacy source or proposal")
	}
	return verifyBoundEvidence(ctx, pin, plan, ciphertext, material, receipt, now)
}

func verifyBoundEvidence(ctx context.Context, pin PinnedOperator, plan upgradecompat.Plan, ciphertext *Ciphertext, material EvidenceMaterial, receipt Receipt, now time.Time) (VerifiedEvidence, error) {
	if ciphertext == nil {
		return VerifiedEvidence{}, errors.New("upgrade evidence requires pinned ciphertext")
	}
	statement, err := checkBoundEvidence(pin, plan, material, receipt, now)
	if err != nil {
		return VerifiedEvidence{}, err
	}
	if err := ciphertext.Check(ctx, receipt); err != nil {
		return VerifiedEvidence{}, err
	}
	return VerifiedEvidence{valid: true, receipt: receipt, statement: statement, digest: evidenceDigest(material)}, nil
}

// checkBoundEvidence checks public signed material only. It grants no live
// datastore authority and is shared by execution and read-only preflight.
func checkBoundEvidence(pin PinnedOperator, plan upgradecompat.Plan, material EvidenceMaterial, receipt Receipt, now time.Time) (Attestation, error) {
	if !pin.Valid() || !plan.Valid() || now.IsZero() || len(material.Signature) == 0 || len(material.Signature) > MaxSignatureBytes {
		return Attestation{}, errors.New("upgrade evidence requires pinned authority and route")
	}
	s := receipt.Snapshot
	if err := CheckReceiptPlan(receipt, plan); err != nil {
		return Attestation{}, err
	}
	if pin.InstanceID() != s.InstanceID {
		return Attestation{}, errors.New("backup snapshot differs from current installation pin")
	}

	a, err := ParseAttestation(material.Attestation)
	if err != nil {
		return Attestation{}, err
	}
	bridges := plan.BridgeDigests()
	slices.Sort(bridges)
	if a.Authority != s.Authority || a.ReceiptSHA256 != releaseidentity.Hash(material.Receipt) || a.RouteSHA256 != plan.Digest() || !slices.Equal(a.BridgeSHA256, bridges) || a.TargetIdentity != plan.Target() || a.InstanceID != s.InstanceID || a.RestoreEpoch != s.RestoreEpoch || a.RecoveryIncarnation != s.RecoveryIncarnation || a.SourceGeneration != s.SourceGeneration || a.RouteGeneration != s.RouteGeneration || a.OperatorKeyID != pin.KeyID() || a.IssuedAt.Before(s.CreatedAt) || a.IssuedAt.After(now) || !now.Before(a.ExpiresAt) {
		return Attestation{}, errors.New("upgrade attestation binding or validity refused")
	}
	if err := releasetrust.VerifyOperatorSignature(pin.public, material.Signature, material.Attestation); err != nil {
		return Attestation{}, errors.New("upgrade attestation operator signature refused")
	}
	return a, nil
}

// Length framing makes the digest unambiguous without re-encoding signed bytes.
func evidenceDigest(m EvidenceMaterial) releaseidentity.Digest {
	raw := []byte("hikyo-upgrade-evidence/v1\x00")
	for _, part := range [][]byte{m.Receipt, m.Attestation, m.Signature} {
		raw = binary.BigEndian.AppendUint64(raw, uint64(len(part)))
		raw = append(raw, part...)
	}
	return releaseidentity.Hash(raw)
}

// CheckReceiptPlan compares public snapshot claims with an authenticated route.
// It proves neither private readability nor current database authority.
func CheckReceiptPlan(receipt Receipt, plan upgradecompat.Plan) error {
	if receipt.Validate() != nil || !plan.Valid() {
		return errors.New("receipt needs authenticated route")
	}
	s := receipt.Snapshot
	manifest, err := plan.SourceManifest(s.Engine)
	if err != nil {
		return err
	}
	digest, err := manifest.Digest()
	if err != nil || digest != s.MigrationSHA256 || plan.Source() != s.SourceIdentity || plan.SourceSchemaDigest() != s.SourceSchemaSHA256 {
		return errors.New("backup snapshot differs from authenticated route")
	}
	return nil
}
