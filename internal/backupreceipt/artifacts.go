// Package backupreceipt owns public upgrade-backup evidence. Parsed claims are
// not authority; only verified signatures, pinned ciphertext and live bindings
// can produce an admission result. Private custody never enters these schemas.
package backupreceipt

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

const (
	ReceiptFormat                = "backup-receipt/v1"
	AttestationFormat            = "upgrade-attestation/v1"
	RotationFormat               = "operator-key-rotation/v1"
	MaxArtifactBytes             = 64 << 10
	MaxSignatureBytes            = 1 << 20
	MaxCiphertextBytes     int64 = 1 << 40
	MaxRecipients                = 64
	MaxBridges                   = 32
	MaxAttestationLifetime       = 24 * time.Hour
)

// Nonce identifies a one-use assertion or recovery incarnation. It is a
// nonzero 256-bit CSPRNG value encoded as fixed lowercase hexadecimal.
type Nonce string

func NewNonce() (Nonce, error) {
	var raw [32]byte
	if _, err := io.ReadFull(rand.Reader, raw[:]); err != nil {
		return "", errors.New("upgrade evidence randomness unavailable")
	}
	return Nonce(hex.EncodeToString(raw[:])), nil
}

func (n Nonce) Validate() error {
	if releaseidentity.Digest(n).Validate() != nil || string(n) == strings.Repeat("0", 64) {
		return errors.New("invalid upgrade evidence nonce")
	}
	return nil
}

var instancePattern = regexp.MustCompile(`^ins_[0-9a-f]{32}$`)

// AuthorityKind distinguishes persisted authority from a signed proposal that
// F5 must install atomically with the complete legacy bootstrap and nonce.
type AuthorityKind string

const (
	LedgerAuthority         AuthorityKind = "applied-ledger/v1"
	LegacyProposalAuthority AuthorityKind = "legacy-proposal/v1"
)

func validGeneration(kind AuthorityKind, source, route int64) bool {
	if kind == LegacyProposalAuthority {
		return source == 0 && route == 1
	}
	return kind == LedgerAuthority && source >= 1 && source < math.MaxInt64 && route == source+1
}

// Snapshot is read from the actual archived database, never from a separate
// live preflight or a caller's previous-version claim. It is also included in
// the encrypted manifest, which cannot contain its own ciphertext digest.
type Snapshot struct {
	Authority             AuthorityKind          `json:"authority"`
	BackupID              Nonce                  `json:"backup_id"`
	InstanceID            string                 `json:"instance_id"`
	Engine                releaseidentity.Engine `json:"engine"`
	SourceIdentity        releaseidentity.Source `json:"source_identity"`
	SourceSchemaSHA256    releaseidentity.Digest `json:"source_schema_sha256"`
	MigrationSHA256       releaseidentity.Digest `json:"migration_set_sha256"`
	RestoreEpoch          int64                  `json:"restore_epoch"`
	RecoveryIncarnation   Nonce                  `json:"recovery_incarnation"`
	SourceGeneration      int64                  `json:"source_generation"`
	RouteGeneration       int64                  `json:"route_generation"`
	CreatedAt             time.Time              `json:"created_at"`
	RecipientFingerprints []string               `json:"recipient_fingerprints"`
}

func (s Snapshot) Clone() Snapshot {
	s.RecipientFingerprints = slices.Clone(s.RecipientFingerprints)
	return s
}

func (s Snapshot) Validate() error {
	if s.BackupID.Validate() != nil || !instancePattern.MatchString(s.InstanceID) || s.Engine.Validate() != nil ||
		s.SourceIdentity.Validate() != nil || s.MigrationSHA256.Validate() != nil || s.RecoveryIncarnation.Validate() != nil ||
		s.SourceIdentity.Genesis == releaseidentity.FreshGenesisV1 || s.RestoreEpoch < 0 || !validGeneration(s.Authority, s.SourceGeneration, s.RouteGeneration) || !canonicalTime(s.CreatedAt) {
		return errors.New("invalid backup snapshot identity")
	}
	if s.SourceSchemaSHA256.Validate() != nil || (s.SourceIdentity.IsRelease() && s.Authority != LedgerAuthority) || (!s.SourceIdentity.IsRelease() && s.Authority != LegacyProposalAuthority) {
		return errors.New("invalid source schema fingerprint")
	}
	if len(s.RecipientFingerprints) == 0 || len(s.RecipientFingerprints) > MaxRecipients || !slices.IsSorted(s.RecipientFingerprints) {
		return errors.New("invalid backup recipient inventory")
	}
	for index, fingerprint := range s.RecipientFingerprints {
		digest, ok := strings.CutPrefix(fingerprint, "age-x25519-sha256:")
		if !ok || releaseidentity.Digest(digest).Validate() != nil || (index > 0 && s.RecipientFingerprints[index-1] == fingerprint) {
			return errors.New("invalid backup recipient fingerprint")
		}
	}
	return nil
}

func (s *Snapshot) UnmarshalJSON(raw []byte) error {
	type wire Snapshot
	var decoded wire
	if err := decodeClosed(raw, &decoded, []string{"authority", "backup_id", "instance_id", "engine", "source_identity", "source_schema_sha256", "migration_set_sha256", "restore_epoch", "recovery_incarnation", "source_generation", "route_generation", "created_at", "recipient_fingerprints"}); err != nil {
		return err
	}
	*s = Snapshot(decoded)
	var members map[string]json.RawMessage
	_ = json.Unmarshal(raw, &members)
	var source map[string]json.RawMessage
	if json.Unmarshal(members["source_identity"], &source) != nil || len(source) != 1 {
		return errors.New("source identity must name exactly one source variant")
	}
	for name, value := range source {
		if (name != "genesis" && name != "release") || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("invalid source identity variant")
		}
	}
	return s.Validate()
}

// Receipt is the public final commit marker for an exact encrypted archive.
// ManifestSHA256 hashes the exact decrypted manifest member, not a re-encoding.
type Receipt struct {
	Format           string                 `json:"format"`
	CiphertextSHA256 releaseidentity.Digest `json:"ciphertext_sha256"`
	CiphertextBytes  int64                  `json:"ciphertext_bytes"`
	ManifestSHA256   releaseidentity.Digest `json:"encrypted_manifest_sha256"`
	Snapshot         Snapshot               `json:"snapshot"`
}

func (r Receipt) Validate() error {
	if r.Format != ReceiptFormat || r.CiphertextSHA256.Validate() != nil || r.ManifestSHA256.Validate() != nil || r.CiphertextBytes <= 0 || r.CiphertextBytes > MaxCiphertextBytes {
		return errors.New("invalid backup receipt")
	}
	return r.Snapshot.Validate()
}

func ParseReceipt(raw []byte) (Receipt, error) {
	var r Receipt
	if err := decodeClosed(raw, &r, []string{"format", "ciphertext_sha256", "ciphertext_bytes", "encrypted_manifest_sha256", "snapshot"}); err != nil {
		return Receipt{}, err
	}
	return r, r.Validate()
}

type Attestation struct {
	Authority           AuthorityKind            `json:"authority"`
	Format              string                   `json:"format"`
	ReceiptSHA256       releaseidentity.Digest   `json:"receipt_sha256"`
	RouteSHA256         releaseidentity.Digest   `json:"route_sha256"`
	BridgeSHA256        []releaseidentity.Digest `json:"bridge_sha256"`
	TargetIdentity      releaseidentity.Identity `json:"target_identity"`
	InstanceID          string                   `json:"instance_id"`
	RestoreEpoch        int64                    `json:"restore_epoch"`
	RecoveryIncarnation Nonce                    `json:"recovery_incarnation"`
	SourceGeneration    int64                    `json:"source_generation"`
	RouteGeneration     int64                    `json:"route_generation"`
	OperatorKeyID       releaseidentity.Digest   `json:"operator_key_id"`
	IssuedAt            time.Time                `json:"issued_at"`
	ExpiresAt           time.Time                `json:"expires_at"`
	Nonce               Nonce                    `json:"nonce"`
}

func (a Attestation) Validate() error {
	if a.Format != AttestationFormat || a.ReceiptSHA256.Validate() != nil || a.RouteSHA256.Validate() != nil || a.TargetIdentity.Validate() != nil ||
		!instancePattern.MatchString(a.InstanceID) || a.RestoreEpoch < 0 || a.RecoveryIncarnation.Validate() != nil || a.SourceGeneration < 0 ||
		!validGeneration(a.Authority, a.SourceGeneration, a.RouteGeneration) || a.OperatorKeyID.Validate() != nil || a.Nonce.Validate() != nil ||
		!canonicalTime(a.IssuedAt) || !canonicalTime(a.ExpiresAt) || !a.ExpiresAt.After(a.IssuedAt) || a.ExpiresAt.Sub(a.IssuedAt) > MaxAttestationLifetime {
		return errors.New("invalid upgrade attestation")
	}
	if a.BridgeSHA256 == nil || len(a.BridgeSHA256) > MaxBridges || !slices.IsSorted(a.BridgeSHA256) {
		return errors.New("invalid upgrade bridge inventory")
	}
	for i, digest := range a.BridgeSHA256 {
		if digest.Validate() != nil || (i > 0 && a.BridgeSHA256[i-1] == digest) {
			return errors.New("invalid upgrade bridge digest")
		}
	}
	return nil
}

func ParseAttestation(raw []byte) (Attestation, error) {
	var a Attestation
	if err := decodeClosed(raw, &a, []string{"authority", "format", "receipt_sha256", "route_sha256", "bridge_sha256", "target_identity", "instance_id", "restore_epoch", "recovery_incarnation", "source_generation", "route_generation", "operator_key_id", "issued_at", "expires_at", "nonce"}); err != nil {
		return Attestation{}, err
	}
	return a, a.Validate()
}

func canonicalTime(t time.Time) bool {
	return !t.IsZero() && t.Location() == time.UTC && t.Nanosecond() == 0 && t.Year() >= 1970 && t.Year() <= 9999
}

// Missing or null zero-valued counters are not the same as an explicit zero.
// Exact member spelling also prevents case-insensitive JSON struct matching
// from creating two wire spellings of a supposedly closed public artifact.
func decodeClosed(raw []byte, target any, fields []string) error {
	if len(raw) == 0 || len(raw) > MaxArtifactBytes || definitions.DecodeStrict(raw, target) != nil {
		return errors.New("invalid closed upgrade artifact")
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil || len(members) != len(fields) {
		return errors.New("missing upgrade artifact fields")
	}
	for _, field := range fields {
		if value, ok := members[field]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("missing or null upgrade artifact field")
		}
		if field == "created_at" || field == "issued_at" || field == "expires_at" {
			var value string
			if json.Unmarshal(members[field], &value) != nil {
				return errors.New("invalid upgrade artifact timestamp")
			}
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil || parsed.UTC().Format(time.RFC3339) != value {
				return errors.New("upgrade artifact timestamp must be canonical UTC seconds")
			}
		}
	}
	return nil
}
