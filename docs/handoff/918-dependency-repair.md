# Grouped dependency compatibility repair

This human-owned change replaces Dependabot PR #918 and recovers the useful
repairs from closed, unmerged PR #914. It retains all 24 dependency updates from
#918. Keeping the repair on a maintained branch prevents bot regeneration from
discarding the compatibility work again.

## Reproduced failures and repairs

- WebAuthn 0.18.2 changed extension inputs and outputs. The original #918 package
  build fails with four compiler errors; current main and #918 with only
  WebAuthn reverted to 0.17.4 build successfully. Use the typed credProps option
  and outputs, preserving non-discoverable behavior for missing residency.
  The software authenticator no longer sends registration-only extension output
  during login; real unsolicited outputs remain refused.
- WebAuthn now refuses IP-address RP IDs. Infer localhost only for the ordinary
  loopback/wildcard listeners, derive managed defaults from the effective node,
  and reject explicit server IP origins with migration guidance. Legacy snapshots
  storing the exact old derived default are normalized before contextual server
  validation, without making the default explicit. Offline
  administration remains available. Hostname configuration, browser fixtures,
  Compose and Kubernetes callers retain separate transport and public origins.
- MCP SDK 1.8.0 conflicted with the exact 1.7.0 test pin. Review the 1.8.0
  changelog, retain the exact pin/replacement checks at 1.8.0, and preserve the
  encoded client-name workaround because the new SDK still compares raw names.
- The workspace credential test's supposedly anonymous Playwright request
  inherited configured storage state. The original desktop flow reproduced
  expected 401 / actual 403. Use the fixture's resolver-aware, cookie-free fetch
  transport with only the explicit bearer. This also avoids the Linux OS DNS
  failure for serving.localhost found by independent review. Retain the 401,
  successful 200 and revoked 401 checks.
- Chart and unattended Kubernetes fixtures still configured an IP public origin.
  The same signed image refuses that origin; changing it to localhost reaches
  a ready server. Update hostname, TLS SAN, chartdoctor admission and callers
  together. Real TLS transport tests verify accepted localhost and refused
  wrong pins and unowned origins. Preserve
  failed-deployment diagnostics without reading bootstrap credential Job logs.

## CI boundaries

The 220-second rapid feedback budget and its existing selected checks stay
intact. Dependency changes still verify module integrity, and changed Go owners
still compile even when go.mod also changes.

The new read-only `dependency-compatibility` workflow gives automatic advisory
evidence for PR go.mod/go.sum changes. It compiles production and test archives selected by the Linux host target
and default build tags, and runs vet without executing init or tests. Other
platforms and release build tags remain separate protected validation jobs. Its separate 20-minute
budget matches the existing full app-build budget. It has immutable checkouts,
no persisted checkout credentials, no secrets and no cache publication. Real
dependency API-removal regressions cover unchanged production, internal-test
and external-test consumers, along with vet failures and no-execution behavior.

This split is deliberate: #914's attempt to place the complete export graph in
the rapid lane exceeded its hosted deadline. The measured local export graph
contains roughly 3.05 GiB of archives, exceeding the 1.5 GiB feedback seed bound.
The first hosted compatibility run at dce9ffdc passed in 3m58s, including
3m39s for compilation and vet with setup-go caching disabled. This measured
run does not establish a universal cold timing or a faster merge guarantee.

The new trusted-main `queue-report` workflow publishes advisory
`merge-queue-validation` status on the source PR head. It attributes runs through
authenticated workflow identity, immutable merge parents, the current PR head,
run attempts and fresh workflow history. It serializes writers and rejects
superseded or foreign identities. It reads GitHub metadata only, never executes
queue code with its status-write token, and becomes active after this change
merges. Protected admission and exact-current-base full merge proof are unchanged.

## Operator compatibility

Before upgrading an installation whose public origin is an IP address, configure
a hostname using the existing binary and verify DNS/TLS. Explicit managed IP origins must
also change; environment overrides do not replace adopted managed values.
Changing RP hostname affects existing passkeys. The configuration documentation
describes the compatibility boundary and recovery with the previous binary.
There is no datastore schema migration in this change.

## Validation at initial publication

- Full Go production build passed.
- WebAuthn/config unit and race tests, relevant service/app tests and scoped vet
  passed. WebAuthn integration passed on SQLite and the CI-pinned PostgreSQL 18
  image in an isolated database.
- Full MCP server tests and production client checks passed.
- Web typecheck, lint, 198 unit-test files / 1,691 tests, and production build
  passed. All 17 desktop and 17 mobile workspace flows passed with explicit
  anonymous request state. Final transport regressions and focused desktop/mobile
  credential flows also passed with Node 26.7 explicitly pinned and without
  injected Node hooks.
- Full CI tooling/controller tests, feedback fixtures, reporter race checks,
  dependency-consumer regressions, pinned actionlint and ShellCheck passed.
- Documentation verification passed, including build, policy, CSP and offline
  browser checks.
- The server package's stalled-stream deadline test failed twice during local
  concurrent work. Its isolated run, subsequent complete package run, and
  unchanged-main complete package run passed. The original failures are retained;
  no timeout or assertion was changed, and a root cause is not claimed.
- The first local kind run stopped before application rollout in the TLS init
  stage. Direct same-image origin refusal/ready evidence is established, while
  complete kind and hosted exact-head results remain separate gates.

Independent review was CLEAN at dce9ffdc, with hosted Go/frontend feedback,
CodeQL and compatibility checks passing. Subsequent review identified chartdoctor
hostname validation and literal legacy managed-origin migration gaps; their
repairs passed independent incremental review, config/runtime tests and races,
owner activation tests, real chartdoctor TLS transport tests and compatibility
scope regressions. New exact-head hosted CI remains required. Fresh full merge-group proof remains required before declaring this change
delivered.
Merge authorization does not authorize release or production deployment.


## First full queue run and native campaign repair

The first merge candidate, d00575b5 (head 5cb7da22 plus main 031bf17b), ran in
[38696437549](https://github.com/Hikyo-Org/Hikyo/actions/runs/38696437549).
Forty-five jobs passed, including all browser shards, Kubernetes, Compose,
Go/race/isolation suites, cross-platform checks and release snapshots. The
native source-artifact security campaign failed at WebAuthn enrollment with
401 instead of 200; three aggregate gates also failed because of that result.

A local same-binary reproduction proved the server advertised RP ID localhost
while the harness signed for 127.0.0.1. Updating only the public origin and TLS
certificate passed enrollment but failed step-up with 401. The assertion helper
also replayed registration-only credProps output, which WebAuthn 0.18 rejects.
The harness now uses a localhost public origin and verified certificate, keeps
its transport listeners on loopback, and omits registration outputs during
assertions. No server validation, assertion, timeout or rate limit was relaxed.

Fresh bootstrap origin statements are now distinguished from historical managed
defaults. Fresh explicit IP origins refuse before serving; explicit localhost
remains explicit. Historical derived-IP snapshots still migrate, and stale
bootstrap values cannot replace adopted configuration. A process statement
matching the exact current hostname derivation still counts as explicit, as
required by the existing public-origin policy; no new persisted provenance was
introduced to distinguish equal fresh and historical hostname statements. Regression coverage
executes LoadBootstrap, seed preparation and actual application Boot, including
native TLS and stale seed cases.

Two positive runtime drills also retained IP public origins: configuration
rollout on kind and managed-configuration upgrade rehearsal. Their public origins
and CLI trust now use localhost. Twenty-one Python tests and real HTTP/HTTPS
native startup checks passed, including CA and hostname verification. Complete
manual kind and officially signed image-upgrade rehearsals remain separate,
unverified drills. Existing negative IP/migration tests remain intact.

The new queue reporter was replayed read-only against the authenticated failed
run and correctly produced a failure for source PR #920. It becomes available
for future callbacks only after merge; it does not backfill this historical run.

At this follow-up commit, final native campaign evidence and fresh hosted
validation are still pending. The PR body records subsequent exact-head results.


## Second queue run and validation metadata repair

The next full candidate, b8a01d13 (head 3bad1af0 plus main ef3d9005), ran in
[38714270787](https://github.com/Hikyo-Org/Hikyo/actions/runs/38714270787).
The native security campaign passed on hosted Linux, and the full application
package passed. The run had 43 successful jobs, two underlying validation
failures, three failed aggregate gates and one cancelled isolation shard.
Fresh complete candidate validation therefore remains required.

The raw-SQL protocol guard correctly rejected the changed newTarget function
body because its reviewed caller fingerprint had not been refreshed after the
public-hostname edit. Review confirmed the same cryptographic owned database
name, identifier sanitization, successful-CREATE guard and guarded cleanup.
Only the two existing caller hashes are refreshed; protocol bodies, helper
fingerprints and the caller allowlist remain unchanged. Positive inventory
checks and typed adversarial mutation coverage remain enforced.

The new Python test imported a parent-directory fixture and created an untracked
scripts/ci/__pycache__ directory. A clean isolated checkout reproduced the exact
sequence: 21 Python tests passed, then required-CI floor preflight refused the
dirty source before reaching its intended refusal cases. Python bytecode caches
are now ignored through the repository's canonical ignore policy. The floor
source-cleanliness gate is unchanged, and regression coverage ensures ordinary
untracked source remains visible. This fixes generated-file hygiene rather
than allowing dirty candidates.
