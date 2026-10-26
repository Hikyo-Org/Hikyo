# Apple signing and notarization

Apple Developer Program enrollment makes Developer ID distribution available.
Enrollment alone does not install a signing identity or configure CI credentials.
Stable and nightly releases require Apple signing for both macOS architectures
before their final hashes and Cosign signatures are generated. Stable releases
also require notarization. Nightlies may publish without disk images after a
verified terminal Apple rejection; their macOS tarballs
remain signed, with notarization explicitly unverified in the release notes.
Local development snapshots keep their existing signing profile.

The fallback requires explicit terminal-rejection authorization from the notarization helper after input
verification and successful retention of notarization diagnostics. A failed
diagnostics upload blocks fallback even when the helper also failed.
A later diagnostics upload, finalized packaging, or artifact
upload failure after successful notarization blocks publication.

## One-time credential setup

1. Create a **Developer ID Application** certificate through Xcode or the Apple
   Developer account. Install it with its private key in Keychain Access and
   export the identity as a password-protected `.p12`. A development certificate
   or a downloaded `.cer` without its private key cannot sign these releases.
   Check locally with `security find-identity -v -p codesigning`; expect a valid
   `Developer ID Application: NAME (TEAMID)` identity. A Developer ID Installer
   certificate is for installer packages; these DMGs use Developer ID Application.
2. Create a team App Store Connect API key authorized to submit notarizations.
   Retain its `.p8` private key, key ID, and issuer ID. This pipeline uses team
   API keys, including the issuer ID, rather than individual keys or an Apple
   Account password.
3. In the Hikyo repository's Environments settings, create `apple-signing`.
   Choose custom deployment branch policies with exactly branch `main` and tag
   pattern `v*`. Both Apple jobs use this environment. The environment policy
   matches ref names; it does not restrict who creates matching tags. Keep these
   repository rulesets active as the tag-creation and immutability boundaries:
   - `release tags require admin role`: only repository admins may create `v*`
     tags, excluding `v*-nightly.*`.
   - `nightly tags require Hikyo release app`: only the Hikyo release app may
     create `v*-nightly.*` tags.
   - `release tags are immutable`: prohibit updates and deletion of every `v*`
     tag, with no bypass actors.

   Configure these secrets in the environment, then remove any repository-scoped
   copies after verifying the new environment's secret metadata:
   - `APPLE_DEVELOPER_ID_P12_BASE64`: base64 encoding of the exported identity.
   - `APPLE_DEVELOPER_ID_P12_PASSWORD`: the export password.
   - `APPLE_NOTARY_KEY_P8`: the complete private key, preserving its PEM lines.
4. Configure these non-secret repository variables:
   - `APPLE_SIGNING_IDENTITY`: the full Developer ID Application identity name.
   - `APPLE_TEAM_ID`: the identity's ten-character team ID.
   - `APPLE_NOTARY_KEY_ID`: the team API key ID.
   - `APPLE_NOTARY_ISSUER_ID`: the team API issuer UUID.

Enter secrets through GitHub's private settings or a private secret input, never
in chat, source control, workflow arguments, or report attachments. Keep an
encrypted backup of the signing identity and notarization key. Routine releases
use these online Apple credentials in addition to short-lived GitHub OIDC for
Cosign; the long-lived Apple identity is not an offline or hardware-bound key.

Apple documents [Developer ID certificate creation](https://developer.apple.com/help/account/certificates/create-developer-id-certificates/)
and the [notarization workflow](https://developer.apple.com/documentation/security/customizing-the-notarization-workflow).

## Artifact flow

Both release workflows build the SPA and all GoReleaser outputs once on Linux.
Stable builds require source validation, docs, and the floor benchmark; nightly
builds require exact-head validation and authenticated predecessor compatibility.
A dedicated `apple-sign` job on a macOS runner
receives those exact outputs from the same workflow run. It imports the identity
into a disposable Keychain, verifies the candidate hash, tag, commit, and Mach-O
architecture, and signs each macOS executable with hardened runtime and a secure
timestamp. Nightly signing instead binds the immutable build outputs and
GoReleaser metadata to the main workflow SHA and resolved nightly version.

For each architecture it creates and signs
`hikyo_VERSION_Darwin_ARCH.dmg` and retains the signed tarball. This job receives
only the Developer ID certificate and password. Its artifact ID and independent
digest of all four signed macOS files bind the handoff to `apple-notarize`.
The notarization job receives only the API key, verifies the handed-off bytes
and signatures, submits each DMG to `notarytool`, requires an
`Accepted` response, staples the ticket, validates it, verifies the Developer ID
signature against the configured team, and assesses the disk image with
Gatekeeper. The corresponding tarball contains the same signed executable.
Temporary certificate files, private API keys, and the Keychain are removed on
success and failure. Only public artifacts are transferred to the final job.

Each submission also produces a JSON receipt binding its ID and status to the
release version, source commit, architecture, disk-image filename, and SHA-256
of the submitted image before stapling changes its bytes. Completed submissions
(`Accepted`, `Invalid`, or `Rejected`) have their detailed Apple log downloaded
and checked against the receipt. An accepted submission cannot proceed without
a valid matching log.
Warnings are surfaced as counts for review; they do not by themselves change
Apple's acceptance status.

Both workflows' notarization jobs retain these JSON files as Actions artifacts named
`apple-notarization-stable-RUN_ID-RUN_ATTEMPT` or
`apple-notarization-nightly-RUN_ID-RUN_ATTEMPT`, including after a failed notarization
step. Retention is 90 days (subject to the repository limit). A rejected or
timed-out submission keeps its receipt even if Apple has no log available yet;
an earlier architecture's completed evidence survives a later failure. Inspect
the recorded submission before retrying. No receipt is expected when the
notarization job fails before any submission.

The v2 receipts also bind the repository, original run, original signed artifact
ID and digest, channel, source ref, Developer ID identity and team. The helper
accepts prior evidence only when every binding and retained submitted-image
digest matches. `--prepare-only` verifies the signed handoff without credentials
and writes both intents. CI persists them before `--submit-only` uploads without
waiting, then persists UUIDs before `--notarize-only` queries or waits for them.
Each checkpoint is an immutable Actions artifact named
`apple-notary-state-RUN_ID-ATTEMPT-STAGE`, retained for 90 days. It contains only
allowlisted receipts, validated logs, exact submitted DMGs and its closed digest
inventory. The helper uses a dedicated directory outside private credential
scratch and release payloads, with an exclusive lock and atomic receipt writes.
Credentials, raw submission errors and invalid/mismatched logs are excluded.

On nightly failures, `apple-release-recovery.yml` runs automatically and every
15 minutes. It authenticates the original main workflow/run and checkpoint,
checks main ancestry and current attempt, then reruns failed jobs of that same
run. Successful build/signing jobs are reused. It has only repository read and
Actions write permissions, no Apple secrets, publication key or OIDC authority.
Recovery is bounded to 12 attempts and seven days; cancellation is respected.
Unknown, missing or conflicting receipt identity never permits another upload.
Missing UUID reconciliation requires a unique history entry with matching
filename and submitted hash in Apple's terminal diagnostics. Empty or incomplete
history is an unresolved exception, not proof that no submission occurred.

The final Linux job consumes these finalized artifacts, rechecks candidate and
archive hashes and independently compares Linux/Windows archive bytes with the
original build digest. Nightly compatibility is also bound to its original
build digest. The stable job builds the image and chart from canonical Linux
binaries, and creates provenance and the release manifest. Accepted disk images
are covered by the authenticated release manifest as `installer` assets,
using the existing artifact type accepted by deployed nightly readers. Stable
releases also sign each asset with Cosign. Checksums retain their
existing contract of exactly six platform archives. Signing failures or missing
signing credentials stop every release before manifest signing.

Nightly publication waits for the separate notarization job to finish. Success
selects its finalized artifact and includes both notarized DMGs after native
installation checks on `macos-15` (ARM64) and `macos-15-intel`. Both gates validate
the ticket, signatures, architecture and installed executable bytes, assess the
DMG and installed CLI with Gatekeeper, and run `--version` and `--help` while
retaining quarantine. This is hosted native CLI installation evidence, not a
claim of physical clean-Mac or Homebrew lifecycle qualification. An Apple
`Invalid` or `Rejected` result permits fallback only when both submissions have
matching terminal diagnostics and source metadata, both signatures and the
signed-byte handoff have been verified. That path selects
the original signing job's immutable artifact, independently rechecks the signed
macOS digest, publishes the six signed platform
archives and eight Linux packages, and omits every DMG, including any image that
was accepted before the other architecture failed. The release notes state that
macOS notarization was not verified. Linux, Windows, and container publication
retain their existing validation, signature, startup, and upgrade checks.
A cancelled or skipped notarization job does not authorize a new publication.
Failed handoff or input signature checks also block publication.
Malformed or conflicting status evidence, ticket verification, mounted-payload
and credential-cleanup failures cannot authorize fallback. An interruption
between UUID capture and waiting retains the recovery hold, including a failed
checkpoint upload. A later final checkpoint cannot erase that unresolved state.
Stable publication continues to require successful notarization.

Pending UUIDs and unavailable status/log queries hold publication for automatic
continuation. A diagnosed ticket connection loss can resume ticket operations
using the existing Accepted UUIDs. Other trust or native-installation failures
block DMG publication. Successful receipts, matching Apple logs and both native
installation receipts are embedded in `NIGHTLY-BUILD.txt` before its manifest is
signed. That existing immutable asset preserves small evidence beyond Actions
artifact expiry without adding a new trust authority or artifact kind.

The build and signing jobs can pass while notarization fails. GitHub still marks
the overall workflow as failed when its notarization job fails, even if nightly
publication and the container job succeed. Published manifests and assets remain
immutable; a later notarization retry does not append DMGs to an existing release.

The default notarization wait is 30 minutes per submission. The packaging helper
accepts `APPLE_NOTARY_TIMEOUT` in the duration syntax supported by
`notarytool --timeout`; workflow use can set that environment variable on the
Apple notarization step. A timeout leaves a submission processing at Apple. The
original run resumes from its retained UUID, never from a new build or upload.
An existing version tag must
never be recreated to bypass a release failure.

### Recovering ticket lookup failures

An `Accepted` receipt and matching Apple log do not establish that stapling or
ticket validation succeeded. `stapler validate` compares the attached ticket
with the ticketing service. A CloudKit connection-loss diagnostic such as
`NSURLErrorDomain -1005` after acceptance calls for network investigation;
exit 68 alone does not distinguish a network failure from missing notarization.
GitHub's [runner-images investigation](https://github.com/actions/runner-images/issues/14924)
records matching connection failures and an HTTP/3 transport reproduction.
That investigation does not establish the transport cause of a Hikyo failure.

Retain the submission receipt, matching log, and original signed DMG. Check its
pre-stapling SHA-256 against the receipt before recovery. For an accepted
submission, repeating ticket operations on those verified bytes avoids another
submission or signing operation. Every signature, stapling, validation and
Gatekeeper gate must still pass before a DMG can be published.

`--notarize-only` never uploads. It requires both prepared intents and either
reuses retained UUIDs or performs strict history reconciliation. On retries CI
restores the original signed artifact, not partially stapled bytes. Both images
pass acceptance, mounting, payload comparison, detach and private cleanup before
either finalized image replaces its original. Final submitted and download
hashes remain distinct. A missing UUID after a crash remains unresolved until
matching Apple evidence identifies it. Never remove quarantine, bypass
Gatekeeper or recreate a tag to force recovery.

A protected-main `nightly-release` repository dispatch may include
`{"qualify_recovery":true}` in `client_payload` for a real recovery qualification.
Its first attempt deliberately stops after UUID checkpoint upload. Automatic
continuation then queries those same submissions and must pass every actual
Apple, ticket, installation and publication gate. The deliberate interruption
is not Apple acceptance evidence.

Stable human review and publication remain the existing
[online release ceremony](online-signing.md). Nightlies retain their main-only
publication environment, scoped app token, startup and populated-upgrade gates,
and authenticated predecessor chain. Already published runs skip Apple signing
and retain their original artifacts. Neither Apple job can write to
the repository, publish a release, or mint a Cosign OIDC identity.

## DMG and Homebrew installations

Both release workflows wrap notarization in
`scripts/release/with-apple-ticket-network.sh`. On disposable GitHub-hosted
macOS runners, this temporarily rejects outbound UDP port 443 in a dedicated
packet-filter anchor so Apple's ticket client falls back from HTTP/3 to TCP.
This addresses the accepted-notarization/stapling Error 68 reported in
[actions/runner-images#14924](https://github.com/actions/runner-images/issues/14924).
The wrapper preserves the parent ruleset, removes its anchor rules, releases
its own firewall enablement token on exit, and propagates command or cleanup
failure. It refuses local Macs and self-hosted runners. Apple acceptance,
stapler validation and Gatekeeper assessment remain mandatory for DMGs.

Both workflows retain their signed and finalized handoff artifacts for 30 days.
These contain release files, not the signing Keychain or credential scratch.
This preserves the exact signed bytes for inspection after an Apple service
failure; it does not authorize rebuilding, resubmitting an uncertain upload,
or adding files to an already-published manifest.

Hikyo is a command-line executable with an embedded web UI. Each DMG contains
`hikyo` and `LICENSE`; it is not a macOS application bundle. Download the DMG
matching the Mac's architecture from the verified public release. After opening
it, copy `hikyo` to a writable directory on the user's executable search path,
preserving its signature and execute permission, and run `hikyo --help`.

The Homebrew cask generated by `ceremony.sh homebrew VERSION` downloads these
same stapled DMGs with architecture-specific SHA-256 values from the verified
manifest and installs their `hikyo` executable. It requires both DMGs and refuses
missing, mismatched, or symlinked assets. No quarantine-removal hook is added.
Tap publication remains a separate protected PR after public release verification.

For development releases, `publish-homebrew-cask.sh --nightly` generates
`hikyo@nightly` from the same signed DMG pair. It authenticates the complete
bundle through the recovery-authorized `nightly verify` command before any tap
mutation, then downloads both public DMGs and compares their hashes with the
authenticated manifest. It requires a public nightly prerelease and keeps
`hikyo` stable-only.
See [the package-manager handoff](https://github.com/Hikyo-Org/Hikyo/blob/main/docs/handoff/package-manager-releases.md#nightly-homebrew-publication)
for the publication command. Neither publisher merges its tap PR.

Raw executables and tarballs cannot carry a stapled ticket. Their contained code
is submitted inside the DMG, but a first launch from a tarball may need access to
Apple's notarization service. The DMG carries the ticket for its distribution
container; this does not establish offline first-launch behavior for a copied
standalone executable.

## Acceptance evidence

Before calling Apple distribution operational, run a real approved release with
the configured identity and retain its accepted submission IDs, successful
`stapler validate`, Developer ID verification, and Gatekeeper disk-image result.
Test the quarantined download and tap installation on a different Mac, including
launch and upgrade, on Apple Silicon and Intel. Preserve quarantine during those
checks. A fixture test or successful build is not Apple notarization or clean-Mac
installation evidence.

The maintainer completed certificate and API-key provisioning on 2026-10-21.
The protected GitHub environment, secret relocation, and local signing have
been verified, and a harmless test
disk image has been submitted to Apple. See the [configuration handoff](../handoff/apple-signing.md)
for its processing and acceptance status. No Hikyo release or live tap was
published or changed during setup.
