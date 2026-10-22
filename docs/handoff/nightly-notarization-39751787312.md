# Nightly notarization failure 39751787312

## Hosted evidence

PR #1022 merged as `c136bf0e969fdbc9db7f9092eea6ca2981777725` after
successful full merge-group validation. The main-only manual wrapper triggered
[nightly run 39751787312](https://github.com/Hikyo-Org/Hikyo/actions/runs/39751787312)
on that commit. Full validation and artifact construction passed.

Both Apple signing attempts validated the imported identity and signed and
verified the executable and DMG. The earlier Keychain failure is repaired.
`notarytool submit` then terminated with signal 5 before returning a submission
ID, in jobs 119752884311 and 119758173632. Apple history had no corresponding
submission. Publication and container jobs were skipped. No nightly was released.

The protected Apple credentials already exist. The local API-key backup
authenticates successfully, and the original setup record uploaded decrypted
key material. No credential was changed during this investigation.

## Native reproduction

The hosted image defaults to Xcode 16.4; native diagnosis used Xcode 27 and
notarytool 1.1.3 (42). A small signed probe passed the original direct API-key,
wait, and JSON command options. Harmless architecture fixtures also passed
the full unchanged helper, including the disposable Keychain and cleanup.

The actual Darwin archives were recovered from the retained build artifact and
checked against its original checksum record:

| Architecture | SHA256 |
| --- | --- |
| arm64 | `e98ad87c5f20f762680e19df679eefd31e9c5aed5c525c5a0308f7959e28064f` |
| x86_64 | `4ea7d7143237b0b10b1defdae0acaa304b42f90fac641929d34f8f823c1fb32f` |

The focused helper reproduction used those exact Mac archives and build
metadata. Other-platform files were diagnostic stand-ins; this was not a full
release inventory verification or a publication attempt.

With the original filesystem default, the real arm64 payload produced an APFS
UDZO image which remained attached. A process sample showed notarytool blocked
inside `xar_open_digest_verify` while opening the image, with no upload or
submission in Apple history. Detaching that diagnostic image unblocked the
client, which returned a preflight error. Resubmitting the same detached image
was Accepted as `3055e89c-1484-48e4-9d2d-17801ebfe181`.

Changing only image creation to explicit HFS+ completed the helper for both
actual Mac payloads, with Accepted submissions, matching public logs, zero
warnings, successful stapling and validation, Developer ID verification, and
Gatekeeper acceptance:

| Architecture | Accepted submission |
| --- | --- |
| arm64 | `a40e4511-be36-461e-92be-973133f3ffb2` |
| x86_64 | `030002ed-a607-4c93-b669-0da9e7dddc10` |

This establishes a native filesystem/preflight failure and a successful focused
repair. The native symptom differs from the hosted signal; a fresh protected
nightly must establish whether the hosted crash is also repaired.

## Change and validation

The shared stable/nightly helper now requests `-fs HFS+ -format UDZO` rather than
inheriting the host filesystem. All signature, notarization, receipt/log,
stapling, Gatekeeper, and final checksum gates remain required.

The fixture now requires both the explicit filesystem and compressed UDIF
format. It failed against the original helper with the expected missing-format
requirement. Local focused checks and review results are recorded with the PR.

Local validation passed the full shell fixture, shell syntax, ShellCheck through
`scripts/ci/fast-feedback.sh --working-tree`, whitespace checks, and the focused
workflow/fixture tests:

```sh
GOMAXPROCS=2 go test -p 2 -parallel 2 ./scripts/ci -run 'Test(Apple|NightlyApple)' -count=1
```

## Completion boundary

The user authorized commit, push, PR, merge, and a new nightly. After exact-head
review and protected merge validation, trigger `nightly-manual.yml` on `main`.
Verify the new run's source, both Apple receipts, final DMGs, signed release
inventory, publication, and container stage. Native diagnostic acceptance alone
does not complete the release request.
