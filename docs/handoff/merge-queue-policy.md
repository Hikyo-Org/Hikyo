# Mandatory merge queue policy handoff

This follow-up adopts the protected queue configuration already activated after
bootstrap PR #891. It changes canonical policy, local policy/configuration
fixtures and contributor/operator documentation. It also corrects the queue
publisher's exact recognition of GitHub's observed normalized skipped metadata
job name. Workflow, application, job-registry and frozen full-suite execution
remain unchanged.

## Contract

- `ci-queue-required` admission binds the exact current PR head to useful hosted
  feedback and current source authority. Admission authorizes enqueueing only.
- The mandatory merge queue requires full current-base group proof across all
  16 domains before merging. Rebuilt or reordered groups need fresh proof.
- Queue parameters are `ALLGREEN`, build concurrency one, merge limits one,
  zero minimum wait, `MERGE`, and a 60-minute check-response timeout. Strict
  branch-up-to-date checking is false only with that mandatory queue/context.
- Signatures, DCO, stale-review dismissal, resolved threads and zero owning
  bypass actors remain required. Fork vouch/run approval remain independent.
- `ci-required-next` and `ci:full` remain the permanent full-only rollback route.
  Queue admission never writes that context, so old admission greens cannot
  satisfy restored strict legacy protection.

## Local configuration

Run `scripts/release/configure-repository.sh --main-only Hikyo-Org/Hikyo` only
with operator authorization and administrator access. It reads the unique
existing main ruleset, validates its queue or legacy contract, preserves every
unrelated live rule/PR parameter, atomically replaces only the queue/status-check
rules, and verifies writable readback, treating absent/null unspecified integration
bindings equivalently while retaining every explicit integer binding. Discovery
validates each 100-result page and refuses before any PUT if still incomplete
after 20 pages. It refuses missing or redacted
actor policy, duplicate/absent owning rulesets, malformed JSON, weakened policy,
API errors and readback drift. It never changes tag/release settings, CodeQL,
Actions permissions or git refs. Broad initial configuration retains those
existing responsibilities and can still create the initial main ruleset.

The canonical legacy JSON is a reference. Actual recovery uses the exact saved
pre-transition legacy writable policy, not a newly synthesized approximation.
The CI guide contains the validated capture, restore and equality commands.

## Local validation

The initial complete CI tooling Go package passed (25.338 seconds). The expanded
policy/configuration regression check passes (4.875 seconds), including 32 actual
queue-policy mutations, five malformed legacy-policy cases and 20 mocked
main-only API scenarios. Vet and ShellCheck pass. The existing CodeQL setup and
nightly/release preservation fixtures pass. Pinned documentation diagnostics
report zero errors, warnings or hints; the site builds successfully and the
actual OSS policy fixture passes its positive controls and refusals.

These checks validate the local follow-up, not hosted populated-group acceptance.
No application-wide suite was repeated locally, and no policy mutation or CI
run was dispatched by this follow-up preparation.

## Review follow-up

Five new source-bound checks fail on original head `3423e328c`: a valid positive,
zero or null integration binding, GitHub's nullable readback representation,
and a later-page main owner. They pass after preserving the binding and fully
validating bounded discovery. Cross-page duplicates, malformed/incomplete pages
and changed concrete bindings remain refused. The API fixture now asserts the
ordered list-page(s), owning GET, atomic PUT and final GET sequence.

Main publication also encountered GitHub's normalized spelling of a single
completed/skipped metadata-only matrix job. Architecture reproduced that exact
job name from run 38233557251, then added only its exact canonical spelling to
the existing ignore list. The regression still refuses unknown names, different
events, unfinished/failed/successful jobs and duplicate metadata jobs; useful
feedback and full group proof remain necessary. Its race test passes (6.087s).

## Evidence and remaining hosted acceptance

Bootstrap main is `21773f333b4c37cac61b650baf2bf2c154263e9a`.
The actual minimal trusted-main workflow-token active-empty-queue probe
[38230781293](https://github.com/Hikyo-Org/Hikyo/actions/runs/38230781293)
passed. Protected owning/effective policy and empty queue were captured before
and after. Success follows the forced active-policy `repairGroups -> liveQueue`
path; an inactive workflow success or administrator API request is not the
same capability proof. Redacted bypass actors are explicitly unobservable to
the workflow token; the administrator separately verified zero live actors.

This legitimate policy/docs PR is the first populated hosted acceptance.
Actual event/ref, group/member mapping, all 16 domains, trusted required proof,
landed tree and post-merge protection still require verification. Empty-queue
capability and local fixtures do not prove those outcomes.

No tests or fast checks are removed. Required benchmark compilation/smoke remains
in the complete graph; optional paid performance measurement has a separate
budget and does not become a required paid job. Local hooks are not substitutes
for remote exact-group proof. Build concurrency one does not reserve hosted
runner capacity or guarantee five-minute feedback, and cold caches or group
rebuilds can still cost time.

## Entry points

- [CI operator guide](../development/ci.md)
- [Contributor guide](../../CONTRIBUTING.md)
- [Canonical queue policy](../../release/repository/main-ci-gate.json)
- [Legacy reference policy](../../release/repository/main-ci-gate-legacy.json)
- [Historical bootstrap handoff](trusted-merge-queue.md)
- [GitHub merge queue semantics](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/managing-a-merge-queue)
