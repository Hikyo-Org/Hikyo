# Handoff: #158 AWS Secrets Manager synchronization adapter

Issue: https://github.com/Hikyo-Org/Hikyo/issues/158. Built on the #157
multi-target seam (targets, outbox, health, pause/resume, retain-or-prune).
Current task authorization includes implementation of formerly post-1.0
issues. The ADR claims owner authorization on 2026-09-26, but the live #158
comment list contains no September 26 authorization comment;
that specific authorization reference is not independently corroborated there.
The August 24 deferral and unchanged v1.1 milestone remain visible alongside
the conditional promotion. Owner comment 6220607066 corrected the audit labels
to release:1.0 plus release:decision on October 16; this records conditional
relevance, not final promotion or acceptance. The parent/#79 owner retains
that conditional status in the release ledger. No review waiver or cloud-write authorization is implied. The ADR amendment is declared in `docs/adr/mvp-boundary.md` (declared amendment 8 and
the §4.1 row) with a banner on `docs/adr/deployment-adapter.md`; the
owner-approved review disposition below now governs both AWS portions.
Historical cross-provider SKIP remains; no additional formal pass is required.

## Failed live acceptance and offline repair, 2026-10-18

This section supersedes the execution and resource states in the historical
continuation below. Bootstrap [PR #968](https://github.com/Hikyo-Org/Hikyo/pull/968)
merged as `cb78b6be111029335e744aa3e4dcc956d5194ba1`. Feature
[PR #969](https://github.com/Hikyo-Org/Hikyo/pull/969) remains draft and issue #158
remains open. Standalone adoption-witness migration 77 is owner-approved;
main last verified before execution ended at 76.

The owner personally approved one protected-environment job:
[run 39158333194](https://github.com/Hikyo-Org/Hikyo/actions/runs/39158333194),
attempt 1, candidate `3856fcca58c86e972ea54c493e50c277f78d09aa`, workflow main
`cb78b6be111029335e744aa3e4dcc956d5194ba1`. Exact approval and PostgreSQL startup
passed. Real-provider acceptance failed: all 46 expected results were reported,
with 11 passes, 35 failures, zero skips and zero missing results. Identity and
genuinely expired-credential preflights passed; functional acceptance did not.
Raw test diagnostics were discarded by the trusted runner, so its artifact
cannot establish the exact failed assertion or provider error.

Subsequent read-only CloudTrail inspection recovered all eight unrecorded
CreateSecret errors: InvalidRequestException with a diagnostic referring to
providing ClientRequestToken. Each request lacked that token and both value
fields. The event records also contained eight complete response ARNs. Every
recovered exact ARN was checked through owner-console metadata navigation and
redirected to the empty, unfiltered Frankfurt list. This is additional UI
reconciliation, not direct API absence proof. No raw provider message,
credential or request value was retained. CloudTrail corroborates the token
defect on these eight requests; it does not explain all 35 failed test results
or prove that the repaired candidate passes AWS acceptance.

The run conservatively reserved 21 of the approved 32 cumulative synthetic
creations, including failures and pre-create attempts. Thirteen complete ARNs
were recorded and all thirteen verified absent. Eight reservations had no
recorded ARN; each exact name was separately checked through owner-console
metadata navigation and redirected to an empty, unfiltered Frankfurt list.
These UI checks are not direct API ResourceNotFound evidence. No unrecorded
ARN or prefix-wide sweep was deleted. Artifact ID `12148766232`, evidence JSON
SHA256 `7343dc9e422ed737fff8efd7e43b37075365869fb1b11413362326d86e90f718`.
Its complete-creation-ledger flag remains false; retirement does not turn
failed acceptance into success.

Exact cleanup removed eight roles and their inline policies, the created OIDC
provider, the setup user/login and exact MFA device, both managed policies,
the KMS alias and the dedicated GitHub environment. The temporary setup CLI
session was logged out, its isolated cache removed and its private browser
closed. Root MFA and three preexisting service-linked roles were retained.
The single KMS key is PendingDeletion with an exact seven-day window and
scheduled deletion at `2026-10-25T19:57:36.420Z`; physical deletion is pending.
Actual gross billing is not yet verified against the USD 1 procedural target.

Offline regressions exposed two independent source defects. The real-provider
fixture clock could lag services' wall-time enqueue timestamps and falsely
report an empty drain. The fixture now floors its clock at wall time on every
iteration while preserving explicit future replay advances. Real SQLite and
PostgreSQL claim/authorization/settlement regressions cover both cases.
The raw CreateSecret request also omitted ClientRequestToken. It now carries
the existing stable job/target/generation/name token. Local wire and retry
regressions require the token and preserve metadata-only creation, with both
SecretString and SecretBinary absent. These fixes do not expand provider
permissions. CloudTrail corroborates the eight token-related create rejections;
the clock defect's contribution to this particular live run remains unproven.

No live rerun or new fixture is authorized by these repairs. The original
one-time execution, fixed deadlines and cumulative count are not reset.
Required feature CI, successful live acceptance, current-head review and
verified main merge remain delivery gates. Ordinary CI uses simulated AWS.
No recurring AWS tests, paid-plan upgrade, release or deployment is enabled.

## Historical continuation evidence, 2026-10-18

The preserved R7 candidate was integrated onto main commit
`7bef4d670d2dce0defb963326f0e1e05cfdb6798`. Its original 55 changed paths,
132 source hashes and 31 evidence hashes were verified before integration.
The concurrent changes to `machine-access.spec.ts` were reconciled by retaining
main's file-target lifecycle and adding the AWS flow registration. The original
draft and historical failed runs remain preserved. R7's independent Sol-high
SOURCE_SOUND and Astra-high verification apply to that frozen source candidate;
the integrated, formatted and repaired candidate still needs final review.

Fresh local provider execution passed all 57 top-level tests (155 including
subtests), with no failures or skips:

```sh
GOMAXPROCS=4 go test -p 2 -count=1 ./internal/adapter/awssm
```

The first focused CLI run exposed an incomplete move fixture: it omitted the
target rows that the actual renderer displays. The fixture now returns the
requested pending route, and assertions require the move handle, state, target
and destination. No production renderer behavior was changed. The original
failure and a subsequent fixture compilation correction remain recorded.

The parent reported web typecheck, lint and all 209 unit-test files (1,915 tests)
passing. The first lint failure identified a `finally` return that could hide
browser-test failures; the repaired shared cleanup helper tolerates only an
already-deleted adapter. These checks do not establish rendered-browser proof.
The OIDC and mail helper binaries were built from the integrated checkout.

Focused AWS CLI checks passed all nine top-level tests (31 including subtests),
with no failures or skips, after the fixture repair.

The owner authorized migration 77 on October 18, replacing the old migration 80
reservation. Both SQL files remain byte-identical to the reviewed witness
migration. Canonical `go tool sqlc generate` completed against this checkout.
The SQL adds one column to `adapter_conflicts`, introduced by migration 24;
there is no technical dependency on the previously reserved migrations.
Canonical development compatibility metadata was also regenerated from real
SQLite and PostgreSQL 18 catalogs. The initial PATH-selected PostgreSQL 14 run
could not satisfy the repository's PostgreSQL 18 control catalog; that failed
attempt and its generated artifact remain preserved separately. The committed
metadata uses PostgreSQL 18.6 in UTC.

The actual authenticated API/CLI lifecycle now passes all eight combinations of
API/CLI, JSON-object/per-key and SQLite/PostgreSQL. It exercises TLS,
cookie/CSRF, WebAuthn purpose-bound consent, CLI browser handoff and bearer
rotation, sealed SQL custody and the worker over controlled AWS HTTP. Fixture
repairs added canonical HTTP IDs, corrected WebAuthn route names, and gave the
unchanged production limiter the same controlled clock as authentication;
separate operator decisions occur outside its sliding window. It remains a
controlled integration test, not real AWS or rendered-browser evidence.
The combined focused backend run passed 37 top-level tests (206 including
subtests) across app, server, store, migration and isolation packages. TLS Moto
and both database engines were configured. The two explicit skips were the
real AWS smoke entry point and its replay-process child entry point. The
repository Go import check passed. The broader Go suite and browser checks
are recorded separately below.

The first broader Go run passed 118 packages and 12,714 test cases; 29 packages
had no tests. It was not a full pass: the old legacy-upgrade fixture capped its
schema at 76, reviewed SQL pins needed the new witness projection, and a source
loader caught an intermediate test-helper compile error. Those failures are
preserved. The unsharded isolation package reached the default ten-minute
alarm; CI already partitions its full inventory into dedicated shards. The
legacy fixture now enumerates migration 77, refuses stored witness evidence,
and reverses only the pristine column before proving the pinned legacy catalog.
The SQL pins retain the scope guard, with both-engine tests strengthened to
require an exact witness only while its adoption consent remains pending.

Browser execution exposed an invalid fixture name: `browser/adopt-config` ends
with the forbidden AWS ARN-like suffix. The valid fixture uses
`browser/adoption/config`. Shared service target normalization now refuses
invalid AWS routing as a caller-correctable error before any ceremony or
provider call. A separate worker-first regression reproduced the development
peer creating an owned resource before Plan. The explicit unowned marker now
seeds its foreign metadata before either first Sync or Plan, after authorization
and manifest validation, and never on teardown. No production ownership rule
was changed. The focused repaired checks passed 32 cases, including denied-gate,
invalid-manifest, missing-journal and teardown cases. Original failures and an
intermediate negative-fixture correction remain preserved. The affected store,
isolation and lint rerun passed 29 top-level tests (180 including subtests),
including both engines, Moto, all eight authenticated surface combinations,
the strengthened consent/witness predicates and the reviewed SQL guard. Its
two explicit skips remain the real AWS smoke and replay-child entry points.
Full service and lint package runs passed 823 and 319 cases respectively;
lint retained one explicit skip. The app run caught a second legacy-fixture
omission: migration 77 also had to be removed from the fixture's Goose history.
That failure remains preserved. The repaired legacy proof and routing regression
then passed all 13 focused cases, including both database engines. The full app
rerun passed 183 top-level tests (394 including subtests) in 356.932 seconds,
with two explicit skips for the release-signing and child-process entry points.

The parent browser run passed both lifecycle modes and both adoption modes.
Its final invalid-path check exposed a missing caller-safe detail carrier:
HTTP 400 was correct, but the UI could only show a generic refusal. Routing
normalization now uses the existing safe-detail error type. The HTTP regression
requires the useful path message and absence of the supplied credential canary.
The rebuilt UI app includes that repair. The parent desktop rerun passed all
five AWS flows, and the mobile rerun passed the same five flows. These rendered checks
use the controlled development peer and do not establish real AWS behavior.

The owner reports AWS signup and MFA complete and specifies `eu-central-1` for
all AWS connections. The operator verified MFA and the dedicated Free account and approved the
bounded resource/spend/deletion proposal. Non-root setup, protected private
inputs and successful local checks/review remain prerequisites to execution.
The final integrated source review identified stale setup instructions and
execution-status text. The operative provisioning contract now requires the
owner-created default-policy KMS key, immutable fixture boundary, constrained
non-root setup identity with MFA, and seven exact OIDC trusts plus the eighth
source-only AssumeRole trust. The rejected escalation-capable setup route is
explicitly superseded. These documentation repairs still require reviewer
verification.
No AWS resource or credential writes have occurred. The separate trusted-runner
bootstrap was committed as `47a4e0471f9d84fdcf36d50981e85c6e899374d0` and
opened as [PR #968](https://github.com/Hikyo-Org/Hikyo/pull/968), with signature
verification and DCO checked by the parent. Its CI and merge are still pending;
it does not establish feature acceptance. The integrated feature candidate
remains uncommitted and requires final review, real AWS evidence and CI.
Historical draft sections below describe their
original evidence boundaries; these continuation facts supersede their local
execution status and migration sequencing instructions.

## Shape

- **Provider** `aws-secrets-manager` (`internal/adapter/provider.go`), third in
  the closed set. `internal/adapter/adapter_test.go` pins the provider set and
  now also pins which destination kinds each provider accepts.
- **Destination kinds** `json-object` and `per-key` (`internal/adapter/aws_names.go`).
  Stored columns keep their generic meaning:

  | Column | AWS meaning |
  |---|---|
  | `destination_owner` | 12-digit account id |
  | `destination_name` | json-object: secret name. per-key: optional path prefix ending in `/` |
  | `destination_environment` | optional customer KMS key (applied on create) |
  | `destination_id` | the account number, from STS; ledger namespace |

  Because the KMS key, mode, name, and path live in the routing columns, any
  change is a destination move and gets scrub-before-switch for free.
- **Origin** is the regional endpoint `https://secretsmanager.<region>.amazonaws.com`
  (also `-fips` and `.amazonaws.com.cn`). Any other https origin (VPC endpoint,
  emulator) must carry `region` in the descriptor. Workload-identity modes
  always use AWS's regional STS (it receives the node's projected token or a
  replayable signed AssumeRole); only `static` may set `sts_origin`.
- **Errors**: origin, descriptor, and destination refusals are
  `awssm.ConfigError` (safe detail); the app factory joins `domain.ErrInvalid`
  (API 400 with the detail) and, for a disabled workload identity, also
  `adapter.ErrProviderAuth` so the worker fails the job instead of retrying.
- **Credential** is a sealed JSON access descriptor (`internal/adapter/awssm/auth.go`):
  `ambient`, `assume-role` (role ARN, external id, 900-3600 s), `web-identity`
  (role ARN; token file comes only from the server's `AWS_WEB_IDENTITY_TOKEN_FILE`),
  `static` (access key id + secret key; the only secret-bearing mode). Strict
  decoding; decode errors never echo input.
- **Node policy** `HIKYO_ADAPTER_AWS_WORKLOAD_IDENTITY=allow|deny` (default deny,
  node-scoped, managed-node key). Modes that borrow the server's AWS identity
  are refused without it, so a project admin cannot act as the server's role.

## Client and closure (`internal/adapter/awssm/client.go`)

Hand-rolled awsJson1.1 over a vetted `http.Client` (public-egress dialer with
the operator CIDR policy, TLS 1.2+, no proxy, no redirects, 1 MiB response cap,
deadline below the lease). SigV4 via `aws-sdk-go-v2/aws/signer/v4`; credential
sources via `config`, `credentials`, `credentials/stscreds`, `service/sts`
(promoted from indirect). `service/secretsmanager` is deliberately not a
dependency. Closure is pinned three ways in `client_test.go`: the exact `API`
method set, the exact signed-operation registry, and an AST scan of the
production package refusing any identifier or string literal naming
`GetSecretValue`, `BatchGetSecretValue`, `GetRandomPassword`,
`ForceDeleteWithoutRecovery`, or `ListSecretVersionIds`.

Error mapping: AccessDenied/KMS Encryption/DecryptionFailure/expired or invalid
tokens -> `ErrProviderAuth` (class `auth`); Throttling/TooManyRequests/
RequestLimitExceeded -> `ErrRateLimited` with a `RetryAt` from `Retry-After`
(default 5 s), class `provider_limit`. `LimitExceededException` is service-quota
exhaustion: a definite refusal with no throttle retry deadline. Other 4xx are
definite failures; 5xx and transport
errors ambiguous. Provider messages are dropped; only the closed error code is
kept.

## Module semantics (`internal/adapter/awssm/module.go`)

- Every attempt first proves the account (STS) and every DescribeSecret ARN
  proves account and region; mismatch is `ErrDestinationID`.
- Create: `CreateSecret` without a value but with `MANAGED_BY_HIKYO=<target>`
  and the KMS key, then `PutSecretValue(ClientRequestToken, VersionStages=[HIKYO_PENDING])`.
  `UpdateSecretVersionStage(AWSCURRENT, MoveToVersionId=<token>, RemoveFromVersionId=<observed current>)`
  conditionally promotes the staged value. Only after promotion does Hikyo move
  `HIKYO_CURRENT` and record `TagResource(HIKYO_VERSION=<token>)`. AWS automatically
  makes its first-ever value current even with explicit custom stages; a missing
  predecessor still refuses promotion if a concurrent writer became current.
- Concurrent drift between metadata inspection and either staging or promotion
  fails the conditional promotion, preserving the external value and previous
  Hikyo ownership markers. This requires the metadata-only
  `secretsmanager:UpdateSecretVersionStage` permission. There is no value read.
- Token = sha256(job id, target, generation, name): stable across retries of
  one outbox job (`SyncRequest.JobID`, set by the worker), new per job. A
  landed replay is detected (AWSCURRENT == token with HIKYO_CURRENT,
  HIKYO_VERSION, or this exact job's HIKYO_PENDING marker) and skips the write,
  repairing only missing ownership metadata. A staged but unpromoted version
  does not authorize overwriting a newer external current version. An ambiguous
  first adoption of an existing foreign value can require a fresh version-bound
  consent rather than assuming the current foreign version is still the one reviewed.
- Drift: only the labels of the version holding AWSCURRENT are consulted, plus
  the `HIKYO_VERSION` tag. moto drops custom labels from superseded versions
  while AWS keeps them; the rule holds under both (tested both ways). Consent to
  overwrite an external edit is `HIKYO_VERSION=<that version id>`, bound to that
  version only.
- Ownership: untagged + Reserved -> `Refuse` (conflict artifact, adoptable);
  untagged + Owned (adopted) -> tag, then write; tagged for another target ->
  conflict; pending deletion and ours -> `RestoreSecret`.
- Per-key outcomes are independent: conflicts, definite failures, and
  ambiguous outcomes are reported per name and the loop continues; auth, rate
  limit, destination move, and journal/gate errors stop it. Prune runs only
  after every desired name converged.
- Prune/teardown: `DeleteSecret` with `RecoveryWindowInDays=30`; already
  deleted or missing is success. A secret tagged for another target is never
  deleted.

## Store and migration

`00067_aws_secrets_manager_adapter.sql` widens the provider CHECK and the
destination-kind CHECKs on `adapter_targets`, `adapter_route_move_targets`,
`adapter_route_move_claims`, `adapter_ledger`. SQLite rebuilds five tables
under `legacy_alter_table=ON` with every current column (including the 00040
multi-target columns) and recreates `adapters_active_origin` and
`adapter_ledger_active_provider_name`. `internal/store/migrate/aws_adapter_migration_test.go`
seeds pre-00067 rows and proves they, the partial unique index, and the child
FKs survive on both engines.

`internal/buildcompat/development.json` was regenerated against the CI-pinned
`postgres:18@sha256:06cad38a...` (the diff is exactly the version-67 entry and
`schema_sha256` per engine). Regenerate it again after any change to 00067.

AWS-aware name collision (`refuseAWSNameCollision`, `reserveAWSMoveClaims`):
one account and region is one namespace across both kinds, so claims are
computed per target (`adapter.ClaimedNames`) and compared case-insensitively.
AWS kinds never reuse the CI sentinel pseudo-names, so two json-object targets
with the same (empty) prefix in one account are allowed.

## Surfaces

- API: `AdapterDestinationKind` += `json-object`, `per-key`; `AdapterProvider`
  x-extensible-enum += `aws-secrets-manager`; field descriptions updated. No
  new fields or routes. `apigen` and `clients/ts` regenerated.
- CLI: `--kind json-object|per-key`, `--secret`, `--kms-key`, and `--aws-auth`
  with `--aws-role-arn`, `--aws-external-id`, `--aws-session-seconds`,
  `--aws-region`, `--aws-access-key-id`, `--aws-sts-origin` on `adapter create`,
  `adapter update --origin`, `adapter credential set`, `adapter target add`.
  The static secret key only ever arrives via no-echo prompt, `--stdin`, or
  `--value-file`. Help golden updated.
- Web: provider option, `AwsAccessFields` (descriptor in sensitive state),
  AWS kinds in `TargetForm`, AWS-aware target labels. Sensitivity inventory
  re-pinned with a review note.
- `adapter target show --format workflow` renders a names-only consumption
  snippet for AWS (`adapter.ConsumptionForTarget`).

## Tests

- `internal/adapter/awssm`: module behaviour against an in-memory AWS double,
  wire lifecycle against `awssmtest` (in-process signed Secrets Manager + STS
  emulator), error classification, descriptor contract, closure.
- `internal/isolation/adapter_aws_e2e_test.go` (both engines, always runs):
  real service/store/outbox/worker, sealed descriptor, snapshot decryption;
  both modes delivered byte-exact, cross-kind collision refused, value-blind
  plan, one new version per publish, external edit refused with drift
  attention, teardown with recovery window, no value-read on the wire,
  value-blind audit, INTENT/OUTCOME linkage.
- `internal/isolation/aws_external_e2e_test.go`: the existing emulator
  lifecycle, concurrency, replacement, and interrupted-adoption contracts.
- `internal/isolation/aws_real_durable_test.go` and `aws_real_fixture_test.go`:
  draft real AWS service/outbox tests with separate writer, synthetic oracle,
  and cleanup roles. Both datastore engines are mandatory. Protected manifest
  input replaces the former raw-key environment variables and shared identity.
  One real entry point shares a cumulative 32-creation-attempt guard across
  both engines, auth modes and cleanup seeding. A persistent local single-use
  marker refuses same-namespace reruns in the protected fixture directory.
  These drafts have not been compiled or executed. See the full
  [sandbox acceptance procedure](../testing/aws-sandbox.md), exact policy
  templates, secure-input prerequisites, and explicitly uncovered real cases.

## Open items

- The owner explicitly approved route B for #158 on 2026-10-16: independent
  GPT-6.1 Sol high substantive source review plus Astra-high verification for
  BOTH original Anthropic AWS amendment `0171da3f1` and OpenAI #844 AWS
  concurrency/IAM/replay/custody/adoption changes introduced by `6b1e8552`.
  No further quota-gated formal run is required. Preserve historical formal
  SKIP and provider provenance; resolve findings and recheck the candidate.
  Substantive R1 found an initial-consent race and a fixture deadline bug;
  R2 found a replay regression in the consent repair. Source fixes and
  regression drafts are present; R3 independently verified all 38 pinned
  files and returned SOUND for the scoped source review. Astra independently
  rechecked the hashes and repair paths. No runtime acceptance is claimed.
  Package-review SOUND is not substantive or runtime acceptance.
  Real AWS acceptance, CI, signing/DCO and verified merge remain mandatory.
  This #158-only disposition does not authorize resources, spending, release,
  Cloudflare, other ADRs or a global quota/provider-policy change.

- Account signup was reported completed by the parent, but MFA enrollment,
  actual plan and a non-root setup identity remain unresolved. No fixture
  region, roles, key, secure delivery or test writes are approved. The real
  suite has not run. Local config metadata showed only unrelated
  S3-compatible profiles;
  no usable authorized AWS credentials were established. Organization-secret
  metadata was inaccessible, so its contents remain unknown.
- A secure delivery path for local descriptor/token files must exist before
  requesting secrets. Runtime, cloud writes, account/policy provisioning, and
  spend remain held. The manifest documents scope; it does not grant it.
- The dev fake provider (`internal/app/adapter_fake_dev.go`) treats AWS kinds
  generically; AWS browser drafts are registered through machine-access.spec.ts; no AWS-specific pinned-surface claim or runtime proof exists.
- `ListSecrets` cannot be resource-scoped in IAM; per-key planning and
  connection tests need it.


## PR #828 integration and adversarial review, 2026-09-27

Merged main's sealed-webhook and SSH changes while retaining AWS workload
identity opt-in, access descriptors, CLI routes and WebUI fields. Reserved
migration 00067 on both engines; provider constraints retain sealed-webhook.
The AWS credential parser previously accepted a complete JSON object followed
by an unmatched `]` or `}`. A reproduced regression now requires EOF after
the one descriptor, without returning raw credential bytes on errors.

Validation: AWS provider and CLI tests, SQLite/PostgreSQL AWS migration tests,
provider/restore-drill app tests, AWS isolation tests, Web typecheck/lint and
1171 unit tests. Generated API/client/config inventory and empirical build
compatibility were refreshed. Native cross-provider review skipped: session
quota was still unknown after the policy response window. Ordinary review
completed for this integration; remote CI and lower-PR integration remain
required before merge.

## Final feature-stack integration (2026-09-27)

The ordered schema now includes Cloudflare 62, Vault 63, PKI 64, transit 65,
temporary access 66 and AWS 67. AWS widening retains all prior provider and
kind constraints. The empty legacy fixture reverses the combined adapter
kind widenings once, retaining its exact legacy schema check.

Validated the combined app/store/service/isolation paths on both engines,
all provider and CLI tests, API/authz/conformance, generated TS 20 tests,
and 1206 web tests with typecheck/lint. Final PKI documentation adds no
runtime behavior; its reviewed sensitivity inventory is retained.
Native cross-provider review remains skipped pending session quota evidence;
the requested reviewer is Astra 6 at medium effort.

## Current source-draft handoff, 2026-10-16

The real suite draft adds SQL-backed replay after rebuilding the worker/runtime,
role authentication, KMS denial, live conditional-promotion races, adoption,
and recovery-window lifecycle checks. This original worker-reconstruction case was not process-restart proof; the later cold-process draft is described below.
Source guards and policy templates are reviewable; runtime checks, complete
surface acceptance, real IAM negatives, expiry/replacement/move cases,
completion of the approved substantive review route, CI, and PR merge remain
outstanding. No further quota-gated formal run is owed for these two AWS portions. Do not close #158 from these
files, prior merged implementation PRs, or historical emulator results.

Issue #589 owns the narrow amendment-7 relocation/wording integration in
`mvp-boundary.md`; this branch does not edit that file. The parent coordinates
integration while preserving the adapter rows and amendments.

## Adoption-consent repair draft

Plan now records metadata-only consent as canonical `[complete ARN, AWSCURRENT
version]`, including the AWS replacement suffix. The conflict artifact and
worker ledger retain this witness through the existing destination, generation,
adoption and successful-effect fences. A missing legacy AWS witness requires a
fresh plan; Vault's numeric version witness is unchanged. The first adoption
write refuses an external version or replacement introduced after Plan, whether
before or after the Adopt request. Empty-current resources remain ARN-bound.

A successful provider promotion can precede a missing journal outcome. Pending
Owned adoption may resume only on the same complete ARN when ownership and
current-version markers prove this exact job token is current. That retry uses
the existing metadata-only repair path; it cannot rebase consent onto a foreign
version, another job, replacement ARN, or malformed/missing witness. Unit and
both-engine service/SQL retry drafts cover definite marker failure and failed
Finish with crash-window closure, exact-once versions and preserved custody.

Migration `00077_aws_adoption_witness.sql` persists this witness on both engines.
The owner authorized this standalone number on October 18 in place of the old
80 reservation. The SQL is unchanged from the reviewed candidate; canonical
sqlc generation has now run. No unsigned sibling migrations were copied.
Compilation and database/runtime proof are tracked in the continuation above.


## Post-R3 source-only fixture additions

The former migration 80 reservation is superseded above; original R1-R3 hashes/history remain
unchanged. Later source drafts add writer IAM read/scope denials on exact
synthetic ARNs, same-target partial conflict/recovery, live plan-to-dispatch
version/replacement cases and locally injected interruption after real AWS
promotion on both engines. The fixture retains old cleanup ARNs while selecting
the latest same-name identity. The global 32-attempt guard is unchanged; nominal
creations were 29 in that snapshot and are now 31 after two move destinations. A separate narrow Sol source audit found no confirmed new
findings in these five fixture files; it does not extend historical R3 SOUND
or establish compilation/runtime acceptance.

AWS browser form/ceremony/redaction/routing/pause/teardown-choice drafts are
registered through the existing machine-access spec. The harness replaces the
provider factory and therefore cannot validate AWS delivery or descriptor
parsing. No new surface-registry claim is made. Node checks and browser runs
remain held. API/CLI completion, real credential expiry, destination moves and
process restart remain explicit gaps.

Static import tracing confirms the awssm provider package has no generated-store
binding dependency. Its enabled-role constructor test now isolates SDK input
files/environment. The requested bounded provider-only Go micro slot can run
before the signed migration chain; it grants neither canonical generation nor
database, live AWS or UI execution. Root's independent narrow witness trace found
no new defect but was explicitly not a whole-source or runtime verdict.

## Whole-owner source reconciliation after provider validation

The narrow plan regression and full awssm package passed on the pinned source
set: 56 top-level tests, zero failures/skips. Preserve both preceding failures:
the offline launcher stopped during toolchain checksum selection; the direct
run then found one stale plan expectation (55 passes, one failure), repaired
with an exact ARN/empty-version witness plus JSON non-disclosure assertion.
These results do not cover later isolation/API/CLI/browser additions.

Later source drafts add two actual child processes for hard-exit/replay using
the original admitted store/root, exact job pinning and create refusal; both
engines exercise per-key-to-JSON scrub/activation and two-target retain. Expired
static/source sessions and workload tokens use authentic protected sidecars,
expiry-specific STS refusals and fresh-client identity recovery. Same-client
cached role-session refresh remains an explicit additional coverage gap.

CLI dispatch tests cover both kinds and all four modes, stdin replacement,
KMS moves, names-only consumption and errors. API tests cover routing/status
serialization and real descriptor refusals through the safe renderer. Browser
drafts cover four-mode credential replacement and raw plan/connection redaction.
The transport/fake seams do not prove actual AWS, full authenticated API/CLI,
or desktop/mobile runtime behavior. No new production defect has been asserted
from these unexecuted drafts. The full source matrix is
[aws-acceptance-matrix.md](../testing/aws-acceptance-matrix.md).

Setup now lists exact proposed IAM/KMS/OIDC action scopes and escalation limits.
The account's MFA/private setup, actual plan, non-root authority, secure delivery,
resource/spend/destructive approvals and private non-root inputs remain
explicit prerequisites. The owner selected eu-central-1; migration sequencing
was independently resolved as recorded above. No source review or local provider pass authorizes them.

## Post-R5 authenticated surface and fixture source

R5 SOURCE_SOUND remains bound to its original50-file inventory. The later
provider57 run includes controlled same-client cache refresh/refusal, with zero
failures/skips; it does not execute later application/isolation/browser drafts.
See `docs/testing/aws-acceptance-matrix.md` for the new authenticated API/CLI
matrix, actual AWS-module development peer and browser adoption, durable real
credential expiry/revocation cases, complete dev-only boot/switch boundary and
secure file-input versus unavailable private-delivery distinction. These additions
require a new full source review and allocated formatting/compilation/execution.
The original32 create-attempt cap is unchanged. No cloud writes are authorized.
