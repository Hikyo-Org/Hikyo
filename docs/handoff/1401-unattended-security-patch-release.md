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

| Piece | Where |
| --- | --- |
| Patch-only candidate rule (exact latest stable plus one patch) | `scripts/release/stable` (`--patch-only` on `candidate` and `prepare`) |
| Tag authority decided by who pushed the tag (bot run: unattended rules; person: verified tag plus admin) | `scripts/release/require-signed-tag.sh` |
| Breaking-patch refusal: one shared comparison of the candidate's whole compatibility declaration with the previous stable release's, run at plan time before tagging and again at build time | `scripts/release/check-unattended-compatibility.sh`, `compat_checks` in `stable-reconcile.sh`, `release.yml` |
| Level-triggered reconciler | `scripts/release/stable-reconcile.sh`, `.github/workflows/stable-release-reconciler.yml` |
| Build wiring and manifest digest artifact | `.github/workflows/release.yml`, `scripts/release/sign-stable-draft.sh` |
| Repository policy and rollback | `scripts/release/configure-repository.sh`, `release/repository/stable-trust-branches.json`, `scripts/release/configure-stable-rollback_test.sh` |

## Evidence

Local, on the final head: every `scripts/release/*_test.sh` under dash (including `stable-workflow_test.sh`, which now runs and passes), the `scripts/release/stable` and `scripts/ci` Go tests (`go test -p 2`, through `agent-heavy`), shellcheck on the touched scripts and actionlint on `release.yml` and `stable-release-reconciler.yml`. The pre-existing environment failures are `configure-dev_test.sh` (bash only) and `snapshot-manifest_test.sh` (needs a GoReleaser snapshot). The fixtures use fake `gh` and `go` binaries and a real temporary git repository. Hosted CI results are on the PR.

### What was mutation-proved (round 3)

Each row below was mutated by hand-written swaps of the production line (the fixture or Go test must fail with the mutation and pass without). The raw log is kept by the author, not committed. **Mutated and caught:** the compatibility gate at plan and build time (skip, ignored refusal, lookup failure treated as refusal, wrong trust, no normalization, partial comparison, prerelease baseline, baseline not below the candidate, no signature verification, no manifest binding, download failure exit code); the green-main read failure; the failed synced-trust listing, merge-base failure, release lookup failure and same-tag guard; the floor stop and 60-commit cap; the last-PR-commit rule; the Dependabot author, committer and verified checks; trust-lag escalation and its threshold; the five `release.yml` workflow invariants; the plan job token scope; the re-arm clock and patience; the empty-file and empty-encoding checks; the PR gate rules for the App (single admission path, queue group path, bot-id binding, branch pattern, fork head, file whitelist, deleted files, empty and oversized lists, malformed id); the synced-trust semver ordering, prerelease and highest-release handling, outstanding drafts, pagination and exit codes; the draft counted as published; the re-run split; the cleanup semver check; the App-bypass marker gate, tag order, pagination, ambiguity, admin plus App trust bypass and the verifier's creation target and role assertions; and the actor and verification-reason rules of `require-signed-tag.sh`. Five reconciler survivors and one half-marker survivor were fixed by new tests and re-mutated to CAUGHT.

**Mutated, survived, and why that is accepted:** `publish` re-checking `release_run` on the bound run is defense in depth (`require-signed-tag.sh` already refuses a non-`release.yml` path of the same run id, so one fake cannot isolate it); S-I2 "prerelease tags count" and "missing tag passes" are equivalent mutants (the anchored key capture and the earlier lookup already cover them); the S-I1 rename allowance and the S-I4 enforcement duplicate were redundant conditions, so the code was removed instead.

**Not mutation-proved:** all documentation edits (ADR custody paragraph, online-signing guide, `branching.md`, `ci.md`, this handoff); the ST-C1 repair of the `stable-workflow_test.sh` fixture (its failure was observed on the previous head: `release: no release-build run of this tag`, then fixed); the actionlint and shellcheck cleanups.

The earlier claim that every second-round fix was mutation-proved was untrue. Round 2 recorded mutations only for the S-I4 and run-path rows; the other rows had fixtures but no recorded mutation.

## LIVE VERIFICATION still open

Nothing below was run. Each item changes behavior if the assumption is wrong, so verify before activation, or on the first run:

1. The value of `needs.*.result` for a job that timed out (the reconciler and the publish gate treat anything other than `success` as not done).
2. Whether a contents-read App token can see draft releases (the planner relies on it; a miss reads as no draft and fails closed to a stall).
3. `gh pr merge --auto` against the merge queue for the Dependabot and the trust-sync PR.
4. Whether the tap-only token can read this repository's releases (the homebrew job).
5. The App permission input names (`permission-actions`, `-administration`, `-checks`, `-contents`, `-pull-requests`, `-vulnerability-alerts`, `-metadata`) are accepted by `actions/create-github-app-token`.
6. The `createCommitOnBranch` author format against the DCO gate (the trailer must equal the bot author).
7. `require_extra_approval_for_unattributed_changes` at 0 required approvals for the trust-sync PR.

## Owner order: ruleset before activation (S-I2)

`configure-repository.sh` with `HIKYO_STABLE_RELEASE_APP_ID` refuses to add the App to the stable tag creation policy until the highest stable tag's commit contains the actor-based `require-signed-tag.sh` (the `triggering_actor` and `release_run_jq` markers). Before that release, the tag authority was the old signature rule, and an App bypass would let a stolen key push a verified tag as the App. So the order is: merge this PR, release one human stable (any version) from a commit that has it, then run the script with the App id, then set the two variables. Until a stable tag carries it, the script stops and says why.

## Remaining work

1. The owner performs the numbered OWNER SETUP in the PR description; the two repository variables are the last step.
2. Run the live checklist on the first real security patch; start with a `dry_run` dispatch.
3. Recovery gap: a failed app-tagged build cannot be retried (immutable tag, App has no `actions: write`). The reconciler opens one stall issue. If the failed build staged a draft, delete it, because an outstanding draft blocks every later candidate: `gh release delete vX.Y.Z --repo Hikyo-Org/Hikyo --yes`. The tag stays burned; release the next patch with `ceremony.sh`. To finish a good app-tagged draft by hand, export `STABLE_RELEASE_APP_BOT_ID=<bot id>` and run the ceremony `publish`.
4. While the `release/sync-trust-*` ruleset is active it admits only the App and repository admins; `ceremony.sh sync-trust` run by an admin still works, a non-admin push does not. Rolling back deletes the ruleset (see Rollback).
5. A next candidate waits until trust records every published release, prereleases included, and no stable draft is outstanding. A human prerelease therefore blocks the unattended path until a `ceremony.sh sync-trust` merges.
6. The compatibility gate refuses any patch whose whole declaration differs from the previous stable release's, so a patch that adds a migration stays with the ceremony.

## Live on every human release build as soon as this merges

`release.yml` gains `actions: read` on `build-artifacts` and `build-signed-draft`; `require-signed-tag.sh` reads the build run (actor, triggering actor, types) before the existing verified-tag rule, with no collaborator permission read; the patch-only and compatibility steps run for any authority other than `human`; the draft notes and step summary name the authority; and the build uploads a `release-manifest-sha256-RUN-ATTEMPT` artifact (30 days). A human release behaves as before except for those reads and that artifact.

## Rollback

1. Delete either `STABLE_RELEASE_APP_*` variable: every job is skipped again.
2. Rerun `configure-repository.sh` without `HIKYO_STABLE_RELEASE_APP_ID`: the tag policy returns to admin-only (the App bypass is removed) and the `stable trust sync branches require stable-release app` ruleset is deleted and asserted gone, so a non-admin human `ceremony.sh sync-trust` works again. Leaving that ruleset in place after step 1 would block it.
