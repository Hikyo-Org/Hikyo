# Merge policy without a required queue

The owner approved replacing the required merge queue with strict up-to-date
`ci-required` checks on 2026-10-09, after PR #864's queue validation exceeded the
90-minute queue timeout despite green PR validation.

The canonical policy is `release/repository/main-ci-gate.json`. It removes only
`merge_queue` and sets `strict_required_status_checks_policy` to `true`.
Required PRs, signatures, review-thread resolution, protection against deletion
and force pushes, and the empty bypass list remain. The live ruleset update
preserves all other live rule parameters, including GitHub-added parameters.

A green PR that contains current `main` can merge directly. If `main` advances,
update the PR branch and validate the new head. There is no mandatory second
full suite for an unchanged, current candidate. Main-branch CI remains.
The existing `merge_group` workflow path is retained for compatibility but is
inactive while no queue is required.

Local validation: JSON policy assertions, changed-path classifier fixtures,
Git diff whitespace checks, and a comparison proving all other live rules were
preserved. The live ruleset was read back to verify strict CI, signature
protection, no queue requirement, and no bypass actors.

The owner-approved operational change is recorded in the OSS mechanics ADR.
Formal cross-model amendment review has not been performed.

PR #864 must receive fresh validation against current `main` before merging.
This handoff does not claim CI completion or merge.
