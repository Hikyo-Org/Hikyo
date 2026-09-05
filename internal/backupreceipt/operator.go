package backupreceipt

import (
	"bytes"
	"errors"
	"math"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

// PinnedOperator is constructed only from current installation custody. Never
// call PinOperator with a key found in an archive, receipt or signature bundle.
type PinnedOperator struct {
	instance string
	id       releaseidentity.Digest
	public   []byte
}

func PinOperator(instance string, publicKey []byte) (PinnedOperator, error) {
	if !instancePattern.MatchString(instance) || len(publicKey) == 0 || len(publicKey) > MaxArtifactBytes {
		return PinnedOperator{}, errors.New("invalid installation operator pin")
	}
	id, err := releasetrust.OperatorKeyID(publicKey)
	if err != nil {
		return PinnedOperator{}, errors.New("invalid installation operator public key")
	}
	return PinnedOperator{instance: instance, id: id, public: bytes.Clone(publicKey)}, nil
}

func (p PinnedOperator) KeyID() releaseidentity.Digest { return p.id }
func (p PinnedOperator) InstanceID() string            { return p.instance }
func (p PinnedOperator) Valid() bool {
	return instancePattern.MatchString(p.instance) && p.id.Validate() == nil && len(p.public) > 0
}

// LiveSource is read under the authoritative gate's transaction. It carries
// claims, not permission. F5 must construct it from F2's current ledger, never
// by copying fields from the receipt it is trying to verify.
type LiveSource struct {
	InstanceID          string
	Engine              releaseidentity.Engine
	Source              releaseidentity.Source
	SourceSchemaSHA256  releaseidentity.Digest
	MigrationSHA256     releaseidentity.Digest
	RestoreEpoch        int64
	RecoveryIncarnation Nonce
	Generation          int64
}

func (s LiveSource) Validate() error {
	if !instancePattern.MatchString(s.InstanceID) || s.Engine.Validate() != nil || s.Source.Validate() != nil || !s.Source.IsRelease() || s.MigrationSHA256.Validate() != nil || s.RestoreEpoch < 0 || s.RecoveryIncarnation.Validate() != nil || s.Generation < 1 || s.Generation == math.MaxInt64 {
		return errors.New("invalid live upgrade source")
	}
	if s.SourceSchemaSHA256.Validate() != nil {
		return errors.New("live source requires an actual schema fingerprint")
	}
	return nil
}

// LegacyInspection contains only facts inspected from the pre-ledger snapshot.
// It deliberately has no generation or recovery incarnation.
type LegacyInspection struct {
	InstanceID      string
	Engine          releaseidentity.Engine
	SchemaSHA256    releaseidentity.Digest
	MigrationSHA256 releaseidentity.Digest
	RestoreEpoch    int64
}

func (s LegacyInspection) Validate() error {
	if !instancePattern.MatchString(s.InstanceID) || s.Engine.Validate() != nil || s.SchemaSHA256.Validate() != nil || s.MigrationSHA256.Validate() != nil || s.RestoreEpoch < 0 {
		return errors.New("invalid inspected legacy source")
	}
	return nil
}

// LegacyProposal is explicitly proposed authority. Only F2's atomic bootstrap
// may persist it alongside the complete pending operation and consumed nonce.
type LegacyProposal struct{ RecoveryIncarnation Nonce }

func NewLegacyProposal() (LegacyProposal, error) {
	nonce, err := NewNonce()
	return LegacyProposal{RecoveryIncarnation: nonce}, err
}

func (p LegacyProposal) Validate() error { return p.RecoveryIncarnation.Validate() }

type RotationMode string

const (
	PriorKeyRotation RotationMode = "prior-key"
	LocalBreakGlass  RotationMode = "local-break-glass"
)

// Rotation binds a public key change to the existing authority domain. The
// break-glass mode additionally requires F5's explicit local recovery custody;
// a signature by the proposed new key alone never authorizes that transition.
type Rotation struct {
	Format                  string                 `json:"format"`
	Mode                    RotationMode           `json:"mode"`
	InstanceID              string                 `json:"instance_id"`
	RecoveryIncarnation     Nonce                  `json:"recovery_incarnation"`
	RestoreEpoch            int64                  `json:"restore_epoch"`
	MaxKnownCredentialEpoch int64                  `json:"max_known_credential_epoch"`
	NextEpoch               int64                  `json:"next_epoch"`
	CurrentKeyID            releaseidentity.Digest `json:"current_key_id"`
	NewKeyID                releaseidentity.Digest `json:"new_key_id"`
	IssuedAt                time.Time              `json:"issued_at"`
}

func (r Rotation) Validate() error {
	if r.Format != RotationFormat || (r.Mode != PriorKeyRotation && r.Mode != LocalBreakGlass) || !instancePattern.MatchString(r.InstanceID) || r.RecoveryIncarnation.Validate() != nil || r.RestoreEpoch < 0 || r.MaxKnownCredentialEpoch < r.RestoreEpoch || r.MaxKnownCredentialEpoch == math.MaxInt64 || r.NextEpoch != r.MaxKnownCredentialEpoch+1 || r.CurrentKeyID.Validate() != nil || r.NewKeyID.Validate() != nil || r.NewKeyID == r.CurrentKeyID || !canonicalTime(r.IssuedAt) {
		return errors.New("invalid operator key transition")
	}
	return nil
}

func ParseRotation(raw []byte) (Rotation, error) {
	var r Rotation
	if err := decodeClosed(raw, &r, []string{"format", "mode", "instance_id", "recovery_incarnation", "restore_epoch", "max_known_credential_epoch", "next_epoch", "current_key_id", "new_key_id", "issued_at"}); err != nil {
		return Rotation{}, err
	}
	return r, r.Validate()
}

// KeyTransition authenticates a transition request, not its application. F5
// owns the epoch/pending CAS and durable installation-pin journal. A request
// with RequiresLocalRecovery()==true must never reach an HTTP/controller gate.
type KeyTransition struct {
	valid     bool
	statement Rotation
	digest    releaseidentity.Digest
	next      PinnedOperator
}

func (t KeyTransition) Valid() bool                    { return t.valid }
func (t KeyTransition) RequiresLocalRecovery() bool    { return t.statement.Mode == LocalBreakGlass }
func (t KeyTransition) Statement() Rotation            { return t.statement }
func (t KeyTransition) Digest() releaseidentity.Digest { return t.digest }
func (t KeyTransition) NextOperator() PinnedOperator   { return t.next }

// RotationSource separates the ledger's restore authority from the strongest
// actual credential stamp, which can legitimately advance on ordinary rotation.
type RotationSource struct {
	Live                    LiveSource
	MaxKnownCredentialEpoch int64
}

func VerifyKeyTransition(current PinnedOperator, newPublicKey, statement, bundle []byte, source RotationSource, now time.Time) (KeyTransition, error) {
	live := source.Live
	if source.MaxKnownCredentialEpoch < live.RestoreEpoch || !current.Valid() || live.Validate() != nil || current.instance != live.InstanceID || len(bundle) == 0 || len(bundle) > MaxSignatureBytes {
		return KeyTransition{}, errors.New("operator transition needs current installation authority")
	}
	r, err := ParseRotation(statement)
	if err != nil {
		return KeyTransition{}, err
	}
	next, err := PinOperator(live.InstanceID, newPublicKey)
	if err != nil {
		return KeyTransition{}, err
	}
	if r.InstanceID != live.InstanceID || r.RecoveryIncarnation != live.RecoveryIncarnation || r.RestoreEpoch != live.RestoreEpoch || r.MaxKnownCredentialEpoch != source.MaxKnownCredentialEpoch || r.CurrentKeyID != current.id || r.NewKeyID != next.id || r.IssuedAt.After(now) {
		return KeyTransition{}, errors.New("operator transition does not match current authority")
	}
	signer := current
	if r.Mode == LocalBreakGlass {
		signer = next
	}
	if releasetrust.VerifyOperatorSignature(signer.public, bundle, statement) != nil {
		return KeyTransition{}, errors.New("operator transition signature refused")
	}
	return KeyTransition{valid: true, statement: r, digest: releaseidentity.Hash(statement), next: next}, nil
}

// CheckOperatorSignature validates exact public statement bytes against current
// installation custody. It does not validate that statement's semantic claims.
func CheckOperatorSignature(pin PinnedOperator, bundle, payload []byte) error {
	if !pin.Valid() || len(bundle) == 0 || len(bundle) > MaxSignatureBytes || len(payload) == 0 || len(payload) > MaxArtifactBytes {
		return errors.New("operator signature needs bounded public material and current pin")
	}
	if releasetrust.VerifyOperatorSignature(pin.public, bundle, payload) != nil {
		return errors.New("current operator signature refused")
	}
	return nil
}
