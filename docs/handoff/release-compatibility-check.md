# Handoff: pre-tag compatibility check against the last stable release

Part 2 of the split of the unattended security patch release work (owner
decision, 2026-11-15, option B). The human tag ceremony stays. This change
tags nothing, publishes nothing, changes `release-build` not at all and adds no
App, token or secret.

## Result

- `scripts/release/check-compatibility-unchanged.sh REPOSITORY TRUST_DIR VERSION DECLARATION`
  compares a candidate's generated `upgrade-compatibility.json` with the one the
  highest stable release below `VERSION` published. Both are normalized with
  `jq -S` minus `version`, `sequence` and `commit`, and must be byte-identical.
  Exit 0 identical, 1 different or baseline missing or unverifiable, 2 bad
  usage, 4 a lookup failed (nothing decided).
- The baseline is the highest plain `X.Y.Z` in the authenticated trust metadata
  below `VERSION`. No prerelease and not GitHub's "latest" flag. It is
  downloaded, verified with `stable verify --published` (with `--latest` when it
  is trust's highest release), and its declaration must be bound to the signed
  manifest.
- `release-compatibility-check` (workflow_dispatch from main, read-only token)
  runs the real generator on scratch PostgreSQL at main's tip and then the
  script. The operator runs it before tagging:
  `gh workflow run release-compatibility-check.yml --ref main -f version=X.Y.Z`.
  Green means identical. Red means a difference, an unverifiable earlier release
  or a failed lookup; the step summary says which.
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

## Not verified, and the risk

- **The workflow has not run on GitHub.** A workflow that exists only on a
  branch cannot be dispatched. After merge, run it once for the next patch
  version and read the step summary. Expected today: a patch that touches no
  migration reports "unchanged"; anything else reports a difference.
- The 30-minute job timeout is an estimate, not a measurement.
- The check compares main's tip. If main moves before tagging, run it again.
- It does not check CI status, trust synchronization or that the version is the
  patch successor; the release ceremony and build still decide those.

## Next steps

1. After merge, dispatch the workflow once and record the run.
2. Part 3 (Homebrew follows the latest release) and part 4 (the reconciler that
   prepares the tag command) build on this script.
