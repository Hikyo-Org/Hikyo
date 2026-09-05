# Advisory integration CI repair

Main commit `3700a0ef24af59cfb4d3d159b4eda147a74b0e77` already contains the
SCIM paging, workspace consent, browser secret lifetime, and human OIDC
transport remediations delivered through the combined HTTP advisory branch.
The four original private PRs are superseded by that integration.

The `generated` job in main run `33991135025` failed its Format step on
`internal/config/config_test.go`. The shell's standalone gofmt accepted the
file, but the repository's Go 1.27 toolchain formatter required different
alignment for three trailing comments. Apply the formatter from
`$(go env GOROOT)/bin/gofmt`, matching the existing CI check. No test logic or
runtime behavior changes.

Validation: repository-wide formatting and SQLC, OpenAPI, scanning, and CRD
generation freshness pass. Config race tests pass. The existing integrated
web source passes typechecking, all 801 unit tests, production build, and all
20 TypeScript client tests. The remaining advisory-specific local checks and
exact-head hosted CI are tracked by the delivery session.
