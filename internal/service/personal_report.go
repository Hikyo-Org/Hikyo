package service

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// OffboardingLimits are the report's fixed statements of what it cannot show
// or undo (personal-values ADR, Offboarding 3 and 4).
var OffboardingLimits = []string{
	"Hikyo cannot undo a disclosure. It cannot revoke a token at its provider or erase what already left the server, so rotate every value listed here at its source.",
	"Deleted values persist in backups until those backups expire.",
	"Evidence starts at evidence_from. Audit retention has removed anything older.",
	"Only audited disclosures appear. Config-only exports are not audited per key, so config values read that way are not listed.",
	"The person's own personal values are not listed. Hikyo did not deliver them to anyone else.",
}

// PersonalCount is one key's matrix mark: counts, never values.
type PersonalCount struct {
	KeyID    string
	Name     string
	Holders  int
	Notices  int
	Required bool
}

// PersonalCounts is the matrix marks for one environment. Eligible is the
// denominator: humans currently holding both read and reveal there.
type PersonalCounts struct {
	Eligible int
	Items    []PersonalCount
}

// OffboardingReceipt is one shared key a person is recorded as receiving.
type OffboardingReceipt struct {
	EnvironmentID   domain.EnvID
	EnvironmentName string
	KeyName         string
	LastReceivedAt  time.Time
	ChangedSince    bool
}

// OffboardingServiceAccount is a service account the person created.
type OffboardingServiceAccount struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// OffboardingReport lists what to rotate after someone leaves (ADR,
// Offboarding 3). Names only.
type OffboardingReport struct {
	Principal       domain.PrincipalID
	Username        string
	EvidenceFrom    time.Time
	GeneratedAt     time.Time
	Received        []OffboardingReceipt
	ServiceAccounts []OffboardingServiceAccount
	Limits          []string
}

// Counts returns per-key holder and notice counts for the matrix marks. Only
// keys with a holder, a notice or a requirement are listed.
func (s *PersonalValues) Counts(ctx context.Context, actor Actor, scope domain.Scope) (PersonalCounts, error) {
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (PersonalCounts, error) {
		caller, err := s.caller(ctx, az, actor)
		if err != nil {
			return PersonalCounts{}, err
		}
		p, err := az.Authorize(ctx, caller, authz.OpPersonalValueCounts, scope)
		if err != nil {
			return PersonalCounts{}, err
		}
		keys, err := r.Catalogue().List(ctx, p)
		if err != nil {
			return PersonalCounts{}, err
		}
		counts, err := r.PersonalReport().Counts(ctx, p)
		if err != nil {
			return PersonalCounts{}, err
		}
		eligible, err := az.PersonalEligible(ctx, p)
		if err != nil {
			return PersonalCounts{}, err
		}
		out := PersonalCounts{Eligible: eligible, Items: []PersonalCount{}}
		for _, k := range keys {
			c := PersonalCount{KeyID: k.ID, Name: k.Name, Holders: counts.Holders[k.ID], Notices: counts.Notices[k.Name], Required: counts.Required[k.ID]}
			if c.Holders > 0 || c.Notices > 0 || c.Required {
				out.Items = append(out.Items, c)
			}
		}
		ev, err := domainEvent(ctx, audit.EventPersonalValueCountsRead, caller.Principal,
			audit.Object{Type: "environment", ID: string(scope.Env)}, audit.Payload{"keys": len(out.Items)})
		if err != nil {
			return PersonalCounts{}, err
		}
		return out, r.Audit().InsertTenant(ctx, p, ev)
	})
}

// OffboardingReport builds the offboarding report for one member of a project:
// the shared keys they are recorded as receiving and the service accounts they
// created, names only. member is a username or a principal id; a principal
// whose account is gone is still reported by id. Someone with no recorded tie
// to the project answers as nonexistent.
func (s *PersonalValues) OffboardingReport(ctx context.Context, actor Actor, scope domain.Scope, member string) (OffboardingReport, error) {
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (OffboardingReport, error) {
		caller, err := s.caller(ctx, az, actor)
		if err != nil {
			return OffboardingReport{}, err
		}
		p, err := az.Authorize(ctx, caller, authz.OpPersonalOffboardingReport, scope)
		if err != nil {
			return OffboardingReport{}, err
		}
		now := time.Now().UTC()
		out := OffboardingReport{GeneratedAt: now, Received: []OffboardingReceipt{}, ServiceAccounts: []OffboardingServiceAccount{}, Limits: slices.Clone(OffboardingLimits)}
		account, err := az.AccountByUsername(ctx, member)
		if errors.Is(err, domain.ErrNotFound) {
			account, err = az.AccountByPrincipal(ctx, domain.PrincipalID(member))
		}
		switch {
		case err == nil:
			out.Principal, out.Username = account.PrincipalID, account.Username
		case errors.Is(err, domain.ErrNotFound):
			out.Principal = domain.PrincipalID(member)
		default:
			return OffboardingReport{}, err
		}

		receipts, err := r.PersonalReport().Receipts(ctx, p, string(out.Principal))
		if err != nil {
			return OffboardingReport{}, err
		}
		accounts, err := az.ServiceAccountsIn(ctx, scope)
		if err != nil {
			return OffboardingReport{}, err
		}
		for _, sa := range accounts {
			if sa.CreatedBy == out.Principal {
				out.ServiceAccounts = append(out.ServiceAccounts, OffboardingServiceAccount{ID: sa.ID, Name: sa.Name, CreatedAt: sa.CreatedAt})
			}
		}
		if len(receipts) == 0 && len(out.ServiceAccounts) == 0 {
			tied, err := personalProjectTie(ctx, r, az, p, scope, out.Principal)
			if err != nil {
				return OffboardingReport{}, err
			}
			if !tied {
				return OffboardingReport{}, domain.ErrNotFound
			}
		}

		envs, err := r.Environments().List(ctx, p)
		if err != nil {
			return OffboardingReport{}, err
		}
		envNames := map[string]string{}
		for _, e := range envs {
			envNames[e.ID] = e.Name
		}
		changes, err := r.PersonalReport().KeyChanges(ctx, p)
		if err != nil {
			return OffboardingReport{}, err
		}
		changed := map[[2]string]time.Time{}
		for _, c := range changes {
			changed[[2]string{c.EnvironmentID, c.KeyName}] = c.At
		}
		for _, x := range receipts {
			out.Received = append(out.Received, OffboardingReceipt{
				EnvironmentID: domain.EnvID(x.EnvironmentID), EnvironmentName: envNames[x.EnvironmentID], KeyName: x.KeyName,
				LastReceivedAt: x.At, ChangedSince: changed[[2]string{x.EnvironmentID, x.KeyName}].After(x.At),
			})
		}

		// The evidence reaches back to the oldest event the project's trail
		// holds, and no further than the shorter, access-class retention.
		out.EvidenceFrom = now
		evidence, err := r.PersonalReport().Evidence(ctx, p)
		if err != nil {
			return OffboardingReport{}, err
		}
		if evidence.Any {
			out.EvidenceFrom = evidence.Oldest
			if bound := now.AddDate(0, 0, -evidence.AccessDays); evidence.AccessDays > 0 && bound.After(out.EvidenceFrom) {
				out.EvidenceFrom = bound
			}
		}

		ev, err := domainEvent(ctx, audit.EventPersonalOffboardingReportRead, caller.Principal,
			audit.Object{Type: "principal", ID: string(out.Principal)}, audit.Payload{
				"subject_principal_id": string(out.Principal), "received": len(out.Received), "service_accounts": len(out.ServiceAccounts),
			})
		if err != nil {
			return OffboardingReport{}, err
		}
		return out, r.Audit().InsertTenant(ctx, p, ev)
	})
}

// personalProjectTie reports whether a principal with nothing to report is
// still someone the project knows: a current grant reaching the project, or an
// event in its trail naming them.
func personalProjectTie(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, scope domain.Scope, principal domain.PrincipalID) (bool, error) {
	grants, err := az.GrantsOf(ctx, principal)
	if err != nil {
		return false, err
	}
	for _, g := range grants {
		if g.Scope.Org == scope.Org && (g.Scope.Project == "" || g.Scope.Project == scope.Project) {
			return true, nil
		}
	}
	return r.PersonalReport().MemberSeen(ctx, p, string(principal))
}
