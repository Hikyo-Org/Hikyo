# Merge queue controller handoff

The implementation starts from main `868703c75cb5ee65b073f09b1790d874378dc0f0`. Queue mode is inactive until an explicit protected ruleset transition. Merging this PR does not enable admission-only merges.

## Check meanings

- `ci-required-next` remains the existing full current-main candidate proof, including the `ci:full` recovery route. It is never written by queue admission.
- `ci-queue-required` is a separate context. With mandatory queue inactive it blocks. With live queue policy active it admits an exact PR head only after useful Go/frontend feedback, current workflow authority and fork vouch, and canonical DCO. The queued merge-group SHA needs the entire full suite and trusted publication proof before merge.
- `merge-group-proof` is a read-only evidence marker. Native workflow jobs cannot intentionally declare either trusted required context.

## Validation and trust

The unchanged reusable CI workflow forces all 16 domains for merge groups. The job registry, frozen coverage baseline, application tests, fast lanes and their limits are unchanged. New controller tests cover policy drift, complete membership, source/run/attempt identities, fork vouch, DCO, stale callbacks, base ancestry, planner/scheduler compatibility, API errors and late native-check collisions. Both permanent final publication race regressions and the independent adversarial replays pass. The complete CI tooling package, gate race test, vet and pinned Actionlint pass locally.

The publisher executes trusted default-branch code only. Canonical DCO reads bare immutable Git metadata, never PR code or hooks. Current-main advancement can invalidate group proof; it does not require an unchanged PR head to obtain fresh fast feedback. Rebuilt or reordered group identities require fresh full proof. Build concurrency one limits full group dispatch, not reserved fast-runner capacity or a universal five-minute guarantee.

GitHub Actions check names are not cryptographic producer identity. The controller refuses foreign/native same-name checks, posts owned blocking results and never edits foreign checks; the underlying platform name ambiguity existed before this change. Runtime uses owning/effective live rules. Redacted bypass actors remain explicitly unobservable, while visible actors are rejected. An administrator must independently verify zero bypass on activation/readback.

## Protected rollout

1. Pass the existing exact-current-main full candidate gate on this bootstrap PR and merge it while queue mode is inactive.
2. Observe actual trusted-main workflow-token access to effective rules and queue GraphQL fields. Do not substitute administrator-only capability evidence. Preserve the legacy producer because queue initialization and full-proof rollback depend on it.
3. Save the complete live writable ruleset and executable restoration payload. Atomically replace the required context with `ci-queue-required`, set mandatory `ALLGREEN` queue with build limit one, merge limits one, zero minimum wait and 60-minute check timeout, and change strict-current-branch policy to false only in that same transition. Retain every other protection and verify exact readback. Do not use the broad repository configuration script for this isolated transition.
4. Put the legitimate canonical queue-policy/operator-guide follow-up through the queue. Capture actual group event/ref, workflow-token queue entries, complete member mapping, all 16 domains, trusted required proof and landed tree. Entry `headCommit` semantics are a hosted acceptance requirement, not an assumption already proved by fixtures.
5. If capability or proof fails, keep merges blocked or atomically restore the saved strict `ci-required-next` policy. Stored or late queue admission greens cannot satisfy the restored full-only context. Preserve the producer and review every remaining activation change.

The queue is not claimed active or working until the first real full merge-group run and merge are verified. Expected stale groups remain blocking pending; malformed API, source, identity, collision and write errors remain operational failures.

Primary semantics: [GitHub merge queue](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/managing-a-merge-queue), [GitHub repository rules API](https://docs.github.com/en/rest/repos/rules).
