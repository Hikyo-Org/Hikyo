# CLI test process replacement

## Problem and cause

`go test -count=1 -json ./internal/cli/` reported success after starting only
77 of the 276 listed top-level tests. The log ended inside
`TestRunMergeCollisionAndAllowOverride`, without a package completion event.

The test used `HIKYO_TEST_COLLIDE` as its inherited collision variable.
`sanitizedEnviron` intentionally removes the `HIKYO_` namespace before merging,
so the first invocation had no collision to refuse. Its unset `IO.Exec` then
called the real platform executor. On Unix, executing `true` replaced the test
binary and exited successfully, discarding the remaining tests and cleanup.

## Changes

- Use `APP_TEST_COLLIDE` so the collision survives sanitization. Keep both the
  refusal assertion and the explicit override assertion that fetched data wins.
- Reject unexpected child execution in both shared test IO constructors,
  `composeIO` and `testIO`. Tests expecting execution explicitly inject captures.
  A broken refusal now returns an unexpected exit code instead of replacing the
  test binary. Production execution and namespace sanitization are unchanged.
- Repair fixtures exposed by the complete suite: supply delivery receipts for
  direct snapshot/cursor failure tests and a valid-format SPKI pin for the trust
  atomic-write test. The snapshot failure case now reaches its intended storage
  failure instead of stopping at receipt validation.
- Allocate the Compose sync fixture's runtime directory under `/dev/shm` on
  Linux. An ordinary temporary directory can be disk-backed, causing the real
  tmpfs guard to refuse before the test reaches Docker apply and lock checks.
  Other platforms retain their ordinary temporary directory; all guards and
  assertions remain intact.

## Verification

First, adding only the rejecting executor made the original collision fixture
fail with exit 126 instead of `ExitRefused`, proving the early-execution path.
After correcting the fixture, the full suite exposed the missing receipt and
trust-pin failures. Both were reproduced independently before repair.

Final validation on Go 1.27.2, macOS arm64:

```sh
go test -count=1 -json ./internal/cli/
go test -race -count=1 -json ./internal/cli/
go vet ./internal/cli/
git diff --check
```

Both test runs started and completed all 276 top-level tests, with zero
failures and an explicit package pass event. Their test names were compared
against `go test -list '^Test' ./internal/cli/`; a process exit code alone was
not accepted as proof. Changed Go files also pass `gofmt` inspection.

Independent static review identified the external-package `testIO` execution
gap. It was repaired and re-reviewed with no remaining findings, then both
complete suites were rerun. Remote CI status is tracked by the associated PR.

The first full CI run exposed the Linux Compose sync fixture failure above.
It was reproduced using a cross-compiled Linux arm64 CLI test binary inside
the local `golang:1.27` container as UID/GID 1000, with `/tmp` disk-backed.
The corrected binary then passed all 275 Linux top-level tests in that container;
the additional macOS test covers the Darwin-only protected clipboard.
The follow-up received independent static review with no findings, plus fresh
macOS normal/race runs and vet. Linux CI must still validate the pushed commit.

## Scope

Only CLI tests and this handoff change. No runtime, authentication, database,
release, or deployment behavior changes. This is separate from issue #611.
