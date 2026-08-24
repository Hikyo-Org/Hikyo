# Release signing runbook

CI builds and publishes an **unsigned draft**. It never receives either private
key. The maintainer signs locally; only verified, already-published bytes become
an official release.

Ordinary `main` CI also retains GoReleaser archives for 14 days as an unsigned
development artifact named for the exact source commit. Those snapshots carry
checksums and an explicit warning, but no release manifest or signature. They
are evaluation output only: they never create a GitHub Release and cannot enter
this ceremony as a release candidate.

## Interactive macOS assistant

`scripts/release/ceremony.sh` guides the same locked ceremony as five resumable
phases: bootstrap, candidate/PR, immutable tag and draft verification, recovery
binding plus primary signing, and publication plus external verification.

Run a mutation-free preview first:

```sh
./scripts/release/ceremony.sh --dry-run
```

Then run the selected real phase without the flag. `state.json` lives under
`${XDG_STATE_HOME:-~/Library/Application Support}/hikyo/release-ceremony/` and
contains only version, tag, phase, and repository identity; public draft and
signed assets live beside it. Rerunning a phase loads that checkpoint, verifies
already-created tags, PRs, downloads, OCI signatures, and release assets, then
continues from the first incomplete boundary. The assistant never stores a
private key or passphrase on workstation storage. It refuses an online route during offline
phases, uses an unindexed macOS RAM disk, disables core dumps, requires exact
typed confirmation before GitHub mutations, and delegates cryptographic policy
to the release scripts documented below. It refuses to mount signing storage
unless macOS reports FileVault is on, which keeps workstation swap encrypted.
Cross-release rollback memory remains separately at
`${XDG_STATE_HOME:-~/Library/Application Support}/hikyo/release-trust.json`;
never delete or version-scope that file.

The operator still controls network disconnection, removable-media insertion
and ejection for normal releases, both passphrase entries used by each key,
GHCR login, and PR review. Store four distinct secrets in Vaultwarden: primary
Cosign, recovery Cosign, primary `age`, and recovery `age` passphrases. Never
attach the `.key.age` files to Vaultwarden items. The assistant accepts those files only
from external/removable volumes and refuses networking while key media remains
mounted.
Dry-run output is guidance only; it does not create progress state.

## Pinned tools

- GoReleaser `v2.17.1`
- cosign `v3.1.3`
- age `v1.3.1`
- Syft `v1.50.0`
- Helm `v4.2.3`
- Go `1.27.0` from `go.mod`
- Docker client `29.7.2` with Buildx `v0.36.1-desktop.1` for live GHCR
  image and chart digest resolution

Verify downloaded tool archives against the checksum asset attached to that
exact upstream release. GitHub Actions are full-SHA pinned in each workflow;
the repository setting rejects tag-form action references.

## One-time trust bootstrap

1. Disconnect networking. Run `ulimit -c 0`. Mount a memory-backed working
   directory; do not use a normal disk, synced folder, indexed folder, or a
   location covered by workstation backups.
2. Run `cosign generate-key-pair --output-key-prefix primary-1` and separately
   `cosign generate-key-pair --output-key-prefix recovery-1`, using different
   password-manager passphrases. While still on memory-backed storage, wrap
   each Cosign-encrypted `.key` again with `age --passphrase`; copy only the
   resulting `.key.age` file to two distinct external USB devices in separate
   locations. Recovery media must be separate from primary media. Store the
   distinct `age` passphrases in the password manager too.
3. Commit only both `.pub` files, `release/trust/root.json`, and the initial
   `metadata.json`. `root.json` pins each public-key filename and SHA-256.
   Sequence 1 metadata pins the bootstrap primary from the root and records the
   first version as `pending_release`; it does not claim that draft is current.
4. Sign `metadata.json` with the recovery root using `cosign sign-blob
   --new-bundle-format=false --tlog-upload=false --use-signing-config=false
   --bundle metadata.sigstore.json`. The legacy-output switches are required by
   pinned cosign v3.1.3: they keep the operation offline and allow the OCI
   ceremony to emit a raw signature. Commit the bundle. Eject media,
   wipe the tmpfs, then reconnect networking.
5. Before the first tag, verify the recovery signature, pinned bootstrap key,
   and every public-key hash with
   `scripts/release/verify-bundle.sh --root release/trust/root.json --metadata
   release/trust/metadata.json --metadata-signature
   release/trust/metadata.sigstore.json --state PATH --trust-only`. This writes
   trust sequence 1 with null latest-release fields. The later bound metadata
   must advance to sequence 2 before it can replace that state. Separately run
   `scripts/release/test-fixtures.sh` for the complete signed-bundle rehearsal.
   A real release tag remains blocked until all production trust files exist.

The recovery key signs trust-metadata changes only. A primary signature can
never replace the recovery root. Current v1 verification deliberately requires
recovery authorization for rotation and revocation; this is stricter than
allowing a routine old-primary rotation and keeps the recovery direction
one-way.

Cosign performs every cryptographic operation and defines the OCI signature
payload; the shell code only enforces the project-specific release-range and
recovery policy locked by the ADR. TUF was not substituted for that policy:
doing so would replace the mandated long-lived cosign root, recovery-only
authority, and release-range revocation semantics. Changing those semantics is
an ADR amendment, not an implementation refactor.

## Per-release ceremony

1. For every release after the bootstrap release, before tagging, increment the
   trust-metadata `sequence`, set `event.type` to `release-candidate`, and add
   `pending_release` with the exact version, monotonic release sequence, and 64
   zeroes as its `manifest_sha256`. Do **not** change `releases`,
   `highest_release`, or `highest_release_sequence`: the currently published
   installer must remain current while the new release is only a draft.
   Recovery-sign this candidate metadata offline, commit it, and merge it to
   `main`. For the first release, sequence 1 bootstrap metadata is already the
   candidate; skip this step and bind it directly to sequence 2 after CI builds
   the draft.
2. Create `vX.Y.Z` at that merged commit. CI verifies reachability and prior
   non-use, proves a changed-SHA update to the permanent non-release
   `v-ruleset-probe` tag is rejected, builds GoReleaser archives for
   Linux/macOS/Windows on amd64/arm64,
   copies the same GoReleaser Linux binaries into the distroless amd64/arm64
   image, produces Debian, RPM, APK, and Arch Linux packages for amd64/arm64,
   pushes that image and the digest-pinned Helm chart, emits binary
   provenance binding both package inputs to the candidate commit and hashes,
   source and image SPDX SBOMs, renders an installer containing the exact trust
   root and verifier hashes, and opens a draft GitHub release. CI also makes the
   cosign OCI payloads while it has registry access; it never signs them.
3. With networking on and no decrypted key present, download every draft asset.
   Recompute `checksums.txt`, compare the GHCR index digest with
   `image-index.digest` and `chart-index.digest`. Confirm both prepared OCI
   payloads name those exact published subjects. Confirm the manifest's
   version, sequence, commit, and signing key match the canonical
   `release-candidate.json` artifact; its hash is part of the manifest.
4. Disconnect networking, mount tmpfs, decrypt only the recovery key, and run
   `scripts/release/bind-manifest.sh release-manifest.json metadata.json
   metadata.bound.json`. Recovery-sign `metadata.bound.json`, then re-encrypt
   and eject recovery media. Reconnect only after plaintext is gone; commit the
   bound file as `release/trust/metadata.json` plus its signature to `main`.
   `bind-manifest.sh` increments the trust sequence again, converts the pending
   row into a finalized release row, and only then advances `highest_release`.
   The release remains a draft. This makes rebuilding different bytes under an
   already-used version fail verification even if a primary key signs them.
5. Disconnect networking again, set `ulimit -c 0`, mount tmpfs, decrypt only the
   primary key there, disable core dumps, and run
   `scripts/release/sign-bundle.sh`. It creates a cosign bundle for every asset
   and raw signatures for both prepared OCI payloads. Remove plaintext,
   unmount tmpfs, and eject the key media before networking returns.
6. With networking restored and no private key mounted, run
   `scripts/release/publish-oci-signatures.sh BUNDLE ROOT METADATA
   METADATA_SIGNATURE`. It re-verifies the candidate-bound bundle, derives the
   primary public key from that candidate, attaches the offline signatures to
   the exact image and chart digests, then requires `cosign verify` to succeed
   for both published subjects. Upload the manifest and every
   `*.sigstore.json` bundle to the draft; raw OCI signatures are transport
   scratch and are not release assets.
7. Redownload the complete draft and verify it through
   `verify-bundle.sh --published --state
   "$XDG_STATE_HOME/hikyo/release-trust.json"`; then publish the draft. The
   installer fetches current recovery-signed metadata from `main`, then runs the
   same published-subject check before extracting a binary. The pinned root and
   verifier code still come from the immutable release tag.
   GitHub immutable releases locks its assets at publication. Preserve that
   state file; deleting it discards locally remembered rollback protection.
8. After the now-public release is downloaded and verified again, the ceremony
   renders `Casks/hikyo.rb` from the signed macOS archive records and opens or
   refreshes `Hikyo-Org/homebrew-tap`'s protected release PR. Drafts never reach
   this step, prereleases do not update the stable cask, and the ceremony never
   merges the tap PR. Review its `ci-required` result and merge separately.
   Homebrew authorizes the tap/cask and checks the rendered SHA-256, but does
   not independently verify Hikyo's pinned signing root; this is a convenience
   channel, not an official fail-closed installer. Users requiring that trust
   guarantee must use the complete signed-bundle verification path.

`checksums.txt` remains GoReleaser's exact six-archive checksum list. Native
packages are not added to that legacy list; each package byte stream is instead
hashed in the release manifest and receives its own offline Cosign bundle. The
package payload is only `/usr/bin/hikyo` plus the MPL-2.0 license: it creates no
configuration and never installs or starts a service. Before the draft is
created, the release build extracts all eight packages and compares their
binary and license bytes with the canonical architecture archives and source
license.
`binary-provenance.json` records the GoReleaser configuration hash and proves
that each Linux archive input and OCI image input has the same binary hash. The
signed release manifest is authoritative for the canonical release candidate,
binaries, native packages, binary provenance,
SBOMs, installer, chart, digest files, and OCI payloads.

Hash agreement proves asset consistency, not an honest CI build. Reproducible
build comparison is the named future control for compromised-CI risk.

## Automated nightly publication identity

Nightly builds do not use either offline signing key. An organization-owned
GitHub App named `Hikyo Nightly Release`, installed only on this repository,
owns nightly tag and prerelease publication. It has only repository
`Contents: read and write`, no webhook, and no event subscriptions.
Each nightly publishes the six platform archives and all eight native Linux
packages produced by the same verified GoReleaser snapshot. They remain
explicitly unsigned development artifacts, not pinned-root releases.

The workflow stores the app client ID in the
`NIGHTLY_RELEASE_APP_CLIENT_ID` repository variable and its private key in the
`NIGHTLY_RELEASE_APP_PRIVATE_KEY` Actions secret. Each run mints a short-lived,
current-repository-only token and requests only `contents: write`. The built-in
workflow token remains read-only. `configure-repository.sh` applies the
dedicated app as the sole bypass actor for creation of `v*-nightly.*`; stable
tag creation remains admin-only and every `v*` tag remains immutable.

## Rotation, revocation, and loss

- Rotation: recovery-sign metadata that closes the old primary at a named
  release sequence and activates the new primary at the next. No overlap.
- Primary compromise or workstation compromise: recovery-sign a distinct
  revocation event. `verify-bundle.sh` refuses that primary for old releases too.
- Lost primary: conservatively recovery-revoke it, then rotate; this prevents a
  later recovery of the old private key from fabricating a historical bundle.
  Lost recovery: out-of-band
  recovery-root rebootstrap; a primary-signed replacement is invalid. Both lost:
  full out-of-band trust bootstrap.
- Run restore/decrypt/sign/verify from each USB copy yearly and before first use
  after any storage change.
