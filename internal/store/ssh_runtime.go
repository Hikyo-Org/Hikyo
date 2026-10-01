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
		return db.sshQueries().sshLiveCertificates(ctx, now, afterID, limit)
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
		n, err := tx.sshQueries().sshRevokeForAuthority(ctx, c, at)
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
		return tx.sshQueries().sshRevocationAudit(ctx, c, at, "sau_"+uuid.Must(uuid.NewV7()).String(), string(payload))
	})
	return changed, err
}

// Gauges returns the instance-wide SSH counts for /metrics: live (issued,
// unexpired) certificates and published KRL entries (revoked, unexpired,
// signed by a key hosts still trust).
func (r *SSHRuntime) Gauges(ctx context.Context, now time.Time) (active, krlEntries int64, err error) {
	err = dbRead(ctx, r.db, func(db adapterDB) error {
		var err error
		active, krlEntries, err = db.sshQueries().sshGauges(ctx, now)
		return err
	})
	if err != nil {
		return 0, 0, err
	}
	return active, krlEntries, nil
}
