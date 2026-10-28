# Unattended Apple nightly release continuation

Implementation baseline: `ae2f1dda8bac294fa5bd3a20927115f5f3f00844`.
Scope: Hikyo nightly continuation, native CLI installation acceptance and
persistent public Apple evidence. Stable uses the shared phased helper, but its
signed-tag, recovery delegation, product acceptance, draft review and local
publication holds remain unchanged. No stable policy is activated.

## Frozen release contract

- Build adapter: GoReleaser v2.17.1, Linux-hosted credential-free frontend and
  CGO-disabled Go CLI builds. Six canonical archives cover Linux, Darwin and
  Windows on ARM64 and AMD64 (download spelling `x86_64`). The eight native Linux
  packages, chart, image and compatibility/provenance consumers retain their
  existing contracts.
- Apple topology: two standalone signed CLI executables, signed tarballs, and
  two HFS+ UDZO DMGs. No app bundle, package installer or universal binary.
  Each DMG is submitted once, Accepted with matching diagnostics, stapled and
  validated before Gatekeeper and mounted-payload checks.
- Nightly policy: existing automatic daily schedule and trusted default-branch
  repository dispatch. Version derives from the stable base, UTC date, original
  run number and eight-character source SHA. Recovery retains that identity.
- Trust: existing recovery-authorized `nightly/v1` policy, pinned root, workflow
  identity, numeric repository/owner IDs, provenance, rollback floors and
  predecessor compatibility. Apple publisher trust does not replace that policy.
- Exact CI: the nightly calls the complete reusable `ci.yml` graph for its own
  `github.sha`, and its build and publisher require that validation result.
  PR admission remains `ci-queue-required`; full current-base merge candidate
  validation remains separate from admission. Stable's `require-green-main.sh`
  still requires qualifying exact-SHA `ci-main.yml` or legacy `ci.yml` proof.

The live configuration captured on 2026-10-26 had no reviewers or wait timers
in either release environment. `apple-signing` allowed main and `v*` tags;
`nightly-release` allowed main. Both environments and their secret isolation are
retained without changing settings. Active rulesets require PRs, signatures,
resolved conversations and merge queue on main, immutable release tags, scoped
nightly tag creation and administrator-only stable tag creation. GitHub immutable
releases were enabled. The legacy branch-protection endpoint returned 404;
the active rulesets, not that response, establish the current protections.

## Checkpoint and continuation

1. Download the exact signed artifact ID and recheck the independently exported
   signed macOS digest. Restore only an authenticated checkpoint from an earlier
   attempt of the same run, source, channel and ref.
2. `--prepare-only` verifies all signatures and writes both v2 intents without
   Apple credentials. Persist the prepared checkpoint before any upload.
3. `--submit-only --architecture arm64` marks its upload uncertain before the
   provider call, uploads without waiting, and atomically records its UUID.
   Persist that checkpoint before submitting Intel with `--architecture x86_64`,
   then persist Intel before waiting. No retained UUID authorizes another upload.
4. `--notarize-only` queries/waits for retained UUIDs with a finite timeout,
   retrieves matching terminal diagnostics and stages ticket operations on
   private copies. Both architectures, mounted payloads, detach and private
   cleanup must pass before finalized bytes can leave the job.
5. Two credential-free native runners verify the exact finalized candidate.
   They assess the quarantined DMG, require an online notarization check on the
   standalone CLI, compare signed executable bytes, and run native version/help
   checks while retaining quarantine. No app-bundle assessment substitutes for
   this project's standalone executable topology.
6. Publication preserves existing source, digest, authenticated inventory,
   predecessor, production startup, populated-upgrade and immutable-release
   gates. Small Apple and installation evidence is embedded in the existing
   signed `NIGHTLY-BUILD.txt` asset, retained with the immutable release.

The Actions-only recovery worker runs on completed nightly failure and every
15 minutes. It verifies the checkpoint digest, original workflow/run metadata,
main ancestry and current attempt immediately before rerunning failed jobs.
Continuation comes from bound durable receipts even if interruption loses step
outputs. GitHub must explicitly record the architecture's upload step skipped in
every intervening attempt before an old prepared intent may start. Otherwise
UUID/history reconciliation is required, including a runner lost mid-upload.
Successful build and signing jobs are reused. Automatic recovery stops at 12
attempts or seven days, and does not override cancellation. Missing UUIDs require
unique history and matching filename/hash diagnostics; unavailable or ambiguous
evidence is an exception requiring investigation, never a new automatic upload.

Positive pending state, an unavailable status/log query for a retained UUID, or
the narrowly identified ticket connection loss holds publication for recovery.
The existing terminal-notarization tarball fallback remains explicit and omits
both DMGs. Failed native acceptance blocks the notarized release. A public
immutable fallback is never amended later with recovered DMGs.

## Real recovery qualification

From trusted main, a `nightly-release` repository dispatch may carry
`{"qualify_recovery":true}` in `client_payload`. Its first attempt deliberately
fails after real submission UUIDs are persisted. The automatic worker must
continue the same run with the same signed artifact and UUIDs. Acceptance,
diagnostics, staples, Gatekeeper, native installations, publication and downloaded
public hashes must all be observed independently. The injected interruption and
synthetic fixtures are not Apple proof.

Local fixture results qualify only source failure contracts. This handoff does
not predeclare hosted CI, Apple acceptance, same-candidate recovery or public
delivery successful. Those results must be recorded against actual source, run,
attempt, signed artifact, UUIDs and published asset IDs/digests. Physical clean-Mac
and Homebrew lifecycle qualification remain distinct from hosted CLI checks.

Operator reference: [Apple signing and recovery](../release/apple-signing.md).
Stable authority: [online signing](../release/online-signing.md) and
[the stable signing decision](../adr/stable-workflow-signing.md).
