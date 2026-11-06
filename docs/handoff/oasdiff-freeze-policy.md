# oasdiff upgrade: preserve the API freeze's no-removal promise

## Result and scope

Follow up the oasdiff deferral in [PR #1109](https://github.com/Hikyo-Org/Hikyo/pull/1109).
Preserve the additive-only policy in `docs/adr/api-cli-surface.md` on oasdiff
v1.35.0, which reached `dev` through
[PR #1174](https://github.com/Hikyo-Org/Hikyo/pull/1174) without this gate change
and left both past-sunset fixtures failing there. The authored API specification,
allowlist, release policy and freeze-tag activation rules are unchanged.

## Why the gate needs its own removal check

oasdiff v1.35.0 intentionally omits deprecated request-property and required
response-property removals after their `x-sunset` date. Endpoint and parameter
checks also omit past-sunset removals. Running every check down to INFO cannot
recover a finding that the checker never emits.

`api.CheckFreeze` now combines two independent sources of violations:

1. A structural check on the diff produces Hikyo-owned error IDs, without
   consulting deprecation or sunset metadata. It refuses deleted endpoints,
   parameters, properties, subschemas (such as an array's `items`), open-enum
   values, responses (including `default` and wildcards), response links and
   media-type encodings. Traversal covers request bodies, parameters, response
   bodies and headers, including nested schemas, modified composed branches and
   resolved references. Deleting a schema from a keyword holding exactly one,
   such as `items` or `additionalProperties`, is refused even when it is empty.
2. Every upstream checker finding still passes through the existing addition
   allowlist. New upstream IDs remain forbidden until explicitly reviewed. The
   stability threshold is set to `draft`, because a `x-stability-level` of
   `alpha` or `draft` otherwise hides upstream findings for that operation. It
   also means promoting an operation from those levels is refused; the spec uses
   no stability labels today.

Upstream checkers do not descend into callbacks, so the gate cannot prove that
a change inside one is additive. It refuses every callback removal and every
change oasdiff reports inside a callback, additions included. Adding a whole callback remains
permitted, and `api/openapi.yaml` declares none today. This is a deliberate
fail-closed choice; a future additive callback change needs this rule revisited.

The structural property walker is supplied by oasdiff; Hikyo owns the decision
to refuse its deletions. Violations have a total sort order for reproducible
diagnostics. The structural check can report a removal alongside an upstream
finding for the same edit. Both identify a forbidden change; neither suppresses
the other.

Before diffing, normalize inherited parameters into each operation with the
upstream common-parameter helper, including callback paths. This operates only
on freshly parsed documents, and a seen-callback guard bounds cyclic references.
Removing a shadowed path declaration or moving an unchanged parameter to an
operation is neutral; removing an effective inherited parameter still fails.
Both directions of declaration movement are tested. Because path-level
parameters now compare as operation parameters, the operation-level rules apply
to them: an optional addition passes, and an edit such as deprecating one or
widening its `maxLength` is refused.

oasdiff v1.35.0 also changes two security-scheme checks. OAuth and OpenID Connect
URLs differing only by RFC-equivalent host case or default port now pass, and
OAuth scope additions and edits are refused in every flow rather than only
`implicit`. The spec declares neither scheme type today.

## Known limits

The guard is live, not dormant: `refs/tags/v1.0.0` exists and
`scripts/ci/check-api-freeze.sh` diffs every revision against it. The forbidden
changes this gate still passed on oasdiff v1.35.0 (and v1.34.1), such as a
security alternative removed next to `{}`, closed-enum values in response
headers and request media-type renames, were listed in
[#1217](https://github.com/Hikyo-Org/Hikyo/issues/1217). They are now refused
by a third, document-coordinate layer; see
[the #1217 handoff](./1217-api-freeze-contract-layer.md).

## Regression evidence and local verification

Before the gate change, upgrading to v1.35.0 reproduced both PR #1109 fixture
failures: `a forbidden change passed the gate`. Keep those negative fixtures.

The additional tests assert specific structural violation IDs and locations,
including ordinary, deprecated, past/future-sunset and malformed-sunset property
removals; inline, nested, array, composed and shared/recursive schemas;
operation/inherited parameters; endpoints; callbacks, including cyclic ones; each
removal upstream omits; and every stability level. Positive cases preserve
permitted additions in every property context, added subschemas and whole
callbacks, open-enum growth and identical-document comparisons.

Run from the repository root with its pinned Go toolchain:

```sh
GOMAXPROCS=2 go test -p 2 ./api -count=1
GOMAXPROCS=2 go test -race -p 2 ./api -count=1
GOMAXPROCS=2 go vet -p 2 ./api ./scripts/ci/api-freeze-guard
GOMAXPROCS=2 GOFLAGS='-p=2' scripts/ci/check-api-freeze_test.sh
scripts/ci/fast-feedback.sh --working-tree --plan
GOMAXPROCS=2 GOFLAGS='-p=2' scripts/ci/fast-feedback.sh --working-tree
```

Expected results: the API and race suites pass, vet reports no findings, the
shell fixture verifies dormant, invalid-tag, active-pass and active-refusal
paths, and fast feedback verifies formatting, modules and API compilation.
The shell fixture clones committed HEAD, so run it after committing the change.

## Remaining delivery gates

Require independent Standards and Spec review and fresh hosted feedback,
dependency compatibility, invariants and protected merge-group proof for the
actual candidate. Local checks establish only their named coverage. This
follow-up does not authorize merge, release, deployment or a stable freeze tag.
