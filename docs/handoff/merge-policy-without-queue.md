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
Git diff whitespace checks, documentation status validation (35 entries), and a comparison proving all other live rules were
preserved. The live ruleset was read back to verify strict CI, signature
protection, no queue requirement, and no bypass actors.

The owner-approved operational change is recorded in the OSS mechanics ADR.
Formal cross-model amendment review has not been performed.

PR #864 must receive fresh validation against current `main` before merging.
This handoff does not claim CI completion or merge.

GitHub still rejects both its update-branch API and a signed Git push for #864
as queued, although GraphQL reports `isInMergeQueue: false` and no queue entry.
Re-enqueue/dequeue, cancellation of the old queue run, and close/reopen did not
clear the lock. The PR is open, the live queue requirement remains removed, and
the stale queue branch could not be deleted for the same reason.
A signed merge of current `main` is prepared at `/tmp/hikyo-864-update`, commit
`c4e61440da2642e9b9503e5d58b6fb6f85432678`, but has not been pushed.
Its complete range verifies cryptographically; the original bot commit passes
the repository's existing Dependabot DCO exemption after API actor verification,
and the new merge commit carries the maintainer's DCO sign-off.
GitHub must clear its stale lock before the original branch can be updated.
