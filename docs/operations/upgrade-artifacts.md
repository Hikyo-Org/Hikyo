# Verified upgrade artifacts

The [locked compatibility ADR](../adr/signed-upgrade-compatibility.md) defines
release trust and execution obligations. This foundation verifies release
evidence and computes a route. It does not stop a server, authorize a database
migration, consume an operator nonce, or enable the retired remote updater.

The integrated runtime gate consumes this public evidence at every production
boot and migration. See [manual deployment](manual-upgrades.md) for the required
offline directory layout, persistent operator custody and deployment mounts.
The release artifact directory must be assembled into that bounded gate layout;
the existing download bundle is not interchangeable with it.

## Source build declaration

`upgrade-compatibility.json` contains the target profile, canonical version,
release sequence and source commit. Each engine binds every ordered migration
version and its exact SQL SHA-256, the canonical resulting domain catalog
SHA-256, and explicitly reviewed source identities, catalogs and inventories.
The catalog uses the same inspector as database admission. Independently
validated gate control tables are excluded; unknown extra objects remain drift.

`release/compatibility/sources.json` is the reviewed source declaration. Its
initial edges name only the explicit empty and pinned pre-ledger genesis states.
It does not invent a previous release or infer compatibility from SemVer.

### Release-source edges

An installed stable release is recorded under its release identity, and the
planner routes it only through an edge whose `source.release` is that exact
identity. The `fresh/v1` and `legacy/v1` genesis edges never match it. The edge
lives in the declaration of the release that is the **target** of the upgrade,
and that declaration is signed with the target release. A shipped release
therefore cannot gain an edge afterwards: v1.1.0 declares none, so an installed
v1.0.0 has no ordinary route to v1.1.0 (`no authenticated upgrade route`,
[#1422](https://github.com/Hikyo-Org/Hikyo/issues/1422)). Three things can
route a stranded release. None is automatic, and each needs an owner decision:

- **Nothing.** The installation stays on its release until it is restored into
  a newer one.
- **A recovery-signed bridge** (`hikyo.dev/recovery-bridge/v1`) per engine from
  the stranded release to its successor, authorized by a new offline-signed
  catalog and passed to `assemble-upgrade --bridge`. It needs the recovery-key
  ceremony and operator attestation at upgrade time. See
  [bridges](../../release/trust/BRIDGES.md), which documents only legacy bridges.
- **An extra edge in a later release.** That release's `sources.json` also
  names the stranded release. The planner accepts it when both releases were
  signed under the same policy (v1.0.0 and v1.1.0 both use
  `github-actions-stable`, so the policy skip in `plan.go` does not apply). The
  `--edges-only` check ignores it. The full check (without `--edges-only`)
  removes only the baseline's edge before comparing, so it reports the extra
  edge as a difference for a maintainer to confirm. The patch planner
  (`security-patch-plan.sh`) refuses a patch whose `sources.json` gains more than
  the baseline edge, so such a release must be a minor one.

Every stable release after v1.0.0 must carry, per engine, one edge from the
highest earlier stable release (the baseline). Author it by hand, in
`release/compatibility/sources.json`, before tagging:

1. Download the baseline's release assets and verify them with
   `go run ./scripts/release/stable verify --trust release/trust --directory DIR --version X.Y.Z --commit COMMIT --historical --published`,
   where `COMMIT` is `.source_commit` in the baseline's `release-manifest.json`.
   It prints the `identity` object.
2. For each engine add `{"source": {"release": IDENTITY}, "migrations": M,
   "schema_sha256": S, "mode": "maintenance"}`. `IDENTITY` is the printed object
   verbatim; `M` and `S` are that engine's `migrations` and `schema_sha256` from
   the baseline's own `upgrade-compatibility.json`. Use `restart` only when the
   target's migrations and schema digest equal the baseline's.
3. Run `scripts/release/check-compatibility-unchanged.sh --edges-only
   Hikyo-Org/Hikyo release/trust VERSION release/compatibility/sources.json`.
   It fails when an edge is missing or differs from what the baseline published.
   After the generator runs, the same script without `--edges-only` compares the
   whole declaration with the baseline's.

`scripts/release/nightly sources` drops these stable edges, because a nightly
declaration cannot name a stable source. The generator itself refuses an edge
whose sequence is not below the candidate's.

The tag workflow (`release.yml`) runs no edge check. A stable release built from
a commit that lacks the edge ships without it, so the edge must be on the tagged
commit: either promote `dev` to `main` or merge a same-repository `hotfix/`
branch that adds it. Only the release planner (`security-patch-plan.sh`) checks the
edge before offering a tag.

Build the artifact before building any release binary:

```sh
# This DSN must name a new, empty, isolated PostgreSQL database.
export HIKYO_RELEASE_SCHEMA_POSTGRES_DSN='postgres://.../release_schema_scratch'
go run ./scripts/release/compatibility \
  --candidate /path/to/release-candidate.json \
  --out /path/to/upgrade-compatibility.json
```

The tool owns a temporary SQLite database and applies the actual embedded
migrations on both engines. It refuses a nonempty PostgreSQL database. It leaves
the supplied scratch PostgreSQL database for inspection; the build runner owns
its cleanup. This is a build tool, not a runtime migration entry point.

GoReleaser embeds the exact declaration bytes and SHA-256 through
`HIKYO_COMPATIBILITY_BASE64` and `HIKYO_COMPATIBILITY_SHA256`. The stable release
workflow generates these before building and copies the same bytes into the
release inventory. Manifest creation, recovery binding, signing and verification
require the compatibility artifact. The declaration cannot embed its own digest
or its containing manifest digest. Runtime receives that independently verified
envelope and separately authenticates the platform payload.

`buildcompat.Current` and `buildcompat.Verify` are the production build binding.
The separately named `Development` and `VerifyDevelopment` APIs expose a committed
synthetic `0.0.0+local.dev` declaration for the isolated development trust domain.
Its all-zero commit is a synthetic sentinel, not a Git commit claim. Its only
source is fresh genesis. Production `Current` refuses this reserved version.
Development custody and database-domain isolation remain admission requirements.

Regenerate and check development claims using fresh scratch PostgreSQL databases:

```sh
go run ./scripts/release/compatibility --development \
  --check internal/buildcompat/development.json
# To regenerate, use --out with a new file, review it, then replace the source.
```

CI independently reproduces the exact development bytes from real SQLite and
PostgreSQL catalogs. Changed SQL, missing migrations or catalog drift fail.

## Offline trust snapshot

Installation configuration pins `root.json` and the recovery public key. A
downloaded artifact cannot choose its own root. `VerifySnapshot` authenticates
stable metadata and a `hikyo.dev/upgrade-trust/v1` catalog in one of two shapes.
Legacy snapshots are signed directly by the recovery key. Otherwise the recovery
key delegates through the recovery-signed `stable-policy.json`, and metadata and
catalog signed by the stable release workflow are authenticated through that
delegation. Either way it validates active primary keys. The catalog binds the
exact current metadata digest, authorized nightly policy digests and recovery
bridge digests. Missing, withdrawn or substituted evidence fails closed.

Generate a review artifact after updating the stable metadata:

```sh
scripts/release/create-upgrade-catalog.sh metadata.json 2 \
  reviewed-policies reviewed-bridges upgrade-catalog.json
```

The policy and bridge directories contain only reviewed JSON statements and
optional corresponding `.sigstore.json` signatures. Catalog creation does not
authorize its output. While the stable metadata is recovery-signed, sign the
exact catalog during the existing offline recovery-key ceremony using the same
key-based Cosign profile as trust metadata:

```sh
cosign sign-blob --yes --new-bundle-format=false --tlog-upload=false \
  --use-signing-config=false --key /offline/recovery.key \
  --bundle upgrade-catalog.sigstore.json upgrade-catalog.json
```

Once the stable workflow has signed the metadata, a recovery-signed catalog over it
is refused. Follow
[Re-authorize trust over workflow-signed metadata](signed-nightlies.md#re-authorize-trust-over-workflow-signed-metadata)
instead.

Distribute the catalog and signature alongside the current metadata and keys.
No key or catalog was created for production by this implementation. The
installation must persist the verified metadata/catalog sequence and digest
floors, highest stable release sequence, and externally pinned root/domain
binding atomically with admission. Lower counters and different bytes at an
already accepted counter refuse. A snapshot has no mutable authority accessors.

`VerifyStable` accepts a currently authorized historical stable release for
planning. `RequireLatestStable` is an explicit separate installer policy. The
existing manual installer retains its mandatory latest selection and persisted
anti-rollback checks. A previously verified cached release cannot be reused with
a different current snapshot; it must be reverified under current revocations.

## Nightly profile

`VerifyNightly` uses maintained Sigstore libraries inside the binary. It requires
a recovery-authorized exact policy, pinned offline Fulcio/Rekor trust material,
exact GitHub repository and owner numeric IDs and URIs, issuer, workflow, main
ref, source/build commit and runner environment. The policy also pins the Rekor
log ID and checkpoint origin. Certificate transparency can be required by the
explicit recovery-authorized policy; production policy should require SCTs.

Exactly one Rekor entry must include both its signed integrated timestamp and
its inclusion proof/checkpoint. The maintained verifier checks signatures,
certificate chain and validity at signed time, artifact binding, Merkle proof
and checkpoint signature. Additional structural checks require the same entry
index, tree size, pinned log and checkpoint origin. Local wall time never
substitutes for signed-time evidence. TSA-only Rekor v2 evidence does not satisfy
this profile's signed integrated-time contract.

The signed manifest contains the complete release inventory, including exact
platforms and artifact digests. Its signature and identity are never rewritten
for a particular host. Only the fixed enclosing `release-manifest.json` and
`release-manifest.sigstore.json` pair is outside that inventory.

The automatic binary updater authenticates this manifest before downloading
payloads. It selects exactly one archive for the host OS and architecture, plus
shared or matching compatibility, policy, trust-root, provenance, checksum,
release-note and SBOM metadata. Linux amd64 LXC installations therefore fetch the
Linux amd64 archive, without Windows, macOS, ARM or OS-package downloads. Debian,
RPM, APK and Arch packages are not used by the binary updater.

`VerifyNightlyManifest` authenticates release identity and inventory without
claiming payload verification. `VerifyNightly` then requires and hashes exactly
the chosen inventory. Missing native archives, ambiguous native archives, extra
payloads and substituted bytes fail. An empty platform retains complete-release
verification for offline publication tooling.

Platform bundles use `hikyo.dev/offline-upgrade-bundle/v2` and an explicit
`nightly_platform` in `index.json`. The loader still supports complete v1 bundles;
v2 requires a supported platform and its exact signed selection. Private cache
and bundle payloads share storage on the same filesystem, remain immutable, and
are independently reverified. Cache removal does not invalidate a linked bundle.

Historical binaries only understand complete v1 bundles. Before stopping the
service or migrating, the coordinator probes every staged route executable with
`--upgrade-bundle-formats`. A required intermediate release without v2 support
fails with an explicit compatibility error, without downloading foreign payloads.
The initial invocation from an older installed updater still follows that older
binary's download behavior until it hands off to the new release.

Current unsigned nightlies remain unsupported; no fallback constructs a stable
identity. The [signed-nightly runbook](signed-nightlies.md) covers automated
publication, offline public trust authorization and complete-payload assembly.

## Planner and recovery edges

`upgradecompat.Bind` creates a node only from a verified release and its exact
declaration bytes. `PlanRoute` accepts an inspected source, current snapshot,
independently verified target/intermediate nodes, and the complete current
catalog's verified bridge statements. Omitting a statement cannot recover an
ordinary restart edge that the bridge overrides. Unrelated statements do not
require downloading unrelated release payloads. Duplicate bridge pairs refuse.
It checks
all supplied proof and graph bounds, even for a same-release restart. Limits are
256 release identities, 1,024 edges across both engines and 32 route hops.
Unknown schemas, fields, engines, modes, inconsistent identities and descending
edges refuse. It chooses the fewest hops and ascending release-sequence ties.

An ordinary edge cannot depart from a withdrawn or unauthenticated source.
A recovery-root-signed bridge can authorize that exact installed source's exit,
including its schema, SQL inventory and policy digest, while requiring a normally
authenticated target. It overrides an ordinary edge for the same pair and
requires maintenance. The returned bridge digest and
`RequiresOperatorAttestation` obligation do not supply the second proof: the
instance attestation, backup evidence, current operator pin and atomic one-use
nonce consumption remain required at admission.

`hikyo.dev/legacy-nightly-bridge/v1` is the explicit first-hop form for an unsigned
pre-ledger installation. It names `source_genesis: "legacy/v1"`, exact source
schema/migrations, and an independently authenticated nightly target and policy.
It carries no invented `source` release or `source_policy_sha256`. Only recovery
signatures and the current catalog authorize it. An ordinary nightly legacy
edge is an inspection candidate and cannot create an executable route. Fresh
installs and existing stable-profile rules are unchanged.

Plans expose defensive copies. Their digest binds the exact route, schemas,
migrations, inventory, policies and bridge statements. Unrelated later metadata
updates do not alter that route digest, but application must reverify all proof
against its current snapshot immediately before use.


## Immutable production trust stamp

The build stamps the same `HIKYO_UPDATE_TRUST_ROOT` and `HIKYO_UPDATE_RECOVERY_KEY` base64 values into both the existing installer variables and the server admission binding: `github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedTrustRoot` and `github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedRecoveryPublicKey`. `buildcompat.ProductionTrust()` refuses unstamped, oversized, noncanonical, structurally invalid or mismatched public trust. Runtime configuration and downloaded bundle roots cannot override this build anchor. An explicitly customized self-host build can stamp its own root at build time. These values are public trust material, never private signing custody.
