# Handoff: #152 approval-mediated temporary access requests and grants

Issue: https://github.com/Hikyo-Org/Hikyo/issues/152. Builds on the #151
approval engine's shapes (approver sets, SCIM-group eligibility, reauthentication
purposes) without sharing its tables.

Spec: `docs/adr/mvp-boundary.md` declared amendment 5 + criterion **C-ACC**;
`docs/adr/permission-model.md` amendment banner (time-bound human grants).

## What shipped

Full vertical, both engines (sqlite + postgres):

- **Migration 00060**: `access_policies`, `access_policy_approvers`,
  `access_policy_bypassers`, `access_requests`, `access_votes` (proof-bound,
  project/environment classes) and `access_grants` (class `authn`, the
  time-bound grant rows). Env deletion cascades requests and rows.
- **Chokepoint**: `ListGrantsForPrincipal` unions the principal's
  `access_grants` rows whose `expires_at` is after the transaction clock, under
  the same privacy and restore-reconciliation gates. The service `authorize`
  prelude fixes the clock (`TxAuthorizer.SetClock`); an unset clock is the wall
  clock. Historical recovery of schemas 50-59 uses a pre-60 projection.
- **authz**: `access.policy-write` / `access.policy-read`
  (`manage-members@project`), `access.request-create`, `access.request-read`
  (audited-none), `access.vote`, `access.cancel`, `access.revoke`,
  `access.bypass` (all `read@env`; every one except `access.request-read` has a
  reachable post-grant 403). Scheduler site gains the sweep and metric doors.
- **audit**: ten `access.*` events (policy_changed, policy_read, requested,
  voted, granted, cancelled, invalidated, revoked, expired, bypassed), tenant
  trail, SECURITY retention, no value material.
- **service** (`internal/service/access.go`): policy CRUD; immutable requests
  bounded by the covering policy; votes with live eligibility, self-approval
  rule and an approver-can-grant check; grant on quorum in the vote
  transaction; cancel; early revoke (rows deleted, holder's sessions
  rotated); emergency access (named principal, `access` reauthentication
  purpose, reason, default one hour capped by the policy); hourly
  `access_expiry_sweep` (releases rows, rotates sessions, `access.expired`).
- **API**: `access-policies` CRUD (project) and the environment's
  `access-requests` queue, create, `emergency`, `{id}/vote|cancel|revoke`;
  `access` joins `ReauthPurpose`. Regenerated apigen + TS client.
- **CLI**: `access policy list|create|update|delete`,
  `access request list|create|approve|reject|cancel|revoke|emergency`.
- **web**: project-scoped **Temporary access** surface (request form, queue,
  decisions, revoke, policy editor, emergency access through the ceremony
  modal).
- **metrics**: `hikyo_access_requests_open`, `hikyo_access_grants_active`,
  guarded by `hikyo_access_gauges_known`.

## Decisions taken / deviations from the handoff comment (accept or ticket)

1. **Temporary grants are their own table, not `grants.expires_at` + a `jit`
   origin.** A row shared by a `jit` and a `manual` origin would keep a NULL
   expiry once the manual origin is released through any of the several
   release paths (manual revoke, SCIM, lockout retention, privacy), silently
   becoming permanent. A separate table cannot be promoted, is invisible to
   the grantor bound (so temporary authority can never be re-granted by a
   project member manager who happens to hold it), and needs no origin
   bookkeeping. `grant.created` does not fire; `access.granted` carries the
   principal, capabilities and absolute expiry instead.
2. **Access policies carry their own approver set, not an
   `approval_policy_id`.** Referencing a #151 policy would make any
   environment with an access policy also gate its publishes (the change
   gate keys on the environment). Access votes likewise have their own table
   rather than an `approval_requests.kind`.
3. **Requests require `read@env`.** A request is never a discovery oracle; a
   principal who cannot see the environment cannot ask to be lifted in it.
   Policy administration is `manage-members@project` (MFA-mandatory).
4. **Votes take no reauthentication ceremony** (the approver must be an
   eligible approver who could grant every requested capability and hold
   `read@env`); emergency access does.
5. **Emergency access binds the environment alone** (purpose `access`, empty
   key unit, consumed directly so an empty unit cannot waive it). The CLI
   runs it through the inline TOTP ceremony; a passkey-only (window 0)
   environment needs the browser, because `cli_reauth_handoffs` constrains
   its purpose set and requires a key unit. Workspace step-up does not carry
   it either (the modal says so).
6. **Folders are not a scope**; requests address one environment; a policy
   with `environment_id` empty covers each environment of the project.
7. **Disabling or changing a policy is lazy for open requests** (invalidated
   at the next decision, as in #151) and never touches granted access, whose
   absolute expiry stands.
8. **Not built (follow-ups):** a "Request access" affordance on the matrix's
   403 states, and temporary grants on the Members page (the Temporary access
   queue shows them with their end time).

## Tests

- `internal/isolation/access_e2e_test.go` (both engines): the lifecycle
  (policy refusals, request bounds, pre-approval refusal, ineligible voters,
  exact expiry, one second before/after expiry on the chokepoint, manual
  grant outliving the temporary one, sweep with session rotation, approver /
  holder / member-manager revoke, cancel, reject, review expiry,
  approver-cannot-grant, policy change invalidation, disabled policy, emergency
  access refusals and time bound, reconciliation and privacy gates, env delete
  cascade); concurrent approvals from two service instances; cross-node expiry.
  `runAccessLifecycle` is also the audit suite's emitter for all ten types.
- Playwright (`web/e2e/flows/matrix.spec.ts`, `temporary access`): browser
  request by an invited read-only user, browser approval by the administrator,
  real use, a five-second grant refused after expiry with no sweep, revocation
  ending access and the holder's sessions; pinned assertion set, desktop and
  mobile.
- Web unit tests and stories for the page.

## Gotchas / recipes for the next session

- `internal/buildcompat/development.json` must be regenerated against
  postgres **18** (16 yields a different schema digest). Docker works here:
  `docker run -d --network host -e PGPORT=5434 ... postgres:18`, then the
  recipe in `docs/handoff/744-adapter-origin-conflicts.md`.
- The legacy upgrade drill fixture (`internal/app/backup_upgrade_drill_test.go`)
  enumerates post-legacy migrations; a new migration adds its version and its
  reversal there.
- Local Playwright with the preinstalled Chromium: point
  `PLAYWRIGHT_BROWSERS_PATH` at a directory whose `chromium_headless_shell-<rev>`
  links the installed headless shell under the expected
  `chrome-headless-shell-linux64/chrome-headless-shell` name.
- Re-pin `annotated_queries.json` / `operation_formulas.json` from the failing
  test's `current:` block, and the web `sensitiveInventory.json` with a review
  note.
