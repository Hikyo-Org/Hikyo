# PR 844: DeepSec remediation

## Outcome

The original DeepSec finding set was remediated across the Go services,
CLI, web client, operator, deployment chart, CI, and release tooling. The
2026-10-01 refresh merges main through `6305b79a6` and adds the fixes below.
Final DeepSec revalidation and exact-head remote CI remain separate gates.

The final medium finding no longer sends CLI passwords or bearer credentials
over loopback TCP. Local CLI authentication now uses a Unix-domain socket with
strict path custody and kernel-reported peer UID verification on Linux and
macOS. Remote HTTPS retains certificate pinning. Unsupported platforms fail
closed and require pinned HTTPS.

## Operator impact

- Configure the server with `--cli-socket` or `HIKYO_CLI_SOCKET`.
- Create the socket parent as an existing, real, owner-controlled `0700`
  directory. The server creates the socket as `0600`.
- Pass `--socket` during local credential establishment. The binding is stored
  in the immutable trust entry.
- Re-establish legacy loopback-only trust entries before credential-bearing CLI
  requests. They remain readable for migration but cannot send credentials.
- After an unclean server exit, restart automatically reclaims a same-user
  `0600` socket only when its connection is refused. A private persistent startup
  lock serializes concurrent restarts. Live sockets, symlinks, and other objects
  are refused without removal.
- Argon2 cost changes are refused at startup and during runtime configuration
  replacement while current-epoch password credentials use different costs.
  Restore the previous Argon2 settings; select costs before establishing passwords.
- Kubernetes metrics stay loopback-only. The chart does not expose a scrape
  Service or support Prometheus scraping pod IPs. A separately configured
  same-pod collector can scrape loopback and forward via authenticated transport.

## Review repairs

- Legacy CRD objects compare absent creation policies as the `Owner` default.
- Pinned loopback HTTPS is accepted without a Unix socket; HTTP still requires one.
- Restore holds prevent certificate issuance and all fresh CRL signing.
  Existing CRLs remain readable. Signing resumes only after operator
  revocation reconciliation. Worker key use, signing and publication share
  one restore-admission guard; stale captured work cannot bypass the hold.
- Optional authenticator input no longer announces itself as required.
- CLI help names Forgejo federation refusal, workflow fixtures pin each protected
  condition, and fuzz classification tolerates toolchain diagnostics around JSON.

## Validation

### 2026-10-01 merged checkpoint

- Web: 1,431 unit tests, typecheck, lint and production build pass. Targeted
  desktop and mobile regressions pass 18 tests each. Light stories pass 71
  checks, with 14 targeted dark checks. SAML success is covered at the SPA
  unit and actual server-router boundary, not by a live external IdP login.
- Pending login/workspace proof retirement and protected import regressions
  pass on SQLite and PostgreSQL under the race detector. Both lock winners
  are exercised; old proofs cannot mint fresh-generation sessions.
- Restored-issuer CRL hold, existing-read, reconciliation-resume and
  restore-admission exclusion checks pass on both engines under race.
- Full compose, crypto, importer and CLI race suites pass at the client-state
  checkpoint. The importer commitment primitive now lives under
  `internal/crypto`; the crypto ownership gate and affected full race suites
  pass again after that move.
- Complete merged Go checks and forced DeepSec revalidation are in progress.
  Earlier counts below describe earlier checkpoints, not this head.

### Follow-up security checkpoint

- SCIM: all 66 top-level lifecycle/race cases pass on SQLite and PostgreSQL,
  including same-session org-A withdrawal with org-B continuity.
- Web: 1,454 units pass, with fresh typecheck/lint/build and real initial
  password/second-factor and passkey login document-refresh checks.
- Actual CLI online fetch, encrypted snapshot, unavailable owned Unix socket,
  offline rendering, durable receipt and authenticated reconciliation pass on
  both engines. The complete eight-case Compose CLI race suite passes.
- Receipt-forgery and blinded SCIM subject audit regressions pass on both engines
  under race. Hidden-secret value and occurrence changes no longer move a
  presence-only caller's cursor; visible value/presence changes still do.
- Supported archive restore destroys pending adapter move credentials and
  destinations on both engines under race. Reserved-prefix SQLite trigger
  admission and exhausted PostgreSQL sequence validation regressions pass.
- Native Windows custody execution passes on the pushed checkpoint in GitHub
  CI, including the six new protected-lock/temporary-file tests. This is runtime
  evidence, not merely successful cross-compilation.
- Frozen follow-up DeepSec revalidation completed all 67 verdicts: 64 fixed,
  one false positive and two duplicates, with no true positives or uncertain
  results in that batch. Final helper discovery and residual-policy checks are
  separate, ongoing runs; this is not a claim that the whole project is clean.
- Exact owner-runtime race checks pass all 18 executions, including live HTTP
  admission, password-cost refusal, rollback, drain and activation recovery.
- Final residual revalidation completed 21/21 verdicts: 17 fixed, one duplicate
  and three true positives. The true positives are the nightly credential
  custody gate and the two explicitly pending adapter policy decisions below.
- Six new protocol/platform helpers were investigated without new findings.
  Discovery also identified three additional bugs in HA receipt-key freshness
  and grouped access-rule mutation recovery. Their fixes pass targeted tests;
  final forced DeepSec revalidation records all six follow-up verdicts fixed,
  with no true positives or uncertainty. Exact-head remote CI remains pending.
- Two independent replicas pass receipt/fetch/reconciliation race regressions
  on both engines, three repetitions each. Rotation invalidates old receipts
  everywhere, with fresh cursor and change-token equality across replicas.
- Web: final 1,464 unit tests, typecheck and lint pass. Actual generated-transport
  rule recovery tests cover lost/malformed responses, partial deletion, session
  refresh refusal and stale cached controls during failed/deferred listing.
- Full local preflight passes in 111 seconds, including build, vet, format,
  generated contracts, client/web checks and chart/release fixtures. The fresh
  ordered Go core run passes all 135 package results, including its app tail.
- Final key-door discovery investigates four files without new findings. The
  isolated finding inventory contains 265 records: 255 fixed, three false
  positives, four duplicates and the three unresolved policy/custody gates.

### CI follow-up checkpoint

- The annotated SQL inventory now pins the canonical 959 entries. Review covers
  both dialects, the approved restore fences, pending-proof retirement and
  canceled credential moves; the inventory guard remains unchanged and passes
  three race-enabled repetitions.
- Cursor regression fixtures now follow the authorized manifest: losing secret
  disclosure changes its full commitment. A separate unchanged config-only
  projection isolates historical-pin cursor invalidation; read-only delivery
  isolates machine-reveal generation changes. Both engines pass three
  race-enabled repetitions without restoring hidden-content commitments.
- The real CI-pinned Moto lifecycle and four concurrent-write/explicit-consent
  cases pass three race-enabled repetitions. First-value publication proves
  the exact owned job version is already current before skipping a redundant
  empty-predecessor promotion; other writes retain the observed predecessor CAS.
  Test-only reads explicitly select AWSCURRENT to avoid Moto's stale default
  version cache. Production never gains a secret-value read operation.
- Final AWS repairs recheck durable authority before every provider request,
  preserve custody after partial writes and refuse unmarked replacement names
  without fresh scope-bound adoption. Untagged resources cannot be pruned.
  Writes and deletes use a verified complete ARN with exact name correspondence;
  deleting/recreating an empty name cannot capture a subsequent plaintext write.
  The real pinned-Moto replacement race and the actual grant-revocation flow on
  both database engines pass three race-enabled repetitions. Final forced AWS
  revalidation returns all five verdicts fixed, without uncertainty.
- Nonce-bearing SPA document checks assert `private, no-cache`, matching the
  shared-cache exclusion. The actual desktop and mobile deep-link flows pass;
  web typecheck, lint and 1,466 units pass. Rule settlement waits for the session
  owner's invalidations before confirming the authoritative listing, preventing
  canceled concurrent refetches from hiding successful add/remove notices.
  Session refusal still invalidates stale listings and suppresses success.
  The unchanged desktop and mobile add/reach/remove flow passes against the
  final embedded bundle; final rule DeepSec revalidation returns two fixed
  verdicts without true positives or uncertainty. Refreshed exact-head CI is
  separate and remains pending.
- The refreshed isolated inventory contains 267 findings: 257 fixed, three
  false positives, four duplicates and the same three pending policy/custody
  gates below. No uncertain or unrevalidated findings remain. Fast local
  preflight passes in 43 seconds; the full generated-code gate is rerun after
  committing the intentional SQL comment outputs. Final focused rediscovery and
  fresh-head GitHub checks remain separate delivery evidence.

### Earlier checkpoint

- Full Go suite: 8,485 tests passed across 134 packages.
- Isolation suite: 2,002 tests passed.
- Web: typecheck and 1,287 tests passed.
- Generated TypeScript client: typecheck and 21 tests passed.
- Helm, fuzz classification, Linux and Windows compile checkpoints passed.
- Commit signature, DCO, and diff hygiene checks passed.

Review-repair validation: 1,288 web tests plus typecheck/lint; password-cost,
CRL-hold and isolation invariant checks on SQLite and PostgreSQL; CLI and CRD
admission suites; 50 race-enabled socket test repetitions; Windows app/service
compilation; chart assertions and ShellCheck. Workflow mutation probes reject
widened conditions independently on the trusted validation job and step.

## Remaining gates

GitHub CI and human review remain required before merge. No merge authorization
is included in this handoff.

## Fresh finding repairs and compatibility

- SCIM changes remain org-scoped, including grant additions, deprovision/delete
  and lockout-retention cure. They do not kill instance-wide sessions or pending
  proofs. Each operation rechecks current grants immediately; unrelated org
  authority and manual origins survive. This policy amendment was explicitly
  approved on 2026-10-01 and is recorded in the SCIM and human-auth ADRs.
- Local sockets verify every ancestor and intermediate symlink target, including
  relative paths with `..`. Socket mode changes and cleanup are bound to the
  created inode. Compose snapshot/watermark updates serialize project writers;
  local key creation is atomic and retries recover interrupted creation.
- New import run artifacts use v2 keyed commitments. Regenerate old v1 run
  artifacts into a fresh directory; existing templates remain readable, and no
  existing artifact is modified or deleted. Plaintext input hashes are no longer
  published. Rotate low-entropy secrets if historical exposed hashes leaked.
- Mixed access-rule replacement refuses before any create/revoke until an
  atomic backend replacement exists. Pure additions/removals remain supported.
  Protected immediate imports require exact-key publish ceremonies. Canceled
  import/grant work cannot continue; untouched retention inputs follow fresh
  policy; SAML browser completion refreshes the authenticated session.
- Machine fetches share a 30/min, burst-60 principal token bucket across
  credentials, in addition to org/instance limits. AWS create collisions recheck
  current-version consent. Reused nightly images require source-SHA-bound
  verification before any execution and cannot be newly signed by a replay.
- Human API admission applies 300/min sustained, burst 600, per live human
  session. One request is charged once across retries and nested operations;
  authorization still rechecks current grants. Anonymous SPA documents no
  longer reveal configured remote origins in their CSP.
- API revision 9 requires authenticated per-value offline snapshot receipts.
  Unsigned legacy snapshots and records fail closed with online-refresh
  guidance; pending evidence is not deleted. A revoked serving credential may
  still reconcile through a live same-account presenter with its valid receipt.
  Receipts prove prior server delivery, not the client's asserted later use.
  Audit events label authenticated delivery with `receipt_verified: true` and
  retain the receipt-bound revision and keyed snapshot change token.
  Issuance and reconciliation use one database-selected, transaction-locked
  scoped key snapshot, not a replica's cached handle. The cursor, change token
  and receipt remain purpose-separated and observe the same active key.
- SCIM subject audit commitments retain the v1 64-hex format but use fresh
  private HMAC keys, preventing bare-hash dictionaries and cross-event equality.
  Existing historic audit disclosures cannot be undone by this source change.
- Windows trust/session custody verifies owner, DACL and reparse-point state;
  atomic temporary files receive a protected DACL before their first byte.
  Passphrase archive restore caps age's scrypt logN at 18 (256 MiB), matching
  Hikyo exports. Larger untrusted headers are refused before key derivation.
- File-sync retries finish interrupted pruning/collection without removing the
  current generation or foreign files. Identical legacy generations republish
  once to establish durable predecessor metadata.
- Operator scrubbing requires a matched canonical delivery-refusal marker,
  not an arbitrary JSON 404. Older unmarked servers fail safe by retaining data.
- AWS publication promotes the observed version using an exact previous-version
  compare-and-swap before advancing Hikyo ownership markers. Concurrent external
  values survive. Ambiguous first adoption requires fresh version-bound consent.
- Vault marker repair is allowed only for unconsumed explicit adoption provenance
  bound to the current scope, destination and target generation. Ordinary owned
  entries never adopt a later unmarked value. The operator approved replacing
  ambiguous automatic recovery with an explicit review hold on 2026-10-01.

The operator approved both adapter policy amendments on 2026-10-01. Their
implementation replaces the earlier two adapter policy residuals above:

- Each outbox execution attempt has a two-minute deadline and a bounded
  five-second durable-settlement grace. Parent shutdown cancels both. Retries
  continue indefinitely with the existing backoff; infrastructure timeouts are
  not misclassified as authorization denial. Successful per-name progress is
  reused only for the same job, generation and immutable worker-bound input
  revision, proven by the scoped INTENT audit record. Legacy records without
  that witness do not authorize skipping delivery.
- Vault ambiguous dispatched writes stop for operator review. A coincidentally
  matching version or marker cannot prove Hikyo wrote it. Custody remains held
  across resync; a fresh conflict plan and version-bound explicit adoption
  authorize a new CAS attempt, not ownership of a previous unknown write.
- AWS interrupted legacy adoption preserves the operator-approved predecessor
  before staging. Lost ownership tags on a held name are surfaced by a fresh
  conflict plan, so explicit re-adoption is possible without releasing custody
  or overwriting a concurrent foreign version.
- GitLab adoption records the real destination scope. Migration 72 pauses
  legacy targets with incorrect blank-scope claims, marks them for operator
  review and releases only those claims. It does not copy ownership or delete
  provider data. It also detaches exact old-generation terminal job pointers
  without rewriting their outcomes or touching provider effect fences. For
  interrupted moves, it holds the containing move at attention-required and
  retires its pending sibling jobs. An operator can cancel the move, preserving
  the repair pause, then explicitly resume with fresh full-environment consent.
- Service mutation retries refresh their security clock inside every new
  transaction. GitHub recipient preflight consumes the org concurrency slot
  and one principal rate charge before provider contact, including requests
  whose later reauthentication ceremony refuses the mutation.

Vault best-effort token cleanup also inherits the operation context, so it
cannot start fresh background provider work after the execution deadline.
Secret material is still dropped synchronously. Paused teardown remains
claimable, and final scrub erases provider credentials only when no retained
target or pending scrub needs them. Credential changes retire pending jobs and
detach their pointers without rewriting terminal outcomes. Shared-environment
adoption consumes each single-decision reauthentication window only once.

Current local evidence: all app, service, store, adapter and audit owner tests
pass, including the app upgrade/recovery tail in 289 seconds. Both-engine race
checks cover deadline/progress/publication changes (231 seconds), credential
and teardown lifecycle (445 seconds), GitLab scope collisions (181 seconds),
operator cancel/resume (47 seconds), and interrupted migration recovery
(30 seconds). Service security controls pass three race repetitions. Final
fast preflight passes in 61 seconds; isolation guard races pass, and the reviewed
annotated-query pin changes are limited to the two dialects' paused-tombstoned
scrub claim predicate. The development schema artifact exactly matches fresh
SQLite and PostgreSQL generation. Cleanup discovery reports zero new findings;
all 11 final forced verdicts are fixed without uncertainty.

The refreshed isolated checkpoint contains 282 findings: 274 fixed, three false
positives, four duplicates and one true positive, the human-only nightly key
custody gate below. No uncertain or unrevalidated findings remain at this
checkpoint. Frozen-source rediscovery, the committed full generated-code gate
and new exact-head remote CI are separate delivery evidence; earlier inventories
above describe their own checkpoints.

The subsequent lifecycle checkpoint contains 289 findings: 279 fixed, three
false positives, four duplicates, one human-only custody gate and two newly
discovered origin/configuration findings awaiting their final fix verdicts.
All four repository findings and both runtime findings are revalidated fixed.
Discovery of seven repository/runtime helper and SQL files reports zero new
findings. Actual SQLite and PostgreSQL regression tests cover stale custody
workers, deadline preparation rollback, concurrent target-lock removal,
unfinished-move deletion refusal and released-row adoption recovery. Existing
scrub fixtures now create real authorized teardown jobs rather than rewriting
a converge job's kind while leaving its target active. The query guard still
has only the two reviewed claim-selector hash changes, now including the
operational parent/target lifecycle restriction without a new exception.

The final full app/service/store/adapter/audit owner run passed app and service
plus adapters, audit and migration packages; its two invalid scrub fixtures
were subsequently repaired and pass three race repetitions. The full service
race suite repeated three times reached its 12-minute bound during unrelated
self-configuration origin-recovery tests, without an earlier assertion
failure. This timed-out run is not a pass. Changed adapter/service controls
retain focused race coverage; refreshed owner tests and exact-head CI remain
separate required delivery evidence.

### Final origin, authorization and transport checkpoint

Canonical provider origins are now shared by the service, application factory
and all seven provider clients. Host case, IDNA, IP forms and default ports
cannot create independent custody for the same supported provider endpoint.
Meaningful provider paths and Vault namespaces remain distinct. New inputs are
normalized before persistence; stored legacy aliases fail closed until an
operator performs the supported keep-remote recovery. Keep-remote preserves
old Vault/AWS ownership markers, so recreating a target can require an explicit
destination-side marker handoff rather than assuming the new target owns it.

AWS regional, FIPS and verified AWS-owned VPC endpoint aliases share custody
within their real account, region and partition without changing transport.
Different regions remain independent. Untrusted custom endpoints cannot prove
account identity merely by returning a matching STS response or ARN, so their
custody remains endpoint-scoped. Both JSON-object and per-key destinations use
the same physical secret namespace, including configuration inputs. Held
claims block the opposite mode before provider creation or INTENT recording.

The narrowly reviewed metadata-only held-custody candidate query covers the
exact provider, destination, repository, scope, surface and normalized name.
It is private, paged and caller-context bounded; partial scans never authorize
writes. Its two dialects are the only new instance-scoped query exceptions.
The earlier two claim-selector hash changes remain independently reviewed.
Actual SQLite execution caught an unsupported sqlc macro shape despite
successful generation; the final equality-based query passes actual execution
on both engines and the exact query pins pass three race repetitions.

Fresh actual-database evidence covers canonical/cross-kind concurrent adoption
(134 seconds) and production AWS publication, direct preparation and concurrent
reservation (178 seconds), each with three race repetitions per engine. Full
service and store owner tests passed in 73 and 132 seconds at that checkpoint.
Adoption also rechecks the live session and every environment after custody
scans and row-lock waits, before transaction commit. Expiry rolls back custody,
jobs, audit and staged consent without consuming valid consent twice. Its
service race tests pass in 41 seconds and actual both-engine grant expiry plus
unchanged PostgreSQL row-lock/session expiry races pass in 53 seconds.

The final seam discovery contained five further findings covering credential
text in transport errors, paused-target route moves and endpoint-specific
repository identity recovery. Final forced verdicts mark all five fixed. The
refreshed export contains 295 findings: 287 fixed, three false positives, four
duplicates and one human-only custody gate, with no uncertain or unrevalidated
findings. Eleven service/client findings and four move/AWS-client findings
have final forced fixed verdicts; discovery of the new shared transport-error
helper reports zero new findings.

All seven clients use safe public transport-error text while preserving typed
cancellation, timeout and egress-refusal predicates. Actual TLS redirect and
malformed response probes reproduce the credential leak before the fix. Full
provider suites pass three race repetitions; vet and pinned import formatting
pass. AWS also hides receiver-controlled response codes in public error text,
including STS and credential-source refusals; the typed code and existing
auth/throttle/not-found classifications remain unchanged. Actual header, body,
credential-provider and STS regression probes pass with the full provider race
suites. Paused destination moves and any origin move containing a paused target
refuse atomically before jobs, custody release or remote cleanup. The operator
must explicitly resume the target first. Origin creation and replacement clear
endpoint-specific pending repository and destination pins, then activation
resolves and persists both identities from the new endpoint. Old cleanup-route
identities remain intact. Actual both-engine move races pass in 308 seconds;
the latest full service/store owner suites pass in 75 and 147 seconds.

The prior pushed head `7f65616c6` failed core conformance because wall-clock
elapsed time refilled a token bucket during its shared-principal assertion.
The regression now uses an explicit clock without changing production rates;
unit races and actual both-engine conformance races pass three repetitions.
All race shards, including shard 2, passed on that old head. The new signed
commit's full generated-code preflight and exact-head remote CI are separate
delivery checks; old-head or local green results are not their substitutes.

Claude reviews are skipped at the operator's request, not reported as clean.
Main's current base-controlled gate permits same-repository workflow edits.
The approved universal gate admits same-repository workflow edits only with a
current independent maintainer's approval on the exact PR head. Fork workflow
edits remain blocked. This is not a current bootstrap blocker for PR 844.

The nightly signing-key custody finding remains a human-only configuration
gate: move the repository-scoped secret to a protected main-only environment
and remove its repository-scoped copy. The workflow now names that environment,
but source guards cannot protect an existing repository-wide private key.
Use [the custody handoff](nightly-release-custody.md); no repository settings or
secrets were mutated by this remediation.

## CI repair after the custody and retry checkpoint

The pushed checkpoint `39aeb4959` exposed several root causes, repeated across
trusted and fork CI. These are repaired without weakening the guards:

- `ConfigurationForUpdate` now has its own store operation, granted only to
  adapter configuration. Inspection, planning, deletion and system proofs
  cannot use the locking door; it is not an unaudited read-only operation.
- Test fixtures use the existing sanctioned driver harness rather than raw
  handles outside its owner. Target-state comparisons preserve every exported
  field without importing reflection into a proof-handling store test. The
  public retirement flow moved to isolation tests, preserving dependency
  direction and its actual metadata-only service call.
- Four new complex scoped queries have exact reviewed authority records and
  independent generated-query controls. Ten changed records were reviewed
  against both dialects, generated bindings and callers before refreshing their
  pins. No production query, generator or analyzer exemption was changed.
- Direct GitLab namespace positives now seed the exact destination scope;
  independent-scope and foreign-chain refusals remain covered. Forgejo
  conformance fixtures use supported bare origins instead of URL paths.
- The concurrent PostgreSQL reserve/removal probe preserves the deliberate
  serialization-conflict contract. If the old attempt rolls back with SQLSTATE
  40001, its retry after removal must return superseded, with no custody revival.

Local evidence includes full lint (110 seconds), CI-equivalent static
invariants (15 seconds), new generated-direct boundaries on both engines with
three race repetitions (389 seconds), paused moves (456 seconds), public legacy
retirement (80 seconds), all four dependency contexts (13 seconds), the
reserve/removal race (47 seconds), and generation-fenced reservation conformance
on both engines with three race repetitions (114 seconds).

A supplemental default-parallel static-analyzer race run also reproduced a
Go 1.27 type-checker race inside `packages.Load`, before analyzer execution.
Its separate single-checker diagnostic does not change application concurrency,
CI settings or race detection. A first diagnostic overlapped removal of a
temporary pin extractor and is not passing evidence; final checks run after
the complete Go source tree is frozen. The new pushed head's remote CI remains
a separate required delivery check.

The next head exposed an exact-expiry test-fixture mismatch on Linux. The
direct SQLite query used variable-width nanoseconds while the production
wrapper correctly uses canonical fixed-width microseconds. A deterministic
nanosecond clock reproduces the failure on macOS too. The corrected fixture
matches both production bindings and checks one microsecond before, exactly
at, and one microsecond after expiry, plus the equivalent untruncated input.
Both-engine race checks pass three repetitions (46 seconds), and the compiled
Linux test passes three SQLite repetitions in an isolated Docker container.
No production expiry predicate, timestamp codec, query or reviewed pin changed.
