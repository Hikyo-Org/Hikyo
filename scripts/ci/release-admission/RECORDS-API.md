# Trusted release records API

This module is source-only. It grants no execution, source adoption, provider or
resource authority. All dictionaries are closed records. Validation raises
`ValueError`; external authority is unavailable by default.

## Imports and bounded helpers

Start a fresh interpreter with this package on its import path. The trusted Root
bootstrap must first load `load_successor` and supply independently reviewed
raw map/manifest pins, before importing any public helpers or their consumers:

```python
from load_successor import bootstrap_helpers, load_unconfigured

# Supplied externally by the trusted Root bootstrap after independent review.
pins = dict(reviewed_map_sha256=reviewed_map_sha256,
            reviewed_manifest_raw_sha256=reviewed_manifest_raw_sha256)
helpers = bootstrap_helpers(**pins)
from release_records import Observation, ReleaseAdmission
modules = load_unconfigured(**pins)
```

`bootstrap_helpers` verifies the complete closure, then compiles the five shared
helpers from exact snapshots in dependency order. `load_unconfigured` also
bootstraps helpers before loading separate unconfigured model instances. Repeat
calls retain helper/dataclass identities only for the loader-established objects,
same package root and exact helper closure. Ordinary pre-imports are unproven,
even with the real source filename or an apparently matching bytecode header.
Removed/replaced registered modules and changed helper snapshots also refuse.
The loader does not silently replace cached objects and leave old references
behind. Restart the interpreter and bootstrap with the correct review pins;
clearing `sys.modules`, reloading helpers or deleting bytecode caches is not a
supported recovery. Trust in the root loader/bootstrap remains explicit; helper
attributes cannot self-authenticate provenance. This establishes source execution
integrity only, never source adoption, owner approval or external authenticity.
The finite test runner and pipeline fixtures compute local integrity pins for
source checks only, before importing these APIs. They supply no Root review.

`bounded.py` exports `canonical(value) -> bytes`, `digest(value) -> str`,
`strict_json(raw: bytes)`, `require(condition, label)`, `exact(value, fields)`,
`sha(value, length=64) -> bool`, `instant(value) -> datetime`,
`fresh(value, now)`, `clock(now)` and `stamp(now)`. Canonical JSON retains
ASCII escaping, sorted keys, compact separators, 2,000,000 bytes, depth 40 and
2,048 list items. Exact integers exclude booleans. UTC clocks are explicit.

`release_policy.py` exports immutable `POLICY_SHA256`, `TRUSTED_FILES`,
`CANDIDATE_CI_FILES`, `REQUIRED_CI_JOBS`, `FROZEN`, `REPOSITORY`, `OWNER_ACTOR`,
`ACCEPTANCE_SERIES`, `HORIZON`, `EVIDENCE_MODE` and `SCHEMAS`. The semantic
policy digest includes the pinned nine contracts, constants and helper pins;
imports are inert and perform no filesystem I/O. `DESIGN_RAW_PINS` exposes the
three reviewed raw hashes, and `design_originals_checked(originals)` explicitly
checks loader-supplied whole bytes against them and the embedded contracts. The
parent loader verifies the static specification closure before imports.

## Configuration and record store

`ReleaseAdmission(configuration, records, *, implementation, historical_history,
authority=None)` copies inputs. `records` is a content-addressed dictionary
from canonical SHA256 to whole record objects. `configuration` is the exact
`releaseConfiguration` contract. `implementation` is a closed static manifest:
`{schema: "hikyo-158/release-implementation/v1", files: {relativePath: SHA256}}`.
It must include `core/next_fixture_model.source-only.py` and
`core/test_next_fixture_model.py`; runtime configuration/approval/adoption/status
files are excluded. Static JSON is limited to the three spec JSON files, the
two root origin/history pin indexes, and the explicitly named adapter
source, descriptor and registry indexes in `_STATIC_JSON_PATHS`.
CJS is permitted under `adapters/` or `core/`.
No runtime JSON is accepted. `historical_history` is the whole immutable historical
record supplied by the parent; its authenticity and original G validation
remain required external checks. No directory or module is loaded from data.

Properties return copies: `configuration`, `resource_configuration`,
`release_vector`. `configuration_checked()` returns the checked composite.
`source_manifest_checked(manifest)` validates exact v4 manifest and static pins.
`attempt_binding_checked(binding, manifest)` validates exact binding and all
composite constituents. Pure structural validation does not authenticate data.

## External boundary

Implement `Authority.verify_record(kind, record, records, at) -> Verification`.
It MUST authenticate complete genuine originals, exact raw bytes/projections,
truthful native decision chronology and all evidence predicates for `kind`.
For release/source evidence it MUST run the real collector and original
historical checker, including complete REST/GraphQL, pagination, jobs,
signature/DCO, source-role, lineage, final-readback and review checks. A digest,
boolean, cached claim or synthetic native wrapper is insufficient.

`Verification(purpose, subject, originals, verified_at, decision_sequence=None)`
contains the exact complete subject and nonempty tuple of
`Original(raw: bytes, projection: dict)`. The core checks shape/byte bounds and
exact subject correspondence. Each original must be complete, at most 2M bytes;
the verification aggregate is at most 24MB. Returning this object is an explicit
trusted boundary, never a way for caller-supplied records to self-authenticate.
No production implementation is provided here. Default `UnavailableAuthority`
raises on every method. Test support supplies only an external authority double.

Implement `Authority.observe(release_sha256, at, *, historical=False)
-> Observation` with an authenticated complete native stream from sequence 1,
including unrelated entries, exact terminal sequence, immutable scope identity,
current persistent per-release head and persistent monotonic watermark. It MUST
perform whole-byte native correspondence (exactly one match, LIMIT 2), verify
truthful decision projections and refuse unavailable/gapped/ambiguous state.
`Observation(scope, observed_at, terminal_sequence, events, head)` carries
`NativeEvent(sequence, raw, decision)` entries; decision is `None` for unrelated
native entries or a closed projection `{kind, releaseSHA256, adoptionSHA256,
subjectSHA256, custodySHA256, occurredAt}`. Kinds: `owner-approval`,
`root-adoption`, `root-hold`, `owner-withdrawal`, `root-recovery`.

`Head(release_sha256, adoption_sha256, status_sha256, revision, watermark)` is
persistent authenticated authority, not a convenience cache. Implement
`Authority.compare_and_swap(expected, proposed, decision_sha256, observation)
-> Head` to atomically check the exact persistent predecessor AND observed
native stream terminal, consume the decision once globally, and durably advance
the per-release head and watermark. A crash must expose either the whole old
state or whole new state. Scope changes, stream movement, reused decisions and
same-predecessor races refuse. Returned head must equal proposed exactly.
Persistent integration belongs to the parent; no in-memory production fallback.

Stream verification is capped at 50,000 native sequences and five seconds.
Core checks contiguous sequences, decision order, all relevant withdrawals,
head/watermark consistency, freshness and terminal coverage. The adapter must
also enforce these bounds during its I/O, before returning an observation.

## Lifecycle and admission

`adopt_now(now)` validates the configured adoption/genesis, current complete
stream, prior generation and recovery/reauthorization, then calls CAS. Initial
adoption consumes the genesis `decisionCustodySHA256` for its root-adoption
decision. Root-only recovery revalidates
unchanged unexpired standing approval but consumes the NEW recovery `rootDecisionCustodySHA256`.
Owner reauthorization consumes its NEW genuinely later owner
`originalDecisionCustodySHA256` and names
the actual withdrawal, affected adoption and prior approval. Both successor
routes require independent remediation review. No in-chain unhold exists.

`transition_now(status_sha256, now)` appends an authenticated Root hold or owner
withdrawal to the current adoption using CAS. `eligible -> held/revoked` and
`held -> revoked` only; revoked has no outgoing transition. Hold reasons are the
four literal policy reasons; withdrawal reason is `owner-withdrawal`; genesis
reason is `release-adopted`. Failed observations never write status.

`admit_now(binding, manifest, now, *, reserved_binding=None)` validates immutable
configuration/manifest/binding, authenticates release/source evidence and checks
fresh authoritative eligibility. If `reserved_binding` is supplied, it must be
byte-identical to binding. Returns a non-authoritative checked summary.

`reconstruct_original_for_cleanup(binding, manifest, action_at, now)` validates
the same immutable records and externally authenticated historical stream/head
at the real action time, without requiring current main or current eligibility.
Returns a cleanup-only summary. It does not perform current exact-resource
owner/MFA/quiescence/survivor checks; parent cleanup MUST still require them.
Historical reconstruction never consumes decisions or mutates authority.

The collector can call `verify_external(kind, record, at)` for a required full
verified-record result. Candidate/main/source evidence remains required via
`verify_record("attemptTrustBinding", binding, ...)` on every admission; a
matching source manifest never substitutes for that evidence.
