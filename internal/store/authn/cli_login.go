package authn

import (
	"context"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// CLILogin is an opaque first-party login transaction, never a federated transaction.
type CLILogin struct {
	ID, Transport, PKCEChallenge, RedirectURI, BrowserSessionID, Snapshot string
	StateVerifier, CodeVerifier, UserCodeVerifier                         []byte
	CreatedAt, ExpiresAt, ConsumedAt, LastPollAt                          time.Time
	PollInterval                                                          int64
}

func (h CLILogin) Live(now time.Time) bool { return h.ConsumedAt.IsZero() && now.Before(h.ExpiresAt) }
func (r *Resolver) CreateCLILogin(ctx context.Context, h CLILogin) error {
	if r.sq != nil {
		return r.sq.InsertCLILogin(ctx, sqlitegen.InsertCLILoginParams{ID: h.ID, StateVerifier: h.StateVerifier, CodeVerifier: h.CodeVerifier, UserCodeVerifier: h.UserCodeVerifier, Transport: h.Transport, PkceChallenge: h.PKCEChallenge, RedirectUri: h.RedirectURI, CreatedAt: encodeTime(h.CreatedAt), ExpiresAt: encodeTime(h.ExpiresAt), LastPollAt: encodeTime(h.CreatedAt)})
	}
	return r.pg.InsertCLILogin(ctx, pggen.InsertCLILoginParams{ID: h.ID, StateVerifier: h.StateVerifier, CodeVerifier: h.CodeVerifier, UserCodeVerifier: h.UserCodeVerifier, Transport: h.Transport, PkceChallenge: h.PKCEChallenge, RedirectUri: h.RedirectURI, CreatedAt: pgTimestamp(h.CreatedAt), ExpiresAt: pgTimestamp(h.ExpiresAt), LastPollAt: pgTimestamp(h.CreatedAt)})
}
func (r *Resolver) CLILoginByState(ctx context.Context, verifier []byte) (CLILogin, error) {
	if r.sq != nil {
		row, err := r.sq.CLILoginByState(ctx, verifier)
		if err != nil {
			return CLILogin{}, notFoundOr(err)
		}
		return cliLoginSQLite(row)
	}
	row, err := r.pg.CLILoginByState(ctx, verifier)
	if err != nil {
		return CLILogin{}, notFoundOr(err)
	}
	return cliLoginPG(row), nil
}
func (r *Resolver) CLILoginByCode(ctx context.Context, verifier []byte) (CLILogin, error) {
	if r.sq != nil {
		row, err := r.sq.CLILoginByCode(ctx, verifier)
		if err != nil {
			return CLILogin{}, notFoundOr(err)
		}
		return cliLoginSQLite(row)
	}
	row, err := r.pg.CLILoginByCode(ctx, verifier)
	if err != nil {
		return CLILogin{}, notFoundOr(err)
	}
	return cliLoginPG(row), nil
}
func (r *Resolver) CLILoginByUserCode(ctx context.Context, verifier []byte) (CLILogin, error) {
	if r.sq != nil {
		row, err := r.sq.CLILoginByUserCode(ctx, verifier)
		if err != nil {
			return CLILogin{}, notFoundOr(err)
		}
		return cliLoginSQLite(row)
	}
	row, err := r.pg.CLILoginByUserCode(ctx, verifier)
	if err != nil {
		return CLILogin{}, notFoundOr(err)
	}
	return cliLoginPG(row), nil
}
func cliLoginSQLite(row sqlitegen.CliLoginHandoff) (CLILogin, error) {
	created, e := decodeTime(row.CreatedAt)
	if e != nil {
		return CLILogin{}, e
	}
	expires, e := decodeTime(row.ExpiresAt)
	if e != nil {
		return CLILogin{}, e
	}
	consumed, e := decodeNullTime(row.ConsumedAt)
	if e != nil {
		return CLILogin{}, e
	}
	polled, e := decodeTime(row.LastPollAt)
	if e != nil {
		return CLILogin{}, e
	}
	return CLILogin{ID: row.ID, Transport: row.Transport, PKCEChallenge: row.PkceChallenge, RedirectURI: row.RedirectUri, BrowserSessionID: row.BrowserSessionID.String, Snapshot: row.Snapshot, StateVerifier: row.StateVerifier, CodeVerifier: row.CodeVerifier, UserCodeVerifier: row.UserCodeVerifier, CreatedAt: created, ExpiresAt: expires, ConsumedAt: consumed, LastPollAt: polled, PollInterval: row.PollInterval}, nil
}
func cliLoginPG(row pggen.CliLoginHandoff) CLILogin {
	return CLILogin{ID: row.ID, Transport: row.Transport, PKCEChallenge: row.PkceChallenge, RedirectURI: row.RedirectUri, BrowserSessionID: row.BrowserSessionID.String, Snapshot: row.Snapshot, StateVerifier: row.StateVerifier, CodeVerifier: row.CodeVerifier, UserCodeVerifier: row.UserCodeVerifier, CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time, ConsumedAt: row.ConsumedAt.Time, LastPollAt: row.LastPollAt.Time, PollInterval: row.PollInterval}
}
func (r *Resolver) ApproveCLILogin(ctx context.Context, id, session, snapshot string, code []byte) (bool, error) {
	if r.sq != nil {
		n, e := r.sq.ApproveCLILogin(ctx, sqlitegen.ApproveCLILoginParams{ID: id, BrowserSessionID: nullString(session), Snapshot: snapshot, CodeVerifier: code})
		return n == 1, e
	}
	n, e := r.pg.ApproveCLILogin(ctx, pggen.ApproveCLILoginParams{ID: id, BrowserSessionID: pgText(session), Snapshot: snapshot, CodeVerifier: code})
	return n == 1, e
}
func (r *Resolver) PollCLILogin(ctx context.Context, id string, at time.Time, interval int64) error {
	if r.sq != nil {
		return r.sq.PollCLILogin(ctx, sqlitegen.PollCLILoginParams{ID: id, LastPollAt: encodeTime(at), PollInterval: interval})
	}
	return r.pg.PollCLILogin(ctx, pggen.PollCLILoginParams{ID: id, LastPollAt: pgTimestamp(at), PollInterval: interval})
}
func (r *Resolver) ConsumeCLILogin(ctx context.Context, id string, at time.Time) (bool, error) {
	if r.sq != nil {
		n, e := r.sq.ConsumeCLILogin(ctx, sqlitegen.ConsumeCLILoginParams{ID: id, ConsumedAt: nullTimeString(at)})
		return n == 1, e
	}
	n, e := r.pg.ConsumeCLILogin(ctx, pggen.ConsumeCLILoginParams{ID: id, ConsumedAt: nullPGTime(at)})
	return n == 1, e
}

func (r *Resolver) PruneCLILogins(ctx context.Context, at time.Time) error {
	if r.sq != nil {
		return r.sq.PruneCLILogins(ctx, encodeTime(at))
	}
	return r.pg.PruneCLILogins(ctx, pgTimestamp(at))
}
