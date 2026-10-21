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

Apple credentials are confined to the signing step and the `apple-signing`
environment, restricted to branch `main` and tag pattern `v*`. The environment
contains all three secrets; repository-scoped copies were removed after their
environment metadata was verified. The four non-secret variables remain at
repository scope. The helper uses a disposable
Keychain and removes temporary certificate and API-key files on success and
failure. The candidate hash, source commit, tag, input checksums, executable
architectures, certificate type, and team are checked. Nightly signing checks
main, exact workflow SHA, resolved nightly version, and GoReleaser metadata
instead of a stable candidate. Published nightly runs skip signing without
replacing their original artifacts. The six-archive checksum
contract and Linux/Windows payloads are preserved. An independent original-build
digest checks the four Linux/Windows archive bytes after the Apple stage, and
nightly compatibility is checked against its original build digest. The image
preparation helper validates both Linux archives before writing any OCI input
or provenance, compares their members with the loose GoReleaser binaries, and
copies the executable image inputs from the archives themselves.

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
- The uncached full CI-script suite passed after the installer compatibility
  correction. Focused stable/nightly workflow tests then passed for protected
  environment binding, container retries across skipped signing ancestors,
  independent Linux/Windows integrity, and original nightly compatibility.
  Changed archive bytes are refused even with regenerated valid checksums.
- Image-root fixtures prove archive-derived OCI inputs and executable modes,
  with 14 refusal cases passing under BSD and GNU tar. Altering only a loose
  Linux binary, including arm64 after valid amd64 input, leaves no partial OCI
  root or provenance. Missing, linked, duplicated, malformed, non-executable,
  and mismatched archive content is rejected.
- The docs site now maps the Apple guide and its links. Content preparation,
  generator regression tests, generation, and the owning docs package check
  passed on pinned Node 26.7.0 with zero errors, warnings, or hints.
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
Standards and Spec reviews are CLEAN for implementation head
`f60208d4256cc6880d953b3d6f8a43837ca86f00`, including the corrected rollout
compatibility. Cross-provider review was skipped because the Anthropic
subscription is unavailable. Remote CI remains pending at this checkpoint. A
public Hikyo release and clean-Mac installation have not been verified. Configuration checks below use a harmless
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
Hikyo's protected `apple-signing` environment with all three required secrets
and the repository with four non-secret variables.
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
`probe.dmg`; the initial five-minute wait and a subsequent twenty-minute wait
expired while Apple still reported `In Progress`. Timeouts do not cancel the
submission. No duplicate was submitted. Acceptance, ticket stapling, and
Gatekeeper acceptance remain unverified; use the existing submission when
checking its eventual result.

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

## Merge-queue fixture repair

The first merge-group validation exposed two stale fixture assumptions: the
fast-feedback synthetic docs checkouts omitted the newly consumed Apple guide,
and the manifest fixture still counted only one `installer` after DMGs adopted
that existing kind. The guide is now included in both fixture setup paths and
the deletion-selection matrix. The manifest assertion requires all three
installers and exactly one `install.sh`, alongside its existing two-DMG check.
No production workflow, refusal, or required check was weakened.

The exact complete fast-feedback fixture passed on Node 26.7.0, including lane,
deadline, and dependency-compatibility subtests. Fail-fast runs of the manifest,
Homebrew renderer/publisher, archive-derived image root, release-binary reuse,
stable workflow/ceremony, nightly manifest, and Apple signing fixtures passed.
ShellCheck and diff checks passed for the repair.

The next merge-group run reached the cache-policy fixture, which still assumed
that every stable and nightly job used Ubuntu. The Apple signing jobs require
native macOS tools. The runner policy now admits only `apple-sign` in those two
workflows on `macos-15`, requires its protected `apple-signing` environment, and
refuses shared caches. All other jobs in those workflows retain the Ubuntu
runner requirement. This changes the policy fixture, not the production jobs.
The baseline and 25 mutation/control cases passed, covering both workflows,
wrong or duplicate runners, wrong environments, shared caches, setup-action
cache defaults, explicitly disabled caches, extra macOS jobs, and an unrelated
workflow using the same job name. The remaining supply-chain shell fixtures and
uncached queue-controller tests passed. ShellCheck and independent Standards
and Spec reviews passed for this repair.

The final CodeRabbit review requested the tag-creation dependencies in the
credential setup guide. Live readback confirmed the active admin-only release
creation rule (excluding nightlies), app-only nightly creation rule, and
zero-bypass update/deletion prohibition for all `v*` tags. The guide now names
those rulesets and distinguishes their creator restrictions from the
environment's ref-name policy. No live ruleset or environment was changed.
Content preparation, its regression test, and the owning docs package check
passed on Node 26.7.0 with zero diagnostics. Both delta review axes are CLEAN.

## Notarization diagnostic retention follow-up

The post-merge audit of PR #1018 found that the helper checked `Accepted` but
did not retrieve Apple's detailed notarization log. Apple's custom workflow
guide recommends reviewing logs even after success because they can contain
warnings. The follow-up retains a receipt for each submission with its version,
source commit, architecture, submitted disk-image name and pre-staple digest,
submission ID/status, and diagnostic availability. Completed submissions have
their detailed logs checked against this identity; accepted results require a
valid matching log before ticket stapling and publication can continue.

Stable and nightly signing jobs upload only dedicated public JSON diagnostics
with `always()`, so rejection, timeout, and later-architecture failures preserve
available evidence. Private Keychain/API-key scratch files, raw errors, and
invalid or mismatched provider logs are excluded. Retention uses the configured
Actions artifact policy. This adds evidence to the existing fail-closed release
flow; it does not establish an accepted real release or clean-Mac installation.

The fixture covers 37 scenarios, including all original 19 refusals, accepted
logs with warnings or optional fields, rejection, timeouts with and without an
ID, log download/shape/identity failures, stale or overlapping evidence paths,
pre-staple digest binding, and retained evidence after second-architecture
failure. Secret values and provider-controlled text are checked for absence from
workflow output. The original helper fails this regression at its missing
receipt. Focused workflow guards and ShellCheck passed; independent Standards
and Spec reviews found no issues.

CodeRabbit identified a missing terminal status: Apple can return `Rejected`
as well as `Invalid`. The initial normalizer recorded it as `Unknown` and
skipped its detailed log, although the release remained blocked. A fixture-first
regression reproduced the missing receipt status and log using mixed-case
`rEjEcTeD`. The normalizer and completed-status branch now retain `Rejected`
and retrieve its log while still refusing publication and ticket stapling.
