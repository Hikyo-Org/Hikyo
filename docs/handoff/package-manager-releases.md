# Package-manager release handoff

## Outcome

GoReleaser produces eight native Linux package assets from the canonical
release binaries: Debian, RPM, APK, and Arch Linux for amd64 and arm64. Packages
contain only `/usr/bin/hikyo` and `/usr/share/doc/hikyo/LICENSE`; no maintainer
script creates configuration, generates keys, installs a service, or starts a
process.

Every filename and native metadata identity is derived from the release version
and checked before signing. Package-bearing releases refuse SemVer build
metadata because Arch package metadata cannot preserve `+...` independently.
Prerelease identifiers must start with a letter so Arch's separator-free native
version remains distinct from every stable patch version.

The existing Linux-package trust boundary remains closed. `checksums.txt` still covers the
exact six bare archives. `create-manifest.sh` classifies every native package,
records its normalized format and architecture, and rejects unknown names.
`verify-bundle.sh` requires exactly one amd64 and one arm64 artifact for each of
the four formats. Offline signing therefore creates an independent Cosign
bundle for every package before the draft can become public.

## Homebrew publication

`render-homebrew-cask.sh` derives both macOS checksums from the verified release
manifest. Phase 5 calls `publish-homebrew-cask.sh` only after the public release
has been downloaded and passed `verify-bundle.sh --published`.

The publisher independently refuses a draft, skips prereleases, writes a
version-scoped branch in `Hikyo-Org/homebrew-tap`, and opens or refreshes a PR.
It never pushes to protected `main` and never merges. The release ceremony
prints the PR URL for separate review and the tap's `ci-required` gate.

Homebrew does not independently verify Hikyo's pinned signing root. It trusts
the selected tap/cask and verifies the archive SHA-256 recorded there; a tap
compromise can replace the hook, key, URL, and checksum together. The cask is
therefore a downstream convenience channel. Signed-bundle verification remains
the official fail-closed installation path.

## Nightly Homebrew publication

The development channel uses `Casks/hikyo@nightly.rb`; `Casks/hikyo.rb` remains
stable-only. Both consume the architecture-specific signed, notarized and
stapled DMGs, and each cask declares a conflict with the other because they
install the same `hikyo` executable.

After a nightly containing **both** DMGs is public, download its complete
bundle into an empty directory. From the repository root, with the pinned Go
toolchain, `gh`, `jq`, GitHub tap write access and recovery-authorized
`release/trust` files available, run:

```sh
# Set tag to the exact published nightly tag and bundle to its complete download.
: "${tag:?Set tag to the published nightly tag}" "${bundle:?Set bundle to an empty download directory}"
gh release download "$tag" --repo Hikyo-Org/Hikyo --dir "$bundle"
scripts/release/publish-homebrew-cask.sh --nightly \
  Hikyo-Org/Hikyo "$tag" "$bundle" Hikyo-Org/homebrew-tap
```

`HIKYO_HOMEBREW_TRUST_DIR` may select an independently pinned public trust
directory. The publisher requires a public nightly prerelease, authenticates
the entire bundle using the existing nightly verifier, and checks each DMG
against the authenticated manifest. Missing images, tampering or failed
authentication refuse publication before any tap mutation. The publisher also
downloads both DMGs from that public release and compares them with the
authenticated hashes. A missing or replaced public asset refuses the tap PR
even when the retained local bundle is valid. Existing branch
protection and the tap PR review remain in force. After that PR is merged:

```sh
brew install --cask hikyo-org/tap/hikyo@nightly
```

Nightly publication does not update the stable cask or merge the tap PR. The
nightly workflow does not gain a tap token. Publication is an explicit operator
step, as for stable casks. A nightly without accepted DMGs cannot update this
convenience channel.

## Validation

```text
GoReleaser v2.17.1 config check                              passed
GoReleaser snapshot                                        passed (8 packages)
snapshot-manifest_test.sh                                  passed
create-manifest_test.sh                                    passed
ceremony_test.sh                                           passed
render-homebrew-cask_test.sh                               passed
publish-homebrew-cask_test.sh                              passed
package-identity_test.sh                                   passed
verify-native-packages.go against real snapshot            passed (8 packages)
go test ./...                                               passed (3,983 tests)
three-round adversarial review                             passed
```

The snapshot proved `checksums.txt` still names only six archives. Its
`artifacts.json` reported all eight package assets under `hikyo-packages`, with
one amd64 and one arm64 output per format. CI, the official release build, and
the networked draft-validation boundary of the signing ceremony all run the
package verifier. It reads every package's full payload and hook metadata,
requires exactly the binary plus license, compares both files byte-for-byte
with the source license and canonical architecture archive, and executes
`hikyo version` on matching Linux runners.

## External boundary

`Hikyo-Org/homebrew-tap` was created as a public metadata-only repository. Its
`main` branch requires an up-to-date PR and `ci-required`, permits squash merge
only, forbids force-push/deletion, and has no bypass actors. Bootstrap commit
`41ca152e05e62609501141e36489a407c5684ff1` is green. PR #1 adds the shared
MPL-2.0 license at exact head
`e192846db3c71b26b5889402b2a540036865995c`; it is green and intentionally
awaits the human merge gate.

Hosted apt, RPM, APK, and pacman repositories remain out of scope; local
package files use the signed Hikyo bundle instead of repository-native package
signatures.

Unsigned nightlies publish the same eight package formats as convenience
artifacts after snapshot verification. They do not enter the offline signing
ceremony and do not carry the pinned-root guarantee.
