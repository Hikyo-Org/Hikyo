# PR 1029: grouped Go dependencies

[PR #1029](https://github.com/Hikyo-Org/Hikyo/pull/1029) retains the six
Dependabot updates and selects kin-openapi v0.152.0 instead of v0.151.0.
The other direct updates remain chi v5.5.0, go-jose v4.1.6, oasdiff v1.34.0,
go-diceware v0.6.0, and sigstore v1.11.1. Go 1.27.3 owns the tidy-generated
module checksums.

The raw-bundle server fixture now uses escaped member names and non-canonical
ordering instead of duplicate members. This preserves its byte-fidelity check
under kin-openapi's stricter JSON decoder; separate checks prove duplicate
members are rejected before the definitions service runs. The SSE deadline
fixture bounds both socket buffers so its stalled frame does not depend on
platform buffer autotuning. Runtime server code and deadlines are unchanged.

## Verification

The local validation sequence is:

```sh
go mod tidy
scripts/ci/fast-feedback.sh --working-tree
go test -p 2 -parallel 2 ./api/... ./internal/deliverytarget/... ./internal/pairing/... ./internal/jwkssource/... ./internal/releasetrust/... ./internal/devupgrade/... ./internal/upgradecustody/... ./internal/oidcfed/... ./internal/server/...
go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml
git diff --exit-code -- api/apigen
git diff --check
```

Hosted useful feedback, Linux dependency compatibility, repository invariants,
and full current-base merge-group validation are separate evidence. Read the
PR's checks and queue run for the exact tested head and merge-group revision.
The requested endpoint is merge. Release publication and deployment require
their own evidence.
