# Separate Apple signing and notarization

Implementation: [PR #1026](https://github.com/Hikyo-Org/Hikyo/pull/1026).

## Requested behavior

Keep builds and Apple signing independently visible when Apple's notarization
service fails. Nightlies may publish everything except DMGs after a failed
notarization attempt. This includes signed macOS tarballs whose notarization is
explicitly unverified. Stable releases retain mandatory notarization.

## Artifact and credential flow

Both release workflows have `build-artifacts`, `apple-sign`, and
`apple-notarize` jobs. Signing receives the Developer ID P12 and password;
notarization receives the API key. Both use the protected `apple-signing`
environment with read-only repository permissions and Xcode 26.3.

The helper supports `--sign-only DIST` and `--notarize-only DIST`, retaining the
existing single-argument entrypoint for local full signing and notarization.
Signing produces both architectures' signed tarballs and HFS+ UDZO DMGs before
any provider request. Notarization consumes the same run's immutable signing
artifact, verifies the independent digest of its four macOS files and their
signatures, and performs submission, matching public-log validation, stapling,
and Gatekeeper checks. It does not import signing credentials or rebuild code.
The four-file digest inventory is owned once by
`scripts/release/apple-artifact-digest.sh`, shared by both workflows' producers
and consumers.

## Publication outcomes

| Outcome | Nightly publication | DMGs | macOS tarballs |
| --- | --- | --- | --- |
| Build or signing failed | Blocked | None published | None published |
| Signed-byte handoff or input signature validation failed | Blocked | None published | None published |
| Notarization succeeded | Existing release gates apply | Both accepted and stapled | Signed and notarized |
| Apple-side notarization or stapling failed after input verification | Existing release gates apply | Both omitted | Signed; notarization unverified in notes |
| Notarization helper failed and diagnostics upload also failed | Blocked | None published | None published |
| Diagnostics upload, finalized packaging, or artifact upload failed after successful notarization | Blocked | None published | None published |
| Notarization cancelled or unexpectedly skipped | Blocked for a new release | None published | None published |

The final job selects the notarization artifact only on success. A failed Apple
operation permits fallback only when the notarization helper step itself failed
after verifying both architectures' source metadata and signatures and reporting
`inputs_verified=true`, and the diagnostics upload must succeed. A failed,
cancelled, skipped, or missing diagnostics outcome blocks fallback even when the
helper also failed. A later packaging or upload failure after successful
notarization blocks publication. The final job selects the original signing
artifact, independently rechecks its signed
macOS digest, and excludes all disk images from staging. A signed-byte handoff
mismatch or signature failure blocks publication.
It retains exact-source validation, non-Darwin input hashes, authenticated
predecessors, complete manifest signing and verification, startup and populated
upgrade checks, and scoped publication credentials. Containers still depend on
successful publication. Already-published container retries preserve their
existing skip behavior.

The existing manifest and deployed reader accept zero or two DMGs. No schema or
trust-policy change is required. A successful release may coexist with a failed
notarization job and therefore an overall failed Actions run. Published releases
are immutable; no later job appends disk images to a previously signed inventory.

## CI publisher compatibility

The admission bootstrap accepts authenticated Jobs API metadata in `pending`,
`queued`, or `in_progress` state while the current setup step is executing.
It still requires the unique job name, run, attempt, event-specific source head,
positive job ID, and original start time. This handles the observed lagging
`pending` status without resetting the setup-inclusive 60-second budget or
extending its 45-second work cutoff and 15-second publication reserve.
Cold toolchain download and final policy verification can independently exhaust
that budget; the compatibility correction does not establish a timing fix or
authorize success without fresh complete proof.

The publisher also removes redundant API work: policy-only workflow authority
verification retains its final PR read after permission or approval checks,
instead of performing two consecutive identical reads at that boundary. Legacy
polling retains its own checks. Queue admission initializes repository identity
without fetching the unrelated legacy candidate workflow; legacy validation
still requires that workflow's authenticated identity. Independent final policy,
source, feedback, role and check-ownership proof remains required.

These source reductions do not change the trusted-main activation boundary or
prove hosted cold-start latency. The documented full-only recovery route in the
CI guide requires operator authorization and fresh complete current-main
candidate validation. A policy proposal or a `ci:full` label alone cannot
satisfy the active queue gate.

## Verification boundary

Regression checks execute the actual workflow publication condition, signed-byte
handoff guards, and staging commands across accepted, failed, cancelled, skipped,
missing, and substituted inputs. Shell fixtures cover phase-specific credentials,
signature and architecture checks, keychain cleanup, retained diagnostics, and
signed archive preservation. Local checks do not establish hosted Apple
acceptance or publication. Record exact-head CI, merge, and the subsequent
nightly's actual outcome separately.

Local validation passed: the helper's stable/nightly fixture matrix, shell
syntax and ShellCheck, executable workflow-condition and staging regressions,
signed-byte transfer and publication-policy tests, the runner/cache policy,
nightly release and zero/two-DMG manifest fixtures, native nightly trust-reader
DMG tests, and action-pin checks. Shared fast feedback passed Go formatting,
CI-owner compilation, ShellCheck, Actionlint, and documentation preparation.
