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

## Validation

- `pnpm --dir scripts/mcp-conformance install --frozen-lockfile` passed.
- `pnpm --dir scripts/mcp-conformance audit --prod --json` reported zero
  vulnerabilities.
- `sh scripts/ci/check-mcp-conformance.sh` passed the actual Inspector CLI smoke
  and all three scenario baselines. The caching scenario retains its three
  existing expected unsupported prompts/resources cases; no new failure was
  accepted.
- `scripts/ci/fast-feedback.sh --working-tree` passed its selected whitespace
  check. Its domain plan is empty for these inputs, so it is not the MCP
  compatibility evidence.
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
