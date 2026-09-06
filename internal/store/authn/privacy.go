package authn

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// PrivacyAccountView is deliberately free of credential material. Only host-local
// operator workflows may read restricted accounts through this resolution seam.
type PrivacyAccountView struct {
	ID          string    `json:"account_id"`
	PrincipalID string    `json:"principal_id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email"`
	CreatedAt   time.Time `json:"created_at"`
	State       string    `json:"state"`
}
type PrivacyActivity struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	At        time.Time `json:"at"`
	Outcome   string    `json:"outcome"`
	SourceIP  string    `json:"source_ip,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
}

func (r *Resolver) PrivacyAccount(ctx context.Context, principal string) (PrivacyAccountView, error) {
	if r.sq != nil {
		a, err := r.sq.PrivacyAccount(ctx, principal)
		if err != nil {
			return PrivacyAccountView{}, notFoundOr(err)
		}
		at, err := decodeTime(a.CreatedAt)
		return PrivacyAccountView{a.ID, a.PrincipalID, a.Username, a.DisplayName, a.Email, at, a.PrivacyState}, err
	}
	a, err := r.pg.PrivacyAccount(ctx, principal)
	if err != nil {
		return PrivacyAccountView{}, notFoundOr(err)
	}
	return PrivacyAccountView{a.ID, a.PrincipalID, a.Username, a.DisplayName, a.Email, a.CreatedAt.Time, a.PrivacyState}, nil
}
func (r *Resolver) PrivacyActivity(ctx context.Context, principal string) ([]PrivacyActivity, error) {
	out := []PrivacyActivity{}
	if r.sq != nil {
		rows, err := r.sq.PrivacyAuditInstance(ctx, nullString(principal))
		if err != nil {
			return nil, err
		}
		if len(rows) > 10000 {
			return nil, errors.New("privacy: activity exceeds 10000 rows per trail; use reviewed paged audit export")
		}
		for _, v := range rows {
			at, err := decodeTime(v.OccurredAt)
			if err != nil {
				return nil, err
			}
			out = append(out, PrivacyActivity{v.ID, v.Type, at, v.Outcome, v.SourceIp.String, v.UserAgent.String})
		}
	} else {
		rows, err := r.pg.PrivacyAuditInstance(ctx, pgText(principal))
		if err != nil {
			return nil, err
		}
		if len(rows) > 10000 {
			return nil, errors.New("privacy: activity exceeds 10000 rows per trail; use reviewed paged audit export")
		}
		for _, v := range rows {
			out = append(out, PrivacyActivity{v.ID, v.Type, v.OccurredAt.Time, v.Outcome, v.SourceIp.String, v.UserAgent.String})
		}
	}
	if r.sq != nil {
		rows, err := r.sq.PrivacyAuditTenant(ctx, nullString(principal))
		if err != nil {
			return nil, err
		}
		if len(rows) > 10000 {
			return nil, errors.New("privacy: activity exceeds 10000 rows per trail; use reviewed paged audit export")
		}
		for _, v := range rows {
			at, err := decodeTime(v.OccurredAt)
			if err != nil {
				return nil, err
			}
			out = append(out, PrivacyActivity{v.ID, v.Type, at, v.Outcome, v.SourceIp.String, v.UserAgent.String})
		}
	} else {
		rows, err := r.pg.PrivacyAuditTenant(ctx, pgText(principal))
		if err != nil {
			return nil, err
		}
		if len(rows) > 10000 {
			return nil, errors.New("privacy: activity exceeds 10000 rows per trail; use reviewed paged audit export")
		}
		for _, v := range rows {
			out = append(out, PrivacyActivity{v.ID, v.Type, v.OccurredAt.Time, v.Outcome, v.SourceIp.String, v.UserAgent.String})
		}
	}
	return out, nil
}

// RestrictPrivacyPrincipal serializes on the same principal row as grant writers.
// The caller checks lockout and records the operation in this transaction.
func (r *Resolver) RestrictPrivacyPrincipal(ctx context.Context, principal, state string) error {
	if err := r.LockPrincipalRow(ctx, domain.PrincipalID(principal)); err != nil {
		return err
	}
	if r.sq != nil {
		return r.sq.PrivacySetState(ctx, sqlitegen.PrivacySetStateParams{PrivacyState: state, PrincipalID: principal})
	}
	return r.pg.PrivacySetState(ctx, pggen.PrivacySetStateParams{PrivacyState: state, PrincipalID: principal})
}

// ErasePrivacyAccount deletes authentication custody and direct account identity.
// Historical content and audit records retain pseudonymous references, not names.
func (r *Resolver) ErasePrivacyAccount(ctx context.Context, account, principal, username string) error {
	if err := r.LockPrincipalRow(ctx, domain.PrincipalID(principal)); err != nil {
		return err
	}
	if r.sq != nil {
		if err := r.sq.PrivacyEraseHandoffs(ctx, principal); err != nil {
			return err
		}
	} else {
		if err := r.pg.PrivacyEraseHandoffs(ctx, principal); err != nil {
			return err
		}
	}
	if r.sq != nil {
		if err := r.sq.PrivacyEraseAuthorities(ctx, account); err != nil {
			return err
		}
	} else {
		if err := r.pg.PrivacyEraseAuthorities(ctx, account); err != nil {
			return err
		}
	}
	if r.sq != nil {
		if err := r.sq.PrivacyErasePasswords(ctx, account); err != nil {
			return err
		}
	} else {
		if err := r.pg.PrivacyErasePasswords(ctx, account); err != nil {
			return err
		}
	}
	if r.sq != nil {
		if err := r.sq.PrivacyEraseTOTPChallenges(ctx, account); err != nil {
			return err
		}
	} else {
		if err := r.pg.PrivacyEraseTOTPChallenges(ctx, account); err != nil {
			return err
		}
	}
	if r.sq != nil {
		if err := r.sq.PrivacyEraseTOTP(ctx, account); err != nil {
			return err
		}
	} else {
		if err := r.pg.PrivacyEraseTOTP(ctx, account); err != nil {
			return err
		}
	}
	if r.sq != nil {
		if err := r.sq.PrivacyEraseRecoveryCodes(ctx, account); err != nil {
			return err
		}
	} else {
		if err := r.pg.PrivacyEraseRecoveryCodes(ctx, account); err != nil {
			return err
		}
	}
	if r.sq != nil {
		if err := r.sq.PrivacyEraseCeremonies(ctx, nullString(account)); err != nil {
			return err
		}
	} else {
		if err := r.pg.PrivacyEraseCeremonies(ctx, pgText(account)); err != nil {
			return err
		}
	}
	if r.sq != nil {
		if err := r.sq.PrivacyEraseWebAuthn(ctx, account); err != nil {
			return err
		}
	} else {
		if err := r.pg.PrivacyEraseWebAuthn(ctx, account); err != nil {
			return err
		}
	}
	if r.sq != nil {
		if err := r.sq.PrivacyEraseOIDCTransactions(ctx, nullString(account)); err != nil {
			return err
		}
	} else {
		if err := r.pg.PrivacyEraseOIDCTransactions(ctx, pgText(account)); err != nil {
			return err
		}
	}
	if r.sq != nil {
		if err := r.sq.PrivacyEraseSAMLTransactions(ctx, nullString(account)); err != nil {
			return err
		}
	} else {
		if err := r.pg.PrivacyEraseSAMLTransactions(ctx, pgText(account)); err != nil {
			return err
		}
	}
	if r.sq != nil {
		if err := r.sq.PrivacyEraseExternalIdentities(ctx, account); err != nil {
			return err
		}
	} else {
		if err := r.pg.PrivacyEraseExternalIdentities(ctx, account); err != nil {
			return err
		}
	}
	if r.sq != nil {
		if err := r.sq.PrivacyEraseSCIMMembers(ctx, account); err != nil {
			return err
		}
	} else {
		if err := r.pg.PrivacyEraseSCIMMembers(ctx, account); err != nil {
			return err
		}
	}
	if r.sq != nil {
		if err := r.sq.PrivacyEraseSCIMUsers(ctx, account); err != nil {
			return err
		}
	} else {
		if err := r.pg.PrivacyEraseSCIMUsers(ctx, account); err != nil {
			return err
		}
	}
	if r.sq != nil {
		if err := r.sq.PrivacyEraseGrantOrigins(ctx, principal); err != nil {
			return err
		}
	} else {
		if err := r.pg.PrivacyEraseGrantOrigins(ctx, principal); err != nil {
			return err
		}
	}
	if r.sq != nil {
		if err := r.sq.PrivacyEraseGrants(ctx, principal); err != nil {
			return err
		}
	} else {
		if err := r.pg.PrivacyEraseGrants(ctx, principal); err != nil {
			return err
		}
	}
	if r.sq != nil {
		return r.sq.PrivacyEraseAccount(ctx, sqlitegen.PrivacyEraseAccountParams{Username: username, AccountID: account})
	}
	return r.pg.PrivacyEraseAccount(ctx, pggen.PrivacyEraseAccountParams{Username: username, AccountID: account})
}

type PrivacySession struct {
	ID         string    `json:"id"`
	Artifact   string    `json:"artifact"`
	AuthMethod string    `json:"auth_method"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	SourceIP   string    `json:"source_ip"`
	UserAgent  string    `json:"user_agent"`
}

func (r *Resolver) PrivacySessions(ctx context.Context, p string) ([]PrivacySession, error) {
	out := []PrivacySession{}
	if r.sq != nil {
		rows, err := r.sq.PrivacySessions(ctx, p)
		if err != nil {
			return nil, err
		}
		if len(rows) > 10000 {
			return nil, errors.New("privacy: too many sessions")
		}
		for _, v := range rows {
			created, err := decodeTime(v.CreatedAt)
			if err != nil {
				return nil, err
			}
			seen, err := decodeTime(v.LastSeenAt)
			if err != nil {
				return nil, err
			}
			out = append(out, PrivacySession{v.ID, v.Artifact, v.AuthMethod, created, seen, v.SourceIp, v.UserAgent})
		}
	} else {
		rows, err := r.pg.PrivacySessions(ctx, p)
		if err != nil {
			return nil, err
		}
		if len(rows) > 10000 {
			return nil, errors.New("privacy: too many sessions")
		}
		for _, v := range rows {
			out = append(out, PrivacySession{v.ID, v.Artifact, v.AuthMethod, v.CreatedAt.Time, v.LastSeenAt.Time, v.SourceIp, v.UserAgent})
		}
	}
	return out, nil
}

func (r *Resolver) CorrectPrivacyAccount(ctx context.Context, account, username, displayName string) error {
	if r.sq != nil {
		return r.sq.PrivacyCorrectAccount(ctx, sqlitegen.PrivacyCorrectAccountParams{Username: username, DisplayName: displayName, AccountID: account})
	}
	return r.pg.PrivacyCorrectAccount(ctx, pggen.PrivacyCorrectAccountParams{Username: username, DisplayName: displayName, AccountID: account})
}
