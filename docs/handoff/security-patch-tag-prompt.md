# Handoff: security patch tag prompt

Part 4 of the split of the unattended security patch release work (owner
decision, 2026-11-15, option B). The human tag ceremony stays. This change tags
nothing, publishes nothing, changes `release-build` not at all and adds no App,
token or secret. Unattended tagging is deferred to a later App phase.

Stacked on part 2 (`feat/compat-check-last-stable`, #1406): the issue's
compatibility statement uses `check-compatibility-unchanged.sh`. Merge #1406
first; this PR then retargets to `dev`. The actionlint ignore for the
`vulnerability-alerts` scope was the separate prerequisite #1413; it is merged on
`dev` (ee8056912), where ci-feedback runs actionlint from the trusted base checkout.

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
  compatibility ADR, accept that everything on the commit ships; the release notes
  belong to step 2, where `publish` refuses the placeholder). `recheck` takes the kind (`patch` or `minor`) and refuses when the
  version is not that kind's successor of the latest stable release.
- **Homebrew stays manual.** `ceremony.sh homebrew X` remains the last step in
  the issue and no tap credential is added. This replaces, for now, the earlier
  decision that Homebrew follows the latest release automatically (part 3 found
  that automation would need a cross-repo credential that does not exist).

- **The release-source edge is required, for a patch and for a minor.** An
  installed release is recorded under its release identity and `PlanRoute` only
  follows an edge whose source is that exact release; the genesis edges of
  v1.0.0, v1.1.0, main and dev never match it. The plan therefore checks, with
  part 2's `check-compatibility-unchanged.sh --edges-only` and before it starts
  PostgreSQL, that the target's `release/compatibility/sources.json` has a sound
  edge from the latest stable release per engine (validated against that
  release's own verified declaration). A missing or wrong edge makes the state
  `blocked` with a prerequisite naming the edge, and `recheck` enforces the same
  check against the commit that is tagged. **Tooling gap, reported to the owner:**
  no supported command authors a stable release edge (`scripts/release/nightly
  sources` reads verified nightly directories only), so the issue says the edge
  is written by hand and reviewed. Until one is added, expect "edge missing" on
  the first post-promotion run.

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
  existing releases, the fallback-channel gate that `release.yml` asserts (the
  target's own script and record, so a stale record stops the plan rather than the
  build), the release-source edge and, last, compatibility with the previous
  stable release. A version whose tag exists, has no release and whose
  `release-build` runs all failed and that has no PUBLISHED release is skipped (the
  patch and the minor alike, each with its own note; a burned minor tag orders
  nothing, so it does not force a minor: the patch is still planned unless the
  compatibility declaration differs). Drafts never count: the plan's
  `contents: read` token cannot be relied on to list them and the maintainer's
  `recheck` can, so counting them would let the two pick different versions. The
  issue names the failed run and says to delete the draft the failed run may have
  left (re-running its failed jobs cannot create the release while it exists, and
  an outstanding stable draft stops the trust synchronization check), then to
  re-run its failed jobs: a failure before the build (validation, docs,
  floor-bench, everything `build-artifacts` needs) is almost always rerunnable, and
  only a byte set that cannot be completed safely makes the version burned.
  "Staged" means the tag's `release-build` push run succeeded, never the draft
  listing; a staged tag whose commit lacks a pending security merge says so, since
  that merge needs a further release. A tag whose
  run has not finished, or that has no run at all, is shown with the run link,
  the hint and the way out, and offers no tag command. A fixed alert that is not explained by a merged security pull request
  (the same filter as the listing: Dependabot's numeric id, base `main`,
  `dependabot/` branch) blocks the tag instead of only raising an alarm. An
  alert list with no alert at all is cross-checked over REST before it is
  believed.
  `require-synced-trust.sh` exits 1 only for "not synchronized" and 2 when it
  cannot read the releases, so a failed read fails the run instead of blocking
  the plan with a trust message. A pending merge that main reaches only through a
  side parent is held by the merge commit that brought it, so candidates qualify
  on ancestry alone.
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
  release, a patch carries no change to the migrations or compatibility files and
  its `sources.json` only gained the release-source edge from the latest
  release (every other edge unchanged, at most one edge from it per engine), the release tools the
  commands run from the commit equal the main tip's (below the tip), and the tag
  checks pass (including the fallback gate and the edge).
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
- The hourly red-tip walk reads up to 60 commits of source proof; its API budget
  per run is an estimate, not a measurement. Step 0 adds three reads per open
  security pull request.
- The maintainer's own `gh` login needs the scopes `recheck` reads (Dependabot
  alerts through GraphQL); confirm once with a real `recheck`.
- The red-tip tag block runs `git fetch hikyo-upstream main` before the signature
  check, which needs `refs/remotes/hikyo-upstream/main` (CONTRIBUTING.md sets the
  remote up); a clone without that remote stops there.
- `gh issue` behavior (list limit, body files) is exercised only against a fake.
- The job timeout (45 minutes) is an estimate, not a measurement.
- The planner checks the fallback-channel gate (it needs `node`, as on the
  hosted runner) but not the other gates that run after the tag: docs-live
  (`release.yml` runs `check-docs-live.sh` in the same step as the fallback gate,
  against the live site), the floor benchmark and the Apple jobs. A failure there
  is almost always rerunnable (re-run the failed jobs; the version is burned only
  if the byte set cannot be completed safely), but nothing here sees it coming.
- **Cost of the live checks.** `stable verify` needs the complete asset set of the
  release it verifies: it requires the closed inventory (no missing or extra
  file), every artifact's digest and signature, and, with `--published`, the
  published OCI subjects. There is no mode that verifies just the manifest, the
  declaration and their signatures, and none is added here, because that would be
  a new weaker verification path. So every `--edges-only` or full compatibility
  check downloads the whole baseline release (v1.1.0 is about 0.93 GB in 80
  assets): once per hourly run while the edge is missing, up to four times per run
  for a ready minor (the edge check and the full check, for the patch and for the
  minor), and once per local `recheck`. A narrower identity-only verify mode would
  be a separate change to the verifier; it is deferred.
- `recheck` does not rerun the compatibility comparison (it needs scratch
  PostgreSQL); the plan did, for the planned commit. For a patch it checks that no
  migration or compatibility file changed since the latest release and that
  `sources.json` only gained the edge from the latest release, and it enforces the edge itself.
- The burned-version rule trusts `release-build` run conclusions: a run that
  failed and was re-run successfully counts as succeeded (a re-run of failed jobs
  updates the same run), so it clears the skip; the plan never needs the draft
  listing for that. A tag with no run at all is not skipped (nothing says it is
  dead), so its issue shows the manual way out. The burned-minor path and the
  draft rule (plan and recheck agree because drafts never count) are evidenced
  only by the fixture, whose fake `gh` gives the recheck a draft the plan cannot
  see.
- The edge's behavior in `PlanRoute` is not exercised against real v1.1.0 files.
  By reading the code, no existing test covers a release-to-release upgrade from
  a real v1.0.0 install: v1.1.0 declares only genesis edges.
- The window between `recheck` and the tag push is not closed. The ceremony's
  own build checks apply after the tag.
- A persistent alarm for a fixed alert with no linked merge clears when a stable
  release is published after the fix, through the ordinary ceremony.

## Next steps

1. Merge part 2 (#1406) to `dev`, retarget this PR to `dev`, merge it.
2. After the promotion to `main`, dispatch the workflow once and record the run and issue.
3. Live checks that remain after promotion: the first sweep, `gh issue` writes with
   the real token, the `vulnerability-alerts` acceptance, one real `recheck`
   run by a maintainer, the first dispatch of the compatibility check (expect
   "edge missing" until a reviewed edge from the latest stable release is on
   main), the post-tag gates (docs-live, floor benchmark, Apple jobs) that
   the planner cannot see, and a decision on the missing stable edge tool.
