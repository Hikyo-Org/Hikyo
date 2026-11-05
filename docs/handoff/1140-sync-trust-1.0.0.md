# Handoff: PR 1140, sync trust files for the published v1.0.0

## What the PR does

It imports the trust files published with the immutable v1.0.0 release into
`release/trust`: `metadata.json` and `metadata.sigstore.json` at sequence 2,
`catalog.json` and `catalog.sigstore.json` at sequence 4, plus the recovery-signed
`stable-policy.json`, `stable-policy.sigstore.json` and `stable-trusted-root.json`
delegation. Metadata and catalog are signed by the stable release workflow
(Sigstore keyless, `event.signed_by = github-actions-stable`), not by the recovery
key. The four signed files are byte-identical to the release and must never be
edited. The Go verifier (`internal/releasetrust/snapshot.go`, `VerifySnapshot`)
already accepted this shape. Two shell consumers did not (see blockers).

## Why main and not dev

The first-use bootstrap (`install/upgrade-nightly.sh`) fetches `release/trust` from
`refs/heads/main` raw URLs, and the nightly workflow reads it from its checkout of
`main`. Same-repository `release/` and `hotfix/` branches may target `main`
(`docs/development/branching.md`).

This PR changes no path in `policyPaths` (`scripts/ci/source-proof/resolve.go`), so
it does not block `nightly-promote`. A classifier edit was tried and dropped: it
changed no hosted plan, and it would have failed every promotion until a backmerge.
Backmerge `main` into `dev` before the next promotion, as the branching guide
requires for commits that reach `main` first.

## Merge blockers found and fixed

1. `scripts/release/assemble-nightly.sh` copied only four snapshot files, so
   `internal/upgradeassembly/assembly.go` skipped the delegation and assembly failed
   with `stable policy: invalid key-based Cosign bundle`. This broke the nightly
   assembly step and the nightly branch of `scripts/security/verify-release.sh`.
   Fix: copy `stable-policy.json`, `stable-policy.sigstore.json` and
   `stable-trusted-root.json` when the trust directory has a policy.
2. `install/upgrade-nightly.sh` (`recovery_authorization`) accepted only
   recovery-signed envelopes and failed with `invalid recovery signature envelope`.
   Fix: dual mode mirroring `VerifySnapshot`. Legacy recovery-signed snapshots work
   as before. Workflow-signed ones need a recovery-signed stable policy, strict
   policy and delegated-metadata checks, `cosign verify-blob` for metadata and
   catalog with identity derived from the authenticated policy, and the
   certificate-claim check. The embedded rollback minimum is now metadata 2 and
   catalog 4 (the imported state).

## Other changes

- `scripts/release/ceremony.sh`: the `--offline` candidate step drops
  `source_commit` and `release_tag`, which `snapshot.go` refuses on recovery-signed
  metadata.
- `scripts/release/nightly/main_test.go`: the "directory" subcases now assert the
  read error from `loadTrust`.
- Docs: `release/trust/README.md`, `release/trust/BOOTSTRAP.md`,
  `docs/operations/upgrade-artifacts.md` and `docs/operations/signed-nightlies.md`
  (new section on re-authorizing trust over workflow-signed metadata).

## Evidence pointers

- `sh install/upgrade-nightly_test.sh`: legacy path, workflow path against the
  committed `release/trust` (exact `cosign verify-blob` argv with a recording
  stand-in, real cosign when on PATH), per-check negative tests for the policy and
  delegated metadata, and replay refusal on empty state. CI installs cosign v3.1.3
  in the same `supply-chain-checks` job (`.github/workflows/ci.yml`), so the
  real-cosign test runs there.
- `scripts/ci/check-nightly-release_test.sh`: the assembly snapshot must contain the
  three delegation files (fake `go` records the snapshot).
- `internal/releasetrust/stable_keyless_test.go`,
  `TestRecoveryReauthorizationOverWorkflowSignedTrust`: which re-authorization
  shapes `VerifySnapshot` accepts or refuses.
- `scripts/release/ceremony_test.sh`: runs the script's own candidate jq filter on
  the committed metadata.
- `scripts/release/assemble-nightly.sh` against the real latest nightly: failed with
  `stable policy: invalid key-based Cosign bundle` before the fix and published a
  bundle after it.
- `verify.py authorize` against the committed trust with real cosign and a dead
  HTTP(S) proxy set: exit 0, floor metadata 2 and catalog 4.

## Deliberately unfixed

- S4, Python mirror strictness. The bootstrap verifier accepts some validly signed
  but malformed shapes that Go refuses, and its floor ignores
  `highest_release_sequence`. Exploiting this needs a valid workflow or recovery
  signature. Next step: port the remaining `ValidateMetadata` checks and the
  highest-release floor, with tests.
- S5, offline candidates. `scripts/release/ceremony.sh` `phase_candidate` never
  re-signs the catalog, so an offline candidate still fails verification, as it
  does on `main`. Next step: have the candidate phase write and recovery-sign a
  catalog binding the candidate metadata.
- S1 follow-up, `--latest` floor. After a recovery re-authorization raises main
  past the sequences embedded in the latest stable release's assets (v1.0.0
  carries 2 and 4), `install.sh` runs `stable verify --latest` and refuses new
  stable installs with `metadata: trust rollback refused` until the next stable
  release. Historical verification keeps passing. Next step: a code decision on
  whether `--latest` should compare against the release's embedded trust or
  current main, or whether every re-authorization must ship with a stable release.
  Documented in `docs/operations/signed-nightlies.md`.
- ADR open decisions (`docs/adr/stable-workflow-signing.md` does not settle them):
  the ADR says recovery authorizes a policy once, while the re-authorization section
  re-signs it, so the ADR needs an amendment; the ADR says the catalog binds the
  policy digest and the re-authorization steps follow that, but `VerifySnapshot`
  does not require the field on recovery-signed catalogs; which
  `event.type` a catalog-only recovery re-authorization uses (the verifier accepts
  any valid type); no scripted ceremony re-signs `stable-policy.json`
  (`scripts/release/stable` refuses an existing policy); the bootstrap pins the
  stable policy's identity fields, so a policy that changes them breaks the
  bootstrap until it is updated.
- `.github/workflows/docs.yml` has no `install/upgrade-nightly.sh` entry in its
  `paths` filter, so a bootstrap-only change does not redeploy hikyo.app. Next step:
  add the path (a `.github/` edit, not made here).
- The bootstrap script is served by the docs deploy. After this merges, hikyo.app
  serves the old script until the docs deploy runs. In that window it fails closed
  with `invalid recovery signature envelope`; no unauthenticated code runs.
