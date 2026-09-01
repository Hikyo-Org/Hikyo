# Issue #520 - dormant API freeze guard

Status: implemented on t3code/impl-520.

## Contract

- v1.0.0 is the sole API freeze tag, matching the locked API/CLI ADR.
- Before that tag exists, freeze-guard succeeds with an explicit dormant
  message. Prerelease tags do not arm it.
- A v1.0.0 ref that cannot resolve to a commit fails closed.
- After that tag exists, CI reads api/openapi.yaml from the tagged commit and
  runs the existing fail-closed api.CheckFreeze policy against the proposed
  document.
- freeze-guard is a planned required job for full and API change plans.

## Verification

scripts/ci/check-api-freeze_test.sh creates an isolated local repository and
proves dormant, malformed-tag refusal, identical-spec pass, and breaking-spec
refusal paths without pushing a tag. The refusal fixture removes a deprecated
endpoint and must report api-path-removed-with-deprecation.
