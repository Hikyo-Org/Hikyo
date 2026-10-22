# Nightly signing failure 39736147015

## Observed failure

The [scheduled nightly run](https://github.com/Hikyo-Org/Hikyo/actions/runs/39736147015),
attempt 1, tested main commit `e886fa491f66c3a9fdfc3b0104a8958c04b2f34b`.
Validation and artifact construction passed. The
[Apple signing job](https://github.com/Hikyo-Org/Hikyo/actions/runs/39736147015/job/119700141238)
verified all six input archive checksums, then failed with
`error: The specified item could not be found in the keychain.` No notarization
receipt was produced; publication and container jobs were skipped.

All three required secrets exist in the protected `apple-signing` environment:
`APPLE_DEVELOPER_ID_P12_BASE64`, `APPLE_DEVELOPER_ID_P12_PASSWORD`, and
`APPLE_NOTARY_KEY_P8`. The job received masked values. The local PKCS12 imports
successfully and matches the configured identity. No secret was changed.

## Repair

The helper created a disposable Keychain but omitted registering it in the user
search list. The `codesign --keychain` option restricts identity selection;
certificate-chain resolution still uses the search list. See the installed
`codesign` manual and [GitHub's runner setup](https://docs.github.com/en/actions/how-tos/deploy/deploy-to-third-party-platforms/sign-xcode-applications).
A configured login Keychain can conceal this omission during local checks.

`scripts/release/sign-apple-artifacts.sh` now registers the imported Keychain
alongside existing entries, checks that the configured identity is valid there,
and restores the original list before deleting the disposable Keychain. Paths
with spaces are preserved. An interrupted registration attempt also triggers
restoration. Import and identity failures receive explicit helper messages.

The shell fixture requires the signing Keychain and original entries to remain
available during signing. It verifies restoration after successful signing,
notarization refusals, a partially failed search-list update, and invalid,
different, or unreadable imported identities.

## Validation and limits

The strengthened fixture failed before the repair with the keychain error.
Afterward, validation passed:

```sh
scripts/ci/fast-feedback.sh --working-tree
GOMAXPROCS=2 go test -p 2 -parallel 2 ./scripts/ci -run 'Test(Apple|NightlyApple)' -count=1
sh -n scripts/release/sign-apple-artifacts.sh scripts/release/sign-apple-artifacts_test.sh
git diff --check
```

A native smoke on macOS 27.0.1 ran the actual helper with the existing PKCS12.
It signed and verified an arm64 Mach-O executable and DMG with the configured
Developer ID and secure timestamp. Notarization was mocked to stop the flow;
no Apple submission or release publication occurred. The original search list
was restored and credential scratch was removed.

An additional experiment replacing the default login Keychain produced a
certificate-chain error both before and after search-list registration. It is
not successful clean-runner signing proof. The original hosted log also does
not identify the exact failed Security.framework operation, so the missing
registration is a documented setup defect and proposed repair, not a proven
explanation of every possible cause of the hosted error.

Validated source baseline is the commit above with local script changes. The
SHA-256 of the two-script Git diff is
`2e6cf7a6c4aafdadcd3e82a2db813e374735ce66230e6b60bd7ca40b24452234`.
The initial validation above was local. Delivery is authorized through signed
commit, push, PR, protected merge, and a new nightly. Hosted recovery requires
a run containing the repair, successful signing and accepted
notarization for both architectures, validated stapled tickets, and completed
publication checks.
