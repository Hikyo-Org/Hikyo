# Handoff: CLI update channels and in-place binary updates

Implementation base: `22046b898b15420dedd2236890b1294e77688061`.

## Contract

- GoReleaser stamps stable artifacts with `stable`, nightly artifacts with
  `nightly`, and every unstamped source build with `off`.
- A persisted `hikyo update channel stable|nightly|off` choice overrides a
  published artifact default. Source builds remain off and cannot enter an
  apply path without embedded trust. Legacy update snapshots migrate to the
  current artifact default and refresh once into the asset-aware state schema.
- Every interactive command checks the existing 24-hour release snapshot before
  dispatch, including server modes, failures, and `hikyo run`; services without
  a terminal remain non-blocking. `hikyo update check` refreshes immediately.
- When a newer selected release exists, Hikyo asks on the controlling terminal.
  Declining changes nothing. Accepting downloads the exact GoReleaser archive
  and `checksums.txt`, requires GitHub's immutable SHA-256 digest for both, and
  verifies the archive checksum. Stable additionally requires GitHub immutable
  release state and Hikyo's recovery-root-pinned signed manifest, trust metadata,
  candidate, and selected-archive verification.
- Replacement is serialized across processes. Unix atomically renames a synced,
  unique same-directory file over the resolved executable. Windows recoverably
  moves the mapped old image aside, publishes the replacement at the original
  path before reporting success, and retries backup cleanup on later starts.
  The current command stops before dispatch; the next invocation uses the
  replacement. A separately running server needs its normal service restart.
  The updater never elevates privileges.

## Verification

- Focused Go packages, release fixtures, workflow lint, documentation, and the
  final full repository suite are recorded in the delivering commit evidence.
- `scripts/ci/check-nightly-release_test.sh` passed, including stable, nightly,
  and source-build channel assertions.
- A temporary nightly-stamped binary queried the live GitHub Releases API and
  selected `0.0.1-nightly.20260824.7.g377a3f39`; stdin was non-interactive and
  no installation was attempted.
- Documentation validation completed its status and Astro checks; final full
  repository validation is recorded in the delivering commit/PR evidence.

## Operational boundary

The CLI self-updater is separate from the server's privileged deployment helper.
It changes only the executable file the current CLI resolves. Compose, systemd,
Flux, database migration, encrypted backup, health, and rollback remain owned by
the existing helper lifecycle for running instances.
