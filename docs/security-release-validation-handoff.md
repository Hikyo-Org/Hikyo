# Native release security validation handoff

Task: retire OpenAEV CI and replace it with native application security checks.
Pull request: [#917](https://github.com/Hikyo-Org/Hikyo/pull/917).
Date: 2026-10-14. Branch: `dunky13/security-release-validation`.
Base: `4f1d1ec632728b517c9871735e3e69f1655d35a4`.

## Result and activation

PRs [903](https://github.com/Hikyo-Org/Hikyo/pull/903),
[904](https://github.com/Hikyo-Org/Hikyo/pull/904) and
[905](https://github.com/Hikyo-Org/Hikyo/pull/905) were closed and unwatched.
Both historical OpenAEV Actions workflows are disabled. Fresh main has no
OpenAEV workflow dependency. The old paused worktree remains recoverable and
is separate from this implementation.

The replacement has no private infrastructure dependency. Stable/prerelease
source validation uses the same full suite as nightlies. A separate hosted
post-publication job authenticates actual release bytes, runs native attacks
against SQLite and PostgreSQL, runs Chromium against the embedded SPA, scans
the executable, and publishes complete passing JSON/HTML evidence only.
Its failure does not block publication or modify the release.

Existing CI coverage is conserved by strict baseline tests with an explicit
additions fixture. Four semantic fuzz targets supplement existing crash
targets. The source CI also executes the native harness and browser campaign.
Application authentication, authorization and rate limits are unchanged.

The post-release workflow becomes active only after this branch reaches main.
Local execution does not establish a GitHub-hosted run. Stable release
ceremony execution remains unproven until an actual stable/prerelease run.

## Local evidence

| Check | Observed result | Scope |
| --- | --- | --- |
| Go policy/workflow/source suites | All five selected packages passed; no test failures | Local source checkout |
| Final security tools and CI tests | All three packages passed; race and vet passed | Local harness, publisher and CI conservation |
| Web tests | 197 files, 1,689 tests passed; typecheck and lint passed | Local source checkout; 12 seconds warm, 32.5 seconds cold wall time |
| Semantic fuzz | Each of four new targets executed a 100,000-iteration campaign | Local Go fuzz engine, bounded corpus |
| Source-shaped native campaign | 14/14 cases per datastore, cleanup complete, 250 seconds | Current application source with authenticated ephemeral fixture trust |
| Published Darwin native campaign | 14/14 cases per datastore, 24 generated steps each, cleanup complete, 373 seconds | Verified nightly 77 Darwin ARM64 executable |
| Published Darwin browser campaign | 3/3 cases, no skips, 55 seconds | Same released executable, hardened native-auth browser profile |
| Published Linux native campaign | 14/14 cases per datastore, 24 generated steps each, cleanup complete, 388 seconds | Verified Linux AMD64 bytes under local Rosetta Docker emulation |
| Published Linux browser campaign | 3/3 cases, no skips, 82 seconds on a subsequent isolated attempt | Same verified Linux bytes under local Docker emulation; original failed attempt retained |
| Independent source review | No open findings after fixes; actual receipt producer/consumer controls pass | OpenAI GPT-6.1 Sol, high reasoning; cross-provider review skipped |

The authenticated nightly is
`v0.0.1-nightly.20261014.77.g2d5d494d`, source
`2d5d494de5818601c30f91736f1668f531411247`.
Darwin ARM64 executable SHA-256:
`deb8e91fdaa3e03a1588564e5a779154c95b4ff6568851d8dfccc7f4d440b24c`.
Linux AMD64 executable SHA-256:
`5c46b5463465afdd4811168fa8ae89f766f63208036f8ed27b6e0a87e645591a`.
This release revision and the implementation branch are separate identities.
No local result is presented as a version-bound hosted PASS artifact.

The first emulated Linux browser attempt crashed both application renderers
while the native campaign was also running. A bounded rerun after that
campaign completed passed with unchanged product policy, test budgets and
container limits. Cgroup OOM and OOM-kill counters remained zero. The original
receipt, logs and traces are retained; the renderer crash cause is unproven,
and the later pass does not erase it. The hosted workflow runs the native and
browser campaigns sequentially, but its execution is still separate proof.

The mandatory native cases are native bootstrap, authorized controls,
cross-project denial, cross-tenant denial, unchanged denied mutation,
grant removal/restoration, credential revocation, cursor binding,
projection/revision boundaries, restart continuity, audit boundary and
generated sequences. Counts describe successful oracle groups, not a
security coverage percentage or distinct attacks.

The browser cases are stored hostile content, hostile origin and deliberately
unsafe oracle controls. Browser tests record loopback HTTP development
transport and development/protected disclosure windows of 300/0 seconds.
The native campaign independently uses production TLS and the default zero
window with actual signed WebAuthn ceremonies. Neither profile covers every
external identity provider, browser engine or deployment configuration.

## Owners and reproduction

- `.github/workflows/ci.yml`: full source gate and additive campaigns.
- `.github/workflows/release.yml`: stable/prerelease source validation parity.
- `.github/workflows/security-release.yml`: read-only hosted artifact campaign.
- `scripts/security/verify-release.sh`: native signed release verification.
- `scripts/security/artifact-harness`: actual-binary native campaigns.
- `scripts/security/release-evidence`: strict receipt validation and HTML output.
- `web/e2e/security.config.ts`: hardened compiled-SPA browser profile.
- [Security checks](security-testing.md): public scope, limitations and pinned ASVS review mapping.

From the repository root, with the pinned Go and Node toolchains installed:

```sh
go test ./scripts/security/... ./scripts/ci -count=1
go test -race ./scripts/security/... -count=1
go vet ./scripts/security/...
scripts/ci/run-go-tool.sh actionlint .github/workflows/ci.yml .github/workflows/release.yml .github/workflows/security-release.yml
shellcheck scripts/security/*.sh
pnpm --dir web typecheck
pnpm --dir web lint
pnpm --dir web test
```

For actual release execution, use the workflow's verification step first.
Run the harness with the verified binary and assembled offline bundle, plus
an explicitly owned loopback PostgreSQL database server. The harness creates
and drops its own randomly named database; its parent user needs `CREATEDB`.
It rejects DSN target overrides and inherited connection parameters. Do not
point it at production infrastructure.

The publisher requires complete receipts for the same Linux AMD64 bytes,
all native and browser cases, exact fixture configuration, scan identity,
cleanup and run attempt. Old, weakened, skipped or incomplete receipts reject.
Failures preserve safe Actions logs but publish no custom failing artifact,
release note or release asset. Passing Actions artifacts expire after 90 days.
The existing CI badge stays; no additional fuzz badge is added.

## Remaining verification

Review follow-up: the scanner receipt validator now embeds the same
`scripts/ci/go-tool-modules.txt` pin used by the launcher. It still requires
exact scanner-version equality. A build-overlay regression demonstrates that
a reviewed manifest upgrade changes the expected version in both paths;
unexpected versions and missing, duplicate or malformed pins reject.

The native source campaign moves into a dedicated required job selected by
the existing `web-go` plan. The original `web-go` job keeps its 15-minute
budget and complete test/no-egress steps. The new job has a 35-minute ceiling,
bounded setup/build steps and a separate campaign budget; its internal 10-minute campaign
deadline is unchanged. This addresses a shared job-budget risk, rather than
a reproduced hosted timeout. The source campaign now invokes a prebuilt harness:
compilation occurs in the existing five-minute fixture-build step. Its
11-minute runtime step keeps the harness's ten-minute deadline separate
from compilation, with no timeout increases. Four actual-shell regression scenarios cover command
ordering, exact inputs, and failures in fixture generation, UI compilation
and harness compilation; no campaign or success receipt follows a failed
build. The local warm harness build took 1.43 seconds; this is not a hosted
cold-build performance claim. The optional performance benchmark was skipped
by its existing rolling budget policy; no credit override was requested.

The hosted source campaign on `8af142e` completed all 28 mandatory native
cases across SQLite and PostgreSQL, with 24 generated steps per datastore,
under the unchanged runtime deadline. This is GitHub-hosted source-fixture
proof, not post-publication validation of released assets. Its full CI run
still failed isolation shard 0: the cache registry classified a pure workflow
policy test as a cache implementation because its name contained `Caches`.
The test is renamed to `TestMainValidationWorkflowPolicySecurityReleaseUsesEphemeralToolchains`;
its body and existing supply-chain selection prefix are unchanged. The
original invariant failure reproduced; the renamed test passes the actual
cache invariant and full existing supply-chain Go selector. Independent
review confirms this is a name-only repair with no analyzer or registry change.

Queue admission on `8af142e` exhausted the trusted-main publisher's work
budget; one supported manual reassessment also exhausted it. Both original
logs are retained. The publisher has a setup-inclusive 60-second deadline,
including a 15-second failure-publication reserve. Cold SDK setup and repeated
initial API inventories consume that envelope. Source changes in this PR
cannot affect that publisher until they reach protected main. No terminal
check was reset, deadline extended, gate bypassed or repeated retry loop run.
The worker repair passes its authenticated initial policy and complete role
inventory directly into admission, after matching the live PR number, actor,
head/ref/repository and base ref/repository. The controller fixture reduces
reads from 28 to 23; this is local call-count evidence, not a hosted latency
claim. Final source/main, policy, authority, canonical DCO, complete feedback,
global roles and owned checks remain independent and in their original order.
Delayed-movement tests refuse changed identity, duplicates, queue collisions,
feedback attempts/lanes, policy, authority, DCO and check ownership. Full
publisher tests, race checks and the actual trusted workflow-authority shell
fixture pass. Group routing retains its original reads; no cross-invocation
cache or dispatch-supplied authority is introduced.

Two further direct publisher attempts on `afdc011` exhausted the same
trusted-main work budget during final feedback or check-history reads.
Final canonical DCO passed in 46 milliseconds; approximately 22 seconds of
cold setup left only 23 work seconds. Both failed logs are retained.
Direct admission now also reuses its complete, identity-matched initial role
inventory for preliminary shared-head detection, reducing the healthy
controller fixture from 24 reads to 23. The worker remains at 23 reads.
Fresh final roles still follow complete final feedback. The intermediate
ownership scan is preserved because it has a recovery side effect: if a
foreign native green appears and the next PR refresh fails, it establishes
a distinct owned blocking generation. A retained regression covers that
combined race while keeping the prior terminal record unchanged. One fewer
read offers headroom, not hosted deadline proof. These changes still require
legitimate protected-main activation before affecting the running publisher.

Candidate-local reusable workflows can execute this new job while loading an
older protected-base checker. The aggregate checks its success/skip result
against the strict Boolean plan before adapting only the recognized new
result key for the older registry. The trusted checker and registry remain
unchanged. Seventy-two behavioral migration scenarios exercise the actual
older checker, current policy, missing/failing proof and malformed inputs.

Supply-chain follow-up: the artifact-reuse fixture previously crossed job
boundaries and treated attempt-specific report names as shared-app artifact
names. Its reported failure was reproduced on `982a515`. Checks now bind to
each job and the transfer action's actual `with` inputs, retaining exactly
one stable app producer/consumer and the pinned transfer action. Twenty-eight
regressions cover legitimate reports, adjacent-job contamination, duplicate,
patterned, wildcard and unnamed app downloads, quoted names and input spoofing.
The existing supply-chain test selector and no-egress lifecycle checks pass.

Latest review follow-up: the immutable release resolver has 25 behavioral
scenarios executing its actual shell with strictly ordered read-only API
fixtures. Lightweight, annotated and nested tags are accepted; mismatched
nightly source SHAs, ambiguous tags and untrusted events reject without
partial outputs. These tests run under the existing supply-chain selector.
The release job explicitly disables setup-node automatic package-manager
caching. The pinned action currently enables automatic caching only for npm,
so this is a preventive policy guarantee for this pnpm repository.

Grant removal and restoration now retain the same native-issued credential
across mandatory restarts. Native credential metadata independently confirms
that removed-grant denials are not caused by expiry or revocation. Seeded
sequences place at most two additional restarts before their fixed revocation
tail. Seed 314 was run against the verified nightly 77 Darwin ARM64 binary:
14/14 mandatory cases per datastore, 24 generated actions, including two
restarts while grants were removed and the generated credential was live,
cleanup complete, 372 seconds. The mandatory removal/restoration cases now
report 9/5 successful oracle groups. This newer harness proof does not
retroactively extend the earlier Linux campaign's scope. The SAML helper's
unused parameter was removed; its 100,000-iteration semantic campaign passes.

The frontend feedback failure on `ffd78e0` was reproduced with an empty
Go toolchain/module/build cache: compilation of the real IdP helper consumed
the test's 30-second startup window. The helper now builds during bounded
preparation before the timed cases, using the existing prebuilt-binary input.
The original startup assertions, 30-second case limit and four-minute feedback
supervisor remain unchanged. The complete 1,689-test suite passes cold in
32.5 seconds wall time and warm in 12 seconds. Canary teardown now closes
unfinished HTTP requests after closing its listener; two real-socket
regressions cover completed keep-alive and unfinished requests. Three browser
security cases also pass against the published Darwin executable. That local
receipt omits source SHA, so it supplies scoped browser proof only and cannot
be published as a complete version-bound PASS receipt.

Full CI also exposed the missing driver-handle inventory entry for the new
harness. The repair grants only `scripts/security/artifact-harness/runtime.go`
its owned disposable-database setup/cleanup exception. Neighboring files,
lookalike packages, subpackages and production packages remain refused;
generated query imports stay forbidden. The actual repository confinement
invariant is retained.

The delayed `test_core` failure on `1756814` also identified a separate
raw SQL protocol gap, reproduced on `dfe02a7`: both disposable-database CREATE
and DROP operations were attributed to the broad `newTarget` lifecycle
function. The repair extracts two small, named engine protocols with reviewed
function bodies, a strict lowercase random-name/sanitization dependency and
only `newTarget` as their reviewed caller. The lifecycle receives no raw SQL
authority. All 95 prior protocol records, 245 prior helper records, query
contracts and build-context records remain unchanged. The actual guard passes
in all four build contexts. Thirteen typed source-overlay cases reject tenant
SELECT/UPDATE, altered statements, unsafe identifier/validation changes,
unowned names, lost creation-state gating, extra or aliased callers, wrong
function owners and neighboring files. The original failed log is retained.

A fresh local PostgreSQL fixture executed the exact copied runtime source
(SHA-256 `25288898b416ca4226d8474eba4f44986acee54c3fe28ba1e374cd9665597904`).
Owned CREATE/DROP passed; an actual `newTarget` creation collision preserved
the existing database; invalid/unowned names rejected before SQL. Parent
database identity and marker data were unchanged. All owned databases and
markers were removed, and the task-owned container was removed after proof.
A supplementary probe requires the exact CREATE-stage error and confirms
that the forced nonce reader was used once. An unforced constructor succeeds
with a different owned database and passes cleanup, so the collision refusal
oracle rejects that control. Its first diagnostic attempt had a duplicated
cleanup call in the temporary probe; that failed source/log pair is retained,
and the corrected probe passes without a repository code change.
This validates the extracted setup/cleanup path; the earlier complete
published-binary campaigns remain observations from their earlier harness
revision. Updated full hosted campaign proof remains a separate gate.

A final local Go run exposed a separate queue-toolchain test race: the fixture
created a whole-second deadline with only one work second remaining. A second
boundary could expire that work before the expected Go calls. The fixture now
anchors wall-clock reads, adds the consumed-second refusal scenario, and
retains the real hanging process, timeout and elapsed-time assertions.
Production queue workflow and helper deadlines are unchanged; the original
failed local run is preserved.

Public standalone HTML now uses absolute contribution/security policy links;
artifact documentation distinguishes `github.run_id` from the nightly build's
run number. CodeRabbit's optional private-helper docstring percentage warning
is not a repository gate; no documentation threshold or lint policy was muted.

Local Linux native and browser campaigns have completed with the original
failed browser attempt preserved. The signed, DCO-signed main-based PR is
open. Validate its exact-head CI and reviews. Keep hosted activation and actual
stable release proof separate from those source checks. Merge and release
remain subject to their respective human authorization gates.
