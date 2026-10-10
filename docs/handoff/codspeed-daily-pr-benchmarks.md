# PR #863: Daily and requested PR wallclock benchmarks

The [automatic PR admission update](codspeed-automatic-pr.md) adds selective PR
runs and supersedes the separate 528/72-minute allowance described below. This
document retains the original PR #863 implementation and review history.

Hikyo runs CodSpeed walltime once daily on main, and on demand for a specific PR head from a bot comment. There is no paid run on every merge. The implementation runs the Go benchmark suite, including fixed publish, import, history and diff cases, plus a separate large-matrix browser check. It does not add an end-to-end load-test harness.

## Behavior

- `benchmark-smoke.yml` runs PR smoke checks on hosted runners and cancels superseded heads.
- `codspeed.yml` schedules main at **03:17 UTC daily**. Main-only manual dispatch remains available and includes walltime rather than only smoke checks.
- Every opened, reopened or updated PR receives one **Performance** comment with **Run benchmark** and **Use confirmed credit** checkboxes. A new head resets the previous head's request.
- A collaborator with write, maintain or admin permission can tick **Run benchmark**. The controller checks live permission, comment identity, current head and both unchanged benchmark workflows (`pr-benchmark.yml` and its reusable `matrix-performance.yml`) before admitting spending.
- The request reruns `pr-benchmark.yml` for that exact head. Its first attempt is discovery-only; the trusted verifier executes on later attempts. Its original event stays `pull_request`, including for forks. The PR executes with a read-only token, no secrets, no OIDC and no shared main-cache writes.
- The controller and reporter execute tooling from main. They hold comment write authority; the PR benchmark does not. No PR artifact is executed or used to establish PR identity.
- Results link to the exact run attempt. CodSpeed supplies performance analysis against the available main baseline on the same Graviton label. This is not an exact merge-base versus head paired comparison or a release acceptance proof. A daily main result must exist before a useful comparison can be expected.
- Stale heads and different rerun attempts cannot reuse a request. The Macro job checks again before executing PR code. A separate trusted completion reporter updates the result even if the requester's monitoring job ends.
- Daily and requested runs share `codspeed-walltime` serialization. `queue: max` preserves up to 100 pending requests instead of replacing the previous pending request. The PR execution workflow has its own per-PR group, so the controller can retain the global lease while the requested measurement runs.

The controller waits up to 25 minutes for runner assignment and measurement, then requests cancellation. GitHub can require maintainer approval before a fork's initial workflow runs. Wait for that initial exact-head check before ticking the box. Benchmark workflow changes must land on main before their PR can use this paid lane.

## Allowance and credit

The admission module reads both benchmark workflows' paginated run and job history, including all attempts. Initial PR smoke/discovery runs skip per-run job queries; admitted reruns still include every attempt, even when the original run predates the window. A creation-date filter would lose that usage, so all history pages remain included. It counts the complete assigned Macro job, rounds up to a minute and adds another minute of margin. Failed and cancelled assigned jobs count. Cancelled or skipped jobs that never received a runner do not turn queue time into cost. Incomplete usage or an active Macro job prevents a new admission.

Each new measurement reserves **16 minutes** against a **15-minute job timeout**. A **32-day lookback** avoids relying on an unverified billing reset date. **528 minutes** are reserved for daily coverage, including boundary overlap and the next full reservation; ordinary requested runs have **72 minutes**. Together these allowances are **600 minutes**. Accounting uses conservative full-job time, not only the timed benchmark section.

An explicit **Use confirmed credit** request adds at most **156 minutes** to the requested-run allowance over that same window. At the published **$0.032/minute** rate, that is **$4.992**, under $5. Earlier credit-funded requests do not consume the daily allowance. Main manual dispatch exposes the equivalent `use_credit` input. Credit is off by default.

At a five-minute full job, daily running is about **155 actual runner minutes in a 31-day month**, before provider rounding. At the full timeout it is **465 actual runner minutes**, with conservative CI accounting and extra boundary reserves on top. The offered $100 corresponds to **3,125 overage minutes** if it is eligible for this runner.

This repository ledger covers the two Hikyo workflows, not other repositories, deleted history, or CodSpeed's invoice. It does not establish the provider's exact billing interval, rounding rules or budget-stop semantics. Use CodSpeed's account-side monthly Macro budget as the billing backstop. The $100 credit's availability, expiry, application to Macro usage and remaining balance must be confirmed before enabling credit. This code does not replenish credits, create a subscription or change billing settings. Its $5 limit is per rolling window, not a lifetime $100 balance tracker.

## Activation after merge

1. Verify CodSpeed's app installation and that its runner group permits this public repository and the benchmark workflows. Recent main jobs were cancelled without a runner assignment, so runner availability remains unproven.
2. Let the daily run establish a main baseline, or trigger it once:

   ```sh
   gh workflow run codspeed.yml --repo Hikyo-Org/Hikyo --ref main
   ```

   Expected: hosted smoke and admission checks run; an admitted Macro job measures and uploads the full suite. Inspect the actual runtime and upload before considering the integration verified. A timeout is a failed measurement, not proof that the suite fits.

3. Open or update a PR after the workflows are on main. Expected: one Performance comment and a hosted request check. Tick **Run benchmark** as a maintainer. Expected: the same exact-head PR workflow starts another attempt, an assigned Macro job runs, and the comment links to its result. CodSpeed's asynchronous analysis may appear separately.
4. If the credit grant is active and eligible, confirm its balance/expiry and set the account's monthly Macro budget in CodSpeed Billing. Only then enable the local credit option:

   ```sh
   gh variable set CODSPEED_CREDIT_CONFIRMED --repo Hikyo-Org/Hikyo --body true
   ```

   Expected: checked credit requests are accepted within their additional allowance. Leave the variable unset while grant details are uncertain, and disable it when the credit expires or is exhausted. The code does not spend overage automatically.

## Validation and tooling

Local validation covers budget boundaries, failed jobs, queue-only cancellations, billing-window overlap, prior attempts, both workflow histories, API failures, exact head/run/attempt identity, unauthorized edits, forged bot markers, modified benchmark YAML, and skipped measurements. A process-level fake GitHub CLI exercises the successful checkbox request through rerun and result reporting, without live comments or spending.

Commands from the repository root:

```sh
go test -race ./scripts/ci/benchmark ./scripts/ci/workflow-lint ./scripts/ci/codspeed-budget ./scripts/ci/pr-benchmark-control ./scripts/ci -count=1
go vet ./scripts/ci/benchmark ./scripts/ci/workflow-lint ./scripts/ci/codspeed-budget ./scripts/ci/pr-benchmark-control
./scripts/ci/run-go-tool.sh actionlint
./scripts/ci/check-cache-policy_test.sh
./scripts/ci/check-trusted-ci-scripts_test.sh
shellcheck scripts/ci/run-go-tool.sh scripts/ci/check-cache-policy_test.sh
```

The repository-pinned Actionlint 1.7.12 predates GitHub's supported `concurrency.queue` field. `workflow-lint` validates its enum, cancellation compatibility, duplicate keys and standalone syntax, preserves diagnostic line numbers, then sends the remaining unchanged workflow to pinned Actionlint. No Actionlint error pattern is ignored. Existing action-pin, runner/cache and trusted-CI policy checks remain active; only the two admitted benchmark lanes may use the approved Macro label. Ordinary CI's package inventory includes the new Go packages and their regression tests.

Local checks have passed. Remote CI, live bot posting, Macro execution and CodSpeed upload have not been verified for this change. Repository variables and account settings remain unchanged. Activation steps require the workflows to be merged first.

## References

- [Requested Rewrite checkbox example](https://git.dbugit.io/DBugIT_V3/Rewrite/pulls/698#issuecomment-28409)
- [CodSpeed Macro runner pricing](https://codspeed.io/docs/features/macro-runners)
- [CodSpeed monthly budget setting](https://codspeed.io/docs/features/seats-and-billing#macro-runner-budget)
- [CodSpeed Go compatibility](https://codspeed.io/docs/benchmarks/go#compatibility)
- [GitHub queued concurrency](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency)
- [GitHub workflow rerun API](https://docs.github.com/en/rest/actions/workflow-runs#re-run-a-workflow)
- [Actionlint queue support issue](https://github.com/rhysd/actionlint/issues/657)

## UX coverage extension

Daily main and admitted PR requests also run the large-matrix browser check on
GitHub-hosted Chromium. This adds no CodSpeed Macro usage. The Go selection now
includes publish, mixed import, long-lived history and sparse diff measurements.
See [UX performance benchmark additions](ux-performance-benchmarks.md) for the
exact datasets, reset semantics, browser ceilings and local verification.

## Review and CI repair

[PR #863](https://github.com/Hikyo-Org/Hikyo/pull/863) includes fixes for its first review and CI run:

- Initial PR discovery returns `allowed=false` without invoking a verifier that has not landed on main. Reruns still use only trusted main tooling and exact head/run/attempt admission.
- Integrity checks cover the reusable browser workflow as well as the Macro workflow. Browser checkout comes from the original event; scheduled/main calls accept no caller-selected ref.
- Passing browser tests write their timing reports to disk and attach those paths. Local verification found reports for both viewports in the strict upload directory. The automated preview binds to loopback.
- PR smoke is a separate cancelling workflow; the paid main lane retains serialized queued admission.
- The ledger avoids jobs queries for initial PR attempts. It retains paginated old-run discovery because an old run can be rerun within the billable window.
- Formatting was corrected. Core CI prepares cross-platform compiler exports before tests, then runs repository analysis after the runtime and app suites. This combines main’s PR #860 cache preparation with this PR’s separate analysis scheduling. Inventory tests enforce each package once, ordering, and failure propagation.
- The reported PR merge-SHA issue was rejected using the live REST response: run `37799478288` has `head_sha=94f34741cdb27fe1cc2509386b7dfc4c19767f1d`, matching the PR branch head, not a synthetic merge SHA.
- The suggested `-benchtime` flag was rejected because CodSpeed supports only `-bench`. The complete native default-duration suite passed in 60 seconds locally; this is not hosted Macro runtime proof. The paid 15-minute job timeout remains unchanged.

After repair, local browser checks, typecheck, Oxlint, native benchmark timing and controller/workflow regression checks passed. Remote checks for the follow-up commit remain the final CI verification boundary.
