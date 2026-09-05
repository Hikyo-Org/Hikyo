#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
: "${HIKYO_TEST_POSTGRES_DSN:?client skew requires PostgreSQL as well as SQLite}"
baseline=HEAD
mode=pre-freeze-rehearsal
if git show-ref --verify --quiet refs/tags/v1.0.0; then
	baseline=refs/tags/v1.0.0
	mode=frozen-v1.0.0
fi
commit=$(git rev-parse --verify "$baseline^{commit}")
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
git archive "$commit" clients/ts | tar -x -C "$work"
client="$work/clients/ts"
# Never regenerate the baseline. Its exact checked-in SDK and validators are
# the older consumer. Package lifecycle hooks are unnecessary for this harness.
(cd "$client" && pnpm install --frozen-lockfile --ignore-scripts)
printf 'client skew: mode=%s baseline=%s server=%s\n' "$mode" "$commit" "$(git rev-parse HEAD)"
CI=true HIKYO_FROZEN_CLIENT_ROOT="$client" go test -count=1 -json \
	./internal/isolation -run '^TestFrozenGeneratedClientAgainstCurrentServer$' | tee "$work/result.jsonl"
node --test scripts/ci/check-client-skew-evidence.test.mjs
node scripts/ci/check-client-skew-evidence.mjs "$work/result.jsonl"
