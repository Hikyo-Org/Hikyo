# CI reliability repair, October 9, 2026

This branch addresses the six findings from the recent Hikyo CI investigation in one PR. The authorized endpoint is a reviewed PR with CI evidence. Merge, release and deployment remain separate.

## Implemented changes

1. Incorporate main PR #865, which concurrently landed Go 1.27.2, golang.org/x/net v0.60.0 and the compatible x/tools v0.50.0. CI and release setup read go.mod. Both rebuilt binary shapes pass local govulncheck with zero symbol-level findings. Remove obsolete dependency checksums left behind by that update. Add daily linked-binary scanning and stable compiler patch proposals to prevent recurrence.
2. Preserve the existing lint-cache preparation and docs-link fixes. Log elapsed preparation time for every default, UI, Darwin and Windows context. Main PR #865 also updated the source-build prerequisites to the patched compiler.
3. Refresh race package weights from all eight jobs in green main run 37836518512. Plan twelve PR race shards with an explicitly estimated 20-second process-startup allowance. Preserve the 30-minute job and 20-minute package limits. Capture per-target Go JSON and invocation wall time for later calibration. Split the scheduled isolation race suite into eight independent PostgreSQL jobs, preserving its 150-minute test timeout. Prove all 564 discovered targets, including fuzz seeds and runnable examples, appear exactly once.
4. Add the stable GitLab ErrSPKIPinMismatch cause and use errors.Is through redacted transport errors. Exercise Version against real local TLS, verifying the pin mismatch sends no HTTP requests. Keep normal certificate authority validation and public error redaction.
5. Give the trusted fork gate categorized failure summaries and direct links to upstream failing jobs. Retry only classified read-only network, HTTP 429 and HTTP 5xx failures, at most three attempts. Permanent failures, malformed JSON and invalid metadata fail closed. Keep exact-head, approval, author and trusted-base execution policies. Add daily module update proposals through Dependabot while preserving the pinned API generator.
6. Preserve the existing mobile touch-target assertions and mail deadline-ordering repair. The focused deadline test passed thirty local repeats during investigation; the combined repair also runs the mail race tests.

## Security maintenance

The new go-security workflow scans fresh UI-tagged and untagged linked binaries daily and on manual dispatch. It has contents-read authority only, uses locked frontend dependencies, and does not consume PR artifacts or publish caches. Both scans execute even if the first reports a vulnerability.

A separate read-only check fetches official Go release metadata and proposes only a newer stable patch within the current major.minor line. A stale pin fails and publishes an applyable go.mod.patch plus a summary. It never edits the checkout or authors unsigned bot commits. Dependency PRs and compiler proposals still require normal signed delivery, review and validation.

## Validation and remaining evidence

Local checks cover both vulnerability scans, API and CI Go tests, GitLab and mail race tests, planner inventory and scheduling fixtures, fork-gate and patch-proposal fixtures, Go import formatting, ShellCheck, pinned Actionlint and full docs verification. Preparation and repository lint checks run with the new compiler. The broader local core Go suite was interrupted after its PostgreSQL instance exhausted the Docker VM disk (SQLSTATE 53100) and stopped. This run is not passing evidence; full database coverage awaits hosted Linux CI. Only task-owned test resources were removed. On head 9826f9d20, the candidate GitLab lifecycle run 37927372692 passed in 7m06s, and all eight scheduled isolation race shards in run 37927376736 passed with assigned runtimes of 27m52s to 39m10s. These validate the lifecycle and isolation changes on that head; subsequent gate-only changes still require current-head PR CI.

The twelve-shard cost model predicts 741 to 751 seconds before workflow overhead. This is a planning estimate, not hosted duration proof. Ordinary PR validation deliberately uses the trusted base planner and scheduler, so new hosted timing artifacts begin when the changed scheduler executes on trusted main. The scheduled isolation workflow can validate its own candidate implementation via an authorized branch dispatch. Retain that trust boundary.

Historical reference runs: security failure 37908112958; cold-cache and docs failure 37787217921; race timeout 37778272046; GitLab E2E failure 37307626983; scheduled isolation timeout 37300715184. The existing unsupported CodSpeed flags and gh pagination repair are retained rather than rewritten.

## PR review repairs

PR #869 review identified valid GitHub App author logins rejected by metadata validation, compiler build-output events hidden from readable race logs, and untested lint timing output. Permit one optional literal [bot] author suffix while retaining workflow approval and human reviewer identity checks. Both race log filters now retain compiler diagnostics and preserve failing exit status. Fixtures exercise bot approval boundaries, malformed identities, actual workflow compiler errors, and successful versus failed lint preparation timing.

## Queue timeout recovery

The current-head trusted gate exhausted its unchanged 90-minute polling window while GitHub still queued healthy fork validation jobs. No test failure caused this gate failure. Rerun only the failed trusted gate after validation has progressed or finished. The added default-branch workflow_run callback performs this recovery automatically after fork-ci completion: bind repository, workflow, PR, head, latest run and attempt using GitHub API metadata, then request at most one rerun of a failed or timed-out ci-required job. A bounded read-only settling check handles the race with trusted-job cleanup. Never retry the POST, execute PR tooling, publish success directly, extend the original polling window, or relax approvals. The original gate repeats its exact-head and maintainer checks. This callback becomes active after it lands on the default branch. Pending-job timeout summaries now show job progress and the actual validation URL.
