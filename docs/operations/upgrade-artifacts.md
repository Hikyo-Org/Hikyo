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
stable metadata with that key, validates active primary keys, and verifies an
additional recovery-signed `hikyo.dev/upgrade-trust/v1` catalog. The catalog binds
the exact current metadata digest, authorized nightly policy digests and recovery
bridge digests. Missing, withdrawn or substituted evidence fails closed.

Generate a review artifact after updating the stable metadata:

```sh
scripts/release/create-upgrade-catalog.sh metadata.json 2 \
  reviewed-policies reviewed-bridges upgrade-catalog.json
```

The policy and bridge directories contain only reviewed JSON statements and
optional corresponding `.sigstore.json` signatures. Catalog creation does not
authorize its output. During the existing offline recovery-key ceremony, sign
the exact catalog using the same key-based Cosign profile as trust metadata:

```sh
cosign sign-blob --yes --new-bundle-format=false --tlog-upload=false \
  --use-signing-config=false --key /offline/recovery.key \
  --bundle upgrade-catalog.sigstore.json upgrade-catalog.json
```

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

The closed inventory contains every payload asset, including executables with
exact platforms, compatibility, provenance, checksums, other metadata and
applicable exact OCI references/digests. Only the fixed enclosing
`release-manifest.json` and `release-manifest.sigstore.json` pair is outside its
own inventory. All actual inventory readers are hashed before a nightly release
is returned. Extra, missing, duplicate, unknown or substituted assets refuse.
Verified staging bytes must remain immutable until execution. Current unsigned
nightlies remain unsupported; no fallback constructs a stable identity.

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

Plans expose defensive copies. Their digest binds the exact route, schemas,
migrations, inventory, policies and bridge statements. Unrelated later metadata
updates do not alter that route digest, but application must reverify all proof
against its current snapshot immediately before use.


## Immutable production trust stamp

The build stamps the same `HIKYO_UPDATE_TRUST_ROOT` and `HIKYO_UPDATE_RECOVERY_KEY` base64 values into both the existing installer variables and the server admission binding: `github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedTrustRoot` and `github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedRecoveryPublicKey`. `buildcompat.ProductionTrust()` refuses unstamped, oversized, noncanonical, structurally invalid or mismatched public trust. Runtime configuration and downloaded bundle roots cannot override this build anchor. An explicitly customized self-host build can stamp its own root at build time. These values are public trust material, never private signing custody.
