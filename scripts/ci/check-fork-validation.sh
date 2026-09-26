#!/bin/sh
# Trusted merge gate for a fork pull request (#813). trusted-ci runs this from
# the base branch under pull_request_target; it never checks out or executes PR
# code. The fork's validation ran untrusted under pull_request (ci-fork.yml),
# from YAML in the PR's merge ref. Accept that result only when:
#   1. the PR head is still HEAD_SHA (a newer push gets its own gate),
#   2. the PR changes nothing under .github/, so the run executed base YAML,
#   3. fork-ci's latest run for HEAD_SHA completed and its aggregate gate
#      ("validation / ci-required") succeeded.
# Anything else fails closed.
set -eu

: "${GH_REPO:?}" "${PR_NUMBER:?}" "${HEAD_SHA:?}"
timeout_seconds=${FORK_GATE_TIMEOUT_SECONDS:-5400}
poll_seconds=${FORK_GATE_POLL_SECONDS:-30}
gate_job='validation / ci-required'

fail() {
	printf 'fork validation gate: %s\n' "$1" >&2
	exit 1
}

pr=$(gh api "repos/$GH_REPO/pulls/$PR_NUMBER" --jq '[.head.sha, (.changed_files | tostring)] | join(" ")')
current_head=${pr% *}
changed_files=${pr#* }
[ "$current_head" = "$HEAD_SHA" ] ||
	fail "PR head moved from $HEAD_SHA to $current_head; the newer run decides"

# The files endpoint lists at most 3000 entries. A shorter list than
# changed_files could hide a workflow change, so it fails closed.
files=$(gh api --paginate "repos/$GH_REPO/pulls/$PR_NUMBER/files?per_page=100" \
	--jq '.[] | [.filename, (.previous_filename // "")] | @tsv')
listed=$(printf '%s\n' "$files" | grep -c . || true)
[ "$listed" -ge "$changed_files" ] ||
	fail "listed $listed of $changed_files changed files; cannot prove .github/ is untouched"
if printf '%s\n' "$files" | tr '\t' '\n' | grep '^\.github/' >&2; then
	fail "a fork PR that changes .github/ needs a maintainer to land it from a branch in this repository"
fi

deadline=$(($(date +%s) + timeout_seconds))
while :; do
	run=$(gh api "repos/$GH_REPO/actions/workflows/ci-fork.yml/runs?event=pull_request&head_sha=$HEAD_SHA&per_page=1" \
		--jq '.workflow_runs[0] // empty | "\(.id) \(.status)"')
	if [ -n "$run" ] && [ "${run#* }" = completed ]; then
		run_id=${run% *}
		conclusion=$(gh api --paginate "repos/$GH_REPO/actions/runs/$run_id/jobs?filter=latest&per_page=100" \
			--jq ".jobs[] | select(.name == \"$gate_job\") | .conclusion")
		[ "$conclusion" = success ] ||
			fail "fork-ci run $run_id: '$gate_job' concluded '${conclusion:-missing}'"
		printf 'fork validation gate: fork-ci run %s passed on %s\n' "$run_id" "$HEAD_SHA"
		exit 0
	fi
	[ "$(date +%s)" -lt "$deadline" ] ||
		fail "no completed fork-ci run for $HEAD_SHA within ${timeout_seconds}s (a first-time contributor's run needs maintainer approval); re-run this job once it finishes"
	printf 'fork validation gate: waiting for fork-ci on %s (%s)\n' "$HEAD_SHA" "${run:-no run yet}"
	sleep "$poll_seconds"
done
