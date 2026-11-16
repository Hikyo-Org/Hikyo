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

## Owner decisions recorded here (2026-11-16)

- **No auto-merge.** The workflow token cannot add a pull request to the merge
  queue, so a person queues each security update. The separate auto-merge
  proposal (#1404) is closed. Step 0 of the issue lists every open qualifying
  Dependabot security pull request into main (alert-linked, opened by Dependabot
  by numeric id, verified Dependabot commits committed by GitHub, not a draft,
  admission check `ci-queue-required` of the Actions app not failed on its newest
  run) with its head SHA, and tells the maintainer to click **Merge when ready**.
  Qualifying pull requests the merge queue removed after their newest commit are
  listed separately and need a person. The plan job needs only read permissions
  (`pull-requests: read`, `checks: read`); no write scope was added.
- **A minor when compatibility blocks a patch.** Once a promotion brings
  unreleased migrations, the compatibility check refuses every patch, so the
  issue proposes `X.(Y+1).0` with a checklist of prerequisites that block
  tagging (read the declaration difference, review the edge modes against the
  compatibility ADR, accept that everything on the commit ships, write the release
  notes). `recheck` takes the kind (`patch` or `minor`) and refuses when the
  version is not that kind's successor of the latest stable release.
- **Homebrew stays manual.** `ceremony.sh homebrew X` remains the last step in
  the issue and no tap credential is added. This replaces, for now, the earlier
  decision that Homebrew follows the latest release automatically (part 3 found
  that automation would need a cross-repo credential that does not exist).

## Result

- `scripts/release/security-patch-plan.sh plan` decides, read-only, whether a
  merged Dependabot security update is missing from the latest published stable
  release, and which commit would carry the release: the newest of the last 60
  first-parent commits of main that holds every pending security merge, records
  the latest stable release in trust, and has complete green source proof. A red
  tip falls back to the last green commit and never goes below the newest
  pending merge; there the target's tree must also equal the tip's for every
  release tool path the commands run. It then checks trust synchronization,
  authentication, the candidate, main history, source proof, stray tags,
  existing releases and, last, compatibility with the previous stable release.
  A published release that trust does not record keeps the issue open (trust
  commands first) even when nothing else is pending.
- `issue FILE` applies the plan to the single issue (marker, title and the
  workflow's own bot, bound by its numeric user id and type through the REST
  listing, because anyone can open an issue with the marker and its author could
  later edit commands people copy), edits only on a change, closes it when
  nothing is waiting. `issue --failed RUN_URL` is what the issue job runs when the
  plan failed, was cancelled or timed out, so the issue says that nothing was
  decided. A workflow GitHub rejects at parse time runs no job, so that case is
  repaired only by the next successful run.
- `recheck KIND VERSION COMMIT [--tip]` is the first link of the issue's command.
  It refuses unless the checkout is clean and at the planned commit and every
  pending security merge is in it (no grace period), no fixed alert lacks a
  linked merge, the version is the kind's successor, trust records the latest
  release, a patch carries no change to the migrations or compatibility sources,
  and the tag checks pass.
- With a green tip the command is `recheck ... --tip && ceremony.sh build`.
  With a red tip `build` cannot be used (it tags the tip), so the issue gives
  an explicit signed tag of the green commit, a signature check of the new
  history, and its push. The review, publish, sync-trust and Homebrew commands
  follow in both cases, each from a fresh `origin/main`; Homebrew is last.
- Fail closed: unparseable or impossible timestamps, unreadable GitHub answers
  (a read failure of the source proof has its own exit code), unplaceable
  security merges (they block any tag) and FIXED alerts whose pull request is
  missing, unmerged or without a fix time stop the run or reach the issue as
  "needs a person"; nothing is silently skipped. Only the newest check run is
  judged.
- `.github/workflows/security-patch-plan.yml`: `plan` job (`actions`, `checks`,
  `contents`, `pull-requests`, `vulnerability-alerts`, all `read`) then `issue`
  job (`contents: read`, `issues: write`, `always()` on main). Concurrency group
  `security-patch-plan`.
- Sibling fix: the fallback-channel alert in `reliability.yml` matched the issue
  author by login, which `gh` spells `app/github-actions` and REST spells
  `github-actions[bot]`. It now binds the bot by numeric id and type through the
  REST listing and stops when the listing fails; a test runs both steps against a
  fake `gh`.

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
  #1348 if it is a pending security merge (a person queues it, step 0). Expect
  an issue then; read it.
- `vulnerability-alerts: read` on a push or schedule token: the 2026-11-15 check
  covered a workflow token; whether GitHub accepts the scope on this workflow is
  confirmed only by its first run on main (run link recorded after promotion).
  Until #1413 is on `dev`, the pinned actionlint rejects the scope.
- The hourly red-tip walk reads up to 60 commits of source proof; its API budget
  per run is an estimate, not a measurement. Step 0 adds three reads per open
  security pull request.
- The maintainer's own `gh` login needs the scopes `recheck` reads (Dependabot
  alerts through GraphQL); confirm once with a real `recheck`.
- The repository's `pre-push` hook needs `refs/remotes/hikyo-upstream/main`;
  `git fetch hikyo-upstream main` before the red-tip push if it refuses.
- `gh issue` behavior (list limit, body files) is exercised only against a fake.
- The job timeout (45 minutes) is an estimate, not a measurement.
- The planner does not check the docs-live or fallback-channel record; the
  ceremony does.
- `recheck` does not rerun the compatibility comparison (it needs scratch
  PostgreSQL); the plan did, for the planned commit. For a patch it checks only
  that no migration or compatibility source changed since the latest release.
- The window between `recheck` and the tag push is not closed. The ceremony's
  own build checks apply after the tag.
- A persistent alarm for a fixed alert with no linked merge clears when a stable
  release is published after the fix, through the ordinary ceremony.

## Next steps

1. Merge #1413 and part 2 (#1406) to `dev`, retarget this PR to `dev`, merge it.
2. After the promotion to `main`, dispatch the workflow once and record the run and issue.
3. Live checks that remain after promotion: the first sweep, `gh issue` writes with
   the real token, the `vulnerability-alerts` acceptance, and one real `recheck`
   run by a maintainer.
