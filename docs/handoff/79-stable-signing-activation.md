# #79 stable signing activation receipt

Status: **prepared activation PR; maintainer merge approval required**.
The [reviewed preparation packet](79-stable-trust-preparation.md) and
[stable signing decision](../adr/stable-workflow-signing.md) define this change.
This receipt records work performed on 2026-10-29 by the agent under Marc Went's
explicit backup/restore, drill and conditional exact-policy signing authority.

## Exact authority

- Source base: `ce5e85835383b6428676ff595e2a0c1a72f6a088`.
- Trust commit: `843d9c1f0c8d96902b045a21abeffd06419f57f5`.
- Policy SHA256: `0fe2cb70af971a694e15f68ddfa9a92646e0584a360f3df1893da12290f37203`.
- Trusted-root JSON SHA256: `6494e21ea73fa7ee769f85f57d5a3e6a08725eae1e38c755fc3517c9e6bc0b66`.
- Recovery public-key file SHA256: `1eb7ad2092668b73621c21a1eeb801ed6391bc794df1909abce8e1d45e03a229`.

The proposal reproduced the preparation packet byte for byte. Every pinned
trust, producer and verifier input in that packet remained unchanged. Live
repository `1316165429`, organization `316726515`, release workflow `329147028`,
immutable releases and main/tag rulesets matched the reviewed authority.
Stable policy on main and the `v1.0.0` ref returned HTTP 404 before signing.
No existing activation PR or branch was found.

After the real drill passed, the existing online ceremony displayed the exact
proposal. Its policy and root hashes were independently checked immediately
before the confirmation. The owner directed the agent to proceed in the
attended interaction under the existing authorization. The restored signer
produced one policy signature. The ceremony authenticated it and opened
[PR #1095](https://github.com/Hikyo-Org/Hikyo/pull/1095) with a signed/DCO commit.

Only three active trust files are added: `stable-policy.json`,
`stable-policy.sigstore.json` and `stable-trusted-root.json` in `release/trust`.
Root, metadata, catalog, primary, nightly and bridge authority are preserved.

## Real recovery proof

An AES-256 encrypted backup on the owner-approved local workstation is outside
the running Vaultwarden instance and its hypervisor failure domain. It includes
a consistent native container snapshot with database, attachments, configuration,
original server authentication key and restore/decryption dependencies. A
separate logical export covered the expected personal records and attachments,
including archived records. Private inventories, locations and checksums remain
in the protected operator record. This local copy does not establish protection
against loss of the workstation.

The backup was closed and reopened using decryption information available
independently of the source vault. The native snapshot restored into a disposable
Docker target with networking disabled, no published ports and an in-memory
root filesystem. Outbound integrations were suppressed. The original server
binary and authentication key served the restored data through a loopback HTTPS
endpoint with explicit certificate trust. Original account authentication
recovered the encrypted recovery attachment and passphrase from that restored
service. Their private checksums and pinned public key matched. The CLI logged
out before the restored server was removed.

The restored signer was denied network access and reads of original workstation
custody and Keychain. Its harmless, domain-labelled challenge verified with the
pinned recovery public key; a modified challenge failed verification.

- Redacted backup receipt: `79-native-backup-20261029`.
- Redacted restore receipt: `79-native-authenticated-restore-20261029`.
- Challenge SHA256: `ffaf58b81cbba5783445e081537e6d86d1f49c0022dc9207983365fbd61d6155`.
- Tools: Cosign v3.1.3, Bitwarden CLI 2026.10.1, restored Vaultwarden 1.37.4-r0,
  native `vzdump` snapshot/zstd and Docker isolation.
- Positive verification, modified-challenge rejection and isolation probes: PASS.

The independent encrypted backup and required evidence are retained. The
disposable restored server, encrypted restore scratch, recovered signing
materials and temporary secret input files were removed after the one policy
signature. Production vault data and original workstation custody were preserved.

## Validation and owner gate

The actual signed delegation passed Go preflight. Modified policy, changed root,
invalid signature and every incomplete nonempty policy/signature/root set were
rejected. Shared feedback and uncached scoped tests passed for `releasetrust`,
the stable release CLI, self-update, upgrade bundles and upgrade assembly, with
pinned Cosign interoperability enabled. Existing stable workflow, ceremony and
bundle shell fixtures passed, including producer/verifier negative contracts.
These fixtures establish local contracts; they do not prove a live stable OIDC
signing run or release acceptance.

Independent adversarial Standards and Spec review and exact-head hosted checks
are recorded on the PR. The owner's 2026-10-29 review policy retains those
reviews and removes mandatory cross-model review. This activation implements
the accepted signing decision and makes no locked-ADR amendment. Historical
formal ADR review dispositions are unchanged. Same-provider agent review does
not establish independent human or cross-provider review.

The owner must review the exact PR head and checks before separately authorizing
merge. After an approved merge, fresh main must pass
`scripts/release/ceremony.sh status`. This PR does not establish authenticated
policy on main, final integrated-main proof, stable release acceptance or
publication. No stable tag/freeze, release/registry publication, Apple submission,
deployment or merge was performed. #158 AWS, PR #969 and #1057 remain excluded;
accepted release deferrals are unchanged.
