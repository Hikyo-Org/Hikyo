package authn

import (
	"context"
	"crypto/subtle"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// RemotePairingInvite is finite administrative consent, not a public request.
// Sealed state and capability verifiers never leave the authentication boundary
// except as ciphertext for the instance keyring's service owner.
type RemotePairingInvite struct {
	ID, Label, Origin, SPKIPin                                       string
	CapabilityVerifier, PAKESealed                                   []byte
	CredentialEpoch                                                  int64
	CreatedBy                                                        domain.PrincipalID
	CreatedAt, ExpiresAt, CredentialExpiresAt, RevokedAt, ConsumedAt time.Time
}

func (i RemotePairingInvite) Live(now time.Time, epoch int64) bool {
	return i.CredentialEpoch == epoch && i.RevokedAt.IsZero() && i.ConsumedAt.IsZero() && now.Before(i.ExpiresAt) && now.Before(i.CredentialExpiresAt)
}

type RemotePairingExchange struct {
	ID, InviteID                     string
	StateSealed                      []byte
	CredentialEpoch                  int64
	CreatedAt, ExpiresAt, ConsumedAt time.Time
}
type RemotePairedConnection struct{ ConnectionID, PublicJWK, Thumbprint, Origin string }

func sqlitePairingInvite(x sqlitegen.RemotePairingInvite) (RemotePairingInvite, error) {
	out := RemotePairingInvite{ID: x.ID, Label: x.Label, Origin: x.Origin, SPKIPin: x.SpkiPin, CapabilityVerifier: x.CapabilityVerifier, PAKESealed: x.PakeSealed, CredentialEpoch: x.CredentialEpoch, CreatedBy: domain.PrincipalID(x.CreatedBy)}
	var err error
	if out.CreatedAt, err = decodeTime(x.CreatedAt); err != nil {
		return out, err
	}
	if out.ExpiresAt, err = decodeTime(x.ExpiresAt); err != nil {
		return out, err
	}
	if out.CredentialExpiresAt, err = decodeTime(x.CredentialExpiresAt); err != nil {
		return out, err
	}
	if out.RevokedAt, err = decodeNullTime(x.RevokedAt); err != nil {
		return out, err
	}
	out.ConsumedAt, err = decodeNullTime(x.ConsumedAt)
	return out, err
}
func pgPairingInvite(x pggen.RemotePairingInvite) RemotePairingInvite {
	return RemotePairingInvite{ID: x.ID, Label: x.Label, Origin: x.Origin, SPKIPin: x.SpkiPin, CapabilityVerifier: x.CapabilityVerifier, PAKESealed: x.PakeSealed, CredentialEpoch: x.CredentialEpoch, CreatedBy: domain.PrincipalID(x.CreatedBy), CreatedAt: canon(x.CreatedAt.Time), ExpiresAt: canon(x.ExpiresAt.Time), CredentialExpiresAt: canon(x.CredentialExpiresAt.Time), RevokedAt: nullablePGTime(x.RevokedAt), ConsumedAt: nullablePGTime(x.ConsumedAt)}
}
func (r *Resolver) CreateRemotePairingInvite(ctx context.Context, i RemotePairingInvite) error {
	if r.sq != nil {
		return r.sq.CreateRemotePairingInvite(ctx, sqlitegen.CreateRemotePairingInviteParams{ID: i.ID, Label: i.Label, Origin: i.Origin, SpkiPin: i.SPKIPin, CapabilityVerifier: i.CapabilityVerifier, PakeSealed: i.PAKESealed, CredentialEpoch: i.CredentialEpoch, CreatedBy: string(i.CreatedBy), CreatedAt: encodeTime(i.CreatedAt), ExpiresAt: encodeTime(i.ExpiresAt), CredentialExpiresAt: encodeTime(i.CredentialExpiresAt)})
	}
	return r.pg.CreateRemotePairingInvite(ctx, pggen.CreateRemotePairingInviteParams{ID: i.ID, Label: i.Label, Origin: i.Origin, SpkiPin: i.SPKIPin, CapabilityVerifier: i.CapabilityVerifier, PakeSealed: i.PAKESealed, CredentialEpoch: i.CredentialEpoch, CreatedBy: string(i.CreatedBy), CreatedAt: pgTimestamp(i.CreatedAt), ExpiresAt: pgTimestamp(i.ExpiresAt), CredentialExpiresAt: pgTimestamp(i.CredentialExpiresAt)})
}
func (r *Resolver) RemotePairingInviteByCapability(ctx context.Context, v []byte) (RemotePairingInvite, error) {
	var i RemotePairingInvite
	if r.sq != nil {
		x, err := r.sq.RemotePairingInviteByCapability(ctx, v)
		if err != nil {
			return i, notFoundOr(err)
		}
		var err2 error
		i, err2 = sqlitePairingInvite(x)
		if err2 != nil {
			return i, err2
		}
	} else {
		x, err := r.pg.RemotePairingInviteByCapability(ctx, v)
		if err != nil {
			return i, notFoundOr(err)
		}
		i = pgPairingInvite(x)
	}
	if subtle.ConstantTimeCompare(v, i.CapabilityVerifier) != 1 {
		return RemotePairingInvite{}, domain.ErrNotFound
	}
	return i, nil
}
func (r *Resolver) RemotePairingInviteByID(ctx context.Context, id string) (RemotePairingInvite, error) {
	if r.sq != nil {
		x, err := r.sq.RemotePairingInviteByID(ctx, id)
		if err != nil {
			return RemotePairingInvite{}, notFoundOr(err)
		}
		return sqlitePairingInvite(x)
	}
	x, err := r.pg.RemotePairingInviteByID(ctx, id)
	if err != nil {
		return RemotePairingInvite{}, notFoundOr(err)
	}
	return pgPairingInvite(x), nil
}
func (r *Resolver) ListRemotePairingInvites(ctx context.Context) ([]RemotePairingInvite, error) {
	out := []RemotePairingInvite{}
	if r.sq != nil {
		xs, err := r.sq.ListRemotePairingInvites(ctx)
		if err != nil {
			return nil, err
		}
		for _, x := range xs {
			i, err := sqlitePairingInvite(sqlitegen.RemotePairingInvite{ID: x.ID, Label: x.Label, Origin: x.Origin, SpkiPin: x.SpkiPin, CredentialEpoch: x.CredentialEpoch, CreatedBy: x.CreatedBy, CreatedAt: x.CreatedAt, ExpiresAt: x.ExpiresAt, CredentialExpiresAt: x.CredentialExpiresAt, RevokedAt: x.RevokedAt, ConsumedAt: x.ConsumedAt})
			if err != nil {
				return nil, err
			}
			out = append(out, i)
		}
	} else {
		xs, err := r.pg.ListRemotePairingInvites(ctx)
		if err != nil {
			return nil, err
		}
		for _, x := range xs {
			out = append(out, pgPairingInvite(pggen.RemotePairingInvite{ID: x.ID, Label: x.Label, Origin: x.Origin, SpkiPin: x.SpkiPin, CredentialEpoch: x.CredentialEpoch, CreatedBy: x.CreatedBy, CreatedAt: x.CreatedAt, ExpiresAt: x.ExpiresAt, CredentialExpiresAt: x.CredentialExpiresAt, RevokedAt: x.RevokedAt, ConsumedAt: x.ConsumedAt}))
		}
	}
	return out, nil
}
func (r *Resolver) CountLiveRemotePairingInvites(ctx context.Context, now time.Time) (int64, error) {
	if r.sq != nil {
		return r.sq.CountLiveRemotePairingInvites(ctx, encodeTime(now))
	}
	return r.pg.CountLiveRemotePairingInvites(ctx, pgTimestamp(now))
}
func (r *Resolver) RevokeRemotePairingInvite(ctx context.Context, id string, now time.Time) (bool, error) {
	var n int64
	var err error
	if r.sq != nil {
		n, err = r.sq.RevokeRemotePairingInvite(ctx, sqlitegen.RevokeRemotePairingInviteParams{Now: nullTimeString(now), ID: id})
	} else {
		n, err = r.pg.RevokeRemotePairingInvite(ctx, pggen.RevokeRemotePairingInviteParams{Now: pgTimestamp(now), ID: id})
	}
	return n == 1, err
}
func (r *Resolver) ConsumeRemotePairingInvite(ctx context.Context, id string, now time.Time, epoch int64) (bool, error) {
	var n int64
	var err error
	if r.sq != nil {
		n, err = r.sq.ConsumeRemotePairingInvite(ctx, sqlitegen.ConsumeRemotePairingInviteParams{Now: nullTimeString(now), ID: id, Epoch: epoch})
	} else {
		n, err = r.pg.ConsumeRemotePairingInvite(ctx, pggen.ConsumeRemotePairingInviteParams{Now: pgTimestamp(now), ID: id, Epoch: epoch})
	}
	return n == 1, err
}
func (r *Resolver) CreateRemotePairingExchange(ctx context.Context, e RemotePairingExchange) error {
	if r.sq != nil {
		return r.sq.CreateRemotePairingExchange(ctx, sqlitegen.CreateRemotePairingExchangeParams{ID: e.ID, InviteID: e.InviteID, StateSealed: e.StateSealed, CredentialEpoch: e.CredentialEpoch, CreatedAt: encodeTime(e.CreatedAt), ExpiresAt: encodeTime(e.ExpiresAt)})
	}
	return r.pg.CreateRemotePairingExchange(ctx, pggen.CreateRemotePairingExchangeParams{ID: e.ID, InviteID: e.InviteID, StateSealed: e.StateSealed, CredentialEpoch: e.CredentialEpoch, CreatedAt: pgTimestamp(e.CreatedAt), ExpiresAt: pgTimestamp(e.ExpiresAt)})
}
func (r *Resolver) RemotePairingExchangeByID(ctx context.Context, id string) (RemotePairingExchange, error) {
	if r.sq != nil {
		x, err := r.sq.RemotePairingExchangeByID(ctx, id)
		if err != nil {
			return RemotePairingExchange{}, notFoundOr(err)
		}
		o := RemotePairingExchange{ID: x.ID, InviteID: x.InviteID, StateSealed: x.StateSealed, CredentialEpoch: x.CredentialEpoch}
		if o.CreatedAt, err = decodeTime(x.CreatedAt); err != nil {
			return o, err
		}
		if o.ExpiresAt, err = decodeTime(x.ExpiresAt); err != nil {
			return o, err
		}
		o.ConsumedAt, err = decodeNullTime(x.ConsumedAt)
		return o, err
	}
	x, err := r.pg.RemotePairingExchangeByID(ctx, id)
	if err != nil {
		return RemotePairingExchange{}, notFoundOr(err)
	}
	return RemotePairingExchange{ID: x.ID, InviteID: x.InviteID, StateSealed: x.StateSealed, CredentialEpoch: x.CredentialEpoch, CreatedAt: canon(x.CreatedAt.Time), ExpiresAt: canon(x.ExpiresAt.Time), ConsumedAt: nullablePGTime(x.ConsumedAt)}, nil
}
func (r *Resolver) CountLiveRemotePairingExchanges(ctx context.Context, id string, now time.Time) (int64, error) {
	if r.sq != nil {
		return r.sq.CountLiveRemotePairingExchanges(ctx, sqlitegen.CountLiveRemotePairingExchangesParams{InviteID: id, Now: encodeTime(now)})
	}
	return r.pg.CountLiveRemotePairingExchanges(ctx, pggen.CountLiveRemotePairingExchangesParams{InviteID: id, Now: pgTimestamp(now)})
}
func (r *Resolver) ConsumeRemotePairingExchange(ctx context.Context, id string, now time.Time, epoch int64) (bool, error) {
	var n int64
	var err error
	if r.sq != nil {
		n, err = r.sq.ConsumeRemotePairingExchange(ctx, sqlitegen.ConsumeRemotePairingExchangeParams{Now: nullTimeString(now), ID: id, Epoch: epoch})
	} else {
		n, err = r.pg.ConsumeRemotePairingExchange(ctx, pggen.ConsumeRemotePairingExchangeParams{Now: pgTimestamp(now), ID: id, Epoch: epoch})
	}
	return n == 1, err
}
func (r *Resolver) BindRemotePairedConnection(ctx context.Context, c RemotePairedConnection) error {
	if r.sq != nil {
		return r.sq.BindRemotePairedConnection(ctx, sqlitegen.BindRemotePairedConnectionParams{ConnectionID: c.ConnectionID, PublicJwk: c.PublicJWK, Thumbprint: c.Thumbprint, Origin: c.Origin})
	}
	return r.pg.BindRemotePairedConnection(ctx, pggen.BindRemotePairedConnectionParams{ConnectionID: c.ConnectionID, PublicJwk: c.PublicJWK, Thumbprint: c.Thumbprint, Origin: c.Origin})
}
func (r *Resolver) RemotePairedConnectionByID(ctx context.Context, id string) (RemotePairedConnection, error) {
	if r.sq != nil {
		x, err := r.sq.RemotePairedConnectionByID(ctx, id)
		return RemotePairedConnection{x.ConnectionID, x.PublicJwk, x.Thumbprint, x.Origin}, notFoundOr(err)
	}
	x, err := r.pg.RemotePairedConnectionByID(ctx, id)
	return RemotePairedConnection{x.ConnectionID, x.PublicJwk, x.Thumbprint, x.Origin}, notFoundOr(err)
}
func (r *Resolver) ClaimRemotePairingReplay(ctx context.Context, thumbprint string, v []byte, expires time.Time) (bool, error) {
	var n int64
	var err error
	if r.sq != nil {
		n, err = r.sq.ClaimRemotePairingReplay(ctx, sqlitegen.ClaimRemotePairingReplayParams{Thumbprint: thumbprint, JtiVerifier: v, ExpiresAt: encodeTime(expires)})
	} else {
		n, err = r.pg.ClaimRemotePairingReplay(ctx, pggen.ClaimRemotePairingReplayParams{Thumbprint: thumbprint, JtiVerifier: v, ExpiresAt: pgTimestamp(expires)})
	}
	return n == 1, err
}
func (r *Resolver) CountRemotePairingReplays(ctx context.Context) (int64, error) {
	if r.sq != nil {
		return r.sq.CountRemotePairingReplays(ctx)
	}
	return r.pg.CountRemotePairingReplays(ctx)
}
func (r *Resolver) CountRemotePairingReplaysForKey(ctx context.Context, thumbprint string) (int64, error) {
	if r.sq != nil {
		return r.sq.CountRemotePairingReplaysForKey(ctx, thumbprint)
	}
	return r.pg.CountRemotePairingReplaysForKey(ctx, thumbprint)
}
func (r *Resolver) PruneRemotePairingState(ctx context.Context, now time.Time) error {
	if r.sq != nil {
		if err := r.sq.PruneRemotePairingReplays(ctx, encodeTime(now)); err != nil {
			return err
		}
		if err := r.sq.PruneRemotePairingExchanges(ctx, encodeTime(now)); err != nil {
			return err
		}
		return r.sq.PruneRemotePairingInvites(ctx, encodeTime(now))
	}
	if err := r.pg.PruneRemotePairingReplays(ctx, pgTimestamp(now)); err != nil {
		return err
	}
	if err := r.pg.PruneRemotePairingExchanges(ctx, pgTimestamp(now)); err != nil {
		return err
	}
	return r.pg.PruneRemotePairingInvites(ctx, pgTimestamp(now))
}

// restorePairingNextEpoch supports archives predating the pairing schema.
func (r *Resolver) restorePairingNextEpoch(ctx context.Context, next int64) (int64, error) {
	var n int64
	var err error
	if r.sq != nil {
		err = r.sqdb.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('remote_pairing_invites','remote_pairing_exchanges')`).Scan(&n)
	} else {
		err = r.pgdb.QueryRow(ctx, `SELECT COUNT(*) FROM pg_catalog.pg_class WHERE relkind IN ('r','p') AND oid IN (to_regclass('remote_pairing_invites'),to_regclass('remote_pairing_exchanges'))`).Scan(&n)
	}
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return next, nil
	}
	if n != 2 {
		return 0, fmt.Errorf("authn: incomplete remote pairing schema")
	}
	var raw any
	if r.sq != nil {
		raw, err = r.sq.RemotePairingMaxCredentialEpoch(ctx)
	} else {
		raw, err = r.pg.RemotePairingMaxCredentialEpoch(ctx)
	}
	if err != nil {
		return 0, err
	}
	v, err := nextEpoch(raw)
	if err != nil {
		return 0, err
	}
	return max(next, v), nil
}

func nullablePGTime(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return canon(t.Time)
}

func (r *Resolver) ClaimRemotePairingAttempt(ctx context.Context, id string, now time.Time) (bool, error) {
	var n int64
	var err error
	if r.sq != nil {
		n, err = r.sq.ClaimRemotePairingAttempt(ctx, sqlitegen.ClaimRemotePairingAttemptParams{ID: id, Now: encodeTime(now)})
	} else {
		n, err = r.pg.ClaimRemotePairingAttempt(ctx, pggen.ClaimRemotePairingAttemptParams{ID: id, Now: pgTimestamp(now)})
	}
	return n == 1, err
}
