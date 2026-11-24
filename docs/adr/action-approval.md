# Hikyo action approval (ADR, proposed 2026-11-23)

> **Status (2026-11-23, [#1533](https://github.com/Hikyo-Org/Hikyo/issues/1533), sub-issue of epic [#1532](https://github.com/Hikyo-Org/Hikyo/issues/1532)):**
> *Decision* records the decisions the owner made on 2026-11-23 in the epic,
> grounded in the current code. Policy-level rules this record adds beyond
> the epic are marked **Proposed** where they appear and listed under *Open
> items*. Sections marked "Proposed implementation design for #1534" (*The
> approvable operations* and *Approving* in part, *The approval lifecycle*,
> *Ordering inside operations*, *The request digest* and *Resolved state*,
> *The server-authored summary*, *The combined ceremony*, *Audit* and the
> registry-completeness test under *Equivalent-effect paths*) are
> design for the server and CLI sub-issue
> ([#1534](https://github.com/Hikyo-Org/Hikyo/issues/1534)); that sub-issue's
> review may revise them (Open item 29). Proposed until the owner accepts it;
> operative when the implementing pull requests (#1534 and the web sub-issue,
> [#1535](https://github.com/Hikyo-Org/Hikyo/issues/1535)) merge.
> The *Equivalent-effect paths* table is the initial seed of a per-operation
> registry declaration and may be incomplete: each review round so far has
> found more paths (Open item 3).
> Independent adversarial Standards and Spec review is required on the
> recording pull request.
>
> **Governance.** This record adds a conjunct to the formulas of the
> permission-model ADR ([#15](https://github.com/Hikyo-Org/Hikyo/issues/15))
> and a category to the catalogue of the audit-model ADR
> ([#24](https://github.com/Hikyo-Org/Hikyo/issues/24)). The
> [amendment procedure](../../GOVERNANCE.md#amendment-procedure) requires
> reopening those originating tickets, running the same adversarial
> cross-model review that locked them, and recording the amendment in each
> ADR. None of that has happened: #15 and #24 are closed, cross-model review
> has not run and is not CLEAN, and neither ADR carries a banner. The owner
> must either have the cross-model review run or waive it for this decision
> only, as earlier owner-stated waivers did. GOVERNANCE.md has no waiver
> clause, so a waiver departs from its procedure. See Open item 14.

## Context

Consequential actions are confirmed today by the client that performs them.
A script, or an AI agent running the CLI on a human's session, passes the same
confirmation as the human would. Nothing on the server can require that the
human who owns the session approved this specific action.

The confirmation inventory for the operations this ADR covers:

| Action | Confirmation today | Enforced by |
| --- | --- | --- |
| `rotate-token-key`, `rotate-master-key`, `rotate-scanning-key` | `--yes` | CLI only: the operations take no request body (`api/openapi.yaml`, `rotateTokenKey`, `rotateMasterKey`, `rotateScanningKey`) |
| `rotate-dek`, `rotate-root-key` | `--yes` | CLI only: `RotateDEKRequest` carries `scope`, `org`, `project`; `RotateRootKeyRequest` carries `phase`; neither carries a confirmation |
| `project delete` | `--confirm <project-name>` | CLI only: `deleteProject` has no body |
| `remote remove` | typed name at an interactive prompt | CLI only (`internal/cli/remotes.go`, `session.ConfirmName`); `removeRemote` has no body |
| `instance-config adopt` | `--yes`, and an exact single-decision reauthentication | `--yes` is CLI only. The server consumes a reauthentication bound to the preview token (`internal/service/self_config_provision.go`, `ConsumeSelfConfigReauth`) |
| `rotation run-now` | `--confirm-destructive`, and the publish ceremony | Server: a self-asserted boolean, `RunRotationNowRequest.confirm_destructive` (`ErrRotationRunNowUnconfirmed`), plus the publish ceremony over the environment and both keys (`requireRotationCeremony`), both in `internal/service/credential_rotation.go` |
| `instance-config credential-policy set` | `--confirm` | Server, as a self-asserted boolean: `SetCredentialPolicyRequest.confirm` acknowledges the enumerated affected credentials |
| `instance-config developer-credential-policy set` | none | Nothing: the CLI sends the policy (`internal/cli/developer_credentials.go`, `runDeveloperCredentialPolicy`) |
| `instance-config apply` | `--confirm-restored-credentials`, and an exact single-decision reauthentication | Server: the boolean `InstanceConfigApplyRequest.confirm_restored_credentials` (`api/openapi.yaml`, schema `InstanceConfigApplyRequest`), plus a reauthentication bound to revision, generation, `plan_digest` and that boolean (`internal/service/self_config_apply.go`, `ConsumeSelfConfigReauth`) |
| `values copy` into a protected environment | `--confirm-protected` | Server: a human takes the publish ceremony; a machine's `confirm_protected: true` suffices (`internal/service/values.go`, `authorizeDestination`) |
| `values publish` into a protected environment | `--confirm-protected <env,...>` | Server: a human takes the ceremony; a machine names the set in `confirmed_protected_environments` (`PublishRequest`) |
| `sa credential mint`, `rotate`, `revoke` | reauthentication on mint; none on revoke | Server, for the reauthentication; no per-action confirmation |

Every server-side confirmation above is either a value the caller sends or a
reauthentication ceremony. The ceremonies (protected publish and copy,
run-now, self-config adopt and apply, mint) prove presence. Most bind an
enumerated unit, not the request; only the self-config ones bind the request.
None is a server-recorded approval that a policy can require of any covered
operation, and machines skip all of them.

### Reauthentication is not approval

The reauthentication machinery already binds a passkey signature to one
decision, and this ADR reuses it, but it does not answer the question this
ADR asks.

- A reauthentication intent is a closed value naming purpose, operation,
  environments and key set (`internal/service/reauth_intent.go`,
  `ReauthIntent`, `reauthIntentDescriptors`). It binds the enumerated unit,
  not the request. The self-config intents are the exception: they bind the
  preview token, or the revision, generation, plan digest and
  restored-credential confirmation (`NewSelfConfigReauthIntent`,
  `internal/service/reauth_self_config.go`).
- Where the effective window is `0`, a passkey opens a single-decision window
  (`internal/service/webauthn.go`, `ReauthPasskeyFinish`: `single := effWin <= 0`).
  That window lives to the hard cap; the single-decision flag, not the
  clock, limits it to one decision (`internal/service/webauthn.go`). An
  exact-intent binding is single use and lives five minutes from the
  ceremony; the self-config and developer-credential bindings are the only
  two that `ReauthPasskeyFinish` passes to `bindExactIntent` (`bindExactIntent` sets `WindowExpiresAt = now +
  5m` and the hard cap to the same instant). Elsewhere the window slides, so
  one ceremony covers many actions.
- Machine identities skip the ceremony structurally: a caller without a
  session id never takes it (`internal/service/ceremony.go`, `skipsCeremony`).
- The CLI reaches the passkey through a browser handoff that the same
  principal must approve (`internal/service/cli_reauth.go`, `ApproveCLIReauth`
  refuses when `h.PrincipalID != caller.Principal`; `CLIReauthLifetime` is
  five minutes from the handoff's start).

So the parts exist: exact binding, single use, five-minute life, same-principal
approval across sessions, and a passkey ceremony bound to a server-side
binding. What is missing is a server-recorded approval of one exact request,
applicable to operations that are not disclosures and that have no
environment.

## Decision

The owner decided, on 2026-11-23 in [#1532](https://github.com/Hikyo-Org/Hikyo/issues/1532),
the nine points below. Each "Recorded rationale" and "Recorded reading" is
this record's reading; the epic does not state it.

1. **Approver: the same human.** The human who started the action proves
   presence. Four-eyes stays with change approvals (secret-change approvals,
   [#151](https://github.com/Hikyo-Org/Hikyo/issues/151)).
   *Recorded rationale:* the threat is a client acting on a human's session
   without the human, not one human acting without a second.
2. **Enforcement: server-side, for every client including the web.** A
   machine credential that calls a covered operation is refused with an error
   naming the policy, and the policy screen warns about it (for example,
   covering publish stops CI publishes to protected environments).
   *Recorded rationale:* a guard the client can skip is the guard the agent
   skips.
3. **Policy: an instance floor plus project additions.** Instance admins set
   the floor. Project admins may add operations or environments and never
   remove what the floor covers. The default is empty (opt-in). Loosening
   needs a passkey approval and is audited; tightening is audited only.
   *Recorded rationale:* an opt-in default changes nothing on upgrade, and the
   floor keeps a project admin from opting out of what the operator requires.
   *Recorded reading:* for "copy and publish to protected environments" the
   floor covers protected environments as a class, because an instance floor
   cannot name one project's environments; project additions name specific
   environments.
4. **Operations: an `approvable` flag on the authz operation registry.** The
   initial set: the five crypto rotations (DEK, master, root, token, scanning
   keys), `rotation run-now`, project delete, credential-policy and
   developer-credential-policy updates, self-config adopt and apply, copy and
   publish to protected environments, remote remove, and service-account
   credential mint, rotate and revoke.
   *Recorded rationale:* the registry is the single table every request is
   authorized through, so the flag cannot drift from the operation it
   describes.
5. **Binding: one exact action.** Operation, target ids and request digest;
   single use; five-minute expiry.
   *Recorded rationale:* the exact-intent passkey binding already works this
   way (`bindExactIntent`); a reusable approval would be a window by another
   name.
6. **Flow: refuse, approve, retry.** The request is refused with `conflict`
   and the exact detail `action-approval-required (apr_…)`. The server records
   a pending approval with a server-authored summary. The human approves
   through new endpoints: an inline authenticator code first, the browser
   approval page when a passkey is required (always at protected
   environments) or when no code is possible. The identical retry consumes
   the approval, matched by session, operation and digest. No new fields or
   headers on existing routes, so no freeze exception.
   *Recorded rationale:* existing clients already handle a `conflict` with a
   detail, and no frozen operation's shape has to change.
   *Recorded reading:* "authenticator code" means a possession-factor code
   (TOTP); approval never accepts a password or a recovery code, for the
   reasons the human-auth ADR gives for reauthentication.
7. **Display: the server's summary everywhere.** The CLI prints the server's
   summary before asking for a code; the browser page shows the same summary;
   an operation without a summary falls back to operation, target and digest.
   *Recorded rationale:* text the client composes is text the agent controls.
8. **Combined ceremony.** When an action also needs reauthentication, one
   passkey signature covers both.
   *Recorded rationale:* two prompts for one action train people to click
   through the second.
9. **Audit.** Requested, approved, consumed, expired and refused (including
   machine refusals) in the target scope's trail; policy changes in the
   instance trail.
   *Recorded rationale:* the people who audit a scope see the approvals for
   actions on it.

## The approvable operations

*The flag and the initial set are the owner's. Validation, the policy
writer's refusal and the mapping to registry rows are proposed
implementation design for #1534 (Open item 29).*

`opSpec` (`internal/authz/registry.go`) gains one boolean, `approvable`,
validated at registry construction like the other row fields. A policy may
name only operations whose row carries it; the policy writer refuses an entry
for any other operation, or naming the policy writes. The policy writes carry
it too, but
their coverage follows the loosening rule, not a policy entry (*Policy*,
Open item 4).

The initial set maps to these registry rows:

| Epic name | Registry operation(s) | Class | Machine-reachable today |
| --- | --- | --- | --- |
| DEK rotation | `crypto.rotate-dek` | instance | no (`human-session`) |
| master-key rotation | `crypto.rotate-master-key` | instance | no |
| root-key rotation | `crypto.rotate-root-key` (each phase) | instance | no |
| token-key rotation | `crypto.rotate-token-key` | instance | no |
| scanning-key rotation | `crypto.rotate-scanning-key` | instance | no |
| rotation run-now | `rotation.run_now` | tenant, environment | no: `RunNow` calls `requireRotationHuman` (`internal/service/credential_rotation.go`) and the spec admits only `human-session` |
| project delete | `project.delete` | tenant, project | no |
| credential-policy update | `identity.credential-policy-update` | instance | no |
| developer-credential-policy update | `developer-credential.policy-update` | instance | no |
| self-config adopt and apply | `self-config.adopt`, `self-config.apply` | instance; tenant at environment | no |
| publish to a protected environment | `value.publish` | tenant, environment | **yes** |
| copy to a protected environment | `value.copy-destination`, `value.copy-destination-config` | tenant, environment | **yes** |
| remote remove | `remote.remove` | instance | no |
| service-account credential mint, rotate, revoke | `identity.credential-mint`, `identity.credential-revoke` | tenant, project | no |

"Machine-reachable" reads `x-hikyo-artifacts` in `api/openapi.yaml` and the
machine allowlists in `internal/domain/permission.go`. Automation may hold
`read`, `edit`, `publish`, `definitions-edit`, `issue-certificate`,
`crypto-use` and `audit-read` (`machineAllowlists`), and `reveal` only by the
per-project opt-in (`machineRevealByOptIn`). Credential rotation is not its
own operation: it is a mint plus a revoke of the predecessor
(`internal/service/identities.go`, `MintCredential`).

**The machine consequence, stated once.** A policy that covers publish or
copy at protected environments stops every automation credential from writing
there: a CI publish (`publish`), a CI copy of config material (only
`publish@destination` is needed), and a copy of secrets under the
machine-reveal opt-in (`reveal` on both legs), each also holding the source
read (`value.list`) that copy authorizes first. Today such a machine confirms
by sending `confirmed_protected_environments` or `confirm_protected`, the
"explicit field in the immutable plan" path in `authorizeDestination`.
Covering `rotation.run_now` stops no machine, because run-now is already
human-only.

## The approval lifecycle

*Proposed implementation design for #1534 (Open item 29): the record, its
states and transitions. The rules marked Proposed with their own Open item
are policy-level.*

An action approval is a server-side record:

- `id` (`apr_…`; the prefix is free: #151 uses `apol`, `avote`, `aapr`,
  `abyp`, `areq` in `internal/service/approvals.go`);
- the requesting session and principal;
- operation, target id chain and request digest;
- the resolved state at refusal (see *Resolved state*);
- the reauthentication intent(s) the action will also consume (see *The
  combined ceremony*);
- the server-authored summary and the complete affected list (see *The
  server-authored summary*);
- the proof it requires (code or passkey, or passkey only);
- state, `requested_at`, `expires_at`, and on transition the approver's
  factor class and timestamps.

States:

| State | Entered when | Leaves to |
| --- | --- | --- |
| `pending` | A human session calls a covered operation with no matching approved record. The request is refused and the record is committed before the response (see *Committing the pending record*). | `approved`, `expired`, and `refused` (if Open item 11) |
| `approved` | The requesting principal presents an accepted proof for this id. A compare-and-set moves `pending` to `approved` in the same transaction that spends the proof. | `consumed`, `expired` |
| `consumed` | The identical retry from the requesting session matches operation, digest and resolved state (Open item 25). A compare-and-set moves `approved` to `consumed` inside the operation's own transaction (Open item 20). | terminal |
| `expired` | `expires_at` passes before consumption. | terminal |
| `refused` | The human declines the pending approval (Open item 11). | terminal |

**Committing the pending record.** The refusal is often raised inside the
operation's open write transaction, and SQLite has a single writer, so the
record is never written in a transaction nested inside it. Two mechanisms
exist in the code:

- Where the check runs before the transaction has written anything, the
  transaction writes the pending record, returns without error so it
  commits, and the refusal is surfaced after it returns. Publish already does
  this for a #151 invalidation ("The gate committed an invalidation; commit
  the transaction so it persists, then surface the refusal after the tx
  returns", `internal/service/publish.go`).
- Otherwise the refusal is captured, the transaction package rolls the
  attempt back, and the record is then written in its own small transaction
  before the error returns. That is how a captured authorization denial is
  flushed (`internal/authz/denial.go`; audit-model ADR § Denials).

Which mechanism each operation uses is part of Open item 29.

Rules:

- **Five minutes.** Proposed: `expires_at` is `requested_at + 5m` (the
  `CLIReauthLifetime` precedent), and approving does not extend it. The
  alternative is `approved_at + 5m` (the `bindExactIntent` precedent). With the
  request anchor, a human who spends four minutes on the page leaves the CLI
  one minute to retry. Open item 18.
- **Single use.** Consumption is a compare-and-set, so two identical
  concurrent retries cannot both consume it. Proposed (Open item 28): the
  loser is refused again and gets a new pending approval.
- **Same human, any of their sessions.** The approving request must
  authenticate as the requesting principal; the approval stays bound to the
  requesting session. This is the `ApproveCLIReauth` rule: a CLI request can
  be approved on the browser page of the same person.
- **Authorization first.** Proposed (Open item 28): the approval check runs
  after authorization and request validation, before any durable side
  effect of the covered action. Two existing writes run earlier and stay
  there: self-config adopt's status-read audit event, and a publish's
  invalidation of a stale #151 request; see *Ordering inside operations*. A caller who may not perform the operation gets the operation's
  ordinary refusal and no pending record, so the approval refusal is never
  an existence oracle. This follows `ErrReauthRequired` and
  `ErrProtectedDestination` (`internal/service/grants.go`,
  `internal/service/values.go`): authorization succeeded, the state refuses.
  Where each operation places it is under *Ordering inside operations*.
- **The check is a conjunct, not a substitute.** The retry is authorized from
  scratch. A grant revoked between approval and retry refuses the retry.
- **Records are private to the requester.** Proposed (Open item 28): a
  principal other than the requester who reads or approves an id gets
  `not_found`. The requester can read and approve it from any of their
  sessions.

**Granularity.** Proposed (Open item 19): one approval per request, bound to
the route's operation (`x-hikyo-operation`; `copyValues` is
`value.copy-destination`), with every leg and environment the request reaches
in `targets`, required when any of them is covered; the summary names every
covered destination. This covers a mixed copy (both destination legs in
`authorizeDestination`), `--to` with several destinations, and publish's
`publish@each-affected-environment`. The resulting counts: a credential
rotation takes two approvals (mint, then revoke), and a mint can succeed
while its revoke waits, leaving two live credentials, the direction
`MintCredential` calls safe; a root-key rotation takes three, one per phase.

**Machine refusal.** The epic requires an error naming the policy. Proposed
(Open item 1): a machine principal calling a covered operation creates no
record, because it cannot approve, so the refusal is final and only audited.
It is refused with `conflict` and an exact detail naming the policy source
and entry, for example
`action-approval-machine-refused (floor: value.publish at protected environments)`
or `action-approval-machine-refused (project addition: value.publish at env_…)`.
The operation is recorded in the audit payload. Local host authority is also
session-less; whether it is refused like a machine is Open item 7.

## Ordering inside operations

*Proposed implementation design for #1534 (Open item 29), except the copy
reordering, which is policy-level (Open items 21, 25 and 28).*

The approval check sits where each operation already authorizes before
durable work. The refusal ahead of a ceremony's refusal is the order *The
combined ceremony* needs.

**Key rotations** (`internal/service/rotation.go`,
`internal/service/revisions.go`). No new authorizing read is added, so no
rotation's error order changes:

- DEK: in the authorizing read transaction `RotateDEK` runs before its
  prepare, because the project prepare may durably create the project's
  initial key (`internal/crypto/keyring.go`, `projectDEKSet`).
- Root: in the entry authorization (`RotateRootKey`: a read transaction
  without a budget, the budget charge with one), ahead of `stageCustody`,
  which durably re-seals upgrade custody under the new root before
  `rootRotationTx` runs (Open item 27).
- Master: in the budget charge at entry (`chargeDefaultAtEntry`,
  `internal/service/budget.go`) when a budget is configured; otherwise in the
  write transaction.
- Token and scanning: in the write transaction. They are budget-exempt and
  authorize only there.

Where the check runs in the write transaction, the prepare has already run.
The master, token and scanning prepares are undone by `abort` only when the
transaction does not commit: once `tx.Write` returns nil, each operation
adopts the new key (`adopt()` after the transaction in `RotateMasterKey`,
`internal/service/rotation.go`, and in `RotateTokenKey` and
`RotateScanningKey`, `internal/service/revisions.go`). So these three must
record their pending approval with the rollback mechanism under *Committing
the pending record*, never the commit mechanism; a refusal then leaves no key
behind. The DEK check, the root check and the master check with a budget run
in read transactions (the budget charge authorizes in `tx.Read` and its
counters are in memory, `internal/service/budget.go`), which cannot write
the pending record, so they also capture the refusal and write the record in
its own small transaction. Consumption is in each operation's write
transaction.

**Self-config.** Adopt (`internal/service/self_config_provision.go`) takes
the preview token from a separate `PreviewAdoption` call. Its first
transaction authorizes `self-config.preview`, answers an idempotent replay of a completed
adoption with the current status, and writes a status-read audit event; its
final transaction authorizes `self-config.adopt` and consumes the exact reauthentication.
The check sits in that final transaction, before the reauthentication.

Apply (`internal/service/self_config_apply.go`, `Apply`) does work before its
final transaction. A first transaction authorizes, and an idempotency lookup
returns an existing job for a repeated key without looking at
`prepare_only` or `plan_digest`; otherwise it writes a job row (`BeginJob`)
and a `self_config.apply_requested` audit event. Apply then waits for node
preparation, and a prepare-only request returns after that wait. A full
apply runs `prepareDeploymentCommit` outside the final transaction, which
authorizes again and consumes the exact reauthentication. For a request with
`prepare_only` false, the check sits in the first transaction, after the
idempotency lookup, and runs only when no job exists for the key (before
`BeginJob`) or the existing job is still `preparing`. For a new job it
sits after the snapshot read, which refuses a collected revision
(`collectedRevisionError`). For a new job, the
restored-credentials validation (the binding's `Suspended` flag against
`confirm_restored_credentials`, which refuses with `invalidDetail`) runs
today after `BeginJob` in the same transaction; it only reads, so #1534
evaluates it before the check. A repeated key whose job has left `preparing` returns the
job's status and performs nothing (`job.Status != "preparing"` in `Apply`),
so a check there would leave a pending approval no retry consumes; adopt
answers its completed replay the same way, so both leave a finished
request unchecked. A full apply that reuses a prepare-only job's key finds
that job still `preparing` and is checked. Consumption is in the final
transaction with the reauthentication. Prepare-only requests are Open item
16.

Restore (`restore_deployment: true` on the same route) does not reach
`Apply`: the server routes it to `RestoreDeployment`
(`internal/server/self_config.go`;
`internal/service/self_config_deployment_restore.go`), which has its own
transactions. A first transaction authorizes `self-config.apply`, and a
repeated restore returns the current status and performs nothing. Otherwise
it reads the next rollout sequence, the decision command is signed outside
the transactions (`DecisionCommand`), and a final transaction authorizes
again and consumes the exact reauthentication (`ConsumeSelfConfigReauth`).
Its exact reauthentication binds the restore action, revision, generation,
schema version and plan digest (`NewSelfConfigReauthIntent` with
`rollout-restore`). The check sits in the first transaction, after the repeat
check and the external-phase conflict, before the rollout sequence is read;
consumption is in the final transaction. The deployment checks that follow
the first transaction today (`deploymentMatches` and the enrollment id
against the node's deployment identity, refusing with
`configrollout.ErrUnsupported`) read only the binding, the rollout row and
the node's identity, so #1534 evaluates them before the check.

**Publish** (`internal/service/publish.go`). Before the check, two earlier
steps can write or refuse, and both stay ahead of it:

- Selection and key-group closure (`resolveVersions`, `selectVersions`).
  For a #151 merge whose pinned drafts went stale, the request is invalidated
  (`commitInvalidation`, `draft_edited`) and committed, and the publish is
  refused. That is an accepted earlier write: it records that a #151 request
  can no longer merge, and publishes nothing.
- The restore preview-token check, which refuses a stale token
  (`ErrStalePreview`) before `approvalGate` can persist a #151 request.

The check sits after both, since it needs the closed set for the resolved
state, and before `approvalGate` (`internal/service/approvals.go`). That gate
can commit a staged #151 request before the protected ceremony. So a covered
publish that would stage is checked too, unless Open item 23 decides staging
needs no action approval. In that case the check moves to every path that
does not stage: before `approvalMergeGate`, before `approvalBypassGate`,
and, where no #151 policy covers the environment, after `approvalGate`
returns its empty outcome and before the protected gate. The merge and
bypass gates can commit invalidations of the #151 request, and the bypass
gate takes a ceremony (`requireCeremony`) before the protected gate, so that
placement needs `approvalGate` split in two.

Consumption sits later than the check. `Revisions.PublishPlanned` commits
its transaction when `approvalGate` returns a refusal or a staged #151
request (its `approval.refusal` and `approval.created` returns), and a
refusal carries invalidations already committed (`commitInvalidation`) by
`approvalGate`'s prologue, for a live request whose policy is gone, by
`approvalMergeGate` or by `approvalBypassGate`. The approval is therefore
consumed only after both returns, beside the protected ceremony, so a
publish that ends in either return never spends it (Open item 20).

**Rotation runs through publish.** *Proposed implementation design
for #1534.* The rotation executor publishes through the same function
(`e.Revisions.PublishPlanned` with `PublishRequest{rotation: hooks}`,
`internal/service/rotation_executor.go`), as `LocalPrincipal(authority)`, a
session-less principal (`internal/service/service.go`).
`requireUnattendedEnvironment` refuses protected and #151-covered
environments at create, resume and release-hold, and also in the executor's
pre-run gate, before any mint (`gateWithin`, `internal/service/rotation_executor.go`,
calls `requireRotationStartable`, `internal/service/credential_rotation.go`).
So #151 normally stops a run at that gate with `ErrRotationApprovalPolicy`.
Inside `PublishPlanned`, a #151 refusal or staged request is only the
backstop, reached after a mint: it returns `errRotationNotPublished`, and
the executor rolls the minted credential back (`rollbackAfterPublishError`).
Both halt the run (`rotationGateReason`, `rotHaltGate`). The gate does not
refuse an environment only a project addition covers, so a scheduled run
there would reach the check, which therefore tests
`request.rotation != nil` explicitly. Under Open item 22's proposed reading
it skips such a run, since no session exists to approve one. The
alternative extends `requireUnattendedEnvironment` to environments a
project addition covers, so the gate halts the run before any mint, and the
check in `PublishPlanned` returns `errRotationNotPublished` without
recording a pending approval, as the backstop for a policy that changes
between the gate and the publish. Open item 22 decides which.

One request validation runs after `approvalGate` today: a human request
must leave `confirmed_protected_environments` empty, or it is refused with
`invalidDetail`. It reads only the request, so #1534 evaluates it before the
check.

**Copy** (`internal/service/values.go`). Today `Copy` authorizes the source
read at entry (`chargeDefaultAtEntry` with `value.list`), resolves the named
keys to metadata (`resolveCopySourcePlan`), and then, per destination,
`authorizeDestination` interleaves the legs, the #151 `CoveringPolicy`
lookup and the protected gate. After that loop come the per-destination
template check (`copySourceTemplateKey`, refusing with `invalidDetail`) and
`openCopySourcePlan`, which calls `loadCopySourcePlan` to authorize
`value.copy-source` and read the source entries, then takes the source
ceremony. Authorization first (Open item 28), one approval per request (Open
item 19) and source entry ids at refusal (Open item 25) need a covered copy
to run:

1. every destination's legs (with the #151 lookup) authorized and, when
   secrets are carried, `value.copy-source`, with the source entries loaded
   by `loadCopySourcePlan`, which neither decrypts nor records a disclosure;
2. request validation, including the per-destination template check;
3. the approval check;
4. the protected gates, then the source ceremony.

That changes the error order of a covered copy: a caller carrying secrets
without `value.copy-source` gets the source refusal before any protected gate; an
absent source value returns `not_found` before the protected gate; and a
caller refused at destination 2 no longer sees destination 1's protected
refusal first. Proposed: the reordering applies only when the effective
policy covers the copy, so an uncovered copy keeps today's order (Open item
21). The coverage read runs after the source-read authorization
(`value.list`) in `resolveCopySourcePlan`, which runs with or without a
budget, so only a caller who may list the source environment can see which
order applies. A caller who may list the source but fails a destination leg
or `value.copy-source` never reaches an authorized attempt, yet can tell from
the error order whether the policy covers the copy (Open item 28). The
alternative is to authorize every leg first for every copy, covered or not,
and read coverage only after that step (Open item 21).

**The other covered operations.** Each runs in one write transaction that
authorizes first:

- Run-now (`RunNow`, `internal/service/credential_rotation.go`): after the
  in-flight run check, before `requireRotationCeremony`.
- Project delete (`Projects.Delete`, `internal/service/hierarchy.go`): after
  authorization, before `releaseProjectRules` and the delete.
- Remote remove (`RemoveRemote`, `internal/service/remotes.go`): after the
  remote is resolved by name, which yields its id for `targets`, before the
  delete.
- Credential-policy update (`Identities.SetPolicy`,
  `internal/service/identities.go`): after the affected-credential
  enumeration and the unconfirmed-preview return, which changes no policy,
  before `SetCredentialPolicy`.
- Developer-credential policy update (`DeveloperCredentials.SetPolicy`,
  `internal/service/developer_credentials.go`): after authorization. Its
  clamps are written inside the enumeration loop, so the clamp set is
  computed before the first `ClampDeveloperCredentialExpiry`, or the refusal
  uses the rollback mechanism under *Committing the pending record*.
- Mint (`MintCredential`, `internal/service/identities.go`): after
  `postStateReach`, which yields the resolved state, before
  `RequireDisclosureAuthority`.
- Revoke (`RevokeCredential`, `internal/service/identities.go`): after the
  service account is resolved, before the revoke.

## The request digest

*Proposed implementation design for #1534 (Open item 29).*

The digest binds the approval to one exact request. It is SHA-256 over a
stable JSON preimage built with the encoder the passkey bindings already use
(`internal/service/webauthn.go`, `operationBinding`, `jsonwire.Stable`):

```json
{"v":1,"operation":"<registry operation>","targets":{...immutable ids...},"request":{...}}
```

- **Targets are immutable ids resolved at authorization time**, never path
  segments or names as typed: the resolved org, project, environment, key,
  service-account, credential, rotation or remote id.
- **`request` is a per-operation canonical struct**, not the generated
  request type. `jsonwire.Stable` honours `omitempty` and `omitzero`, so a
  generated type can drop a member whose zero value matters. Each canonical
  struct names every member explicitly, after defaults are applied, and a
  golden test pins its bytes.
- **Sets are sorted and de-duplicated.** `CanonicalEnvironmentSet` does both;
  `CanonicalKeySet` only sorts (`internal/service/reauth.go`), so the
  canonical struct de-duplicates key sets itself.
- **Names resolve to ids in `targets`.** Copy names keys. The key ids they
  resolve to go into `targets`, so a rename swap that makes the same name
  address a different key changes the digest; the names stay in `request` as
  sent.
- **No member is excluded.** `idempotency_key` on self-config adopt and apply
  is hashed: an identical retry repeats it anyway.

Per operation:

| Operation | Targets | Request members |
| --- | --- | --- |
| `crypto.rotate-token-key`, `crypto.rotate-master-key`, `crypto.rotate-scanning-key` | instance | none (no body) |
| `crypto.rotate-dek` | instance, or org and project ids | `scope`, `org`, `project` |
| `crypto.rotate-root-key` | instance | `phase` |
| `rotation.run_now` | org, project, environment, rotation | `confirm_destructive`, `expected_generation`, `skip_overlap` |
| `project.delete` | org, project | none (no body) |
| `identity.credential-policy-update`, `developer-credential.policy-update` | instance | the full replacement body |
| `self-config.adopt` | instance | `preview_token`, `idempotency_key` |
| `self-config.apply` | instance and the environment chain | every `InstanceConfigApplyRequest` member, including `plan_digest`, `prepare_only`, `confirm_restored_credentials`, `restore_deployment`, `idempotency_key` |
| `value.publish` | org, project, the addressed environment and every affected environment | `version_ids`, `preview_token`, `confirmed_protected_environments`, `approval_request_id`, `bypass`, `purpose` |
| `value.copy-destination` | org, project, source and destination environments, the resolved key ids | `source_environment_id`, `keys`, `destination_environment_ids`, `confirm_protected` |
| `remote.remove` | instance, remote | none (no body) |
| `identity.credential-mint` | org, project, service account | every `MintCredentialRequest` member |
| `identity.credential-revoke` | org, project, service account, credential | none (no body) |
| floor policy write (new operation) | instance | the full replacement body |
| project-addition policy write (new operation) | org, project | the full replacement body |

### Resolved state

Proposed (Open item 25). The digest binds the request, but some requests mean
different things at different times:

- **Publish.** Key-group closure pulls the owner's other pending changes in
  the same group and environment into the publish, to a fixed point
  (`internal/service/publish.go`, `selectVersions`). A change staged after
  the approval can join the closed set.
- **Copy.** The request names keys, not value versions, so the source values
  can change between approval and retry. `ValueEntry.ID` is immutable and a
  rewrite mints a new one (`internal/store/values.go`), so the entry ids
  serve as the source versions.
- **Run-now.** An omitted `expected_generation` means the current generation
  (`RunRotationNowRequest`).
- **Mint.** The environments a new credential can reach, current and
  historical, are computed from live state after authorization
  (`internal/service/identities.go`, `postStateReach`), and a grant or
  environment added between approval and retry widens them. Its ceremonies
  range over both sets (`RequireDisclosureAuthority` in
  `internal/service/grants.go` takes one per environment in their union).
- **Credential-policy update.** The server enumerates the live credentials
  the change affects (`internal/service/identities.go`, `SetPolicy`,
  `Affected`); a credential minted between approval and retry joins that set.
- **Developer-credential policy update.** The update clamps the expiry of
  every live developer credential the new ceiling shortens
  (`internal/service/developer_credentials.go`,
  `ClampDeveloperCredentialExpiry`); a credential issued between approval and
  retry joins that clamp set.
- **Policy writes.** The digest binds the full replacement body, but what it
  removes depends on the policy current at retry: an entry another admin adds
  between approval and retry would be removed by the same body. The prior
  effective coverage is bound as resolved state.

At refusal, the pending record stores the resolved state: the closed version
set for publish, the source entry id of each copied key, the concrete
generation for run-now, the reachable current and historical environment
sets for mint, the affected-credential set for a credential-policy update,
the clamp set for a developer-credential policy update, and the prior
coverage for a policy write. Consumption
recomputes it inside the operation's transaction and refuses on any mismatch
with a new pending approval. This is server-side only, so nothing changes on
the wire. Capturing the copy entry ids needs the copy reordering under
*Ordering inside operations*.

**Why the retry must be identical.** The human approves the summary and
complete affected list of one action. If a changed retry, or an unchanged retry against changed state,
could consume the approval, an agent could obtain approval for a harmless
action and spend it on a harmful one with the same operation and target. The
digest binds the request and its resolved ids, and the resolved state binds
the state listed above. State outside that list is not bound by the
approval; the retry re-runs authorization and the operation's own
preconditions against it.

## The server-authored summary

*Proposed implementation design for #1534 (Open item 29), except the
rendering rule (Open item 24).*

The server writes the summary when it records the pending approval, from the
resolved targets, the validated request and the resolved state. Clients render
it; none composes its own. It counts what will happen, including the closed
set: for example "Publish 5 changes (3 selected, 2 added by key-group
closure) to production (env_…) in project payments (prj_…)", or "Rotate the
instance master key". Proposed (Open item 29): next to each count, the
summary lists what it counts. Publish and copy list the affected key names
with their ids; a mint lists the service account and both the current and
the historical environment ids the credential can reach (*Resolved state*);
a revoke names the credential id. Each list in the
summary is capped, with "and N more" for the rest, so the summary is only a
preview. A summary never contains secret material. An operation with no
summary template gets the generic form: operation, target ids and the full
digest. Its target ids are not capped, so they are already the whole list
and do not appear again in the complete affected list.

**The complete affected list.** Proposed (Open item 29). A capped preview
alone would let an agent order a copy or publish so that a sensitive key
falls after the shown names, where "and N more" hides it. So the server
also stores, with every pending approval, the complete affected list: every
entry each capped list counts, each id quoted with its name as of refusal.
The server generates it from the same stored inputs as the summary, not
from a separate query: the digest (the resolved ids in `targets` and the
canonical request, for example publish's `version_ids`, self-config's
`plan_digest` or `preview_token`, or a policy write's replacement body) and
the stored resolved state (for example publish's closed set). It serves
through the approval read (*Approving*). The approval binds the ids, through
the digest and the resolved-state comparison at consumption (*Resolved
state*); the names are as of refusal and are display only.

Every surface that accepts a proof shows the complete list before it takes
one: the browser approval page, the CLI, the inline ceremony in the web
sub-issue's existing dialogs (#1535), and any approve-by-id command. A
surface that reaches an approval later, for example after a non-interactive
CLI printed only the summary and approval id (Open item 10), shows the
complete list then.

- The browser approval page shows the complete list in full, never
  truncated, next to the approve control. Proposed (Open item 10): where a
  passkey is not required, the page also accepts the authenticator code, so
  an account whose only possession factor is TOTP can approve a list the
  CLI will not print inline.
- The CLI prints the complete list in full before the code prompt. Proposed
  (Open item 10), as a reading of Decision 6's "when no code is possible":
  where the CLI will not print the list inline (a list longer than a bound
  it sets, or output it cannot show in full), it takes no code and sends
  the human to the browser approval page, as when a passkey is required.

This holds only where the server or an unmodified client controls the
display. The server cannot know what a client showed. The browser page
shows what the server sends, and an unmodified CLI prints the complete
list, but an agent that controls the human's channel can relay only the
preview, or anything else, and an inline code does not bind to the action
(*Approving*, what an inline code proves; Open item 12).

Every rendering rule for the summary (Open item 24) applies to the complete
list too.

Names are user-controlled text, so the summary shows them quoted next to their
immutable ids. Entity names refuse control characters and the bidi
embedding, override and isolate characters U+202A to U+202E and U+2066 to
U+2069 (`internal/service/hierarchy.go`, `checkName` and `hasDirectionControl`),
but still admit LRM, RLM and ALM and other Unicode format characters such as
zero-width ones. Folder path segments refuse only control characters
(`checkText`). Service-account names skip `checkText` entirely: they apply
only `hasDirectionControl` and a 64-byte bound
(`internal/service/identities.go`, `createServiceAccount`). The service
applies no control-character, UTF-8 or whitespace check to them, so C0 and
C1 controls, including ESC, are not refused there. Proposed (Open item 24): the CLI and the browser page render
control characters, bidi and other format characters in the summary as
visible escapes, and render the summary as plain text.

## Approving

*Inline code first, the browser page, and a passkey at protected
environments and for loosening are the owner's. The endpoint set and the
code replay rule are proposed implementation design for issue #1534 (Open
item 29).*

Per the decision, the human approves through new endpoints; the existing
routes do not change. The minimum set is: read one approval (state, summary,
complete affected list, accepted proofs); approve with an authenticator
code; begin and finish a passkey assertion for one approval; and the browser
approval page. A decline
endpoint is added only if Open item 11 adopts decline. The paths belong to
the server sub-issue (Open item 10). What the page offers an account with no
possession factor is Open item 6.

- **Inline code first** (owner decision). Where the account has a confirmed
  authenticator and a passkey is not required, the CLI prints the summary and
  the complete affected list and asks for a code. Proposed (Open item 10),
  as a reading of Decision 6's "when no code is possible": a list the CLI
  will not print inline sends the human to the browser page instead, which
  also accepts the code where a passkey is not required (*The complete
  affected list*). The code is spent with the existing replay rule
  (`ConsumeReauthEvidence`, `AdvanceTOTPStep` in `internal/service/reauth.go`).
- **What an inline code proves, plainly.** A TOTP code does not bind to the
  action: only WebAuthn can bind its challenge to a decision (human-auth ADR
  § Reauthentication, "TOTP cannot"). The code is typed into the same CLI or
  chat channel the agent controls, and the agent can edit the CLI's config.
  An agent that holds the seed, or that relays the prompt to the human and
  the code back, can approve its own request. The inline code therefore
  resists a script that has no access to the human, not an agent that
  controls the human's channel.
- **The browser page with a passkey resists that agent only while the agent
  holds no passkey of its own.** The assertion is bound to the relying-party
  origin and to this approval, and it needs an enrolled authenticator and user
  verification. But enrolling a passkey takes one existing proof: the TOTP
  code where the account has one, otherwise the password
  (`internal/service/webauthn.go`, `EnrolPasskeyStart` via
  `verifyLocalCreationProof`; proof selection in `internal/service/reauth.go`).
  The relying party prefers no attestation (`internal/webauthnrp/webauthnrp.go`,
  `PreferNoAttestation`), so a software authenticator enrols, and the enrol
  routes admit a human session, so the agent can call them through the CLI's
  session. An agent that relays one code therefore gets a passkey of its own
  and passes every later passkey approval. Enrolment deletes the principal's
  other sessions (human-auth ADR § Account-security mutations), which the
  human may notice, but it does not block the enrolment. See Open item 26.
- **Enrolment with no proof.** An account with no local proof whose session
  holds a live credential-establishment stamp may enrol with no proof at all
  (`internal/service/credential_establish.go`, `verifyLocalCreationProof`
  and `checkEstablish`). The stamp is written only when a federated
  establish flow completes, on the session that started it
  (`completeFederatedEstablish`; the OIDC callback carries
  `txn.InitiatingSessionID`, `internal/service/oidc_flow.go`), and lives
  five minutes (`CredentialEstablishLifetime`). The establish transaction
  is bound to the initiating session (`checkBinding`, `bindingSession`), and
  the start route `oidcStart` admits `human-session`, so a CLI session can
  start one. Its callback succeeds on the CLI session only if the agent
  obtains the IdP redirect's code and state, after the human completes the
  IdP login: the same relay as a TOTP code. Only the OIDC and OAuth2
  callbacks write the stamp (the callers of `completeFederatedEstablish`,
  `internal/service/oidc_flow.go` and `internal/service/oauth2_flow.go`),
  and `instance-config` cannot repoint either provider at another IdP. The
  issuer is immutable after create (`ErrIssuerImmutable`, refused in
  `internal/service/oidc.go` and `internal/service/oauth2_providers.go`;
  `putOidcProvider` and `putOauth2Provider` in
  `api/openapi.yaml`), and discovery must return the same issuer. A
  client-id change is refused for a pairwise-subject provider while links
  exist (`ErrPairwiseClientID`). Links are keyed by kind, issuer and
  subject (`ExternalIdentityByKey`; the `external_identities` unique key),
  so a provider deleted and re-created under another issuer reaches no
  existing account. So the route exists, gated on a relayed IdP login (Open
  items 6 and 26). SAML differs: `saml-provider.put` and
  `saml-provider.refresh-metadata`, under `instance-config` with no
  reauthentication, accept new IdP signing certificates whose fingerprints
  the caller itself confirms (`assessSAMLMetadata`,
  `internal/service/saml_providers.go`). A holder of the new key can then
  log in through that provider as any linked account (`SAMLACS`,
  `internal/service/saml.go`), though a SAML login writes no stamp. See
  *Equivalent-effect paths*.
- **Where a passkey is required.** A passkey is always required when
  the covered operation targets a protected environment and for every
  loosening. Whether a policy entry may require it elsewhere is Open item 12.
  At protected environments this adds no lockout: their window is capped at
  `0` (`ProtectedWindowCap`, `internal/service/settings.go`), and an effective
  `0` window already needs WebAuthn.
- **Never a password, never a recovery code:** this record's reading of
  Decision 6 (see there).
- **Proposed:** the CLI opens the page at its configured server origin and the
  approval id, never at a URL taken from a response body (Open item 10).

## The combined ceremony

*Proposed implementation design for #1534 (Open item 29).*

When the covered action also needs reauthentication, one passkey signature
covers both (owner decision). Steps:

1. The approval refusal is returned before the reauthentication refusal.
2. The pending record carries the reauthentication intent(s) the action will
   consume (`ReauthIntent`).
3. The assertion's challenge binds those intents' binding
   (`reauthIntentBinding.challengeBinding`) together with the approval id,
   digest and resolved state.
4. Finishing it opens the bound single-decision window(s) and moves the
   approval to `approved` in one transaction. The windows open on the
   requesting session, as a CLI handoff's do: the browser approves
   (`ApproveCLIReauth`) and the windows open on the handoff's CLI session
   (`internal/service/cli_reauth.go`). A window opened this way is bound to
   the approval id and is consumable only together with that approval. The retry consumes both in the operation's transaction.

Two code facts constrain this. Ceremony consumption compares the stored
binding with the intent's binding byte for byte, in both places a
single-decision window is spent (`internal/service/reauth.go` and the CLI
handoff approval in `internal/service/cli_reauth.go`, each refusing when
`ceremony.OperationBinding != binding.challengeBinding`), so a challenge that also binds the
approval needs that comparison changed. And a ceremony row carries one
`EnvironmentID` and one `OperationBinding` (`internal/store/authn/webauthn.go`,
`WebAuthnCeremony`; read in `internal/service/webauthn.go`). Adapter intents
already bind an environment set while the row keeps one `EnvironmentID`
(`internal/service/webauthn.go`, `adapterOperationBinding`), which is the
precedent for one signature over several environments.

The reauthentication an action takes today:

- a publish into several protected environments takes one ceremony per
  environment (`internal/service/publish.go`, a `NewPublishReauthIntent` per
  protected environment);
- a copy into N protected destinations takes one per destination: a
  disclosure intent names exactly one environment
  (`internal/service/reauth_intent.go`) and `authorizeDestination` runs the
  protected gate once per destination (`internal/service/values.go`);
- a copy that carries secrets also takes the source reveal ceremony
  (`openCopySourcePlan` with `disclosureIntent(PurposeCopy, source)`);
- run-now takes the publish ceremony over the environment and both keys
  (`requireRotationCeremony`, `internal/service/credential_rotation.go`);
- self-config adopt, apply and restore take an exact single-decision window
  bound to the request (`ConsumeSelfConfigReauth`, `internal/service/reauth_self_config.go`;
  `bindExactIntent`), consumed through the same byte-equal comparison;
- a mint takes one per current and historical environment the credential
  can reach (`RequireDisclosureAuthority`, `internal/service/grants.go`);
- a #151 emergency bypass consumes a bypass decision over the environment
  and key set (`approvalBypassGate`, `NewBypassReauthIntent`,
  `internal/service/approvals.go`), after which the publish skips its
  protected ceremony (`internal/service/publish.go`).

How one signature carries all of them is the mechanics question in Open
item 19 (Decision 8). Where
both halves accept a TOTP code instead, whether one code covers both is Open
item 15.

## Policy

A policy entry names an approvable operation, and for environment-level
operations the environments it covers.

- **Instance floor.** Set by instance admins (owner decision; the capability
  that makes someone an instance admin is Open item 8) and stored at instance
  scope (where exactly is Open item 5). For publish and copy it covers
  protected environments (Decision 3, recorded reading); any other selector
  is Open item 9.
- **Project additions.** Set by project admins (owner decision; the
  capability is Open item 8), from the project's settings screen; the API is
  the new policy endpoints (Open item 10), not new members on existing
  project routes. They add operations and
  environments for their project and cannot remove what the floor covers:
  the effective policy for a project is the floor's union with its
  additions. A project addition meaningfully adds tenant-class operations.
  Most instance-class operations, such as the master, root, token and
  scanning rotations and remote remove, have no project scope to cover. The
  exception is `crypto.rotate-dek`: instance-class in the registry, but a
  request with `scope`, `org` and `project` rotates one project's key
  (`RotateDEKRequest`; `RotateDEK` authorizes it at that project). Whether a
  project addition may cover project-scoped DEK rotation is part of Open
  item 8.
- **Default empty.** A fresh or upgraded instance covers nothing.
- **Loosening rule** (owner decision). Loosening needs a passkey approval and
  is audited; tightening is audited only. Loosening is refused with the same
  `action-approval-required` flow, and an inline code is not accepted.
  Proposed: a change is loosening when the new effective coverage does not
  contain the old, so a change that adds and removes is loosening (Open
  item 4). Proposed (Open item 4): the floor and project-addition writes are
  new registry rows that carry `approvable`, so the record, digest and
  ceremony apply to them, but a policy does not choose their coverage: every
  loosening write is covered and no tightening write is, and a policy entry
  naming them is refused.
- **Indirect loosening.** Other writes reduce effective coverage without
  touching the policy: clearing the protected flag; deleting an environment,
  through `environment.delete` or `definitions.apply` (which holds
  `StoreEnvironmentsDelete`), both machine-admitted (`environment.delete`
  admits `machine-credential` and needs `definitions-edit`, which automation
  may hold); and rewriting the floor through self-config apply or restore if
  the floor lives where those write. Open item 4.
- **Warning.** The epic requires the policy screen to warn about machine
  refusals. Proposed (Open item 28): it lists the machine principals that
  hold a capability the new coverage would refuse, before the change is
  saved.

## Audit

*Proposed implementation design for #1534 (Open item 29): payloads and the
sweeper. The events and trails are the owner's; the category name is Open
item 2.*

The `approval.*` category belongs to #151 and already holds
`approval.requested`, `approval.expired` and `approval.policy_changed`
(`internal/audit`). Action approvals therefore take their own category,
proposed as `action_approval`:

| Event | Trail | Payload |
| --- | --- | --- |
| `action_approval.requested` | target scope | approval id, operation, target ids, digest, required proof |
| `action_approval.approved` | target scope | approval id, factor class, approving session |
| `action_approval.consumed` | target scope | approval id, consuming session |
| `action_approval.expired` | target scope | approval id; written by the expiry sweeper as a system actor |
| `action_approval.refused` | target scope | approval id or none, operation, policy source and entry, cause: `machine-principal`, or `declined` if Open item 11 adopts decline |
| `action_approval.policy_changed` | instance | level (floor or project), before and after coverage, loosening or tightening, the approval id for a loosening |

The target scope's trail is the tenant trail for tenant-class operations and
the instance trail for instance-class ones. Summaries are not repeated in
payloads; ids and the digest are. Whether project-addition changes also land
in the project's tenant trail is Open item 17.

## Freeze analysis

The `/api/v1` freeze is live: `v1.0.0` was tagged on 2026-10-31.

**Schema layer** (`api/freeze.go`, `PermittedChanges`):

- The approval endpoints and the policy read and write endpoints are new
  paths: `endpoint-added`, `api-operation-id-added`.
- Covered operations that do not declare a `409` today (for example
  `rotateTokenKey`, `rotateScanningKey` and `removeRemote`, which declare 401,
  403, 404, 429, 500 and 503 besides their success status) gain one:
  `response-non-success-status-added`.
- Their descriptions gain the approval rule: `description-changed`.
- **`ErrorCode` does not grow.** The refusal reuses `conflict`, whose wire
  policy already carries a caller-safe `detail` (`internal/server/errors.go`,
  `allowSafeDetail`). Existing conflicts with a detail include the
  reauthentication refusal (`reauthenticate over the environments this
  operation makes reachable, then retry (env_…)`, a `detailErr`
  (defined in `internal/service/values.go`) built in
  `internal/service/grants.go`, matched by `web/src/routes/Members.tsx` and by
  `forbidden` in `internal/cli/reveal_window.go`), the protected
  destination refusal, which carries the environment id, the git-mode
  definitions refusal (`internal/service/definitions_apply.go`,
  `gitModeRefusal`), `plan already applied; re-plan`
  (`internal/service/definitions_apply_exec.go`), the stale file-target
  generation (`internal/service/file_targets.go`), `self_connected`
  (`internal/service/remotes.go`, `ErrSelfConnected`) and the
  delivery-target refusals built by `conflictDetail`
  (`internal/service/delivery_targets.go`). The `Error.detail` description
  already admits "a `conflict` whose refusal names the caller's own state".
- **No new members or headers on existing routes,** and no `x-hikyo-*` marker
  on a frozen operation.

**Behaviour layer.** The api-cli-surface ADR § Version promise makes a frozen
operation's authorization formula and error semantics part of the contract,
and changing them is breaking even when the schema diff is empty. Covering an
operation adds a conjunct and a refusal to a frozen operation, and a covered
copy also reorders its refusals (*Ordering inside operations*). The `detail`
precedents above show the wire shape, not that a new refusal is permitted.
The nearest post-freeze precedent is the post-quantum declared amendment in
the api-cli-surface ADR's banner (2026-11-04,
[#25](https://github.com/Hikyo-Org/Hikyo/issues/25)): on PKI issuer and
certificate requests naming the new algorithm, it changed a refusal
`detail` and moved the authorization refusal ahead of the contract refusal,
and it was recorded as a declared amendment rather than a freeze exception.
"No freeze exception" (an owner decision) rests on one reading: **a default-off conjunct that only an operator's explicit opt-in
activates is non-breaking**, because with the default empty policy no frozen
operation changes behaviour, and an opted-in instance fails older clients
loudly with a `409` they cannot cure, in the same class as setting the
protected flag. Open item 21 asks the owner to confirm that reading, and
whether it also needs a declared amendment banner on the api-cli-surface ADR
as #25 had.

**Exception set.** Proposed (Open item 21): the passkey begin and finish
endpoints for an approval are WebAuthn ceremony endpoints, so they join the
closed authentication-protocol exception set of the api-cli-surface ADR § One
surface. Adding a member to that set is itself a spec review event.

**CLI.** No verb or flag is removed or repurposed. `--yes` and the
`--confirm*` flags keep their meaning. Proposed (Open item 28): they remain
necessary, and are no longer sufficient where a policy covers the
operation.

## Threat-model notes

- **Self-approval through the inline code:** see *Approving*; Open item 12.
- **Self-approval through an agent-enrolled passkey:** see *Approving*; Open
  item 26.
- **Replay.** A consumed approval cannot be consumed again (compare-and-set).
  A code is spent once per step (`AdvanceTOTPStep`). A passkey challenge is
  spent with its ceremony. An approval from another session or principal does
  not match.
- **Paths around covered operations.** An agent holding a publish-ceremony
  window from a plain reauthentication at a protected environment, outside
  any approval, can import or declare values for those keys with no action
  approval; an agent on a session that may revoke one credential can delete
  the service account and revoke all of them. Every such path is listed
  under *Equivalent-effect paths*; see Open item 3.
- **Spending an approval's window elsewhere.** A combined ceremony opens a
  publish-intent window over an environment and key set. Import, declare,
  run-now, copy's destination gate, and rotation create, resume and
  release-hold (`requireRotationCeremony`,
  `internal/service/credential_rotation.go`) build the same intent,
  `NewDisclosureReauthIntent(PurposePublish, …)` over one environment and
  their keys (`internal/service/reauth_intent.go`,
  `internal/service/ceremony.go`). Environment clone builds the copy-source
  intent a carried-secrets copy takes (`disclosureIntent(PurposeCopy, …)`,
  `internal/service/hierarchy.go`). A mint's ceremonies go through
  `RequireDisclosureAuthority`, which builds `NewMintReauthIntent(env, nil)`
  per environment (`internal/service/grants.go`); the same function serves
  federated binding creation (`internal/service/federation.go`) and grant
  widening on a machine principal (`checkMachineWidening`,
  `internal/service/grants.go`), and the same intent is built directly by
  certificate issue (`internal/service/pki_certificates.go`), lease mint
  (`internal/service/dynamic.go`) and SSH certificate issue
  (`internal/service/ssh.go`). An unbound window satisfies the mint intent
  too (the comment in `RequireDisclosureAuthority`). #1534 needs this full
  consumer list. So an uncovered operation over the same unit could spend
  the window the human opened for an approved action. Hence the rule in *The combined ceremony*, step 4: such
  a window is consumable only together with its approval. Open item 3.
- **Digest confusion and state drift:** see *The request digest* (names
  resolve to ids in `targets`) and *Resolved state*.
- **Hiding an entry past the cap.** An agent that controls the request can
  order its keys so a sensitive one falls after the summary's cap. On the
  browser page and in an unmodified CLI, the human sees the complete
  affected list before giving a proof, and the approval binds every id
  (*The complete affected list*). An agent that controls the human's channel
  can still show only the preview: that is the inline-code self-approval
  threat (*Approving*; Open item 12). A residual risk remains even with the
  full list shown: one entry buried in a very long list is easy to miss, and
  the list can only expose it, not make the human read it.
- **Approval-page spoofing.** Proposed: the CLI builds the page URL from its
  configured origin. A passkey assertion is bound to the relying-party id, so
  a look-alike origin cannot obtain a usable one. A phished code is useless
  without the human's session, because approving requires authenticating as
  the requesting principal. Names in the summary and in the complete
  affected list are quoted next to their ids, and control and format
  characters are shown as escapes (Open item 24).
- **Race between approve and consume.** Both transitions are compare-and-set.
  Expiry is checked when consuming, with the clock read at that moment, as
  `requireCeremony` reads `auth.now()` at consumption rather than earlier
  (`internal/service/ceremony.go`), so an approval that expires while the
  retry waits on a lock is refused. The retry re-runs authorization,
  preconditions and the resolved-state comparison.
- **Pending-record flooding.** Only callers who pass authorization can create
  records, the per-principal rate limits of the audit-model ADR § Denials
  throttle a principal spraying requests, and records expire after five
  minutes. De-duplication and a per-session cap are Open item 13.

## Alternatives rejected

- **A CLI-config-only guard.** The agent drives the CLI and can edit its
  config or call the API directly. It also covers no other client.
- **Reusable windows.** A sliding approval window lets an agent run any
  number of covered actions after one human approval. The decision is one
  exact action.
- **An explicit approval header on the retry.** The freeze gate would accept
  a new optional header: oasdiff's `new-optional-request-header-parameter` is
  on `PermittedChanges`, and the contract layer (`api/freeze_contract.go`,
  `parameters`) refuses only an added required parameter. So this is rejected
  on cost, not legality: every covered frozen operation and every generated
  client would carry it, and it adds nothing the (session, operation, digest)
  match does not already bind.
- **Four-eyes.** A second approver is the secret-change approval model
  of #151: policy-bound, environment-scoped, pinned to the publish preview
  token, and riding the `publish@env` chokepoint (the mvp-boundary ADR's
  declared amendment 3, recorded in the permission-model ADR). It answers "should this change ship", by the policy's
  approvers (the requester only where the policy permits self-approval,
  mvp-boundary C-APV); action approval answers "is the requester present" for one
  request. They compose: merging an approved #151 request into a covered
  environment still takes the merging human's action approval, with
  `approval_request_id` and `bypass` in its digest. A covered publish under
  a #151 policy can stage a #151 request instead of publishing (`approvalGate`,
  defined in `internal/service/approvals.go` and called from
  `internal/service/publish.go`; `PublishRequest.purpose`). Whether
  staging needs an action approval, and whether a machine's staging publish
  is refused, is Open item 23.

## Consequences

- Nothing changes until an operator opts in.
- Machines: see *The approvable operations*, the machine consequence.
- Other operations reach the same end state as covered ones; see
  *Equivalent-effect paths* below and Open item 3.
- Scheduled rotation runs have no request and no session; how they relate to
  a policy is Open item 22.
- Approval counts: see *The approval lifecycle*, Granularity.
- Every covered action by a human costs one round trip more: refuse, approve,
  retry.
- An instance or project admin whose only possession factor is TOTP cannot
  loosen a policy, including removing a project addition they added
  themselves, until they enrol a passkey, because loosening needs one.
  Password-only and OIDC-only admins are in the same position (Open item 6).
- A new table on both engines, created by a store migration.

### Equivalent-effect paths

One criterion for every row: a path is equivalent when it changes what a
protected or covered environment's consumers receive, or who can
authenticate or decrypt, in the way a covered operation does. The rows come
from the registry (`internal/authz/registry.go`) and the
`x-hikyo-operation` mappings in `api/openapi.yaml`, human or machine. This
table is the initial seed of the registry declaration below and may be
incomplete. Artifacts are
the route's `x-hikyo-artifacts`: "human" is `human-session`, "machine" is
`machine-credential`. "Open 3" means the owner decides whether the policy
covers it (Open item 3).

| Covered operation | Other path (artifacts) | What it does | Status |
| --- | --- | --- | --- |
| `value.publish`, `value.copy-destination` | `value.import` (`importValues`; human), `value.set` (`declareValues`; human) | Write cells and publish them directly. Each authorizes `value.publish` and takes the publish ceremony at a protected environment (`internal/service/import.go`, `(*Values).Import`; `internal/service/values.go`, `declare`). #151 already refuses them in a covered environment (`approvalGatedTrigger`, `internal/service/approvals.go`) | Open 3 |
| same | `pin.set` (`createRevisionPin`; human) | Moves a workload to a historical revision, with a reveal ceremony for it (`internal/service/pins.go`, `Set`); writes no cells | Open 3 |
| same | `definitions.apply` (`applyDefinitionsPlan`; human, machine) with `allow_delete` | Deletes keys: `ClearKey` clears each deleted key's values in every environment, protected ones included, and `fanOut` republishes every environment (`internal/service/definitions_apply_exec.go`; `fanOutSchemaPublishProofs`, `internal/service/publish.go`), with no protected ceremony; the plan only refuses if the protected set grew. Its environment deletes remove only empty environments (`guardDeletions` refuses one holding a live value) | Open 3 |
| same | `key.schema-publish-values`, `key.schema-publish-empty` (no route of their own) | Key create (as `key.schema-publish-empty`, or as `key.schema-publish-values` when the key joins an existing group), rename, update-declaration, reclassify, set-group and the `key.delete` route authorize it per environment (`authorizeKeyFanOut`, `internal/service/keys.go`) and republish each. Key-group delete and `definitions.apply` instead authorize `value.publish` itself per environment (`fanOut` with no key proofs reaches `republishWithStorage`, `internal/service/publish.go`), outside `Revisions.PublishPlanned`, where the approval check sits. Each republish changes the snapshot consumers receive | Open 3 |
| same, and `project.delete` | `environment.delete` (`deleteEnvironment`; human, machine) | Deletes the environment with its values (`ClearEnvironment`), with no protected check (`Environments.Delete`, `internal/service/hierarchy.go`). Unlike `definitions.apply`, this route does not require the environment to be empty | Open 3 |
| same, and `rotation.run_now` | `rotation.run` (no route; the rotation executor) | Publishes a rotated value through `Revisions.PublishPlanned` with `PublishRequest.rotation`, as a session-less `LocalPrincipal` (`internal/service/rotation_executor.go`); how the check treats it is under *Rotation runs through publish* | Open 22; see Open 7 |
| same, and `rotation.run_now` | `rotation.create`, `rotation.resume`, `rotation.release_hold` (human) | Start or restart a schedule whose runs publish | Excluded at protected and #151-covered environments, which create and resume refuse (`requireUnattendedEnvironment`, `internal/service/credential_rotation.go`); release-hold checks only an `Active` rotation, and a paused or halted one stays inert until resume re-checks. Open 3 at an environment only a project addition covers |
| same | `environment.create` with a source (`cloneEnvironment`; human, machine) | Copies values into a new environment | Excluded: the new environment is not protected (neither `CreateEnvironmentRequest` nor `CloneEnvironmentRequest` carries the flag) and has no id a project addition could name |
| same | `value.stage` (`setValue`, `clearValue`; human, machine), `value.initialize` (human), `revision.restore` (human) | Stage drafts | Excluded: a draft reaches the environment only through a publish (`Restore` stages, `internal/service/rollback.go`) |
| same | `pin.release` (human) | Returns a workload to the environment's published head. Every pin expires, but a pin may last up to 365 days (`MaxPinLifetimeDays`, `internal/service/pins.go`), so a release can change what the workload receives well before that | Open 3 |
| same | `personal-value.set` (human) | Sets the caller's own value | Excluded: personal values never enter a snapshot, revision or pin ([flat-model ADR](./flat-model.md) banner) |
| same | `key.delete` route (human) | Removes a key | Excluded for this route only: it is refused while the key holds a value in any environment (`Keys.Delete`, `internal/service/keys.go`), so it clears nothing; its republish is the `key.schema-publish-values` row, and `definitions.apply`'s key deletion does clear values (row above) |
| same | `adapter.sync` (human) | Delivers the environment's published state to its targets | Excluded: delivers only what was already published |
| same | Adapter retargeting: `adapter.configure` (`createAdapter`, which requires a first target; `addAdapterTarget`, a destination move in `updateAdapterTarget`, `updateAdapterOrigin`), `adapter.delete` (including `removeAdapterTarget`), `adapter.credential-set`, `adapter.adopt` (human) | Creating an adapter, or adding a target, on a protected environment delivers it to a new external destination. A target's environment is immutable (`internal/service/adapters.go`), but removing and re-adding a target, or moving its destination (`targetDestinationChanged`), can point a destination that received a protected environment at an unprotected one; adopt takes over existing external names. Configure, credential-set, adopt and sync take an adapter reauthentication per environment (`requireAdapterCeremony`), not an action approval | Open 3 |
| same | #151 merge and emergency bypass | Publish a reviewed or bypassed change | Covered: both run inside `value.publish` (`approvalGate`) |
| any covered environment | `environment.settings-update` clearing the protected flag (human) | Removes floor coverage | Loosening, Open 4 |
| `project.delete` | `org.delete` (human) | Deletes an org | Excluded: no cascade; a project keeps the ancestry constraint live (`projects.org_id REFERENCES orgs (id)`, `internal/store/migrations/sqlite/00003_tenant_chain.sql`; `Orgs.Delete`, `internal/service/hierarchy.go`) |
| `identity.credential-mint` | `identity.binding-create` (`createFederatedBinding`; human) | Creates a federated credential for a service account under the same `postStateReach` and `RequireDisclosureAuthority` (`CreateBinding`, `internal/service/federation.go`) | Open 3 |
| same | `developer-credential.mint` (human) | Issues a credential that reads one environment on the human's behalf | Open 3 |
| same | `grant.create-*` and `grant.template-*` on a machine principal (human) | Widen what an existing machine credential can read: `checkMachineWidening` requires `manage-identities` and `RequireDisclosureAuthority`, the mint's own conjunct (`internal/service/grants.go`; `MintCredential`'s comment in `internal/service/identities.go`) | Open 3 |
| same | `federation.issuer-update` (`updateFederationIssuer`; human) | Replaces an issuer's JWKS source, including a static JWKS, under `instance-config` with no ceremony (`UpdateIssuer`, `internal/service/federation.go`). A binding pins only claims the signer controls (`BindingRequest`, `internal/service/federation.go`), so a key the caller holds authenticates as every service account bound to that issuer | Open 3; instance-class, so only the floor can cover it |
| same | `saml-provider.put`, `saml-provider.refresh-metadata` (human) | Accept new IdP signing certificates with caller-confirmed fingerprints and no reauthentication; the new key logs in as any account linked to the provider (*Approving*, enrolment with no proof) | Open 3; instance-class |
| same | `remote-credential.create`, `scim-credential.mint` (human) | Issue a peer-instance connection or SCIM credential | Excluded: `instance-credential` is admitted only on `remote.directory-serve` and `scim-credential` only on the SCIM routes, none of which reads or writes a value |
| same | `lease.mint` (human, machine), `certificate.issue` (human, machine), `ssh-cert.issue` (human, machine) | Issue a dynamic-provider lease, an X.509 certificate or an SSH certificate | Excluded: none is an artifact the API admits (`internal/operation/contract.go` lists the artifacts), so none authenticates as a Hikyo principal |
| `identity.credential-revoke` | `identity.service-account-delete` (`deleteServiceAccount`; human) | Revokes every credential of the account under the same `manage-identities@project` formula (`DeleteServiceAccountAggregate`, `internal/service/identities.go`) | Open 3 |
| same | `identity.binding-create` with `replaces` (human) | Revokes the replaced credential (`RevokeMachineCredential` in `CreateBinding`) | Open 3 |
| same | `developer-credential.revoke-project` (human) | Revokes a human's developer credential | Open 3 |
| same | `grant.revoke-*` on a service account (human) | Removes the account's authority | Excluded: the credential stays live; only its grants change |
| same | `federation.issuer-delete` (human) | Removes a federation issuer | Excluded: refused while live bindings name the issuer (`DeleteIssuer`, `internal/service/federation.go`) |
| `remote.remove` | `remote-credential.revoke`, `remote-pairing.revoke` (human) | Revoke a credential a peer holds, or a pairing invite | Excluded: neither deletes a remote; `RemoveRemote` is the only route caller of `Remotes().Delete`. Backup restore also deletes every restored remote (`DeleteRestoredRemotes`, `internal/store/authn/restore.go`), a host path (`hikyo restore`, `app.RunRestore` in `internal/app/backup.go`) outside the registry (*Registry completeness*, outside the test's reach; Open 3) |
| the five crypto rotations | `crypto.reencrypt-instance`, `crypto.reencrypt-project` (human) | Complete a DEK rotation: move ciphertext onto the active version and retire superseded ones (`internal/service/reencrypt.go`) | Open 3 |
| same | `pki-issuer.rotate`, `ssh-ca.rotate`, `transit.key-rotate`, `saml-sp-key.rotate` (human) | Rotate an issuer, CA, transit or SAML key | Excluded: different keys, not among Decision 4's five. The five rotation methods are called only from their routes (`internal/server/rotation.go`, `internal/server/revisions.go`) |
| both policy updates | none | `SetCredentialPolicy` and `SetDeveloperCredentialPolicy` each have one caller, and the self-config runtime keys carry neither policy (`internal/runtimeconfig/catalogue.go`) | n/a |
| `self-config.adopt`, `self-config.apply` | `value.publish`, `value.import`, `value.set` into the self-config environment | Publish a configuration revision | Excluded: a revision takes effect only through an apply job, and `BeginJob` has one caller, `Apply`; restore authorizes `self-config.apply` |
| same | `self-config.provision-project` | Creates the managed project's initial key | Excluded: authorized only inside `Adopt` (`internal/service/self_config_provision.go`) |

**Registry completeness.** *Proposed implementation design for #1534 (Open
item 29).* Every review round has found more paths, so prose alone will not
close this table. #1534 adds a test that every operation in
`internal/authz/registry.go` declares exactly one of: `approvable`;
equivalent-of a covered operation, with its disposition; or excluded, with a
reason code. An operation with no declaration, including a newly added one,
fails CI. Routes with no `x-hikyo-operation`, the authentication and factor
routes (for example `setPassword`, `enrolTotpStart`, `stepUpTotp`,
`reauthTotp` and the CLI login and reauthentication handoffs in
`api/openapi.yaml`), are included in the same test. A per-operation
declaration alone is not enough, because an approvable operation is also
reached from code that is not its gated function. The test therefore also
covers call sites:

- **Discovery is mechanical.** A go/types scan finds every use of a
  constant value of type `authz.Operation` that is approvable (matching
  values, not `Op*` identifier names, so a string literal converted to
  `authz.Operation` is caught) and every call to a reauthentication intent
  constructor (`NewPublishReauthIntent`, `NewMintReauthIntent`,
  `NewDisclosureReauthIntent`, `newReauthIntentForOperation`,
  `internal/service/reauth_intent.go`).
- **The scan follows the call graph to entry points.** A direct reference
  alone misses a new caller of a shared helper: the rotation executor
  references only `rotation.run`, `rotation.resume` and `value.stage`, yet
  publishes through `Revisions.PublishPlanned`
  (`internal/service/rotation_executor.go`). Proposed: from each discovered
  site the scan walks callers up to an entry point (a route handler, an
  executor, a sweeper or a host command), and the test diffs the discovered
  (entry point, helper, trigger) tuples against the declared tuples in both
  directions, so a new caller and a stale declaration both fail; a
  hand-kept list could catch neither. The alternative is a typed
  declaration argument that each such helper takes and the scan reads at
  every call site. The scan covers `./...` from the module root, including
  `internal/server`, `internal/app`, `cmd/hikyo` and `scripts/`, whose
  acceptance fixture is a package in the same module and also calls
  `PublishPlanned` (`scripts/acceptance/mcp-fixture/seed.go`; declared as a
  fixture entry point). `_test.go` files are out of scope.
- **Dynamic operations and purposes are flagged.** An
  `authz.Operation(<variable>)` conversion cannot be resolved statically
  (for example `internal/service/cli_reauth.go`,
  `internal/service/workspace.go`, `internal/service/reauth.go`,
  `internal/service/reauth_intent.go` and `internal/service/webauthn.go`),
  and neither can an intent constructor call whose operation or purpose
  argument is not a constant: `NewDisclosureReauthIntent` takes
  `ReauthPurpose(<variable>)` in `internal/server/remotes.go`,
  `internal/server/webauthn.go` and `internal/server/cli_reauth.go`, and a
  purpose parameter in `disclosureIntent` (`internal/service/ceremony.go`)
  and `newReauthIntentFromBinding` (`internal/service/reauth_intent.go`).
  The scan reports each one, and each needs an explicit manual declaration.
- **Shared helpers are declared per caller.** A helper reached from several
  operations cannot carry one gated or equivalent-of declaration, so its
  declarations are keyed by entry point and by the trigger argument the
  helper already takes. `republish` (`internal/service/publish.go`) authorizes
  `value.publish` through `republishWithStorage` and is called from `Copy`
  (`copyOpCopy`, `copyOpBulkApply`) and `declare` (`"declare"`,
  `internal/service/values.go`), `Import` (`"import"`,
  `internal/service/import.go`) and environment create and clone
  (`"environment-create"`, `internal/service/hierarchy.go`);
  `republishWithStorage` is also reached through `fanOut` from key-group
  delete and `definitions.apply`. `Revisions.PublishPlanned` is declared the
  same way: its route caller (`internal/server/revisions.go`) is gated, and
  its rotation executor caller is the *Rotation runs through publish* case.
  The same applies to the intent helpers: the mint intent is built by
  `RequireDisclosureAuthority` (`internal/service/grants.go`; mint, binding
  creation, grant widening) and directly by certificate issue, lease mint
  and SSH certificate issue (*Spending an approval's window elsewhere*),
  and `disclosureIntent` (`internal/service/ceremony.go`) builds a
  disclosure intent for whichever purpose its caller passes.
- **Effects without an approvable operation are keyed to primitives.**
  `environment.delete`, `identity.service-account-delete`,
  `federation.issuer-update` and the `saml-provider.*` writes never
  authorize an approvable operation, so a declaration naming only the
  operation would never be re-checked when the code changes. Their
  declarations are keyed to effect primitives. The registry's `storeOps`
  are enforced at run time: the store checks each through `authz.Verify`
  (for example `sqliteRemotes.Delete`, `internal/store/repos_remote.go`).
  The `TxAuthorizer` mutators carry effects `storeOps` does not show, but
  they are plain forwarders with no proof or operation check
  (`internal/authz/forwarders_gen.go`): `DeleteServiceAccountAggregate`,
  `UpdateFederationIssuer`, and for the SAML writes `CreateSAMLProvider`,
  `UpdateSAMLProvider` and `DeleteSAMLProvider`; the
  `identity.service-account-delete` and `federation.issuer-update` rows list
  only an audit insert. So mutator reach is computed by the same call-graph
  scan from each operation's call site, and an operation that gains a
  mutator or a store op fails until declared.
- **Guarded exclusions name their test.** Each exclusion that rests on a
  code guard names the test that pins the guard, and #1534 adds one where
  none does. For example: `Keys.Delete`'s `EnvironmentsWithValue` refusal
  (`internal/service/keys.go`), the `orgs` foreign key behind `org.delete`,
  `DeleteIssuer`'s refusal while bindings name the issuer
  (`internal/service/federation.go`), `requireUnattendedEnvironment`, the
  `environment.create` request shape that carries no protected flag,
  artifact admission for remote, SCIM and lease credentials
  (`internal/operation/contract.go`), and single-caller facts: `BeginJob`
  called only from `Apply`, provisioning only inside `Adopt`, the crypto
  rotations only from their routes, and `RemoveRemote` as the only caller
  of `Remotes().Delete` (`internal/service/remotes.go`). The call-site scan
  itself can pin the single-caller facts.
- **Outside the test's reach.** Host commands outside the registry, which
  no registry operation authorizes. `hikyo restore` deletes every restored
  remote (`DeleteRestoredRemotes`, `internal/store/authn/restore.go`)
  through the mutator `TxAuthorizer.InvalidateRestoredExternalCredentials`,
  called from `service.CompleteRestore` (`internal/service/restore.go`),
  which runs from restore admission (`internal/app/backup_admission.go`)
  and from the upgrade drill (`internal/app/backup_upgrade_drill.go`). The
  scan can report that mutator reach from those entry points, but there is
  no operation to declare it against. Effects in executors and sweepers
  that skip the registry are the same. They need review.

This table is the test's initial seed.

## Reconciliation with upstream ADRs

- **permission-model ADR (#15).** Each covered operation's formula gains a
  conjunct: where the effective policy covers it, a consumed action approval
  for this exact request. It rides the operation's own chokepoint and is
  evaluated after the formula (Open item 28). Declared amendment; banner and governance in
  Open item 14.
- **audit-model ADR (#24).** The catalogue gains the `action_approval`
  category. Declared amendment; banner and governance in Open item 14.
- **human-auth ADR.** Unchanged. Approval reuses its factor rules: possession
  factors only, passkey at an effective `0` window.
- **api-cli-surface ADR.** Unchanged under the reading in Open item 21,
  unless the owner asks there for a declared-amendment banner as #25 had.
- **machine-identities ADR.** Unchanged. Machines are refused, never
  approved.
- **[mvp-boundary ADR](./mvp-boundary.md).** Its § 4.1 row "Approval
  workflows" was promoted by declared amendment 3 for **changes** only:
  policy-bound, multi-person and environment-scoped review of secret and
  config changes (#151). Action approval is single-person and also covers
  rotations, deletes, remote remove and credential operations, which fall
  outside that promotion. § 4.2 keeps "Approval-to-reveal" Out; action
  approval is distinct from it, because the approver is the requester. Later
  records reconcile against this ADR: the
  [developer-credentials ADR](./developer-credentials.md) admits its
  credentials explicitly, and the [desktop-app ADR](./desktop-app.md) calls
  the app a post-MVP addition. Whether this record is a post-MVP addition or
  a declared amendment to the mvp-boundary ADR is Open item 14.

## Open items

Each is a detail the epic leaves open and the code forces, or a rule this
record proposes. Each needs the owner's confirmation.

1. **Machine refusal.** Proposed: a machine call creates no record, and is
   refused with `conflict` and
   `action-approval-machine-refused (<source>: <entry>)`, naming the policy
   source (floor or project addition) and the entry; the operation goes in
   the audit payload.
2. **Audit category name.** Proposed `action_approval`, because `approval.*`
   is #151's (`internal/audit`).
3. **Equivalent-effect paths.** For every row marked "Open 3" in
   *Equivalent-effect paths* (Consequences), should the policy that covers
   the matching operation cover that path too? The rows include
   `identity.service-account-delete`, which revokes every credential of an
   account under the same formula as the covered revoke, and the
   machine-admitted `environment.delete`, which deletes an environment with
   its values, and `definitions.apply`, whose key deletion clears values in
   every environment. The options per row are to make it
   approvable and covered with its operation, or to leave it uncovered.
   Whatever is decided, a window opened by an approval ceremony stays bound
   to its approval (*The combined ceremony*, step 4), so no uncovered path
   can spend it. The table may be incomplete. The registry-completeness test
   proposed under it (Open item 29) makes every operation, and every call
   site it enumerates, declare its disposition, so a missed operation fails
   CI. Paths outside the registry, namely host commands with no registry
   operation (such as `hikyo restore`, which reaches
   `DeleteRestoredRemotes` through
   `TxAuthorizer.InvalidateRestoredExternalCredentials` from restore
   admission and the upgrade drill) and effects in executors and sweepers
   that skip the registry, are outside the test and would still need a
   review to find.
4. **What counts as loosening.** Proposed: any change after which effective
   coverage does not contain the old (*Policy*, loosening rule), with the
   policy writes carrying `approvable` but covered by that rule rather than
   by policy. Which of the indirect writes listed under *Policy* count too?
5. **Where the floor is stored.** In instance configuration that self-config
   apply and restore can rewrite, or in a separate store only the policy
   endpoint writes?
6. **Accounts with no possession factor.** Approval never accepts a password
   or a recovery code (Decision 6, recorded reading). What does the page offer a password-only or OIDC-only
   account: OIDC reauthentication (`prompt=login`, `max_age=0`), or a refusal
   that points to factor enrolment? An OIDC-only account whose session holds
   a live credential-establishment stamp can enrol a passkey with no proof
   (*Approving*, enrolment with no proof), and a CLI session can obtain such
   a stamp through a relayed IdP login; is that an acceptable route to the
   passkey the page needs?
7. **Local host authority.** It is session-less and skips the reauthentication
   ceremony (`skipsCeremony`). Is it exempt from approval, as break-glass, or
   refused like a machine?
8. **Admin capability mapping.** The epic decides that instance admins set
   the floor and project admins add to it. Proposed: `instance-config` for
   the floor and `project-settings` for project additions (permission-model
   ADR § Capability atoms). Also: may a project addition cover
   project-scoped `crypto.rotate-dek` (*Policy*)?
9. **Floor selectors beyond protected environments.** The floor covers
   protected environments (Decision 3, recorded reading). Should it offer
   any other environment class, for example every environment? This bears
   on run-now: rotation create refuses a protected environment
   (`requireUnattendedEnvironment`, `internal/service/credential_rotation.go`),
   and the executor halts on a protected-destination refusal
   (`rotationGateReason`, `internal/service/rotation_executor.go`). So a
   floor run-now entry limited to protected environments covers only
   rotations created before their environment became protected, whose runs
   then halt; in effect it covers nothing.
10. **Endpoints and the CLI flow.** Endpoint paths; how the CLI learns a
    browser approval completed (polling the read endpoint, or the human
    pressing Enter); what a non-interactive CLI does (proposed: print the
    summary and approval id and exit with the existing refusal exit code,
    so the surface that later takes the proof shows the complete affected
    list); the proposed routing rule that a CLI which will not print the
    complete affected list inline takes no code and sends the human to the
    browser page, and the bound that triggers it; the proposed rule that the
    page accepts the authenticator code where a passkey is not required
    (*The complete affected list*); and the proposed rule that the CLI opens
    the page only at its configured origin.
11. **Decline.** The epic lists `refused` as an audit event. Proposed: an
    explicit decline, through its own endpoint, moves a pending approval to
    the terminal `refused` state. Failed code attempts do not; the factor
    failure counters bound them.
12. **Inline code strength.** The inline code does not resist an agent that
    controls the human's channel or holds the seed. Outside protected
    environments and loosening, may a policy entry require a passkey (for
    example for root-key rotation)?
13. **Pending de-duplication and cap.** Proposed: an identical refused request
    while a pending record exists for the same session, operation and digest
    returns the same `apr_` id, and a session holds a bounded number of
    pending records.
14. **Governance.** The permission-model (#15) and audit-model (#24)
    amendments need their tickets reopened, the cross-model review run or
    waived by the owner for this decision only, and banners recorded. Do the
    banners land with this record or with the implementation? And since
    declared amendment 3 promoted only multi-person change approval (the
    mvp-boundary ADR § 4.1, "Approval workflows"), is this record a post-MVP
    addition beside that ADR, or a declared amendment to it
    (*Reconciliation with upstream ADRs*)?
15. **One code for both halves.** A TOTP code is spent once per step. In a
    combined flow where both reauthentication and approval accept a code, may
    one code cover both, or does the second wait for the next step?
16. **Prepare-only requests.** `self-config.apply` with `prepare_only: true`
    commits a job row (`BeginJob`) and a `self_config.apply_requested` audit
    event, but no deployment, and returns before the transaction that
    consumes reauthentication (*Ordering inside operations*). Proposed: it is
    not refused for approval. Likewise the unconfirmed credential-policy
    preview, which returns the affected credentials with a policy-read audit
    event and changes no policy.
17. **Policy changes in the project trail.** The epic puts policy changes in
    the instance trail. Should a project-addition change also land in that
    project's tenant trail for its `audit-read` holders?
18. **Expiry anchor.** Proposed: five minutes from the request
    (`CLIReauthLifetime`). Alternative: five minutes from approval
    (`bindExactIntent`).
19. **Approval granularity and counts.** Confirm the granularity proposed
    under *The approval lifecycle*. And how does one signature carry N
    intents (Decision 8), when a ceremony row has one `EnvironmentID` and one
    `OperationBinding`: a binding over an environment set, as adapter intents
    do; several rows under one challenge; or a new row shape? The largest
    cases are a copy into several protected destinations and a mint's one
    intent per current and historical environment
    (`RequireDisclosureAuthority`, `internal/service/grants.go`).
20. **Consumption and failed operations.** Proposed: consumption commits only
    with the operation's effect, so an approval survives an operation that
    fails, until it expires, and the human can retry without approving again.
    Alternative: consume on first use whatever the outcome.
21. **Freeze reading.** Confirm that a default-off conjunct that only an
    operator's opt-in activates, including the covered-copy refusal
    reordering, is non-breaking under the api-cli-surface ADR § Version
    promise, so that no freeze exception is needed. For copy, choose
    between the reorder only when covered (*Ordering inside operations*)
    and authorizing every leg first for every copy, which changes the error
    order of uncovered copies too but reveals nothing about coverage. The
    post-quantum declared amendment in that ADR's banner (#25) is the
    post-freeze
    precedent for a refusal reorder; does this change need the same
    declared-amendment banner? And confirm that the approval passkey
    endpoints join the authentication-protocol exception set (*Freeze
    analysis*).
22. **Scheduled rotation runs.** Proposed: a scheduled run has no request and
    no session, so no approval applies. Alternative, following #151: extend
    `requireUnattendedEnvironment`, which refuses protected and #151-covered
    environments at create, resume and release-hold and in the executor's
    pre-run gate (`gateWithin`), to environments a project addition covers,
    so a run halts before any mint; the check in `Revisions.PublishPlanned`
    then backstops a policy change between the gate and the publish. Either
    way that check must branch on `request.rotation`, because the gate does
    not refuse an environment only a project addition covers today
    (*Rotation runs through publish*).
23. **#151 staging.** Does a covered publish that stages a #151 request
    (`approvalGate`, `PublishRequest.purpose`) need an action approval, and is
    a machine's staging publish refused?
24. **Summary rendering.** Proposed under *The server-authored summary*:
    escape C0 and C1 controls (including ESC), bidi and other format
    characters, and render as plain text, in the summary and in the
    complete affected list. Should service-account names also
    gain the `checkText` checks?
25. **Resolved-state binding.** Proposed under *Resolved state*, with the
    copy reordering under *Ordering inside operations*.
26. **Agent-enrolled passkeys.** One relayed code enrols a passkey the agent
    controls (*Approving*); on an account with no local proof, a CLI
    session that obtains a credential-establishment stamp through a relayed
    IdP login needs no code at all. Options:
    (a) make account-security mutations approvable, requiring an existing
    passkey where the account has one. This restates a locked rule the code
    does not implement: the human-auth ADR § Account-security mutations rule
    1 requires "a possession factor where the account has one", which
    includes a passkey, but the proof selection takes TOTP, then the
    password, and never a passkey (`internal/service/reauth.go`).
    (b) accept an approval only from a passkey enrolled before the pending
    record was created.
    (c) state the residual risk and leave enrolment as it is.
27. **Staged root custody on an unconsumed approval.** With the approval
    check at root entry, a refusal stages nothing. But `stageCustody`
    re-seals custody before `rootRotationTx`, and nothing unstages it when
    that transaction fails, so a consumption that fails there (for example
    an approval that expired meanwhile) leaves custody staged under a root
    that never became active. Is that acceptable, as for any failed prepare
    transaction today, or must a failed consumption unstage?
28. **Other lifecycle and policy-screen rules.** Proposed: authorization
    first, with no record for a caller the operation refuses, including the
    covered-copy reordering it requires; records private to the requester,
    others getting `not_found`; the effective policy not secret from a
    caller who may list the copy source, accepting that a caller who may
    list the source but fails another leg learns coverage from the error
    order; the loser of two concurrent retries gets a new pending record; the policy screen's warning lists the machine
    principals the new coverage would refuse; and `--yes` and the
    `--confirm*` flags remain necessary.
29. **Record, digest and audit design.** The sections marked "Proposed
    implementation design for #1534" (*The approvable operations* and
    *Approving* in part, *The approval lifecycle*, *Ordering inside
    operations*, *The request digest* and *Resolved state*, *The
    server-authored summary*, *The combined ceremony*, *Audit* and the
    registry-completeness test) are design for #1534, including which
    mechanism commits each operation's
    pending record (*Committing the pending record*). Does the owner accept them as the starting design,
    with #1534's review free to revise them?

## Propagations

- Server and CLI sub-issue (#1534): registry flag, approval store and
  migration, per-operation canonical structs with golden tests,
  resolved-state capture, endpoints, policy with floor semantics, CLI flow,
  browser approval page, audit events. The approval check sits where each
  operation already authorizes before durable work, and a covered copy runs
  in the order under *Ordering inside operations*.
- Web sub-issue (#1535): policy screen with the machine warning, inline
  ceremony in existing dialogs, which show the complete affected list before
  taking a proof.
- User docs in the implementation pull requests: `cli-reference.mdx`,
  `configuration.mdx` and the feature page, per AGENTS.md.
