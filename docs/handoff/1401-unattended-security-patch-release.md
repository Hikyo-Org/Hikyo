# PR 1401: unattended stable patch releases for Dependabot security updates

Status: **implemented, inert until the owner provisions the stable-release App**.
Pull request: [#1401](https://github.com/Hikyo-Org/Hikyo/pull/1401). Decision:
the 2026-11-15 owner-locked amendment banner in the
[stable workflow signing ADR](../adr/stable-workflow-signing.md). Operating
guide: [unattended security patch releases](../release/online-signing.md#unattended-security-patch-releases).

## Result

A merged Dependabot security update, once trust records every published release, releases itself as the next PATCH version:
the reconciler tags with an App-created annotated tag, `release-build` stages a
signed draft, the reconciler publishes it, syncs trust through an App-signed PR
with auto-merge and deletes the merged branch. The next tag waits for the live
docs itself. The Homebrew cask follows every latest stable release. Nothing in this PR created or changed any tag, release, ruleset,
environment, secret, variable, App or repository setting.

### Owner-accepted risk (2026-11-15): no admin checkpoint on an unattended patch

The owner accepted and kept "patch from the latest green `main`" exactly as designed; the tag range is not restricted. A human release has an administrator push the tag and a maintainer review the draft. An unattended patch has neither, and it carries everything merged to `main` since the previous stable release, not only the Dependabot update. Main's required CI and the merge queue are the only controls on what ships. Main's ruleset requires 0 approvals, and same-repo `hotfix/` and `release/` branches are admitted. The compatibility gate, the patch-only rule and the security-merge floor narrow the schema and version a patch may change, not the code it contains. Recorded in the [ADR](../adr/stable-workflow-signing.md). The ADR also states plainly that a stolen App key can ship code (it can tag a commit on a branch it pushed and publish through the API); earlier text said it could not.

| Piece | Where |
| --- | --- |
| Patch-only candidate rule (exact latest stable plus one patch) | `scripts/release/stable` (`--patch-only` on `candidate` and `prepare`) |
| Tag authority decided by who pushed the tag (bot run: unattended rules; person: verified tag plus admin) | `scripts/release/require-signed-tag.sh` |
| Breaking-patch refusal: one shared comparison of the candidate's whole compatibility declaration with the previous stable release's, run at plan time before tagging and again at build time | `scripts/release/check-unattended-compatibility.sh`, `compat_checks` in `stable-reconcile.sh`, `release.yml` |
| Level-triggered reconciler | `scripts/release/stable-reconcile.sh`, `.github/workflows/stable-release-reconciler.yml` |
| Build wiring and manifest digest artifact | `.github/workflows/release.yml`, `scripts/release/sign-stable-draft.sh` |
| PR gate refuses the App everywhere except its trust-sync pull request, and any App commit in another pull request; the bot id is wired into every job that runs `pr-gate` | `scripts/ci/pr-gate/stable_release_app.go`, `ci-queue-gate.yml`, `ci-queue-worker.yml`, `ci-dev-gate.yml`, `ci-check-retirement.yml`, `scripts/ci/stable_release_app_wiring_test.go` |
| Publish rechecks the tag authority marker and the security-merge floor | `authority_at`, `floor_check`, `publish` in `stable-reconcile.sh`, `commit_has_actor_tag_authority` in `scripts/lib/release.sh` |
| Repository policy and rollback | `scripts/release/configure-repository.sh`, `release/repository/stable-trust-branches.json`, `scripts/release/configure-stable-rollback_test.sh` |

## Evidence

Local, on the final head: every `scripts/release/*_test.sh` under dash (including `stable-workflow_test.sh`, which now runs and passes), the `scripts/release/stable` and `scripts/ci` Go tests (`go test -p 2`, through `agent-heavy`), shellcheck on the touched scripts and actionlint on `release.yml` and `stable-release-reconciler.yml`. The pre-existing environment failures are `configure-dev_test.sh` (bash only) and `snapshot-manifest_test.sh` (needs a GoReleaser snapshot). The fixtures use fake `gh` and `go` binaries and a real temporary git repository. Hosted CI results are on the PR.

### What was mutation-proved (round 3)

Each row below was mutated by hand-written swaps of the production line (the fixture or Go test must fail with the mutation and pass without). The raw log is kept by the author, not committed. **Mutated and caught:** the compatibility gate at plan and build time (skip, ignored refusal, lookup failure treated as refusal, wrong trust, no normalization, partial comparison, prerelease baseline, baseline not below the candidate, no signature verification, no manifest binding, download failure exit code); the green-main read failure; the failed synced-trust listing, merge-base failure, release lookup failure and same-tag guard; the floor stop and 60-commit cap; the last-PR-commit rule; the Dependabot author, committer and verified checks; trust-lag escalation and its threshold; the five `release.yml` workflow invariants; the plan job token scope; the re-arm clock and patience; the empty-file and empty-encoding checks; the PR gate rules for the App (single admission path, queue group path, bot-id binding, branch pattern, fork head, file whitelist, deleted files, empty and oversized lists, malformed id); the synced-trust semver ordering, prerelease and highest-release handling, outstanding drafts, pagination and exit codes; the draft counted as published; the re-run split; the cleanup semver check; the App-bypass marker gate, tag order, pagination, ambiguity, admin plus App trust bypass and the verifier's creation target and role assertions; and the actor and verification-reason rules of `require-signed-tag.sh`. Five reconciler survivors and one half-marker survivor were fixed by new tests and re-mutated to CAUGHT.

**Mutated, survived, and why that is accepted:** `publish` re-checking `release_run` on the bound run is defense in depth (`require-signed-tag.sh` already refuses a non-`release.yml` path of the same run id, so one fake cannot isolate it); S-I2 "prerelease tags count" and "missing tag passes" are equivalent mutants (the anchored key capture and the earlier lookup already cover them); the S-I1 rename allowance and the S-I4 enforcement duplicate were redundant conditions, so the code was removed instead.

**Not mutation-proved:** all documentation edits (ADR custody paragraph, online-signing guide, `branching.md`, `ci.md`, this handoff); the ST-C1 repair of the `stable-workflow_test.sh` fixture (its failure was observed on the previous head: `release: no release-build run of this tag`, then fixed); the actionlint and shellcheck cleanups.

The earlier claim that every second-round fix was mutation-proved was untrue. Round 2 recorded mutations only for the S-I4 and run-path rows; the other rows had fixtures but no recorded mutation.

### What was mutation-proved (round 4)

51 production-line swaps were applied one at a time and the matching fixture or Go test had to fail. **All 51 were caught** after the survivors below were fixed. **Caught:** the explicit `states:` of the alert query; the numeric-id binding of the alert author, the PR commit author and the PR commit committer; the foreign-commit and unlinked-FIXED-alert alarms, the alarm surviving a later close and the publication-time bound; the tag-authority marker at plan, `tag_checks`, `publish`, `require-signed-tag.sh` (older commit, unreadable commit, each of the two markers alone); the security-merge floor (needs an included merge, grace window, plan, publish); the tap merge judging only the newest `ci-required` run (oldest run, unfiltered list, id instead of start time); the bot sign-off on the squash commit (message, title) and on the tap add and update commits; the PR gate refusing App commits in anyone else's PR (author id, committer id, both noreply emails, the 250-commit limit, pagination), and failing closed on a client id without a bot id; the bot id wired into each of the four gate workflows (and the third `pr-gate` step of the queue gate); the pinned Cosign step in plan, publish, sync-trust and homebrew; the issue job and issue text on `cancelled`; the publish token's pull-request and alert read scopes; the `misconfigured` job (fails, runs).

**Survivors found by the first run and fixed by new tests, then re-mutated to CAUGHT:** the tap update commit sign-off (the fixture only covered the add path; it now drives the update path with `GH_BRANCH_HEAD` at main); each single authority marker of `commit_has_actor_tag_authority` (new `authority-actor-only` and `authority-run-only` modes); the issue text on `cancelled` (the workflow test now reads the step's `FAILED` env); the committer-id clause of the PR gate (the first mutation pattern did not match the line; rerun with the exact text).

**Not mutation-proved (round 4):** the tap ruleset `require_extra_approval_for_unattributed_changes` and strict checks (live items, no code); the 30-minute plan timeout (an estimate from the 99 s measured compatibility step and the 933 MB, 80-asset v1.1.0 download, not a measured run; live item); every documentation edit (ADR, online-signing guide, `branching.md`, `ci.md`, this handoff, the PR body); the OWNER SETUP order; the live verification list; the claim that a stolen App key could publish through the API (reasoning, not tested); equivalent or redundant mutants that were not pursued.

## LIVE VERIFICATION still open

Nothing below was run. Each item changes behavior if the assumption is wrong, so verify before activation, or on the first run:

1. The value of `needs.*.result` for a job that timed out or was cancelled (the issue job now matches `failure` and `cancelled`; confirm a timeout reports one of them).
2. Whether a contents-read App token can see draft releases (the planner relies on it; a miss reads as no draft and fails closed to a stall).
3. `gh pr merge --auto` against the merge queue for the Dependabot and the trust-sync PR.
4. Whether the tap-only token can read this repository's releases (the homebrew job).
5. The App permission input names (`permission-actions`, `-administration`, `-checks`, `-contents`, `-pull-requests`, `-vulnerability-alerts`, `-metadata`) are accepted by `actions/create-github-app-token`, including on the publish token, which now also reads pull requests and alerts for the floor recheck.
6. The `createCommitOnBranch` author format against the DCO gate (the trailer must equal the bot author), and the same sign-off on the tap commit and its squash merge.
7. The tap's ruleset (id 21276517) has `require_extra_approval_for_unattributed_changes: true` and strict required checks. Verify on the first tap pull request that the App's PR with a signed-off bot commit is mergeable by the App's squash merge without a human approval, and that the strict check leaves the head at the commit the `ci-required` run covers; `require_extra_approval_for_unattributed_changes` also needs checking for the trust-sync PR at 0 required approvals.
8. The plan job's timeout is 30 minutes, an estimate and not a measurement. Known: "Generate exact compatibility" took 99 s in the v1.1.0 release-build (run 42526720406); v1.1.0 has 80 assets (933 MB). Unmeasured: the baseline download and signature verification through the OCI path. Read the plan job's duration on the first dry run and size the timeout from it.
9. A real Dependabot security update reports `committer.login == web-flow`, verified, and author id 49699333 / committer id 19864447 on every commit (read-only evidence: PR #1370; the first real security PR confirms it). If not, the reconciler raises the stall issue naming the PR.
10. The explicit `states: [OPEN, FIXED]` argument of `vulnerabilityAlerts` is accepted, and fixed alerts keep `dependabotUpdate.pullRequest` (read-only evidence: 177 alerts, 149 fixed).
11. The `misconfigured` job fails a run with only the client id set; no other job may run in that state.

## Owner order

1. Merge this PR. Merge `main` into `dev` afterwards: the PR gate reads `ci-dev-gate.yml` from the base and head of a dev pull request and requires both to equal main's, so until `main` is merged into `dev` the dev gate runs the old publisher copy without the bot id and its stable-release rule is inert for pull requests into `dev` (the `main` gates are covered).
2. Release one human stable (any version) from a commit that has the actor-based `require-signed-tag.sh` (S-I2 below).
3. Set `STABLE_RELEASE_APP_BOT_ID` (the App's numeric bot user id) **before** the App is installed or granted contents write, so the PR gate is armed from the first moment the key exists. It does nothing else while the client id is unset.
4. Install the App, run `HIKYO_STABLE_RELEASE_APP_ID=<id> scripts/release/configure-repository.sh`, and store the key and client id in the `stable-release` environment (main only; no required reviewers is expected).
5. Set `STABLE_RELEASE_APP_CLIENT_ID` last. It is the on/off switch of the reconciler. With the client id set and the bot id empty the PR gate fails closed and the reconciler's `misconfigured` job fails every run.
6. Start with a `dry_run` dispatch.

## Ruleset before activation (S-I2)

`configure-repository.sh` with `HIKYO_STABLE_RELEASE_APP_ID` refuses to add the App to the stable tag creation policy until the highest stable tag's commit contains the actor-based `require-signed-tag.sh` (the `triggering_actor` and `release_run_jq` markers). Before that release, the tag authority was the old signature rule, and an App bypass would let a stolen key push a verified tag as the App. The release path enforces the same on the tag being built: `require-signed-tag.sh` (bot branch), and the reconciler's plan, tag and publish steps refuse a tag whose own commit lacks the markers. So the order is the one above. Until a stable tag carries it, the script stops and says why.

## Remaining work

1. The owner performs the numbered OWNER SETUP in the PR description; `STABLE_RELEASE_APP_BOT_ID` comes before the App is installed and `STABLE_RELEASE_APP_CLIENT_ID` is the last step.
2. Run the live checklist on the first real security patch; start with a `dry_run` dispatch.
3. Recovery gap: a failed app-tagged build cannot be retried (immutable tag, App has no `actions: write`). The reconciler opens one stall issue. If the failed build staged a draft, delete it, because an outstanding draft blocks every later candidate: `gh release delete vX.Y.Z --repo Hikyo-Org/Hikyo --yes`. The tag stays burned; release the next patch with `ceremony.sh`. To finish a good app-tagged draft by hand, export `STABLE_RELEASE_APP_BOT_ID=<bot id>` and run the ceremony `publish`.
4. While the `release/sync-trust-*` ruleset is active it admits only the App and repository admins; `ceremony.sh sync-trust` run by an admin still works, a non-admin push does not. Rolling back deletes the ruleset (see Rollback).
5. A next candidate waits until trust records every published release, prereleases included, and no stable draft is outstanding. A human prerelease therefore blocks the unattended path until a `ceremony.sh sync-trust` merges.
6. The compatibility gate refuses any patch whose whole declaration differs from the previous stable release's, so a patch that adds a migration stays with the ceremony.

## Live on every human release build as soon as this merges

`release.yml` gains `actions: read` on `build-artifacts` and `build-signed-draft`; `require-signed-tag.sh` reads the build run (actor, triggering actor, types) before the existing verified-tag rule, with no collaborator permission read; the patch-only and compatibility steps run for any authority other than `human`; the draft notes and step summary name the authority; and the build uploads a `release-manifest-sha256-RUN-ATTEMPT` artifact (30 days). A human release behaves as before except for those reads and that artifact.

## Rollback

1. Delete `STABLE_RELEASE_APP_CLIENT_ID`: every job is skipped again. Keep `STABLE_RELEASE_APP_BOT_ID` set while the App is installed and its key is valid: deleting it disarms the PR gate for a key that still has contents write. Remove it only after the App is uninstalled and its key revoked.
2. Rerun `configure-repository.sh` without `HIKYO_STABLE_RELEASE_APP_ID`: the tag policy returns to admin-only (the App bypass is removed) and the `stable trust sync branches require stable-release app` ruleset is deleted and asserted gone, so a non-admin human `ceremony.sh sync-trust` works again. Leaving that ruleset in place after step 1 would block it.
