# CI critical-path optimization handoff

## Outcome

- `internal/isolation` is excluded from the monolithic `test_core` job and its
  514 top-level tests are assigned exactly once across three independent
  PostgreSQL-backed runners. The existing `test` job ID is now the aggregate,
  so the required-gate contract does not change.
- CI, Pages, and release docs jobs plus web browser jobs run in the
  digest-pinned Playwright 1.62.1 Noble image. This removes per-leg browser
  downloads and `apt` dependency installs. Release docs verification is an
  explicit prerequisite job, leaving the release build on its normal runner.
- The browser matrix has four groups per viewport. The long matrix flow is
  isolated in group 4, and `app-build` supplies both the UI binary and fake OIDC
  provider so browser legs perform no Go setup or compilation.
- Both `ci-required` gates and the test/race/fuzz fan-in jobs retain ordinary
  failure aggregation but skip after workflow cancellation, allowing a
  superseding pull-request head to acquire concurrency immediately.

## Coverage and trust model

- The base-controlled planner discovers valid top-level `Test*` declarations
  from the checked-out head with the Go parser. Stable FNV-1a assignment keeps
  every test on exactly one shard without adding `t.Parallel()` to stateful
  integration tests.
- Pull requests use the planner from their base SHA, preventing head code from
  omitting its own validation. Main uses the checked-in planner directly.
- Browser and docs images use the same exact Playwright version declared by
  their package manifests and a multi-architecture image-index digest.
- Docs PWA and container live-site checks share a Zod-backed JSON validator;
  fallback-channel fixtures generate their invalid variants without a parser.
  The browser image therefore needs no live `jq` or `apt` provisioning step.
- Browser artifacts remain run-scoped and exact-head. Missing prebuilt OIDC
  binaries fail before a browser flow starts; local development retains the
  source-build fallback.

## Timing boundary

The previous observed critical path was 8m52s for `internal/isolation`; browser
runs also suffered fresh-runner `apt` stalls above ten minutes. The new graph
removes both serialized costs, but hosted-runner timing is pending the first
post-merge main run. Do not present local timings as production evidence.

## Verification

- Planner fixture and an independent `go test -list` comparison proved all 514
  current isolation tests are complete and disjoint (180/163/171 targets).
- Actionlint; ShellCheck; registry, trusted-script, cache-policy,
  artifact-reuse, required-job, and changed-path fixtures passed.
- Web TypeScript typecheck and all 602 unit tests passed on Node 26.7.0.
- The exact Playwright image digest resolved on arm64 and contained Chromium.
- The complete docs verifier passed inside that image, including the offline
  Chromium route, live-site fixtures, and fallback-channel fixtures.
- Full PostgreSQL-backed `go test -count=1 ./...` passed; the unchanged
  monolithic isolation baseline completed in 554.444 seconds.
