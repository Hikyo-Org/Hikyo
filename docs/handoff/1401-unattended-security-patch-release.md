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
with auto-merge and waits for the docs. The Homebrew cask follows every latest
stable release. Nothing in this PR created or changed any tag, release, ruleset,
environment, secret, variable, App or repository setting.

| Piece | Where |
| --- | --- |
| Patch-only candidate rule (exact latest stable plus one patch) | `scripts/release/stable` (`--patch-only` on `candidate` and `prepare`) |
| Second tag authority (run actor equals the bot id) | `scripts/release/require-signed-tag.sh` |
| Breaking-patch refusal | `scripts/release/check-unattended-compatibility.sh` |
| Level-triggered reconciler | `scripts/release/stable-reconcile.sh`, `.github/workflows/stable-release-reconciler.yml` |
| Build wiring and manifest digest artifact | `.github/workflows/release.yml`, `scripts/release/sign-stable-draft.sh` |
| Repository policy | `scripts/release/configure-repository.sh`, `release/repository/stable-trust-branches.json` |

## Evidence

Local, on this branch: the `scripts/release/stable` Go tests, `scripts/ci` Go tests (including the new workflow invariants and the baseline coverage normalizer), `scripts/release` fixtures (`require-signed-tag_test.sh`, `check-unattended-compatibility_test.sh`, `stable-reconcile_test.sh`, `stable-workflow_test.sh`, `stable-ceremony_test.sh`), `check-nightly-release_test.sh`, `check-codeql-default-setup_test.sh`, shellcheck and actionlint. The fixtures use fake `gh` and `go` binaries and a real temporary git repository. Hosted CI results are on the PR.

Not verified: any real GitHub App, environment, token mint, ruleset, tag build, publication, trust PR, auto-merge or Homebrew merge. The PR body carries the LIVE VERIFICATION checklist.

## Remaining work

1. The owner performs the numbered OWNER SETUP in the PR description; the two repository variables are the last step.
2. Run the live checklist on the first real security patch; start with a `dry_run` dispatch.
3. Recovery gap: a failed app-tagged build cannot be retried (immutable tag, App has no `actions: write`). The reconciler opens one stall issue; a maintainer releases the next patch with `ceremony.sh`. A possible mitigation not built here: refuse to tag when migrations changed since the last stable tag.
4. With the `release/sync-trust-*` ruleset active, `ceremony.sh sync-trust` cannot push its branch; the reconciler syncs trust after human releases too.
5. Confirm on the first sync-trust PR that `require_extra_approval_for_unattributed_changes` does not demand an approval.

## Rollback

Delete either `STABLE_RELEASE_APP_*` variable: every job is skipped again. Rerun `configure-repository.sh` without `HIKYO_STABLE_RELEASE_APP_ID` to return the stable policies to admin-only.
