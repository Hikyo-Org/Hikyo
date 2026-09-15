#!/usr/bin/env bash
# Triage a failed CI run without clicking through the Actions UI: prints the
# failed leaf jobs and the tail of each failed step's log. Answers "why is the
# 22-min race shard red" in one command.
#
#   scripts/dev/ci-failed.sh            latest failed ci.yml run on this branch
#   scripts/dev/ci-failed.sh <run-id>   a specific run
set -uo pipefail

command -v gh >/dev/null 2>&1 || { printf 'ci-failed: gh CLI required\n' >&2; exit 2; }

run_id=${1:-}
if [ -z "$run_id" ]; then
	branch=$(git rev-parse --abbrev-ref HEAD)
	run_id=$(gh run list --workflow=ci.yml --branch "$branch" --status=failure \
		--limit=1 --json databaseId -q '.[0].databaseId')
	[ -n "$run_id" ] || { printf 'ci-failed: no failed ci.yml run on branch %s\n' "$branch" >&2; exit 1; }
fi

printf '\033[1m== failed leaf jobs (run %s) ==\033[0m\n' "$run_id"
# Skip the aggregator gates (ci-required, race, test, fuzz) — they only echo a
# dependency's failure and add noise.
gh run view "$run_id" --json jobs \
	-q '.jobs[] | select(.conclusion=="failure" and (.name|test("required|^race$|^test$|^fuzz$")|not)) | "  \(.name)"'

printf '\n\033[1m== failed step log tails ==\033[0m\n'
# --log-failed already scopes to failed steps; surface error lines + context.
gh run view "$run_id" --log-failed 2>/dev/null \
	| grep -iE 'error|--- FAIL|DATA RACE|panic|##\[error\]|not assignable|✘' \
	| grep -viE 'fail-on-cache-miss|error handling|errors\.(Is|As|New)' \
	| tail -60
