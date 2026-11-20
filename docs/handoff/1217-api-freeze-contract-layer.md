# #1217: refuse the remaining forbidden `/api/v1` changes

## Result

[#1217](https://github.com/Hikyo-Org/Hikyo/issues/1217) listed forbidden changes
that `api.CheckFreeze` let through. The guard is live: `refs/tags/v1.0.0`
exists and `scripts/ci/check-api-freeze.sh` diffs every revision against it.

Enumerating oasdiff blind spots did not converge, so `CheckFreeze` now has a
third, independent source of violations next to the oasdiff allowlist and
`freezeRemovals`: `freezeContract` in `api/freeze_contract.go`. It parses the
raw base and revised documents (not kin-openapi's model, which would silently
drop keywords it does not carry) with kin-openapi's own decoder, so YAML 1.2
scalars such as `off` stay strings. Numbers stay exact (`json.Number`), so
integers beyond float64 precision remain distinct (in YAML up to the uint64
range, beyond which yaml.v3 decodes a float). It walks them with knowledge of
each OpenAPI object kind, and refuses every difference except a short list of permitted
additions and prose edits. Its violation IDs are `hikyo-contract-removed`,
`hikyo-contract-added` and `hikyo-contract-changed`. oasdiff, the allowlist and
`freezeRemovals` are unchanged; no source suppresses another.

## Rules

- Default: a keyword present in the base must be present in the revision with
  a deeply equal value, and a keyword absent from the base stays absent.
  Unknown keywords and every `x-*` extension are strict. This includes the
  `x-hikyo-*` operation contract (`min-revision`, `formula`, `operation`,
  `class`, `artifacts`), which oasdiff ignored: editing it on an existing
  operation is now refused, matching the ADR's rule that the promise covers
  authorization and behaviour.
- Prose is exempt only where it is a keyword: `description`, `summary`,
  `title`, `example`, `examples`, `externalDocs`, `$comment`, the whole `info`
  object, the `components.examples` section and OAuth scope descriptions. A
  property, header, server variable or scope *named* `description` is a name and
  stays protected.
- `deprecated` may become `true`; no other change to it is accepted.
- Permitted additions: paths, operations, response statuses, response media
  types, response headers, links, callbacks, components, top-level and
  operation tags, `operationId` where there was none, schema `properties`,
  optional parameters, `x-extensible-enum` values and, in a schema only
  responses read, `items` or `additionalProperties` as a schema. Encoding
  headers take the context of the body they belong to.
- `required` has set semantics and depends on who reads the schema. Growth is
  accepted only where no request reaches the schema and only for a property
  added in the same revision. Shrinking is accepted only where no response
  reaches it. Component schemas and headers take the union of how both
  documents reach them, following `$ref` transitively. A path item reached
  through `$ref` keeps the directions of an inline one, and every component
  (parameter, request body, response, header) reaches what it references in
  its own direction even when nothing uses it. Within each document, a schema
  or header nothing reaches counts as both read and written, so referencing a
  formerly unreferenced component from one side does not relax it. Keys of
  `properties`, `patternProperties`, `$defs`, `dependentSchemas`, `encoding`
  and `headers` are names: a property called `not` freezes nothing.
- A parameter's `required` may go from true to false. Path-level parameters are
  merged into each operation first, keyed by `in` and name, with header names
  compared case-insensitively, so moving a parameter between a path and its
  operations is neutral in both directions. A parameter declared twice in one
  list is an error.
- `enum` never changes, except at the five `MLDSA65EnumLocations`, which may
  grow by exactly `mldsa65`. That list restates the api-cli-surface amendment
  of 2026-11-04 in document coordinates and does not widen it.
- Under `oneOf`, `not` and `if`, narrowing or widening a subschema can break
  either direction (kin-openapi validates `oneOf` as exactly one match), so
  `required`, open enums and element schemas are frozen there, and in every use
  of a component reached through them. Under `not` and `if`, whose meaning is
  inverted, a new property is refused too. `contains` is frozen the same way
  when `maxContains` is present, since widening the predicate can push a valid
  array over the limit.
- `$ref` values are compared as strings, never followed during comparison. The
  target is compared once in the components walk, with the context of every
  reader; a deep ref such as `#/components/schemas/A/properties/b` gives that
  context to all of `A`. Renaming or inlining a component is therefore
  refused. Any other `$ref` is an error, because its reader's rules could not
  reach the target: one outside `#/components/`, or a deep ref into a section
  other than `schemas` and `headers` (such as a parameter's schema), and one
  whose target does not exist under its literal, unescaped name. Every `$ref`
  key counts, even inside example data, so ref-shaped example data is an
  error: telling data from schema there would need the typed walk, and the
  error is the fail-closed side of that ambiguity.
- Composition lists (`allOf`, `anyOf`, `oneOf`, `prefixItems`) are positional
  and must keep their length. `patternProperties`, `dependentSchemas`, `$defs`,
  encodings, server variables and OAuth scope names accept no additions.
  Existing callbacks, inline or in `components.callbacks`, are compared
  strictly after parameter normalization. The first `components` object in a
  document without one is an addition like any other component.

## Disposition of every listed item

"Refused" means `CheckFreeze` returns at least one `hikyo-contract-*`
violation. Each refused item is a named case in
`TestFreezeContractRefusesForbiddenChanges` or
`TestFreezeContractRefusesForbiddenChangesInTheRealSpec` in
`api/freeze_contract_test.go`.

| Item (issue body and comments) | Disposition |
|---|---|
| Security alternative removed next to `{}` (`[{}, {a: []}]` to `[{}]` or `[]`), fixture and `POST /api/v1/auth/saml/{provider}/start` | Refused. Permanent fixture `remove-security-alternative-next-to-anonymous-fails` |
| Closed enum value removed from a response header, fixture and `Hikyo-Audit-Receipt` | Refused. Permanent fixture `remove-response-header-enum-value-fails` |
| Closed enum growth in a response header, `Hikyo-Audit-Receipt` to `[v1, mldsa65]` or `[v1, mldsa44]` | Refused. Permanent fixture `grow-closed-response-header-enum-fails` |
| Request media type renamed within the JSON family | Refused |
| Property removed from an unreferenced component (`AuditExportReceipt`, `AuditExportReceiptLine`) | Refused, because the layer walks every component. Owner decision 2026-11-13: option A, keep components frozen, see "Owner decisions" |
| Deleted `allOf`, `anyOf`, `oneOf`, `prefixItems` branch in parameter or header schemas and in parameter or header `content` | Refused |
| Deleted `prefixItems` entry in a request or response body | Refused |
| Deleted `patternProperties`, `dependentSchemas`, `dependentRequired` or discriminator entries | Refused |
| Media type swapped in parameter or header `content`; `content` replaced by `schema`; whole `content` deleted | Refused |
| Closed enum removal or type change inside parameter or header `content` | Refused |
| Link `parameters`, `requestBody` or `operationId` changed | Refused |
| Encoding `contentType` or `style` changed; encoding header removed; property removed inside an encoding header | Refused |
| Callback expression differing only inside `{...}` removed or renamed | Refused |
| `additionalProperties: false` added on a nested object or body root; `not` or `pattern` added at the body root | Refused |
| `required` naming an undeclared request property; new `required` entry in a parameter schema | Refused |
| Nested property type change or dropped nested `required` in a response header schema | Refused |
| Whole OAuth flow removed, or all of `flows` | Refused |
| `servers` URL changed at document, path or operation level; server variable `enum` or `default` changed or removed | Refused |
| `explode`, `style`, `allowEmptyValue`, `allowReserved` changed | Refused |
| Malformed non-array `x-extensible-enum` removed | Refused |
| First review of this work: directionless edits under `not`, `if` and `oneOf` (real spec `PrepareRemotePairingRequest`), YAML 1.1 booleans (real spec `UpdateStatus.channel` `off`), request encoding headers, deep refs, a ref into `paths` | Refused, in `TestFreezeContractRefusesDirectionlessAndMisreadChanges` and the real-spec test. The ref into `paths` is an error rather than a panic |
| Every change inside an existing callback refused, additions included (#1125) | Accepted, unchanged. `api/openapi.yaml` declares no callbacks. Revisit when one is needed |
| `$ref` inside response-header `content` makes diffing fail with an error | Accepted, unchanged. It fails closed, without a specific diagnostic |

## Reproduction on `dev` at `426bf59ce`

The issue body described the guard as dormant; it has been live since
`v1.0.0` was cut. Each gap already has a case from #1232 that fails if the gap reopens, so
no case was added for this pass. Run verbosely with
`GOMAXPROCS=2 go test -p 2 ./api -run 'TestFreezeContractRefuses' -v -count=1`,
every case below passed, meaning `CheckFreeze` refuses it, except the three
subtests in the last row, which live in another test function and failed for a
fixture reason, since repaired (below). Each passing case logs the violation
named here at the mutated location, not an incidental one. The `content` deletion cases also
add an `x-other` extension to keep the object non-empty; the deletion itself
is reported separately (`.../content/application~1json removed`).

| Gap | Case (`TestFreezeContractRefusesForbiddenChanges` unless noted) | Violation logged |
|---|---|---|
| Security alternative next to `{}` | `security alternative removed, anonymous kept`, `security emptied`; real spec `samlStart ...` | `hikyo-contract-changed` at `.../post/security` |
| Closed enum value removed from a response header | `response header enum value removed`; real spec `Hikyo-Audit-Receipt enum value removed` | `hikyo-contract-removed` at `.../headers/hikyo-audit-receipt/schema/enum` (the real-spec case mutates the first of the four export operations only) |
| Closed enum growth in a response header | `response header enum grown`, `... by mldsa65`; real spec `... grown by mldsa65`, `... mldsa44` | `hikyo-contract-added` at the same `enum` |
| Request media type rename in the JSON family | `request media type renamed to ...` (vendor, problem+json) | `hikyo-contract-removed` on `application~1json` and `hikyo-contract-added` on the new name |
| Component schema no operation references | `property removed from an unreferenced component`; real spec `AuditExportReceipt ...`, `AuditExportReceiptLine ...` | `hikyo-contract-removed` at `/components/schemas/<name>/properties/...` |
| Deleted `allOf`, `anyOf`, `oneOf`, `prefixItems` branch in parameter or header schema or `content` | `<keyword> branch deleted in param`, `header`, `param-content`, `header-content` (16 cases) | `hikyo-contract-changed` at `.../schema/<keyword>` |
| Deleted body `prefixItems` entry | `prefixItems entry deleted in request`, `... in response` | `hikyo-contract-changed` at `.../schema/prefixItems` |
| Deleted `patternProperties`, `dependentSchemas`, `dependentRequired`, discriminator entry | `<keyword> entry deleted in param`, `header` | `hikyo-contract-removed` or `-changed` at `.../schema/<keyword>` |
| Media type swapped, `content` replaced by `schema`, `content` deleted, enum or type change inside `content` | `media type swapped in ...`, `... content replaced by schema`, `... content deleted`, `enum value removed in ...`, `type changed in ...` | `hikyo-contract-added` and `-removed` on the media type, `-added` on `schema`, `-removed` on `enum`, `-changed` on `type` |
| Link, encoding and callback changes | `link ... changed`, `encoding ... changed`, `encoding header removed`, `property removed inside an encoding header`, `callback expression removed`, `... renamed` | `hikyo-contract-changed` at the link or encoding keyword; `-removed` at `.../headers/x-enc` (header removed) or `.../headers/x-enc/schema/properties/eb` (property removed); `-changed` at `.../callbacks/cb` |
| Request narrowing | `items added to a request array`, `additionalProperties false added ...`, `not added ...`, `pattern added ...`, `required names an undeclared request property`, `required grows inside a parameter schema` | `hikyo-contract-added` at the added keyword |
| Nested header schema change, OAuth flows, server variables, parameter serialization, malformed `x-extensible-enum` | the remaining cases in the same test | `hikyo-contract-changed` or `-removed` at the exact keyword |
| Request encoding header gains `items`, `additionalProperties`, a required property | `TestFreezeContractRefusesDirectionlessAndMisreadChanges/request_encoding_header_gains_*` | **Failed at `426bf59ce` for a fixture reason, repaired by #1298, see below** |

### Mutation sweep of the real spec

A scratch probe, not committed, took every node of `api/openapi.yaml` outside
prose (28,377 of 30,399), deleted it, and for strings and booleans also edited
it, then ran the document layer against the original, on `426bf59ce`. Deleting
a list element replaced it with a marker, so list shrinkage is not covered here
(the explicit cases cover it), and neither are additions or numeric edits. A
mutation that left a `$ref` unresolvable errored, which is the fail-closed side
(2,197 deletions, 4,810 edits). The only mutations the layer let through were
257 shapes, every one a `required` edit: an entry dropped from a component
schema that no response reaches (checked independently by following `$ref`s
from every response, header and callback), or a parameter's `required` going
from true to false. The ADR counts both as additive, so the sweep found no gap.

### Items already satisfied without a diff

- The `docs/handoff/oasdiff-freeze-policy.md` wording was corrected by #1232
  ("The guard is live, not dormant").
- The issue's "Done when" item on `docs/release/acceptance-1.0.md` is met:
  rows S1 and the API/CLI freeze row link #1217 and this handoff.

### Three failing subtests at `426bf59ce`, not caused by the gate

`request_encoding_header_gains_items`, `..._additionalProperties` and
`..._a_required_property` failed at `426bf59ce` with `gate errored: ... failed
to unmarshal data`. The `encoded` fixture in
`TestFreezeContractRefusesDirectionlessAndMisreadChanges` closed
`"requestBody"` with one `}` too many, so neither side parsed. This pass did
not edit them; [#1292](https://github.com/Hikyo-Org/Hikyo/issues/1292) was
repaired in [#1298](https://github.com/Hikyo-Org/Hikyo/pull/1298). With the
extra `}` removed in a scratch copy before that, the gate refused all three
with one `hikyo-contract-*` violation each, and they pass on the rebased tree.

## Known false refusals, accepted

`TestFreezeKnownFalseRefusals` pins which source still refuses each, so a change
in that is noticed, except the `oneOf` row, which
`TestFreezeContractRefusesDirectionlessAndMisreadChanges` covers. The new layer
accepts every row except the OAuth scope addition, the single-branch wrapper
and the `oneOf` row, which it refuses deliberately.

| Change | Refused by | Reason for accepting |
|---|---|---|
| Prose-only edit to `components.schemas.ID.description` | oasdiff (`request-body-any-of-added`, `-removed`) | **Affects the real spec.** oasdiff matches `anyOf` branches by content. Fixing it means filtering upstream findings or allowlisting ids that also report real breaks; both weaken the ADR's oasdiff gate. Editing that description is refused until an owner decides otherwise |
| OAuth scope reworded | oasdiff (`api-security-component-oauth-scope-changed`) | The spec declares no OAuth scheme. Revisit with the first one |
| OAuth scope added | oasdiff and the new layer | Same. A new scope is a new authorization requirement name; refusing it is fail-closed |
| Single-branch `oneOf`, `anyOf` or `allOf` wrapper in a parameter or header | `freezeRemovals` and the new layer | The schema shape and the generated types change |
| Path-level header parameter removed while an operation shadows it in another case | oasdiff and `freezeRemovals` | Upstream common-parameter matching is case-sensitive. The spec has no such pair |
| A new required response property on a component any `oneOf` reaches, such as the shared `Error` envelope (a `CLIDeviceErrorResponse` branch) and 13 other response-only components | The new layer (`hikyo-contract-added` on `required`) | **Affects the real spec.** Making a branch narrower can make a formerly ambiguous value match exactly one branch, so the server may send what old clients reject. Optional property additions there are still accepted. Kept refused pending owner decision 3 below |
| New response header | oasdiff allowlist (`response-header-added`) | Allowlist policy, unchanged by this work |
| Response property deprecated | oasdiff allowlist (`response-property-deprecated`) | Allowlist policy, unchanged by this work |

## Known exposure, accepted

A new optional property in a `oneOf` branch is accepted, though a property
with a schema narrows its branch: a value that matched two branches can then
match exactly one and become valid, which old clients reject.
`TestFreezeStillPermitsPropertyAdditions/*/oneOf` on `dev` pins property
additions under `oneOf` as permitted, so it stays accepted under owner
decision 3.

## Owner decisions

1. **Decided 2026-11-13 by the owner (Marc Went), option A: unreferenced
   components are part of the promise.** Recorded as an owner decision record in
   the [API and CLI surface ADR](../adr/api-cli-surface.md); it clarifies how the
   freeze applies and changes no ADR clause, gate or spec text. The layer
   freezes them: removing a property from `AuditExportReceipt` is refused, and
   so is removing, renaming or inlining any component schema, even where the
   inlined shape is identical. Two root schemas have no reference from any
   operation: `AuditExportReceiptLine` and `AdapterVisibility`.
   `AuditExportReceipt` is reachable only through the first. The export
   descriptions promise the receipt schemas. Options considered:
   - **A. Freeze components (accepted, no change).** Every component is frozen
     whether or not an operation reads it. Cost, accepted: the receipt schemas
     stay undiscoverable from the operations, so generated clients carry no
     receipt type. It keeps the receipt schemas as promised and needs neither
     an ADR amendment nor any change to the gate or the spec.
   - **B. Reference the schemas from the spec (rejected).** Measured against
     v1.0.0 on this tree: pointing the `application/x-ndjson` response schema
     at `AuditExportReceiptLine` is refused (`hikyo-contract-added` on `$ref`,
     `hikyo-contract-removed` on `type`, among other findings), because it changes an existing media
     type's schema. A second response media type that references it passes the
     gate, but it documents a media type the server does not serve, and the
     freeze makes that permanent. Doing B honestly therefore needs an ADR
     amendment to `api-cli-surface.md`.
   - **C. Exempt unreferenced components from the gate (rejected).** Code
     change in `reach` and `components`. Cost: the receipt schemas the
     descriptions promise become editable after the freeze, which contradicts
     those descriptions.
2. **Open: the allowlist false refusals above** (`response-header-added`,
   `response-property-deprecated`, the `ID` description edit) stay refused
   until the allowlist is changed deliberately.
3. **Open: `oneOf` freezes requiredness in every component it reaches,** including
   the shared `Error` envelope. Relax it to permit new required response
   properties there if the exactly-one exposure is judged theoretical. New
   optional properties under `oneOf` carry the same exposure and stay
   permitted, as the pre-existing `dev` test pins; tighten both together if
   the exposure is judged real.

## Evidence and local verification

- The live pair passes: `go run ./scripts/ci/api-freeze-guard --base <v1.0.0
  api/openapi.yaml> --revised api/openapi.yaml`, and v1.0.0 against itself.
  Every edit since the tag (prose, `mldsa65` on the five enums, the new
  `instance_directory` response property) is accepted with no exemption beyond
  `MLDSA65EnumLocations`.
- The same pair with `Hikyo-Audit-Receipt` grown to `[v1, v2]` is refused with
  `hikyo-contract-added` on each export response header and on the
  `AuditReceipt` parameter.

```sh
git show v1.0.0:api/openapi.yaml >/tmp/v1.0.0.yaml
GOMAXPROCS=2 go run ./scripts/ci/api-freeze-guard --base /tmp/v1.0.0.yaml --revised api/openapi.yaml
GOMAXPROCS=2 go test -p 2 ./api -count=1
GOMAXPROCS=2 go test -race -p 2 ./api -count=1
GOMAXPROCS=2 go vet -p 2 ./api ./scripts/ci/api-freeze-guard
```

## PR #1232 review (CodeRabbit)

All thirteen findings were triaged: eleven fixed, one declined, one needing
no change. The fixes are eight in the layer (first component addition,
reusable callback normalization, path-item ref directions, property names in
the reachability walk, unused wrapper directions, `contains` under
`maxContains`, numeric precision, duplicate parameters) and three in tests (an
`operationId` on an existing operation, the `ID` description search bounded
to its block, exact refusal sources asserted). Each layer fix has a case in
`TestFreezeContractReviewFollowUps` that fails on the earlier layer. Declined:
refusing new properties under `oneOf`, see the known exposure above. No
change needed for the `mldsa65` reader finding: no header in v1.0.0 reaches
the five amended enums, and making one reach them is a `$ref` change, which
is refused.

A fresh review of those fixes found that a callback or other entry named like
a name map (`headers`, `properties`...) hid its `$ref` from validation, that a
non-map `components` extension was no longer validated, and that wrappers
referenced inside a callback took the operation's direction although the
server writes a callback request. All three are fixed and pinned in the same
test; wrappers inside a callback now count as read and written.

CodeRabbit's re-review of `4c4670c` found that a reusable request body,
response or parameter with an inline schema still took its usual direction
in the components walk when a callback referenced it. Each now takes the
direction its references give it, so one a callback references is compared
as read and written, and its parameter `required` may no longer drop.

## Remaining delivery gates

Independent Standards and Spec review, hosted feedback and merge-group proof
for the actual candidate. This work does not authorize merge or release.
