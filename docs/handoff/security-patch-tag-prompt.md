# Handoff: security patch tag prompt

Part 4 of the split of the unattended security patch release work (owner
decision, 2026-11-15, option B). The human tag ceremony stays. This change tags
nothing, publishes nothing, changes `release-build` not at all and adds no App,
token or secret. Unattended tagging is deferred to a later App phase.

Stacked on part 2 (`feat/compat-check-last-stable`, #1406): the issue's
compatibility statement uses `check-compatibility-unchanged.sh`. Merge #1406
first; this PR then retargets to `dev`. The actionlint ignore for the
`vulnerability-alerts` scope is the separate prerequisite #1413, which must be on
`dev` first (ci-feedback runs actionlint from the trusted base checkout).

**Delivery.** Feature pull requests land on `dev` and reach `main` by the normal
promotion. The workflow's push-to-main, schedule and manual triggers, and its
`github.ref == refs/heads/main` guard, mean **nothing runs until a promotion
lands it on `main`**. `recheck` and the fixture work from `dev` today.

## Result

- `scripts/release/security-patch-plan.sh plan` decides, read-only, whether a
  merged Dependabot security update is missing from the latest published stable
  release, and which commit would carry the patch: the newest of the last 60
  first-parent commits of main that holds every pending security merge, records
  the latest stable release in trust, and has complete green source proof. A red
  tip falls back to the last green commit and never goes below the newest
  pending merge. It then checks trust synchronization, authentication, the
  candidate, main history, source proof, stray tags, existing releases and, last,
  compatibility with the previous stable release.
- `issue FILE` applies the plan to the single issue (marker, title and the
  workflow's own bot as author, because anyone can open an issue with the
  marker and its author could later edit commands people copy), edits only on
  a change, closes it when nothing is waiting.
- `recheck VERSION COMMIT [--tip]` is the first link of the issue's command. It
  refuses unless the checkout is at the planned commit and every pending
  security merge is in it (no grace period), the version is the patch
  successor, trust records the latest release, and the tag checks pass.
- With a green tip the command is `recheck ... --tip && ceremony.sh build`.
  With a red tip `build` cannot be used (it tags the tip), so the issue gives
  an explicit signed tag of the green commit plus its push. The review, publish,
  sync-trust and Homebrew commands follow in both cases; Homebrew is last.
- Fail closed: unparseable timestamps, unreadable GitHub answers, unplaceable
  security merges and FIXED alerts whose pull request is missing, unmerged or
  without a fix time stop the run or reach the issue as "needs a person";
  nothing is silently skipped. Only the newest check run is judged.
- `.github/workflows/security-patch-plan.yml`: `plan` job (`actions: read`,
  `contents: read`, `vulnerability-alerts: read`) then `issue` job
  (`contents: read`, `issues: write`). Concurrency group `security-patch-plan`.

## Evidence

- `dash scripts/release/security-patch-plan_test.sh`: a real git repository and
  fakes for gh, go, source proof, trust synchronization and the compatibility
  check. It also runs the issue's tag block in a clone and checks every
  ceremony command's argument count against the real `stable-ceremony.sh`.
- Mutations of the script and the workflow: see the PR description.
- `go test -p 2` through `agent-heavy` for `./scripts/ci/...` and
  `./scripts/release/...`; shellcheck; the pinned actionlint.

## Not verified, and the risk

- **The workflow has not run on GitHub.** The first run after merge may queue
  #1348 if it is a pending security merge. Expect an issue then; read it.
- `vulnerability-alerts: read` on a push or schedule token was verified for a
  workflow token on 2026-11-15; the pinned actionlint does not know the scope, so
  `.github/actionlint.yaml` ignores that one message for this workflow.
- `gh issue` behavior (list limit, body files) is exercised only against a fake.
- The job timeout (45 minutes) is an estimate, not a measurement.
- The planner does not check the docs-live or fallback-channel record; the
  ceremony does.
- `recheck` does not rerun the compatibility comparison (it needs scratch
  PostgreSQL); the plan did, for the planned commit.
- **Owner decision 2026-11-16: Homebrew stays manual.** `ceremony.sh homebrew X`
  remains the last step in the issue and no tap credential is added. This
  replaces, for now, the earlier decision that Homebrew follows the latest
  release automatically (part 3 found that automation would need a cross-repo
  credential that does not exist).

## Next steps

1. Merge #1413 and part 2 (#1406) to `dev`, retarget this PR to `dev`, merge it.
2. After the promotion to `main`, dispatch the workflow once and record the run and issue.
3. Live checks that remain after promotion: the first sweep, `gh issue` writes with
   the real token, and one real `recheck` run by a maintainer.
