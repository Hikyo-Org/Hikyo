# Apple signing and notarization

Apple Developer Program enrollment makes Developer ID distribution available.
Enrollment alone does not install a signing identity or configure CI credentials.
Stable and nightly releases require Apple signing and notarization for both
macOS architectures before their final hashes and Cosign signatures are
generated. Local development snapshots keep their existing signing profile.

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
3. In the Hikyo repository's GitHub Actions settings, configure these secrets:
   - `APPLE_DEVELOPER_ID_P12_BASE64`: base64 encoding of the exported identity.
   - `APPLE_DEVELOPER_ID_P12_PASSWORD`: the export password.
   - `APPLE_NOTARY_KEY_P8`: the complete private key, preserving its PEM lines.
4. Configure these repository variables:
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
A macOS runner
receives those exact outputs from the same workflow run. It imports the identity
into a disposable Keychain, verifies the candidate hash, tag, commit, and Mach-O
architecture, and signs each macOS executable with hardened runtime and a secure
timestamp. Nightly signing instead binds the immutable build outputs and
GoReleaser metadata to the main workflow SHA and resolved nightly version.

For each architecture it creates and signs
`hikyo_VERSION_Darwin_ARCH.dmg`, submits it to `notarytool`, requires an
`Accepted` response, staples the ticket, validates it, verifies the Developer ID
signature against the configured team, and assesses the disk image with
Gatekeeper. The corresponding tarball contains the same signed executable.
Temporary certificate files, private API keys, and the Keychain are removed on
success and failure. Only public artifacts are transferred to the final job.

The final Linux job consumes these finalized artifacts, rechecks candidate and
archive hashes, builds the image and chart from the unchanged canonical Linux
binaries, and creates provenance and the release manifest. Both disk images
are covered by the authenticated release manifest. Stable releases also sign
each asset with Cosign. Checksums retain their
existing contract of exactly six platform archives. Apple failures or missing
credentials stop the release before manifest signing; there is no unsigned fallback.

The default notarization wait is 30 minutes per submission. The packaging helper
accepts `APPLE_NOTARY_TIMEOUT` in the duration syntax supported by
`notarytool --timeout`; workflow use can set that environment variable on the
Apple signing step. A timeout leaves a submission processing at Apple. Inspect
that submission before retrying the original run. An existing version tag must
never be recreated to bypass a release failure.

Stable human review and publication remain the existing
[online release ceremony](online-signing.md). Nightlies retain their main-only
publication environment, scoped app token, startup and populated-upgrade gates,
and authenticated predecessor chain. Already published runs skip Apple signing
and retain their original artifacts. The Apple job cannot write to
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
GitHub credentials and local signing have been verified, and a harmless test
disk image has been submitted to Apple. See the [configuration handoff](../handoff/apple-signing.md)
for its processing and acceptance status. No Hikyo release or live tap was
published or changed during setup.
