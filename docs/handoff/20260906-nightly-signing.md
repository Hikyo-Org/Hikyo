# Nightly signing regression repair

## Incident and accepted repair

`0.0.1-nightly.20260906.24.g90b4ca6a` failed production startup with
`binary has no bounded production trust stamp`. The nightly workflow made trust
embedding optional and omitted the compatibility declaration required by the
new runtime gate. CLI self-update replaced the executable without its runtime
bundle or upgrade evidence. The operator restored the September 5 nightly.

The user approved automated signed nightlies with one-time offline recovery
authorization. The earlier proposed unsigned exception was superseded. Stable
release verification and existing offline private-key custody remain in force.

## Implementation

- Nightly CI requires exact OIDC/checkout SHA, green main CI and authenticated
  public bootstrap; builds both-engine compatibility and embeds mandatory trust;
  signs a closed twenty-payload inventory with pinned Cosign and GitHub OIDC;
  runs the runtime verifier before creating the tag or release.
- Offline assembly accepts complete nightly directories with `--nightly`,
  reauthenticates/copies payloads durably and checks the resulting runtime bundle.
  `assemble-nightly.sh` prepares the public snapshot/key/bridge inputs.
- Ordinary legacy-to-nightly routes refuse. The new, separately versioned
  `legacy-nightly-bridge/v1` names inspected legacy schema/migrations and an exact
  signed nightly target/policy without inventing a source release identity.
  Recovery signature, current catalog and instance backup/drill proof are all
  required. `nightly legacy-bridges` emits unsigned review proposals.
- Nightly CLI updates authenticate and durably stage the complete release. They
  preserve the executable. Trust rollback and nightly sequence/equivocation
  checks persist separately from stable release counters.
- Actual packaged-process acceptance boots both fresh engines and restarts.
  After the first signed release, CI additionally runs the previous published
  binary, populates a protected value, exports/drills a backup, signs disposable
  local operator evidence, boots the candidate and proves readability/restart.

## Verification

Passed relevant Go packages: `internal/releasetrust`, `internal/selfupdate`,
`internal/cli`, `internal/store/upgrade`, `internal/upgradecompat`,
`internal/upgradebundle`, `internal/upgradegate`,
`scripts/release/assemble-upgrade`, `scripts/release/compatibility`, and
`scripts/release/nightly`. PostgreSQL paths used a disposable PostgreSQL 18
container through `HIKYO_TEST_POSTGRES_DSN`. The new packaged upgrade test passed
both engines with real test-local Sigstore proofs and distinct release builds.

The broader run exposed an old bundle fixture that allowed unbridged legacy
nightlies. That fixture now proves refusal and then success with a recovery
bridge; its full package passed after correction.

`actionlint`, ShellCheck, `scripts/ci/check-nightly-release_test.sh`,
`git diff --check`, and the Node 24 docs production build passed. Standards and
spec reviewers returned CLEAN in R2 after durability and upgrade-admission fixes.

## Activation boundary

The user subsequently authorized an online bootstrap with encrypted local key
storage. Genuine keys and recovery signatures were generated on the operator's
Mac; public files now exist in `release/trust`. Both default `nightly preflight`
and `verify-bundle.sh --trust-only` pass. Private keys remain outside the checkout
with passphrases in macOS Keychain. See the exact provenance and fingerprints in
[the bootstrap record](../../release/trust/BOOTSTRAP.md). This was explicitly not
an offline ceremony. Actual GitHub OIDC signing/publication and a live installation
remain unverified pending review/merge and a successful nightly run. After the first signed
target exists, its exact legacy bridge statements need a second offline recovery
authorization and advancing catalog before old populated databases can upgrade.

The workflow uses Rekor v1 signed integrated-time and inclusion/checkpoint
evidence. Policy changes, log/root rotation and legacy bridges require offline
recovery authorization; ordinary nightlies under the same policy do not. Private
project keys stay off Actions. Runtime installation still follows the manual
backup/drill and full-stop writer procedure. Review/merge, bootstrap activation
and deployment are separate from local validation.
