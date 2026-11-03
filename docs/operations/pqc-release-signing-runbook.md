# PQ release signing, bootstrap and recovery runbook (draft, NOT EXECUTABLE)

> **Not executable.** This is a plan for review, written for
> [#1132](https://github.com/Hikyo-Org/Hikyo/issues/1132). It must not be
> followed on any production system until **the owner names the custody of the
> ML-DSA recovery key** (DEC-14) and the maintainer has reviewed this runbook and
> given security approval, which has not happened. No signer exists, no root,
> key or trust file has been created or changed, and no release, tag,
> publication, root activation, notarization submission or environment cutover is
> authorized by this document. It contains no command to run, on purpose: the
> signing tool is not built, and the verifier fixtures generate their own
> nonproduction keys inside tests.
>
> Independent adversarial review is required; cross-provider review is SKIPPED by
> explicit owner instruction (2026-11-02) and is not CLEAN.

The classical procedures stay as they are: [release signing](../release/signing.md),
[online signing](../release/online-signing.md) and
[signed nightlies](signed-nightlies.md). Apple signing and notarization are
untouched; read [Apple signing](../release/apple-signing.md) first if that ever
changes. The verifier behavior this runbook relies on is in
[the #1132 handoff](../handoff/1132-release-verifiers.md); limits it cannot lift
are in [release trust exceptions](../security/pqc/release-trust-exceptions.md).

## What a PQ claim can and cannot say

A `pqc-auth` claim for release and update needs three things at once: a
persisted verification that required every algorithm of the pinned set, an
anchor that reached the host without a forgeable introduction, and every
binary-replacement path closed. The first is built as a verifier. The second is
unsolved (E-11): a PQ key first introduced over the classical channel is only as
authentic as that channel. The third is open (E-15): the nightly track, the
first-use bridge script, `sudo hikyo upgrade` and image replacement have no PQ
signer. **The claim stays HOLD.** Do not publish wording that says otherwise.

## Roles and keys

| Role | Algorithm set | Held by | Signs | Cannot sign |
| --- | --- | --- | --- | --- |
| Recovery set | P-256 recovery key (existing) **and** an ML-DSA-65 recovery key, both required | Owner, offline. ML-DSA custody **not named: HOLD** | The PQ keyset, the stable policy, nightly policies, every bridge, and the root generation | Releases |
| Release set | One ML-DSA-65 release key, valid for a declared release-sequence range | A named maintainer, at the local publish step | Stable manifest, optionally individual artifacts, and metadata and catalog under a keyless delegation | The keyset, any policy, any bridge, any root |
| Delegated CI | Sigstore keyless identity for the pinned workflow (existing) | GitHub Actions | Unchanged classical evidence only | Anything PQ. CI never holds a PQ key and gains no root, keyset or bridge authority from the new algorithm |

Only these algorithm identifiers exist in the PQ form: `ecdsa-p256-sha256` and
`mldsa65`. ML-DSA-44, ML-DSA-87, SLH-DSA, RSA, Ed25519 and P-384 are refused.

## Preconditions (all must be recorded before any step below)

1. The owner names ML-DSA recovery custody and a failure domain separate from
   the datastore and the root key (E-18, DEC-12). Software custody only; no HSM
   or KMS is assumed.
2. A release containing the readers and verifiers has shipped and the reviewed
   minimum version is recorded. Without it nothing can verify a PQ document.
3. The maintainer has reviewed the verifier and this runbook and given security
   approval for the implementation. "Security approval: not given" holds today.
4. The owner has decided how the pin reaches hosts (a second stamped value
   beside the unchanged v1 root, or something else). That mechanism is **not
   built** and is not designed here.
5. Every other binary-replacement path is closed or registered: the nightly
   track, `sudo hikyo upgrade`, image replacement, the first-use bridge script
   and the chart image policy.

## Key generation (requirements, not steps)

- Generate on the custodian host with the platform entropy source, through
  protected input and output paths. Seeds are 32 bytes and a seed is the whole
  key; private keys are stored as seed-only PKCS#8.
- A private key never appears in argv, a log, an audit record, a receipt, an
  error, a commit or a CI secret. The dangerous-print escape is never used.
- The public key is distributed as a canonical SPKI PEM; the key identity is
  `mldsa65:` plus a SHA-256 over the length-prefixed domain, algorithm and
  public key bytes. A key file is accepted only when its digest matches the pin.
- Escrow follows the existing rule: a failure domain separate from the datastore
  and the root key. With one maintainer that means separate storage and access
  control, not separate people.

## Bootstrap: introducing a PQ pin (planned)

1. Prepare the PQ root, a new schema (`hikyo.dev/trust-root/v2`) naming the
   existing P-256 recovery key and one new ML-DSA recovery key, with a generation
   number. It extends, never replaces, the v1 root; the v1 root bytes and digest
   stay as they are so every comparison against them stays true.
2. Out of band, publish the recovery fingerprints (key identities, not key
   bytes) on a channel independent of the release channel, and have the owner
   confirm them against the offline material.
3. Ship a build that carries the PQ root as an additional pinned value. Any
   host that accepts this build over the classical channel after a quantum
   forger exists cannot tell a forged build from a real one: this window is the
   E-11 limit and the runbook cannot close it.
4. A host records its first persisted verification that required every
   algorithm only after the keyset, metadata, catalog and a release have all
   verified under the full pinned set. Until then it holds no claim.
5. Do not require PQ on any path in the same release that introduces the pin.
   Require it only after the minimum reader version is observable for the
   fleet the operator controls, and say plainly what cannot be observed
   (E-17).

## Routine signing at the local publish step (planned)

The signing input for every document is fixed by the verifier: the length-prefixed
label for the document kind, the profile, the pin digest and generation, the
ordered required algorithm and key identities, the document kind, channel,
version, subject, source commit, sequence and policy digest, then the SHA-256 of
the exact bytes. Every required algorithm signs the same bytes; the ML-DSA
signature uses the label as its context. A signature set lists one signature
per required algorithm and nothing else.

1. Build and verify the release by the existing keyless procedure. Do not skip it.
2. On the maintainer host, produce the release-role signature over the stable
   manifest. Publish the signature set as an extra file beside the release; it is
   deliberately **not** listed in the manifest, because the shipped installers
   refuse an unknown manifest artifact kind.
3. Verify the set with the verifier fixture path on a different host before
   publication, and keep the exact digests as evidence.
4. Never sign in CI and never sign a document the keyless run produced without
   reading its bytes first.

Note that the stable release-verification tool in CI refuses unlisted files in a
release directory; adding sidecars to that directory needs a reviewed change to
that tool and is not part of this ticket.

## Release key rotation and revocation (planned)

- A rotation or revocation is a new keyset at the next sequence, signed by the
  **full** recovery set. One remaining algorithm cannot replace the other.
- Rotation: add the new release key with a start sequence, close the old key's
  range, publish the new keyset. Exactly one non-revoked key may cover a release
  sequence; two covering keys are refused.
- Revocation: mark the key revoked in a new keyset sequence. A revoked key
  cannot sign even for sequences it once covered. Do not re-publish an older
  keyset: the persisted floor refuses it, and a revoked key is never revived.
- After a revocation no PQ verification succeeds until a keyset names a new key.
  Classical verification is unchanged, because PQ is not required on any path
  today.

## Recovery of a lost or compromised ML-DSA recovery key (planned)

There is no in-band transition. The only recovery is an **operator-installed
root of a higher generation, obtained from an independently verified source**
(an offline reinstall). That is root replacement, not an authenticated
transition, and the claim it supports is limited accordingly (E-11, E-18).

1. Generate a new ML-DSA recovery key under the custody rules above.
2. Issue a new PQ root with the next generation and the same P-256 recovery key.
3. Publish a keyset at the next sequence signed by the new full recovery set.
4. The operator installs the new root on each host from the independent source.
   Hosts keep their sequence floors; a host presented with the old root after the
   new one is refused as a rollback below the generation floor.
5. Treat all evidence signed by the lost key as unverifiable for new decisions,
   and record the loss.

## Rollback

Roll back only to a build at or above the format floor, with valid trust and
retained custody. Never strip PQ verification, lower a persisted floor, re-enable a
revoked key or destroy retained keys. A floor with a newer format than a reader
understands makes that reader refuse the surface; a reader built before the floor
existed cannot refuse (E-17).

## Evidence to keep for each signing event

Candidate commit, pin digest and generation, key identities, the signature set
digests, the verifier output on an independent host, the persisted floor values,
and the exact bytes verified. Local verification, hosted CI, real-provider checks
and runtime acceptance are separate evidence classes and are reported separately.

## Open decisions for the owner

1. Name the ML-DSA recovery key custody (blocks everything above).
2. Decide whether the release key is distinct from the recovery key. The verifier
   and fixtures require them distinct; the decided inputs do not say.
3. Decide how the PQ pin reaches a binary (a second stamped value is the only
   shape that leaves the v1 root digest unchanged).
4. Decide whether the nightly track gets a PQ signer (E-15).
5. Approve the signing granularity: manifest only, or manifest plus every artifact
   (a measured size difference is in the handoff).
