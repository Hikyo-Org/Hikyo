# CodSpeed Go runner compatibility

The CodSpeed job in run 36993203753 failed before executing benchmarks.
Its Go runner ignored the unsupported `-run` flag but interpreted the following
`^$` value as a package path, producing `malformed import path "^$"`.

The workflow now uses `go test -bench=.` with the existing five package paths.
CodSpeed generates its own benchmark runner, so the usual Go flag for skipping
tests is unnecessary. Keep native Go test flags out of this command unless the
CodSpeed Go integration explicitly supports them.

Compatibility reference: <https://codspeed.io/docs/benchmarks/go#compatibility>.

Validation: Actionlint, existing workflow action-pin tests, and the CodSpeed job
on the pull request. The hosted job is the integration check for instrumentation
and upload; native `go test` alone cannot validate CodSpeed's argument parsing.

Hosted run 36993711345 passed on ed30893f2620a435402cbc8dd7b5bafb0697aa97:
11 benchmark cases executed and performance data uploaded in a 3m 37s job.

The trusted gate also had two independent defects. A conflicted PR could wait
for a `pull_request` validation run that GitHub cannot start; it now fails
immediately when the API reports `mergeable: false` (unknown remains pending).
Workflow approval lookup combined unsupported `gh api --slurp --jq` flags; it
now retrieves all pages first and filters with standalone jq, checking both
commands for failure. Fixture tests enforce CLI compatibility and conflict
diagnostics. Workflow edits require independent exact-head maintainer approval,
except same-repository PRs authored by the pinned BDFL (GitHub user ID 991668)
while that account retains current maintainer permission. Every PR still needs
successful exact-head validation and DCO. GitHub's active
[`main requires PR and release CI` ruleset](https://github.com/Hikyo-Org/Hikyo/settings/rules/20539346)
enforces signed commits for changes landing on main. Merge queue validation
checks the combined candidate on `merge_group` events before it lands on main.
