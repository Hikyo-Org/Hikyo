# Nightly bootstrap record

Created 2026-09-06 for nightly release activation. The operator explicitly
authorized generating the keys on a network-connected Mac and retaining their
encrypted files locally. This is an exception to the offline/removable-media
bootstrap procedure, not evidence that an offline ceremony occurred.

The local encrypted Cosign key files live under
`~/Library/Application Support/hikyo/nightly-bootstrap/` with owner-only access.
Independent randomly generated passphrases live in macOS Keychain under service
`io.hikyo.nightly-bootstrap.cosign`, accounts `primary-1` and `recovery-1`.
They are not included in Git or Actions. FileVault was enabled at generation.
The retained local `tools/key-custody` helper retrieves a passphrase internally
and invokes pinned Cosign without printing the passphrase.

Public identities:

| Document | SHA-256 |
| --- | --- |
| Root JSON | `6ef6ae8d643359bae441c2708fb0950571edc579a8fef7cd7455d20dbcda266e` |
| Recovery public key | `1eb7ad2092668b73621c21a1eeb801ed6391bc794df1909abce8e1d45e03a229` |
| Nightly policy | `afaeb19fc0299f700234a74aa0817f77ce0833853b327a9d9d9d126d25d5b04f` |
| Sigstore trusted root | `6494e21ea73fa7ee769f85f57d5a3e6a08725eae1e38c755fc3517c9e6bc0b66` |

Sigstore roots were fetched through the maintained TUF client using its embedded
public-good root. The current Rekor v1 checkpoint was verified against those
authenticated log keys before pinning its origin in the policy. Repository and
owner numeric IDs were checked against GitHub. Production SCT verification is
enabled. Cosign v3.1.3 was checked against both its upstream checksum asset and
GitHub's release-asset SHA-256.

Recovery-signed metadata sequence 1 and initial catalog sequence 1 authorize the
nightly policy. Catalog sequence 2 additionally authorizes the two exact
[legacy bridges](BRIDGES.md) into the first published signed nightly. There are
no approved stable releases. The metadata's
`pending_release` of `1.0.0` is the schema-required unsigned bootstrap placeholder;
it is not a release approval. This root remains technically capable of
authorizing stable releases. Its online/local custody must be considered before
future stable activation; it must never be described as an offline-generated root.

Local verification passed `nightly preflight` and `verify-bundle.sh --trust-only`.
PRs #687 and #689 activated the public bootstrap and corrected Rekor shard-index
verification. The first actual OIDC-signed nightly,
`v0.0.1-nightly.20260906.26.g52c8b012`, passed independent verification of every
published asset and packaged production startup/restart on both engines.
Its legacy bridge statements and catalog sequence 2 were subsequently signed
with the same encrypted local recovery key under the approved nightly custody
exception. They require separate installation backup/drill evidence and a full
writer stop; signing these public documents does not upgrade a running server.
