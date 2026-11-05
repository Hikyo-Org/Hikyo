# Production trust bootstrap

This directory holds the committed public trust material: `root.json`,
`recovery-1.pub`, `primary-1.pub`, `metadata.json` (`metadata.sigstore.json`),
the upgrade `catalog.json` (`catalog.sigstore.json`) and the recovery-signed
`stable-policy.json` delegation with its Sigstore root. The recovery key signs
the stable policy; the stable release workflow then signs metadata and
catalog, and verifiers authenticate those through that delegation. Changing the
policy's inventories is a reviewed recovery re-signing (see
[signed nightlies](../../docs/operations/signed-nightlies.md#re-authorize-trust-over-workflow-signed-metadata)). Legacy
recovery-signed snapshots stay accepted. The `*.example` files are the fixture
shapes used by `scripts/release/test-fixtures.sh`.

The keys behind this root were generated on 2026-09-06 for nightly activation
under the custody exception recorded in [BOOTSTRAP.md](BOOTSTRAP.md): on a
network-connected Mac, encrypted private keys retained locally, passphrases in
the macOS Keychain. That record, not `docs/release/signing.md`, describes how
these particular keys came to exist. Stable release activation must consider
that custody first; see `docs/release/release-plan-1.0.md`.

Never commit `*.key`, decrypted key material, or a signing passphrase. Release
CI refuses to run until the production bootstrap files exist and verify.
