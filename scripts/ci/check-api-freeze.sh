#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
cd "$repo_root"

freeze_tag=v1.0.0
freeze_ref="refs/tags/$freeze_tag"
if ! matched_ref=$(git for-each-ref --format='%(refname)' "$freeze_ref"); then
	printf '%s\n' "API freeze guard: failed to inspect $freeze_ref" >&2
	exit 1
fi
if [ -z "$matched_ref" ]; then
	printf '%s\n' "API freeze guard dormant: $freeze_tag does not exist"
	exit 0
fi
if [ "$matched_ref" != "$freeze_ref" ]; then
	printf '%s\n' "API freeze guard: unexpected ref lookup result: $matched_ref" >&2
	exit 1
fi
if ! freeze_commit=$(git rev-parse --verify "$freeze_ref^{commit}" 2>/dev/null); then
	printf '%s\n' "API freeze guard: $freeze_ref does not resolve to a commit" >&2
	exit 1
fi

base_spec=$(mktemp)
trap 'rm -f "$base_spec"' EXIT HUP INT TERM
git show "$freeze_commit:api/openapi.yaml" >"$base_spec"

go run ./scripts/ci/api-freeze-guard \
	--base "$base_spec" \
	--revised api/openapi.yaml
printf '%s\n' "API freeze guard passed: api/openapi.yaml is compatible with $freeze_tag"
