# Handoff: #151 secret-change approvals (policy-bound review and merge)

Issue: https://github.com/Hikyo-Org/Hikyo/issues/151. Model label
`model:opus-4.8`; implemented by Claude Opus 4.8. Review routing: Codex
`gpt-5.6-sol` high (one-way, per WORKSTYLE).

Spec: `docs/adr/mvp-boundary.md` declared amendment 3 + criterion **C-APV**;
`docs/adr/permission-model.md` amended fog note + the Publish-formula
conditional-approval conjunct.

## What shipped

Full vertical, both engines (sqlite + postgres):

- **Migration 00037** (both engines): `approval_policies`,
  `approval_policy_approvers`, `approval_policy_bypassers`,
  `approval_requests`, `approval_votes`. FK `ON DELETE CASCADE` from requests
  to environments and from policies to projects; votes/approvers/bypassers
  cascade from their parents.
- **authz**: `OpApprovalPolicyWrite` / `OpApprovalPolicyRead`
  (`project-settings@project`), `OpApprovalRequestRead` (`read@env`,
  audited-none), `OpApprovalVote` (`publish@env`). `OpApprovalBypass` exists as
  the bypass reauthentication-purpose + audit carrier. The **merge/bypass
  DECISION is not an operation** — it is a live conjunct (`approvalGate`) inside
  the ordinary `OpValuePublish` chokepoint.
- **audit**: eight `approval.*` events (`policy_changed`, `policy_read`,
  `requested`, `voted`, `merged`, `invalidated`, `expired`, `bypassed`), all
  tenant-trail, SECURITY retention, no value/plaintext/digest of a value.
- **ceremony**: two reauthentication purposes, `approve` and `bypass`.
- **service** (`internal/service/approvals.go`): policy CRUD, voting (live
  eligibility, quorum, idempotency, self-approval veto), the publish gate
  (create request / merge / emergency bypass), and the hourly expiry sweep.
- **API**: `approval-policies` CRUD (project), `approval-requests` list + vote
  (env), and `publishPendingChanges` gains `approval_request_id` / `bypass` /
  `purpose` request fields and a **202 `ApprovalRequestSummary`** response when a
  covered publish stages a request.
- **CLI**: `approval policy {list,create,update,delete}` and `approval request
  {list,approve,reject,merge,bypass}`; `values publish` prints the staged
  request on 202.
- **web**: a project-scoped **Change approvals** surface (policy editor +
  per-environment review queue with approve/reject/merge/bypass) and the matrix
  / restore publish flows branch on the 202 ("submitted for approval").
- **metrics**: `hikyo_approval_requests_open` and
  `hikyo_approval_requests_expired_total`, label-free.
- **scheduler**: `approval_expiry_sweep` job beside `payload_gc`.

## Decisions taken / deviations from the original handoff (accept or ticket)

1. **Request creation rides the publish proof, not `edit@env`.** `PublishPlanned`
   only materializes the CALLER'S OWN drafts (`resolveVersions` →
   `ListForOwner(caller)`), so the merger must be the requester. Creating a
   request therefore happens inside the publish attempt (which already holds
   `publish@env`), not via a separate `edit@env` verb. Consequence below.
2. **The merger is the requester; the bypasser is the requester.** A separate
   person cannot merge or bypass another's reviewed change in v1, because there
   is no cross-owner publish of staged drafts. A follow-up that adds cross-owner
   publish would lift this.
3. **Bypass is request-bound**: an emergency bypass names an existing request
   (no free-standing bypass), takes a current reauthentication ceremony and a
   reason, and resolves that request to `bypassed`. It cannot mutate the policy.
4. **`OpApprovalRequestRead` is `read@env`** (the original handoff said
   `publish@env`), so the review-queue read is audited-none like every other
   `read` conjunction; the queue carries no value plaintext.
5. **Invalidation is lazy** (at vote/merge), not eager on policy update: the
   policy-write proof is project-scoped and deliberately does not reach into
   env-scoped request rows. A policy version bump fails every pinned request
   closed the next time it is voted on or merged, emitting `approval.invalidated`
   from that env-scoped proof. The invalidation COMMITS and the operation then
   refuses (commit-then-refuse), so the state change is not rolled back with the
   error.
6. **Out-of-band value changes** (`values copy` / bulk-apply / clone-onto an
   existing env / import) into a covered environment are **refused** with
   `ErrApprovalRequired` via a single `republish` chokepoint. Structural
   fan-outs (schema `declare`, `environment-create`, and the draft
   `values`/`restore` publishes that already ran the gate) are exempt.
7. **Deferred, with disclosure: the in-browser vote/bypass reauthentication
   ceremony as a first-class WebAuthn purpose.** The client `Ceremony` binds
   `SIGNED_OPERATION ∈ {reveal, copy, publish}`; `approve`/`bypass` are distinct
   server operations. A protected (window-0) environment can only spend a
   single-decision WebAuthn window, which is operation-bound, so vote/bypass in
   the browser work today only where the environment's effective reauthentication
   window is > 0 (an unbound TOTP/step-up window is accepted for any intent). The
   full approve/bypass-as-WebAuthn-purpose wiring (openapi `ReauthPurpose` enum,
   `cli_reauth.go`, `webauthn.go` intent, `Ceremony.tsx`) is a follow-up. The
   vote/merge/bypass FLOW itself is exercised end to end at the service layer on
   both engines.

## Tests

- `internal/isolation/approvals_e2e_test.go`: the full lifecycle on both engines
  (policy CRUD, gated-publish request creation, quorum/idempotency/conflict/
  self-approval, merge through the ordinary publish, invalidation on a policy
  change, expiry sweep, reauthenticated bypass) — also the runtime audit-emitter
  obligation for all eight events. Plus `TestApprovalEdges*` for the four
  fail-closed edges (draft-edited, policy-deleted merge, env-delete cascade).
- Playwright (`web/e2e/flows/matrix.spec.ts`, `change approvals` describe): the
  browser proposal (covered publish → 202 → request in the queue) and the
  surface pinned assertion set. Rides `matrix.spec.ts` (group on main) because a
  new spec file's pinned claims never run on the PR.

## Gotchas / recipes for the next session

- Re-pin `operation_formulas.json` and `annotated_queries.json` from the
  failing-test `current:` block (no `-update` flag; see the two `TestInvariant`
  regenerations in the branch history).
- New scheduler store ops → `systemSites[SiteScheduler]` + the
  `TestInvariant11SystemProofEnumeration` pinned set.
- Wire-registry snapshot counts (`internal/authz/wire_registry_test.go`) move
  when classify.go grows entries.
- e2e ports for this session: `HIKYO_E2E_PORT`=45870 … `_OIDC2`=45876;
  `NODE_OPTIONS=--dns-result-order=ipv4first`; desktop then mobile sequentially.
- Node 24 (`eval "$(fnm env)"; fnm use 24`) before any pnpm/tsc/playwright.
