package service

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Incident enumeration (incident-containment ADR, "Enumerating unresolved
// cleanup"). Every source reuses an existing reader: the grant and rule census,
// the session, developer-credential, service-account, machine-credential,
// invitation, linked-identity, factor and passkey readers on the resolution
// surface, the existing adapter, dynamic-provider and SCIM service readers, and
// the two new bounded lease and delegation listings. An object whose owner
// capability the caller lacks is listed as blocked, not read on the caller's
// behalf and not omitted.
//
// Grants and rules are census, not work: they decide the footprint and appear as
// counts. Containment keeps them and release never regrants (O1 B, O4 A), so no
// item revokes one.
//
// Every row carries Hikyo ids and closed vocabulary only. Names, issuers,
// subjects, usernames, addresses and adapter names never reach the ledger.

const (
	// maxSourceItems bounds one source's rows. A source with more is recorded
	// with an unknown, enumeration-truncated item so the ledger never claims a
	// complete census it did not take.
	maxSourceItems = 2000
	// maxScanPages bounds how many pages a filtered scan reads.
	maxScanPages = 20
	// maxLookups bounds the per-row follow-up reads one enumeration makes inside
	// its authorizing transaction (an account lookup per member, a credential
	// read per service account, a grant and footprint read per member). When it
	// is spent the source stops and records an enumeration_truncated item.
	maxLookups = 4000
)

// IncidentCensus counts the access rows the enumeration read. They decide the
// footprint; no item revokes them.
type IncidentCensus struct {
	Grants int
	Rules  int
}

// incidentCandidate is one row the enumeration found.
type incidentCandidate struct {
	owner, kind, id, scope string
	outcome                domain.ItemOutcome
	reason                 domain.ItemReason
}

// tenantRead is one existing tenant reader to run after the proofless step. The
// owner capabilities are probed in the authorizing transaction, so each item the
// tenant reader finds is pending only when the caller holds its owner's
// capability and blocked otherwise.
type tenantRead struct {
	source string
	scope  domain.Scope
	// adapterID narrows an adapter incident to its one adapter.
	adapterID string
	// verify makes a missing object an answer of not found.
	verify bool
	// configure and credentialRevoke are the owner capabilities of the items the
	// reader lists: pausing and moving targets, clearing or revoking credentials.
	configure        bool
	credentialRevoke bool
}

const (
	srcAdapters = "adapters"
	srcDynamic  = "dynamic"
	srcSCIM     = "scim"
)

type collector struct {
	cands  []incidentCandidate
	seen   map[string]bool
	reads  []tenantRead
	census IncidentCensus
	// probed remembers each capability probe by operation and scope, so a source
	// with many objects under one scope asks once.
	probed  map[string]bool
	lookups int
	budget  int
}

func newCollector(budget int) *collector {
	if budget <= 0 {
		budget = maxLookups
	}
	return &collector{seen: map[string]bool{}, probed: map[string]bool{}, budget: budget}
}

// spend takes n lookups from the enumeration's budget. It reports false, and
// takes nothing, when the budget cannot cover them.
func (c *collector) spend(n int) bool {
	if c.lookups+n > c.budget {
		return false
	}
	c.lookups += n
	return true
}

// holds reports whether the caller holds an owner's capability at a scope,
// without recording an operation or a denial, asking once per operation and scope.
func (c *collector) holds(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, op authz.Operation, scope domain.Scope) (bool, error) {
	key := string(op) + "\x00" + renderScope(scope)
	if v, ok := c.probed[key]; ok {
		return v, nil
	}
	var v bool
	var err error
	if scope == (domain.Scope{}) {
		v, err = az.HoldsInstanceCapability(ctx, caller, op)
	} else {
		v, err = az.CallerHolds(ctx, caller, op, scope)
	}
	if err != nil {
		return false, err
	}
	c.probed[key] = v
	return v, nil
}

func (c *collector) add(owner, kind, id string, scope domain.Scope, holds bool) {
	outcome, reason := domain.ItemPending, domain.ReasonNone
	if !holds {
		outcome, reason = domain.ItemBlocked, domain.ReasonNeedsAnotherOperator
	}
	c.put(incidentCandidate{owner: owner, kind: kind, id: id, scope: renderScope(scope), outcome: outcome, reason: reason})
}

func (c *collector) put(x incidentCandidate) {
	key := x.owner + "\x00" + x.kind + "\x00" + x.id
	if c.seen[key] {
		return
	}
	c.seen[key] = true
	c.cands = append(c.cands, x)
}

// unknown records an object whose state Hikyo cannot observe.
func (c *collector) unknown(owner, kind, id string, scope domain.Scope, reason domain.ItemReason) {
	c.put(incidentCandidate{owner: owner, kind: kind, id: id, scope: renderScope(scope), outcome: domain.ItemUnknown, reason: reason})
}

// truncated records that a source held more rows than the ledger takes.
func (c *collector) truncated(owner, id string, scope domain.Scope) {
	c.unknown(owner, domain.ObjectKindEnumeration, id, scope, domain.ReasonEnumerationTruncated)
}

// outOfScope lists, for one scope, what no workflow revokes. The five classes
// are the ADR's list and are the same for every scope kind: delivered plaintext,
// local copies, running processes, external sessions and issued certificates in
// use all outlive any one scope's cleanup.
func (c *collector) outOfScope(id string, scope domain.Scope) {
	for _, o := range []struct {
		kind   string
		reason domain.ItemReason
	}{
		{domain.ObjectKindDeliveredPlaintext, domain.ReasonDeliveredPlaintext},
		{domain.ObjectKindLocalCopy, domain.ReasonLocalCopy},
		{domain.ObjectKindRunningProcess, domain.ReasonRunningProcess},
		{domain.ObjectKindExternalSession, domain.ReasonExternalSession},
		{domain.ObjectKindIssuedCertificate, domain.ReasonIssuedCertificateUsed},
	} {
		c.put(incidentCandidate{owner: domain.OwnerNone, kind: o.kind, id: id, scope: renderScope(scope), outcome: domain.ItemOutOfScope, reason: o.reason})
	}
}

// blockedSource records a whole source the caller may not read.
func (c *collector) blockedSource(owner, kind, id string, scope domain.Scope) {
	c.put(incidentCandidate{owner: owner, kind: kind, id: id, scope: renderScope(scope), outcome: domain.ItemBlocked, reason: domain.ReasonNeedsAnotherOperator})
}

// noRevokeOwner records an object Hikyo holds and cannot remove administratively
// today (a linked identity, a factor, a passkey). It stays open until the person
// removes it themselves after release or the operator attests it resolved.
func (c *collector) noRevokeOwner(owner, kind, id string, scope domain.Scope) {
	c.unknown(owner, kind, id, scope, domain.ReasonNoRevokeOwner)
}

// pageDeveloperCredentials scans one org's live delegations in id order. keep
// narrows them; it reports whether the scan stopped early.
func pageDeveloperCredentials(ctx context.Context, az *authz.TxAuthorizer, org domain.OrgID, authority domain.PrincipalID, now time.Time,
	keep func(authz.DeveloperCredential) bool, add func(authz.DeveloperCredential) error) (bool, error) {
	after, kept := "", 0
	for page := 0; page < maxScanPages; page++ {
		rows, err := az.LiveDeveloperCredentialsInOrg(ctx, org, authority, after, authz.MaxIncidentPage, now)
		if err != nil {
			return false, err
		}
		for _, c := range rows {
			if !keep(c) {
				continue
			}
			if kept >= maxSourceItems {
				return true, nil
			}
			kept++
			if err := add(c); err != nil {
				return false, err
			}
		}
		if len(rows) < authz.MaxIncidentPage {
			return false, nil
		}
		after = rows[len(rows)-1].ID
	}
	return true, nil
}

// collect runs every proofless reader the target's scope names, inside the
// authorizing transaction, and records which tenant readers still have to run.
func (s *Incidents) collect(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, a incidentAccess, now time.Time) (*collector, error) {
	c := newCollector(s.MaxLookups)
	var err error
	switch a.kind {
	case domain.IncidentScopeAccount:
		err = s.collectAccount(ctx, az, a, now, c)
	case domain.IncidentScopeOrg:
		err = s.collectOrg(ctx, az, a, now, c)
	case domain.IncidentScopeProject:
		err = s.collectProject(ctx, r, az, a, now, c)
	case domain.IncidentScopeAdapter:
		err = s.collectAdapter(ctx, az, a, c)
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Incidents) collectAccount(ctx context.Context, az *authz.TxAuthorizer, a incidentAccess, now time.Time, c *collector) error {
	target := domain.PrincipalID(a.scopeID)
	instance := domain.Scope{}

	// Grants and rules are census: they decided the footprint and are counted.
	// Containment keeps them, so neither becomes a work item.
	grants, err := az.GrantRowsForPrincipal(ctx, target)
	if err != nil {
		return err
	}
	rules, err := az.RulesOf(ctx, target)
	if err != nil {
		return err
	}
	c.census = IncidentCensus{Grants: len(grants), Rules: len(rules)}

	// Ending a person's sessions and delegations is the credential-reset
	// operation the incident itself was authorized under (O2: a holder who can
	// reset a password could already end that person's sessions), so the opener
	// holds those owners' capability by construction and no per-row probe applies.
	sessions, err := az.SessionsForPrincipal(ctx, target)
	if err != nil {
		return err
	}
	for i, ss := range sessions {
		if i >= maxSourceItems {
			c.truncated(domain.OwnerSessionRevoke, a.scopeID, instance)
			break
		}
		c.add(domain.OwnerSessionRevoke, domain.ObjectKindSession, ss.ID, instance, true)
	}

	orgs := a.footprint
	if a.org != "" {
		orgs = []domain.OrgID{a.org}
	}
	for _, org := range orgs {
		stopped, err := pageDeveloperCredentials(ctx, az, org, target, now,
			func(authz.DeveloperCredential) bool { return true },
			func(d authz.DeveloperCredential) error { return s.addDeveloperCredential(ctx, az, a.caller, c, d) })
		if err != nil {
			return err
		}
		if stopped {
			c.truncated(domain.OwnerDeveloperRevoke, string(org), domain.Scope{Org: org})
		}
	}

	// Linked identities, the TOTP factor and passkeys: Hikyo holds them and has
	// no administrative removal, so each is a ledger item the person removes
	// themselves after release or the operator attests resolved (ADR, Credential
	// replacement). They are listed with that truth, not as pending revocations.
	identities, err := az.ExternalIdentitiesForAccount(ctx, a.account.ID)
	if err != nil {
		return err
	}
	for i, x := range identities {
		if i >= maxSourceItems {
			c.truncated(domain.OwnerExternalIdentity, a.scopeID, instance)
			break
		}
		c.noRevokeOwner(domain.OwnerExternalIdentity, domain.ObjectKindExternalIdentity, x.ID, instance)
	}
	totp, err := az.ConfirmedTOTP(ctx, a.account.ID)
	switch {
	case err == nil:
		c.noRevokeOwner(domain.OwnerFactorRemoval, domain.ObjectKindTOTPFactor, totp.ID, instance)
	case !errors.Is(err, domain.ErrNotFound):
		return err
	}
	passkeys, err := az.WebAuthnCredentialsForAccount(ctx, a.account.ID)
	if err != nil {
		return err
	}
	for i, k := range passkeys {
		if i >= maxSourceItems {
			c.truncated(domain.OwnerFactorRemoval, a.scopeID, instance)
			break
		}
		if k.Disabled {
			continue
		}
		c.noRevokeOwner(domain.OwnerFactorRemoval, domain.ObjectKindPasskey, k.ID, instance)
	}

	// The created-by relation of the offboarding report: machine principals the
	// account created are listed, not revoked unless selected.
	after, seen := "", 0
	for {
		rows, err := az.ServiceAccountsCreatedBy(ctx, target, a.org, after, authz.MaxIncidentPage)
		if err != nil {
			return err
		}
		for _, sa := range rows {
			if seen >= maxSourceItems {
				c.truncated(domain.OwnerServiceAccountReview, a.scopeID, instance)
				rows = nil
				break
			}
			seen++
			scope := domain.Scope{Org: sa.Org, Project: sa.Project}
			ok, err := c.holds(ctx, az, a.caller, authz.OpServiceAccountDelete, scope)
			if err != nil {
				return err
			}
			c.add(domain.OwnerServiceAccountReview, domain.ObjectKindServiceAccount, sa.ID, scope, ok)
		}
		if len(rows) < authz.MaxIncidentPage {
			break
		}
		after = rows[len(rows)-1].ID
	}

	c.outOfScope(a.scopeID, instance)
	return nil
}

func (s *Incidents) addDeveloperCredential(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, c *collector, d authz.DeveloperCredential) error {
	// An org-level delegation is revoked under the credential-reset the incident
	// was authorized with; a project-level one needs the project owner's revoke.
	ok := true
	if d.Scope.Project != "" {
		var err error
		ok, err = c.holds(ctx, az, caller, authz.OpDeveloperCredentialRevokeProject, domain.Scope{Org: d.Scope.Org, Project: d.Scope.Project})
		if err != nil {
			return err
		}
	}
	c.add(domain.OwnerDeveloperRevoke, domain.ObjectKindDeveloperCred, d.ID, d.Scope, ok)
	return nil
}

// memberCanContain reports whether the caller may contain one org member: the
// same classification credential-reset applies. A target with an instance grant
// is host-local, one inside exactly one org needs credential-reset there, and one
// in several orgs or none needs instance scope.
func (s *Incidents) memberCanContain(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, c *collector, p domain.PrincipalID) (bool, error) {
	grants, err := az.GrantsForResetTarget(ctx, p)
	if err != nil {
		return false, err
	}
	footprint, err := az.PrincipalFootprintOrgs(ctx, p)
	if err != nil {
		return false, err
	}
	org, instanceCap, orgCount := classifyResetTarget(grants, footprint)
	switch {
	case instanceCap:
		return false, nil
	case orgCount == 1:
		return c.holds(ctx, az, caller, authz.OpIncidentOrg, domain.Scope{Org: domain.OrgID(org)})
	}
	return c.holds(ctx, az, caller, authz.OpIncidentInstance, domain.Scope{})
}

func (s *Incidents) collectOrg(ctx context.Context, az *authz.TxAuthorizer, a incidentAccess, now time.Time, c *collector) error {
	scope := a.scope

	// The members come from the org's footprint: every principal with a grant or
	// a rule in it, so a member who holds rules and no grants is listed too. The
	// grants and rules are counted, not revoked. Machine principals hold grants
	// too; only human accounts are listed.
	lines, err := az.GrantLinesInOrg(ctx, string(a.org))
	if err != nil {
		return err
	}
	ruleLines, err := az.RuleLinesInOrg(ctx, a.org)
	if err != nil {
		return err
	}
	ruleIDs := map[string]bool{}
	for _, l := range ruleLines {
		ruleIDs[l.ID] = true
	}
	c.census = IncidentCensus{Grants: len(lines), Rules: len(ruleIDs)}

	candidates := make([]domain.PrincipalID, 0, len(lines)+len(ruleLines))
	for _, l := range lines {
		candidates = append(candidates, l.Principal)
	}
	for _, l := range ruleLines {
		candidates = append(candidates, l.Principal)
	}
	members, listed := map[domain.PrincipalID]bool{}, 0
	for _, p := range candidates {
		if members[p] {
			continue
		}
		members[p] = true
		if listed >= maxSourceItems || !c.spend(3) {
			c.truncated(domain.OwnerAccountContain, a.scopeID, scope)
			break
		}
		if _, err := az.AccountByPrincipal(ctx, p); errors.Is(err, domain.ErrNotFound) {
			continue
		} else if err != nil {
			return err
		}
		ok, err := s.memberCanContain(ctx, az, a.caller, c, p)
		if err != nil {
			return err
		}
		listed++
		c.add(domain.OwnerAccountContain, domain.ObjectKindAccount, string(p), scope, ok)
	}

	stopped, err := pageDeveloperCredentials(ctx, az, a.org, "", now,
		func(authz.DeveloperCredential) bool { return true },
		func(d authz.DeveloperCredential) error { return s.addDeveloperCredential(ctx, az, a.caller, c, d) })
	if err != nil {
		return err
	}
	if stopped {
		c.truncated(domain.OwnerDeveloperRevoke, a.scopeID, scope)
	}

	invitations, err := az.ListLiveOrgEmailInvitations(ctx, a.org, now)
	if err != nil {
		return err
	}
	if len(invitations) > 0 {
		ok, err := c.holds(ctx, az, a.caller, authz.OpEmailInvitationRevoke, scope)
		if err != nil {
			return err
		}
		for i, inv := range invitations {
			if i >= maxSourceItems {
				c.truncated(domain.OwnerEmailInvitation, a.scopeID, scope)
				break
			}
			c.add(domain.OwnerEmailInvitation, domain.ObjectKindEmailInvitation, inv.ID, scope, ok)
		}
	}

	canList, err := c.holds(ctx, az, a.caller, authz.OpSCIMBindingList, scope)
	if err != nil {
		return err
	}
	canRevoke, err := c.holds(ctx, az, a.caller, authz.OpSCIMCredentialRevoke, scope)
	if err != nil {
		return err
	}
	if canList {
		c.reads = append(c.reads, tenantRead{source: srcSCIM, scope: scope, credentialRevoke: canRevoke})
	} else {
		c.blockedSource(domain.OwnerSCIMCredential, domain.ObjectKindOrg, a.scopeID, scope)
	}

	c.outOfScope(a.scopeID, scope)
	return nil
}

func (s *Incidents) collectProject(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, a incidentAccess, now time.Time, c *collector) error {
	scope := a.scope

	// The project's grants and rules are census: counted, never revoked.
	lines, err := az.GrantLinesInOrg(ctx, string(a.org))
	if err != nil {
		return err
	}
	for _, l := range lines {
		if l.Grant.Scope.Project == scope.Project {
			c.census.Grants++
		}
	}
	rules, err := az.RulesForProject(ctx, a.org, scope.Project)
	if err != nil {
		return err
	}
	c.census.Rules = len(rules)

	// Service accounts and their live credentials.
	accounts, err := az.ServiceAccountsIn(ctx, scope)
	if err != nil {
		return err
	}
	epoch, err := az.CredentialEpoch(ctx)
	if err != nil {
		return err
	}
	ok, err := c.holds(ctx, az, a.caller, authz.OpCredentialRevoke, scope)
	if err != nil {
		return err
	}
	listed := 0
scan:
	for _, sa := range accounts {
		if !c.spend(1) {
			c.truncated(domain.OwnerMachineCredential, a.scopeID, scope)
			break
		}
		creds, err := az.MachineCredentialsFor(ctx, sa.ID)
		if err != nil {
			return err
		}
		for _, cred := range creds {
			if !cred.Live(now, epoch) {
				continue
			}
			if listed >= maxSourceItems {
				c.truncated(domain.OwnerMachineCredential, a.scopeID, scope)
				break scan
			}
			listed++
			c.add(domain.OwnerMachineCredential, domain.ObjectKindMachineCredential, cred.ID, scope, ok)
		}
	}

	stopped, err := pageDeveloperCredentials(ctx, az, a.org, "", now,
		func(d authz.DeveloperCredential) bool { return d.Scope.Project == scope.Project },
		func(d authz.DeveloperCredential) error { return s.addDeveloperCredential(ctx, az, a.caller, c, d) })
	if err != nil {
		return err
	}
	if stopped {
		c.truncated(domain.OwnerDeveloperRevoke, a.scopeID, scope)
	}

	// Non-terminal leases, through the new bounded project listing.
	proof, err := az.Authorize(ctx, a.caller, authz.OpIncidentLeasesProject, scope)
	if err != nil {
		return err
	}
	if err := pageProjectLeases(ctx, r, az, proof, a, c); err != nil {
		return err
	}

	if err := s.probeAdapters(ctx, az, a, c, ""); err != nil {
		return err
	}
	providers, err := c.holds(ctx, az, a.caller, authz.OpDynamicProviderInspect, scope)
	if err != nil {
		return err
	}
	if providers {
		revoke, err := c.holds(ctx, az, a.caller, authz.OpDynamicProviderCredentialRevoke, scope)
		if err != nil {
			return err
		}
		c.reads = append(c.reads, tenantRead{source: srcDynamic, scope: scope, credentialRevoke: revoke})
	} else {
		c.blockedSource(domain.OwnerDynamicCredential, domain.ObjectKindProject, a.scopeID, scope)
	}

	c.outOfScope(a.scopeID, scope)
	return nil
}

// probeAdapters decides whether the adapter reader runs and under which owner
// capabilities. A caller who may not inspect adapters still opens the incident
// (credential-reset at the org does, O2); the adapters are then recorded as
// blocked for another operator rather than read on the caller's behalf.
func (s *Incidents) probeAdapters(ctx context.Context, az *authz.TxAuthorizer, a incidentAccess, c *collector, adapterID string) error {
	inspect, err := c.holds(ctx, az, a.caller, authz.OpAdapterInspect, a.scope)
	if err != nil {
		return err
	}
	if !inspect {
		if adapterID != "" {
			c.blockedSource(domain.OwnerAdapterTargetPause, domain.ObjectKindAdapter, adapterID, a.scope)
		} else {
			c.blockedSource(domain.OwnerAdapterTargetPause, domain.ObjectKindProject, a.scopeID, a.scope)
		}
		return nil
	}
	configure, err := c.holds(ctx, az, a.caller, authz.OpAdapterConfigure, a.scope)
	if err != nil {
		return err
	}
	revoke, err := c.holds(ctx, az, a.caller, authz.OpAdapterCredentialRevoke, a.scope)
	if err != nil {
		return err
	}
	c.reads = append(c.reads, tenantRead{
		source: srcAdapters, scope: a.scope, adapterID: adapterID, verify: adapterID != "",
		configure: configure, credentialRevoke: revoke,
	})
	return nil
}

// pageProjectLeases lists a project's non-terminal leases a page at a time
// through the new bounded listing.
func pageProjectLeases(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, proof authz.Proof, a incidentAccess, c *collector) error {
	after, seen := "", 0
	for {
		rows, err := r.Dynamic().ListNonTerminalLeasesInProject(ctx, proof, after, authz.MaxIncidentPage)
		if err != nil {
			return err
		}
		for _, l := range rows {
			if seen >= maxSourceItems {
				c.truncated(domain.OwnerLeaseRevoke, a.scopeID, a.scope)
				return nil
			}
			seen++
			scope := domain.Scope{Org: domain.OrgID(l.OrgID), Project: domain.ProjectID(l.ProjectID), Env: domain.EnvID(l.EnvironmentID)}
			ok, err := c.holds(ctx, az, a.caller, authz.OpLeaseRevoke, scope)
			if err != nil {
				return err
			}
			c.add(domain.OwnerLeaseRevoke, domain.ObjectKindLease, l.ID, scope, ok)
		}
		if len(rows) < authz.MaxIncidentPage {
			return nil
		}
		after = rows[len(rows)-1].ID
	}
}

func (s *Incidents) collectAdapter(ctx context.Context, az *authz.TxAuthorizer, a incidentAccess, c *collector) error {
	if err := s.probeAdapters(ctx, az, a, c, a.adapterID); err != nil {
		return err
	}
	c.outOfScope(a.scopeID, a.scope)
	return nil
}

// readTenantSources runs each existing tenant reader in its own transaction
// under its own capability. A reader that refuses (the capability moved since
// the probe) records the source as blocked; any other failure fails the open.
func (s *Incidents) readTenantSources(ctx context.Context, actor Actor, c *collector) error {
	for _, r := range c.reads {
		var err error
		switch r.source {
		case srcAdapters:
			err = s.readAdapters(ctx, actor, c, r)
		case srcDynamic:
			err = s.readDynamic(ctx, actor, c, r)
		case srcSCIM:
			err = s.readSCIM(ctx, actor, c, r)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func refusedRead(err error) bool {
	return errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrUnauthorized)
}

func (s *Incidents) readAdapters(ctx context.Context, actor Actor, c *collector, r tenantRead) error {
	if s.Adapters == nil {
		return errIncidentUnwired
	}
	views, err := s.Adapters.List(ctx, actor, r.scope)
	if refusedRead(err) {
		if r.verify {
			c.blockedSource(domain.OwnerAdapterTargetPause, domain.ObjectKindAdapter, r.adapterID, r.scope)
		} else {
			c.blockedSource(domain.OwnerAdapterTargetPause, domain.ObjectKindProject, string(r.scope.Project), r.scope)
		}
		return nil
	}
	if err != nil {
		return err
	}
	found := false
	for _, v := range views {
		if r.adapterID != "" && v.Adapter.ID != r.adapterID {
			continue
		}
		found = true
		id := v.Adapter.ID
		if v.Adapter.CredentialPresent {
			c.add(domain.OwnerAdapterCredential, domain.ObjectKindAdapter, id, r.scope, r.credentialRevoke)
		}
		// The upstream PAT lives at the provider: Hikyo can only be told it was
		// revoked there.
		c.unknown(domain.OwnerAdapterUpstream, domain.ObjectKindAdapter, id, r.scope, domain.ReasonUpstreamNotObservable)
		for i, t := range v.Targets {
			if i >= maxSourceItems {
				c.truncated(domain.OwnerAdapterTargetPause, id, r.scope)
				break
			}
			c.add(domain.OwnerAdapterTargetPause, domain.ObjectKindAdapterTarget, t.ID, r.scope, r.configure)
		}
		// The adapter list shows an adapter mid route-move (state moving), so that
		// move is a ledger item. Target-level moves and scrub jobs are not
		// observable: the target reader lists active targets only, tombstoned
		// adapters are hidden, and no existing reader lists non-terminal moves or
		// scrub work (the move counter is private to teardown). A later slice needs
		// a bounded reader for them.
		if v.Adapter.State == "moving" {
			c.add(domain.OwnerAdapterMove, domain.ObjectKindAdapter, id, r.scope, r.configure)
		}
	}
	if r.verify && !found {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Incidents) readDynamic(ctx context.Context, actor Actor, c *collector, r tenantRead) error {
	if s.Dynamic == nil {
		return errIncidentUnwired
	}
	views, err := s.Dynamic.List(ctx, actor, r.scope)
	if refusedRead(err) {
		c.blockedSource(domain.OwnerDynamicCredential, domain.ObjectKindProject, string(r.scope.Project), r.scope)
		return nil
	}
	if err != nil {
		return err
	}
	for i, v := range views {
		if i >= maxSourceItems {
			c.truncated(domain.OwnerDynamicCredential, string(r.scope.Project), r.scope)
			break
		}
		c.add(domain.OwnerDynamicCredential, domain.ObjectKindDynamicProvider, v.ID, r.scope, r.credentialRevoke)
		c.unknown(domain.OwnerDynamicUpstream, domain.ObjectKindDynamicProvider, v.ID, r.scope, domain.ReasonUpstreamNotObservable)
	}
	return nil
}

func (s *Incidents) readSCIM(ctx context.Context, actor Actor, c *collector, r tenantRead) error {
	if s.SCIM == nil {
		return errIncidentUnwired
	}
	org := r.scope.Org
	bindings, err := s.SCIM.ListBindings(ctx, actor, org)
	if refusedRead(err) {
		c.blockedSource(domain.OwnerSCIMCredential, domain.ObjectKindOrg, string(org), r.scope)
		return nil
	}
	if err != nil {
		return err
	}
	listed := 0
	for _, b := range bindings {
		creds, err := s.SCIM.ListCredentials(ctx, actor, org, b.ID)
		if refusedRead(err) {
			c.blockedSource(domain.OwnerSCIMCredential, domain.ObjectKindSCIMBinding, b.ID, r.scope)
			continue
		}
		if err != nil {
			return err
		}
		for _, cred := range creds {
			if !cred.Live {
				continue
			}
			if listed >= maxSourceItems {
				c.truncated(domain.OwnerSCIMCredential, string(org), r.scope)
				return nil
			}
			listed++
			c.add(domain.OwnerSCIMCredential, domain.ObjectKindSCIMCredential, cred.ID, r.scope, r.credentialRevoke)
		}
	}
	return nil
}

var errIncidentUnwired = errors.New("service: incident enumeration is not wired to its tenant readers")
