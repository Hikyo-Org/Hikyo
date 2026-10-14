# Native release security validation handoff

Task: retire OpenAEV CI and replace it with native application security checks.
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
| Web tests | 196 files, 1,687 tests passed; typecheck and lint passed | Local source checkout |
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

Local Linux native and browser campaigns have completed with the original
failed browser attempt preserved. Open one main-based PR with cryptographic
signature and DCO, and validate its exact-head CI and reviews. Keep hosted activation and actual
stable release proof separate from those source checks. Merge and release
remain subject to their respective human authorization gates.
