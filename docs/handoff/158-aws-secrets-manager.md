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

## Merge queue repair, 2026-10-30

Merge-group run 41119940247, attempt 1, tested
`b08f85d20eb6ba1f7c3ca260b01a72b236353376`: base
`142b0916e3479d4a6e40e1e1602d23cdf197a2ae` and PR head
`5ef53b13cc92b33f6c281afe80dc461ccf45f3d6`. The run failed: 43 of 48
jobs passed; three leaf jobs and two aggregate/proof jobs failed.

- Preflight reported the missing explicit `v4` signer import alias in
  `internal/adapter/awssm/awssmtest/server_test.go`. The pinned repository
  goimports check reproduces it; the alias is restored without behavior changes.
- Isolation shard 0 and web-go failed during Go cache restoration, before their
  test steps started. Both job annotations report runner disk exhaustion;
  their log downloads return BlobNotFound. Passing sibling jobs restored the
  same approximately 1.9 GiB compressed compiler archive. These failures do not
  establish failing isolation or transport tests.
- Every shared compiler-cache consumer now reserves 20 GiB on the actual Go
  cache filesystem before restoration. Only disposable GitHub-hosted Linux
  runners may reclaim the two fixed, unused Android/.NET SDK directories.
  Go/Node toolchains, Actions caches, Docker resources, source and artifacts
  are preserved. Insufficient capacity or failed measurement/deletion refuses
  the job. No existing test, runner, cache key/authority or job deadline changes.
- Offline fixtures cover the hosted-runner boundary, capacity/no-op threshold,
  actual cache-filesystem selection, exact cleanup paths and permanent failures.
  Workflow mutation checks require the exact unconditional preparation before
  all eight shared-cache restores. The frozen coverage baseline remains intact;
  only this exact additive preparation is normalized after validation.

Hosted execution of the repaired candidate and a new current-base full-suite
merge group remain required. The failed run remains failed. No live AWS run,
credential change, provisioning, spending, release or deployment is authorized.

## Current merge disposition, 2026-10-30

The owner explicitly directed PR #969 to be merged, remaining issues repaired,
and no more AWS runs. This supersedes this PR's earlier requirement for another
successful real-AWS acceptance run before application merge and its separate
human merge hold. It grants no AWS execution, provisioning, credential change,
cleanup operation, spending, release or deployment authority.

The failed c11 result and independent cleanup below remain historical evidence.
Successful real-provider acceptance is still unproven. Issue #158 remains open
for that acceptance gap; merging this repair does not complete its acceptance
criteria. Reusable controller qualification remains in #1057. No additional
owner-assisted AWS review round or provider run is requested.

Current-main integration retains main's migration 80 (`auth_refusal_window`)
and moves the unmerged AWS witness and lookup indexes to migrations 81 and 82
on both engines. The compatibility declaration is regenerated from actual
SQLite/PostgreSQL catalogs, and the legacy archive fixture retains all three
additions and its evidence refusal checks. Current source Standards and Spec
review, signed/DCO commits, exact-head feedback and mandatory current-base merge
queue validation remain required. Controlled loopback tests are local evidence.

The current-base lifecycle check exposed a stale fixture login: all eight
API/CLI, naming-mode and engine cases attempted password-only CLI sign-in after
passkey enrollment. Main correctly refuses that path. The fixture now asserts
that refusal, follows the actual cookie/CSRF-authenticated browser approval and
PKCE loopback redemption, and checks passkey assurance on a distinct CLI session.
Adapter-operation reauthentication and its bearer-rotation assertions remain.
This repair changes test drivers only, with no production authentication change.

## Prior c11 status, 2026-10-28

Issue #158 remains unfinished. The c11 owner-assisted attempt is closed after
failed real AWS acceptance and independently verified cleanup. Reusable
controller qualification remains #1057. No further AWS attempt, resource,
reservation, window or application merge is authorized by the closed attempt.

Run [40600194878](https://github.com/Hikyo-Org/Hikyo/actions/runs/40600194878),
attempt 1, tested application `361e02e3915eef840429d88589c39689f4a58dfb`
with trusted workflow/main `24e198b32a3ffd8d4013207b2f974d76ec49be6f`.
All 46 expected records are terminal: 43 passed and three failed. There is one
failed leaf, PostgreSQL lifecycle; the other failures are its lifecycle and
suite parents. Assertion 227 means "durable AWS target did not converge:
conflict". The bounded evidence does not identify its exact lifecycle point.
Its evidence SHA-256 is
`fc7845e26914f43a0fea943fc8375f9e9d5152c58895e2c42c3c6f689f38e398`.
Other recorded web-identity and replay leaves passed. The whole AWS gate failed.

All 27 acknowledged ARNs and two unacknowledged reserved names independently
returned strict NotFound. Complete regional inventory including planned
deletion was empty after 335.922 seconds of terminal quiescence and in final
readback. The eight exact roles, owned issuer, boundary, alias and nine consumed
protected variables are absent. Original setup v5 is restored; v2-v5 and their
dates are preserved, with actual new v11 retained nondefault. Setup identity,
MFA and its sole attachment are unchanged. The seventh new key is
PendingDeletion for `2026-11-03T23:50:52.008Z`, not physically deleted.
Every prior schedule remains unchanged. The canonical closure SHA-256 is
`72867f7734ad0c74551ec75b75f705c9ba90570b6361b331c17a4775c468d041`.

The c10 reassigned pool remains charged: 29 creation attempts and three unused
slots closed without refund or automatic reassignment, zero new reservation
charge and 304 lifetime charges. Its sole hosted attempt is consumed. All prior
reserves remain. The final timestamped billing proof retained a USD 6.19 usage
projection within the approved USD 6.30 ceiling; it is not a settled final bill.

A local service/adapter reproduction found that one or two stale consent-tag
observations after an acknowledged manual tag update produce the same conflict
assertion on both engines. This matches the failure category; it does not prove
the precise cause of the c11 failure. The fixture now observes the one consent
write before draining the queued retry. It reads only the recorded ARN, at most
three times within the same 15-second deadline covering tag and observation.
It preserves owner, current-version, Hikyo-marker, key and deletion witnesses.
Persistent staleness, a changed witness, provider refusal and cancellation fail
loudly. It never repeats the tag mutation.

The local protocol emulator also now models AWS's automatic AWSPREVIOUS move
when PutSecretValue moves AWSCURRENT. Its signed-wire regression covers default
and explicit current staging, idempotent replay and a custom staged version.
All provider traffic in these reproductions is loopback-only. The final local
fixture selection passed 208 terminal records with SQLite and PostgreSQL,
including bounded negative controls, with no failures or skips. The affected
adapter/emulator consumer selection passed 218 terminal records. Production
adapter code, dependency locks, the closed provider API, genuine expiry,
46-record hosted contract and creation accounting are unchanged.

Five of five model reviews remain consumed, with no independent CLEAN or sixth
review claimed. The correction needs concrete owner source disposition before
any further AWS run. Any such run also needs a fresh explicit grant for its
consumed attempt, key, reservation and budget limits, followed by the retained
Ready, exact-policy-hash and pending-deployment gates. Final current-base CI,
the separate human application merge decision and actual issue closure remain
outstanding. No new clock starts from this source repair.

## Prior c09 result and repair, 2026-10-27

Issue #158 remains unfinished. The c09 attempt is closed after failed real AWS
acceptance and independently verified cleanup. Reusable controller qualification
remains #1057. No additional AWS attempt or new resource authority is inferred.

Run [40510334025](https://github.com/Hikyo-Org/Hikyo/actions/runs/40510334025),
attempt 1, tested application `f44f9fd717c1980fddc11ac6690a8a6e6e311104`
with trusted workflow/main `9d620dd451a56408ccab8360283dfeef0320eac7`.
All 46 records are terminal: 34 passed and 12 failed, including parents.
Four leaves failed: PostgreSQL lifecycle and web-identity role auth
(`assertion-066`), PostgreSQL version-before-adopt (`assertion-017`) and SQLite
marker-refusal replay (`assertion-160`). These categories do not establish their
actual causes. Both replacement-after-adopt leaves passed.

Artifact `12738459949` has ZIP SHA-256
`871c0828cc36ec1b0ad12c3b0d29d4dc145e42dffab7b32dbfe3590b2a30c3d4`;
its validated `evidence.json` SHA-256 is
`122be28ffe19fd13600e3b5e9da9a7f64ceff8a2381ef5ae1ae584d6ca70f85f`.
It retains `complete_creation_ledger=false`. All 27 recorded ARNs and two
additional reserved-name targets were independently strict-not-found. Complete
regional inventory including planned deletion was empty after terminal
quiescence and in the final read. Cleanup does not turn failed acceptance green.

The eight exact roles, owned issuer, boundary and c09 alias are absent. Nine
execution variables were removed while environment and OIDC protections remain.
Original expired setup v5 is restored, with v2-v5 and new v9 documents and dates
preserved. Setup identity, MFA, sole attachment and absence of keys/groups/inline
policies were independently verified. The fifth new key is PendingDeletion for
`2026-11-03T14:19:45.807Z`, not physically deleted. Six prior pending schedules
and two previously deleted keys were unchanged. Final cleanup receipt SHA-256:
`4db0330e33816d30041f9904c2dfad0c70acb0a00040ea22944d5151b0fee434`.
The 32-slot pool is closed without refund, with 29 reservation attempts and three
unused slots; 272 lifetime reservations remain charged. All prior cost reserves
remain retained. The timestamped final bill showed USD 0.07 gross; the retained
usage projection was USD 4.53 against the approved USD 4.60 ceiling.

Focused local reproductions identify three fixture defects matching the failed
categories, without proving the real run's causes:

- Replay advances the logical queue clock beyond lease/retry horizons. Comparing
  that clock with the real authorization deadline refused a live queued retry.
  The fixture now gates against real time while each mutation independently
  checks the unchanged real expiry. The original drain attempt budget remains.
- A transient read can require a third loader entry for the same job and consent
  witness. The fixture counts distinct delivered tokens/versions rather than
  requiring exactly two loader entries. The observer still checks job and
  witness identity on every load; the one-version predicate is unchanged.
- An acknowledged schedule or restore can precede its metadata observation.
  The fixture observes only the exact recorded ARN, at most three reads in the
  existing 15-second deadline, before asserting the requested deletion state.
  Persistent mismatch, foreign identity, permanent refusal and cancellation
  remain failures. No mutation repeats to obtain an observation.

The complete offline AWS-fixture selection passed 178 parent/subtest records,
zero failures or skips, with SQLite and PostgreSQL enabled. Complete-diff Go
application feedback and isolation vet passed. Dependencies, production adapter
code, genuine expiry checks and creation accounting remain unchanged.

The PostgreSQL web-identity failure remains unattributed. A companion trusted
runner change prepares six closed convergence-class identifiers, 225-230, in the
existing v2 artifact shape, preserving identifiers 001-224. Source publication
must pass its separate human gate before these identifiers can be retained by
a future hosted run. No raw target state, provider message or credential is
published. These local changes do not establish successful provider acceptance.

Five of five model reviews remain consumed, cross-provider unavailable. No
independent CLEAN or sixth review is claimed. Changed source needs a concrete
owner disposition. Any further AWS execution needs a new explicit grant for
its consumed run, key, reservation and budget limits, then the retained Ready,
exact-policy-hash and pending-deployment gates. Final application merge still
requires successful real acceptance, independent cleanup, mandatory current-base
CI and the separate human merge decision. No new execution clock starts here.

## Prior c08 result and c09 source preparation, 2026-10-27

Issue #158 remains unfinished. The owner selected bounded, owner-assisted AWS
acceptance; reusable controller qualification belongs to #1057. Historical
authorizations below are retained evidence, not fresh execution grants. Final
application merge still requires the separate human decision, successful real
acceptance, verified cleanup and mandatory CI for the actual current-base pair.

The latest real run, `40485386416`, attempt 1, tested application
`327dec159d680fab7b33a251ab46a2d1c16e9fa1` with trusted workflow/main
`9d620dd451a56408ccab8360283dfeef0320eac7`. It captured all 46 expected
records: 40 passed and six failed, including parents. Two leaves failed:
SQLite lifecycle (`assertion-066`, durable convergence) and PostgreSQL
replacement-after-adopt (`assertion-013`, fixture metadata read). These are
observed assertion categories; the artifact does not establish their causes.

Independent cleanup confirmed all 27 recorded ARNs and two additional attempted
names absent, complete namespace inventory empty, eight roles, issuer, boundary
and alias absent, nine execution variables removed, and the exact expired setup
v5 restored. Versions v2-v5 and owned v8 remain retained with their dates. The
c08 key is PendingDeletion for `2026-11-03T10:51:15.444Z`, not physically deleted;
all prior schedules were preserved. The 32-slot pool is closed without refund,
including three unused slots, at 240 charged lifetime reservations. Its sole
run and fourth-key allowance are consumed. No retry, new key, cancellation,
reservation, deployment or renewed execution window is authorized.

Two local regressions reproduce the same assertion categories. An established
delivery followed by a transient metadata 500 leaves the durable job correctly
scheduled for retry, but the fixture asserted convergence before draining it.
An acknowledged replacement followed by one metadata NotFound also failed its
one-shot observation. Neither reproduction proves the real run's root cause.
The fixture corrections:

- Drain only an existing queued network/provider-limit retry, within the
  original 24-attempt budget and manifest expiry. Authentication, conflict,
  ambiguity, refusal, pause and operator-attention states remain failures.
  This advances the fixture's logical queue clock; genuine STS expiry still
  requires the unchanged real-time provider check.
- Observe only the recorded complete ARN, at most three DescribeSecret reads
  inside the existing 15-second deadline. Only NotFound is retried; different
  identity, permanent refusal and cancellation remain failures. Existing safe
  DescribeSecret diagnostics now retain a bounded failure category.

The complete offline AWS-fixture selection passed 162 parent/subtest records,
zero failures or skips, with SQLite and PostgreSQL enabled. Controls verify
persistent-error budget exhaustion, cancellation, identity binding, no value
reads, unchanged creation accounting and no duplicate delivered version. The
complete-diff Go application feedback lane and isolation package vet passed;
dependency locks remained unchanged. Production code, trusted workflow,
acceptance assertions, creation ceilings and real-time expiry checks were not
changed. Local fixtures are not provider acceptance or hosted current-pair CI.

Five of five model review rounds are consumed; cross-provider review remains
unavailable. No independent CLEAN or sixth review is claimed for this increment.
Executing materially changed sources requires a concrete owner disposition.
Any further provider attempt also requires an explicit exception to the closed
run, key, reservation and budget grants, followed by the separate exact-policy
and pending-deployment approvals. Prepare and validate source before starting
another expiring clock. Preserve the failed runs and canonical cleanup receipts.

## Historical status, 2026-10-22

The latest banked owner directive is **A, accept runs until green but below $15 billable**.
Controlled manual attempts until real AWS green are authorized within the
reviewed bounded plan. Both gross and billable exposure, including remaining
completion reserves and unreflected taxes/fees, must stay strictly below USD 15;
aim below USD 5. Credits cannot erase gross usage. This is admission budgeting,
not an instantaneous provider hard cap. Unknown costs hold new spending, not
necessary approved cleanup. Automatic retries, recurring jobs, paid upgrades
and unrelated resources remain outside the authorization.

All operations target account `099256073061`, region `eu-central-1`.
Each attempt permits at most 32 synthetic secret creations, one replacement
KMS key, the same eight roles, one temporary boundary and at most one OIDC
provider. The first banked generation retains 176 reservations and adds 32,
for 208 conservative lifetime reservations and at most one dispatch. Namespace
`hikyo-e2e-20261022c05/` and its fixture and cleanup ledger directories were
unused at the supplied handoff checkpoint: no lease, admission, reservation or
claim had been made. Later manual generations require genuine outcome/history
and a freshly reviewed concrete derivation. Never refund reservations, reset
claims or replay consumed plans after failure.

The preceding c04 CreateKey admission failed freshness and its exact abort
cleanup completed. No aliases, roles, boundary, OIDC provider, secrets or
dispatch were created by that failed admission. Its key is scheduled for
seven-day deletion at `2026-10-28T23:31:42.338Z`. Preserve all four existing KMS
deletion schedules. The historical synthetic secret was already confirmed
absent; do not repeat its deletion without new exact evidence.

Retain the setup identity, sole named MFA device, account, login sessions and
protected GitHub environment until #158 is complete and satisfied. The approved
setup-policy sequence deletes only obsolete nondefault v1, activates bounded
v6, and restores existing self-service v5 after exact retirement. The fixed
scope horizon remains `2026-10-26T12:22:56Z`; fixture leases are at most 24 hours,
with the existing shorter authorization/cleanup windows and startup buffer.
Fresh identity, effective-policy, inventory, cost and protected-job readbacks
remain required before activation.

The earlier official CLI identity and attached-MFA readback is an October 21
snapshot, not current access proof. On October 22 the user reported the old T3
pane signed out and explicitly requested Chrome with Playwright. The parent
opened headed Chrome session `hikyo-158-complete`, observed the correct
account/user, and the human confirmed signing in with the existing MFA.
Official CLI access refresh and actual AWS admission/acceptance remain pending.
Browser sign-in and listed MFA enrollment do not prove MFA-backed CLI access or
real-provider success. Source-only preparation helpers and their reviews remain
operator artifacts outside Git and create no execution authority.

Historical real execution did occur: run `39158333194` reported 11 passes and
35 failures across 46 expected results; later run `39495071346` failed before
its real suite. Successful real AWS acceptance remains unproven. Preserve those
failed outcomes and exact cleanup evidence rather than describing the suite as
never run.

Historical full candidate run `39653750185` passed 48 jobs at PR head
`44ef87300646ecb0d67370e92c33af44f7056eb8` against base
`e886fa491f66c3a9fdfc3b0104a8958c04b2f34b`. Signed merge commit
`c64a4f977e6ea27a2842fff90f506ffeafc24c01` combines main
`70a34271ea9415946048866914b325321c5626d5` and that PR head, without conflicts,
and was pushed to PR #969. The PR remains unmerged to main. Fresh validation
of the final head/base pair is required after the documentation changes.
All 15 GitHub review threads are resolved,
but the aggregate review still says changes requested; this is not final review
approval.

The user now explicitly authorizes eventual merge after the required gates.
Real AWS acceptance, exact cleanup, final candidate review/CI and verified merge
remain mandatory. Human protected-deployment approval is a distinct gate and
is not supplied by resource authorization, browser sign-in or CLI Allow.
The dated sections below preserve historical source, validation, authorization
and resource snapshots; this checkpoint supersedes their earlier limits and
pending merge-permission statements.

### October 22 continuation validation

The continuation combines application source from PR head `44ef873` with main
`70a34271` and reconciles the checkpoint, authorization and review-route prose.
The complete-diff shared feedback driver passed: 55 changed Go files were
formatted, the affected Go owner packages compiled, web typechecking and lint
passed, and all 2,016 unit tests in 217 files passed with at most two workers.

Focused Go execution passed 166 provider checks, 107 owner checks across CLI,
service, server, store and development-provider packages, and 190 isolation
checks. Both SQLite and PostgreSQL 18.6 participated, including authenticated
API/CLI lifecycle coverage. These executions had no skipped tests. The first
local owner batch exposed an incorrectly initialized SQL_ASCII test database:
the IDNA case failed there and passed unchanged against UTF-8. The disposable
database setup was corrected to UTF-8 before the complete owner and isolation
batches passed. No production code or assertion was changed to obtain that pass.

This is local controlled-provider evidence. Successful real AWS execution,
exact cleanup, final integrated-candidate review and current head/base CI remain
required before merge or issue closure.

## Historical revalidation preparation, 2026-10-19

The owner requested continued work until AWS acceptance passes and reuse of
access while #158 remains open. The previously retired setup identity and
fixture have not been recreated. The saved owner Playwright session was
recovered by navigating its blank tab, then verified against account
`099256073061` in `eu-central-1`. No login cookies or credentials were exported.
This owner session is for setup and read-only investigation; acceptance still
uses the dedicated non-root roles. Billing showed USD 0.00 estimated charges
on the Free plan, which does not establish final gross usage.

The owner approved the reusable numerical/resource package: at most three
additional manually approved runs, 32 creations each,
96 additional and 144 lifetime reservations, one new key, the same eight roles,
at most one OIDC provider and the dedicated protected environment. It also
includes one cleanup-role-only force deletion of the exact historical pending
secret below before any new suite creation. The combined USD 1 gross usage
target, existing key deletion schedules and human deployment approval remain.
This explicitly raises the former lifetime creation ceiling from 53 to 144;
the 48 previous reservations remain consumed. Root recorded the decision and
the previous ledger hash separately. No additional run has executed, and no
new AWS fixture or effective policy has been deployed. Each exact protected
job still requires human deployment approval. Retention grants no recurring
run or automatic retry authority.

Local revalidation replaces labelled-version counts with exact sent-token,
current/predecessor, owner and completion-marker checks. DescribeSecret omits
unlabelled versions, so an unchanged map size can still represent one correct
new delivery, consistent with the [AWS DescribeSecret response contract](https://docs.aws.amazon.com/secretsmanager/latest/apireference/API_DescribeSecret.html#SecretsManager-DescribeSecret-response-VersionIdsToStages).
A bounded metadata-only child receipt preserves the same proof
across the existing hard process exit and fresh-process replay. Creation now
retains the validated response ARN before observing eventually consistent
owner metadata; it never repeats CreateSecret or refunds a reservation.
Untrusted successful identities remain explicit unresolved cleanup holds.
The [CreateSecret response contract](https://docs.aws.amazon.com/secretsmanager/latest/apireference/API_CreateSecret.html#API_CreateSecret_ResponseElements)
supplies the new ARN and name without a value read.

A controlled SQL/TLS regression also reproduces the deliberate Finish-loss
fault returning ErrSuperseded during settlement. The real fixture previously
rejected every drain error. Its narrow exception requires a worked job and
the confirmed explicit Finish-loss injector; ordinary worker, marker-refusal
and recovery errors remain refusals. These are local fixture observations,
not a passing real-provider rerun or an explanation of every old failure.

The repaired provider passes 166 checks in each normal and race execution.
The final focused fixture race run passes all 60 records with no skips,
including actual Finish-loss recovery on SQLite and PostgreSQL 18. The normal
full lint gate passes 377 records, with only the intentional reviewed-pin
updater test skipped; vet, whitespace checks and the AST graph refresh pass.
The historical AssumeRole/SQLite failure remains unresolved. These local
results do not replace real AWS acceptance of the next reviewed candidate.

The trusted runner now requires a fresh protected run prefix with no fallback
and has an optional, exact-ARN historical cleanup phase. That phase uses only
verified cleanup-role credentials, durable intent/evidence before at most one
force-delete transmission, and bounded read-only absence checks. Ambiguity
holds before the new suite starts. Its 37 offline tests, actionlint and AST
checks pass, with independent security review finding no remaining material
source issue. The pure local reuse-policy/admission draft has 16 passing
offline cases and independent source review; actual durable orchestration,
proof inputs, effective-policy readbacks and real acceptance remain open.
The trusted-runner [bootstrap PR #1006](https://github.com/Hikyo-Org/Hikyo/pull/1006)
must merge separately before main can execute this capability. Source merge
and approved live execution remain separate gates.

## Earlier continuation and completed retirement, 2026-10-19

This section supersedes the resource and authorization states in the historical
sections below. Issue #158 remains open and feature PR #969 remains draft.
The second approved real-provider execution,
[run 39221282489](https://github.com/Hikyo-Org/Hikyo/actions/runs/39221282489),
attempt 1, failed against candidate
`2682f59f4dbd7680c6647f6979c4270f230d37c5` and trusted main workflow
`911efa1e7eb85e43f5ab681625383911ccaaf93d`. The owner personally approved its
protected-environment deployment; automation did not approve it. All 46 expected
result records are present: 24 pass, 22 fail, zero skips and zero missing.
These counts include containers. Evidence JSON SHA256 is
`1e3b5c372a924cc71a221a3c469dc2d5d650cf7dd56f56ea2d5aeedd16a821e4`.
The reviewed diagnostics bootstrap, PR #999, merged as
`92080565e4d1bcdb33d217b2a77528975d89d2b5`.

The additional one-time authorization was consumed in account `099256073061`,
region `eu-central-1`: 27 new conservative creation reservations plus the prior
21 total 48 of the approved lifetime cap of 53. Unused numerical allowance does
not authorize another run. No further AWS acceptance run or resource creation
is authorized or running. The combined USD 1 gross usage target remains
unchanged; actual gross billing is unverified. No automatic retry, recurring
execution or paid upgrade is authorized.

All 24 runner-recorded secret ARNs were API-verified absent by the trusted
runner, with independent artifact inventory review. Both denied-create names
were separately checked through exact owner-console detail navigation and an
unfiltered list including scheduled deletion; neither exists. Those two checks
are UI evidence, distinct from direct API ResourceNotFound proof.

The remaining Web Identity/PostgreSQL secret was recovered as the exact ARN
`arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261018b/web-identity/postgres/role-document-vmN1Vj`.
It is bound to the replacement key below and PendingDeletion after an exact
seven-day deletion request at `2026-10-19T09:40:57Z`. The console's
"Deleted on 10/19/2026" text is a request-date label. Its final deletion timestamp
is not established, and absence is not claimed. Reviewed containment records
the ordering exception after scheduling that secret, retains its pending ARN,
and preserves `acceptancePassed=false`, `cleanupComplete=false` and
`syntheticCleanupProven=false`.

All eight owned roles, their eight inline policies, the created OIDC provider
and the exact KMS alias are verified absent. The dedicated GitHub environment
is deleted and its exact GET returns 404; repository OIDC defaults were not
changed. The replacement key
`arn:aws:kms:eu-central-1:099256073061:key/10908b8d-fe54-40e3-9210-a3eefea709b6`
is API-verified PendingDeletion with an exact seven-day window and scheduled
deletion at `2026-10-26T09:54:54.536Z`. Physical deletion is pending. The old key
remains scheduled for `2026-10-25T19:57:36.420Z` and was not targeted.

The setup CLI session is officially logged out, its isolated login storage is
removed and its temporary browser is closed. Credential values were not read
or retained. No verified non-root administrative CLI session for this account
was found in installed profile metadata. No unrelated profile credentials were
used. The setup console login profile is removed, fresh owner-console readback
shows its password not enabled, and access-key count is zero. Its sole managed
policy attachment is removed with fresh permission-policy count zero. Both
owned managed policies and all their versions are deleted; fresh exact-policy
console navigation reports each ARN not found. No new session-revocation policy
was created. The setup MFA association was deactivated with HTTP 200. A fresh,
complete HTTP 200 Unassigned-device inventory excludes its exact serial, and a
fresh exact-user check shows attached MFA zero and all credential counts zero.
The controlled cleanup chain permitted no Enable/reassignment. This retires the
known-deactivated device from the owned inventory; it is not an all-device
inventory or an observed DeleteVirtualMFADevice HTTP 200. Guarded cleanup
forwarded zero additional MFA deletions and no MFA creations. Its diagnostic
flags remain unchanged. The bare setup user is deleted; fresh exact-user
owner-console reload reports that `hikyo-158-setup` cannot be found. The initial
confirmation preparation was reconciled before completing the required written
consent; no duplicate deletion confirmation was submitted. Root MFA and
preexisting service-linked roles were not targeted. The final owner-console IAM
dashboard shows zero users, customer-managed policies and identity providers,
with the three preexisting roles retained. The owner Chrome session remains
connected.

All 31 current-run UpdateSecretVersionStage events were inspected through
allowlisted CloudTrail metadata. Exactly three failed: lifecycle SQLite and
PostgreSQL SHARED_KEY, and Web Identity SQLite. Each attempted AWSCURRENT
without RemoveFromVersionId and received InvalidParameterException for the
missing predecessor. The other 28 stage requests succeeded; no observed failed
stage request targeted HIKYO_CURRENT. These observations do not explain every
failed result record.

The reviewed local production repair in `internal/adapter/awssm/module.go`
performs at most three metadata reads of the original complete ARN, with
cancellable 100 ms and 200 ms waits. It skips promotion only after observing
this exact job's token current; foreign or persistently invisible current
versions remain conflicts without ownership-marker advancement. Every read
retains the authorization gate, ARN/tenant/scope checks and ownership/adoption
policy. Nonempty-predecessor compare-and-swap behavior is unchanged. No blind
promotion, secret-value read, write retry or permission expansion was added.

A separate value-metadata scan inspected 32 GetSecretValue event details and
retained 21 exact-scope projections: 20 successes and one failure. At 09:16:06
UTC, the approved oracle role's AWSCURRENT read of the AssumeRole/PostgreSQL
role-document returned ResourceNotFoundException immediately after its
successful HIKYO_CURRENT attachment. Source inspection identifies the positive
oracle assertion before negative external-ID checks. Its one-shot read failed;
logging through the root test pointer explains the suite-level diagnostic.
No later successful read of that exact ARN proves actual transient recovery.

The separately reviewed fixture repair in
`internal/isolation/aws_real_fixture_test.go` captures the exact recorded ARN
once and performs at most three oracle-only AWSCURRENT reads, with the same
cancellable 100 ms and 200 ms waits, only for ResourceNotFoundException.
Persistent absence still fails. Denial, decryption, server/transport errors and
the first visible mismatched value are not retried; exact caller value
assertions remain unchanged. The fix changes observation of documented read
visibility, not the production client's value-blind API, identities, policies,
expiry or creation accounting. Offline wire regressions demonstrate the old
failure and bounded recovery when the provider later exposes the expected
value. They do not establish that the actual failed run would recover.

Fresh local provider validation passed 60 top-level tests plus 105 subtests
(165 pass events) in each normal and race run, with zero test failures/skips.
Offline fixture race validation passed 15 top-level tests plus 46 subtests
(61 pass events), including all 12 new oracle test/subtest events. Two unrelated
PostgreSQL drain-clock subcases skipped because `HIKYO_TEST_POSTGRES_DSN` was
unset; they are not both-engine proof. Scoped vet, formatting and structural
Graphify refreshes passed. Independent security and specification reviews of
both local source repairs found no remaining material findings.

The reviewed local source SHA256 pins are:

| Repository path | SHA256 |
| --- | --- |
| `internal/adapter/awssm/module.go` | `c0878a5e72466d31eaa236d11e3948c1a53be4cd4cd35ce19c7d4d37b2f1f99a` |
| `internal/adapter/awssm/client_test.go` | `00083fa2eadf728e338d43ce2d4ea101f5cb7e2ccae75453820a4e82f0f120a2` |
| `internal/isolation/aws_real_fixture_test.go` | `6fa0ae972b70482be522f9fff4b50cf93c7c8b7e25662917ee94d7cb9378d3d3` |
| `internal/isolation/aws_real_oracle_test.go` | `2da9e550a7804e99d4bf8bab60219fe275838169eccdaa2a883cb1dd793c97eb` |

Main now owns migration 77 for `authorization_pending_indexes` from PR #1000.
The owner approved moving only this issue's standalone adoption witness to 78.
Integration uses main `eb991c991bcad853590cb0ab2c09367052a92c02`, preserving
both engine migration 77 files and the witness SQL bytes. Canonical sqlc
bindings and development compatibility metadata were regenerated from actual
SQLite and PostgreSQL 18 catalogs; an independent fresh-catalog check passed.
The legacy upgrade drill reverses witness 78 before the pristine indexes at 77
and retains its refusal to discard witness evidence. Reviewed SQL pins retain
all main records and only the three existing AWS query records differ.
Earlier main/migration integration checks passed provider, adoption store,
migrations, legacy drills, offline fixture diagnostics, local adoption/lifecycle
isolation, compatibility, full lint, scoped vet/import formatting and 17 Python
runner tests. The signed CLI drill passed with required cosign; full lint
skipped only its explicit opt-in pin updater. Those broader results and the
integration reviews are historical to that integration stage, not fresh proof
for the later source repairs or current-head remote CI.

Both repairs are published in signed/DCO commit
`35309b5b732db430d9f2f12182313e88a8915ad7`; GitHub reports Verified. The
live-tested candidate above is unchanged, and no live AWS run has validated
either repair. Successful real-provider acceptance, required cleanup completion,
current-head CI and verified feature merge remain open delivery gates. No
release or deployment is authorized by this work.

## Failed live acceptance and offline repair, 2026-10-18

This historical section records the first execution and retirement. The current
continuation above supersedes its subsequent authorization and resource state. Bootstrap [PR #968](https://github.com/Hikyo-Org/Hikyo/pull/968)
merged as `cb78b6be111029335e744aa3e4dcc956d5194ba1`. Feature
[PR #969](https://github.com/Hikyo-Org/Hikyo/pull/969) remains draft and issue #158
remains open. At that execution, the standalone adoption witness used migration 77 and
main ended at 76; the later owner-approved slot is 78.

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

These repairs alone did not authorize a live rerun or new fixture. The later
explicit owner approval above supplies the additional bounded scope. The original
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

On October 19, main already contained migration 77 for authorization and pending-read indexes (PR #1000). The owner then approved migration 78 for this standalone adoption witness. Both engine migrations retain the same SQL; canonical bindings and the development compatibility manifest have been regenerated against integrated main and checked against fresh SQLite and PostgreSQL 18 catalogs.

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
legacy fixture now enumerates the adoption-witness migration, refuses stored witness evidence,
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
omission: the adoption-witness migration also had to be removed from the fixture's Goose history.
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
  `--aws-region`, `--aws-sts-origin` on `adapter create`,
  `adapter update --origin`, and `adapter credential set` for ambient,
  AssumeRole, and WebIdentity. Static authentication uses the complete one-line
  JSON descriptor with `mode: static`, access-key ID, secret access key, optional
  session token and region through the no-echo prompt, `--stdin`, or
  `--value-file`, without any `--aws-*` flags. The removed identifier flag and
  `--aws-auth static` return a safe migration error before input or HTTP.
  Destination flags also apply to `adapter target add`. Help golden updated.
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
  Local guard and offline fixture tests compile and pass, as recorded in the
  October 21 section; successful real AWS execution remains outstanding. See the full
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

- The dedicated account, retained non-root setup identity/MFA and protected
  environment exist. The October 22 banked directive authorizes the bounded
  manual series recorded in the [current checkpoint](#current-status-2026-10-22).
  Official CLI refresh, effective-policy/inventory/cost
  admission and successful real AWS acceptance remain pending. Earlier real
  execution failed; it must not be described as never run.
- The trusted GitHub OIDC workflow and protected-file projection route are
  implemented and merged. Fresh run-specific private inputs, token renewal,
  authentic expired sidecars and separate human deployment approval still
  require execution. Browser sign-in does not populate fixture credentials.
  The manifest records scope; it does not itself grant authority.
- The dev fake provider delegates AWS kinds to the production AWS module over
  the metadata-aware peer in `internal/app/adapter_fake_aws_dev.go`; other
  providers retain the generic fake. Authentication and the remote API remain
  simulated, with no SDK client, STS call or network transport. AWS browser
  drafts are registered through `machine-access.spec.ts`; this dev peer does
  not establish AWS-specific pinned-surface or real-provider runtime proof.
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

Migration `00080_aws_adoption_witness.sql` persists this witness on both engines.
The owner replaced the old 80 reservation with 77 on October 18, then approved
78 on October 19 after main occupied 77. The SQL is unchanged from the reviewed candidate; canonical
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

R5 SOURCE_SOUND remains bound to its original 50-file inventory. The later
57-test provider run includes controlled same-client cache refresh/refusal, with zero
failures/skips; it does not execute later application/isolation/browser drafts.
See `docs/testing/aws-acceptance-matrix.md` for the new authenticated API/CLI
matrix, actual AWS-module development peer and browser adoption, durable real
credential expiry/revocation cases, complete dev-only boot/switch boundary and
secure file-input versus unavailable private-delivery distinction. These additions
require a new full source review and allocated formatting/compilation/execution.
The original 32 create-attempt cap is unchanged. At that source checkpoint,
cloud writes were not authorized; the current status records the later approval.


## October 20 current-main integration

Current main `ad65a00d9ba7dd316c7ac9759d1ce433de812f33` includes
migration 78 (`project_key_capacity`, PR #1008). The standalone, unmerged
AWS adoption-witness migration therefore moves to the next free slot, 79,
on both engines. Its SQL bytes and AWS behavior are unchanged. The migration
77 authorization indexes and migration 78 capacity columns remain intact.
The legacy drill refuses both recorded adoption witnesses and project capacity
evidence before reversing 79, 78, and 77 in order. Canonical sqlc generation completed without additional binding drift. Development
compatibility claims were regenerated and verified from actual fresh SQLite
and PostgreSQL 18 catalogs for this combined source state.

This source integration does not grant another AWS run or deployment approval.
Real-provider acceptance, exact cleanup, current-head hosted gates and the
human merge decision remain separate completion requirements.

Local validation of this integrated source passed: provider 166 records, store
adoption/capacity 72, migrations 35, offline fixture 51, and authenticated
isolation/adoption 52, all with zero skips. App upgrade/drill and actual schema
compatibility passed 46 records; the dedicated cosign CLI test remains skipped
without its supply-chain lane input. Full lint passed 377 records with its one
intentional reviewed-pin updater skip. Compatibility unit tests, scoped vet and
import formatting also passed. These local checks do not establish hosted CI
or real AWS acceptance for the resulting merge commit.


## October 21 merged-main integration

Merged main `fdbf911632e3143d76080bb6b1523133fa99cfce` includes the
trusted CLI framing repair from PR #1017, the Go 1.27.3 security patch, and
remote pairing at migration 79. The standalone, unmerged AWS adoption witness
therefore moves from 79 to the next free slot, 80, on both engines. Its SQL
bytes and AWS behavior are unchanged. Main's migration 77 authorization indexes,
78 project capacity policy, and 79 remote-pairing history remain intact.
The legacy drill preserves AWS witness, capacity, and manual-pairing evidence
checks, then reverses pristine 80, 79, 78, and 77 additions in order, including
their Goose history.

This integration changes the candidate source beyond the previously frozen
`391c4b053661e4d84569a304bafa8bbb521b7fc2` snapshot. Prior local validation
and hosted results apply to their recorded commits. Canonical sqlc generation passed without additional binding drift beyond the
combined merge. The development compatibility declaration was regenerated from
fresh SQLite and PostgreSQL 18.6 catalogs and reproduced byte-for-byte against
a second fresh PostgreSQL database. Independent review of the merge increment
found no remaining functional, security, Standards or Spec findings. Its one
stale migration-range comment was corrected to match the through-80 fixture.

Current local validation passed with Go 1.27.3: provider 166 records, adoption
store 34, migrations 35, offline fixture 41, authenticated lifecycle 58,
upgrade/drill and actual schema compatibility 46, and compatibility unit 11,
all with zero failures or skips. Full lint passed 377 records, with only its
intentional `TestUpdateReviewedPins` skip. Scoped vet and staged/unstaged
whitespace checks passed. The PostgreSQL 18.6 validation cluster was isolated
and stopped after the finite batch. These are local test records, including
subtests, not counts of real AWS cases. Exact-head hosted gates and actual
provider execution remain unproven for this integrated candidate.
The already approved single bounded AWS run remains unused; integration does
not launch a fixture or satisfy the separate human deployment gate. Real AWS
acceptance, exact cleanup, final feature review and human merge approval remain
outstanding. The setup identity and MFA remain retained as instructed.

## October 21 review repairs and full-CI cache inventory

The full candidate run `39563506604` at
`e2363eeee2e6058889e8c4ed45bc497a4bddbcfe` failed its isolation shard in
`TestInvariant12CacheDiscipline`: the new controlled credential-refresh test
was missing from the closed cache registry. Its registry entry now names the
fresh SDK cache constructor, synthetic per-client/subtest scope and authority
boundary. Only that exact declaration is registered; the detector and package
coverage remain unchanged. The focused invariant regression passes locally.
All required hosted gates must run again for the resulting commit.

The reviewed increment also fixes unchecked test Write/Close results and reads
fresh metadata after the same-token delivery retry. Fixture manifests accept
only positive finite supported budgets through USD 10; this follows the latest
owner ceiling and does not replace actual scope approval, the stricter trusted
runner's per-fixture budget, or the fresh conservative combined admission of
at most USD 3. Boundary tests reject nonfinite, malformed, zero, negative and
over-ceiling budgets. The production raw HTTP creation token remains present:
the cited tokenless development helper seeds its private in-memory map directly,
while the production module supplies its stable journal token.

The documentation now distinguishes historical source holds from recorded
local validation, uses migration 80, and records the retained setup identity,
MFA and protected environment. Current operator instructions require absent
fixture names before creation, new immutable-ID custody, the exact reviewed
tags, and the latest single-run resource, cleanup and spending limits.

The finite local review-fix batch passed with Go 1.27.3: provider 166 records,
all offline fixture helpers 132, and development AWS peer 31, with no failures
or skips. Scoped vet and whitespace checks also pass. These include subtests
and controlled provider seams; they are not real AWS cases. The new fixture
remains unprepared and the approved single run unused. Successful real AWS
acceptance, exact cleanup, fresh hosted validation and final human merge
approval remain outstanding.

## October 21 candidate base refresh

At `f295418fcf4453f274cdeb9d67ebb07be90132b2`, full candidate run
`39568587837` passed its validation jobs but failed the final parent-binding
proof after main advanced to `da6c622749cb6ef8cf9a984e9f4e4d9e4f4473f7`.
The tested synthetic merge still had base `fdbf9116`; the proof correctly
refused to attest it against the newer main. Main's intervening PR #1016 changes
only dependency manifests/locks and its CI handoff. No SQL migrations, Go
application code or trusted AWS workflow/runner bytes changed.

The new main merge preserves migration 80 and all AWS changes. Its incoming
files merge without conflict. Required validation and a fresh full candidate
run must establish the new commit pair; the prior successful validation jobs
are historical evidence. CodeRabbit rate-limited the requested review of
`f295418`; that review is unavailable, not a fresh approval. All twelve prior
inline findings have concrete resolved dispositions, and the independent
source reviews remain separately recorded. The approved AWS run remains unused.

The integrated web dependency checks passed locally: frozen install,
typecheck, configured lint and all 217 unit-test files (2,016 tests), with
at most two workers. Incoming dependency and handoff files match canonical
main byte-for-byte. The Go application, migrations and trusted AWS sources
remain unchanged from the preceding candidate. These checks do not replace
full hosted validation of the new head/base pair.

## October 21 Apple-signing main integration

Full candidate run `39575036694` completed successfully at
`8705128d6dedb0c885e49cd3e510599a29062b24`, against the recorded main base
`da6c622749cb6ef8cf9a984e9f4e4d9e4f4473f7`. Canonical main subsequently
advanced to `352c3d380841013afa03a02b63246d8892ebc78f` with PR #1018's
Apple-signing, release/nightly custody and associated documentation/tests.

That main integration merged without conflicts or custom resolution edits.
All 38 incoming paths match canonical main byte-for-byte. The AWS provider,
migrations, trusted acceptance workflow and runner sources are unchanged by
this integration. Focused source-equality and whitespace checks passed; the
already delegated broad suite was not repeated locally. The new commit pair
requires fresh hosted validation. PR #969 remains draft and outside the merge
queue; no feature merge, AWS fixture or deployment approval is implied.

## October 24 independent PR review repairs

The subsequent independent PR review at `1eb676a988a2976e7bb20e588df49ffc00ce21b3`
returned **pass with required changes before merge**. This is distinct from
historical frozen source reviews and does not discharge final review or real
AWS acceptance. The repair increment remains subject to fresh independent review
and current-head hosted checks. All operational, adoption, authentication,
provider and delivery holds remain in place.

An upgraded legacy pending adoption can retain the matching ownership tag but
have no provider witness. Sync correctly refuses that combination. Plan now
returns a Conflict with current resource/version metadata so the operator can
review a fresh scoped artifact and explicitly adopt it. Replanning performs no
writes, and neither the matching tag nor historical custody supplies consent.
The regression traverses the service, both datastore engines, outbox and worker.

Migration 81 adds scoped expression indexes for the existing plan and worker
adoption lookups in SQLite and PostgreSQL. It preserves every conflict/effect
record and migration 80's witness semantics. The development compatibility
claims and legacy upgrade drill must include this new migration. Historical
migration bodies and previously recorded source/test/report pins are unchanged.

Both write request shapes omit an empty ClientRequestToken and retain any
provided stable token byte-for-byte. The production module continues to supply
its deterministic journal token; omission does not generate an idempotency token
or establish that a tokenless raw HTTP request is accepted by AWS. The earlier
CodeRabbit development-seed call-graph claim was withdrawn and is not evidence
of a production caller sending an empty token.

Metadata inspection and creation receipts share exact resource identity
validation. The test-only oracle and fixture share one signed transport with
bounded responses, typed provider errors, redacted messages, no redirects and
explicit connection cleanup. The controlled credential refresh test retains its
assertions under `TestClientRefreshesExpiredCredentials`; its synthetic cache is
no longer an entry in the production registry, and the declaration sweep is
unchanged. Transient metadata failures abort planning without a partial artifact.

The historical USD 1 target, October 22 banked aggregate bound strictly below
USD 15, and executable fixture budget ceiling of USD 10 are different limits.
The validator still requires `0 < budget_usd <= 10`; the stricter trusted runner
and conservative admission limits still apply. This repair grants no spending
or execution authority and does not change owner approval, billing policy,
manual/protected AWS CI gates, retry timing or ADR review status.


## October 24 pending-adoption witness renewal followup

A pending first write can retain genuine consent after its ownership tag lands.
If the current provider version or complete resource identity then changes, Sync
refuses that stale consent. Plan now compares pending consent with the canonical
current metadata witness and surfaces a new conflict for explicit adoption.
Ambiguous metadata supplies no usable witness. Historical custody and a matching
ownership tag alone do not authorize a write; the existing exact-scope, generation,
reauthentication, consumption and same-job promoted-token replay checks remain.

This source repair requires fresh independent review. It does not supersede the
original full-PR review, establish real AWS acceptance, satisfy the ADR review
route, or grant authentication, adoption, provider execution or merge approval.
