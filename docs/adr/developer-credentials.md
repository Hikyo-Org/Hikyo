# Bounded developer credentials (#807, draft)

Status: **draft, not locked or operative**. The owner selected the bounded
developer-credential decision lane on 2026-10-09 and accepted future-key
consent and self-delegation. The lifecycle recommendation implements the
owner's instruction to preserve security without breaking development UX.
The precise lifetime policy below remains a proposal for final confirmation.
Credential implementation must wait for the completed grilling, adversarial
cross-provider review, explicit owner lock, and the amendment procedure in
[OSS mechanics](./oss-mechanics.md).

Review availability: the owner confirmed on 2026-10-09 that there is no Claude
subscription. The cached model choices remain OpenAI `gpt-6.1-sol/high` and
Anthropic `claude-opus-5-5/high`, but the Anthropic reviewer is unavailable.
Ordinary OpenAI Standards and Spec reviews can improve this draft; they are
not the cross-provider lock review. Do not request Claude quota, launch Claude,
or substitute a paid API/provider without explicit owner authorization.

## Problem and accepted bounds

[Issue #807](https://github.com/Hikyo-Org/Hikyo/issues/807) asks for local
development launched by non-TTY agents without exporting values to `.env` or
giving an agent an ordinary human session. Ask A already shipped in
[#811](https://github.com/Hikyo-Org/Hikyo/pull/811). This decision addresses ask B.

The owner accepted these design bounds:

- A separate developer credential, minted interactively by a human.
- One unprotected environment, identified by immutable tenant IDs.
- An eight-hour default and an instance ceiling measured in hours.
- Current human `read` and `reveal` authorization checked on every fetch.
- Audit attribution to the delegating human.
- No renewal and no offline delivery.

The existing `run --use-human-session` flag, controlling-terminal requirement,
enumerated confirmation, and bound reauthentication remain unchanged. CI OIDC,
MCP OAuth, dynamic-secret lease renewal, a resident agent, a preferred snapshot
cache, and a varlock plugin are outside this decision. Existing loader
composition remains sufficient until a concrete plugin need is demonstrated.

## Grilling outcomes

1. **Login coupling.** Owner direction: prioritize security without breaking
   development UX. Proposed resolution: ordinary logout or login-session expiry
   does not end the delegation. Account disable/removal, access loss, credential
   epoch invalidation, explicit revocation, expiry, and environment protection
   prevent the next fetch. Alternative: end the delegation when the issuing
   login session ends, accepting interruptions during agent work.
2. **Future keys.** Accepted: consent explicitly covers current and future
   published keys in the one environment until expiry. Alternative: freeze the
   enumerated key set at mint and require another ceremony for added keys.
3. **Mint authority.** Accepted: a human with current `read(E)` and
   `reveal(E)` can delegate their own bounded delivery access after fresh
   scope-bound reauthentication, without `manage-identities`. Alternative:
   additionally require `manage-identities(project)`, accepting administrator
   involvement for ordinary developers.

## Proposed identity and authorization model

Introduce a distinct developer principal, not another service-account kind or
a bearer that resolves directly to the human. It has exactly the fixed
delivery authority for one environment and a recorded human authority
principal. Generic grant mutation, service-account rotation, federation,
historical pins, writes, metadata export, and ordinary human-session endpoints
must not accept this principal or its credential.

The fixed authority belongs to the developer principal; different credentials
must not narrow or widen an existing service account. The implementation must
make the closed delivery surface explicit at admission and the authorization
chokepoint. Possessing a developer credential cannot mint another one.

Every fetch must resolve the credential and developer principal, check its
fixed tenant/environment scope, expiry, revocation and epoch, resolve the
delegator's current active account and memberships, reject a currently protected
environment, and check the delegator's current `read(E)` and `reveal(E)` in the
same authorization transaction as disclosure. Missing state fails closed.
Permission or protection changes concurrent with disclosure follow the
repository's transactional authorization discipline on both database engines.
No request may substitute a caller-supplied human authority ID.

The credential delegates current published delivery only. It cannot use
`reveal-history`, request a historical revision, attach a pin, or gain access
to another environment through inherited grants. Subsequent increases in the
human's grants cannot widen the developer principal's fixed authority.

Minting requires an explicit TTY ceremony that shows the server, organization,
project, environment, lifetime, current key list, and accepted future-key
policy. The human confirms and performs a fresh reauthentication bound to that
disclosure/delegation unit using the existing authentication mechanisms. A
generic existing reauthentication window must not authorize a different
environment or changed consent unit. Non-TTY minting fails before any token is
created. No new password, cookie, MFA, or session implementation is introduced.

Per-fetch delegation checks do not require a continuing human reauthentication
window: unattended delivery is the explicit bounded exception decided here.
The chosen account/session invalidation semantics must be encoded separately
from that window. Ordinary human-session assurance must never be silently
fabricated for a developer principal.

Snapshot the delegator's principal generation and the minting authentication
provenance. Any principal-generation advance invalidates the delegation,
including non-SCIM grant increases and factor changes under the existing
human-auth policy. SCIM origin changes retain their existing org-specific
semantics: check current membership and grants for this environment, without
invalidating unrelated organizations' delegations.

Security invalidation is terminal. Account disable/removal, explicit security
revocation, provider/identity invalidation, and an environment becoming
protected revoke the affected developer credentials in the same mutation
transaction. Relevant SCIM access loss revokes the affected environment's
credentials without revoking unrelated org access. Re-enabling an account,
rejoining an org, or unprotecting an environment cannot revive an old bearer;
another ceremony is required. Per-fetch checks remain mandatory even with
those proactive revocation hooks.

Provider disable/removal or assurance-policy change and identity unlinking
invalidate delegations minted through that provider/identity even if the
human still has another login method. A provider or identity recreated later
must not revive them; persist monotonic invalidation or atomically revoke the
affected rows. The parent login session need not remain live, but its immutable
ID and authentication provenance remain audit facts, not authorization inputs.

Ordinary `logout` retains bounded developer credentials and reports that fact
with the revocation command. Explicit security revocation of an issuing
session also revokes its developer credentials; that operation is distinct
from ordinary logout even though both delete a login session today.
`hikyo dev session revoke --all` revokes all of the caller's developer
credentials as a security action. Any global account/session revocation path
must include developer credentials atomically; this ADR does not claim that
the current UI already has a global sign-out action.

## Proposed lifetime and revocation policy

The instance policy defaults to an eight-hour maximum, with a hard maximum of
eight hours; operators may lower it. An omitted requested TTL uses the lesser
of eight hours and the current instance ceiling. Zero, negative, over-ceiling,
and indefinite TTLs are refused. The server sets the absolute expiry from its
clock at mint. There is no sliding expiry, refresh token, rotation, or renewal.
Another credential requires another interactive mint ceremony.

Proposed resource bound: at most four live developer credentials per human
per instance, across environments. Count and mint atomically on both engines;
concurrent replicas cannot exceed the cap. Refuse at the cap with instructions
to list and revoke an existing credential, rather than silently replacing it.
Expired or revoked rows do not count; audit history is retained under the
existing audit policy. Name this bound in the operations amendment.

Lowering the instance ceiling atomically clamps each outstanding delegation's
durable expiry to `min(previous durable expiry, minted-at + new ceiling)` in
the same serialized policy-change transaction. Persist the reduction; fetching
must not recompute expiry from only the latest ceiling. A concurrent mint must
observe either the prior policy and the clamp, or the new policy. Raising the
ceiling never increases a stored expiry or revives an expired bearer. Time and
policy are checked server-side; local-file timestamps are not authority.

The owner can list and revoke their own developer credentials without
`reveal`, reauthentication, or the presented developer bearer. Project
`manage-identities` holders can inspect and revoke credentials in their project
for incident response, but cannot retrieve a bearer or reassign its delegator.
Revocation is idempotent and one server call. Local deletion alone is not
server revocation, and a failed server revocation must be reported explicitly.

## Proposed credential custody and CLI behavior

Use Hikyo's existing opaque high-entropy token and hashed-verifier primitives,
with a distinct artifact class admitted only to developer delivery. Server
metadata records the developer principal, human delegator, immutable scope,
mint time, recorded expiry, epoch, and revocation. No stored server field can
recover the bearer. Prefixes are hints; database metadata is authoritative.

Proposed commands are `hikyo dev session --env <id> --ttl 8h`,
`hikyo dev session list`, `hikyo dev session revoke --id <id>`, and
`hikyo dev session revoke --all`.
Final grammar must be reconciled with the API/CLI ADR before lock.

Store the bearer in the CLI's private state outside the repository, with a
0700 directory and 0600 file using the existing private-state custody helpers.
Record the exact trusted origin and immutable tenant/environment binding.
Repository configuration selects a target but contains no credential. Refuse
unsafe state paths, permissions, symlinks, origin mismatches, and ambiguous
credential selection. Mint output prints metadata only, never the bearer.
A local write failure after server mint requires best-effort server revocation
and a clear result if that cleanup fails.

`hikyo run` may discover the selected developer credential for the exact
configured origin and environment without a TTY. Explicit machine credentials
retain precedence. `--use-human-session` remains a separate explicit choice.
No missing, expired, rejected, or inaccessible developer credential falls back
to a stored human session or a different principal. State the selected
credential class and expiry without printing secrets.

Developer delivery must neither issue offline snapshot receipts nor write a
snapshot. Server admission rejects offline reconciliation and other machine
management surfaces for this artifact class. The CLI refuses an unreachable
server even if an old machine snapshot exists. This preserves the next-fetch
revocation guarantee; already delivered child-process values cannot be erased.
The child inherits values, but not Hikyo credential environment variables.
Existing loader-control acknowledgements, collision handling, and execution
limits still apply.

## Audit and threat model

Record mint, refused mint, explicit revocation, refused use, and every disclosed
key. Events carry the developer actor, credential identity, immutable scope,
and recorded human authority. No token or plaintext value reaches the trail.
Audit is committed before plaintext leaves the server. Concurrent replicas
observe durable shared revocation, not process-local authorization caches.

An agent running as the same OS user can read the bearer and delivered values.
This does not isolate secrets from an agent-controlled process. Development
environments must contain development-only values, including inherited values.
An unprotected environment is not proof that its contents are safe. Future-key
consent, if selected, explicitly accepts that additional development values
become available during the bounded delegation.

## Required declared amendments before lock

- [Permission model](./permission-model.md): add the developer principal's
  closed authority, recorded-human current authorization, and selected
  self-delegation mint formula. Declare any `manage-identities` exception.
- [Human auth](./human-auth.md): declare bounded unattended human delegation
  without admitting the ordinary human session to unattended launches.
- [Machine identities](./machine-identities.md): declare the new identity and
  artifact class while preserving service-account grant and rotation rules.
- [Compose integration](./compose-integration.md): add a separate online-only
  developer delivery path; retain the per-launch human-session ceremony.
- [API/CLI surface](./api-cli-surface.md), [audit model](./audit-model.md), and
  [operations spec](./ops-spec.md): record lifecycle formulas, exact route and
  command admission, event shapes, lifetime policy, and resource bounds.
- [MVP boundary](./mvp-boundary.md): explicitly admit bounded developer
  credentials on demand from #807; retain the exclusion of JIT deploy tokens.

Do not mark those amendments operative while this ADR is pending. Follow
the locked-decision reopening and adversarial-review procedure for each owning
decision instead of treating #807 alone as authority to change all ADRs.

## Implementation acceptance after lock

Both SQLite and PostgreSQL must prove successful interactive mint and non-TTY
run, exact-scope refusal, protected-at-mint and protected-after-mint refusal,
current read/reveal loss, account disable/removal and membership loss, epoch
invalidation, precise expiry, lowered ceiling, revoked-before-use, concurrent
revocation/disclosure ordering, no bearer self-renewal, and rejection by every
non-delivery route. Test the selected logout and future-key behavior explicitly.
Test lowering the ceiling past an existing credential's expiry and then
raising it before its original expiry: the old bearer must remain expired.

CLI tests must prove private custody, origin binding, credential precedence,
no automatic human fallback, child credential stripping, existing merge and
loader-control checks, failed local persistence cleanup, and no offline serve
or snapshot write. Audit assertions must distinguish actor and authority and
prove durable per-key attribution with no secret material.

Implementation is not complete merely when this decision locks. #807 remains
open until the accepted credential, guide update, and required validation ship.
