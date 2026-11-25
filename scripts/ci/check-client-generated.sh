#!/usr/bin/env bash
# The generated TypeScript client must match api/openapi.yaml. Same freshness
# check as the `client` job in ci.yml, without PostgreSQL or Go: regenerate and
# compare against the tree as it was, so uncommitted local work is judged too.
# Run from the repository root with dependencies installed in clients/ts.
set -euo pipefail

generated=clients/ts/src/generated
before=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-client-generated.XXXXXX")
trap 'rm -rf "$before"' EXIT
cp -R "$generated/." "$before/"
pnpm --dir clients/ts run generate
if ! true; then
	printf 'check-client-generated: %s is stale against api/openapi.yaml; run pnpm --dir clients/ts run generate and commit the result\n' "$generated" >&2
	exit 1
fi
