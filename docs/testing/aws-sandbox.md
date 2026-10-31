# AWS adapter sandbox acceptance

Status: recorded local checks passed; successful real AWS acceptance and current
effective IAM validation remain outstanding. See the [current handoff](../handoff/158-aws-secrets-manager.md#current-status-2026-10-22)
for exact results. The owner-approved B review route covers both AWS amendments;
historical formal SKIP remains. Current-head CI, fresh verification of retained
non-root setup and protected credential delivery precede real execution. Do not use an existing
production identity or an unrelated S3-compatible profile.

## Approved scope and concrete fixture binding

The dedicated account is `099256073061`, in `eu-central-1`. The October 22
banked directive is **A, accept runs until green but below $15 billable**.
Controlled manual attempts until green remain bounded to 32 new synthetic
creations, one replacement KMS key, the same eight roles, one temporary
boundary and at most one OIDC provider per attempt. The first banked generation
is 176 retained + 32 = 208 lifetime reservations, with at most one dispatch;
`hikyo-e2e-20261022c05/` and its ledger directories were unused at the handoff
checkpoint. Preserve all four old KMS deletion schedules and the retained
setup identity/MFA, account, sessions and protected environment. The fixed
horizon is `2026-10-26T12:22:56Z`, with fixture leases at most 24 hours.
Both gross and billable exposure, including completion reserves and unreflected
taxes/fees, must remain strictly below USD 15; aim below USD 5. Credits cannot
erase gross usage. No recurring run, automatic retry, paid upgrade or unrelated
resource is authorized. Parent owns setup and protected execution. The human
confirmed Chrome MFA sign-in; official CLI refresh and actual admission/acceptance
remain pending.
See the [account and provisioning plan](aws-sandbox-provisioning.md) for the
resource bounds and the current handoff for failed-run/cleanup history.

Record an existing, dedicated sandbox account, one commercial AWS region, an
unused prefix matching `hikyo-e2e-[a-z0-9]{8,32}/`, an existing symmetric KMS key
ARN, exact actor role ARNs, and a scope approval reference. Confirm that this is
the account owner's authorized sandbox; STS identity alone proves neither
ownership nor authorization. No test provisions an account, role, OIDC provider,
KMS key, network, deployment, or policy.

Approve a run window of at most two hours, a positive finite budget no greater
than the owner's approved amount or the fixture validator's USD 10 ceiling, and
force deletion of this run's synthetic secrets, including intentionally retained
and scheduled-for-deletion secrets. The budget field records approval; it is
not an AWS billing cap. Obtain a current cost estimate and a monitoring/stop
plan before authorizing the run. Combined gross and billable exposure must each
stay strictly below USD 15,
including completion reserves and unreflected taxes/fees, with a target below
USD 5. The code's USD 10 fixture ceiling and the trusted runner's stricter
USD 1 individual-fixture amount do not increase that combined allowance.
The example manifest is illustrative; no budget field supplies approval or an
instantaneous billing cap. Later manual generations require genuine outcomes,
reconciled resources and a freshly reviewed concrete derivation without
resetting reservations.
One suite entry point shares one cumulative ceiling of 32 CreateSecret
reservations across both engines, every auth mode, and cleanup seeding. Reserve
before sending and emit the nonsecret count/name reservation metadata; failed
attempts, retries, and same-name replacements consume
allowance and never refund it. The nominal first-attempt creation total is 31:
10 lifecycle (including two moved destinations), 6 role, up to 2 KMS-negative, 1 IAM canary, 8 adoption-witness
creations (including two replacements), and 4 promoted-replay seeds. Replacement
name reuse may require extra attempts, which still consume the same 32 ceiling
and can exhaust it; the suite fails closed rather than raising or resetting it.
Only one retry slot remains after 31 first-attempt creations. Process replay
cannot reserve any creates, so starting a child cannot reset this budget.
No parallel suite or repeated run
may reuse the prefix. Failed creates can leave uncertain resources; retain the
names and resolve their provenance before deleting them.

After identity preflight and before any mutation, the fixture creates an
exclusive, persistent nonsecret `.hikyo-aws-run-<scope-hash>.used` marker in the
private fixture directory. Keep that marker after success, failure, or process
exit. A second process using the same account/region/prefix and directory is
refused even with a different authorization reference. This is a local guard,
not an AWS-enforced or cross-directory quota: operators must not copy the
fixture directory or delete its marker to reset allowance. Reconcile ambiguous
resources and obtain a fresh approved namespace before a rerun. The guard and
its regression tests have compiled and passed in local controlled validation;
this does not establish enforcement by real AWS.

Use five distinct writer roles, a separate AssumeRole source role, a separate
oracle role, and a separate cleanup role. No role has another actor's grants.
The writer modes are static temporary credentials, ambient SDK discovery using
a projected WebIdentity token, explicit AssumeRole, explicit WebIdentity, and
static temporary credentials with an explicit KMS deny. Ambient coverage here
does not claim EC2 IMDS or ECS task-role coverage.

## Exact permission templates

The JSON files in [aws-sandbox](aws-sandbox/) are reviewable templates, not
deployment instructions. Resolve every `${...}` token from the approved scope
before use. Never attach an unresolved template. Independently inspect the
effective role policies, permissions boundaries, session policies, resource
policies, SCPs, and KMS key policy. Extra grants invalidate least-privilege proof.

| Template | Attachment and purpose |
|---|---|
| `writer.policy.json` | Each writer, substituting its own mode namespace. Metadata, create, stage, conditional promote, tag, and 30-day scheduled deletion. Explicit value-read and force-delete denies. |
| `static-lifecycle.policy.json` | Static lifecycle writer only: regional metadata listing for per-key planning and restoration inside the same mode namespace. Role-auth JSON tests do not need these grants. |
| `kms-denied.policy.json` | Additional explicit deny on the KMS-negative writer only. No policy modification occurs during the test. |
| `oracle.policy.json` | Synthetic-value oracle only: `GetSecretValue` and KMS decrypt. No write or cleanup permission. |
| `cleanup.policy.json` | External synthetic seed/edit and cleanup only. No value-read permission. Force deletion is confined to the run namespace. |
| `assume-source.policy.json` | Source role: assume only the exact test writer with the exact external ID. |
| `assume-writer.trust.json` | Writer trust: exact source role and external ID. |
| `web-identity.trust.json` | Instantiate separately for all seven OIDC-issued roles in the approved runner route, with exact issuer, audience, and immutable subject. The eighth writer uses source-only AssumeRole trust. No wildcard subjects. |

Substitutions:

| Token | Required value |
|---|---|
| `ACCOUNT`, `REGION`, `KMS_KEY_ARN` | Approved account, region, and existing exact key ARN. |
| `RUN_SECRET_ARN_PATTERN` | `arn:aws:secretsmanager:REGION:ACCOUNT:secret:RUN_PREFIX*`, with concrete scope values. |
| `WRITER_SECRET_ARN_PATTERN` | Same ARN prefix followed by the writer mode and `/*`; for example the concrete run prefix followed by `static/*`. |
| `AUTH_EXPIRES_AT` | Manifest expiry, no later than two hours after startup. |
| `CLEANUP_EXPIRES_AT` | Explicitly approved cleanup expiry after the run window; credentials must last through it. |
| `SOURCE_ROLE_ARN`, `ASSUME_WRITER_ROLE_ARN`, `EXTERNAL_ID` | Separate exact roles and a nonempty external ID matching the descriptor. |
| `OIDC_PROVIDER_ARN`, `OIDC_ISSUER`, `OIDC_AUDIENCE`, `OIDC_SUBJECT` | Existing sandbox OIDC provider and exact claim constraints. Issuer condition-key prefix omits `https://`. |

`ListSecrets` requires `Resource: "*"`; the template restricts region and time,
and the test wrapper refuses a prefix outside the run. IAM does not enforce a
name prefix on this action. This is why the account must contain synthetic
sandbox data only. Deletion conditions distinguish the writer's recovery
window from cleanup's irreversible operation. See the
[AWS authorization reference](https://docs.aws.amazon.com/service-authorization/latest/reference/list_secretsmanager.html).

The existing KMS key policy must permit the exact writer and cleanup roles to
use `kms:GenerateDataKey` and `kms:Decrypt`, and the oracle to use only
`kms:Decrypt`, through regional Secrets Manager and the approved encryption
context. It may delegate these grants to the supplied IAM policies. A key
policy that does not delegate them needs equivalent narrowly scoped statements
from its owner; this suite never changes it. No `kms:Encrypt`, key-management,
or grant-management permission is requested. Decrypt is needed for idempotent
writes and key validation even though Hikyo never reads a value. See
[AWS key usage and encryption context](https://docs.aws.amazon.com/secretsmanager/latest/userguide/security-encryption.html).

The source and trust templates cover
[AssumeRole](https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRole.html)
and [WebIdentity](https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRoleWithWebIdentity.html).
Use descriptor session durations of 900 seconds; the product accepts 900 to
3600 seconds. `GetCallerIdentity` has no added IAM permission requirement. It
is an identity check, not evidence that the above permissions are effective.

## Secure fixture input

[manifest.example.json](aws-sandbox/manifest.example.json) is intentionally
invalid and expired. Supply a protected manifest and protected descriptor/token
files outside Git through an approved secure local delivery mechanism. This
draft requires POSIX file permissions and refuses Windows fixture input. File
names in the manifest can be relative to its directory. The harness rejects
final-component symlinks, non-regular files, group/world permissions, changed
inodes during open, and inputs over 32 KiB. Parent directories must also be
trusted and protected by the operator. The product descriptor limit is 4 KiB.

Static descriptors, including oracle, cleanup, and the AssumeRole source,
require `mode`, `region`, `access_key_id`, `secret_access_key`, and a nonempty
`session_token`. Use temporary credentials only. The other descriptors contain:

- Ambient: `mode: ambient`, `region`.
- AssumeRole: `mode: assume-role`, `region`, `role_arn`, exact `external_id`,
  `session_seconds: 900`.
- WebIdentity: `mode: web-identity`, `region`, `role_arn`,
  `session_seconds: 900`.

Require separate protected negative sidecars: `expired_credential_file` on the
static writer, `expired_source_file` on the AssumeRole writer, and
`expired_token_file` on both ambient and WebIdentity writers. These must be
previously authentic sessions/tokens for the same approved roles/issuer, retained
past natural expiry, with their prior identity/provenance recorded privately.
They cannot prove their previous identity after expiry. Do not manufacture JWTs,
change signed claims or accept a generic invalid-token refusal as expiry proof.
The suite requires an expiry-specific STS refusal and then fresh-source identity
recovery. An initial auth-boundary failure stops before secret mutations.

No descriptor may override STS. Ambient and WebIdentity each require their own
projected token file. The fixture resets ambient credential variables, isolates
shared config, disables IMDS/container sources, and checks all supplied actors
against STS before writes. It does not refresh tokens or borrow local profiles.

No supported bridge from the available one-use `request_secret` reference to
this local test process has been established. Do not request credentials until
that secure injection path is supported. An operator-projected file can be a
separate approved path; it is not implied by this document. Never put secrets in
chat, command arguments, reports, Git, process titles, or shell history.

After parent releases the runtime slot, source guards can be validated without
AWS credentials. Real tests additionally require an isolated PostgreSQL DSN
in the process environment and the approved manifest. The proposed invocation,
from repository root after all prerequisites, is:

```sh
HIKYO_TEST_AWS_REAL_MANIFEST="$AWS_SANDBOX_MANIFEST" \
HIKYO_TEST_AWS_REAL_REQUIRED=1 HIKYO_TEST_AWS_REAL_MUTATIONS=approved \
  go test ./internal/isolation \
    -run '^TestAWSSecretsManagerRealSmoke$' \
    -count=1 -parallel=1 -timeout=30m
```

`AWS_SANDBOX_MANIFEST` is a nonsecret path supplied by the secure delivery step.
The real-suite invocation has not succeeded on the current candidate. Local
guard, offline fixture and authenticated lifecycle tests compile and pass, as
recorded in the current handoff. Earlier provider-only runs remain historical
evidence at their pinned inputs and are not real-provider acceptance.
Do not use `-v` or publish raw test logs without checking them for sensitive
material. A missing manifest skips locally, but fails with the required switch;
a present manifest without the explicit mutation switch fails before AWS use.

## Drafted runtime assertions

The single real suite requires both SQLite and PostgreSQL. It uses the real
adapter service, sealed credential storage, source publication, SQL outbox,
SQL ledger/effects, and worker. The loader shares the existing isolation
fixture's contract; it is not the full application bootstrap.

| Test | Assertions drafted |
|---|---|
| `RealSmoke/lifecycle` | Two-member canonical JSON including Unicode/newline; per-key values; customer KMS metadata; pause/resume revision binding; accepted write with lost response; hard child-process exit after accepted PutSecretValue, then a second process reopens the same signed store/key hierarchy and resumes the exact SQL job without duplicate versions; source unset schedules deletion and republish restores; real external write between staging and promotion remains current while sibling target converges; same-target partial conflict preserves the foreign key and completes its sibling, then retries only after exact-version metadata consent; interrupted version-bound adoption; per-key-to-JSON destination move with old-name scrub before activation, multi-target adapter retention, and prune; INTENT/OUTCOME linkage and audit secrecy. |
| `RealSmoke/auth-boundaries` | Protected expired static/source credentials and ambient/WebIdentity tokens receive expiry-specific real STS refusal; actual product auth refusal and fresh-source reconstruction recovery; configured wrong account refuses after approved-account STS with metadata blocked; wrong region refuses construction against the approved origin. Zero secret creations. |
| `RealSmoke/role-auth` | Ambient projected-token, AssumeRole, and explicit WebIdentity delivery/deletion; workload opt-in refusal; wrong external ID rejected by real STS; distinct approved principal identity; own-namespace GetSecretValue denial and cross-writer TagResource denial on recorded synthetic ARNs. |
| `RealSmoke/kms-denial` | Explicit KMS denial reaches durable auth-failure state and never records a successful value mutation. Key validation may refuse the create itself. |
| `RealSmoke/iam-denial` | Static writer own-namespace GetSecretValue denial, cross-writer TagResource denial and positive synthetic-reader control; one cleanup-seeded ambient-namespace canary. Role-mode equivalents run before their own resources are pruned. |
| `RealSmoke/adoption-witness` | Both engines: external version before/after Adopt and complete-ARN replacement after Adopt but before first dispatch; stale consent refuses without losing custody, fresh Plan/Adopt recovers once. Exact-ARN deletion is bounded; same-name reuse retries consume the shared ceiling. |
| `RealSmoke/promoted-replay` | Both engines: a real AWS promotion followed by locally injected failed Finish or marker refusal; durable Owned/pending witness and job identity survive worker reconstruction, crash-window closure and metadata-only replay without duplicate versions. Local faults are not AWS IAM behavior. |
| `AWSRealFixture*` | Local manifest, principal, protected-file, exact-ARN, expiry, cumulative shared creation ceiling, and single-use run marker regression tests. These are not provider acceptance. |

The production API remains closed and contains no value-read operation. External
positive and negative test oracles alone can sign `GetSecretValue`, confined to
recorded synthetic ARNs. Positive reads use the separate reader identity; negative
probes use the exact writer identity to require an AWS AccessDeniedException.
They add no production method or read grant, discard unexpected success bodies,
and never log values. Cleanup signs seed/edit/metadata/delete with its own identity.
Production client closure tests remain mandatory; a harness wrapper or IAM
template alone does not prove the actual role's effective no-read policy.

Cleanup force-deletes only recorded complete ARNs, then polls metadata for up
to one minute per ARN until not found. AWS deletion is asynchronous. Failure
is a failed run requiring operator reconciliation, not successful cleanup.
Cleanup may continue after the mutation window only under its separate approved
expiry. No prefix sweep, force-delete-by-name, or delete/recreate race assumption
is used. A process kill can bypass test cleanup; retain the approved namespace
and provider-side inventory for independent reconciliation.

## Full acceptance still required

1. Complete final integrated review and required local checks. Provider, fixture
   guards, both-engine durable/authenticated cases and TLS Moto checks have run;
   exact results and preserved failures are recorded in the current handoff.
   Those results remain separate from real AWS execution.
2. Execute the compiled real IAM denials, same-target partial
   failure, plan-time version/ARN replacement and post-promotion durable replay
   cases, auth-boundary negatives, destination-move cleanup, cold-process replay
   and retained multi-target proof against real AWS. Their sources compile, but
   successful provider execution is still outstanding.
   Fresh-client auth recovery is not same-client cached-role-session refresh;
   that longer refresh scenario remains an explicit additional coverage gap. The KMS-denied actor is a separate negative, not isolated proof of its
   no-read policy. Test source or local coverage is not real AWS evidence.
   Do not manufacture throttling by flooding AWS; use controlled local failure
   injection for deterministic retry and label it separately from live service
   behavior.
3. Execute the real suite within the latest banked sandbox/spend bounds for the
   concrete manual generation,
   after final local checks/review and protected input availability. Preserve sanitized exact-head metadata: run ID,
   scope reference, account/region, principal matches, engine/mode/test outcome,
   recorded ARNs, unknown outcomes, replay version counts, cleanup results and
   an independent final synthetic-resource inventory. Do not retain values,
   descriptors, tokens, headers, or raw provider diagnostics.
4. Exercise CLI/API/WebUI create, inspect, credential replacement, plan, consent,
   pause/resume, move and retain/prune on both engines, including denied/error
   states and desktop/mobile UI. Product surface proof is separate from this
   service fixture. Inspect ordinary responses/audit/logs for credential leakage.
5. OIDC and mail helpers were prebuilt for the browser runs. Reconcile
   accepted implementation scope, conditional release:1.0/release:decision
   classification and the unchanged v1.1 milestone with the
   parent and #79 owner before the final ledger; labels do not discharge scope
   or review conditions. The AWS promotion is declared amendment 8 at line 30
   of the current `mvp-boundary.md`.
6. Preserve the accepted #158-only B disposition and historical formal SKIP.
   Perform independent final-candidate code/security review (including these
   post-R3 additions), complete all required checks, make signed+DCO
   commits, obtain GitHub Verified signatures, and proceed through protected PR
   review/CI to a verified main merge. Parent owns PR registration/watch.

The dedicated sandbox and bounded execution are authorized. Controlled local
evidence closes only its recorded checks; successful live AWS execution, final
review, exact-head CI and verified feature merge remain outstanding.


## Local provider validation and schema ordering

The provider package's static import closure reaches only adapter, awssm,
awssmtest, crypto and netpolicy within this repository. It does not import
store/service/isolation or generated SQL bindings. The full provider package
can therefore be validated independently before migration integration, once the
parent allocates the bounded Go micro slot. Its enabled-role constructor test
isolates SDK files/environment to dummy inputs; wire tests use loopback HTTPS
or replacement transports. The historical source-phase provider-only run passed 56 top-level tests, zero failures/skips,
after preserving a launcher verification failure and repairing one stale
plan-witness expectation. These results exclude this isolation suite and all
then-new API/CLI/browser/process/move/auth drafts. The October 18 integrated
provider run passed 57 top-level tests (155 including subtests), with no skips
or failures; the current handoff supersedes the historical execution limits.

The owner authorized migration 77 for the exact AWS witness column on October
18, superseding the previous 80 reservation and prerequisite sequence. After
main occupied 77, the owner approved moving only the witness to 78 on October
19. Main subsequently occupied 78 and 79. The October 21 integration therefore
moves the unmerged witness to 80, preserving main's authorization indexes (77),
project capacity (78) and remote pairing (79). Both AWS SQL files are
byte-identical to the reviewed candidate; canonical SQL generation and fresh
SQLite/PostgreSQL compatibility catalog checks passed against integrated main.
Database validation is tracked in the issue handoff. No unsigned
prerequisite migrations were copied from sibling trees.

The browser source in `web/e2e/flows/aws-adapters.flows.ts` is registered from
existing `machine-access.spec.ts`. Its AWS development peer runs the actual
AWS module with controlled metadata and simulated credentials. It exercises
forms, consent ceremonies, ordinary-response redaction, persisted routing and
pause/resume, adoption, and retain/prune request choices. This remains controlled
browser evidence, not real remote teardown. The authenticated API/CLI fixture
passed all eight engine, surface and destination-mode combinations. Historical
October 18 web validation passed typecheck, lint and 209 unit-test files (1,915
tests). The October 21 integrated dependency checks passed typecheck, lint and
217 unit-test files (2,016 tests); see the
[handoff](../handoff/158-aws-secrets-manager.md#october-21-candidate-base-refresh)
for that evidence, rendered-browser results and current source review status.

## Child-process replay custody

The parent launches the already-built test executable, not a new Go build. A
bounded anonymous stdin pipe carries the original test root and database DSN;
argv and environment contain neither. The writer descriptor stays sealed in SQL
until the child uses the normal loader. Oracle and cleanup credentials are not
passed. The child repeats the signed development admission in Boot mode with
migrations disabled, opens the existing SQLite/PostgreSQL store, and rebuilds
its keyring and runtime. It accepts only the exact target/job, uses a distinct
worker identity, and refuses every CreateSecret reservation.

The first child exits with a dedicated code immediately after AWS accepts the
write, bypassing journal Finish and process defers. The second process runs after
the lease/crash window on the fixture clock and must finish that same durable
job. The parent asserts dispatched custody, unfinished intent, unknown crash
closure, successful replay and one new AWS version. Parent cleanup retains the
full exact-ARN registry; children cannot reset its creation counter. Child stdout
and stderr are discarded to prevent library diagnostics exposing DSNs or keys;
only bounded exit status reaches the test log. Each child has a 90-second parent
deadline and an 80-second test deadline. These sources have not run.
