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
  and reject explicit server IP origins with migration guidance. Offline
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
  a ready server. Update hostname, TLS SAN and callers together. Preserve
  failed-deployment diagnostics without reading bootstrap credential Job logs.

## CI boundaries

The 220-second rapid feedback budget and its existing selected checks stay
intact. Dependency changes still verify module integrity, and changed Go owners
still compile even when go.mod also changes.

The new read-only `dependency-compatibility` workflow gives automatic advisory
evidence for PR go.mod/go.sum changes. It compiles every production and test
archive and runs vet without executing init or tests. Its separate 20-minute
budget matches the existing full app-build budget. It has immutable checkouts,
no persisted checkout credentials, no secrets and no cache publication. Real
dependency API-removal regressions cover unchanged production, internal-test
and external-test consumers, along with vet failures and no-execution behavior.

This split is deliberate: #914's attempt to place the complete export graph in
the rapid lane exceeded its hosted deadline. The measured local export graph
contains roughly 3.05 GiB of archives, exceeding the 1.5 GiB feedback seed bound.
The replacement helper passes on a warm local cache, but that does not establish
cold hosted timing or a faster merge guarantee.

The new trusted-main `queue-report` workflow publishes advisory
`merge-queue-validation` status on the source PR head. It attributes runs through
authenticated workflow identity, immutable merge parents, the current PR head,
run attempts and fresh workflow history. It serializes writers and rejects
superseded or foreign identities. It reads GitHub metadata only, never executes
queue code with its status-write token, and becomes active after this change
merges. Protected admission and exact-current-base full merge proof are unchanged.

## Operator compatibility

Before upgrading an installation whose public origin is an IP address, configure
a hostname using the existing binary and verify DNS/TLS. Managed origins must
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
- Web typecheck, lint, 198 unit-test files / 1,690 tests, and production build
  passed. All 17 desktop and 17 mobile workspace flows passed with explicit
  anonymous request state. Final transport-regression and focused desktop/mobile
  checks cover the subsequent Linux portability correction separately.
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

Independent review, final mobile/kind results, hosted compatibility timing and
full merge-group proof must be recorded before declaring this change delivered.
Merge authorization does not authorize release or production deployment.
