# Portable outer adapters for issue 1046

These are actual source successors of A/H/N/D/K and the required custody,
reviewed-operator and persistent R2 dependencies. They provide no source
adoption, native authentication, execution permission or provider authority.
The frozen originals are preserved outside this package.

## Load and bind

Call `package_adapters.load_adapters(model, *, admission, attempt_binding,
source_manifest, implementation_manifest_sha256, implementation_manifest,
runtime_root, native_store, node_path, transport_root)`.

The return value contains `context` and the real successor modules:

- A: `prepare_root`, `chrome_first_prepare`, `persistent_admission`,
  `persistent_custody`.
- H: `create_holder`, `runner`, `create_custody`, `runtime_binding`.
- N: `preflight`, `root_preflight`, `stage_custody`.
- D: `driver_data`, `paired_core`, `paired_custody`, `child_transport`,
  `n_runtime`, `root_driver`.
- K: `root_connection`, `k_cleanup_custody`.

`implementation_manifest_sha256` is the independently reviewed raw SHA256 of
`adapters/SOURCE-PINS.json`. `implementation_manifest` is the parent's semantic
static manifest. It must hash to the configured release's implementation
reference, include every actual adapter/core source row unchanged, and include
the raw adapter manifest itself. Loading verifies the complete closure before
imports into a separate package instance. No module file identity is replaced.
The package initializer and every relative Python dependency execute from the
verified byte snapshots through a confined source loader/finder. This also
covers lazy imports. Matching-header foreign bytecode caches are never read;
undeclared relative Python modules refuse before execution. `-B` is not used
as the source-authentication boundary.

The exact model must be the verified successor core module. Context requires
`model.RELEASE_ADMISSION is admission` and exact correspondence with
`model.attempt_binding_checked(source_manifest)`. There is no old-model facade,
legacy loader, pin alias, substituted file location, or release-selected code.

Runtime paths are explicit. `runtime_root` confines retained paired inputs;
`native_store` is the parent's readonly native database; `node_path` identifies
the actual pinned Node executable; `transport_root` is the separately
materialized reviewed R2 source closure. No machine path is a default.

After loading native modules, call
`package_adapters.verify_native_dependencies(adapters, modules)` with roles
`model`, `executor`, `ledger`, `keyNormalizer`, `guardDeadline`. It verifies actual
file identities/bytes and requires the parent-repinned guards to match those
bytes. It mutates no module, function, pin, configuration or file identity.

## Binding through actual consumers

The parent supplies `attemptTrustBindingSHA256` in preparation, claim, state,
owner intent, ticket and forward contract. Adapter closed schemas carry the
same field in source binding, script arguments, command context, holder review,
window/result, native descriptor, paired plan/action/receipt and cleanup context.
The unchanged R2 request envelope carries the field inside script arguments.
K carries it in intent, ticket and required preparation.

Each consumer checks the same configured context before its original checks.
Composite digest comparisons use `model.release_configuration_checked()`;
namespace/folder reads use the original eight-field
`model.configuration_checked()`. H and N call the real
`model.admit_current_release(prepared, now)`. Context forward admission invokes
`admission.admit_now(binding, manifest, now, reserved_binding=binding)`.
Cleanup reconstructs the original reserved binding through
`admission.reconstruct_original_for_cleanup`; the real coordinator still owns
current owner/MFA, resource, quiescence, survivor, lease and old-key gates.

Public calls are `chrome_first_prepare.configured_plan/configured_prepare`,
`runner.start/ready/release`, `preflight.descriptor`,
`paired_core.Attempt().run`, `paired_custody.verify_parent_original`, and
`root_connection.reconcile/arm/ticket/accept`. Native stage commands require the
explicit field in their closed arguments. Native installation binds the page
capability to it, and later descriptors cannot switch it.

`prepare_root.make_bundle(modules, resource_inputs, proposal_raw, *,
configuration_raw, configuration_raw_sha256, originals_verifier)` creates A's
original five-part bundle. Resource inputs exclude `proposalMarkdown`.
Pass whole original composite configuration bytes and their independently
reviewed raw pin, actual proposal bytes, and the parent's real
`release_collector.ExternalOriginals` verifier. Original input/budget/key-review
and exclusive publication checks remain. Collection uses the real configured
model and successor collector; it does not reinterpret v2 evidence.
`prepare_root.collection_checked` and `pair_collection.collection_checked`
return a bounded `hikyo-1046/configured-adapter-pair-view/v1` with the binding,
`context.manifest` and the actual compact code returned by
`model.register_pair_collection`. The whole collector envelope is retained
unchanged by the model registry and in the original publication archive. It is
never extended with synthetic members or passed as the nested code packet.

`n_runtime.prepare` accepts explicit configured modules/controller and retained
inputs and returns real preflight/pre-submit closures. The adjacent check reads
the explicit wall clock again after external admission, checks the original
shared window and remaining 60 seconds, and rechecks the existing physical
holder/records without renewal. `root_driver.run` is a
configured callable entry point. A cross-process adopted runtime launcher
remains parent-owned; no standalone configured CLI or runtime adoption is
claimed by the inert paired carrier description.

The original 55-second physical holder, 2-second descriptor age, 60-second
remaining-forward gate, resource caps, permanent once burns, MFA, finite clocks
and old-key protections remain in force.

## Derive, build and seal

`adapters/derive.py --source-root ABSOLUTE_FROZEN_ROOT` verifies `ORIGINS.json`
and deterministically derives reviewable actual successors. Original paths in
that index are portable relative names with raw SHA256 and byte counts.
No HTML, log, report or native transcript is an executable dependency.

`adapters/build_transport.py --source-root ABSOLUTE_FROZEN_ROOT --output
ABSOLUTE_FRESH_EXTERNAL_DIRECTORY` verifies all 148 original transport/package
files, about 14 MB, and creates the local successor registry/source closure.
It never starts Node, an owner, a browser or a provider. The original contract
and official package bytes stay external and do not enter Git.

After the final parent core repin, run `adapters/seal.py --node ABSOLUTE_NODE`.
This builds native stage/cleanup scripts and closed registries from stable
implementation bytes, requires the actual model test file, and seals the full
adapter/core source map. Then reseal the parent's implementation/execution map.
Release/configuration/binding data never enters either generator.

The parent static implementation JSON allowlist must include the exact
additional source artifacts `adapters/SOURCE-PINS.json`,
`adapters/STAGE-SOURCE-PINS.json`, `adapters/DESCRIPTOR-CONTRACT.json`,
`adapters/stage-registry.json`, `adapters/cleanup-registry.json`, and
`adapters/TRANSPORT-ORIGINS.json`. The source template is
`adapters/operator-template.js`.

## Qualification

`test_adapter_context.py` requires explicit `HIKYO_158_ARCHIVE_ROOT` and
`HIKYO_158_NODE_PATH`. Missing archive, map, original or Node fails qualification;
there are no skip paths. The archive supplies the frozen source tree at
`aws-recovery-20261020/banked-series-approval-20261022`. Every selected fixture
and transport read verifies exact original bytes, without redacted substitutes.

Tests use the parent's real `pipeline_support.Pipeline`, verified loader,
collector, configured model, record authority checks and ledger. Real pinned
Node 26.7.0/R2/Zod compiles all 14 registry entries. A real H process owns the
actual FIFO/physical-lock handshake; N descriptors and D driver code execute.
Page/CDP/DOM/XHR/native event boundaries are synthetic, and D's external child
response is explicitly doubled. These tests do not establish a persistent owner,
live native custody, provider result, deployment or adoption.

From the repository root, using the dedicated Python environment:

```sh
HIKYO_158_ARCHIVE_ROOT="${HIKYO_158_ARCHIVE_ROOT:?Select exact retained archive}" \
HIKYO_158_NODE_PATH="${HIKYO_158_NODE_PATH:?Select actual pinned Node}" \
PYTHONPATH=scripts/ci/release-admission \
"${RELEASE_ADMISSION_VENV:?Select dedicated environment}/bin/python" \
  -B -m unittest test_adapter_context -v
```

The broader parent qualification command also runs this module. Qualification
is separate from the portable unit suite and has no green fallback.

`test_adapter_source_loader` runs in the portable unit lane without an archive
or adoption. It exercises the genuine pinned source-package import mechanism
with executable foreign initializer/dependency caches and undeclared imports.

`test_adapter_context.prepare_through_adapters(p, root)` is the explicit
qualification seam for actual A `run_plan/configured_plan` and
`run_prepare/configured_prepare` plus publication. It must run before creating
`pipeline_support.Execution`. The actual published preparation replaces
`p.prepared` and `p.f.prepared`; the same records, owner and historical proof
populate `p.f.records/owner/historical`. `execute_native_create(execution)`
then carries that same model/ledger into actual H and D arm/finish using the
real `n_runtime.prepare` closures. The pipeline worker owns actual operator
registry integration and downstream lifecycle consumers; no alternative proof
adapter is substituted by A. Synthetic native/public/provider originals and
the external child transport remain explicitly doubled boundaries.
