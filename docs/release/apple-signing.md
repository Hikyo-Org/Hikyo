# Apple signing and notarization

Apple Developer Program enrollment makes Developer ID distribution available.
Enrollment alone does not install a signing identity or configure CI credentials.
Stable and nightly releases require Apple signing for both macOS architectures
before their final hashes and Cosign signatures are generated. Stable releases
also require notarization. Nightlies may publish without disk images when the
separate notarization job fails; their macOS tarballs remain signed, with
notarization explicitly unverified in the release notes. Local development
snapshots keep their existing signing profile.

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
step. Retention follows the repository's Actions artifact policy. A rejected or
timed-out submission keeps its receipt even if Apple has no log available yet;
an earlier architecture's completed evidence survives a later failure. Inspect
the recorded submission before retrying. No receipt is expected when the
notarization job fails before any submission.

The helper requires an empty `APPLE_NOTARY_EVIDENCE_DIR` for this public diagnostic
output; it refuses prior evidence instead of mixing attempts.
CI sets it to a dedicated runner temporary directory outside the private
credential scratch directory and release payloads. The uploader selects only
the receipt and validated log JSON files. Credentials, raw submission errors,
and invalid or mismatched logs are excluded.

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
selects its finalized artifact and includes both notarized DMGs. An Apple-side
failure permits fallback only after source metadata, both signatures and the
signed-byte handoff have been verified. That path selects
the original signing job's immutable artifact, independently rechecks the signed
macOS digest, publishes the six signed platform
archives and eight Linux packages, and omits every DMG, including any image that
was accepted before the other architecture failed. The release notes state that
macOS notarization was not verified. Linux, Windows, and container publication
retain their existing validation, signature, startup, and upgrade checks.
A cancelled or skipped notarization job does not authorize a new publication.
Failed handoff or input signature checks also block publication.
Stable publication continues to require successful notarization.

The build and signing jobs can pass while notarization fails. GitHub still marks
the overall workflow as failed when its notarization job fails, even if nightly
publication and the container job succeed. Published manifests and assets remain
immutable; a later notarization retry does not append DMGs to an existing release.

The default notarization wait is 30 minutes per submission. The packaging helper
accepts `APPLE_NOTARY_TIMEOUT` in the duration syntax supported by
`notarytool --timeout`; workflow use can set that environment variable on the
Apple notarization step. A timeout leaves a submission processing at Apple. Inspect
that submission before retrying the original run. An existing version tag must
never be recreated to bypass a release failure.

Stable human review and publication remain the existing
[online release ceremony](online-signing.md). Nightlies retain their main-only
publication environment, scoped app token, startup and populated-upgrade gates,
and authenticated predecessor chain. Already published runs skip Apple signing
and retain their original artifacts. Neither Apple job can write to
the repository, publish a release, or mint a Cosign OIDC identity.

## DMG and Homebrew installations

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
