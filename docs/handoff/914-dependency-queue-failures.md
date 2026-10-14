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
  that must never execute. Hosted timing for this follow-up remains unverified.
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

## Remaining delivery gates

The first full candidate of fix `0da7139d3` exposed a second breaking change:
WebAuthn 0.18 rejects IP-address RP IDs. Compose, macOS file-sync and the headline
restart check fail during boot for literal-IP origins. The file-sync failure was
reproduced locally. A scope decision is pending: retain WebAuthn 0.17.4 in this
batch to preserve existing startup behavior (recommended), or complete a broader
hostname-origin migration and accept the configuration compatibility change.
Do not assume this decision has been made. No origin/security validation has
been bypassed.

Push the signed, DCO-signed fix, request a visible full candidate, and recheck
exact-head reviews and checks. Because the PR author is Dependabot and .github/
changes, `scripts/ci/check-fork-validation.sh` requires an independent current
maintainer's latest approval on the exact head. Do not fabricate that approval
or bypass the policy. After approval, reassess admission and enqueue the exact
head. Merge requires the complete current-base suite and trusted queue proof.
No release or deployment is part of this task.
