# Scoped incident containment and credential recovery (ADR, accepted 2026-11-12)

> **Status: accepted by the owner on 2026-11-12 as recommended, options O1 to O8 as listed in the [Decision record](#decision-record). Operative per scope only when the implementation pull requests merge.**
> Accepting this ADR selects the options. It adds no command, capability or schema today,
> and it authorizes no implementation, release or production change. The amendments it
> declares take effect with the implementation, as the [oss-mechanics ADR](./oss-mechanics.md)
> requires. Independent Spec review ran on the proposal. Cross-provider review did not run
> and is not CLEAN. Maintainer security review and the human merge gate remain required.
> The evidence is in the [verified inventory](../design/incident-containment.md), and the
> interim procedure is the [draft runbook](../operations/incident-containment.md).

Goal, from the issue: an authorized operator can deliberately contain affected
authority, enumerate unresolved cleanup, replace compromised credentials and follow a
complete guide, without mistaking a session reset or a local secret removal for full
containment. Finding F5 of [#1032](https://github.com/Hikyo-Org/Hikyo/issues/1032).

## Context

Containment is spread across owners that each do one thing:

- Reset issuance ends sessions and developer delegations but leaves the old password and
  every other sign-in method working until establishment. Even then, factors and linked
  identities are not touched.
- Ordinary logout leaves developer delegations active, by an earlier locked decision.
- Adapter and dynamic-provider credential removal clear local custody and revoke nothing
  upstream. Clearing the dynamic provider's admin credential also strands pending lease
  drops.
- Lease revocation is asynchronous, can end `unknown`, and never ends sessions already
  connected to the database.
- There is no quarantine and no lockdown. The only account-level denial is privacy
  restriction, which is a different feature (host-local, external receipts, an erasure
  branch).

[#1085](https://github.com/Hikyo-Org/Hikyo/pull/1085) added a tested, per-principal manual
sequence through those owners. It works by removing every grant and rule first and then
resetting. It has no incident record, no enumeration and no resumable state, and removing
grants is destructive: the operator must remember what to restore. The
[inventory](../design/incident-containment.md#2-verified-inventory) re-verifies each claim at
`dev` `426bf59ce`.

The issue asks the owner to decide, before any incident state exists: scope, operator
capability and reauthentication, last-manager and break-glass behavior, irreversible
revocations, recovery and release semantics, concurrency, and whether the first workflow is
an audited orchestration of existing owners or a new dedicated quarantine state.

## Constraints that are not options

These come from locked ADRs and the issue. Every option below respects them.

- Do not reuse privacy restriction, erasure or release, or their receipts.
- Do not change ordinary logout. A new containment path may revoke delegations; logout
  stays as the developer-credentials ADR locked it.
- No automatic global shutdown. Detection (F3, [#1038](https://github.com/Hikyo-Org/Hikyo/issues/1038))
  may throttle or narrowly contain, and an attacker must not be able to trigger an
  outage by tripping a rule.
- [#148](https://github.com/Hikyo-Org/Hikyo/issues/148) owns scheduled provider rotation,
  overlap and safe revocation. This ADR adds no scheduler.
- Authorization stays at the single chokepoint, evaluated inside the operation's own
  transaction, with no cache ([permission-model ADR](./permission-model.md)).
- Break-glass remains host-local with no network route. Instance-capability holders keep
  no network reset path ([human-auth ADR](./human-auth.md)).
- Hikyo does not claim to recall delivered plaintext, caches or live consumers, and does
  not claim an upstream revocation it did not observe.
- Host or database compromise is contained outside Hikyo (isolation) and recovered on a
  clean installation. An in-product state in a datastore the attacker can write is not
  containment evidence ([threat-model ADR](./threat-model.md)).

## Vocabulary

- **Deny**: stop an authority from acting now, in the same transaction, reversibly.
- **Revoke**: end a credential or delegation permanently. A revoked row is never revived.
- **Enumerate**: list the objects a scope could reach, including ones nobody has handled.
- **Track**: keep a durable intent and outcome for work that finishes later or may be
  uncertain.
- **Pending**: tracked, not yet verified. `unknown` is pending.
- **Attested**: an operator states the outcome, because Hikyo cannot observe it. Never
  reported as verified.

## Decisions for the owner

Each decision lists the options, what each costs, and the recommendation the owner
accepted. The accepted options are consistent with each other and form the design
described afterwards.

### O1. The mechanism for the first supported workflow

- **A. Incident record and orchestration of existing owners only.** No new authority
  state. Account containment stays "remove every grant and rule, then reset". Cost: the
  destructive sweep remains, the interval between sweep and reset still lets the old
  password and any allowed IdP or factor create a session (with no authority), and release
  is a manual regrant from the operator's notes. SCIM or any policy writer can regrant
  mid-incident. Lowest risk to the authorization core.
- **B. A, plus a reversible `contained` state on human principals.** The state is a new
  column beside `privacy_state`, enforced at the same predicate sites (14 query references per engine
  outside the privacy owner's own two, covering session resolution, account lookup,
  generation lookup, grant and rule loading, and the manage-members census). A contained principal cannot sign in with any method and
  its grants stop loading, but nothing is deleted. Cost: a migration for both engines, an
  edit to every predicate site except the one recovery lookup, a lint that every new site includes it, and a new way to
  lock a person out (see O3). Closes the old-password, allowed-IdP and stolen-factor window
  without destroying grants. The state denies sign-in, session resolution, grant and rule
  loading and the census. The shared account lookups (by name, id and principal) also serve
  sign-in, MFA and SSO, so they stay gated. Reset and establishment need a separate,
  ungated lookup, callable only from those two flows and enforced by a lint that nothing
  else uses it, because they authenticate with a reset authority and carry no grants.
  Without it the flow in O4 could not run. Factor enrolment needs a session, so it happens
  after release, on first sign-in, under the normal gates.
- **C. B generalized to org, project and instance states.** Cost: an in-product global
  denial an attacker can aim at, and a second place for authority to disagree. Contradicts
  the "no automatic global shutdown" boundary in spirit, and gains little because
  org and project scopes are fan-outs over principals and credentials, not authorities of
  their own.

**Recommendation: B.** Org, project and adapter scopes are selectors over existing owners
and the ledger (O5). Only the account needs a state.

### O2. Who may act, and with what assurance

- **A. New `incident-contain` capability** at org and instance scope.
- **B. No new atom.** Account containment and release use `credential-reset` with the
  same org-bounded test and the same instance-capability exclusion as reset. Opening an
  incident record needs `credential-reset` at org or instance scope, since the human-auth
  ADR keeps that capability off project scope. An incident on a project or an adapter is
  opened by an org-scope holder for the owning org; a project owner without it acts through
  the individual owners, as today. Reset's other refusals do not all carry over:
  containment sets no password, so an account with a linked identity is containable over the
  network, and its release waits on an attested outcome for the identity (O4). A target with
  grants in several orgs or none needs instance scope, as for reset. Every fan-out item needs its
  existing owner's capability (`manage-identities`, `manage-adapters`, `manage-members`,
  `scim-provision`, instance-config, and so on). An item the caller is not authorized for
  is recorded `blocked: needs another operator` and is never performed on the caller's
  behalf.
- **C. Host-local only.** Contain and release exist only as a break-glass command.

All variants require the existing MFA-mandatory session assurance, the operation-bound
fresh reauthentication, and an explicit confirmation of the enumerated list. `--yes`
confirms; it grants neither capability nor proof. Instance-capability targets are
host-local in every variant, as for reset.

**Recommendation: B.** It adds no atom and widens no authority: a holder who can already
reset a password could already end that person's sessions. A (separation of duty from the
help-desk reset role) is a reasonable alternative if the owner wants the two roles split.
C is the safest and the least usable, and makes a network operator unable to act during
an incident.

### O3. Last manager and self-lockout

- **A. Refuse.** Containment is refused when it would leave any org, or the instance,
  without an uncontained `manage-members` holder, and an instance-capability principal is
  containable only while another instance-capability holder stays uncontained with a working
  local credential (the human-auth floor is a credential-retention rule, not a ban on
  containing such an account; the host-local verb for it would be new, specified in the
  implementation issue). It uses the same census as privacy
  restriction, in which a contained or restricted principal is not a holder. The working-credential check for instance-capability holders is new and is not part of
  that shared census, which does not test whether a holder can authenticate. A batch is
  checked after every principal in it is locked, in sorted order. No override. The
  existing host-local break-glass grant is the path to a clean manager first.
- **B. Refuse, with a host-local override** that records an `unadministered` gap.
- **C. Allow** and rely on break-glass recovery afterward.

**Recommendation: A**, consistent with the permission-model lockout invariant and the
human-auth statement that permanent lockout is refused. An override is an unaudited
escape hatch for the one situation (all managers compromised) where the operator should
already be on the host.

### O4. Release and recovery semantics

- **A. Release is a separate audited act and never revives anything.** It returns the
  principal to `none` only after a credential establishment has completed since the
  containment was recorded or, for an account with no local password, after the ledger item
  for its linked identities is attested. An account with both a linked identity and a local
  password needs the host-local reset (network reset refuses linked accounts) and the
  identity attestation. It does not reactivate revoked delegations or credentials, and
  it does not regrant. The operator then regrants the independently approved minimum.
- **B. Release immediately re-enables sign-in** with whatever credentials exist.
- **C. No release.** Re-admission is by removing the principal and recreating it.

**Recommendation: A.** B reopens exactly the window that the state exists to close. C
destroys history and personal values for a person who may be innocent.

### O5. Durability and enumeration

- **A. Computed inventory only.** A read that lists affected objects at request time,
  stored nowhere. Cannot show what was already handled, and cannot resume.
- **B. A durable incident ledger.** One incident row and one row per affected object, each
  with an outcome from a closed set (`pending`, `done`, `unknown`, `attested`,
  `blocked`, `out_of_scope`). It is mutable state, kept apart from the append-only audit
  tables as the refusal-summary staging row is. Every transition is also an append-only
  instance-trail event. Resumption re-reads current state and re-runs only open items, each
  idempotently, through the owner that performs it.
- **C. B plus an external receipt** for each containment, applied again after restore as
  privacy receipts are.

**Recommendation: B.** Restore already treats restored authentication state as inert and
requires per-principal reconciliation under local authority, which covers a restore from an
archive that predates a containment. A receipt (C) would add a second mechanism for a case
the restore gate already catches. If the ledger lives in a datastore the attacker could
write (host or database compromise), it is evidence to investigate and never proof, and
the runbook keeps an external journal for that case.

### O6. How far Hikyo goes upstream

- **A. Local outcomes plus attestation.** Hikyo reports what it changed and what it can
  observe (for a PostgreSQL lease, the role's existence through the provider's own status
  probe). Anything else is `attested` by a named operator with a time, and shown that way.
  Hikyo never prints "revoked upstream" for a PAT or a key it did not check.
- **B. Hikyo calls providers to revoke.** Not available as a general capability: only
  dynamic roles have a provider-side drop, and adapter PATs and cloud keys have no common
  revocation call. It would also overlap #148.

**Recommendation: A.** Add the replace-before-clear rule for custody Hikyo itself needs to
finish cleanup (a provider admin credential while any lease is non-terminal, an adapter
PAT while scrub work is pending): the workflow refuses to clear it and says why, and an
explicit abandon records the loss of cleanup ability as a pending-unrecoverable item.

### O7. What instance scope means

- **A. No in-product lockdown.** Instance scope is an enumeration over every org plus the
  external fence and the host-local reset. The ledger spans all orgs, and org-scope
  containments run under it.
- **B. A host-local serving gate** (`hikyo admin` command) that makes the server refuse
  all non-break-glass requests until lifted.
- **C. A credential-epoch bump** that makes every password verifier inert until it is
  re-established, as restore does.

**Recommendation: A.** The parent issue directs infrastructure isolation for host
compromise and reviewed escalation for any whole-instance shutdown. B duplicates what a
proxy or firewall does, with weaker guarantees. C is a mass reset with a large blast radius.
It is worth keeping in mind for a verified database-verifier theft, which is a rotation
decision for the owner, not a containment default.

### O8. Personal values during a long containment

Personal values are deleted 14 days after their owner loses read, reveal or qualification
(`PersonalRetentionWindow`, `internal/service/personal.go:696`, swept at `:707`). Both the
existing manual sweep and a `contained` state would start that clock.

- **A. Pause the clock while contained.** The retention predicate treats a contained owner
  as still holding. Cost: a small change to the personal-values owner, and values of a
  possibly compromised account are retained.
- **B. Accept it** and require the operator to acknowledge the 14-day limit when
  containing.

**Recommendation: A.** Containment must not destroy a person's data as a side effect. The
interim runbook warns about the clock for the manual sweep.

## Design under the accepted options

### Scopes and what each does at once

The matrix is by effect. "Immediate" means committed in the operation's transaction (or
the owner's) and enforced from the next request. "Pending" means tracked in the ledger and
not yet verified. Detail by credential class, with the owner and evidence per row, is in the
[design document](../design/incident-containment.md#4-authority-and-consumer-map).

| Scope | Immediate | Pending or operator-owned | Who |
| --- | --- | --- | --- |
| **Account** (one human) | `contained` state: no sign-in by any method, no loaded grants; sessions ended; generation advanced; developer delegations revoked; outstanding establishment authorities retired | IdP sessions and linked-identity removal; factors; machine credentials the account created (listed, not revoked unless selected); delivered plaintext; running consumers | `credential-reset` at org scope for a single-org target, instance scope otherwise; host-local for instance-capability targets |
| **Organisation** | The accounts selected from the enumerated footprint (each as above); org SCIM binding credentials revoked | Everything in the account row, per account; provisioners; consumers. Remote credentials and invitations are instance-scoped, so they appear only under Instance | Each item under its owner's capability |
| **Project** | Selected service-account credentials and federation bindings revoked; machine-reveal opt-in off; project developer delegations revoked; adapter targets paused | Adapter and provider upstream secrets; leases; project DEK rotation and re-encryption; consumers | Org-scope `credential-reset` opens the incident; project owners' capabilities for the items |
| **Adapter** (or dynamic provider) | Targets paused, credential jobs retired; local credential cleared only after the replace-before-clear checks | Upstream PAT revocation (attested); remote name scrub; leases to a verified terminal state; existing database sessions | `manage-adapters` / dynamic-provider owner |
| **Instance** | Nothing automatic in product. Fan-out across every org under one ledger; selected remote credentials and invitations revoked by instance-scope holders | The external fence; host-local resets; root, master and DEK rotation order; recovery on a clean installation | Instance-scope holders and the host operator |

Irreversible by design: session and delegation revocation, machine, SCIM, remote and
adapter credential revocation, and local credential clearing. Reversible: the `contained`
state (release, per O4), adapter target pause, machine-reveal opt-in, and environment
protection. The confirmation lists which of its effects are irreversible.

### Enumerating unresolved cleanup

Opening an incident takes a scope (an account, an org, a project or an adapter) and returns
one list, then writes it. Sources use existing readers: the grant, rule and footprint
census; sessions; developer credentials; service accounts and their credentials and
bindings; SCIM bindings and credentials; remotes, remote credentials and invitations;
login providers and linked identities; adapters, targets and pending moves or scrubs;
dynamic providers and every non-terminal lease; and the created-by relation used by the
offboarding report. Two listings do not exist today and are needed: non-terminal leases at
project and instance level (today only per environment), and developer delegations of other
principals at org level. Objects with no revoke owner (delivered plaintext, caches, running
processes, external sessions, issued certificates already in use) are listed as
`out_of_scope` with the reason, so the operator sees what the workflow does not cover.

### Credential replacement

Replacement stays with the existing owners, and the ledger orders and records it.

- **Hikyo-issued machine credentials**: mint the replacement, deliver it through a private
  file, record consumer adoption as `attested`, then revoke the predecessor. Under
  confirmed compromise the order flips (revoke first, accept the outage).
- **Humans**: containment, then reset and establishment, then release (O4), then
  re-enrolment of factors on first sign-in. Factors, passkeys and links that existed before
  survive until their owner removes them, and there is no administrator removal today, so
  a stale or stolen factor is a ledger item the operator must resolve (self-service after
  release, or new work if the owner wants an administrator path). A compromised IdP or factor is replaced at its owner before release.
- **Hikyo-held upstream secrets** (adapter PATs, provider admin credentials): replace
  upstream (`attested`), set the new value, verify with the owner's connection test where one exists (the adapter target test; the
  dynamic provider has none, so that check is attested), then
  clear the old. Replace-before-clear applies (O6).
- **Encryption custody**: the existing order, never reordered: enrolled mounted root
  candidate prepare, verify and finalize; master; instance DEK and re-encryption; each
  affected project DEK and re-encryption. Change-token and scanning-key rotation are
  recorded as separate items and never described as bearer revocation.

### Concurrency and fencing

- Account state changes lock the target principal row, the same lock that grant writers,
  reset and privacy take. A concurrent grant or reset conflicts with an in-flight
  containment and one of them retries or fails closed.
- Authorization reads the state inside each request's own transaction. A request whose
  transaction begins after the commit sees the principal contained. A request already
  authorized before it may have delivered plaintext, and the ledger lists that as
  `out_of_scope` rather than implying it was fenced.
- A multi-principal batch locks principals in sorted order and checks the last-manager
  census once, after all locks are held.
- A ledger resume is idempotent per item. A crash between an owner commit and the ledger
  update leaves the item `pending` or `unknown`, never `done`. The next run reads the
  owner's current state before acting.
- Multi-node and multi-writer fencing is the operator's external fence. No in-process
  guarantee covers a peer, a provisioner or a policy writer outside Hikyo.

### Audit trail

- New instance-trail event types, registered in the closed catalogue, for incident opened,
  item intent, item outcome, principal contained, principal released and incident closed.
  Intent and outcome pairs share a correlation id, as adapter intent and outcome do. The
  incident id is the correlation id, so the existing owners' revocation events
  (`grant.revoked`, `developer_credential.revoked`, session and credential events) need no
  payload change and join to the incident by correlation.
- Actor, scope, a closed reason vocabulary, counts and object ids only. No bearer, secret,
  free-text narrative or attacker-supplied string is stored. Events from host-local
  actions carry the break-glass actor class, as privacy and reset do.
- Owner events keep their own trail and retention. A containment never writes a privacy
  event or receipt.
- Operator-attested outcomes record who attested, when, and the closed outcome, not the
  evidence itself.

### Interaction with the privacy-restriction and last-manager safeguards

- Distinct state, column, audit types and release. Containment never calls
  `ApplyPrivacySubject` and privacy never reads the ledger.
- Effective authority needs both states active. Privacy release does not lift
  containment, and containment release does not lift a privacy restriction. Privacy
  erasure of a contained account wins; the ledger keeps pseudonymous references only.
- One census. A contained or restricted principal is not a `manage-members` holder, so
  containing one manager reduces the count the other safeguard sees, and the second
  refusal is correct.
- A principal whose only `manage-members` authority is a SCIM lockout-retention origin is
  the last holder, so containing it is refused until another holder exists. Adding that
  holder also cures the retention origin ([scim-provisioning ADR](./scim-provisioning.md) 2.4).
- Containment has no erasure branch, deletes no personal data (O8) and writes no
  external receipt (O5).

### Failure, interruption and partial completion

An item the owner has not confirmed is `pending` or `unknown`. A lost response, a restart,
a missing audit event or an unreachable provider never produces `done`. Resume cannot
grant broader authority than the first run (every item re-checks the caller's capability)
and cannot restore a revoked credential. A failed upstream cleanup stays visible on the
incident until the operator closes it, and closing with open items requires an explicit
`closed with residuals` outcome that lists them.

## Declared amendments

Per the [oss-mechanics ADR](./oss-mechanics.md), each is recorded as a declared-amendment
banner on the amended ADR, operative when the implementation pull request merges.

- [human-auth](./human-auth.md): a contained account cannot sign in, which is a
  narrower form of the privacy restriction already accepted. The "local accounts can never
  be fully disabled" floor is read as a credential-retention rule for instance-capability
  holders (O3). The `credential-reset` capability would also authorize containment and
  release, including containing a linked-identity account over the network, which reset
  itself refuses, and an org-scope holder opening a project or adapter incident for the
  owning org (O2).
- [permission-model](./permission-model.md): the capability table is unchanged but
  `credential-reset` gains the meanings above; the lockout census excludes contained
  principals; the chokepoint reads the state in the operation's transaction.
- [audit-model](./audit-model.md): the new event types.
- [developer-credentials](./developer-credentials.md): containment revokes delegations
  like reset does. Ordinary logout is unchanged.
- [machine-identities](./machine-identities.md), [scim-provisioning](./scim-provisioning.md)
  and [remote-instance-pairing](./remote-instance-pairing.md): no change to their owners;
  they gain a correlated invocation from the ledger.

## Not decided here

- Alert-driven or automatic containment. F3 ([#1038](https://github.com/Hikyo-Org/Hikyo/issues/1038))
  owns detection and escalation and must not add a global trigger without a new decision.
- Scheduled rotation and overlap ([#148](https://github.com/Hikyo-Org/Hikyo/issues/148)).
- Independent evidence retention and export completeness
  ([#1035](https://github.com/Hikyo-Org/Hikyo/issues/1035),
  [#1036](https://github.com/Hikyo-Org/Hikyo/issues/1036),
  [#1037](https://github.com/Hikyo-Org/Hikyo/issues/1037)).
- Provider-specific upstream revocation calls.
- The API, CLI and web surface for the ledger. They follow the accepted options and the
  api-cli-surface ADR and are specified in the implementation issue.

## Follow-ups outside this proposal

Small fixes inside existing behavior. They are not folded into this docs change and each
needs its own reviewed PR.

1. **Persist delegation revocation in the other generation-advancing flows.** Credential
   establishment (`internal/service/auth.go:893`), self-service recovery issuance
   (`internal/service/factors.go:1176`) and the passkey clone sweep
   (`internal/service/webauthn.go:587`) advance the generation, which already denies
   delegation use, but do not persist `revoked_at` or the revocation event the way reset
   (`credential_reset.go:281`) and password change (`password.go:113`) do. Listings and
   incident reconciliation can show those delegations as unrevoked.
2. **Record pending leases when the provider admin credential is cleared.**
   `Dynamic.RevokeCredential` (`internal/service/dynamic.go:317`) could add the count of
   non-terminal leases to its audit event. An additive field, no behavior change. Refusing
   the clear is a behavior change and belongs to O6.
3. **List non-terminal leases above environment level**, as read-only listings under the
   existing `lease-inspect` formula.

## Decision record

The owner accepted every recommendation on 2026-11-12 ("as recommended").

| Decision | Accepted option |
| --- | --- |
| O1 mechanism | B: ledger plus a reversible `contained` principal state |
| O2 capability and assurance | B: reuse `credential-reset`, no new atom |
| O3 last manager and lockout | A: refuse, no override |
| O4 release | A: separate act that never revives anything |
| O5 durability | B: durable ledger |
| O6 upstream reach | A: local outcomes plus attestation, with replace-before-clear |
| O7 instance scope | A: no in-product lockdown |
| O8 personal values | A: pause the personal-values deletion clock |

The elaborations under O2 and O3 (containing a linked-identity account over the network,
an org-scope holder opening a project or adapter incident, the credential-retention
reading of the instance-capability floor, and the separate recovery lookup under O1) are
this ADR's reading of the accepted options. They are open to change in the implementation
review.

Acceptance of this ADR does not authorize an implementation, a release or a production
change. The issue's acceptance items that need running software (real session and
delegation failure tests, adversarial remint during containment, restart and concurrency
tests, an interface exercise on desktop, mobile and CLI, and an isolated recovery rehearsal)
remain to be delivered by the implementation.

## Review record

Independent Spec review ran in two rounds against issue #1039; its findings were
addressed in the text. Independent Standards review is recorded on the pull request.
Cross-provider review did not run and is not CLEAN. Maintainer security review and the
human merge gate remain required.
