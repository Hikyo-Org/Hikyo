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

func scanSweptRows(rows adapterRows) ([]pkiSweptRow, error) {
	defer closeRows(rows)
	var out []pkiSweptRow
	for rows.Next() {
		var row pkiSweptRow
		if err := rows.Scan(&row.id, &row.org, &row.project, &row.env, &row.serial, &row.principal, &row.issuerName, &row.renewedFrom); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

const pkiSweepColumns = `c.id,c.org_id,c.project_id,c.environment_id,c.serial,c.principal_id,i.name,COALESCE(c.renewed_from,'')`

// SweepStaleIssuing moves `issuing` rows whose deadline passed to `unknown`:
// the process died, or its outcome transaction failed, between reserving the
// serial and recording the signed leaf. Hikyo cannot prove the leaf never
// escaped, so the CRL publishes unknown serials as revoked (fail closed), and
// the audit trail records an `unknown` OUTCOME. A renewal's predecessor is
// released so it can be renewed again.
func (r *PKIRuntime) SweepStaleIssuing(ctx context.Context, now time.Time, limit int) (int, error) {
	moved := 0
	err := dbTransaction(ctx, r.db, func(tx adapterDBTX) error {
		query := tx.SQLPerEngine(
			`SELECT `+pkiSweepColumns+` FROM pki_certificates c JOIN pki_issuers i ON i.id=c.issuer_id WHERE c.state='issuing' AND c.issuing_deadline<? ORDER BY c.issuing_deadline LIMIT ?`,
			`SELECT `+pkiSweepColumns+` FROM pki_certificates c JOIN pki_issuers i ON i.id=c.issuer_id WHERE c.state='issuing' AND c.issuing_deadline<$1 ORDER BY c.issuing_deadline LIMIT $2 FOR UPDATE OF c SKIP LOCKED`)
		rows, err := tx.Query(ctx, query, tx.Stamp(now), limit)
		if err != nil {
			return err
		}
		due, err := scanSweptRows(rows)
		if err != nil {
			return err
		}
		for _, row := range due {
			changed, err := tx.Exec(ctx, tx.SQL(`UPDATE pki_certificates SET state='unknown', row_version=row_version+1, updated_at=? WHERE id=? AND org_id=? AND state='issuing'`), tx.Stamp(now), row.id, row.org)
			if err != nil {
				return err
			}
			if changed != 1 {
				continue
			}
			if _, err := tx.Exec(ctx, tx.SQL(`UPDATE pki_issuers SET revocation_seq=revocation_seq+1 WHERE id=(SELECT issuer_id FROM pki_certificates WHERE id=?)`), row.id); err != nil {
				return err
			}
			kind := "issue"
			if row.renewedFrom != "" {
				kind = "renew"
				if _, err := tx.Exec(ctx, tx.SQL(`UPDATE pki_certificates SET renewed_by=NULL, updated_at=? WHERE id=? AND org_id=? AND renewed_by=?`), tx.Stamp(now), row.renewedFrom, row.org, row.id); err != nil {
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
		query := tx.SQLPerEngine(
			`SELECT `+pkiSweepColumns+` FROM pki_certificates c JOIN pki_issuers i ON i.id=c.issuer_id WHERE c.state IN ('issued','renewed') AND c.not_after<=? ORDER BY c.not_after LIMIT ?`,
			`SELECT `+pkiSweepColumns+` FROM pki_certificates c JOIN pki_issuers i ON i.id=c.issuer_id WHERE c.state IN ('issued','renewed') AND c.not_after<=$1 ORDER BY c.not_after LIMIT $2 FOR UPDATE OF c SKIP LOCKED`)
		rows, err := tx.Query(ctx, query, tx.Stamp(now), limit)
		if err != nil {
			return err
		}
		due, err := scanSweptRows(rows)
		if err != nil {
			return err
		}
		for _, row := range due {
			changed, err := tx.Exec(ctx, tx.SQL(`UPDATE pki_certificates SET state='expired', row_version=row_version+1, updated_at=? WHERE id=? AND org_id=? AND state IN ('issued','renewed')`), tx.Stamp(now), row.id, row.org)
			if err != nil {
				return err
			}
			if changed != 1 {
				continue
			}
			if err := insertPKITenantAudit(ctx, tx, row, "success", now, pkiTransitionPayload{Kind: "expire", Serial: row.serial, Issuer: row.issuerName, State: "expired"}); err != nil {
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
	query := tx.SQLPerEngine(
		`INSERT INTO audit_tenant_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,scope_class,org_id,project_id,env_id,object_type,object_id,outcome,correlation_id,origin,payload) VALUES (?,'pki.certificate_transition_outcome',1,?,0,?,NULL,'system',?,'env',?,?,?,'pki-certificate',?,?,?,'system',?)`,
		`INSERT INTO audit_tenant_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,scope_class,org_id,project_id,env_id,object_type,object_id,outcome,correlation_id,origin,payload) VALUES ($1,'pki.certificate_transition_outcome',1,$2,false,$3,NULL,'system',$4,'env',$5,$6,$7,'pki-certificate',$8,$9,$10,'system',$11)`)
	stamp := tx.Stamp(at)
	_, err = tx.Exec(ctx, query, "pau_"+uuid.Must(uuid.NewV7()).String(), stamp, stamp, row.principal, row.org, row.project, row.env, row.id, outcome, row.id, string(body))
	return err
}

// DueCRLs lists issuer versions whose CRL must be (re)published, including those
// held after restore. Holds prevent issuance, not revocation publication. Retired
// and revoked versions have no key and are never candidates. Sequence comparison
// is independent of clock skew and keeps revocations racing publication due.
func (r *PKIRuntime) DueCRLs(ctx context.Context, now time.Time) ([]PKICRLCandidate, error) {
	return dbReadResult(ctx, r.db, func(db adapterDB) ([]PKICRLCandidate, error) {
		query := db.SQL(`SELECT i.id,i.name,i.version,i.certificate_der,i.encrypted_private_key,i.dek_version,i.crl_number,i.revocation_seq FROM pki_issuers i WHERE i.state IN ('active','retiring') AND i.encrypted_private_key IS NOT NULL AND i.dek_version IS NOT NULL AND i.certificate_der IS NOT NULL AND (i.crl_der IS NULL OR i.crl_next_update<=? OR i.revocation_seq>i.crl_revocation_seq) ORDER BY i.id`)
		halfLife := now.Add(12 * time.Hour)
		rows, err := db.Query(ctx, query, db.Stamp(halfLife))
		if err != nil {
			return nil, err
		}
		defer closeRows(rows)
		var out []PKICRLCandidate
		for rows.Next() {
			var c PKICRLCandidate
			var dek int64
			if err := rows.Scan(&c.IssuerID, &c.Name, &c.Version, &c.CertificateDER, &c.EncryptedPrivateKey, &dek, &c.CRLNumber, &c.RevocationSeq); err != nil {
				return nil, err
			}
			c.DEKVersion = uint32(dek)
			out = append(out, c)
		}
		return out, rows.Err()
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
		query := tx.SQLPerEngine(
			`INSERT INTO audit_instance_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,object_type,object_id,outcome,correlation_id,origin,payload) VALUES (?,'pki.crl_published',1,?,0,?,NULL,'system',NULL,'pki-issuer',?,'success',NULL,'system',?)`,
			`INSERT INTO audit_instance_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,object_type,object_id,outcome,correlation_id,origin,payload) VALUES ($1,'pki.crl_published',1,$2,false,$3,NULL,'system',NULL,'pki-issuer',$4,'success',NULL,'system',$5)`)
		stamp := tx.Stamp(thisUpdate)
		_, err = tx.Exec(ctx, query, "pau_"+uuid.Must(uuid.NewV7()).String(), stamp, stamp, candidate.IssuerID, string(body))
		return err
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
		var out PKIGauges
		if err := db.QueryRow(ctx, db.SQL(`SELECT COUNT(*) FROM pki_certificates WHERE state IN ('issued','renewed') AND not_after>?`), db.Stamp(now)).Scan(&out.LiveCertificates); err != nil {
			return out, err
		}
		if err := db.QueryRow(ctx, db.SQL(`SELECT COUNT(*) FROM pki_certificates WHERE state='unknown' AND not_after>?`), db.Stamp(now)).Scan(&out.UnknownCertificates); err != nil {
			return out, err
		}
		err := db.QueryRow(ctx, `SELECT COUNT(*) FROM pki_issuers WHERE restore_hold=1 AND state IN ('pending','active','retiring')`).Scan(&out.HeldIssuers)
		return out, err
	})
}
