# Apple signing implementation handoff

## Result

Implemented for Hikyo's stable and nightly release workflows. Both Apple Silicon and
Intel executables receive Developer ID signatures with hardened runtime and
secure timestamps. Their DMGs must pass Apple notarization, ticket stapling,
signature verification, and Gatekeeper assessment before the release draft can
be signed with Cosign. macOS tarballs contain the same signed executables.

The Homebrew cask now consumes these DMGs, with hashes taken from the verified
release manifest. It refuses missing, modified, or symlinked images and adds no
quarantine-removal hook. Historical manifests without DMGs remain verifiable;
new builds require both architecture images. DMGs use the existing `installer`
artifact type so deployed nightly readers can authenticate the new inventory.
Nightlies retain their predecessor,
compatibility, startup, upgrade, and scoped publication gates. Stable human
review, publication, and tap PR approval remain separate steps.

## Implementation

Both workflows build once, transfer artifacts by immutable artifact ID to a
read-only macOS signing job, then transfer finalized outputs to the Linux job
that builds OCI artifacts and generates provenance, manifest hashes, and Cosign
signatures. Transfers use tar archives to preserve executable permissions.
Source validation, docs, the floor benchmark, signed-tag verification, trust
preflight, and PostgreSQL-backed source tests remain required.

Apple credentials are confined to the signing step. The helper uses a disposable
Keychain and removes temporary certificate and API-key files on success and
failure. The candidate hash, source commit, tag, input checksums, executable
architectures, certificate type, and team are checked. Nightly signing checks
main, exact workflow SHA, resolved nightly version, and GoReleaser metadata
instead of a stable candidate. Published nightly runs skip signing without
replacing their original artifacts. The six-archive checksum
contract and Linux/Windows payloads are preserved.

## Validation

- Passed `go test -p 2 ./scripts/ci ./scripts/release/stable ./scripts/release/nightly ./internal/releasetrust`
  through the shared heavy-command queue, including workflow dependency and
  credential-isolation checks.
- Passed mock Apple-tool fixtures for successful stable/nightly runs and 19 refusal paths,
  including missing credentials, wrong identities, altered input, unexpected
  archive entries, symlinked executables, invalid signatures, notarization
  rejection/timeouts, ticket failures, and Gatekeeper rejection. These also
  check secret cleanup and unchanged Linux/Windows payloads.
- Passed Homebrew rendering and publication fixtures, including missing,
  tampered, and symlinked DMGs and refusal to use raw archives.
- Passed manifest producer/verifier fixtures, including DMG classification and
  the existing package/refusal cases. Stable round-trip tests now include DMGs
  and reject changed DMG bytes.
- Passed ShellCheck, Actionlint, and `git diff --check`.
- Review caught a rollout blocker: deployed nightly readers reject unknown
  artifact kinds. DMGs now use the existing `installer` kind. A signed-fixture
  regression ran against all nine production release-trust source files from
  base commit `da6c622749cb6ef8cf9a984e9f4e4d9e4f4473f7` using Go's source
  overlay. That actual previous reader accepted complete and Darwin platform
  verification, excluded DMGs from self-update selection, and rejected the new
  artifact kind as a negative control. Pair, identity, and tampering refusals
  also pass against the current reader.

Delivery PR: [#1018](https://github.com/Hikyo-Org/Hikyo/pull/1018). Commits and
pushes are signed and DCO signed-off, with GitHub verification confirmed.
The maintainer explicitly authorized merge after review and CI.
Remote CI remains pending. A public Hikyo release and clean-Mac installation
have not been verified. Configuration checks below use a harmless
test executable, separate from Hikyo release acceptance.

## Nightly publication credential custody

The live environment audit found the previously documented nightly app key
still at repository scope. The existing local key authenticated successfully
against the expected GitHub App. Configured exactly one `main` branch policy on
`nightly-release`, installed the key as an environment secret, verified its
presence, then removed and verified absence of the repository-scoped copy.
The existing client ID is unchanged. See the [custody handoff](nightly-release-custody.md).
This relocation does not establish absence of historical use; no app-key
rotation was performed.

## Configuration follow-up on 2026-10-21

Created a Developer ID Application certificate after the maintainer signed in to
Apple. Verified that it matches the locally generated encrypted private key and
chains to Apple's trusted root. Installed the identity in Keychain and configured
Hikyo's GitHub repository with all three required secrets and four variables.
The certificate expires on 2027-11-20.

Created a team notarization API key with the Developer role, verified actual
Apple service authentication, and stored the local profile as `hikyo-notary`.
Encrypted local key backups were verified and downloaded plaintext API-key
copies removed. Independent backup outside this Mac remains an operator task.

One initial API key was inadvertently exposed through a browser download data
URL in tool output. It was revoked immediately and confirmed in Apple's revoked
inventory before a replacement was configured. The exposed key was never stored
in GitHub. The replacement was handled without printing its private material.

A real universal test executable passes signature, Apple team, hardened-runtime,
and secure-timestamp checks. A separate smoke check successfully imported the
configured PKCS12 into a disposable CI-style Keychain, signed the executable and
a disk image without interaction, and verified both signatures. This caught and
fixed the required `=` prefix for inline `codesign -R` requirements; regression
fixtures now refuse the old syntax.

Apple received test submission `a814ad98-8834-490c-a7f3-edef44fdfcfd` for
`probe.dmg`; the initial five-minute wait expired while Apple still reported
`In Progress`. That timeout does not cancel the submission. A bounded background
wait is continuing against the same submission; it will staple and validate the
ticket and assess the DMG if Apple accepts it. Stapling and Gatekeeper acceptance
remain unverified until that completes.

The repeatable setup procedure and exact credential names are in the
[Apple signing guide](../release/apple-signing.md). No release or live tap content
has been published or changed during configuration.

After the remaining release gates are satisfied, an approved real build
must produce accepted Apple submission IDs, validated tickets, and passing DMG
assessments. Exercise quarantined direct downloads and tap install/upgrade on
separate Apple Silicon and Intel Macs. A stapled DMG does not prove offline
first-launch behavior of a copied standalone executable.

Apple's [Developer ID certificate guide](https://developer.apple.com/help/account/certificates/create-developer-id-certificates/)
and [notarization workflow](https://developer.apple.com/documentation/security/customizing-the-notarization-workflow)
are the primary references.
