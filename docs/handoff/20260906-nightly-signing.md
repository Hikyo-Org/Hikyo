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
an offline ceremony. Actual GitHub OIDC signing/publication and packaged fresh
startup are now verified as recorded below. The first target's legacy bridge
statements and advancing catalog were signed under the same local nightly
exception. A live existing-database upgrade remains a separate operator action.

The workflow uses Rekor v1 signed integrated-time and inclusion/checkpoint
evidence. Policy changes, log/root rotation and legacy bridges require recovery
authorization, normally offline; this operator's nightly custody exception is
recorded explicitly. Ordinary nightlies under the same policy need no ceremony. Private
project keys stay off Actions. Runtime installation still follows the manual
backup/drill and full-stop writer procedure. Review/merge, bootstrap activation
and deployment are separate from local validation.

## First production signing and shard-index repair

PR #687 merged as `bd4b5b3e0b16c36d4e063001c455f97fb64bfda8`; all 45 PR
checks and all 39 main CI jobs passed. The first signed nightly run
[34049690497](https://github.com/Hikyo-Org/Hikyo/actions/runs/34049690497)
built all packages and signed through GitHub OIDC, then correctly stopped before
publication when the runtime verifier rejected an invalid index comparison.

Rekor v1 signs a global index in the SET but uses a shard-local index for its
Merkle proof. The actual entry has global index `2742136979` and proof index
`2620232717`, with the pinned log key and checkpoint origin. The equality check
was removed; the maintained verifier still authenticates both indexes against
the same entry body, along with the signature and checkpoint. See
[Sigstore's sharding documentation](https://docs.sigstore.dev/logging/sharding/).

The test fixture now signs unequal global/local indexes. It reproduced the
production refusal before the fix, then passed, including independent tampering
of either index. Relevant trust, bundle, self-update, nightly and assembler
packages passed, as did the focused race test. Review returned CLEAN. A local
probe recovered the actual public Rekor entry and verified its exact OIDC
identity/commit, SCT, SET, checkpoint, inclusion proof and signature over manifest
SHA-256 `830c059cf9275ec8d14cf344ec0d5a161e21709aab861133f51abc1262060cbb`.
PR #689 subsequently merged as `52c8b012f1fa45634572d7266c6ab7d3a9d5eed8`,
with 30 passing PR checks and all 39 main CI jobs passing. Retry
[34053156703](https://github.com/Hikyo-Org/Hikyo/actions/runs/34053156703)
published immutable `v0.0.1-nightly.20260906.26.g52c8b012`. Actual OIDC verification
and packaged fresh production startup/restart passed on both engines. All 22
published assets were downloaded and independently verified on the operator's
Mac. Manifest: `e54e0bdb3b9298e234070d27ad75680ed0a6abf75c59aff3815a8c1c30e9d0ee`.

## Existing-database bridge activation

Two exact legacy44-to-nightly26/migration49 statements and catalog sequence 2
were prepared, reviewed CLEAN and signed using the approved encrypted local
nightly recovery key. All 43 source migration-file hashes per engine match the
operator's recovered September 5 commit. The actual signed runtime bundle loads,
both engine routes require a maintenance bridge and operator attestation, and
wrong source schemas, absent bridges and catalog rollback refuse. See
[the public bridge record](../../release/trust/BRIDGES.md).

Local public artifacts are retained beneath
`~/Library/Application Support/hikyo/nightly-bootstrap/`: the complete release
in `verified-nightlies/v0.0.1-nightly.20260906.26.g52c8b012`, assembled public
runtime bundle in `runtime-bundle-20260906.26`, verification output in
`legacy-bridge-verification.txt` and verified catalog floor in
`bridge-verification-floor.json`. `verify-legacy-bridges.go` retains the probe;
run a temporary copy inside this Go module to permit its internal imports.
No live installation was modified. Its first upgrade still needs actual
database inspection, its own backup/drill proof and a full legacy writer stop.
