# #659 verified upgrade foundation handoff

Worktree `/tmp/hikyo-f1-659`, branch `codex/659-verified-upgrade-foundation`, base/governance merge `373910cca63eb8a2e2e6b5a079cc285b64a2ae95`. Parent owns independent review, signed commit, push, PR and merge. No commit or push made. Canonical issue #659 and `docs/adr/signed-upgrade-compatibility.md`.

## Ownership and integration

F1 owns the new `releaseidentity`, `releasetrust`, `upgradecompat`, and `buildcompat` packages, selfupdate trust extraction, app/releasecompat build wiring and its test, scripts/release/compatibility, release compatibility source declarations, release workflow/script changes, reports and this handoff. There are 49 owned files at this checkpoint.

`internal/store/upgrade/`, `internal/crypto/initialize_fresh.go` and `internal/boundary/boundary_test.go` are copied F2 dependencies solely for local compilation/verification. EXCLUDE these from the F1 patch. Parent must integrate F2's current canonical copies. The actual-schema build generator imports F2 through app, so either deliver foundations together or stage the leaf/core before the generator. Latest copied F2 includes PinnedLegacySchemaDigest, explicit upgrade/recovery OperationKind and candidate initialization. Exact39 copied file hashes are recorded in `/tmp/hikyo-659-f2-compile-dependencies.txt`; do not substitute earlier copies. Core/build/dev/boundary tests and the CI-mode actual both-engine generator passed against this refreshed copy (generator1.864s).

## Implemented contract

- Release-wide identity with separately verified platform artifacts; exact fresh/v1 and legacy/v1 Source union; full ordered engine-specific SQL-byte manifests. Stable trust permits existing offline-signed alpha/RC releases; nightly.* is reserved to NightlyV1. Synthetic development0.0.0+local.dev is separate and production build binding refuses it.
- Maintained in-process Sigstore key verifier replaces the existing manual installer algorithms. OperatorKeyID uses canonical PKIX DER. Installer latest selection, stable metadata semantics and persistent anti-rollback behavior remain intact. VerifyStable historical authorization and RequireLatestStable are separate APIs.
- Current recovery-signed snapshot/catalog, authenticated stable/nightly releases and root bridge statements, private verified state, defensive copies, monotonic/equivocation floors. Catalog authorizes exact stable metadata/policies/bridge statement digests; installation root/domain pin remains external. Snapshot.BridgeDigests returns current inventory for loaders.
- Nightly verification uses sigstore-go v1.3.0 with exact offline Fulcio/Rekor root, repository/owner numeric IDs and URIs, OIDC issuer/workflow/main ref/build commit, runner environment, certificate valid at signed time, one-entry signed integrated timestamp plus inclusion proof/checkpoint. Additional structural checks bind pinned checkpoint origin/log ID/tree size/index. require_sct is an explicit non-null policy boolean; synthetic private-CA fixtures choose false, and tests prove omitted choice/required-but-missing SCT refuse. No unsigned nightly fallback or production policy was created.
- Nightly inventory consumes every actual payload reader and checks exact digest, kind/platform and applicable OCI identity. Fixed manifest/signature pair is outside its own inventory. Stable verified inventory requires staging callers to VerifyArtifact for each actual platform payload. Plans are not assertions that mutable paths remain available.
- Closed declarations bind full migration inventories AND F2 canonical domain catalog digests for all source/target engines, including release sources. Bridges bind both schema/migration sides and policy digests. Planner enforces256 releases/1024 edges across both engines/32 hops, ascending sequence, fewest hops/ascending tie, same-release exact identity/schema/migrations and current snapshot proofs. It requires the complete authorized bridge statement inventory: omitting a bridge cannot recover an overridden ordinary restart edge. Duplicate logical bridge pairs refuse. Unrelated statements do not require unrelated payload downloads; every traversed target remains independently authenticated. RequiresOperatorAttestation exposes exceptional-edge obligation but never satisfies it.
- Build generator runs actual embedded migrations on temporary SQLite and caller-owned EMPTY scratch PostgreSQL, refusing existing schemas and changed historical SQL. It uses F2's canonical inspector and pinned legacy declaration, no copied catalog algorithm. Source-owned development declaration regenerates byte-for-byte. Production workflow generates compatibility before GoReleaser, links its exact bytes/digest, copies the same file into the manifest, and requires it in creation/recovery binding/signing/verifying. Existing unsigned snapshot fixture is explicitly synthetic and grants no runtime nightly profile.

## Public integration seams

`releasetrust.VerifySnapshot(PinnedTrust, SnapshotMaterial, SnapshotFloor)`; `Snapshot.Floor()` and `BridgeDigests()`; `VerifyStable`, `VerifyNightly`, `VerifyBridge`; `VerifiedRelease.VerifyArtifact`; `OperatorKeyID` and `VerifyOperatorSignature`.

`upgradecompat.Bind(VerifiedRelease, rawDeclaration)`; `VerifiedNode.Manifest`, `SchemaDigest`, `GenesisSources`; `PlanRoute(snapshot, actualInstalledSource, target, nodes, bridges)`. Plan exposes Source, SourceManifest, SourceSchemaDigest, Target, Steps, Digest, SnapshotDigest, BridgeDigests, RequiresOperatorAttestation.

`buildcompat.ProductionTrust() (releasetrust.PinnedTrust,error)` returns only the immutable build-stamped root/key after bounded canonical base64, closed root schema, root/key digest validation and maintained public-key parsing. Private linker names are `github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedTrustRoot` and `.encodedRecoveryPublicKey`, sourced from existing `HIKYO_UPDATE_TRUST_ROOT` and `HIKYO_UPDATE_RECOVERY_KEY`; existing `main.updateTrustRoot`/`main.updateRecoveryKey` installer stamps remain. Runtime configuration cannot replace these values. Custom builds can explicitly stamp their own trust at build time.

`buildcompat.Current() ([]byte, Declaration,error)` and `Verify(node)` are production bindings. Private linker variables `github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedDeclaration` and `.declarationSHA256` have no runtime setter. `Development()` / `VerifyDevelopment(node)` are explicit isolated-domain build claims; parent owns random development custody and runtime domain enforcement. Shared real signed fixtures are `releasetrust/testfixture.New`, AddStable, Snapshot, AddBridge and Nightly. They create actual maintained-library signatures, never fabricated verified objects.

## Verification evidence

- Scoped race suite69 tests passed before two additional SCT negative cases; final bridge-fix race slice58 tests/3 packages passed. Latest nightly-only race suite passed all 23 tests, including both additional SCT negatives.
- Boundary25 tests passed using F2's exact updated allowlist, with no F1 exception.
- Buildcompat/releaseidentity/selfupdate/boundary37 tests passed after bridge fix.
- Actual both-engine app generator test passed twice, checks exact committed development bytes and nonempty PostgreSQL refusal without catalog mutation. Isolated local scratch generation/check also passed. Every engine has43 SQL files, highest numbered44 with gap. SQLite target catalog6e68554a3faf93566d806781648ddf71d2310246e9644af51ee91c165ba5d2fa; PostgreSQLf9ad9908d1d1d5bb47c129eb0508458cf73a5940b2a02a7e60aa929315e222f0.
- Real Cosign3.1.3 downloaded official release and checksum verified5cf948c2f4dfe59687bdd0b8523709067383e03982cc543475c8a7dc70e92a76. Actual Cosign bundle passed in-process verifier and changed bytes refused. No production keys used. Tool `/tmp/hikyo-f1-tools/cosign-darwin-arm64`.
- Existing full signed release trust/refusal fixture suite passed; final rerun after additional mandatory binding checks passed at `/tmp/hikyo-f1-release-fixtures-final.log`. Manifest fixture passed; binding fixture proves mandatory exact compatibility and tamper/missing refusal; catalog fixture proves exact inventory/no overwrite/dangling-symlink refusal; existing ceremony fixture and release binary reuse fixture passed.
- 432 API/server/definitions/CI consumer tests passed, `/tmp/hikyo-f1-consumer-tests.log`.
- Linux/arm64 and Windows/amd64 core trust/planner/build-binding compilation passed; no external helper runtime dependency.
- `go vet` scoped packages, ShellCheck all modified shell files, actionlint both workflows and `git diff --check` passed.
- Source govulncheck on releasetrust/upgradecompat/buildcompat reports0 reachable vulnerabilities. It also reports2 imported-package and3 module vulnerabilities as unreachable; do not describe all dependencies as vulnerability-free. Evidence `/tmp/hikyo-f1-vulnerabilities.log`.
- `go mod tidy` complete. Maintained Sigstore dependencies raise go-openapi/jsonpointer/jsonreference to1.0.0 and related swag/transitive modules; compilation plus432 consumer tests passed. Final parent integration should run its ordinary full gates/generated checks.

## Independent R1 corrections

The independent reviewer found the release workflow's `go test ./...` had no PostgreSQL test DSN under CI, so the new actual-schema acceptance failed before generation. The test step now creates its own `hikyo_release_tests` database through the service container and sets `HIKYO_TEST_POSTGRES_DSN`. It preserves the separate required-empty `hikyo_release_schema`. The exact `CI=true` generator test passed on a dedicated PostgreSQL18 fixture in1.594s; the generation database public catalog remained empty (count0). Workflow actionlint passed. Independent R2 CLEAN: reviewer replayed the CI-mode actual both-engine generator test successfully in2.113s. No gate was skipped.

## Remaining delivery work

Independent parent review and any valid findings, final exact-head full integration checks, signed commit/PR/green merge remain parent-owned. Foundation completion does not enable the retired updater or satisfy F5 production migration admission. No live signing ceremony, production database mutation, external deployment or production trust artifact was performed.

## Parent integration,5 September2026

Combined F1/F2 branch starts at a3ea2f126b4961e72b05e8ef546f892f3d3b291d. Parent corrected the generator raw-handle boundary through BuildScratchSchema: actual empty catalog checked under canonical lock, same physical connection for embedded migration SQL, catalogs only. Type-aware build-authority confinement rejects aliased function capture outside app/releasecompat.go. Independent review CLEAN. Parent458 race cases, zero skips, nine packages; go vet passed. Actual both-engine generator9.816s race. Packaging and exact-head CI remain recorded separately in PR delivery evidence.

Parent packaging: full GoReleaser2.17.1 six-target snapshot PASS8m32s; eight native payloads PASS; snapshot classification/OCI archive parity PASS; actual arm64 Docker candidate SPA HTML and hashed JavaScript PASS. Seven scoped release fixture suites PASS with actual Cosign3.1.3. Snapshot version0.0.0-snapshot-a3ea2f12 reflects unsigned working-tree rehearsal, not release identity.
