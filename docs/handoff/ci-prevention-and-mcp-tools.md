# CI prevention and MCP tools repair

## Change

Recent validation missed shared policy fixtures, and a local shell batch hid an
earlier failure behind a later successful command. The agent guide now directs
local feedback through the existing shared driver, requires affected consumer
checks, propagates every required failure, and records revision-bound evidence.
Pending advisory checks remain distinct from completed merge proof.

Dependabot could not lift the MCP client/server from 2.2.0 because Inspector
2.10.1 pinned that version. Inspector is now pinned to 2.11.0, with MCP client,
server and core at 2.3.1. The conformance runner and its baseline are unchanged.

## Merge-queue fixture repair

[Merge-group run 39623432104, attempt 1](https://github.com/Hikyo-Org/Hikyo/actions/runs/39623432104/attempts/1)
tested PR head `85ad41900b39` on base `352c3d380841`. Its only root failure was
`TestQueueAuthorityFailureCancelsOtherReadBeforePhaseReturns`; the required
aggregate and group-proof jobs failed downstream.

The paired-read fixture introduced in PR #923 can observe both its readiness
barrier and cancellation channel closed. Selecting cancellation at that first
barrier returned without incrementing the counter, even though production code
had cancelled and joined the reader. Local repetition reproduced five failed
subtests. The fixture now counts cancellation on either return path. Assertions
still require the original error, an uncancelled parent, one cancelled sibling
and no active readers. Production controller behavior is unchanged.

## Validation

- `pnpm --dir scripts/mcp-conformance install --frozen-lockfile` passed.
- `pnpm --dir scripts/mcp-conformance audit --prod --json` reported zero
  vulnerabilities.
- `sh scripts/ci/check-mcp-conformance.sh` passed the actual Inspector CLI smoke
  and all three scenario baselines. The caching scenario retains its three
  existing expected unsupported prompts/resources cases; no new failure was
  accepted.
- At the documentation/MCP-only revision `85ad41900b39`,
  `scripts/ci/fast-feedback.sh --working-tree` passed its selected whitespace
  check with an empty domain plan. This is not the MCP compatibility evidence.
  The follow-up Go fixture repair selects Go feedback and also requires the
  affected controller tests.
- The cancellation regression passed 1,000 repetitions at each of one and two
  CPUs, covering both endpoint orderings, after reproducing five failures before
  the repair.
- `go test -race -p 2 -count=1 ./scripts/ci/pr-gate` and
  `go vet -p 2 ./scripts/ci/pr-gate` passed.
- `scripts/ci/fast-feedback.sh --working-tree --lane=go --go-scope=tooling`
  passed formatting, whitespace and compilation for the affected Go package.
- The package defines no TypeScript typecheck or ESLint configuration. No JS/TS
  source changed, and unrelated web checks were not run.

## Follow-through

Hosted exact-head validation and protected merge-group proof remain required.
This change adds no CI jobs, admission dependencies or deadline increases.

Publisher optimization remains separate: prior PRs #901, #902, #917 and #923
already removed duplicate setup and reused or overlapped reads. Measure the
complete cold/warm path, including shell-verifier API calls, before choosing
another implementation. Additional early policy profiles need scoped input
selection and timing evidence before they become admission dependencies.
