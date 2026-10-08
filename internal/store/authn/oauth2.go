package authn

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	sqlite "modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

type OAuth2Provider struct {
	Profile      string
	ID           string
	Slug         string
	DisplayName  string
	Kind         string
	Issuer       string
	ClientID     string
	ClientSecret []byte
	RedirectURI  string
	Enabled      bool
	DEKVersion   int64
	RowVersion   int64
}

func sqliteOAuth2Provider(row sqlitegen.Oauth2Provider) OAuth2Provider {
	return OAuth2Provider{
		ID: row.ID, Slug: row.Slug, DisplayName: row.DisplayName, Kind: row.Kind,
		Issuer: row.Issuer, ClientID: row.ClientID, ClientSecret: row.ClientSecret, Profile: row.Profile, RedirectURI: row.RedirectUri,
		Enabled: row.Enabled == 1, DEKVersion: row.DekVersion, RowVersion: row.RowVersion,
	}
}

func pgOAuth2Provider(row pggen.Oauth2Provider) OAuth2Provider {
	return OAuth2Provider{
		ID: row.ID, Slug: row.Slug, DisplayName: row.DisplayName, Kind: row.Kind,
		Issuer: row.Issuer, ClientID: row.ClientID, ClientSecret: row.ClientSecret, Profile: row.Profile, RedirectURI: row.RedirectUri,
		Enabled: row.Enabled == 1, DEKVersion: row.DekVersion, RowVersion: row.RowVersion,
	}
}

type NewOAuth2Provider struct {
	Profile      string
	ID           string
	Slug         string
	DisplayName  string
	Kind         string
	Issuer       string
	ClientID     string
	ClientSecret []byte
	RedirectURI  string
	Enabled      bool
	DEKVersion   int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// OAuth2ProviderUpdate is the provider reconfigure carrier. The issuer is absent: it
// is immutable after create (A3).
type OAuth2ProviderUpdate struct {
	ID           string
	DisplayName  string
	ClientID     string
	ClientSecret []byte
	RedirectURI  string
	Enabled      bool
	DEKVersion   int64
	RowVersion   int64
	UpdatedAt    time.Time
}

func (r *Resolver) CreateOAuth2Provider(ctx context.Context, n NewOAuth2Provider) error {
	if r.sq != nil {
		return oauth2ProviderConstraint(r.sq.CreateOAuth2Provider(ctx, sqlitegen.CreateOAuth2ProviderParams{
			ID: n.ID, Slug: n.Slug, DisplayName: n.DisplayName, Profile: n.Profile, Issuer: n.Issuer,
			ClientID: n.ClientID, ClientSecret: n.ClientSecret, RedirectUri: n.RedirectURI,
			Enabled: boolInt(n.Enabled), DekVersion: n.DEKVersion,
			CreatedAt: encodeTime(n.CreatedAt), UpdatedAt: encodeTime(n.UpdatedAt),
		}))
	}
	return oauth2ProviderConstraint(r.pg.CreateOAuth2Provider(ctx, pggen.CreateOAuth2ProviderParams{
		ID: n.ID, Slug: n.Slug, DisplayName: n.DisplayName, Profile: n.Profile, Issuer: n.Issuer,
		ClientID: n.ClientID, ClientSecret: n.ClientSecret, RedirectUri: n.RedirectURI,
		Enabled: boolInt(n.Enabled), DekVersion: n.DEKVersion,
		CreatedAt: pgTimestamp(n.CreatedAt), UpdatedAt: pgTimestamp(n.UpdatedAt),
	}))
}

// OAuth2ProviderBySlug resolves a provider by slug for administration (any enabled
// state), or domain.ErrNotFound.
func (r *Resolver) OAuth2ProviderBySlug(ctx context.Context, slug string) (OAuth2Provider, error) {
	if r.sq != nil {
		row, err := r.sq.GetOAuth2ProviderBySlug(ctx, slug)
		if err != nil {
			return OAuth2Provider{}, notFoundOr(err)
		}
		return sqliteOAuth2Provider(row), nil
	}
	row, err := r.pg.GetOAuth2ProviderBySlug(ctx, slug)
	if err != nil {
		return OAuth2Provider{}, notFoundOr(err)
	}
	return pgOAuth2Provider(row), nil
}

// ListOAuth2Providers lists every configured provider.
func (r *Resolver) ListOAuth2Providers(ctx context.Context) ([]OAuth2Provider, error) {
	if r.sq != nil {
		rows, err := r.sq.ListOAuth2Providers(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]OAuth2Provider, 0, len(rows))
		for _, row := range rows {
			out = append(out, sqliteOAuth2Provider(row))
		}
		return out, nil
	}
	rows, err := r.pg.ListOAuth2Providers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]OAuth2Provider, 0, len(rows))
	for _, row := range rows {
		out = append(out, pgOAuth2Provider(row))
	}
	return out, nil
}

// UpdateOAuth2Provider compare-and-swaps a provider on row_version; false means the
// row moved.
func (r *Resolver) UpdateOAuth2Provider(ctx context.Context, u OAuth2ProviderUpdate) (bool, error) {
	if r.sq != nil {
		n, err := r.sq.UpdateOAuth2ProviderCAS(ctx, sqlitegen.UpdateOAuth2ProviderCASParams{
			DisplayName: u.DisplayName, ClientID: u.ClientID, ClientSecret: u.ClientSecret, RedirectUri: u.RedirectURI,
			Enabled: boolInt(u.Enabled), DekVersion: u.DEKVersion, UpdatedAt: encodeTime(u.UpdatedAt),
			ID: u.ID, RowVersion: u.RowVersion,
		})
		return n == 1, oauth2ProviderConstraint(err)
	}
	n, err := r.pg.UpdateOAuth2ProviderCAS(ctx, pggen.UpdateOAuth2ProviderCASParams{
		DisplayName: u.DisplayName, ClientID: u.ClientID, ClientSecret: u.ClientSecret, RedirectUri: u.RedirectURI,
		Enabled: boolInt(u.Enabled), DekVersion: u.DEKVersion, UpdatedAt: pgTimestamp(u.UpdatedAt),
		ID: u.ID, RowVersion: u.RowVersion,
	})
	return n == 1, oauth2ProviderConstraint(err)
}

// LockOAuth2ProviderForDelete takes the provider row lock inside the delete tx so a
// concurrent Phase-C mint guard serializes behind it. Called before the session
// sweep so the sweep runs with the row held (A14). ErrNotFound means the row is
// already gone (a concurrent delete won).
func (r *Resolver) LockOAuth2ProviderForDelete(ctx context.Context, id string) error {
	if r.sq != nil {
		_, err := r.sq.LockOAuth2ProviderForDelete(ctx, id)
		return notFoundOr(err)
	}
	_, err := r.pg.LockOAuth2ProviderForDelete(ctx, id)
	return notFoundOr(err)
}

// DeleteOAuth2Provider removes a provider. Its transactions and federated sessions
// cascade (A14).
func (r *Resolver) DeleteOAuth2Provider(ctx context.Context, id string) error {
	if r.sq != nil {
		return r.sq.DeleteOAuth2Provider(ctx, id)
	}
	return r.pg.DeleteOAuth2Provider(ctx, id)
}

func (r *Resolver) GuardOAuth2ProviderForMint(ctx context.Context, id string, rowVersion int64, issuer string) (bool, error) {
	if r.sq != nil {
		n, err := r.sq.GuardOAuth2ProviderForMint(ctx, sqlitegen.GuardOAuth2ProviderForMintParams{ID: id, RowVersion: rowVersion, Issuer: issuer})
		return n == 1, err
	}
	n, err := r.pg.GuardOAuth2ProviderForMint(ctx, pggen.GuardOAuth2ProviderForMintParams{ID: id, RowVersion: rowVersion, Issuer: issuer})
	return n == 1, err
}

// OAuth2ProviderForCallback resolves the provider a transaction pinned, by id, so a
// callback exchanges only at the recorded provider (A11).
func (r *Resolver) OAuth2ProviderForCallback(ctx context.Context, id string) (OAuth2Provider, error) {
	if r.sq != nil {
		row, err := r.sq.GetOAuth2ProviderForCallback(ctx, id)
		if err != nil {
			return OAuth2Provider{}, notFoundOr(err)
		}
		return sqliteOAuth2Provider(row), nil
	}
	row, err := r.pg.GetOAuth2ProviderForCallback(ctx, id)
	if err != nil {
		return OAuth2Provider{}, notFoundOr(err)
	}
	return pgOAuth2Provider(row), nil
}

type OAuth2Transaction struct {
	AuthorityID            string
	ID                     string
	PKCEVerifier           string
	ProviderID             string
	Issuer                 string
	RedirectURI            string
	Purpose                string
	BindingKind            string
	InitiatingSessionID    string
	BrowserBindingVerifier []byte
	AccountID              string
	CeremonyID             string
	Browser                bool
	CredentialEpoch        int64
	CreatedAt              time.Time
	ExpiresAt              time.Time
	Consumed               bool
	// Intent is `sign-in` or `sign-up` on a login transaction (#604), empty
	// on every other purpose; SignupScopeOrgID names the org whose policy a
	// sign-up addresses, empty for the instance scope (spec 2.1).
	Intent           string
	SignupScopeOrgID string
}

// NewOAuth2Transaction is the transaction insert carrier.
type NewOAuth2Transaction struct {
	AuthorityID            string
	ID                     string
	StateVerifier          []byte
	PKCEVerifier           string
	ProviderID             string
	Issuer                 string
	RedirectURI            string
	Purpose                string
	BindingKind            string
	InitiatingSessionID    string
	BrowserBindingVerifier []byte
	AccountID              string
	CeremonyID             string
	Browser                bool
	CredentialEpoch        int64
	CreatedAt              time.Time
	ExpiresAt              time.Time
	Intent                 string
	SignupScopeOrgID       string
}

// CreateOAuth2Transaction writes a single-use transaction row.
func (r *Resolver) CreateOAuth2Transaction(ctx context.Context, t NewOAuth2Transaction) error {
	if r.sq != nil {
		return r.sq.InsertOAuth2Transaction(ctx, sqlitegen.InsertOAuth2TransactionParams{
			ID: t.ID, StateVerifier: t.StateVerifier, PkceVerifier: t.PKCEVerifier,
			ProviderID: t.ProviderID, Issuer: t.Issuer, RedirectUri: t.RedirectURI,
			Purpose: t.Purpose, BindingKind: t.BindingKind,
			InitiatingSessionID:    nullString(t.InitiatingSessionID),
			BrowserBindingVerifier: t.BrowserBindingVerifier,
			AccountID:              nullString(t.AccountID), AuthorityID: nullString(t.AuthorityID),
			CeremonyID:      nullString(t.CeremonyID),
			Browser:         boolInt(t.Browser),
			CredentialEpoch: t.CredentialEpoch,
			CreatedAt:       encodeTime(t.CreatedAt), ExpiresAt: encodeTime(t.ExpiresAt),
			Intent:           nullString(t.Intent),
			SignupScopeOrgID: nullString(t.SignupScopeOrgID),
		})
	}
	return r.pg.InsertOAuth2Transaction(ctx, pggen.InsertOAuth2TransactionParams{
		ID: t.ID, StateVerifier: t.StateVerifier, PkceVerifier: t.PKCEVerifier,
		ProviderID: t.ProviderID, Issuer: t.Issuer, RedirectUri: t.RedirectURI,
		Purpose: t.Purpose, BindingKind: t.BindingKind,
		InitiatingSessionID:    pgText(t.InitiatingSessionID),
		BrowserBindingVerifier: t.BrowserBindingVerifier,
		AccountID:              pgText(t.AccountID), AuthorityID: pgText(t.AuthorityID),
		CeremonyID:      pgText(t.CeremonyID),
		Browser:         t.Browser,
		CredentialEpoch: t.CredentialEpoch,
		CreatedAt:       pgTimestamp(t.CreatedAt), ExpiresAt: pgTimestamp(t.ExpiresAt),
		Intent:           pgText(t.Intent),
		SignupScopeOrgID: pgText(t.SignupScopeOrgID),
	})
}

// OAuth2TransactionByState resolves a transaction by the SHA-256 of its state
// artifact, or domain.ErrNotFound.
func (r *Resolver) OAuth2TransactionByState(ctx context.Context, stateVerifier []byte) (OAuth2Transaction, error) {
	if r.sq != nil {
		row, err := r.sq.GetOAuth2TransactionByState(ctx, stateVerifier)
		if err != nil {
			return OAuth2Transaction{}, notFoundOr(err)
		}
		if !verifierMatches(row.StateVerifier, stateVerifier) {
			return OAuth2Transaction{}, domain.ErrNotFound
		}
		created, err := decodeTime(row.CreatedAt)
		if err != nil {
			return OAuth2Transaction{}, err
		}
		expires, err := decodeTime(row.ExpiresAt)
		if err != nil {
			return OAuth2Transaction{}, err
		}
		return OAuth2Transaction{
			ID: row.ID, PKCEVerifier: row.PkceVerifier, ProviderID: row.ProviderID,
			Issuer: row.Issuer, RedirectURI: row.RedirectUri, Purpose: row.Purpose,
			BindingKind: row.BindingKind, InitiatingSessionID: row.InitiatingSessionID.String,
			BrowserBindingVerifier: row.BrowserBindingVerifier, AccountID: row.AccountID.String, AuthorityID: row.AuthorityID.String,
			CeremonyID: row.CeremonyID.String, Browser: row.Browser != 0,
			CredentialEpoch: row.CredentialEpoch, CreatedAt: created, ExpiresAt: expires,
			Consumed: row.ConsumedAt.Valid, Intent: row.Intent.String, SignupScopeOrgID: row.SignupScopeOrgID.String,
		}, nil
	}
	row, err := r.pg.GetOAuth2TransactionByState(ctx, stateVerifier)
	if err != nil {
		return OAuth2Transaction{}, notFoundOr(err)
	}
	if !verifierMatches(row.StateVerifier, stateVerifier) {
		return OAuth2Transaction{}, domain.ErrNotFound
	}
	return OAuth2Transaction{
		ID: row.ID, PKCEVerifier: row.PkceVerifier, ProviderID: row.ProviderID,
		Issuer: row.Issuer, RedirectURI: row.RedirectUri, Purpose: row.Purpose,
		BindingKind: row.BindingKind, InitiatingSessionID: row.InitiatingSessionID.String,
		BrowserBindingVerifier: row.BrowserBindingVerifier, AccountID: row.AccountID.String, AuthorityID: row.AuthorityID.String,
		CeremonyID: row.CeremonyID.String, Browser: row.Browser,
		CredentialEpoch: row.CredentialEpoch, CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
		Consumed: row.ConsumedAt.Valid, Intent: row.Intent.String, SignupScopeOrgID: row.SignupScopeOrgID.String,
	}, nil
}

// ConsumeOAuth2Transaction claims a transaction atomically; false means it was
// already consumed and the caller must fail closed.
func (r *Resolver) ConsumeOAuth2Transaction(ctx context.Context, id string, at time.Time) (bool, error) {
	if r.sq != nil {
		n, err := r.sq.ConsumeOAuth2Transaction(ctx, sqlitegen.ConsumeOAuth2TransactionParams{
			ConsumedAt: sql.NullString{String: encodeTime(at), Valid: true}, ID: id,
		})
		return n == 1, err
	}
	n, err := r.pg.ConsumeOAuth2Transaction(ctx, pggen.ConsumeOAuth2TransactionParams{
		ConsumedAt: pgTimestamp(at), ID: id,
	})
	return n == 1, err
}

func (r *Resolver) BindSessionToOAuth2Provider(ctx context.Context, sessionID, providerID string) (bool, error) {
	if r.sq != nil {
		n, err := r.sq.BindSessionToOAuth2Provider(ctx, sqlitegen.BindSessionToOAuth2ProviderParams{
			Oauth2ProviderID: sql.NullString{String: providerID, Valid: true}, ID: sessionID,
		})
		return n == 1, err
	}
	n, err := r.pg.BindSessionToOAuth2Provider(ctx, pggen.BindSessionToOAuth2ProviderParams{
		Oauth2ProviderID: pgText(providerID), ID: sessionID,
	})
	return n == 1, err
}

func (r *Resolver) DeleteSessionsForOAuth2Provider(ctx context.Context, providerID string) (int64, error) {
	if r.sq != nil {
		return r.sq.DeleteSessionsForOAuth2Provider(ctx, sql.NullString{String: providerID, Valid: true})
	}
	return r.pg.DeleteSessionsForOAuth2Provider(ctx, pgText(providerID))
}

// CredentialAuthorityByID revalidates the authority pinned by a claim transaction.
func (r *Resolver) CredentialAuthorityByID(ctx context.Context, id string) (CredentialAuthority, error) {
	if r.sq != nil {
		row, err := r.sq.GetCredentialAuthorityByID(ctx, id)
		if err != nil {
			return CredentialAuthority{}, notFoundOr(err)
		}
		expires, err := decodeTime(row.ExpiresAt)
		if err != nil {
			return CredentialAuthority{}, err
		}
		return CredentialAuthority{ID: row.ID, AccountID: row.AccountID, Purpose: row.Purpose, IssuedBy: row.IssuedBy, CredentialEpoch: row.CredentialEpoch, ExpiresAt: expires, Consumed: row.ConsumedAt.Valid}, nil
	}
	row, err := r.pg.GetCredentialAuthorityByID(ctx, id)
	if err != nil {
		return CredentialAuthority{}, notFoundOr(err)
	}
	return CredentialAuthority{ID: row.ID, AccountID: row.AccountID, Purpose: row.Purpose, IssuedBy: row.IssuedBy, CredentialEpoch: row.CredentialEpoch, ExpiresAt: row.ExpiresAt.Time, Consumed: row.ConsumedAt.Valid}, nil
}

// ClaimFederatedAuthority consumes a credential-establishment authority for a
// federated claim (#610), recording which kind of credential it established
// (oidc or oauth2). The NULL guard is the atomic claim and a recovery-issued
// authority never matches; false means the caller lost and must fail closed.
func (r *Resolver) ClaimFederatedAuthority(ctx context.Context, id, kind string, at time.Time) (bool, error) {
	if r.sq != nil {
		n, err := r.sq.ClaimFederatedAuthority(ctx, sqlitegen.ClaimFederatedAuthorityParams{ID: id, EstablishedCredentialKind: kind, ConsumedAt: sql.NullString{String: encodeTime(at), Valid: true}})
		return n == 1, err
	}
	n, err := r.pg.ClaimFederatedAuthority(ctx, pggen.ClaimFederatedAuthorityParams{ID: id, EstablishedCredentialKind: kind, ConsumedAt: pgTimestamp(at)})
	return n == 1, err
}
func (r *Resolver) StampCredentialEstablish(ctx context.Context, sessionID, identityID string, expires time.Time) error {
	if r.sq != nil {
		return r.sq.StampCredentialEstablish(ctx, sqlitegen.StampCredentialEstablishParams{SessionID: sessionID, IdentityID: identityID, ExpiresAt: encodeTime(expires)})
	}
	return r.pg.StampCredentialEstablish(ctx, pggen.StampCredentialEstablishParams{SessionID: sessionID, IdentityID: identityID, ExpiresAt: pgTimestamp(expires)})
}

// oauth2ProviderConstraint maps only uniqueness failures to an operator conflict.
// Check, foreign-key and other driver failures remain faults.
func oauth2ProviderConstraint(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: duplicate OAuth2 provider", domain.ErrConflict)
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) && (sqliteErr.Code() == sqlitelib.SQLITE_CONSTRAINT_UNIQUE || sqliteErr.Code() == sqlitelib.SQLITE_CONSTRAINT_PRIMARYKEY) {
		return fmt.Errorf("%w: duplicate OAuth2 provider", domain.ErrConflict)
	}
	return err
}
