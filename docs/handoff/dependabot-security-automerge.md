# Handoff: security-only Dependabot auto-merge to main

Part 1 of the split of the unattended security patch release work (owner
decision, 2026-11-15, option B). The human tag ceremony stays. This change
tags nothing, publishes nothing and adds no App, token or secret.

## Result

`dependabot-security-automerge` (workflow, script and fixture) arms auto-merge
for a Dependabot pull request into main only when all of these hold:

1. An open Dependabot alert links to the pull request (GraphQL
   `vulnerabilityAlerts(states: [OPEN])`, `dependabotUpdate.pullRequest`).
   Nothing about the name, label, title or branch decides this.
2. The author is Dependabot by numeric id (49699333) and login, the head is in
   this repository on a `dependabot/` branch, and the pull request is not a draft.
3. Every commit is authored by Dependabot (id 49699333), committed by GitHub
   (web-flow, id 19864447) and verified, and the checked head is one of them.
4. It changes only `go.mod`, `go.sum`, `package.json`, `package-lock.json`,
   `pnpm-lock.yaml` and `yarn.lock` (any directory), with no rename from
   another kind of file.
5. The head's `ci-queue-required` check, from the Actions app, is not failed or
   cancelled. A refused admission is not re-armed, so a failing update cannot
   loop through the merge queue.

The arm is `gh pr merge --auto --merge --match-head-commit <checked head>`, so a
push after the check cannot slip in. A pull request that fails any rule is
reported (`Not queued: ...`) and fails the run, so a stuck security update is
visible. A routine Dependabot update that no alert links to is reported as left
for a person and does not fail the run.

## How a security update is identified

The alert link is the only reliable source: a version update has none, and a
security update's body carries no distinct marker (the GHSA ids in a body come
from upstream release notes). Reading it needs `vulnerability-alerts: read`.
GitHub added that scope to the workflow token, so no PAT, App or secret is
needed. This was verified on 2026-11-15 with a throwaway `push` workflow on a
scratch branch (deleted afterwards): GitHub accepted the scope, the job log
listed `VulnerabilityAlerts: read`, and both the GraphQL `vulnerabilityAlerts`
field and the REST `dependabot/alerts` endpoint answered with the workflow
token. The pinned actionlint (1.7.12) does not know the scope yet, so
`.github/actionlint.yaml` ignores that one message for this one workflow.

## Evidence

- `dash scripts/ci/dependabot-security-automerge_test.sh` (fake `gh`, 39 mutations
  of the script and workflow each fail it or the Go workflow test).
- `go test -p 2 ./scripts/ci/...` through `agent-heavy`.
- shellcheck, the pinned actionlint, gofmt.

## Not verified, and the risk

- **Dependabot-actor token.** The sweep also starts on `pull_request_target`
  for a Dependabot pull request. GitHub may hand a Dependabot-triggered run a
  read-only token. If so that run fails at `gh pr merge`, and the hourly
  schedule, which does not run as Dependabot, arms the pull request instead.
- **Merge queue entry by the workflow token.** Arming with the workflow token
  should enqueue the pull request when its checks pass. If the merge-group run
  does not start for an entry that the workflow token armed, the entry times out
  after the ruleset's 60-minute response window and the pull request waits for
  a person. This cannot be observed before merge.

## Live checks after merge (read-only unless noted)

1. `gh workflow run dependabot-security-automerge.yml --ref main` (queues
   whatever qualifies, so run it when a security update is open) and read its
   summary. Expect a line per open Dependabot pull request into main.
2. For an armed pull request: `gh pr view <n> --json autoMergeRequest` shows
   the merge method `MERGE`, and a merge-group run starts for it.
3. Dependabot-opened pull request: its run of this workflow shows a green
   `sweep` job, or a red one that the next hourly run supersedes.

## Remaining work in this series

Compatibility check, Homebrew follow-up and a reconciler that prepares the
admin's tag commands follow as separate pull requests.
