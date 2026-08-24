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
