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

## Recovery bridges (#1422)

**Owner decisions 2026-11-24 on #1422.** Existing v1.0.0 installations have no
ordinary edge to v1.1.0, so the owner chose a recovery-signed bridge per engine
(option B), signed with a rotated `recovery-2` (R2), with signed expiry and an
apply-time revocation list (options (a) and (c) on
[#1422](https://github.com/Hikyo-Org/Hikyo/issues/1422)). Custody and review:

- **R2:** a new offline or hardware key `recovery-2` is rotated in first and a
  release that pins it ships. The bridge target is that release, and only
  `recovery-2` signs the bridge. The `recovery-1` exception is not extended to
  bridges, and the verifier enforces it: a build whose root pins `recovery-1` refuses
  every routable bridge (`recovery-bridge/v2` and `legacy-nightly-bridge/v2`); the inert
  expiry-free `legacy-nightly-bridge/v1` statements still load, always expired. The owner holds the key alone and every signing is witnessed and logged
  ([ceremony runbook](../operations/recovery-bridge-ceremony.md), #1560).
  Solo custody ends before 1.2: signing (the key holder, offline) and publishing
  the trust asset and merging the trust pull request (a second person without the
  key) must be separate people by then.
- An independent verifier (`scripts/release/verify-recovery-bridge.sh`, jq,
  cosign and sha256 only, no shared code with the authoring tool) re-derives every
  statement field from the published declarations and compares bytes and digests.
  It is the merge gate for the trust pull request.
- **Signed expiry (a).** A bridge statement is
  `hikyo.dev/recovery-bridge/v2` with a required canonical-UTC `not_after`, at most
  30 days after signing. The gate compares it with the host clock at plan time,
  offline and without a grace period. An expired bridge drops out of route
  planning, not out of snapshot loading, so ordinary routes and the complete
  bridge inventory check are unaffected. The runtime never routes an
  expiry-free statement: the recovery v1 form is refused, and the legacy nightly bridge v1
  form verifies as an always-expired bridge (see the legacy nightly bridge decision below).
  Admission also rejects a `not_after` more than 30 days plus one hour of clock skew
  ahead of the host clock, so the bound holds at runtime and not only where it is signed. A lapsed bridge is replaced by authoring
  and signing a new statement with `recovery-2` and removing the old digest from
  the catalog in the same step (the re-sign path in the runbook; the verifier
  refuses a new bridge whose engine, source and target pair is still catalogued).
- **Revocation list (c).** Before a plan that uses a bridge is applied, the gate
  fetches the list (schema `hikyo.dev/recovery-revocations/v1`): a sequenced list of revoked
  statement digests, signed by the pinned recovery key and bound to the pinned
  root, so the endpoint is only a transport. Once an installation has stored a list, its persisted
  sequence floor refuses a lower sequence and a different list at the same
  sequence. On an installation that has stored nothing (every v1.0.0 install
  at its first bridge apply) the floor cannot tell a replay, so each list also
  carries a signed `issued_at`: a list older than 7 days is `stale`, never
  `checked`, and falls back to the signed expiry like an unreachable endpoint
  (its entries still apply). The list is signed when a bridge is published and
  whenever something is revoked, never on a schedule: the offline recovery key
  is not exercised weekly because freshness buys no security (an endpoint
  controller can force the unreachable path anyway, and stale entries still
  apply). The residual is an endpoint that replays an older validly signed list
  to such an installation, delaying a newer revocation until `not_after`. The
  verifier refuses a future-dated list and one older than the newest bridge it
  covers. If the endpoint is unreachable (DNS, dial or timeout, HTTP
  502, 503 or 504), the check falls back to the signed expiry and the last
  verified list, and records that locally. A reachable but invalid answer (other
  status, a body cut short, oversized body, bad signature, wrong root, older
  sequence, a redirect, certificate error) fails closed. A reset or EOF before any response is no
  answer (unreachable) unless it is a TLS or certificate failure. `HIKYO_UPGRADE_REVOCATION_CHECK=off` is the explicit
  operator opt-out, and `HIKYO_UPGRADE_REVOCATION_URL` points at a mirror.
  The default endpoint is the two assets `recovery-revocations.json` and
  `recovery-revocations.sigstore.json` of the fixed `trust` release in
  `Hikyo-Org/Hikyo` (a stable GitHub release download URL). Publishing or
  replacing those assets is part of the owner's signing step; agents never
  publish them. GitHub serves release assets by redirect, so the fetcher follows
  at most two redirects, each to https on `github.com`,
  `objects.githubusercontent.com` or `release-assets.githubusercontent.com`; any
  other redirect is a reachable but invalid answer. A 404 for the list asset
  (nothing published yet) is no answer, like an unreachable endpoint: the route plans, a
  warning is logged, and the signed `not_after` is the limit; a 404 at the end of a
  redirect is judged the same way. A list that is
  present without its signature (a 404 for the `.sigstore.json` asset), badly signed,
  malformed or bound to another root fails closed. Once a list is stored, an absent
  list falls back to the stored list and its sequence floor. The signature, not the
  host, is the authority. A 429 or any other unlisted status stays a reachable but
  invalid answer and fails closed.
  The fetcher honors the proxy environment (`HTTPS_PROXY`, `NO_PROXY`): unlike the
  fleet clients, which use only an explicit proxy setting, it sends no credentials,
  a proxy-only host would otherwise see the endpoint as unreachable and fall back
  to `not_after`, and whatever a proxy returns is still verified against the pinned
  recovery key.
  **Stored state.** The accepted list and its signature are stored as one file
  (`bridge-revocations-store.json`, written by fsync, one rename and a directory
  fsync), so a crash cannot leave a list beside
  another list's signature. Losing known
  revocations must never widen admission, so stored state that exists but does not
  verify (a damaged file, a list signed by another
  key) is unusable: bridge routes refuse under `off`, when the endpoint is unreachable
  and when the fetched list is stale, with a message naming the files. Only a fresh,
  non-stale, verified online list replaces it, and its sequence must be at or above
  the high-water floor (`bridge-revocations-floor.json`: the highest sequence and its
  digest, raised only when a list is stored, under the lock, never lowered and kept apart
  from the outcome log; a different list at the floor's sequence is equivocation). A
  host with no floor record is on its first run, and an unreadable record refuses. A list that is
  correctly signed by the current recovery key but bound to another (older) root was never
  issued for this root, so it is unusable too (neither its revocations nor their absence can
  be applied) until a fresh list for this root replaces it. The floor is bound to the trust
  root and enforced whenever it exists: a removed store, a store behind the floor (a crash
  between the two writes) and a list that differs from the floor at its sequence are
  unusable. The persistence lock is an operating system file lock, not a lock file. The way out is operator
  action, never automatic: reach the endpoint so a valid list replaces it, or stop the
  service and remove the named files (including the floor record), accepting that revocations known only to that
  list are lost. A security rollback is never blocked.
- **Admission time (F3).** A bridge's `not_after` and its revocation limit what
  may be *admitted*, not what an admitted upgrade may finish. Every site that
  holds the journaled route digest of an upgrade (the gate's in-flight and
  recovery resume, the maintenance configuration, the automatic upgrade resume)
  does not repeat the admission's search and compare: a fresh search over a
  graph with other expiry and revocation facts can pick another route than the
  one admitted. It checks that the journaled digest is the digest of some valid
  path in the authenticated graph (ordinary edges and every verified bridge, with
  parallel edges kept), ignoring expiry; an upgrade admitted before `not_after`
  therefore finishes after it. A revoked bridge on that route still refuses a
  forward resume. A security rollback validates the journaled route the same way
  with neither check, so neither expiry nor revocation ever blocks returning to
  the journaled source. Journals carry no timestamp: their formats are strictly
  decoded and an older image must still read them, so the journaled route digest
  is the record of admission. A new admission skips an expired or revoked bridge;
  if a revoked bridge and its re-signed replacement share a pair, the list picks
  the live one (without the list that pair is a catalog error).
  New admissions (backup preparation, bootstrap, export, drill, preflight and
  automatic route discovery) check both.
- **Legacy nightly bridge (owner decision, #1561).** The legacy nightly bridge carries
  a signed `not_after` like every other bridge, at most 30 days after signing, so
  expiry bounds it. Its schema is `hikyo.dev/legacy-nightly-bridge/v2`, which
  requires a canonical UTC `not_after`; the expiry-free `.../v1` form verifies as an
  always-expired bridge, because the catalog and every bundle list it and must load.
  Migration path: the two `v1` statements in the committed catalog stay
  until the owner re-signs `v2` statements offline at the ceremony and replaces
  their digests in a new catalog; until then the runtime plans no legacy-nightly
  route through them. The owner chooses `not_after` when signing
  (`scripts/release/nightly legacy-bridges --not-after`); agents never sign or
  publish one, and repository fixtures use throwaway keys only. With expiry in
  place the revocation check treats a missing list (404) as no answer: the route
  plans with a warning and expiry is the limit. An expired bridge is refused whatever
  the list says, a present but invalid list refuses, and a revoked entry blocks a
  forward resume while a security rollback is never blocked.
  `HIKYO_UPGRADE_REVOCATION_CHECK=off` stays as the explicit operator opt-out; a
  usable stored list still applies. Repository fixtures do not use it: they point
  `HIKYO_UPGRADE_REVOCATION_URL` at an unresolvable host, so the unreachable path is
  what they exercise. The native security maintenance service reads the policy from
  the host's `hikyo.env` (it loads no `EnvironmentFile`), so `off` or a mirror URL
  set there is honored at discovery too.
  The two committed `v1` digests are policy-listed, so they cannot leave the catalog
  without a policy rotation, an owner tidy-up and not a merge blocker; the runtime loads
  `v1` as expired, so they stay inert and cannot admit a route, whatever they once allowed.
  Ordinary upgrades never contact the endpoint. The snapshot revocation
  (`recovery-bridge.sh revoke`, which drops the bridge from the catalog and never
  re-signs the policy) remains and binds instances whose floor advanced.
  A rehearsal with a throwaway key and a test root precedes the real ceremony,
  and no other stable-policy change may be pending.

**Constraints found while building it.**

- Verifiers accept a recovery signature only from the key pinned in the running
  binary's root. The shipped v1.1.0 pins `recovery-1`, so a `recovery-2` bridge
  into it is refused. Hence R2: rotate first, ship the pinning release, and
  bridge into that release.
- Residual limits, accepted by the owner: an instance that never reaches the
  endpoint is bounded by the 30-day expiry, not recalled sooner; the check record
  is local and unsigned; the list endpoint is a GitHub release asset that only the
  owner publishes; revocation does not recall bundles already distributed, but a
  distributed bundle's bridge stops planning at `not_after` and when listed.
- Each statement binds the stable policy digest. Listing the statements in the
  policy changes that digest, so the policy is never re-signed. The bridges are
  authorized by a recovery-shaped metadata and catalog (next sequences, no
  workflow identity). The fixed policy is therefore an input of the statements.
- A workflow-signed catalog must equal the policy's bridge list, so
  `stable prepare` withdraws recovery-only bridges: the route lasts until the next
  stable release. It refuses when the policy lists a bridge the catalog dropped
  (restoring it would undo a revocation), and `revoke` refuses policy-listed
  digests: both need a policy rotation. Until it ships, `install.sh` can report "trust rollback
  refused" for new stable installs.

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

**Pre-tag compatibility check (owner decision 2026-11-15, option B; the human tag ceremony stays).**
`release-compatibility-check` is an optional, read-only, dispatch-only workflow. Before tagging,
a maintainer can ask whether a release of a given `X.Y.Z` built from main's tip leaves upgrade
compatibility unchanged. It generates the candidate's `upgrade-compatibility.json` with the release
generator on scratch PostgreSQL and compares it, minus version, sequence and commit, with
the declaration of the highest plain stable version below it in authenticated trust. Compatibility
counts as unchanged only when the candidate equals that declaration plus exactly one reviewed
release-source edge per engine whose source is the baseline release's identity, carrying the
migrations and schema digest the baseline published (owner decision 2026-11-16): the upgrade
planner follows an edge only when its source matches the installed release's identity, and
genesis edges never do, so without it an installed release has no route to the next, for a patch as for a minor.
No supported command authors a stable edge; it is written by hand and reviewed. That baseline is
fetched and verified through `stable verify --historical --published` (as the public verifier
does: an older release embeds older trust) and its declaration is bound to the signed
manifest; neither GitHub's "latest" flag nor a prerelease decides. A baseline not signed under the
`github-actions-stable` policy is refused outright: the planner skips an edge across a trust-policy
change, so only a recovery bridge can route an upgrade from it. A difference, a missing or
unverifiable baseline, or a malformed declaration is reported and left to a maintainer. The check
creates no tag, release or trust change and is not part of the build; `release-build` and the
signing and publication path are unchanged. Fully unattended tagging stays deferred to a later App phase.
The workflow lands on `dev` and reaches `main` by promotion; a dispatch-only workflow runs only from the
default branch, so it goes live then.

**Security patch tag prompt (owner decision 2026-11-15, option B; the human tag ceremony stays).**
`security-patch-plan` runs on every push to main and hourly. When a merged Dependabot security
update (an alert-linked pull request authored by Dependabot, bound by numeric id) is missing from
the latest stable release, it picks the target, checks what the ceremony would check, and
writes one issue with exact commands for the signed tag and the existing release ceremony. The target
is the newest of the last 60 first-parent commits of main that holds every pending security merge,
records the latest stable release in trust, and has complete green source proof; a red tip falls back
to the last green commit, never below the newest pending merge. The workflow tags nothing, publishes
nothing and holds no tag or release authority: its plan job is read-only, only its second job writes
the one issue, which it recognizes by title, marker and its own bot as author (numeric id and type),
and that job also runs after a failed or timed-out plan to say so in the issue. A workflow GitHub rejects
at parse time runs no job, so that case is repaired only by the next successful run.
The issue's command starts with `security-patch-plan.sh recheck`, run by the maintainer with their own
login, which refuses when the checkout is dirty, when a newer pending security merge exists, or when the
version, the trust record, the tag checks, the fallback-channel gate or the release-source edge no
longer hold. It does not close the window between the recheck and the tag push, and it does not
regenerate the compatibility declaration; it guards stale instructions, it is not a lock. For a patch
as well as a minor, the plan requires the reviewed release-source edge from the latest stable release in
`release/compatibility/sources.json` (validated against that release's verified declaration), because
an installed release is routed only through an edge whose source is its exact identity. No supported
command authors that edge yet (a tooling gap); the issue says so. Fully unattended tagging stays deferred to a later
App phase. The workflow lands on `dev` and reaches `main` by promotion; its triggers run only from
`main`, so it goes live then.

**No auto-merge (owner decision 2026-11-16).** The workflow token cannot add a pull request to the merge
queue, so security updates are never merged by automation. Step 0 of the issue lists every open
qualifying Dependabot security pull request into main with its head SHA, and a maintainer clicks "Merge
when ready"; one the merge queue removed after its newest commit is marked as needing a person. This
needs only read permissions (`pull-requests: read`, `checks: read`) and replaces the separate
auto-merge workflow proposal (#1404, closed).

**A minor release when compatibility blocks a patch (owner decision 2026-11-16).** Once a promotion
brings unreleased migrations, the compatibility check refuses every patch. The issue then proposes the
next minor `X.(Y+1).0` (a minor ships everything on its commit, not only the security fix) and lists the
prerequisites a person must confirm before tagging: the declaration difference is only the unreleased
migrations, the edge modes follow the compatibility ADR, everything since the latest stable release may
ship, and the release notes are written. `recheck` verifies the kind it was given against the current
state.

**Homebrew stays manual (owner decision 2026-11-16).** `ceremony.sh homebrew VERSION` remains the last
step of the release issue and no tap credential is added. This replaces, for now, the earlier decision
that Homebrew follows the latest release automatically.

Operational entry point: [online signing](../release/online-signing.md).
Primary-source evidence: [release ceremony research](../research/release-signing-ceremonies.md).
