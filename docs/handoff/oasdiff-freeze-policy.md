# oasdiff upgrade: preserve the API freeze's no-removal promise

## Result and scope

Follow up the oasdiff deferral in [PR #1109](https://github.com/Hikyo-Org/Hikyo/pull/1109).
Upgrade oasdiff from v1.34.1 to v1.35.0 while preserving the additive-only
policy in `docs/adr/api-cli-surface.md`. The authored API specification,
allowlist, release policy and freeze-tag activation rules are unchanged.

## Why the gate needs its own removal check

oasdiff v1.35.0 intentionally omits deprecated request-property and required
response-property removals after their `x-sunset` date. Endpoint and parameter
checks also omit past-sunset removals. Running every check down to INFO cannot
recover a finding that the checker never emits.

`api.CheckFreeze` now combines two independent sources of violations:

1. Structural endpoint, parameter and property deletions produce Hikyo-owned
   error IDs, without consulting deprecation or sunset metadata. Property
   traversal covers request bodies, parameters, response bodies and headers,
   including modified callbacks, nested/composed schemas and resolved references.
2. Every upstream checker finding still passes through the existing addition
   allowlist. New upstream IDs remain forbidden until explicitly reviewed.

The structural property walker is supplied by oasdiff; Hikyo owns the decision
to refuse its deletions. Recursive/shared schemas and callbacks have bounded
traversal. Violations have a total sort order for reproducible diagnostics.
The structural check can report a removal alongside an upstream finding for
the same edit. Both identify a forbidden change; neither suppresses the other.

Before diffing, normalize inherited parameters into each operation with the
upstream common-parameter helper, including callback paths. This operates only
on freshly parsed documents. Removing a shadowed path declaration or moving an
unchanged parameter to an operation is neutral; removing an effective inherited
parameter still fails. Both directions of declaration movement are tested.

## Regression evidence and local verification

Before the gate change, upgrading to v1.35.0 reproduced both PR #1109 fixture
failures: `a forbidden change passed the gate`. Keep those negative fixtures.

The additional tests assert specific structural violation IDs and locations,
including ordinary, deprecated, past/future-sunset and malformed-sunset property
removals; inline, nested, array, composed and shared/recursive schemas;
operation/inherited parameters; endpoints and callback properties. Positive
cases preserve permitted additions and identical-document comparisons.

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
