# Trusted CI gate bootstrap

Workflow-edit PRs fail in the base-controlled gate because `gh api` rejects
`--slurp` combined with `--jq`. The script now retrieves all review pages first,
checks retrieval success, and applies standalone jq with its own failure check.
Latest-review, exact-head, independent-reviewer and current-permission checks
are preserved. Regression fixtures enforce the real CLI option restriction.

The gate also rejects known merge conflicts immediately instead of polling for
a `pull_request` validation run that GitHub cannot start. Unknown mergeability
continues polling even after validation completes; the gate passes only after
GitHub reports mergeable=true. Fixture coverage checks the immediate conflict
diagnostic, pending mergeability at the final check, and resolution on a later poll.

This PR deliberately contains no `.github/` changes: the existing gate can
validate it through ordinary PR-scoped CI without calling the broken approval
lookup. Merge this script-only repair before workflow-edit PRs such as #850
and #852, then rerun their trusted-ci jobs after exact-head maintainer approval.
The checkout uses the event's base SHA; if a rerun retains the old base SHA,
synchronize the workflow PR with updated main to trigger a fresh event.

Local validation: gate fixture suite, ShellCheck and `git diff --check`.
Merge requires ordinary exact-head CI; no check or trust boundary is bypassed.
