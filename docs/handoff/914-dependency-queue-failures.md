# PR 914: dependency compatibility and merge-queue visibility

## Failure evidence

PR [914](https://github.com/Hikyo-Org/Hikyo/pull/914), original head
`105cbcd4bd93c322ccf935eb4646fb6899e7864a`, repeatedly left the merge queue.
The latest original failed candidate was
[run 38613046002](https://github.com/Hikyo-Org/Hikyo/actions/runs/38613046002),
merge commit `5a3e06174a75a92201a39510358d7b0aa783b058`.
Its immutable parents are current main `4f1d1ec632728b517c9871735e3e69f1655d35a4`
and the original PR head.

The core build and isolation jobs report the same WebAuthn compilation errors:
v0.18.2 replaces extension maps with typed inputs and outputs. BeginEnrol passed
the removed map API, and credential-property extraction indexed a struct.
This prevented the full candidate from building, regardless of green admission.
Earlier stale mergeability was a separate admission issue, not proof that the
dependency update could pass full validation.

Admission belongs to the PR head; the full suite belongs to the temporary queue
merge commit. The old setup left the admission green visible after queue eviction
without publishing a corresponding failure on the PR head.
Dependency-only fast feedback checked module integrity, not consumer compilation.

## Changes

- Request credential properties with the library's typed registration option,
  and preserve reported resident/non-resident values while missing output stays
  non-discoverable. Malformed output remains refused.
- Stop the software authenticator replaying registration-only credProps during
  login. The new library correctly rejects unsolicited extension output; its
  strict validation remains enabled.
- On go.mod/go.sum changes, verify modules and compile all production and test
  consumers. The initial `go test -c` implementation linked every test binary;
  hosted feedback exhausted its unchanged 220-second budget. The follow-up uses
  `go list -deps -test -export -f '{{""}}' ./...` to build all package/test
  archives without linking or executing test binaries. Its regression fixtures
  cover broken unchanged production and test-only consumers and an init panic
  that must never execute. A separate `go vet ./...` preserves the diagnostics
  previously supplied by `go test -c`, with a compile-valid printf-mismatch
  regression. Hosted timing for this follow-up remains unverified.
- Add the advisory `merge-queue-validation` PR status, with a direct run/attempt
  link. The reporter reads only authenticated GitHub metadata and immutable merge
  parents, so it can report after eviction. Superseded heads, attempts and groups
  cannot publish. A repository-wide writer lease prevents old-base reporters
  overtaking newer reports. This status never replaces protected queue proof.

The new workflow executes trusted default-branch code and never downloads
artifacts or checks out queue/PR code with write credentials. It becomes active
after this change lands on main. An explicit full candidate on this PR provides
visible hosted validation before that activation.

## Verification

- Original WebAuthn compilation errors reproduced locally before the fix.
- All production packages build and all package/test binaries compile.
- Credential-property cases and signed discoverable-login round trip pass;
  unsolicited registration output during login is explicitly refused.
- All TestWebAuthn integration cases pass on SQLite and the exact CI PostgreSQL
  image, `postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941`.
- The initial Homebrew PostgreSQL run failed during signed-schema setup, before
  passkey behavior. Its original log was retained locally. The pinned-image run
  passed without changing the schema policy.
- CI controller/tooling suites, wrapper/controller race checks, import-boundary
  checks, vet, pinned workflow lint and ShellCheck pass.
- Fast-feedback regressions cover broken unchanged production and test-only
  consumers. The original feedback script incorrectly accepted the production
  fixture before the fix; the complete updated fixture suite passes.
- Independent Standards and Spec reviews found no remaining actionable issues.
  Cross-provider review was not requested or run.
- A read-only live replay attributes the original failed run to PR 914 after
  eviction and proposes a failure status. Hosted reporter publication is pending
  activation after merge.

## Hostname migration

The first full candidate of fix `0da7139d3` exposed a second breaking change:
WebAuthn 0.18 rejects IP-address RP IDs. Compose, macOS file-sync and the headline
restart check fail during boot for literal-IP origins. The file-sync failure was
reproduced locally. The maintainer chose the complete WebAuthn 0.18 migration,
including hostname-based development origins and its configuration compatibility
change. WebAuthn remains at 0.18.2; its validation is not bypassed.

- Inferred origins for the default loopback and wildcard listeners use
  `localhost`, preserving the listener, port, TLS scheme and non-explicit status.
  Nonstandard loopback aliases need an operator-configured hostname pointing to
  their actual address; they are not silently advertised as localhost.
- Explicit server IP origins, including browser IPv4 aliases, fail at configuration
  load with hostname migration guidance. Existing hostname origins stay unchanged.
  Offline administration, migration and backup commands can still load an old IP
  origin so the datastore remains maintainable.
- Managed owner configuration now derives an implicit origin from the effective
  listener and TLS candidate, rather than keeping the bootstrap port/scheme.
  Explicit origins remain operator statements; inferred defaults are not exported
  as explicit public origins.
- README, Compose, retention and Kubernetes fixtures use hostname public origins
  with separate IP transport listeners. The browser pair uses `localhost` and
  `serving.localhost`, keeping two cookie jars. Node fixture traffic uses a
  request-local Undici dispatcher because the pinned Linux OS resolver does not
  resolve `.localhost`; URL, Host, Origin and Cookie headers remain unchanged.
  Undici 8.11.0 passes the repository's existing release-age policy, with no
  exclusion added.
- The CSP builder and workspace origin validator recognize `.localhost` as the
  reserved loopback namespace from RFC 6761. Each configured origin is still
  emitted individually; wildcard CSP sources and lookalike domains remain refused.
  New tests reproduced both refusals before the change. Browser `localhost.` was
  tried and rejected by the actual RP library, then replaced with the canonical
  subdomain spelling.

Operators using an IP-address `HIKYO_EXTERNAL_ORIGIN` must configure a hostname
and access the instance through it. Local default development uses
`http://localhost:8080`; production uses the instance's HTTPS hostname. The
socket bind may remain an IP address. Existing managed values must also name the
new origin. Change them using the existing binary before upgrading; if the new
binary already refuses startup, return to the previous binary to configure the
hostname. Environment overrides do not replace adopted managed values. This is
a configuration compatibility change, not a datastore schema migration.

Local validation of this migration includes the full Compose demo, passkey and
CLI integration cases on SQLite and the pinned PostgreSQL image, config/RP race
checks, and web typecheck, lint and fixture-unit checks. The first broad app run
also exposed an inconsistent HTTPS-origin test fixture; marking its manually
configured public origin explicit preserved the existing HSTS assertion. That
check and its sibling TLS/runtime checks pass. Two unrelated packaged-upgrade
tests cannot complete locally because an existing VM listener owns port 8081;
that environment failure is retained, and hosted validation remains required.
The signed-out two-instance popup login passed on desktop and mobile. The final independent
Standards and Spec reviews of the follow-up delta found no actionable issues.
Documentation check and static build pass. The
structural graph was refreshed; its existing Astro parser limitations remain
navigational only.

## Remaining delivery gates

Push the signed, DCO-signed fix, request a visible full candidate, and recheck
exact-head reviews and checks. Because the PR author is Dependabot and .github/
changes, `scripts/ci/check-fork-validation.sh` requires an independent current
maintainer's latest approval on the exact head. Do not fabricate that approval
or bypass the policy. After approval, reassess admission and enqueue the exact
head. Merge requires the complete current-base suite and trusted queue proof.
No release or deployment is part of this task.
