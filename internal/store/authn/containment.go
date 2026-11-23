package authn

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// ErrContainmentUnchanged reports a containment write that changed no row: the
// principal is not human, or already holds the requested state.
var ErrContainmentUnchanged = errors.New("authn: principal is not a human or already holds the requested containment state")

// ContainPrincipal sets the reversible containment state (incident-containment
// ADR, O1). It serializes on the principal row like every grant writer. The
// same UPDATE advances the session generation, which ends every session,
// delegation and step-up of the principal (O4). The same transaction retires
// its pending password proofs and workspace handoffs and consumes every
// outstanding establishment or recovery authority of its account, so nothing
// issued before containment can be redeemed after it.
func (r *Resolver) ContainPrincipal(ctx context.Context, p domain.PrincipalID, at time.Time, incidentID string) error {
	if err := r.LockPrincipalRow(ctx, p); err != nil {
		return err
	}
	var n int64
	var err error
	if r.sq != nil {
		n, err = r.sq.ContainPrincipal(ctx, sqlitegen.ContainPrincipalParams{
			ContainedAt: sql.NullString{String: encodeTime(at), Valid: true}, ContainedIncidentID: nullString(incidentID), PrincipalID: string(p),
		})
	} else {
		n, err = r.pg.ContainPrincipal(ctx, pggen.ContainPrincipalParams{
			ContainedAt: pgTimestamp(at), ContainedIncidentID: pgText(incidentID), PrincipalID: string(p),
		})
	}
	if err := containmentWritten(n, err); err != nil {
		return err
	}
	if err := r.retirePendingProofs(ctx, p); err != nil {
		return err
	}
	// The account read ignores privacy_state, so a restricted principal's
	// authorities are swept too: restricted, contained, released and then
	// privacy-released must not leave a pre-containment authority redeemable.
	accountID, err := r.containmentSweepAccount(ctx, p)
	if err != nil {
		return ignoreDomainNotFound(err)
	}
	return r.ConsumeOutstandingAuthorities(ctx, accountID, at)
}

func (r *Resolver) containmentSweepAccount(ctx context.Context, p domain.PrincipalID) (string, error) {
	var (
		id  string
		err error
	)
	if r.sq != nil {
		id, err = r.sq.GetAccountIDForContainmentSweep(ctx, string(p))
	} else {
		id, err = r.pg.GetAccountIDForContainmentSweep(ctx, string(p))
	}
	return id, notFoundOr(err)
}

// ReleasePrincipalContainment returns the principal to none. Containment removed
// nothing, so release restores nothing. It advances the generation again, and
// retires pending proofs again, so that nothing that predates containment ever
// revives (O4). It also clears the owner's personal-value retention
// suspensions, so release starts a fresh window even when no sweep ran between
// containment and release (O8); a privacy-restricted owner's clock keeps running.
func (r *Resolver) ReleasePrincipalContainment(ctx context.Context, p domain.PrincipalID) error {
	if err := r.LockPrincipalRow(ctx, p); err != nil {
		return err
	}
	var n int64
	var err error
	if r.sq != nil {
		n, err = r.sq.ReleasePrincipalContainment(ctx, string(p))
	} else {
		n, err = r.pg.ReleasePrincipalContainment(ctx, string(p))
	}
	if err := containmentWritten(n, err); err != nil {
		return err
	}
	if err := r.retirePendingProofs(ctx, p); err != nil {
		return err
	}
	if r.sq != nil {
		_, err = r.sq.ClearPersonalSuspensionForOwner(ctx, string(p))
	} else {
		_, err = r.pg.ClearPersonalSuspensionForOwner(ctx, string(p))
	}
	return err
}

func containmentWritten(n int64, err error) error {
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrContainmentUnchanged
	}
	return nil
}

// PrincipalContainedAt reports when the principal was contained, and false when
// it is not contained. Credential establishment compares an authority's mint
// time with it.
func (r *Resolver) PrincipalContainedAt(ctx context.Context, p domain.PrincipalID) (time.Time, bool, error) {
	row, err := r.principalContainment(ctx, p)
	if err != nil || row.state != "contained" {
		return time.Time{}, false, ignoreDomainNotFound(err)
	}
	return row.at, true, nil
}

type principalContainmentRow struct {
	state, privacy string
	at             time.Time
}

func (r *Resolver) principalContainment(ctx context.Context, p domain.PrincipalID) (principalContainmentRow, error) {
	if r.sq != nil {
		row, err := r.sq.GetPrincipalContainment(ctx, string(p))
		if err != nil {
			return principalContainmentRow{}, notFoundOr(err)
		}
		out := principalContainmentRow{state: row.ContainmentState, privacy: row.PrivacyState}
		if row.ContainedAt.Valid {
			if out.at, err = decodeTime(row.ContainedAt.String); err != nil {
				return principalContainmentRow{}, err
			}
		}
		return out, nil
	}
	row, err := r.pg.GetPrincipalContainment(ctx, string(p))
	if err != nil {
		return principalContainmentRow{}, notFoundOr(err)
	}
	return principalContainmentRow{state: row.ContainmentState, privacy: row.PrivacyState, at: row.ContainedAt.Time}, nil
}

// PrincipalPausedByContainment reports whether containment alone holds the
// principal out of authority: contained while privacy leaves it active. A
// privacy-restricted or erased principal keeps privacy's behavior. The
// personal-value retention sweep reads it, so a contained owner's deletion
// clock pauses (incident-containment ADR, O8). An unknown principal is false.
func (r *Resolver) PrincipalPausedByContainment(ctx context.Context, p domain.PrincipalID) (bool, error) {
	row, err := r.principalContainment(ctx, p)
	if err != nil {
		return false, ignoreDomainNotFound(err)
	}
	return row.state == "contained" && row.privacy == "active", nil
}

func ignoreDomainNotFound(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	return err
}

// CredentialEstablishedSince reports whether a credential establishment
// completed for the principal's account after the given moment (O4 A). Only the newest
// authority minted since then speaks for it: minting a newer one consumes the
// older outstanding authorities without establishing anything.
func (r *Resolver) CredentialEstablishedSince(ctx context.Context, p domain.PrincipalID, since time.Time) (bool, error) {
	var at string
	var err error
	if r.sq != nil {
		at, err = r.sq.GetLatestAuthorityConsumedSince(ctx, sqlitegen.GetLatestAuthorityConsumedSinceParams{PrincipalID: string(p), Since: encodeTime(since)})
	} else {
		at, err = r.pg.GetLatestAuthorityConsumedSince(ctx, pggen.GetLatestAuthorityConsumedSinceParams{PrincipalID: string(p), Since: encodeTime(since)})
	}
	if isNoRows(err) {
		return false, nil
	}
	return at != "", err
}

// PrincipalContainedIncident names the incident that contained the principal,
// empty when it recorded none. A principal that is not contained is not found.
func (r *Resolver) PrincipalContainedIncident(ctx context.Context, p domain.PrincipalID) (string, error) {
	if r.sq != nil {
		id, err := r.sq.GetPrincipalContainedIncident(ctx, string(p))
		return id.String, notFoundOr(err)
	}
	id, err := r.pg.GetPrincipalContainedIncident(ctx, string(p))
	return id.String, notFoundOr(err)
}

// PrincipalHasPassword reports whether the principal's account holds a local password.
func (r *Resolver) PrincipalHasPassword(ctx context.Context, p domain.PrincipalID) (bool, error) {
	var n int64
	var err error
	if r.sq != nil {
		n, err = r.sq.CountPasswordCredentialsForPrincipal(ctx, string(p))
	} else {
		n, err = r.pg.CountPasswordCredentialsForPrincipal(ctx, string(p))
	}
	return n > 0, err
}
