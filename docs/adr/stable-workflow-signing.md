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

> **Owner-locked declared amendment (2026-11-02, [#1124](https://github.com/Hikyo-Org/Hikyo/issues/1124), decisions DEC-14 and DEC-15 in the [post-quantum migration ADR](./post-quantum-migration.md#decision-register); originating record: [PR #714](https://github.com/Hikyo-Org/Hikyo/pull/714), merged, and the owner's 2026-09-08 decision; this ADR has no grilling issue, so no ticket is reopened; operative only when the pull request implementing [#1132](https://github.com/Hikyo-Org/Hikyo/issues/1132) ([#1192](https://github.com/Hikyo-Org/Hikyo/pull/1192)) merges). Independent adversarial review is required; cross-provider review is SKIPPED by explicit owner instruction (2026-11-02) and is not CLEAN.** Trust documents (root, metadata, catalog, stable policy, nightly policies, bridges) gain a PQ-capable form valid only when the pinned required signature set verifies; progression is sequence-only; a PQ authenticity claim is made only after a persisted verification that required every algorithm. How PQ keys first reach existing installs is unsolved and is a #1132 feasibility question: the claim stays HOLD until #1132 closes or registers every binary-replacement path, and the nightly track and image replacement have no PQ signer. Stable ML-DSA signatures would come from a maintainer-held key at the local publish step, not from keyless CI; keyless signing is unchanged and stays a classical exception. Backup custody landed as the 2026-11-02 banner on that ADR's custody section.
>
> **Implementation note ([#1132](https://github.com/Hikyo-Org/Hikyo/issues/1132), not owner-locked, neutral on outcome):** the implementing pull request adds readers, verifiers and nonproduction test fixtures only. It activates no signer, root, key or trust file and makes no shipping path require a PQ signature. Its design choices are not part of this amendment and are open to the reviewer and the owner: a recovery-signed keyset of release keys, separate recovery and release signer roles, and a PQ verification record kept apart from the classical floor (persisting it on a host is not built). The last sentence of the amendment text refers to the custody section of signed upgrade compatibility. See the [#1132 handoff](../handoff/1132-release-verifiers.md).

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
