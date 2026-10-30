# PR #1109: compatible Go dependency updates

## Result and scope

Update the AWS SDK core, config, credentials, STS and Smithy modules, plus
`golang.org/x/net`, with the corresponding AWS transitive dependencies.
Retain oasdiff at v1.34.1. No application implementation or release policy changes.

## Deferred oasdiff update

The proposed oasdiff v1.35.0 suppresses deprecated request-property and required
response-property removals once their `x-sunset` date has passed. Hikyo's API
freeze rejects the change IDs oasdiff reports, and its version promise does not
permit removals after a deprecation window. Suppressed findings would therefore
allow forbidden changes through the gate.

The upstream behavior is in
[propertyRemovalId](https://github.com/oasdiff/oasdiff/blob/v1.35.0/checker/deprecation.go).
The owning policy remains in `docs/adr/api-cli-surface.md` and `api/freeze.go`.

Two new negative fixtures under `api/testdata/freeze/` remove only a deprecated
request property or required response property with a fixed past sunset date.
Both failed with v1.35.0 because a forbidden change passed the gate. Restoring
v1.34.1 makes both fixtures and the complete API package suite pass.

## Local evidence

Run from the repository root with the pinned Go toolchain:

```sh
GOMAXPROCS=2 go test -p 2 ./api -count=1
FAST_FEEDBACK_BASE=hikyo-upstream/main scripts/ci/fast-feedback.sh --working-tree --plan
GOMAXPROCS=2 GOFLAGS='-p=2' FAST_FEEDBACK_BASE=hikyo-upstream/main scripts/ci/fast-feedback.sh --working-tree
```

The API test command passed after the deferral. Hosted feedback, dependency
compatibility, repository invariants and the mandatory merge-group suite must
be checked for the final pushed revision; local checks do not replace them.
The optional paid performance benchmark was not run because the repository's
projected budget was exhausted.

## Follow-up

Revisit oasdiff only after the freeze gate independently preserves Hikyo's
no-removal promise or an upstream version restores the needed findings.
Keep both regression fixtures passing without weakening the freeze policy.
This dependency PR does not authorize or establish release, deployment or
live-provider acceptance.
