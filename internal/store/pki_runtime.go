package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// PKIRuntime is the private-PKI worker's system boundary (#154, pki ADR D6-D8).
// Tenant request paths never receive it. Every write is a compare-and-swap on
// the row's current state (or, for a CRL, its prior CRL number), so the worker
// runs on every #146 node without a singleton scheduler lease: a node that
// lost a race affects zero rows. It is the DynamicRuntime shape without the
// claim columns, because no PKI transition calls an external system.
type PKIRuntime struct {
	db *DB
}

func NewPKIRuntime(db *DB) *PKIRuntime { return &PKIRuntime{db: db} }

// PKICRLCandidate is an issuer version whose CRL is due: never published, past
// half its validity, or behind a committed revocation. RevocationSeq is
// captured before the entry snapshot and persisted unchanged on publication.
type PKICRLCandidate struct {
	IssuerID            string
	Name                string
	Version             int64
	CertificateDER      []byte
	EncryptedPrivateKey []byte
	DEKVersion          uint32
	CRLNumber           int64
	RevocationSeq       int64
}

type pkiTransitionPayload struct {
	Kind   string `json:"kind"`
	Serial string `json:"serial"`
	Issuer string `json:"issuer"`
	State  string `json:"state"`
}

type pkiSweptRow struct {
	id, org, project, env, serial, principal, issuerName, renewedFrom string
}

// SweepStaleIssuing moves `issuing` rows whose deadline passed to `unknown`:
// the process died, or its outcome transaction failed, between reserving the
// serial and recording the signed leaf. Hikyo cannot prove the leaf never
// escaped, so the CRL publishes unknown serials as revoked (fail closed), and
// the audit trail records an `unknown` OUTCOME. A renewal's predecessor is
// released so it can be renewed again.
func (r *PKIRuntime) SweepStaleIssuing(ctx context.Context, now time.Time, limit int) (int, error) {
	moved := 0
	err := dbTransaction(ctx, r.db, func(tx adapterDBTX) error {
		q := tx.pkiStoreQueries()
		due, err := q.runtimePKIStaleIssuing(ctx, now, limit)
		if err != nil {
			return err
		}
		for _, row := range due {
			changed, err := q.runtimePKIMarkUnknown(ctx, row, now)
			if err != nil {
				return err
			}
			if changed != 1 {
				continue
			}
			if err := q.pkiBumpCertificateRevocation(ctx, row.id); err != nil {
				return err
			}
			kind := "issue"
			if row.renewedFrom != "" {
				kind = "renew"
				if err := q.runtimePKIReleaseRenewal(ctx, row, now); err != nil {
					return err
				}
			}
			if err := insertPKITenantAudit(ctx, tx, row, "unknown", now, pkiTransitionPayload{Kind: kind, Serial: row.serial, Issuer: row.issuerName, State: "unknown"}); err != nil {
				return err
			}
			moved++
		}
		return nil
	})
	return moved, err
}

// ExpireDue moves issued and renewed leaves past not_after to `expired`.
func (r *PKIRuntime) ExpireDue(ctx context.Context, now time.Time, limit int) (int, error) {
	moved := 0
	err := dbTransaction(ctx, r.db, func(tx adapterDBTX) error {
		q := tx.pkiStoreQueries()
		due, err := q.runtimePKIExpired(ctx, now, limit)
		if err != nil {
			return err
		}
		for _, row := range due {
			changed, err := q.runtimePKIMarkExpired(ctx, row, now)
			if err != nil {
				return err
			}
			if changed != 1 {
				continue
			}
			kind := "expire"
			if err := insertPKITenantAudit(ctx, tx, row, "success", now, pkiTransitionPayload{Kind: kind, Serial: row.serial, Issuer: row.issuerName, State: "expired"}); err != nil {
				return err
			}
			moved++
		}
		return nil
	})
	return moved, err
}

func insertPKITenantAudit(ctx context.Context, tx adapterDBTX, row pkiSweptRow, outcome string, at time.Time, payload pkiTransitionPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return tx.pkiStoreQueries().runtimePKITransitionAudit(ctx, row, "pau_"+uuid.Must(uuid.NewV7()).String(), outcome, string(body), at)
}

// DueCRLs lists issuer versions whose CRL must be (re)published. Restore-held
// versions cannot sign until revocations are reconciled and the hold released.
// Retired and revoked versions have no key and are never candidates. Sequence comparison
// is independent of clock skew and keeps revocations racing publication due.
func (r *PKIRuntime) DueCRLs(ctx context.Context, now time.Time) ([]PKICRLCandidate, error) {
	return dbReadResult(ctx, r.db, func(db adapterDB) ([]PKICRLCandidate, error) {
		return db.pkiStoreQueries().runtimePKIDueCRLs(ctx, now.Add(12*time.Hour))
	})
}

// RevokedEntries is the worker's read of one issuer version's CRL entries.
func (r *PKIRuntime) RevokedEntries(ctx context.Context, issuerID string, now time.Time) ([]PKIRevokedEntry, error) {
	return dbReadResult(ctx, r.db, func(db adapterDB) ([]PKIRevokedEntry, error) {
		return revokedEntries(ctx, db, issuerID, now)
	})
}

// PublishCRL stores a freshly signed CRL under a CAS on the prior number and
// records the instance audit event in the same transaction. A lost race
// (another node published first) returns false and writes nothing.
func (r *PKIRuntime) PublishCRL(ctx context.Context, candidate PKICRLCandidate, der []byte, number int64, entries int, thisUpdate, nextUpdate time.Time) (bool, error) {
	published := false
	err := dbTransaction(ctx, r.db, func(tx adapterDBTX) error {
		ok, err := publishCRL(ctx, tx, candidate.IssuerID, der, candidate.CRLNumber, number, candidate.RevocationSeq, thisUpdate, nextUpdate)
		if err != nil || !ok {
			return err
		}
		published = true
		body, err := json.Marshal(map[string]any{"issuer": candidate.Name, "version": candidate.Version, "crl_number": number, "entries": entries})
		if err != nil {
			return err
		}
		return tx.pkiStoreQueries().runtimePKIPublishedAudit(ctx, "pau_"+uuid.Must(uuid.NewV7()).String(), candidate.IssuerID, string(body), thisUpdate)
	})
	return published, err
}

// SignAndPublishCRL holds runtime admission throughout one local signing act.
// Restore maintenance cannot cross the fresh hold check, key read, revocation
// snapshot, signing callback, and publication. The callback performs only local
// cryptography; no externally visible effect may precede the transaction commit.
func (r *PKIRuntime) SignAndPublishCRL(ctx context.Context, candidate PKICRLCandidate, thisUpdate, nextUpdate time.Time, sign func(PKICRLCandidate, []PKIRevokedEntry) ([]byte, int64, error)) (bool, error) {
	published := false
	err := dbTransaction(ctx, r.db, func(tx adapterDBTX) error {
		q := tx.pkiStoreQueries()
		issuer, err := q.pkiGetIssuer(ctx, candidate.IssuerID)
		if isNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if issuer.RestoreHold || !issuer.KeyPresent || (issuer.State != "active" && issuer.State != "retiring") || issuer.CRLNumber != candidate.CRLNumber {
			return nil
		}
		sealed, err := q.pkiIssuerKey(ctx, issuer.ID)
		if err != nil {
			return err
		}
		fresh := PKICRLCandidate{
			IssuerID: issuer.ID, Name: issuer.Name, Version: issuer.Version,
			CertificateDER: issuer.CertificateDER, EncryptedPrivateKey: sealed.ciphertext,
			DEKVersion: sealed.version, CRLNumber: issuer.CRLNumber, RevocationSeq: issuer.RevocationSeq,
		}
		entries, err := revokedEntries(ctx, tx, issuer.ID, thisUpdate)
		if err != nil {
			return err
		}
		der, number, err := sign(fresh, entries)
		if err != nil {
			return err
		}
		ok, err := publishCRL(ctx, tx, fresh.IssuerID, der, fresh.CRLNumber, number, fresh.RevocationSeq, thisUpdate, nextUpdate)
		if err != nil || !ok {
			return err
		}
		published = true
		body, err := json.Marshal(map[string]any{"issuer": fresh.Name, "version": fresh.Version, "crl_number": number, "entries": len(entries)})
		if err != nil {
			return err
		}
		return q.runtimePKIPublishedAudit(ctx, "pau_"+uuid.Must(uuid.NewV7()).String(), fresh.IssuerID, string(body), thisUpdate)
	})
	return published, err
}

// PKIGauges are the label-free instance-wide counts /metrics and doctor read.
type PKIGauges struct {
	LiveCertificates    int64
	UnknownCertificates int64
	HeldIssuers         int64
}

// Gauges is a proof-free scrape-time system read, like DynamicRuntime.Gauges.
func (r *PKIRuntime) Gauges(ctx context.Context, now time.Time) (PKIGauges, error) {
	return dbReadResult(ctx, r.db, func(db adapterDB) (PKIGauges, error) {
		q := db.pkiStoreQueries()
		var out PKIGauges
		var err error
		out.LiveCertificates, err = q.runtimePKICountLive(ctx, now)
		if err != nil {
			return out, err
		}
		out.UnknownCertificates, err = q.runtimePKICountUnknown(ctx, now)
		if err != nil {
			return out, err
		}
		out.HeldIssuers, err = q.runtimePKICountHeld(ctx)
		return out, err
	})
}
