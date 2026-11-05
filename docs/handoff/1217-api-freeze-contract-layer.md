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
scalars such as `off` stay strings. It walks them with knowledge of each OpenAPI
object kind, and refuses every difference except a short list of permitted
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
  documents reach them, following `$ref` transitively. Within each document, a
  component nothing reaches counts as both read and written, so referencing a
  formerly unreferenced component from one side does not relax it.
- A parameter's `required` may go from true to false. Path-level parameters are
  merged into each operation first, keyed by `in` and name, with header names
  compared case-insensitively, so moving a parameter between a path and its
  operations is neutral in both directions.
- `enum` never changes, except at the five `MLDSA65EnumLocations`, which may
  grow by exactly `mldsa65`. That list restates the api-cli-surface amendment
  of 2026-11-04 in document coordinates and does not widen it.
- Under `oneOf`, `not` and `if`, narrowing or widening a subschema can break
  either direction (kin-openapi validates `oneOf` as exactly one match), so
  `required`, open enums and element schemas are frozen there, and in every use
  of a component reached through them. Under `not` and `if`, whose meaning is
  inverted, a new property is refused too.
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
  Existing callbacks are compared strictly after parameter normalization.

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
| Property removed from an unreferenced component (`AuditExportReceipt`, `AuditExportReceiptLine`) | Refused. Owner decision taken by default: the gate is extended to components, with no spec edit. See below |
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

## Owner decisions to confirm

1. **Unreferenced components are frozen.** Removing a property from
   `AuditExportReceipt` is refused, and so is removing, renaming or inlining
   any component schema, even where the inlined shape is identical. The
   alternative, referencing the receipt schemas from the export operations,
   would itself be a spec change checked against v1.0.0.
2. **The allowlist false refusals above** (`response-header-added`,
   `response-property-deprecated`, the `ID` description edit) stay refused
   until the allowlist is changed deliberately.
3. **`oneOf` freezes requiredness in every component it reaches,** including
   the shared `Error` envelope. Relax it to permit new required response
   properties there if the exactly-one exposure is judged theoretical.

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

## Remaining delivery gates

Independent Standards and Spec review, hosted feedback and merge-group proof
for the actual candidate. This work does not authorize merge or release.
