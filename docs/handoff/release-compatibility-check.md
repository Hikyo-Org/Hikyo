# Handoff: pre-tag compatibility check against the last stable release

Part 2 of the split of the unattended security patch release work (owner
decision, 2026-11-15, option B). The human tag ceremony stays. This change
tags nothing, publishes nothing, changes `release-build` not at all and adds no
App, token or secret.

**Delivery.** This is a feature pull request into `dev`; it reaches `main` by the
normal promotion. A dispatch-only workflow can be started (and its input form
appears) only once its file is on the default branch, and this one refuses to
run from any other ref, so **it is unusable until the promotion lands it on
`main`**. The shell check and its fixture work from `dev` today.

## Result

- `scripts/release/check-compatibility-unchanged.sh REPOSITORY TRUST_DIR VERSION DECLARATION`
  compares a candidate's generated `upgrade-compatibility.json` with the one the
  highest stable release below `VERSION` published. Both are normalized with
  `jq -S` minus `version`, `sequence` and `commit`, and must be byte-identical.
  Exit 0 identical, 1 different, 5 the baseline cannot be established or
  verified (nothing decided about compatibility), 2 bad usage, 4 a lookup failed
  (nothing decided). A difference prints a bounded unified diff before its
  refusal line.
- The baseline is the highest plain `X.Y.Z` in the authenticated trust metadata
  below `VERSION`. No prerelease and not GitHub's "latest" flag. It is
  downloaded and verified with `stable verify --historical --published`, as
  `scripts/security/verify-release.sh` does (an older release embeds older trust,
  and a plain verify refuses that as a "trust rollback"; reproduced against the
  real v1.0.0). Its declaration must be bound to the signed manifest.
- `release-compatibility-check` (workflow_dispatch from main, read-only token)
  runs the real generator on scratch PostgreSQL at main's tip and then the
  script. The operator runs it before tagging, after the sync-trust change that
  records the latest release has merged (the baseline is read from trust):
  `gh workflow run release-compatibility-check.yml --ref main -f version=X.Y.Z`.
  Green means identical. Red means a difference (exit 1), an earlier release
  that cannot be established or verified (exit 5) or a failed lookup (exit 4);
  the step summary names which and quotes the script's own line. A dispatch
  from any other ref fails with exit 2 (the first step), and a version that
  `stable candidate` refuses gets a one-line summary.
- `scripts/lib/release.sh` gains `validate_compatibility_shape`;
  `validate_release_compatibility` calls it and then binds the candidate's
  version, sequence and commit. Behavior of the build path is unchanged.

## Not changed

`release.yml`, tag rules, rulesets, the ceremony and publication path. The check
is advisory for the maintainer: nothing requires it.

## Evidence

- `dash scripts/release/check-compatibility-unchanged_test.sh`: the declarations
  are real (the generator's development declaration plus the reviewed source
  edges); only `gh release download` and the signature verifier are faked.
- 28 mutations of the script, the shared library and the workflow: every one
  fails the fixture or `TestReleaseCompatibilityCheckWorkflowBoundaries`.
- `go test -p 2 ./scripts/ci/... ./scripts/release/...` through `agent-heavy`.
- shellcheck, the pinned actionlint.
- A local run of the real chain against the published `v1.1.0` assets, with the
  generator's schema step on `postgres:18` (the CI major version): the
  generator's declaration for `main`'s commit at that time, checked as `1.1.1`,
  printed `compatibility is identical to v1.1.0` (exit 0). The same run against
  Postgres 14 printed "differs", because the schema digest depends on the
  Postgres major version; CI pins 18. This is my own local run, not a workflow run.

## Not verified, and the risk

- **The workflow has not run on GitHub.** A workflow that exists only on a
  branch cannot be dispatched. After promotion to `main`, run it once for the next patch
  version and read the step summary. **Expect "differs" on the first dispatch**:
  the promotion that lands this workflow also carries the migrations that sit on
  `dev`, so main's tip no longer matches the last stable release.
  The "unchanged" verdict is evidenced separately by a local run (see Evidence).
- The 30-minute job timeout is an estimate, not a measurement.
- The check compares main's tip. If main moves before tagging, run it again.
- It does not check CI status, trust synchronization or that the version is the
  patch successor; the release ceremony and build still decide those.

## Next steps

1. After the promotion to `main`, dispatch the workflow once and record the run.
2. Part 4 (the reconciler that prepares the tag command) builds on this script.
   Homebrew stays manual (owner decision 2026-11-16): `ceremony.sh homebrew X`
   remains the last release step.
