package store

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// SSHRuntime is the SSH certificate sweeper's system boundary (#155). Tenant
// request paths never receive it. The sweeper re-authorizes each live
// certificate's recorded requester through the ordinary authorizer and, on a
// definite refusal, revokes the certificate here. The only write is
// idempotent (`state='issued'` guards it), so every node in a #146 cluster can
// sweep concurrently without a claim fence: two nodes revoking the same row
// affect it once, and the second writes no audit row.
type SSHRuntime struct {
	db *DB
}

func NewSSHRuntime(db *DB) *SSHRuntime { return &SSHRuntime{db: db} }

// SSHSweepCandidate is one live certificate and the facts the sweeper needs to
// re-decide its requester's authority.
type SSHSweepCandidate struct {
	ID                   string
	OrgID                string
	ProjectID            string
	EnvironmentID        string
	ProfileID            string
	Serial               int64
	RequesterPrincipalID string
	RequesterClass       string
	// RequesterListed is whether the requester is still on the profile's
	// requester list (false when the profile was deleted).
	RequesterListed bool
}

// ListLiveCertificates pages through issued, unexpired certificates by id.
func (r *SSHRuntime) ListLiveCertificates(ctx context.Context, now time.Time, afterID string, limit int) ([]SSHSweepCandidate, error) {
	return dbReadResult(ctx, r.db, func(db adapterDB) ([]SSHSweepCandidate, error) {
		query := db.SQL(`SELECT c.id,c.org_id,c.project_id,c.environment_id,c.profile_id,c.serial,c.requester_principal_id,c.requester_class,
CASE WHEN q.principal_id IS NULL THEN 0 ELSE 1 END
FROM ssh_certificates c
LEFT JOIN ssh_profile_requesters q ON q.profile_id=c.profile_id AND q.principal_id=c.requester_principal_id AND q.org_id=c.org_id AND q.project_id=c.project_id AND q.environment_id=c.environment_id
WHERE c.state='issued' AND c.valid_before>? AND c.id>? ORDER BY c.id LIMIT ?`)
		rows, err := db.Query(ctx, query, db.Stamp(now), afterID, limit)
		if err != nil {
			return nil, err
		}
		defer closeAdapterRows(rows)
		var out []SSHSweepCandidate
		for rows.Next() {
			var c SSHSweepCandidate
			var listed int
			if err := rows.Scan(&c.ID, &c.OrgID, &c.ProjectID, &c.EnvironmentID, &c.ProfileID, &c.Serial, &c.RequesterPrincipalID, &c.RequesterClass, &listed); err != nil {
				return nil, err
			}
			c.RequesterListed = listed == 1
			out = append(out, c)
		}
		return out, rows.Err()
	})
}

type sshRevokedPayload struct {
	Serial string `json:"serial"`
	Reason string `json:"reason"`
}

// RevokeForAuthority revokes one certificate whose requester no longer holds
// issuing authority, with its system-actor audit row, in one transaction. It
// reports whether this call changed the row.
func (r *SSHRuntime) RevokeForAuthority(ctx context.Context, c SSHSweepCandidate, at time.Time) (bool, error) {
	var changed bool
	err := dbTransaction(ctx, r.db, func(tx adapterDBTX) error {
		changed = false
		update := tx.SQL(`UPDATE ssh_certificates SET state='revoked',revoked_at=?,revocation_reason='authority-withdrawn' WHERE id=? AND org_id=? AND project_id=? AND environment_id=? AND state='issued'`)
		n, err := tx.Exec(ctx, update, tx.Stamp(at), c.ID, c.OrgID, c.ProjectID, c.EnvironmentID)
		if err != nil {
			return err
		}
		if n != 1 {
			return nil
		}
		changed = true
		payload, err := json.Marshal(sshRevokedPayload{Serial: strconv.FormatInt(c.Serial, 10), Reason: "authority-withdrawn"})
		if err != nil {
			return err
		}
		insert := tx.SQLPerEngine(
			`INSERT INTO audit_tenant_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,scope_class,org_id,project_id,env_id,object_type,object_id,outcome,correlation_id,origin,payload) VALUES (?,?,1,?,0,?,NULL,'system',?,'env',?,?,?,'ssh-certificate',?,'success',?,'system',?)`,
			`INSERT INTO audit_tenant_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,scope_class,org_id,project_id,env_id,object_type,object_id,outcome,correlation_id,origin,payload) VALUES ($1,$2,1,$3,false,$4,NULL,'system',$5,'env',$6,$7,$8,'ssh-certificate',$9,'success',$10,'system',$11)`)
		stamp := tx.Stamp(at)
		_, err = tx.Exec(ctx, insert, "sau_"+uuid.Must(uuid.NewV7()).String(), "ssh.certificate_revoked", stamp, stamp,
			c.RequesterPrincipalID, c.OrgID, c.ProjectID, c.EnvironmentID, c.ID, c.ID, string(payload))
		return err
	})
	return changed, err
}

// Gauges returns the instance-wide SSH counts for /metrics: live (issued,
// unexpired) certificates and published KRL entries (revoked, unexpired,
// signed by a key hosts still trust).
func (r *SSHRuntime) Gauges(ctx context.Context, now time.Time) (active, krlEntries int64, err error) {
	err = dbRead(ctx, r.db, func(db adapterDB) error {
		stamp := db.Stamp(now)
		if err := db.QueryRow(ctx, db.SQL(`SELECT COUNT(*) FROM ssh_certificates WHERE state='issued' AND valid_before>?`), stamp).Scan(&active); err != nil {
			return err
		}
		return db.QueryRow(ctx, db.SQL(`SELECT COUNT(*) FROM ssh_certificates c JOIN ssh_ca_keys k ON k.id=c.ca_key_id
WHERE c.state='revoked' AND c.valid_before>? AND (k.state='active' OR (k.state='retiring' AND k.retire_after>?))`), stamp, stamp).Scan(&krlEntries)
	})
	if err != nil {
		return 0, 0, err
	}
	return active, krlEntries, nil
}
