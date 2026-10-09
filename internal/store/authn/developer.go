package authn

import (
	"context"
	"crypto/subtle"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// DeveloperCredential is immutable delegation metadata; verifier never leaves authn.
type DeveloperCredential struct {
	ID                  string
	PrincipalID         domain.PrincipalID
	AuthorityPrincipal  domain.PrincipalID
	Scope               domain.Scope
	PrefixHint          string
	ParentSessionID     string
	ProviderID          string
	OAuth2ProviderID    string
	SAMLProviderID      string
	AuthMethod          string
	AuthorityGeneration int64
	CredentialEpoch     int64
	CreatedAt           time.Time
	ExpiresAt           time.Time
	RevokedAt           time.Time
}

func (c DeveloperCredential) Live(now time.Time, epoch int64) bool {
	return c.RevokedAt.IsZero() && c.CredentialEpoch == epoch && now.Before(c.ExpiresAt)
}
func (r *Resolver) DeveloperCredentialByVerifier(ctx context.Context, v []byte) (DeveloperCredential, error) {
	if r.sq != nil {
		row, err := r.sq.DeveloperCredentialByVerifier(ctx, v)
		if err != nil {
			return DeveloperCredential{}, notFoundOr(err)
		}
		if subtle.ConstantTimeCompare(v, row.Verifier) != 1 {
			return DeveloperCredential{}, domain.ErrNotFound
		}
		return sqliteDeveloper(row)
	}
	row, err := r.pg.DeveloperCredentialByVerifier(ctx, v)
	if err != nil {
		return DeveloperCredential{}, notFoundOr(err)
	}
	if subtle.ConstantTimeCompare(v, row.Verifier) != 1 {
		return DeveloperCredential{}, domain.ErrNotFound
	}
	return pgDeveloper(row)
}
func (r *Resolver) ListDeveloperCredentials(ctx context.Context) ([]DeveloperCredential, error) {
	out := []DeveloperCredential{}
	if r.sq != nil {
		rows, err := r.sq.ListDeveloperCredentials(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			c, err := sqliteDeveloper(row)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	} else {
		rows, err := r.pg.ListDeveloperCredentials(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			c, err := pgDeveloper(row)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	}
	return out, nil
}
func (r *Resolver) DeveloperCredentialPolicy(ctx context.Context) (time.Duration, error) {
	var seconds int64
	var err error
	if r.sq != nil {
		seconds, err = r.sq.GetDeveloperCredentialPolicy(ctx)
	} else {
		seconds, err = r.pg.GetDeveloperCredentialPolicy(ctx)
	}
	if err != nil {
		return 0, err
	}
	if seconds < 1 || seconds > 28800 {
		return 0, fmt.Errorf("authn: corrupt developer credential ceiling")
	}
	return time.Duration(seconds) * time.Second, nil
}
func (r *Resolver) SetDeveloperCredentialPolicy(ctx context.Context, d time.Duration) error {
	if d < time.Second || d > 8*time.Hour || d%time.Second != 0 {
		return domain.ErrInvalid
	}
	if r.sq != nil {
		return r.sq.SetDeveloperCredentialPolicy(ctx, int64(d/time.Second))
	}
	return r.pg.SetDeveloperCredentialPolicy(ctx, int64(d/time.Second))
}
func (r *Resolver) RevokeDeveloperCredential(ctx context.Context, id string, at time.Time) error {
	if r.sq != nil {
		return r.sq.RevokeDeveloperCredential(ctx, sqlitegen.RevokeDeveloperCredentialParams{ID: id, RevokedAt: nullString(encodeTime(at))})
	}
	return r.pg.RevokeDeveloperCredential(ctx, pggen.RevokeDeveloperCredentialParams{ID: id, RevokedAt: pgTimestamp(at)})
}
func (r *Resolver) ClampDeveloperCredentialExpiry(ctx context.Context, id string, at time.Time) error {
	if r.sq != nil {
		return r.sq.ClampDeveloperCredentialExpiry(ctx, sqlitegen.ClampDeveloperCredentialExpiryParams{ID: id, Ceiling: encodeTime(at)})
	}
	return r.pg.ClampDeveloperCredentialExpiry(ctx, pggen.ClampDeveloperCredentialExpiryParams{ID: id, Ceiling: pgTimestamp(at)})
}
func (r *Resolver) InsertDeveloperCredential(ctx context.Context, c DeveloperCredential, verifier []byte) error {
	if r.sq != nil {
		return r.sq.InsertDeveloperCredential(ctx, sqlitegen.InsertDeveloperCredentialParams{ID: c.ID, PrincipalID: string(c.PrincipalID), AuthorityPrincipalID: string(c.AuthorityPrincipal), OrgID: string(c.Scope.Org), ProjectID: string(c.Scope.Project), EnvID: string(c.Scope.Env), Verifier: verifier, PrefixHint: c.PrefixHint, ParentSessionID: c.ParentSessionID, ProviderID: c.ProviderID, Oauth2ProviderID: c.OAuth2ProviderID, SamlProviderID: c.SAMLProviderID, AuthMethod: c.AuthMethod, AuthorityGeneration: c.AuthorityGeneration, CredentialEpoch: c.CredentialEpoch, CreatedAt: encodeTime(c.CreatedAt), ExpiresAt: encodeTime(c.ExpiresAt)})
	}
	return r.pg.InsertDeveloperCredential(ctx, pggen.InsertDeveloperCredentialParams{ID: c.ID, PrincipalID: string(c.PrincipalID), AuthorityPrincipalID: string(c.AuthorityPrincipal), OrgID: string(c.Scope.Org), ProjectID: string(c.Scope.Project), EnvID: string(c.Scope.Env), Verifier: verifier, PrefixHint: c.PrefixHint, ParentSessionID: c.ParentSessionID, ProviderID: c.ProviderID, Oauth2ProviderID: c.OAuth2ProviderID, SamlProviderID: c.SAMLProviderID, AuthMethod: c.AuthMethod, AuthorityGeneration: c.AuthorityGeneration, CredentialEpoch: c.CredentialEpoch, CreatedAt: pgTimestamp(c.CreatedAt), ExpiresAt: pgTimestamp(c.ExpiresAt)})
}
func sqliteDeveloper(row sqlitegen.DeveloperCredential) (DeveloperCredential, error) {
	c := DeveloperCredential{ID: row.ID, PrincipalID: domain.PrincipalID(row.PrincipalID), AuthorityPrincipal: domain.PrincipalID(row.AuthorityPrincipalID), Scope: domain.Scope{Org: domain.OrgID(row.OrgID), Project: domain.ProjectID(row.ProjectID), Env: domain.EnvID(row.EnvID)}, PrefixHint: row.PrefixHint, ParentSessionID: row.ParentSessionID, ProviderID: row.ProviderID, OAuth2ProviderID: row.Oauth2ProviderID, SAMLProviderID: row.SamlProviderID, AuthMethod: row.AuthMethod, AuthorityGeneration: row.AuthorityGeneration, CredentialEpoch: row.CredentialEpoch}
	var err error
	c.CreatedAt, err = decodeTime(row.CreatedAt)
	if err != nil {
		return c, err
	}
	c.ExpiresAt, err = decodeTime(row.ExpiresAt)
	if err != nil {
		return c, err
	}
	if row.RevokedAt.Valid {
		c.RevokedAt, err = decodeTime(row.RevokedAt.String)
		if err != nil {
			return c, err
		}
	}
	return c, nil
}
func pgDeveloper(row pggen.DeveloperCredential) (DeveloperCredential, error) {
	c := DeveloperCredential{ID: row.ID, PrincipalID: domain.PrincipalID(row.PrincipalID), AuthorityPrincipal: domain.PrincipalID(row.AuthorityPrincipalID), Scope: domain.Scope{Org: domain.OrgID(row.OrgID), Project: domain.ProjectID(row.ProjectID), Env: domain.EnvID(row.EnvID)}, PrefixHint: row.PrefixHint, ParentSessionID: row.ParentSessionID, ProviderID: row.ProviderID, OAuth2ProviderID: row.Oauth2ProviderID, SAMLProviderID: row.SamlProviderID, AuthMethod: row.AuthMethod, AuthorityGeneration: row.AuthorityGeneration, CredentialEpoch: row.CredentialEpoch}
	if !row.CreatedAt.Valid || !row.ExpiresAt.Valid {
		return c, fmt.Errorf("authn: missing developer timestamps")
	}
	c.CreatedAt = row.CreatedAt.Time
	c.ExpiresAt = row.ExpiresAt.Time
	if row.RevokedAt.Valid {
		c.RevokedAt = row.RevokedAt.Time
	}
	return c, nil
}
