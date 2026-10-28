# #1039: manual scoped containment and recovery

Owner: [#1039](https://github.com/Hikyo-Org/Hikyo/issues/1039). Source baseline:
`24e198b32a3ffd8d4013207b2f974d76ec49be6f` (2026-10-28). The branch was then
fast-forwarded to documentation-only main `970ac0db7c6024f91dedcefe34db7ca560b247e3`;
the validated runtime files were unchanged.

This is an operator sequence through existing owners. It adds no incident state,
automatic lockdown, new capability, privacy action, or recovery bypass. The
bounded source repair makes both credential-reset tiers durably revoke and audit
developer delegations in their existing transaction, in addition to generation
invalidation and session deletion. Normal logout retains its existing semantics.
Reset issuance and authority-mint events now name the acting network operator
or host break-glass actor, while keeping the affected account/principal in their
existing target fields. Previously those success events named the target as actor.

## Scope and completion contract

The supported unit is **one named human principal's authority across the
instance**, inside an externally isolated incident window. Repeat the sequence
for every affected principal. Record affected organizations, projects,
environments and downstream consumers before confirming it. A project-only
membership listing cannot establish principal-wide containment: ancestor grants,
other organizations, independent access rules and system-owned origins matter.

The claim is removal of the named principal's grant/rule-derived tenant and
instance capabilities and retirement of existing sessions/delegations. Freshly
authenticated self-service account/profile and credential ceremonies can remain
available; this is not account disablement or suppression of every authentication
ceremony. A still-enabled IdP or stolen factor may
authenticate that account again. With all effective grants and rules removed,
that new session cannot disclose data, regrant authority, or mint a developer
delegation. **Do not restore grants while a compromised password, factor, IdP,
provisioner, administrator or policy writer can still act.** If the requirement
is to prevent even account authentication, keep ingress isolated and replace or
disable those methods through their owners. There is no general account-lock
command in this workflow. Privacy restriction/erasure/release is not its owner.

Do not declare `CONTAINED` until every following condition has evidence:

1. An external fence admits only the clean operator path; other ingress, peer,
   provisioning and policy writers are stopped or fenced, and old requests are
   drained or terminated. All HA nodes and alternate listeners are included.
2. A complete inventory finds no remaining effective grant/origin or access
   rule for the named principal, including instance and inherited authority.
3. A final credential reset has committed after the policy sweep. Old human
   sessions and developer bearers fail. A fresh login using the old allowed
   method fails both disclosure and delegation mint authorization.
4. Each machine, federation, remote, SCIM, upstream and consumer residual below
   has its own outcome. Unverified residuals stay `HOLD`; containment is never
   reported beyond the enumerated scope.

There is no atomic multi-command incident transaction. The external fence is a
required control, not a new in-process guarantee. Each owner serializes its own
mutation and authorization; reset and grant writers lock the target principal.
Concurrent mint either precedes reset and is retired, or loses admission after
reset. Requests already authorized before a commit may have delivered plaintext.
Close/drain their connections and inventory that disclosure separately. A policy
writer allowed to regrant afterwards defeats this procedure; a retry loop cannot
repair an absent fence. On any uncertainty, keep isolation and report `HOLD`.

## Existing authority, assurance and recovery rules

| Action | Existing owner and required authority | Refusal / recovery consequence |
| --- | --- | --- |
| List/remove grants and rules | `manage-members` at the addressed scope, adequate human-session assurance; rule management must cover its entire selector | Project authority cannot remove an ancestor or outside selector. Revoke only the origins this owner controls. |
| Network reset | `credential-reset` at the permitted org/instance scope, MFA-mandatory session assurance | Org scope reaches only org-bounded targets. Instance-capability holders have no network reset path. Do not widen capability to avoid refusal. |
| Host reset | Existing `hikyo admin reset-credential`, approved local datastore/root authority, stopped server or the documented protected local operator path | Never expose it as a network route. This establishes a credential only; it does not log in, remove factors, or confer assurance. |
| Developer revoke | Owner self-revoke, or project `manage-identities` human session | Self `--all` covers the caller only. An operator inventories/revokes other principals' project credentials; never borrows their bearer. Reset additionally retires all target delegations. |
| Last manager | Existing grant lockout census and origin owners | Assign and verify a clean independent manager first. A last-manager refusal or SCIM lockout-retention row is unresolved authority, never success. |
| Break-glass grant | Existing host-local named principal/capability/scope grant | Recovery of clean administration only, durably audited. No privacy bypass, network fallback or automatic incident exception. |
| Key rotation / recovery | Existing instance operators, root-source enrollment, independently held archive/age/root custody | No new authority is granted by this runbook. Host/DB compromise requires external isolation and a clean host. |

Existing MFA requirements and operation-bound reauthentication still apply.
Revocation does not gain a new disclosure or fresh-proof requirement. Widening,
replacement, provider configuration, disclosure and key operations retain their
own authorization and proof formulas. `--yes` confirms an operation; it grants
neither capability nor proof. Before the first irreversible revocation the
operator confirms the exact instance identity, principal and scope, remaining
manager, cleanup credential custodian, and recovery point in the incident log.

These are compositions of the accepted [human-auth](../adr/human-auth.md),
[permission](../adr/permission-model.md),
[developer-credential](../adr/developer-credentials.md),
[machine-identity](../adr/machine-identities.md),
[encryption](../adr/encryption-model.md) and
[operations](../adr/ops-spec.md) contracts. A future reversible quarantine state,
different last-manager behavior or wider break-glass authority needs its own
decision. This handoff approves none of those changes.

## Residual and credential matrix

| Authority / custody | Required disposition | What the human reset does not prove |
| --- | --- | --- |
| Human browser, CLI, workspace and reauth windows | Final reset; verify rejected old artifacts and fresh authorization negatives | Issuance leaves the old password until establishment. Password replacement does not remove all local factors or upstream identities. |
| Developer delegations | Final reset and terminal revocation metadata/events; project inspection where needed | Logout and local file deletion are not revocation. A pre-final-reset login may mint another credential while authority remains. |
| Ordinary grants, templates, break-glass grants and member rules | Inventory all scopes and selectors, remove through their owners; re-list | A successful partial origin release can leave effective authority. Audit `grant.modified` is not `grant.revoked`. |
| SCIM bindings, credentials, mappings, group membership and lockout retention | Fence provisioners, disable/revoke affected binding authority and release origins through SCIM; cure retention with a clean manager | A manual grant revoke cannot remove SCIM/structural origins. SCIM access loss preserves unrelated-org sessions by design. A still-authorized provisioner can regrant. |
| Workload / automation service accounts, all overlap credentials and federation bindings | Enumerate every principal an attacker used or created; revoke all affected credentials AND bindings, or delete the affected service-account aggregate; verify next fetch refusal | Human reset does not revoke these independent principals. Revoking one overlap bearer leaves its siblings or federation live. |
| OIDC, OAuth2 and SAML login providers, linked identities, passkeys, TOTP, recovery codes and pending ceremonies | Remove/disable affected provenance and replace compromised custody through those owners before returning any grants; verify stale and fresh paths | A password reset is not upstream IdP logout or factor removal. Provider disable/re-enable must not revive delegations. |
| Inbound/outbound remotes, pairing invitations, exchanges and peer-side grants | Fence peer ingress/egress, revoke each side's credentials/bindings and pending pairing authority; reconcile both instances and their current incarnations | Local deletion and a human reset do not revoke the peer's independent authority. |
| Adapter PAT / outbound custody, pending moves and scrub jobs | Inventory provider and target routes; replace upstream secrets and local custody deliberately, preserve clean scrub authority until terminal outcomes | Local credential removal destroys local custody only. It neither revokes an upstream PAT nor proves remote deletion. |
| Dynamic provider admin credentials and leases | Preserve usable clean cleanup authority, request revocation, reconcile every lease/effect to verified terminal outcome | `revoking` and `unknown` are pending. Missing admin custody leaves retryable cleanup. PostgreSQL login disable/drop does not terminate existing database sessions. |
| Delivered plaintext, exports, downloaded archives, caches, process environments, CI variables, live consumers and external DB sessions | Replace upstream secret, adopt it in every consumer, invalidate old upstream credential and terminate old sessions under separately authorized provider procedures | Hikyo cannot recall delivered plaintext. Rotation or local custody deletion is not consumer adoption or external session termination. |
| Issued SSH/PKI certificates, CA/signing custody and consumer trust | Inventory issued credentials and active consumers; use the issuing owner and relying party's revocation/trust/session procedures where exposure applies | Human reset or envelope-key rotation does not revoke an issued certificate, remove a trust anchor or terminate an established SSH/TLS session. |
| Root/master/DEKs, change-token key, scanning key, backup age identities | Follow ordered rotation below and inventory old copies independently | Encryption-key changes and change-token/scanning-key rotation revoke neither Hikyo bearers nor provider credentials. Already-readable copied data remains readable. |

For an adapter disclosure, include active and pending destinations, credentials,
jobs and provider-side stored secrets in the residual list. Do not delete the
only cleanup credential first. `Adapters.RevokeCredential` clears local encrypted
custody and fences jobs, while adapter delete/move owners can retain custody
until their scrub barrier completes. Preserve that distinction.

For dynamic leases, `Dynamic.RunLeaseSweep` retries when admin custody is absent.
Record effect intent, outcome and lease state separately. Timeout/connection loss
after a provider call is an unknown outcome until reconciliation. A PostgreSQL
provider's role cleanup is not evidence about already-connected sessions. Obtain
separately authorized DB-admin termination and verify the exact old role/session
set. Do not invent a universal provider-revocation command.

## Ordered operator procedure

### 1. Preserve evidence and choose the incident path

Record an incident identifier, reason, UTC times, exact binary/image and source
identity, instance identity/incarnation, principals, scopes, grant origins/rules,
session/delegation metadata, provider/consumer identifiers and pending jobs.
Preserve independently retained logs/audit and a consistent encrypted archive
before destructive changes when safe. Retain original immutable evidence;
sanitized working copies do not replace it. Never copy raw bearer values,
passwords, root keys or delivered plaintext into the incident log.

Record each planned step **before** running it, then append its committed
outcome, correlation/event identifiers, independently stored evidence digest,
residuals and next reconciliation action. Keep this journal outside the affected
datastore/backup failure domain. A lost response, restart or missing audit outcome
means `UNKNOWN`, not success. Read current state before resuming. Do not restore
the old credential as a retry mechanism.

Blocked attempts with no established disclosure require evidence and bounded
investigation, not automatic global shutdown. Credential theft or provider
disclosure follows the scoped sequence. Suspected host, process or DB control
requires external isolation, stopping all writers, and recovery on a clean host;
do not trust an attacker-controlled process to certify its own containment.

Audit collection/completion and notification acceptance are dependencies of
#1034, #1035, #1036, #1037 and #1038. A `200` or some JSONL alone is not complete
export proof. This lane sends no external notification and claims no independent
retention, delivery receipt or production evidence.

### 2. Isolate and retain clean administration

Use the installation's separately approved ingress/network/host procedure.
Enumerate all HA nodes, direct/alternate listeners, automation writers, SCIM
callers and peers; stop or fence all untrusted paths, drain old requests, and
verify denied access from an untrusted synthetic source plus a working clean
operator path. Keep only explicitly required, clean provider cleanup egress.
The service scheduler and provider queues are writers too. For host/DB recovery,
stop every writer and scheduler; perform provider cleanup from clean independent
authority instead. Do not run cleanup through a compromised host.

Verify a clean manager at every affected org/instance scope, with independent
authentication and recoverable custody. Transfer administration before removing
the last manager. Confirm the scope and irreversible effects in the journal.
If this cannot be done safely, retain external isolation and use the existing
approved host-local recovery owner. Never remove a last-manager invariant or
reuse privacy restrictions to get past it.

### 3. Remove the principal's effective authority, then reset last

Use a clean CLI profile with no inherited org/project/environment defaults.
Resolve the exact instance and verify its identity before mutation. List at
instance scope and in **every** organization; inspect all project/environment
grant scopes, origins and access-rule selectors. Expand the affected set to
principals, service accounts and grants the attacker created or changed.

Existing spellings (substitute confirmed IDs, never bearer values):

```sh
hikyo access grant list --instance clean --instance-scope -o json
hikyo access grant list --instance clean --org affected-org -o json
hikyo access rule list --instance clean --org affected-org -o json
hikyo access grant remove --instance clean --instance-scope --principal affected-principal --capability read
hikyo access grant remove --instance clean --org affected-org --project affected-project --principal affected-principal --capability read
hikyo access rule remove affected-rule --instance clean --org affected-org
```

These are examples of **one exact row at a time**, not a bulk containment
command. Supply `--env` only for a grant actually held at that exact depth;
remove each remaining capability and ancestor separately. Re-list after each
origin-owner release. A surviving SCIM, structural or lockout-retention origin
blocks completion and must be handled by its owner. Fence its writer first.
Remove all rules, including whole-environment and management rules.

Then issue the final reset using the existing appropriate tier. For host-local
authority, after establishing the installation's stopped-writer/operator path:

```sh
hikyo admin reset-credential --principal affected-principal --output-file incident-reset-authority
```

Use a new private output file in approved custody. The command creates the
display-once authority with restrictive permissions; do not print it to logs.
Network reset uses `hikyo account reset-credential affected-principal` with the
same private-file delivery policy and a clean MFA-capable `credential-reset`
session. An instance-capability target must use the host tier. Reset revokes
target sessions and developer delegations atomically; it does not replace the
password until establishment and does not remove all authentication methods.

If resuming after an interrupted sweep, re-inventory first and issue a new final
reset. It sweeps previous outstanding establishment authorities and does not
reactivate revoked developer rows. Reconcile unexpected system origins instead
of treating a second reset as a substitute for their removal.

Before `CONTAINED`, use synthetic equivalent artifacts in a drill to verify
old human/developer refusal, and a fresh login through each still-allowed
affected method followed by denied read/disclosure, self-regrant and developer
mint. Never replay stolen production secrets merely to generate proof.

If no independently held clean factor/recovery proof remains, keep the account
deauthorized. Host password reset is not permission to manufacture possession
proof or to delete factors through SQL. Use the existing recovery-code owner
when clean custody permits it, or the clean empty-target restore and independent
human reconciliation path below. Do not regrant merely because a replacement
password was established while a compromised factor/IdP remains allowed.

### 4. Replace upstream secrets, adopt consumers, then replace local custody

Use each provider's approved replacement/revocation procedure from a clean
operator identity. Where overlap is safe, mint replacement, update every
consumer, verify adoption, revoke the compromised value, and separately drain
old sessions/caches. Where containment requires immediate revocation, revoke
first and record the resulting outage. Verify upstream refusal; local deletion
is not that evidence. Do not put secret values in shell argv or reports.

Preserve or replace the clean cleanup credential before retiring the old one.
Queue affected dynamic leases/adapter scrubs through their existing owners and
observe verified terminal effects. Keep `HOLD` for pending, failed or ambiguous
cleanup. After an independent provider/admin replacement, enter the new local
encrypted credential through the supported file/stdin interface, review its
target and clean authority, and test only the authorized synthetic destination.
Only then remove obsolete local custody. Provider cleanup authority must not be
lost by clearing custody prematurely.

### 5. Rotate encryption custody in the required order

If encryption custody may be compromised, first recover clean host/configuration
control. Inventory root/age custody, archives, snapshots, replicas, exported
data and dual-wrapped transitional copies. Preserve independently approved
archive, original matching root and age identity in separate failure domains.
New keys do not make a stolen readable copy unreadable.

1. Generate the new root through the existing clean custody owner. Enroll its
   alias in `rollout.rootSources` and mount it through the installation's
   [configuration-rollout procedure](../site/src/content/docs/docs/configuration-rollouts.mdx).
   `HIKYO_NEW_ROOT_KEY_FILE` must exactly select the enrolled mounted projection
   `/run/hikyo/rollout/sources/root/<alias>/root-key`. An arbitrary next-key file
   refuses. Initial setup must not select the candidate before enrollment exists.
2. Run root **prepare** while the current root still boots. Record the committed
   dual wrappers. Install/select the enrolled candidate on every relevant node
   through the existing deployment owner, then **verify**, then **finalize**.
   Record the finalized epoch and verify a fresh boot on the new root. An
   interrupted dual-wrapper state is pending, never finalized. Preserve both
   required custody sources until the state is reconciled.
3. Rotate the master only after root finalization. Rotate the instance DEK and
   every affected project's DEK. Complete re-encryption on **each same scope**,
   including instance credential tables, historical/pending/snapshot custody
   and adapter/remote fields handled by their existing owners. Check retirement
   and stragglers, readable data and a fresh keyring load. A new DEK version or
   an interrupted sweep is not completed recovery. Resume re-encryption rather
   than repeatedly minting versions or discarding straggler checks.
4. Rotate change-token/scanning keys separately if required by exposure. A token
   recomputation wave or a scanner-key change is not bearer/upstream revocation.
5. Re-establish independent root escrow proof for the finalized root/incarnation
   and create/test a new encrypted recovery point. Root rotation and restore
   invalidate old escrow verification bindings. Reconcile old retained copies
   without destroying the only approved recovery point.

**CLI dependency: [#1033 / PR #1042](https://github.com/Hikyo-Org/Hikyo/pull/1042).**
The inspected baseline panics because `rotate-dek` and `reencrypt` redefine the
common `--instance` flag. Do not run or bless those baseline commands. This lane
does not duplicate their patch. The dependency's verified source revision is
`8ce0143782f516e63624732a3c14d7b5f39a51e6`, with its command and completion
evidence in the [#1033 handoff](https://github.com/Hikyo-Org/Hikyo/blob/8ce0143782f516e63624732a3c14d7b5f39a51e6/docs/handoff/1033-cli-dek-rotation.md).
It retains `--instance NAME` for target selection and uses `--instance-scope`
for instance encryption scope:

```sh
hikyo rotate-dek --instance clean --instance-scope --yes
hikyo reencrypt --instance clean --instance-scope
hikyo rotate-dek --instance clean --org affected-org --project affected-project --yes
hikyo reencrypt --instance clean --org affected-org --project affected-project
```

These spellings require that dependency, a trusted selected instance and an
authorized current-session MFA ceremony. Its real CLI/API/service tests cover
both engines, interruption, resume, retirement refusal and completed walks.
That separate source proof is not an integrated #1039 rehearsal: record the
integrated revision and completion evidence before claiming one.

Until that dependency is integrated, the existing service owners are
`Rotation.RotateRootKey(prepare, verify, finalize)`, `RotateMasterKey`,
`RotateDEK` and `Reencrypt.ReencryptInstance` / `ReencryptProject`. Their tests
are source/local evidence, not proof that the broken baseline CLI works. No raw
SQL editing, hierarchy regeneration or signing bypass is an alternative.

### 6. Empty-target restore and reconciliation

Use the [backup/restore owner guide](../site/src/content/docs/docs/backup-and-restore.mdx).
Stop all writers on every node. Authenticate the archive before touching an
isolated **empty** target of the matching engine. Keep its network/providers
isolated, use a new SQLite target or explicitly disposable PostgreSQL database,
and retain the original matching root independently from the age identity.
Never run a rehearsal against a production target or an occupied scratch DB.

```sh
hikyo restore run --from approved-archive.age --identity-file backup-identity
hikyo restore status
hikyo restore reconcile --principal independently-approved-principal
```

These host commands use the installation's explicitly loaded configuration:
`HIKYO_DB` must name the empty target, not the source. They require the supported
binary/trust inputs and matching root described by the owner guide. A schema
import alone is not current-incarnation runtime admission. Apply the existing
upgrade/restore receipt and operator admission gates; never force runtime open.

Reconcile each human and its intended roles independently. Restored passwords,
factors, session/bearer and single-use artifacts stay inert; restored identity
links are not blessed by a new SCIM assertion. Reprove identity/reset credentials,
reenrol clean factors, review grants/rules and re-mint approved machine
credentials/bindings. Reapply independently approved privacy receipts through
their owner; do not invent an incident receipt that behaves like one. Review
active and pending adapter destinations, cleared outbound credentials, dynamic
leases, remotes and external effects against current provider reality before
re-enabling any worker. Restored managed credentials remain fenced until explicit
security reconciliation/resumption under their owner.

The existing `hikyo restore drill` accepts separate archive identity/root files,
`--principal`, `--project ORG/PROJECT` and either an empty `--target-sqlite` or
`--target-postgres-dsn-file`. Its `--cleanup` removes a successful SQLite target;
the operator must drop their owned PostgreSQL scratch DB separately. A drill
**writes source health/audit state**. It is not read-only or production-authorized
by this handoff. Failed scratch targets/evidence are retained deliberately until
review, then removed using their recorded ownership inventory.

### 7. Reopen deliberately

Require the complete residual ledger, verified clean credentials/factors/IdPs,
consumer adoption and upstream refusal, terminal provider cleanup, completed
encryption/re-encryption, healthy current-incarnation admission, readable
recovery proof, and working clean managers. Re-test stale sessions/delegations,
fresh remint attempts and unrelated-principal positive controls. Regrant only
the independently approved minimum through existing grant/rule owners. Old
bearers and reset authorities must remain dead after regrant or provider
re-enable. Release external fences incrementally and verify the actual entry
paths; retain `HOLD` for any unverified scope. This source PR neither reopens a
deployed installation nor authorizes production recovery.

## Reproducible evidence and remaining gates

The fixture scope is synthetic SQLite and an exclusively owned PostgreSQL
database/container, local test IdPs and provider fakes. No real incident,
production mutation, external messages, cloud spending or provider cleanup
receipt is claimed. The existing metadata table consumes the corrected revocation
state. Matched desktop/mobile screenshots belong in the PR only, with any
reconstructed baseline labeled. Screenshots do not establish backend revocation.

New real-owner regressions in
[`internal/isolation/incident_containment_test.go`](../../internal/isolation/incident_containment_test.go):

- `TestManualIncidentContainment`: actual passkey delegation mint and disclosure;
  reset-only and grants-only failures; interleaved fresh login/remint; full
  grant/rule sweep; final reset; stale-artifact refusal; compromised allowed
  factor cannot recover authority; password replacement; later regrant cannot
  revive an old delegation; unaffected principal control.
- `TestManualIncidentContainmentAllowedIdPCannotRemintAuthority`: a real local
  OIDC flow remains allowed; fresh login cannot read, disclose, self-regrant or
  mint after the sweep. This tests the distinction between account authentication
  and the scoped authority claim.
- `TestCredentialResetTerminallyRevokesDeveloperCustody`: both reset tiers,
  durable revocation metadata and exactly one attributed developer revocation
  event across repeated reset; host actor and network operator stay distinct
  from the delegating human. Reset issuance/mint events also preserve the actual
  operator rather than attributing their action to the affected human.
- `TestCredentialResetConcurrentDeveloperMint`: a prepared real mint ceremony
  races final reset; any successful earlier mint is unusable on completion.
- `TestManualIncidentContainmentCLI`: the documented grant/rule inventory,
  exact-scope removal and private-file network reset commands traverse the real
  TLS router, service authorization and both engines, followed by old-artifact
  refusal. The tests above cover fresh-login authorization negatives.
- `TestManualIncidentDynamicCleanupResumes`: missing local provider custody
  leaves revocation pending without a provider call; a restarted worker loads
  replacement custody, records ambiguous cleanup as unknown, and reconciles it
  to revoked. The external provider is a controlled fake, not a live receipt.
- `TestProviderRevocationLeavesExistingConsumerSession` in the PostgreSQL
  provider package: over the existing disposable TLS provider fixture, dropping
  the lease role refuses a new password login while an established connection
  can still execute `SELECT 1`. Consumer-session teardown is a separate act.
  The integration tests and their shared fixture execute inside the existing
  outbound-provider package boundary. The first hosted invariant run rejected
  the external test package's raw connection; no driver allowlist or guard was
  widened to repair that placement.
- `scripts/ci/start-dynamic-pg_test.sh` forces a private caller umask and checks
  public certificate/HBA readability, private-key confidentiality, owned-volume
  cleanup and the seven existing invalid/error port-discovery refusals. The
  real TLS exercise exposed unreadable copied public files; the fixture now
  sets their modes explicitly and removes its anonymous volume on failed start.

With an exclusively owned disposable PostgreSQL database configured in
`HIKYO_TEST_POSTGRES_DSN`, reproduce the scoped tests from the repository root:

```sh
go version
go test -p 2 -parallel 2 ./internal/isolation \
  -run '^(TestManualIncident.*|TestCredentialResetTerminallyRevokesDeveloperCustody|TestCredentialResetConcurrentDeveloperMint)$' \
  -count=1 -timeout=5m -v
```

The pinned Go version is `1.27.3`. An absent PostgreSQL DSN can skip that local
leg; a SQLite-only pass is not both-engine evidence. The isolation harness
creates a derived database and resets its schema, so never point it at durable
data or a shared test database owned by another lane. Destroy the entire owned
synthetic database/container after the run, including derived databases.

The initial SQLite run reproduced an unrevoked developer custody row after
successful reset. The composed manual-workflow test passed before the repair,
confirming that the repair does not invent a new login-disable semantic.

Validation revision, final results, dependency CLI proof and remaining human
review are recorded in the PR. Required suites preserve the existing logout,
network reset, lockout/break-glass, service-account/federation, provider cleanup,
full recovery order, re-encryption/straggler, enrolled-root and empty-target
restore regressions, including restore-clears-outbound-credentials.

Full #1039 acceptance remains conditional on integrated #1033 CLI proof, the
maintainer security review, and separately authorized installation/provider/
notification evidence for any such claimed scope. Cross-provider review
unavailability is `SKIPPED`, never `CLEAN`; this task grants no ADR waiver.
