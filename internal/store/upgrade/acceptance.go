package upgrade

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// Acceptance contains only claims copied from F1/F4 verified results by F5.
// Structural validation and durable storage never substitute for verification.
type Acceptance struct {
	Floor             releaseidentity.SnapshotFloor `json:"floor"`
	ReleaseRootDigest releaseidentity.Digest        `json:"release_root_digest"`
	Attestation       *AttestationUse               `json:"attestation"`
}

type AttestationUse struct {
	Authority           string                 `json:"authority"`
	Nonce               Incarnation            `json:"nonce"`
	EvidenceDigest      releaseidentity.Digest `json:"evidence_digest"`
	OperatorKeyID       releaseidentity.Digest `json:"operator_key_id"`
	InstanceID          string                 `json:"instance_id"`
	RestoreEpoch        int64                  `json:"restore_epoch"`
	RecoveryIncarnation Incarnation            `json:"recovery_incarnation"`
	RouteGeneration     int64                  `json:"route_generation"`
	RouteDigest         releaseidentity.Digest `json:"route_digest"`
	IssuedAt            time.Time              `json:"issued_at"`
	ExpiresAt           time.Time              `json:"expires_at"`
}

func (a Acceptance) Validate() error {
	if err := a.Floor.Validate(); err != nil {
		return ErrCorrupt
	}
	if a.Floor.MetadataSequence <= 0 || a.Floor.CatalogSequence <= 0 {
		return ErrCorrupt
	}
	if err := a.ReleaseRootDigest.Validate(); err != nil {
		return ErrCorrupt
	}
	if a.Attestation == nil {
		return nil
	}
	u := a.Attestation
	if u.Authority != "applied-ledger/v1" && u.Authority != "legacy-proposal/v1" {
		return ErrCorrupt
	}
	if u.Nonce == (Incarnation{}) || u.RecoveryIncarnation == (Incarnation{}) || u.InstanceID == "" || u.RestoreEpoch < 0 || u.RouteGeneration < 1 {
		return ErrCorrupt
	}
	for _, digest := range []releaseidentity.Digest{u.EvidenceDigest, u.OperatorKeyID, u.RouteDigest} {
		if err := digest.Validate(); err != nil {
			return ErrCorrupt
		}
	}
	if u.IssuedAt.IsZero() || !u.ExpiresAt.After(u.IssuedAt) || u.ExpiresAt.Sub(u.IssuedAt) > 24*time.Hour {
		return ErrCorrupt
	}
	return nil
}

const nonceDDL = `CREATE TABLE upgrade_nonces (
 trust_domain TEXT NOT NULL,
 instance_id TEXT NOT NULL,
 incarnation TEXT NOT NULL,
 restore_epoch BIGINT NOT NULL,
 nonce TEXT NOT NULL,
 generation BIGINT NOT NULL,
 evidence_digest TEXT NOT NULL,
 PRIMARY KEY(trust_domain,instance_id,incarnation,restore_epoch,nonce)
)`

func (s *Session) accept(ctx context.Context, previous *State, next *State) error {
	a := next.Pending.Acceptance
	if err := a.Validate(); err != nil {
		return err
	}
	if a.Floor.HighestReleaseSequence < int64(next.Pending.Target.Sequence) {
		return ErrConflict
	}
	if previous != nil {
		if previous.ReleaseRootDigest != a.ReleaseRootDigest {
			return ErrConflict
		}
		if err := previous.Floor.Advance(a.Floor); err != nil {
			return err
		}
	}
	next.Floor = a.Floor
	next.ReleaseRootDigest = a.ReleaseRootDigest
	u := a.Attestation
	if next.Pending.RouteSource.Genesis == FreshGenesis {
		if u != nil {
			return ErrConflict
		}
		return nil
	}
	if u == nil || u.InstanceID != next.InstanceID || u.RestoreEpoch != next.RestoreEpoch || u.RecoveryIncarnation != next.RecoveryIncarnation || u.RouteGeneration != next.Generation || u.RouteDigest != next.Pending.RouteDigest {
		return ErrConflict
	}
	if next.Pending.Hop == 0 && ((previous == nil && next.Pending.Source.Genesis == LegacyGenesis) != (u.Authority == "legacy-proposal/v1")) {
		return ErrConflict
	}
	var now time.Time
	if s.engine == releaseidentity.Postgres {
		if err := s.conn.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
	} else {
		var stamp string
		if err := s.conn.QueryRowContext(ctx, `SELECT strftime('%Y-%m-%dT%H:%M:%fZ','now')`).Scan(&stamp); err != nil {
			return err
		}
		var err error
		now, err = time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			return err
		}
	}
	if now.Before(u.IssuedAt) || !now.Before(u.ExpiresAt) {
		return ErrConflict
	}
	if previous != nil && previous.Maintenance && previous.Pending.Phase == Healthy {
		// One attestation is consumed once for the complete immutable route.
		if !equalRecord(previous.Pending.Acceptance.Attestation, u) {
			return ErrConflict
		}
		return nil
	}
	incarnation, _ := next.RecoveryIncarnation.MarshalText()
	nonce, _ := u.Nonce.MarshalText()
	_, err := s.conn.ExecContext(ctx, `INSERT INTO upgrade_nonces(trust_domain,instance_id,incarnation,restore_epoch,nonce,generation,evidence_digest) VALUES($1,$2,$3,$4,$5,$6,$7)`, string(next.TrustDomain), next.InstanceID, string(incarnation), next.RestoreEpoch, string(nonce), next.Generation, string(u.EvidenceDigest))
	return err
}

func encodeFloor(f releaseidentity.SnapshotFloor) string {
	raw, _ := json.Marshal(f)
	return string(raw)
}
