# Stable workflow signing and reviewed publication

Decision date: 2026-09-08. Owner authorized implementation of the combined
SOPS/OpenBao release model while delaying 1.0 for design work. This amendment
becomes operative with the implementation merge and explicit recovery-signed
policy activation. Implementation alone does not activate trust or release 1.0.

This amends the stable-signing custody and ceremony portions of
[OSS mechanics](oss-mechanics.md), [system architecture](system-architecture.md)
and [signed upgrade compatibility](signed-upgrade-compatibility.md). Other
artifact, compatibility, recovery-root and fail-closed requirements remain.

## Decision

> **Owner-locked declared amendment (2026-11-02, [#1124](https://github.com/Hikyo-Org/Hikyo/issues/1124), decisions in the [post-quantum migration ADR](./post-quantum-migration.md#decision-register); originating ticket [#714](https://github.com/Hikyo-Org/Hikyo/issues/714) reopened as PR #714; this ADR has no grilling issue, so the amendment is recorded against the owner's 2026-09-08 decision and the PR; operative only when the [#1132](https://github.com/Hikyo-Org/Hikyo/issues/1132) implementation merges):** Independent adversarial review is required; cross-provider review is SKIPPED by explicit owner instruction (2026-11-02) and is not CLEAN. Decision (keyless stable signing, recovery-signed policy): keyless signing is unchanged and remains a classical exception (E-03). A first-party PQ signature is added in addition, never instead: a maintainer-held ML-DSA-65 release key, declared in a recovery-signed, sequence-only keyset, signs the stable manifest (and optionally artifacts) at the local publish step, not in CI. The recovery-signed stable policy and every bridge need the full pinned recovery set, so the delegated workflow gains no authority over the keyset, policy, bridges or any root. Stable workflow identity alone satisfies no PQ requirement. **This amendment activates no signer, root, key or trust file, and merging the implementation adds readers and verifiers only: nothing requires a PQ signature on any shipping path, the existing classical verification is unchanged, and no release, tag, publication, root activation, notarization submission or environment cutover is authorized by it.** Reviewed draft publication is unchanged.

Use GitHub OIDC and Cosign keyless signatures for ordinary stable releases.
Build a complete signed draft under the exact immutable tag and source commit.
Publish through a separate local helper from clean current main only after the maintainer
reviews the draft and supplies its exact manifest SHA-256. Verify public bytes
again after publication. Record the release's signed metadata/catalog on main
before building the next version.

The existing recovery root authorizes a stable policy once. That policy pins
the repository and owner numeric identities, OIDC issuer, workflow path, stable
tag pattern, hosted runner identity, Fulcio/Rekor trust material and checkpoint
origin, SCT requirement and minimum release sequence. It binds the primary-key,
nightly-policy and bridge inventories. Routine workflow identity cannot grant
itself recovery authority, replace those inventories or authorize a new policy.

Keep the existing pinned root and online encrypted local recovery custody.
The previous offline/removable-media requirement is superseded for this flow.
Vaultwarden and an independently recoverable encrypted backup replace the need
for multiple USB copies. Custody has not been migrated or backed up by this
implementation; the operator must verify recovery before activation.

## Signed evidence and authority

The stable candidate names `github-actions-stable` and `stable-policy.json`.
Its canonical bytes and exact source commit enter the signed manifest. All
artifacts receive authenticated hashes and signatures. The manifest includes
the recovery-signed policy, its trusted root and the signed build-provenance
statement. Metadata and catalog are separate signed envelopes to avoid a
circular manifest hash. The catalog binds the exact stable policy digest.

Verifiers authenticate recovery delegation before accepting workflow signatures.
Certificates and transparency evidence must match the pinned identity and exact
tag/commit. Metadata and catalog sequence floors detect rollback and same-height
equivocation. Newly published metadata may advance a canonical baseline before
the trust-promotion PR merges, but cannot lower locally remembered floors.
Historical verification uses current authorization and revocation, rather than
letting old envelopes restore withdrawn authority.

Nightly signing keeps its distinct constrained policy. Historical keyed releases
retain their primary-key range/revocation semantics. Installers authenticate
standalone verifier bytes before execution. Runtime update, bundle assembly and
the release CLI share stable trust rules and rollback-state encoding/locking.
Older binaries that do not implement the new policy require an authenticated
verifier/runtime update before they can consume these releases.

The chart's optional keyless admission policy pins issuer, workflow/tag identity
and Rekor verification. It does not independently enforce every numeric identity,
SCT and checkpoint constraint of Hikyo's Go verifier. Full release verification
and the production bundle boundary remain required.

## Publication boundary

Build runs in GitHub Actions; publication runs on the maintainer's online Mac
using the existing administrator `gh` login. Build requires a cryptographically
verified signed tag on main history and green exact-commit main CI. The
operator helper additionally requires the current main tip when creating a tag. Publication accepts
only an exact reviewed manifest hash, rechecks tag/CI, verifies the full draft and
actual OCI signatures, and compares the asset inventory again immediately before
publishing. GitHub immutable releases lock the result. Post-publication download
verification must succeed. Retrying an already public immutable release verifies
the same bytes without modifying them.

The release workflow has GitHub publication permissions to stage drafts. The
human gate is enforced by reviewed workflow behavior, not cryptographic
two-person separation or a build token incapable of publishing. One maintainer may
approve their own work. Protected workflow changes and GitHub account security
are essential; compromise of that authority is an accepted material risk.

The immutable-release settings endpoint requires repository Administration:read,
which GitHub's built-in workflow token cannot receive. Running publication with
the existing operator login avoids a new stored PAT or a second GitHub App. The
helper checks the live setting before mutation and verifies the public release's
immutable status afterward; it does not alter repository settings.

## Accepted limitations and alternatives

Keyless signing removes recurring custody of a long-lived release signing key.
It moves routine release authority to GitHub identity and protected workflow
execution. It does not protect against an authorized malicious build. Provenance
is a signed first-party in-toto/SLSA statement binding inputs, producer and
artifact hashes, not an isolated builder, reproducible-build comparison or a
certified SLSA level. Independent builds remain a possible future control.

An offline primary would reduce key-exfiltration exposure but still sign bytes
built by CI and impose the operator burden the owner rejected. Hardware-backed
or threshold recovery can strengthen custody later but is not required now.
Replacing this with TUF would change the deployed trust model and add a separate
migration; this amendment preserves recovery-only policy authority and release
revocation semantics. SOPS/OpenBao are reference project practices, not new
runtime dependencies or hosted secret services.

Recovery custody remains security-critical. A compromised unlocked Vaultwarden
holding both ciphertext and passphrase may expose both. A compromised Mac can
also expose the locally available recovery key. Loss or compromise of that root
requires out-of-band rebootstrap. Revocations reach only verifiers that fetch
authenticated updates. No offline-generated-root or independent-human claim is
made.

Operational entry point: [online signing](../release/online-signing.md).
Primary-source evidence: [release ceremony research](../research/release-signing-ceremonies.md).
