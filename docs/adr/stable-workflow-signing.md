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

> **Owner-locked declared amendment (2026-11-15, unattended stable patch releases for Dependabot security updates; originating record: the owner's 2026-11-15 decisions and the pull request implementing them; this ADR has no grilling issue, so no ticket is reopened; operative only when that pull request merges and the owner provisions the stable-release App).** This amends the "Publication boundary" below for one case: a PATCH release whose only reason is a merged Dependabot security update may be tagged, published and recorded in trust without a human at the keyboard. Every other release stays on the maintainer ceremony. The amendment keeps what the ceremony proves and changes who runs it:
>
> - **Publisher.** A single level-triggered reconciler workflow on `main` (`stable-release-reconciler`) tags, waits for `release-build`, publishes the staged draft with the unchanged `publish-stable-draft.sh`, opens an app-signed trust PR with auto-merge, and deletes the merged trust branch. It acts through a dedicated GitHub App (`stable-release`), never a PAT, and mints a per-job token scoped to that job's permissions behind a main-only `stable-release` environment. It publishes only a draft that the app itself built: it considers a `release-build` run only when the run's actor and triggering actor are both the stable-release bot and its `head_sha` is the tag commit, and it checks that again before it calls the publish helper. A person's tag, even a patch, is left for the ceremony. No step waits for the docs after the trust sync; the next tag already does, because the planner defers while a docs deployment runs or hikyo.app fails the live docs gate.
> - **Manifest SHA-256.** The digest that the maintainer reviewed becomes a tamper check: the build run uploads it, and the reconciler passes the digest of that exact run to the helper, which still rechecks the draft, every signature, the asset inventory, OCI subjects and immutability. It no longer proves human review. A human release still requires the maintainer to supply the reviewed hash.
> - **Second tag authority.** `require-signed-tag.sh` decides by who pushed the tag, read from the `release-build` run itself, never from the tag signature. A run whose actor or triggering actor is a Bot, or whose id is the stable-release bot id, takes only the unattended rules, even when GitHub reports its tag verified: both actors must equal `STABLE_RELEASE_APP_BOT_ID`, the version is the exact patch successor of the latest signed stable, `releases/latest` is that release, no higher stable tag exists, and exact-commit main CI is green. A person's run keeps the verified signed annotated tag rule and also needs repository admin permission. An app tag can never start a minor, a major, the first release or a prerelease. The build derives its authority from the same run, so the draft notes and patch-only signing follow the pusher.
> - **Patch-only and compatibility.** The Go tool refuses `--patch-only` unless the version is exactly the latest stable plus one patch. "Not breaking" is defined operationally: both the stable catalog engine edges must contain exactly one `restart`-mode edge from the previous release sequence, so the signed upgrade path stays continuous. A patch that cannot show this stops and opens the stall issue.
> - **Custody.** The App private key lives only in the `stable-release` environment, which admits `main` alone. It has no workflow-editing, secrets or settings permission. A person with the ability to merge to `main` and approve the environment already held equivalent release authority; this moves that authority from a person's login to a protected, auditable identity and is an accepted risk.
> - **Retry gap.** Release tags are immutable. A failed app-built tag cannot be retried because a re-run changes the triggering actor and the App deliberately has no `actions: write`. The reconciler opens a single stall issue. If the failed build staged a draft, that draft blocks every later candidate (`require-synced-trust.sh` refuses an outstanding draft) until it is deleted: `gh release delete vX.Y.Z --repo Hikyo-Org/Hikyo --yes`. The tag stays burned; a maintainer then releases the next patch by hand. A maintainer can also finish a good app-tagged draft locally with `STABLE_RELEASE_APP_BOT_ID=<bot id>` exported, so that `ceremony.sh publish` sees the app authority and applies the same patch-only rules.
> - **Trust sync.** Because trust files now land through an app-signed `release/sync-trust-*` PR, a ruleset restricts that branch pattern to the App. The human `ceremony.sh sync-trust` push is blocked while that ruleset is active; the reconciler performs the sync, and a maintainer recovers by opening the PR from another branch name. Rollback is `configure-repository.sh` run without `HIKYO_STABLE_RELEASE_APP_ID`: it removes the App bypass from the tag policy and deletes the trust branch ruleset, then asserts that it is gone.
> - **App permissions.** Contents write (tag, trust commit and PR branch, branch cleanup, the tap's cask branch), Pull requests write (queue, trust PR, tap PR), Administration read (the publish helper reads the immutable-releases setting), Vulnerability alerts read (the Dependabot proof), Actions read (the pusher of a run, its digest artifact, and the CI runs behind `require-green-main.sh`), Checks read on the tap only (`ci-required` before the squash merge), Metadata read. Each job mints a token with only the permissions it uses, and the plan reads the tap with its own contents-read token.
>
> Independent adversarial review is required; cross-provider review is SKIPPED because no OpenAI reviewer is available (owner update 2026-11-02) and is not CLEAN. See the [unattended release guide](../release/online-signing.md#unattended-security-patch-releases).

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
