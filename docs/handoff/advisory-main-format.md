# Advisory integration CI repairs

Main commit `3700a0ef24af59cfb4d3d159b4eda147a74b0e77` already contains the
SCIM paging, workspace consent, browser secret lifetime, and human OIDC
transport remediations delivered through the combined HTTP advisory branch.
The four original private PRs are superseded by that integration.

The `generated` job in main run `33991135025` failed its Format step on
`internal/config/config_test.go`. The shell's standalone gofmt accepted the
file, but the repository's Go 1.27 toolchain formatter required different
alignment for three trailing comments. Apply the formatter from
`$(go env GOROOT)/bin/gofmt`, matching the existing CI check. No test logic or
runtime behavior changes from this formatting correction.

The same main run also failed `TestWorkspaceConsentOriginAndLiveness` on both
databases. `StartHandoff` returned its nanosecond deadline while the stored
deadline, subsequently returned by `ShowHandoff`, had microsecond precision.
macOS clock precision hid the mismatch during earlier local validation.
Canonicalize the deadline with the existing `store.CanonTime` before both
persisting and returning it. The test now injects a noncanonical nanosecond
clock on every platform and retains exact equality and expiry rejection.
The deterministic regression failed on both databases before this repair.

Validation: repository-wide formatting and SQLC, OpenAPI, scanning, and CRD
generation freshness pass. Config race tests pass. The existing integrated
web source passes typechecking, all 801 unit tests, production build, and all
20 TypeScript client tests. The remaining advisory-specific local checks and
exact-head hosted CI are tracked by the delivery session.
