package authn

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	sqlite "modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// Registration policy (#606, social-signin spec section 2.1). One row per
// scope (0..1 per org, 0..1 at instance scope, both enforced by unique
// indexes), with the local entry's domains and the external entries' claim
// values as child rows. The sign-up legs (#607, #608) resolve it before any
// principal exists, so it lives on the resolution surface; administration is
// authorized at the chokepoint (registration-policy.*) before these writers
// run. Every mutating method is named in lint.ResolutionSurfaceWriters.

// RegistrationPolicy is one resolved policy with its child rows.
type RegistrationPolicy struct {
	ID string
	// OrgID is empty for the instance-scope policy.
	OrgID domain.OrgID
	// AuthorityPrincipalID is empty only for a row with no authority (the
	// retired JIT fold would have produced one; nothing writes it today).
	AuthorityPrincipalID domain.PrincipalID
	Landing              string
	// Template is set exactly for landing org-template.
	Template     string
	LocalEnabled bool
	// FreshOrgCap is set (> 0) exactly for landing fresh-org.
	FreshOrgCap int64
	Domains     []string
	Entries     []RegistrationEntry
	RowVersion  int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// RegistrationEntry is one external entry: a provider row (never a slug) and
// an optional allowlist of one string claim.
type RegistrationEntry struct {
	ID           string
	ProviderKind string
	ProviderID   string
	Claim        string
	Values       []string
	CreatedAt    time.Time
}

// RegistrationSignupRef names one pending local sign-up row.
type RegistrationSignupRef struct {
	ID               string
	SignupScopeOrgID domain.OrgID
}

// NewRegistrationSignup is the pending-row insert carrier (#584). The request
// writer is #608's; the policy delete already clears these rows.
type NewRegistrationSignup struct {
	ID               string
	Email            string
	TokenVerifier    []byte
	PolicyID         string
	SignupScopeOrgID domain.OrgID
	CredentialEpoch  int64
	CreatedAt        time.Time
	ExpiresAt        time.Time
}

// ErrRegistrationValuesWithoutClaim is the writer half of the one cross-table
// rule no CHECK can hold (spec 2.1): an entry has values iff it has a claim.
var ErrRegistrationValuesWithoutClaim = fmt.Errorf("%w: a registration entry has allowlist values iff it has a claim", domain.ErrInvalid)

func nullCap(c int64) sql.NullInt64 {
	if c == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: c, Valid: true}
}

func pgCap(c int64) pgtype.Int8 {
	if c == 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: c, Valid: true}
}

// registrationConflict folds the two uniqueness guards (one policy per org,
// one instance policy) and duplicate child rows onto domain.ErrConflict so
// both engines answer one refusal.
func registrationConflict(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: a registration policy already exists for this scope", domain.ErrConflict)
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) && (sqliteErr.Code() == sqlitelib.SQLITE_CONSTRAINT_UNIQUE || sqliteErr.Code() == sqlitelib.SQLITE_CONSTRAINT_PRIMARYKEY) {
		return fmt.Errorf("%w: a registration policy already exists for this scope", domain.ErrConflict)
	}
	return err
}

func checkRegistrationEntries(p RegistrationPolicy) error {
	for _, e := range p.Entries {
		if (e.Claim == "") != (len(e.Values) == 0) {
			return ErrRegistrationValuesWithoutClaim
		}
	}
	return nil
}

// RegistrationPolicyFor resolves the policy of one scope with its child rows:
// org "" is the instance scope. domain.ErrNotFound when the scope has none.
func (r *Resolver) RegistrationPolicyFor(ctx context.Context, org domain.OrgID) (RegistrationPolicy, error) {
	var (
		p   RegistrationPolicy
		err error
	)
	if r.sq != nil {
		var row sqlitegen.RegistrationPolicy
		if org == "" {
			row, err = r.sq.GetInstanceRegistrationPolicy(ctx)
		} else {
			row, err = r.sq.GetOrgRegistrationPolicy(ctx, nullString(string(org)))
		}
		if err != nil {
			return RegistrationPolicy{}, notFoundOr(err)
		}
		p, err = sqliteRegistrationPolicy(row)
		if err != nil {
			return RegistrationPolicy{}, err
		}
	} else {
		var row pggen.RegistrationPolicy
		if org == "" {
			row, err = r.pg.GetInstanceRegistrationPolicy(ctx)
		} else {
			row, err = r.pg.GetOrgRegistrationPolicy(ctx, pgText(string(org)))
		}
		if err != nil {
			return RegistrationPolicy{}, notFoundOr(err)
		}
		p = pgRegistrationPolicy(row)
	}
	if err := r.loadRegistrationChildren(ctx, &p); err != nil {
		return RegistrationPolicy{}, err
	}
	return p, nil
}

func sqliteRegistrationPolicy(row sqlitegen.RegistrationPolicy) (RegistrationPolicy, error) {
	created, err := decodeTime(row.CreatedAt)
	if err != nil {
		return RegistrationPolicy{}, err
	}
	updated, err := decodeTime(row.UpdatedAt)
	if err != nil {
		return RegistrationPolicy{}, err
	}
	return RegistrationPolicy{
		ID: row.ID, OrgID: domain.OrgID(row.OrgID.String),
		AuthorityPrincipalID: domain.PrincipalID(row.AuthorityPrincipalID.String),
		Landing:              row.Landing, Template: row.Template.String,
		LocalEnabled: row.LocalEnabled == 1, FreshOrgCap: row.FreshOrgCap.Int64,
		RowVersion: row.RowVersion, CreatedAt: created, UpdatedAt: updated,
	}, nil
}

func pgRegistrationPolicy(row pggen.RegistrationPolicy) RegistrationPolicy {
	return RegistrationPolicy{
		ID: row.ID, OrgID: domain.OrgID(row.OrgID.String),
		AuthorityPrincipalID: domain.PrincipalID(row.AuthorityPrincipalID.String),
		Landing:              row.Landing, Template: row.Template.String,
		LocalEnabled: row.LocalEnabled, FreshOrgCap: row.FreshOrgCap.Int64,
		RowVersion: row.RowVersion, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}
}

func (r *Resolver) loadRegistrationChildren(ctx context.Context, p *RegistrationPolicy) error {
	values := map[string][]string{}
	if r.sq != nil {
		domains, err := r.sq.ListRegistrationPolicyDomains(ctx, p.ID)
		if err != nil {
			return err
		}
		p.Domains = domains
		rows, err := r.sq.ListRegistrationPolicyEntries(ctx, p.ID)
		if err != nil {
			return err
		}
		vals, err := r.sq.ListRegistrationPolicyEntryValues(ctx, p.ID)
		if err != nil {
			return err
		}
		for _, v := range vals {
			values[v.EntryID] = append(values[v.EntryID], v.Value)
		}
		for _, row := range rows {
			created, err := decodeTime(row.CreatedAt)
			if err != nil {
				return err
			}
			p.Entries = append(p.Entries, RegistrationEntry{
				ID: row.ID, ProviderKind: row.ProviderKind, ProviderID: row.ProviderID,
				Claim: row.Claim.String, Values: values[row.ID], CreatedAt: created,
			})
		}
		return nil
	}
	domains, err := r.pg.ListRegistrationPolicyDomains(ctx, p.ID)
	if err != nil {
		return err
	}
	p.Domains = domains
	rows, err := r.pg.ListRegistrationPolicyEntries(ctx, p.ID)
	if err != nil {
		return err
	}
	vals, err := r.pg.ListRegistrationPolicyEntryValues(ctx, p.ID)
	if err != nil {
		return err
	}
	for _, v := range vals {
		values[v.EntryID] = append(values[v.EntryID], v.Value)
	}
	for _, row := range rows {
		p.Entries = append(p.Entries, RegistrationEntry{
			ID: row.ID, ProviderKind: row.ProviderKind, ProviderID: row.ProviderID,
			Claim: row.Claim.String, Values: values[row.ID], CreatedAt: row.CreatedAt.Time,
		})
	}
	return nil
}

// CreateRegistrationPolicy inserts a policy and its child rows. A second
// policy for the same scope is domain.ErrConflict (the unique indexes).
func (r *Resolver) CreateRegistrationPolicy(ctx context.Context, p RegistrationPolicy) error {
	if err := checkRegistrationEntries(p); err != nil {
		return err
	}
	var err error
	if r.sq != nil {
		err = r.sq.InsertRegistrationPolicy(ctx, sqlitegen.InsertRegistrationPolicyParams{
			ID: p.ID, OrgID: nullString(string(p.OrgID)), AuthorityPrincipalID: nullString(string(p.AuthorityPrincipalID)),
			Landing: p.Landing, Template: nullString(p.Template), LocalEnabled: boolInt(p.LocalEnabled),
			FreshOrgCap: nullCap(p.FreshOrgCap), CreatedAt: encodeTime(p.CreatedAt), UpdatedAt: encodeTime(p.UpdatedAt),
		})
	} else {
		err = r.pg.InsertRegistrationPolicy(ctx, pggen.InsertRegistrationPolicyParams{
			ID: p.ID, OrgID: pgText(string(p.OrgID)), AuthorityPrincipalID: pgText(string(p.AuthorityPrincipalID)),
			Landing: p.Landing, Template: pgText(p.Template), LocalEnabled: p.LocalEnabled,
			FreshOrgCap: pgCap(p.FreshOrgCap), CreatedAt: pgTimestamp(p.CreatedAt), UpdatedAt: pgTimestamp(p.UpdatedAt),
		})
	}
	if err != nil {
		return registrationConflict(err)
	}
	return r.writeRegistrationChildren(ctx, p)
}

// ReplaceRegistrationPolicy compare-and-swaps the policy row on expected and
// replaces every child row (a PUT is a full replacement). false means the row
// moved or was deleted underneath the caller.
func (r *Resolver) ReplaceRegistrationPolicy(ctx context.Context, p RegistrationPolicy, expected int64) (bool, error) {
	if err := checkRegistrationEntries(p); err != nil {
		return false, err
	}
	var (
		n   int64
		err error
	)
	if r.sq != nil {
		n, err = r.sq.UpdateRegistrationPolicyCAS(ctx, sqlitegen.UpdateRegistrationPolicyCASParams{
			AuthorityPrincipalID: nullString(string(p.AuthorityPrincipalID)), Landing: p.Landing,
			Template: nullString(p.Template), LocalEnabled: boolInt(p.LocalEnabled), FreshOrgCap: nullCap(p.FreshOrgCap),
			UpdatedAt: encodeTime(p.UpdatedAt), ID: p.ID, RowVersion: expected,
		})
		if err == nil && n == 1 {
			err = r.sq.DeleteRegistrationPolicyEntryValues(ctx, p.ID)
		}
		if err == nil && n == 1 {
			err = r.sq.DeleteRegistrationPolicyEntries(ctx, p.ID)
		}
		if err == nil && n == 1 {
			err = r.sq.DeleteRegistrationPolicyDomains(ctx, p.ID)
		}
	} else {
		n, err = r.pg.UpdateRegistrationPolicyCAS(ctx, pggen.UpdateRegistrationPolicyCASParams{
			AuthorityPrincipalID: pgText(string(p.AuthorityPrincipalID)), Landing: p.Landing,
			Template: pgText(p.Template), LocalEnabled: p.LocalEnabled, FreshOrgCap: pgCap(p.FreshOrgCap),
			UpdatedAt: pgTimestamp(p.UpdatedAt), ID: p.ID, RowVersion: expected,
		})
		if err == nil && n == 1 {
			err = r.pg.DeleteRegistrationPolicyEntryValues(ctx, p.ID)
		}
		if err == nil && n == 1 {
			err = r.pg.DeleteRegistrationPolicyEntries(ctx, p.ID)
		}
		if err == nil && n == 1 {
			err = r.pg.DeleteRegistrationPolicyDomains(ctx, p.ID)
		}
	}
	if err != nil || n != 1 {
		return false, err
	}
	return true, r.writeRegistrationChildren(ctx, p)
}

// writeRegistrationChildren inserts the domains, entries and claim values of p.
func (r *Resolver) writeRegistrationChildren(ctx context.Context, p RegistrationPolicy) error {
	for _, d := range p.Domains {
		var err error
		if r.sq != nil {
			err = r.sq.InsertRegistrationPolicyDomain(ctx, sqlitegen.InsertRegistrationPolicyDomainParams{PolicyID: p.ID, Domain: d})
		} else {
			err = r.pg.InsertRegistrationPolicyDomain(ctx, pggen.InsertRegistrationPolicyDomainParams{PolicyID: p.ID, Domain: d})
		}
		if err != nil {
			return registrationConflict(err)
		}
	}
	for _, e := range p.Entries {
		var err error
		if r.sq != nil {
			err = r.sq.InsertRegistrationPolicyEntry(ctx, sqlitegen.InsertRegistrationPolicyEntryParams{
				ID: e.ID, PolicyID: p.ID, ProviderKind: e.ProviderKind, ProviderID: e.ProviderID,
				Claim: nullString(e.Claim), CreatedAt: encodeTime(e.CreatedAt),
			})
		} else {
			err = r.pg.InsertRegistrationPolicyEntry(ctx, pggen.InsertRegistrationPolicyEntryParams{
				ID: e.ID, PolicyID: p.ID, ProviderKind: e.ProviderKind, ProviderID: e.ProviderID,
				Claim: pgText(e.Claim), CreatedAt: pgTimestamp(e.CreatedAt),
			})
		}
		if err != nil {
			return registrationConflict(err)
		}
		for _, v := range e.Values {
			if r.sq != nil {
				err = r.sq.InsertRegistrationPolicyEntryValue(ctx, sqlitegen.InsertRegistrationPolicyEntryValueParams{EntryID: e.ID, Value: v})
			} else {
				err = r.pg.InsertRegistrationPolicyEntryValue(ctx, pggen.InsertRegistrationPolicyEntryValueParams{EntryID: e.ID, Value: v})
			}
			if err != nil {
				return registrationConflict(err)
			}
		}
	}
	return nil
}

// DeleteRegistrationPolicy removes a policy at an expected row version; its
// child rows cascade. false means the row moved or is already gone. Pending
// sign-ups are NOT cascaded (registration_signups.policy_id is a trail
// pointer): the caller deletes them first, emitting their expiry.
func (r *Resolver) DeleteRegistrationPolicy(ctx context.Context, id string, expected int64) (bool, error) {
	var (
		n   int64
		err error
	)
	if r.sq != nil {
		n, err = r.sq.DeleteRegistrationPolicy(ctx, sqlitegen.DeleteRegistrationPolicyParams{ID: id, RowVersion: expected})
	} else {
		n, err = r.pg.DeleteRegistrationPolicy(ctx, pggen.DeleteRegistrationPolicyParams{ID: id, RowVersion: expected})
	}
	return n == 1, err
}

// CountRegistrationPolicyOrgs is the live count of orgs a policy minted that
// still exist: the `n` of a fresh-org policy's `n / cap` (#585 d9).
func (r *Resolver) CountRegistrationPolicyOrgs(ctx context.Context, policyID string) (int64, error) {
	if r.sq != nil {
		return r.sq.CountRegistrationPolicyOrgs(ctx, sql.NullString{String: policyID, Valid: true})
	}
	return r.pg.CountRegistrationPolicyOrgs(ctx, pgtype.Text{String: policyID, Valid: true})
}

// CreateRegistrationSignup inserts a pending local sign-up row.
func (r *Resolver) CreateRegistrationSignup(ctx context.Context, n NewRegistrationSignup) error {
	if r.sq != nil {
		return r.sq.InsertRegistrationSignup(ctx, sqlitegen.InsertRegistrationSignupParams{
			ID: n.ID, Email: n.Email, TokenVerifier: n.TokenVerifier, PolicyID: n.PolicyID,
			SignupScopeOrgID: nullString(string(n.SignupScopeOrgID)), CredentialEpoch: n.CredentialEpoch,
			CreatedAt: encodeTime(n.CreatedAt), ExpiresAt: encodeTime(n.ExpiresAt),
		})
	}
	return r.pg.InsertRegistrationSignup(ctx, pggen.InsertRegistrationSignupParams{
		ID: n.ID, Email: n.Email, TokenVerifier: n.TokenVerifier, PolicyID: n.PolicyID,
		SignupScopeOrgID: pgText(string(n.SignupScopeOrgID)), CredentialEpoch: n.CredentialEpoch,
		CreatedAt: pgTimestamp(n.CreatedAt), ExpiresAt: pgTimestamp(n.ExpiresAt),
	})
}

// RegistrationSignupsForPolicy lists the pending sign-ups a policy admitted.
func (r *Resolver) RegistrationSignupsForPolicy(ctx context.Context, policyID string) ([]RegistrationSignupRef, error) {
	var out []RegistrationSignupRef
	if r.sq != nil {
		rows, err := r.sq.ListRegistrationSignupsForPolicy(ctx, policyID)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			out = append(out, RegistrationSignupRef{ID: row.ID, SignupScopeOrgID: domain.OrgID(row.SignupScopeOrgID.String)})
		}
		return out, nil
	}
	rows, err := r.pg.ListRegistrationSignupsForPolicy(ctx, policyID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out = append(out, RegistrationSignupRef{ID: row.ID, SignupScopeOrgID: domain.OrgID(row.SignupScopeOrgID.String)})
	}
	return out, nil
}

// DeleteRegistrationSignup deletes one pending row; false means it is gone.
func (r *Resolver) DeleteRegistrationSignup(ctx context.Context, id string) (bool, error) {
	var (
		n   int64
		err error
	)
	if r.sq != nil {
		n, err = r.sq.DeleteRegistrationSignup(ctx, id)
	} else {
		n, err = r.pg.DeleteRegistrationSignup(ctx, id)
	}
	return n == 1, err
}
