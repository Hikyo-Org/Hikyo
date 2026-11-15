# PR 1401: unattended stable patch releases for Dependabot security updates

Status: **implemented, inert until the owner provisions the stable-release App**.
Pull request: [#1401](https://github.com/Hikyo-Org/Hikyo/pull/1401). Decision:
the 2026-11-15 owner-locked amendment banner in the
[stable workflow signing ADR](../adr/stable-workflow-signing.md). Operating
guide: [unattended security patch releases](../release/online-signing.md#unattended-security-patch-releases).

## Result

A merged Dependabot security update releases itself as the next PATCH version:
the reconciler tags with an App-created annotated tag, `release-build` stages a
signed draft, the reconciler publishes it, syncs trust through an App-signed PR
with auto-merge and deletes the merged branch. The next tag waits for the live
docs itself. The Homebrew cask follows every latest stable release. Nothing in this PR created or changed any tag, release, ruleset,
environment, secret, variable, App or repository setting.

| Piece | Where |
| --- | --- |
| Patch-only candidate rule (exact latest stable plus one patch) | `scripts/release/stable` (`--patch-only` on `candidate` and `prepare`) |
| Tag authority decided by who pushed the tag (bot run: unattended rules; person: verified tag plus admin) | `scripts/release/require-signed-tag.sh` |
| Breaking-patch refusal | `scripts/release/check-unattended-compatibility.sh` |
| Level-triggered reconciler | `scripts/release/stable-reconcile.sh`, `.github/workflows/stable-release-reconciler.yml` |
| Build wiring and manifest digest artifact | `.github/workflows/release.yml`, `scripts/release/sign-stable-draft.sh` |
| Repository policy and rollback | `scripts/release/configure-repository.sh`, `release/repository/stable-trust-branches.json`, `scripts/release/configure-stable-rollback_test.sh` |

## Evidence

Local, on this branch: the `scripts/release/stable` Go tests, `scripts/ci` Go tests (including the workflow invariants and the baseline coverage normalizer), `scripts/release` fixtures (`require-signed-tag_test.sh`, `check-unattended-compatibility_test.sh`, `stable-reconcile_test.sh`, `configure-stable-rollback_test.sh`, `stable-workflow_test.sh`, `stable-ceremony_test.sh`), `check-nightly-release_test.sh`, `check-codeql-default-setup_test.sh`, shellcheck and actionlint. The fixtures use fake `gh` and `go` binaries and a real temporary git repository. Every fix of the second review round was proved by a mutation that fails the fixture without it. Hosted CI results are on the PR.

Not verified: any real GitHub App, environment, token mint, ruleset, tag build, publication, trust PR, auto-merge or Homebrew merge. The PR body carries the LIVE VERIFICATION checklist. The human release path now reads the run (`actions/runs/ID`) and the pusher's collaborator permission with the Actions `GITHUB_TOKEN`; that read is unverified live and fails closed.

## Live on every human release build as soon as this merges

Unrelated to the two variables: `release.yml` gains `actions: read` on `build-artifacts` and `build-signed-draft`, `require-signed-tag.sh` reads the build run and the pusher's admin permission before the existing verified-tag rule, the patch-only and compatibility steps run only when the pusher is the bot, the draft notes and step summary name the authority, and the build uploads a `release-manifest-sha256-RUN-ATTEMPT` artifact (30 days). A human release behaves as before except for those reads and that artifact.

## Remaining work

1. The owner performs the numbered OWNER SETUP in the PR description; the two repository variables are the last step.
2. Run the live checklist on the first real security patch; start with a `dry_run` dispatch.
3. Recovery gap: a failed app-tagged build cannot be retried (immutable tag, App has no `actions: write`). The reconciler opens one stall issue. If the failed build staged a draft, delete it, because an outstanding draft blocks every later candidate: `gh release delete vX.Y.Z --repo Hikyo-Org/Hikyo --yes`. The tag stays burned; release the next patch with `ceremony.sh`. To finish a good app-tagged draft by hand, export `STABLE_RELEASE_APP_BOT_ID=<bot id>` and run the ceremony `publish`. A possible mitigation not built here: refuse to tag when migrations changed since the last stable tag.
4. While the `release/sync-trust-*` ruleset is active, `ceremony.sh sync-trust` cannot push its branch; the reconciler syncs trust after human releases too. Rolling back deletes that ruleset (see Rollback).
5. Confirm on the first sync-trust PR that `require_extra_approval_for_unattributed_changes` does not demand an approval.
6. Confirm the first run of the human-path permission read (`collaborators/LOGIN/permission` with the Actions token) on the next human release build.

## Rollback

1. Delete either `STABLE_RELEASE_APP_*` variable: every job is skipped again.
2. Rerun `configure-repository.sh` without `HIKYO_STABLE_RELEASE_APP_ID`: the tag policy returns to admin-only (the App bypass is removed) and the app-only `stable trust sync branches require stable-release app` ruleset is deleted and asserted gone, so the human `ceremony.sh sync-trust` works again. Leaving that ruleset in place after step 1 would block it.
