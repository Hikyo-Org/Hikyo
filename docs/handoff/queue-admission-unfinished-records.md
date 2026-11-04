# Queue and dev admission: an unfinished run no longer looks like a moved PR, and says what it is

## Problem

Dev admission (`gate-dev`) and queue admission (`gate-queue`) run a trusted publisher in a one-minute budget. When it fails or runs out, a reserve publishes a blocking record: a pending one while a rerun remains, a terminal refusal on the last attempt.

1. PR #1160, run 41685149674, attempt 3, 17:40:55: the budget ended inside the feedback jobs read, so the trace never captured the feedback run. The reserve compared "no feedback captured" with the feedback it read now, reported `useful feedback attempt moved during admission failure publication`, and wrote nothing. The check kept the pending record from the earlier attempt, which says a rerun follows, with none left.
2. That record's text, and the refusal's, claimed more than the publisher knows. Every error reaches them (verifier, DCO, GitHub reads), the refusal said "full queue validation remain required" for dev admission, which does not run it, and the pending one stated a rerun as fact. The dev job log said the barrier "stays until the rerun reassesses this head".

## Change (`scripts/ci/pr-gate`)

- `admissionTrace` records that each read returned: `FeedbackRead` and `PolicyRead`. A trace whose read never returned (a cut budget or a failed read) has nothing to compare it with, so a difference there is not movement and the reserve publishes as it would for any unfinished run. `Policy` stays nil for a protection that returned disabled (the queue off), so nil alone cannot mean cut off: a returned disabled protection keeps main's behaviour (the reserve judges nothing and reports `current protection moved or was not captured`), which also reports a protection enabled since. A returned enabled protection is compared as before. A truly moved tuple behaves exactly as on main. A protection read that never returned, with the queue off in the reserve, also keeps main's no-write: the schedule, a push to main and a dispatch do nothing while the queue is off, so the refusal's text would be wrong.
- A queue-context refusal no longer lands on a dev PR. Feedback callbacks run `gate-queue` for dev PRs too; the normal pass retires that head's pending record, but retirement skips completed records, so a refusal would stay red on that SHA for good, with text that is false for it. This also closes main's same hole for cuts after the feedback read.
- The pending and refusal text (`pendingText`, `refusalText`) says the publisher did not finish because it failed or ran out of its budget, and that merging stays blocked. A rerun is promised only while attempts remain. It names what reassesses the head, for the context: a new push to this PR, a feedback result, a push to main, the schedule, or `gh workflow run gate-dev -f pr_number=N` (dev) or `gate-queue` (queue). A push to dev is not named: it reassesses nothing unless it changed the dev publisher or its range cannot be determined. The refusal lists what stays required per context (fast feedback, DCO and policy for dev; feedback and full queue validation for queue) and says to fix an error that persists, because the linked run names it.
- Those claims were checked against `ci-dev-gate.yml` and `ci-queue-gate.yml` (pull request events, feedback `workflow_run`, push to main, schedule, `pr_number` dispatch) and `scripts/ci/discover-dev-prs.sh` (a dev push reassesses only when it changed the dev publisher). Both workflow names resolve with `gh workflow view`.
- The job log line for a moved dev admission (`queueMovement`) no longer states a rerun as fact.
- No workflow, script, budget or retry-bound change.

## Evidence

`queue_unfinished_test.go`: the feedback cut on attempts 1 to 3 on both contexts (attempts 1 and 2 leave the pending record, attempt 3 the refusal, and no error says moved); the protection read never returned, returned disabled, returned disabled and enabled since, and returned enabled and moved; the protection read cut with the queue off in the reserve; no queue refusal on a dev PR; the text per context; the log line. Each condition was mutated and made a test fail, including the dev PR and queue-off conditions: main's feedback and protection comparisons restored, `PolicyRead` never recorded, the earlier "nil means cut off" version, the enabled comparison dropped, the dev command, the dev required list, a rerun stated as fact, the blocked sentence, and the log line. The new tests pass with `-race` and with `GOMAXPROCS=16 -count=50`. No existing test was edited.

## Limits

The last attempt can still leave a pending record when the budget ends before a trace exists, when the reserve can neither read nor write, when the tuple truly moved, when setup fails (the publisher never runs), or when the runner is lost or the retry workflow is disabled. That record keeps the text of the last write that reached it, so it can be the plain fail-safe "Current held-head proof is incomplete", which names nothing that reassesses the head. `gh workflow run gate-dev -f pr_number=N` (or `gate-queue`) reassesses it. #1194 narrows the case of a verdict whose write lands after the work clock; this change does not touch it.

## Parked work

By owner decision two pieces of work are parked. Both branches exist only on the owner's machine and were never pushed, so nothing here depends on them.

- `parked/queue-admission-lost-verdict-guard` kept a pass that reached GitHub although its reply was lost, by matching the record ID and binding on read-back. #1194 narrows the overwritten pass by giving the verdict's write more time.
- `hotfix/queue-publisher-prebuilt` fetched a publisher built by a main-only seed workflow, falling back to compile. Two security reviews found different holes: a tag named `main`, then a commit reachable from main carrying a modified seed workflow. Its third version, which requires the seed workflow blob at the run's commit to equal main's, was not reviewed. #1170 and #1187 have since merged and cut setup cost, so re-measure publisher failures before reconsidering it.

`TestAppleSigningFixtures` hangs on `origin/main` by itself; validation skips it.
