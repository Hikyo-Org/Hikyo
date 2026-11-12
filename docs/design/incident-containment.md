# Scoped incident containment: verified inventory and design

Issue: [#1039](https://github.com/Hikyo-Org/Hikyo/issues/1039), finding F5 of
[#1032](https://github.com/Hikyo-Org/Hikyo/issues/1032).
Status: supporting evidence for the accepted ADR. This document holds the evidence and the mechanics.
The decisions, with their alternatives, are in the
[accepted ADR](../adr/incident-containment.md), and the operator procedure is the
[draft runbook](../operations/incident-containment.md). Nothing here changes product
behavior, and the decisions are the ADR's, accepted by the owner on 2026-11-12.

Inventory verified against `dev` at `426bf59ceb8787234d425f1d5bd2055569fd92d3`
(2026-11-12). The issue pins its evidence to `f14fbec07`, which predates the merged
manual workflow in [#1085](https://github.com/Hikyo-Org/Hikyo/pull/1085) (2026-10-28).
Every item below was re-read at the current SHA. Where the baseline claim no longer
holds exactly, the delta is stated.

## 1. What changed since the pinned baseline

PR #1085 and the CLI repair in [#1033](https://github.com/Hikyo-Org/Hikyo/issues/1033)
(PR #1042) landed after `f14fbec07`. Three consequences for this design:

- A manual, per-principal containment sequence through existing owners already exists:
  [handoff](../handoff/1039-incident-containment.md) and
  `internal/isolation/incident_containment_test.go`. It is an operator procedure. It
  has no incident record, no enumeration surface and no resumable state.
- Credential reset now revokes developer delegations in its own transaction
  (`internal/service/credential_reset.go:281`). The issue's first inventory item
  predates that line.
- `rotate-dek` and `reencrypt` no longer panic. They take `--instance-scope`
  (`internal/cli/testdata/help.txt:181,185`), so the runbook may use them.

## 2. Verified inventory

Each row restates one claim from the issue, gives the current verdict and the evidence.
Line numbers are at `426bf59ce`.

### I1. Reset issuance advances the generation and ends sessions, but the old password stays valid until establishment

**Verdict: holds. One part is stronger than the issue says: delegations are also revoked.**

- `Auth.ResetCredential` (`internal/service/credential_reset.go:62`) and
  `Auth.BreakGlassResetCredential` (`:210`) share `mintResetAuthority` (`:251`). It
  advances the generation (`:271`), deletes the target's sessions (`:274`) and, since
  #1085, persists terminal revocation of every developer delegation (`:281`).
- Nothing in that function touches the password verifier. `Auth.LocalLogin` /
  `attemptLogin` (`internal/service/auth.go:394`) accepts the existing verifier when its
  credential epoch is current (`epochGood`, `:434`, `:457`). A pending reset authority
  is not consulted.
- The verifier is replaced only by `Auth.EstablishCredential` (`:700`), at
  `ReplacePasswordCredential` (`:873`), which then advances the generation again
  (`:893`) and ends sessions (`:896`).
- Test evidence: `internal/isolation/incident_containment_test.go:56-61` (after
  reset issuance the old password still reaches its factor challenge) and `:125-127`
  (refused only after establishment).
- Reset does not reach linked identities, passkeys, TOTP or the external IdP. A network
  reset refuses an account with any linked identity (`credential_reset.go:151-161`) and
  an instance-capability target (`:117-134`). Both then have only the host-local path.

**Gap for containment:** between issuance and establishment the account can still
authenticate with the old password and any allowed factor or IdP. The #1085 sequence
closes this by first removing every grant and rule, so the new session has no
authority. That is destructive and manual.

### I2. Ordinary logout leaves developer delegations active

**Verdict: holds, by design.**

- `Auth.Logout` (`internal/service/auth.go:1096`) revokes the presented session
  (`:1102`) and records `auth.logout`. It does not touch developer credentials.
- Explicit revocation does: `Workspace.RevokeSession`
  (`internal/service/workspace.go:1592`) calls `revokeDeveloperProvenance` (`:1621`),
  and its comment states that ordinary logout deliberately does not.
- The decision is locked in the developer-credentials ADR ("Login coupling": ordinary
  logout or login-session expiry does not end the delegation). The issue forbids silently
  changing it, and the ADR keeps it.
- A delegation also dies when its issuing principal's generation advances
  (`internal/authz/developer.go` liveness check), but that is denial of use, not a
  persisted revocation. See follow-up F1 in the ADR.

### I3. Adapter credential removal clears local custody but does not revoke the upstream PAT

**Verdict: holds.**

- `Adapters.RevokeCredential` (`internal/service/adapters.go:1713`) calls
  `revokeAdapterCredential` (`internal/store/repos_adapters.go:697`). It nulls
  `credential_ciphertext` and the set and expiry timestamps
  (`internal/store/queries/sqlite/adapter_repository.sql:219`, mirrored in the PostgreSQL
  file), retires credential jobs (`repos_adapters.go:715`) and bumps target generations
  (`:718`). No outbound call is made.
- Audit: `adapter.credential.revoke` with `credential_present: false`
  (`adapters.go:1727`). The event cannot say whether the upstream token still works.
- The adapter documentation states the consequence: a later remote scrub may be
  impossible (`docs/site/src/content/docs/docs/deployment-adapters.mdx:327`).
- Clearing custody before the remote work is done removes the credential the scrub
  needs. Targets and adapters are removed "while the PAT still authenticates"
  (`deployment-adapters.mdx:184`).

### I4. Dynamic lease cleanup is asynchronous and needs usable provider authority

**Verdict: holds, with two additional gaps found.**

- `Dynamic.RevokeLease` (`internal/service/dynamic.go:691`) only enqueues the state
  `revoking` and records an intent event (`enqueueLeaseTransition`, `:699`). The provider
  call happens later in `Dynamic.RunLeaseSweep` (`internal/service/dynamic_worker.go:44`).
- The worker opens the provider with the stored admin credential (`openProvider`,
  `:126`). If it is missing (`store.ErrNoProviderCredential`, `:75`) the lease stays due
  and is retried with backoff (`dynamicRetryDelay`, `:29`). It does not fail and it does
  not alert.
- An ambiguous or definite-error drop enters `unknown` (`settleDrop`, `:182`, `:203`),
  surfaced only by the `hikyo_dynamic_effects_unknown` gauge
  (`internal/server/metrics.go:112`).
- `postgres.Provider.DropRole` (`internal/dynamic/postgres/postgres.go:199`) removes
  the role. Its own comment states that this prevents new logins and does not terminate
  backends already connected as the role.
- **Gap A.** `Dynamic.RevokeCredential` (`internal/service/dynamic.go:317`) nulls the
  provider admin credential unconditionally
  (`internal/store/dynamic.go:205`, `dynamic_repository.sql:16`), including while leases
  are `revoking` or `unknown`. Those leases then cannot be dropped until a clean
  credential is set. Provider delete is different: it retains the credential so queued
  drops can finish (`internal/store/dynamic.go:225-233`).
- **Gap B.** Leases can be listed only per environment (`Dynamic.ListLeases`,
  `dynamic.go:640`, requires project and environment). There is no project-wide or
  instance-wide list of non-terminal leases, so an operator cannot enumerate unresolved
  cleanup without walking every environment.

### I5. There is no general quarantine or instance-wide lockdown command

**Verdict: holds.**

- A search of `internal`/`cmd` Go sources for quarantine, lockdown, kill switch,
  freeze, suspend and account lock finds only unrelated or scoped mechanisms: the
  remote-workspace origin kill switch (`internal/authz/forwarders.go:518`), personal
  value suspension (`internal/app/generation.go:475`) and upgrade or backup fences.
  None denies a principal or an instance.
- The closest existing account-level denial is privacy restriction (I6). It is a
  different feature with different authority and receipts.
- Reversible, scoped denials that do exist and that containment can compose with:
  grant and rule removal, project machine-reveal opt-in, environment protection
  (`internal/service/settings.go:163` also revokes delegations in that environment),
  provider disable (revokes delegations by provenance: `oidc.go:504`, `:597`,
  `oauth2_providers.go:192`, `:285`, `saml_providers.go:471`, `:839`), SCIM binding
  disable, adapter and target pause, and external network isolation.

### I6. Privacy restriction has its own receipts, erasure and release semantics, and last-manager safeguards

**Verdict: holds. It is also the only existing account-level denial.**

- `Auth.ApplyPrivacySubject` (`internal/service/privacy.go:95`) takes `restrict`,
  `erase` or `release`. It is host-local only (`internal/app/privacy.go:20`, "exposes no
  network route") and its audit actor is break-glass.
- It sets `principals.privacy_state` (`RestrictPrivacyPrincipal`, `:164`), revokes
  delegations (`:168`) and sessions (`:172`).
- Last-manager safeguard (`:116-151`): for a restrict or erase of an active account it
  locks every org-scope `manage-members` holder, recounts them and refuses with
  `ErrPrivacyLastManager` (`:50`) when none other remains. Project-scope managers are
  not counted (`internal/store/queries/sqlite/permission.sql:94-108`).
- Receipts: `PrivacyReceipt` (`:41`) is kept outside backup retention and re-applied
  after a restore by `ReapplyPrivacyReceipt` (`:231`), which accepts only `erase` and
  `restrict`. A restricted account can be released; an erased one cannot.
- Enforcement is a predicate, `privacy_state = 'active'`, referenced at 14 places in each
  engine's query files, outside the privacy owner's own two: session resolution, account lookup by id, name and email, generation lookup,
  grant and rule loading, the manage-members census and personal-value authorization
  (`internal/store/queries/sqlite/authn.sql:45,54,78,83,88,111,891,904`, `permission.sql:100,107`,
  `rules.sql:25`, `personal_authz.sql:16`, `registration.sql:132`, `webauthn.sql:12`). A restricted principal's grants stop loading,
  so the account has no effective authority, and its grants are preserved.
- This is why privacy restriction must not double as incident containment: it is a
  data-subject action with external receipts, a host-only path and an erasure branch.
  Its predicate sites are, however, the precise enforcement seam a containment state
  would need (ADR, option B of O1).

## 3. Other existing owners the design composes with

These were not in the issue's six items. They bound what containment can claim.

| Class | Revoke owner (path:line) | Notes |
| --- | --- | --- |
| Developer delegations | `DeveloperCredentials.Revoke` (`developer_credentials.go:217`), `revokeDeveloperScope` / `revokeDeveloperProvenance` (`developer_revocation.go:33,41`) | Self `--all` covers the caller only. Project admin revokes per project. |
| Session revocation | `Workspace.RevokeSession` (`workspace.go:1592`), `RevokeAllSessionsFor` (`authz/forwarders_gen.go:1103`) | Self-service lists only the caller's own sessions. |
| Generation advance | `AdvanceGeneration`, called from reset, establishment, factor and passkey changes, grants | Kills sessions and delegations of one principal. No org or instance variant. |
| Password, factors | `Auth.EstablishCredential` (`auth.go:700`), `factors.go`, `webauthn.go` | Replace only the credential they own. |

The remaining classes (machine credentials, federation bindings, SCIM, remotes, login
providers, grants, workspace origins, certificates, outbound custody) are mapped in
section 4.

## 4. Authority and consumer map

Read-only survey of every credential and authority class at `426bf59ce`. "Upstream" says
whether the revoke reaches anything outside Hikyo's own custody. Classes not read deeply are
marked unverified.

| Class | Revoke owner | List scope | Audit trail | Timing | Upstream |
| --- | --- | --- | --- | --- | --- |
| Machine credential | `Identities.RevokeCredential` (`identities.go:560`) | Per service account, project scope (`:490`) | Tenant | Immediate | Local |
| Service account delete | `DeleteServiceAccount` (`identities.go:305`) | Project (`:257`) | Tenant | Immediate. Refused while issued PKI certificates exist | Local |
| Federation binding | Machine revoke; `CreateBinding --replaces` (`federation.go:410`) | Per service account | Tenant | Immediate | Local |
| Federation issuer | `DeleteIssuer` (`federation.go:306`) | Instance (`:262`) | Instance | Immediate. Refused while any binding names it | Local |
| SCIM binding, credential, mapping | `scim_admin.go:512`, `scim_credential.go:202`, `scim_mapping.go:280` | Per org | Tenant | Immediate. Binding delete does not end sessions | Local |
| Inbound remote credential | `Workspace.RevokeConnection` (`workspace.go:386`) | Instance, no limit | Instance | Immediate | Peer keeps its copy |
| Outbound remote | `Remotes.RemoveRemote` (`remotes.go:645`) | Instance | Instance | Immediate | Serving side not revoked |
| Pairing invitation | `RevokeInvite` (`remote_pairing.go:377`) | Instance, newest 100 | Instance | Immediate | Local. Exchanges have no list or revoke |
| Login provider | OIDC `oidc.go:478`, OAuth2 `oauth2_providers.go:166`, SAML `saml_providers.go:816` | Instance | Instance | Immediate; sweeps that provider's sessions and delegations | IdP sessions untouched |
| Factors, passkeys, links | Self-service only (`factors.go`, `webauthn.go`, `oidc_flow.go`) | None for other accounts | Instance | Immediate | None |
| Grant | `Grants.Revoke` (`grants.go:932`) | Per scope, no per-principal view | Both | Immediate; ends sessions only when the row is deleted | Local |
| Rule | `Rules.Revoke` (`rules.go:425`) | Org or project | Tenant | Immediate, ends sessions | Local |
| Temporary access | `Access.Revoke` (`access.go:739`) | Per environment queue, max 200 | Tenant | Immediate | Local |
| Workspace origin | `Workspace.RemoveOrigin` (`workspace.go:555`) | Instance (`:452`) | Instance | Immediate, ends that origin's sessions | Local |
| Adapter credential | `Adapters.RevokeCredential` (`adapters.go:1713`) | Project | Tenant | Immediate, local custody only | Upstream PAT not revoked |
| Adapter teardown | `Delete` (`:2041`), `RemoveTarget` (`:2013`) | Project | Tenant | Queued scrub jobs; credential kept until the last is terminal | Removes remote material unless `--keep-remote` |
| Dynamic admin credential | `Dynamic.RevokeCredential` (`dynamic.go:317`) | Project | Tenant | Immediate. Strands queued drops | Local |
| Dynamic lease | `RevokeLease` (`dynamic.go:691`) | Environment only | Tenant | Asynchronous, durable intent and outcome | Yes, through the worker |
| SSH and PKI certificates, CA keys, transit keys | `ssh.go`, `pki.go`, `pki_certificates.go`, `transit.go` | Environment, issuers instance | Mixed | Local; effective through KRL and CRL | Relying parties must fetch (unverified in detail) |
| Change-token and scanning keys | `revisions.go:675,742` | n/a | Instance | Immediate | Revoke neither bearers nor providers |
| Encryption keys | `rotation.go:96,192,290` | n/a | Instance | Per the rotation order | n/a |

### Findings that bound the design

- **No bulk operation.** No instance-wide or org-wide revoke exists, and no on-demand
  credential epoch or generation advance. The epoch moves only on restore
  (`internal/service/restore.go:154`) and operator-rotation upgrade (`internal/store/upgrade/operator_rotation.go:119`). The
  generation is per principal. Option C of O7 in the ADR would add the first instance variant
  and is not recommended.
- **Almost every revoke is fire and forget.** One local transaction and one audit event,
  with no record of what was meant to be covered. Only dynamic lease transitions, adapter
  scrub jobs, PKI certificate transitions and restore reconciliation keep durable
  intent and outcome records. This is the gap the ledger (O5) fills.
- **No incident correlation.** An operator reconstructs an incident from per-event queries.
- **Enumeration gaps.** Machine credentials have no org or instance listing (the
  `credential-policy set` preview is the only instance-wide view). Leases list only per
  environment, and no gauge counts `minting`, `renewing` or `revoking`. Other principals'
  sessions, factors, linked identities and passkeys cannot be listed or removed by an
  administrator. Grants have no per-principal query, and temporary access is absent from the
  grant list. Pairing exchanges have no list. Adapters have instance counts only
  (`doctor.go:208`).
- **Revoke can hurt cleanup.** Dynamic credential revoke and adapter credential revoke each
  remove the authority later cleanup needs. Provider delete and adapter teardown already
  keep it until queued work is terminal, which is the pattern replace-before-clear extends.
- **Last-manager census** covers `manage-members` at instance and org scope only, runs
  only when a revoke would empty the row, and excludes restricted principals
  (`grants.go:1051,1083`, `permission.sql:100,107`). Whether a remaining holder can still
  authenticate is not checked.
- **Lockout-retention rows** are system origins. A human grant removal refuses them, and only
  adding another holder releases them (`scim_origin_release.go:283,437`).
- **Reset gaps** are as in I1. The network tier refuses instance-capability targets, targets
  with an external identity and multi-org footprints, which leaves only the host-local path.

## 5. Design sketch under the recommended options

The mechanics are in the [ADR](../adr/incident-containment.md#design-under-the-accepted-options).
This section records only what the map adds.

- **State seam.** A `contained` principal state reuses the `privacy_state = 'active'`
  predicate sites in I6. Adding the second condition at those 14 references per engine, plus
  the manage-members census, gives one shared census with no new enforcement path. The
  account lookups by name, id and principal also serve sign-in, MFA and SSO, so they stay
  gated. Reset and establishment need one separate, ungated lookup that only those two
  flows may call.
- **Ledger.** A new table, written by a service that calls existing owners. It records the
  owner function, the object id, and `pending`, `done`, `unknown`, `attested` or
  `out_of_scope`. Dynamic leases already have the intent and outcome shape it would reuse.
- **Listings to add first.** Non-terminal leases above environment level, developer
  delegations of other principals at org level, and per-principal grants. These are
  read-only and independent of every decision, so they are follow-up candidates.
- **Replace-before-clear** extends what provider delete and adapter teardown already do to
  adapter and dynamic credential revoke.
