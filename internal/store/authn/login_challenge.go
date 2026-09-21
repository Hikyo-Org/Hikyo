package authn

import (
	"context"
	"database/sql"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// Login challenges (#760): the single-use, expiring authority proving the
// password step passed for one account, minted by localLogin (202) when a
// factor stands and a browser session was requested. They live on the
// proof-free resolution surface for the same reason the login writers do — the
// proof is what that answer produces — and carry the same discipline as the
// WebAuthn ceremony: random verifier, single-use, expiring, account- and
// credential-epoch-bound.

// LoginChallenge is a resolved challenge. Consumed and expiry state travel with
// it so the caller refuses uniformly rather than branching on a second lookup.
type LoginChallenge struct {
	ID        string
	AccountID string
	Factors   string // JSON array of offered factor classes, as stored
	ExpiresAt time.Time
	Consumed  bool
	CreatedAt time.Time
}

// NewLoginChallenge is the challenge insert carrier.
type NewLoginChallenge struct {
	ID        string
	AccountID string
	Factors   string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// CreateLoginChallenge writes a single-use, expiring challenge row.
func (r *Resolver) CreateLoginChallenge(ctx context.Context, c NewLoginChallenge) error {
	if r.sq != nil {
		return r.sq.InsertLoginChallenge(ctx, sqlitegen.InsertLoginChallengeParams{
			ID: c.ID, AccountID: c.AccountID,
			Factors:   c.Factors,
			ExpiresAt: encodeTime(c.ExpiresAt), CreatedAt: encodeTime(c.CreatedAt),
		})
	}
	return r.pg.InsertLoginChallenge(ctx, pggen.InsertLoginChallengeParams{
		ID: c.ID, AccountID: c.AccountID,
		Factors:   c.Factors,
		ExpiresAt: pgTimestamp(c.ExpiresAt), CreatedAt: pgTimestamp(c.CreatedAt),
	})
}

// LoginChallengeByID resolves a challenge by its high-entropy prefixed-UUIDv7
// id, or domain.ErrNotFound. The id is a continuation handle named in the
// finish URL: it grants no authority on its own (the second factor is still
// required), so it is looked up directly rather than verifier-hashed.
func (r *Resolver) LoginChallengeByID(ctx context.Context, id string) (LoginChallenge, error) {
	if r.sq != nil {
		row, err := r.sq.GetLoginChallengeByID(ctx, id)
		if err != nil {
			return LoginChallenge{}, notFoundOr(err)
		}
		expires, err := decodeTime(row.ExpiresAt)
		if err != nil {
			return LoginChallenge{}, err
		}
		created, err := decodeTime(row.CreatedAt)
		if err != nil {
			return LoginChallenge{}, err
		}
		return LoginChallenge{
			ID: row.ID, AccountID: row.AccountID,
			Factors: row.Factors, ExpiresAt: expires, Consumed: row.ConsumedAt.Valid, CreatedAt: created,
		}, nil
	}
	row, err := r.pg.GetLoginChallengeByID(ctx, id)
	if err != nil {
		return LoginChallenge{}, notFoundOr(err)
	}
	return LoginChallenge{
		ID: row.ID, AccountID: row.AccountID,
		Factors: row.Factors, ExpiresAt: row.ExpiresAt.Time, Consumed: row.ConsumedAt.Valid,
		CreatedAt: row.CreatedAt.Time,
	}, nil
}

// ConsumeLoginChallenge claims a challenge atomically; false means it was
// already consumed and the caller must fail closed.
func (r *Resolver) ConsumeLoginChallenge(ctx context.Context, id string, at time.Time) (bool, error) {
	if r.sq != nil {
		n, err := r.sq.ConsumeLoginChallenge(ctx, sqlitegen.ConsumeLoginChallengeParams{
			ConsumedAt: sql.NullString{String: encodeTime(at), Valid: true}, ID: id,
		})
		return n == 1, err
	}
	n, err := r.pg.ConsumeLoginChallenge(ctx, pggen.ConsumeLoginChallengeParams{
		ConsumedAt: pgTimestamp(at), ID: id,
	})
	return n == 1, err
}
