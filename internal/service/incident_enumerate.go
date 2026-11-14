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
// cleanup"). Every source reuses an existing reader: the grant, session,
// developer-credential, service-account, machine-credential, invitation and
// linked-identity readers on the resolution surface, the existing adapter,
// dynamic-provider and SCIM service readers, and the two new bounded lease and
// delegation listings. An object whose owner capability the caller lacks is
// listed as blocked, not read on the caller's behalf and not omitted.
//
// Every row carries Hikyo ids and closed vocabulary only. Names, issuers,
// subjects, usernames, addresses and adapter names never reach the ledger.

// Owner functions: the closed vocabulary of who revokes or resolves an item.
const (
	OwnerGrantRevoke          = "grant.revoke"
	OwnerSessionRevoke        = "session.revoke"
	OwnerDeveloperRevoke      = "developer_credential.revoke"
	OwnerExternalIdentity     = "external_identity.remove"
	OwnerServiceAccountReview = "service_account.review"
	OwnerMachineCredential    = "machine_credential.revoke"
	OwnerAccountContain       = "account.contain"
	OwnerEmailInvitation      = "email_invitation.revoke"
	OwnerSCIMCredential       = "scim_credential.revoke"
	OwnerLeaseRevoke          = "lease.revoke"
	OwnerAdapterTargetPause   = "adapter_target.pause"
	OwnerAdapterCredential    = "adapter_credential.clear"
	OwnerAdapterUpstream      = "adapter_credential.upstream_revoke"
	OwnerDynamicCredential    = "dynamic_provider.credential_clear"
	OwnerDynamicUpstream      = "dynamic_provider.upstream_revoke"
	// OwnerNone marks an object no workflow revokes (out of scope).
	OwnerNone = "none"
)

const (
	// maxSourceItems bounds one source's rows. A source with more is recorded
	// with an unknown, enumeration-truncated item so the ledger never claims a
	// complete census it did not take.
	maxSourceItems = 2000
	// maxScanPages bounds how many pages a filtered scan reads.
	maxScanPages = 20
	sourcePage   = 500
)

// incidentCandidate is one row the enumeration found.
type incidentCandidate struct {
	owner, kind, id, scope string
	outcome                domain.ItemOutcome
	reason                 domain.ItemReason
}

// tenantRead is one existing tenant reader to run after the proofless step.
type tenantRead struct {
	source string
	scope  domain.Scope
	// adapterID narrows an adapter incident to its one adapter.
	adapterID string
	// verify makes a missing object an answer of not found.
	verify bool
}

const (
	srcAdapters = "adapters"
	srcDynamic  = "dynamic"
	srcSCIM     = "scim"
)

type collector struct {
	cands []incidentCandidate
	seen  map[string]bool
	reads []tenantRead
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
	c.unknown(owner, "enumeration", id, scope, domain.ReasonEnumerationTruncated)
}

// outOfScope lists an object no workflow revokes, with the reason.
func (c *collector) outOfScope(kind string, id string, scope domain.Scope, reason domain.ItemReason) {
	c.put(incidentCandidate{owner: OwnerNone, kind: kind, id: id, scope: renderScope(scope), outcome: domain.ItemOutOfScope, reason: reason})
}

// blockedSource records a whole source the caller may not read.
func (c *collector) blockedSource(owner, kind, id string, scope domain.Scope) {
	c.put(incidentCandidate{owner: owner, kind: kind, id: id, scope: renderScope(scope), outcome: domain.ItemBlocked, reason: domain.ReasonNeedsAnotherOperator})
}

// ownerHolds reports whether the caller holds an owner's capability, without
// recording an operation or a denial.
func ownerHolds(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, op authz.Operation, scope domain.Scope) (bool, error) {
	if scope == (domain.Scope{}) {
		return az.HoldsInstanceCapability(ctx, caller, op)
	}
	return az.CallerHolds(ctx, caller, op, scope)
}

// grantRevokeHolds picks the revoke operation for the depth a grant sits at.
func grantRevokeHolds(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, scope domain.Scope) (bool, error) {
	switch {
	case scope.Org == "":
		return ownerHolds(ctx, az, caller, authz.OpGrantRevokeInstance, domain.Scope{})
	case scope.Project == "":
		return ownerHolds(ctx, az, caller, authz.OpGrantRevokeOrg, scope)
	case scope.Env == "":
		return ownerHolds(ctx, az, caller, authz.OpGrantRevokeProject, scope)
	}
	return ownerHolds(ctx, az, caller, authz.OpGrantRevokeEnv, scope)
}

// pageDeveloperCredentials scans one org's live delegations in id order. keep
// narrows them; it reports whether the scan stopped early.
func pageDeveloperCredentials(ctx context.Context, az *authz.TxAuthorizer, org domain.OrgID, authority domain.PrincipalID, now time.Time,
	keep func(authz.DeveloperCredential) bool, add func(authz.DeveloperCredential) error) (bool, error) {
	after, kept := "", 0
	for page := 0; page < maxScanPages; page++ {
		rows, err := az.LiveDeveloperCredentialsInOrg(ctx, org, authority, after, sourcePage, now)
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
		if len(rows) < sourcePage {
			return false, nil
		}
		after = rows[len(rows)-1].ID
	}
	return true, nil
}

// collect runs every proofless reader the target's scope names, inside the
// authorizing transaction, and records which tenant readers still have to run.
func (s *Incidents) collect(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, a incidentAccess, now time.Time) (*collector, error) {
	c := &collector{seen: map[string]bool{}}
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

	grants, err := az.GrantRowsForPrincipal(ctx, target)
	if err != nil {
		return err
	}
	for i, g := range grants {
		if i >= maxSourceItems {
			c.truncated(OwnerGrantRevoke, a.scopeID, instance)
			break
		}
		ok, err := grantRevokeHolds(ctx, az, a.caller, g.Grant.Scope)
		if err != nil {
			return err
		}
		c.add(OwnerGrantRevoke, "grant", g.ID, g.Grant.Scope, ok)
	}

	sessions, err := az.SessionsForPrincipal(ctx, target)
	if err != nil {
		return err
	}
	for i, ss := range sessions {
		if i >= maxSourceItems {
			c.truncated(OwnerSessionRevoke, a.scopeID, instance)
			break
		}
		c.add(OwnerSessionRevoke, "session", ss.ID, instance, true)
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
			c.truncated(OwnerDeveloperRevoke, string(org), domain.Scope{Org: org})
		}
	}

	identities, err := az.ExternalIdentitiesForAccount(ctx, a.account.ID)
	if err != nil {
		return err
	}
	for i, x := range identities {
		if i >= maxSourceItems {
			c.truncated(OwnerExternalIdentity, a.scopeID, instance)
			break
		}
		c.add(OwnerExternalIdentity, "external_identity", x.ID, instance, true)
	}

	// The created-by relation of the offboarding report: machine principals the
	// account created are listed, not revoked unless selected.
	after, seen := "", 0
	for {
		rows, err := az.ServiceAccountsCreatedBy(ctx, target, a.org, after, sourcePage)
		if err != nil {
			return err
		}
		for _, sa := range rows {
			if seen >= maxSourceItems {
				c.truncated(OwnerServiceAccountReview, a.scopeID, instance)
				rows = nil
				break
			}
			seen++
			scope := domain.Scope{Org: sa.Org, Project: sa.Project}
			ok, err := ownerHolds(ctx, az, a.caller, authz.OpServiceAccountDelete, scope)
			if err != nil {
				return err
			}
			c.add(OwnerServiceAccountReview, "service_account", sa.ID, scope, ok)
		}
		if len(rows) < sourcePage {
			break
		}
		after = rows[len(rows)-1].ID
	}

	for _, o := range []struct {
		kind   string
		reason domain.ItemReason
	}{
		{"delivered_plaintext", domain.ReasonDeliveredPlaintext},
		{"cache", domain.ReasonLocalCopy},
		{"running_process", domain.ReasonRunningProcess},
		{"external_session", domain.ReasonExternalSession},
	} {
		c.outOfScope(o.kind, a.scopeID, instance, o.reason)
	}
	return nil
}

func (s *Incidents) addDeveloperCredential(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, c *collector, d authz.DeveloperCredential) error {
	ok := true
	if d.Scope.Project != "" {
		var err error
		ok, err = ownerHolds(ctx, az, caller, authz.OpDeveloperCredentialRevokeProject, domain.Scope{Org: d.Scope.Org, Project: d.Scope.Project})
		if err != nil {
			return err
		}
	}
	c.add(OwnerDeveloperRevoke, "developer_credential", d.ID, d.Scope, ok)
	return nil
}

func (s *Incidents) collectOrg(ctx context.Context, az *authz.TxAuthorizer, a incidentAccess, now time.Time, c *collector) error {
	scope := a.scope

	// The accounts selected from the org's footprint. Machine principals hold
	// grants too; only human accounts are listed.
	lines, err := az.GrantLinesInOrg(ctx, string(a.org))
	if err != nil {
		return err
	}
	members, listed := map[domain.PrincipalID]bool{}, 0
	for _, l := range lines {
		if members[l.Principal] {
			continue
		}
		members[l.Principal] = true
		account, err := az.AccountByPrincipal(ctx, l.Principal)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if listed >= maxSourceItems {
			c.truncated(OwnerAccountContain, a.scopeID, scope)
			break
		}
		listed++
		c.add(OwnerAccountContain, "account", account.ID, scope, true)
	}

	stopped, err := pageDeveloperCredentials(ctx, az, a.org, "", now,
		func(authz.DeveloperCredential) bool { return true },
		func(d authz.DeveloperCredential) error { return s.addDeveloperCredential(ctx, az, a.caller, c, d) })
	if err != nil {
		return err
	}
	if stopped {
		c.truncated(OwnerDeveloperRevoke, a.scopeID, scope)
	}

	invitations, err := az.ListLiveOrgEmailInvitations(ctx, a.org, now)
	if err != nil {
		return err
	}
	if len(invitations) > 0 {
		ok, err := ownerHolds(ctx, az, a.caller, authz.OpEmailInvitationRevoke, scope)
		if err != nil {
			return err
		}
		for i, inv := range invitations {
			if i >= maxSourceItems {
				c.truncated(OwnerEmailInvitation, a.scopeID, scope)
				break
			}
			c.add(OwnerEmailInvitation, "email_invitation", inv.ID, scope, ok)
		}
	}

	ok, err := ownerHolds(ctx, az, a.caller, authz.OpSCIMBindingList, scope)
	if err != nil {
		return err
	}
	if ok {
		c.reads = append(c.reads, tenantRead{source: srcSCIM, scope: scope})
	} else {
		c.blockedSource(OwnerSCIMCredential, "org", a.scopeID, scope)
	}

	for _, o := range []struct {
		kind   string
		reason domain.ItemReason
	}{
		{"delivered_plaintext", domain.ReasonDeliveredPlaintext},
		{"cache", domain.ReasonLocalCopy},
		{"running_process", domain.ReasonRunningProcess},
		{"issued_certificate", domain.ReasonIssuedCertificateUsed},
	} {
		c.outOfScope(o.kind, a.scopeID, scope, o.reason)
	}
	return nil
}

func (s *Incidents) collectProject(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, a incidentAccess, now time.Time, c *collector) error {
	scope := a.scope

	// Service accounts and their live credentials.
	accounts, err := az.ServiceAccountsIn(ctx, scope)
	if err != nil {
		return err
	}
	epoch, err := az.CredentialEpoch(ctx)
	if err != nil {
		return err
	}
	ok, err := ownerHolds(ctx, az, a.caller, authz.OpCredentialRevoke, scope)
	if err != nil {
		return err
	}
	listed := 0
scan:
	for _, sa := range accounts {
		creds, err := az.MachineCredentialsFor(ctx, sa.ID)
		if err != nil {
			return err
		}
		for _, cred := range creds {
			if !cred.Live(now, epoch) {
				continue
			}
			if listed >= maxSourceItems {
				c.truncated(OwnerMachineCredential, a.scopeID, scope)
				break scan
			}
			listed++
			c.add(OwnerMachineCredential, "machine_credential", cred.ID, scope, ok)
		}
	}

	stopped, err := pageDeveloperCredentials(ctx, az, a.org, "", now,
		func(d authz.DeveloperCredential) bool { return d.Scope.Project == scope.Project },
		func(d authz.DeveloperCredential) error { return s.addDeveloperCredential(ctx, az, a.caller, c, d) })
	if err != nil {
		return err
	}
	if stopped {
		c.truncated(OwnerDeveloperRevoke, a.scopeID, scope)
	}

	// Non-terminal leases, through the new bounded project listing.
	proof, err := az.Authorize(ctx, a.caller, authz.OpIncidentLeasesProject, scope)
	if err != nil {
		return err
	}
	if err := pageProjectLeases(ctx, r, az, proof, a, c); err != nil {
		return err
	}

	adapters, err := ownerHolds(ctx, az, a.caller, authz.OpAdapterInspect, scope)
	if err != nil {
		return err
	}
	if adapters {
		c.reads = append(c.reads, tenantRead{source: srcAdapters, scope: scope})
	} else {
		c.blockedSource(OwnerAdapterTargetPause, "project", a.scopeID, scope)
	}
	providers, err := ownerHolds(ctx, az, a.caller, authz.OpDynamicProviderInspect, scope)
	if err != nil {
		return err
	}
	if providers {
		c.reads = append(c.reads, tenantRead{source: srcDynamic, scope: scope})
	} else {
		c.blockedSource(OwnerDynamicCredential, "project", a.scopeID, scope)
	}

	for _, o := range []struct {
		kind   string
		reason domain.ItemReason
	}{
		{"delivered_plaintext", domain.ReasonDeliveredPlaintext},
		{"cache", domain.ReasonLocalCopy},
		{"running_process", domain.ReasonRunningProcess},
		{"issued_certificate", domain.ReasonIssuedCertificateUsed},
	} {
		c.outOfScope(o.kind, a.scopeID, scope, o.reason)
	}
	return nil
}

// pageProjectLeases lists a project's non-terminal leases a page at a time
// through the new bounded listing.
func pageProjectLeases(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, proof authz.Proof, a incidentAccess, c *collector) error {
	after, seen := "", 0
	envHolds := map[domain.EnvID]bool{}
	for {
		rows, err := r.Dynamic().ListNonTerminalLeasesInProject(ctx, proof, after, store.MaxDynamicLeasePage)
		if err != nil {
			return err
		}
		for _, l := range rows {
			if seen >= maxSourceItems {
				c.truncated(OwnerLeaseRevoke, a.scopeID, a.scope)
				return nil
			}
			seen++
			scope := domain.Scope{Org: domain.OrgID(l.OrgID), Project: domain.ProjectID(l.ProjectID), Env: domain.EnvID(l.EnvironmentID)}
			ok, known := envHolds[scope.Env]
			if !known {
				var err error
				if ok, err = ownerHolds(ctx, az, a.caller, authz.OpLeaseRevoke, scope); err != nil {
					return err
				}
				envHolds[scope.Env] = ok
			}
			c.add(OwnerLeaseRevoke, "lease", l.ID, scope, ok)
		}
		if len(rows) < store.MaxDynamicLeasePage {
			return nil
		}
		after = rows[len(rows)-1].ID
	}
}

func (s *Incidents) collectAdapter(ctx context.Context, az *authz.TxAuthorizer, a incidentAccess, c *collector) error {
	c.reads = append(c.reads, tenantRead{source: srcAdapters, scope: a.scope, adapterID: a.adapterID, verify: true})
	for _, o := range []struct {
		kind   string
		reason domain.ItemReason
	}{
		{"delivered_plaintext", domain.ReasonDeliveredPlaintext},
		{"cache", domain.ReasonLocalCopy},
		{"running_process", domain.ReasonRunningProcess},
	} {
		c.outOfScope(o.kind, a.scopeID, a.scope, o.reason)
	}
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
			return domain.ErrNotFound
		}
		c.blockedSource(OwnerAdapterTargetPause, "project", string(r.scope.Project), r.scope)
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
			c.add(OwnerAdapterCredential, "adapter", id, r.scope, true)
		}
		// The upstream PAT lives at the provider: Hikyo can only be told it was
		// revoked there.
		c.unknown(OwnerAdapterUpstream, "adapter", id, r.scope, domain.ReasonUpstreamNotObservable)
		for i, t := range v.Targets {
			if i >= maxSourceItems {
				c.truncated(OwnerAdapterTargetPause, id, r.scope)
				break
			}
			c.add(OwnerAdapterTargetPause, "adapter_target", t.ID, r.scope, true)
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
		c.blockedSource(OwnerDynamicCredential, "project", string(r.scope.Project), r.scope)
		return nil
	}
	if err != nil {
		return err
	}
	for i, v := range views {
		if i >= maxSourceItems {
			c.truncated(OwnerDynamicCredential, string(r.scope.Project), r.scope)
			break
		}
		c.add(OwnerDynamicCredential, "dynamic_provider", v.ID, r.scope, true)
		c.unknown(OwnerDynamicUpstream, "dynamic_provider", v.ID, r.scope, domain.ReasonUpstreamNotObservable)
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
		c.blockedSource(OwnerSCIMCredential, "org", string(org), r.scope)
		return nil
	}
	if err != nil {
		return err
	}
	listed := 0
	for _, b := range bindings {
		creds, err := s.SCIM.ListCredentials(ctx, actor, org, b.ID)
		if refusedRead(err) {
			c.blockedSource(OwnerSCIMCredential, "scim_binding", b.ID, r.scope)
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
				c.truncated(OwnerSCIMCredential, string(org), r.scope)
				return nil
			}
			listed++
			c.add(OwnerSCIMCredential, "scim_credential", cred.ID, r.scope, true)
		}
	}
	return nil
}

var errIncidentUnwired = errors.New("service: incident enumeration is not wired to its tenant readers")
