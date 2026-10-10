#!/bin/sh
# Default-branch workflow_run callback. Read GitHub-owned PR/run associations;
# a PR-controlled run title alone never authorizes a rerun. The sole mutation
# reruns an existing base-controlled gate which repeats every approval check.
set -eu
: "${GH_REPO:?}" "${SOURCE_RUN_ID:?}" "${SOURCE_RUN_ATTEMPT:?}"
case "$SOURCE_RUN_ID:$SOURCE_RUN_ATTEMPT" in *[!0-9:]*) printf 'invalid source run identity\n' >&2; exit 1 ;; esac
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-gate-reconcile.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

report() {
	printf 'PR gate reconciliation: %s\n' "$1" >&2
	[ -z "${GITHUB_STEP_SUMMARY:-}" ] || printf '%s\n' "$1" >>"$GITHUB_STEP_SUMMARY"
}
fail() { report "$1"; exit 1; }
noop() { report "No rerun: $1"; exit 0; }
read_api() {
	gh api "$@" >"$work/response" || fail 'read-only GitHub API request failed'
	jq -e -s 'length == 1' "$work/response" >/dev/null 2>&1 || fail 'malformed GitHub API response'
	cat "$work/response"
}
repo=$(read_api "repos/$GH_REPO") || exit 1
repo_id=$(printf '%s\n' "$repo" | jq -er '.id | select(type == "number" and . > 0 and floor == .)') || fail 'invalid repository metadata'
fork_workflow=$(read_api "repos/$GH_REPO/actions/workflows/ci-fork.yml") || exit 1
fork_id=$(printf '%s\n' "$fork_workflow" | jq -er 'select(.path == ".github/workflows/ci-fork.yml" and (.name == "fork-ci" or .name == "pr-validation")) | .id | select(type == "number" and . > 0 and floor == .)') || fail 'invalid PR validation workflow identity'
trusted_workflow=$(read_api "repos/$GH_REPO/actions/workflows/ci-control.yml") || exit 1
trusted_id=$(printf '%s\n' "$trusted_workflow" | jq -er 'select(.path == ".github/workflows/ci-control.yml" and .name == "trusted-ci") | .id | select(type == "number" and . > 0 and floor == .)') || fail 'invalid trusted workflow identity'
source=$(read_api "repos/$GH_REPO/actions/runs/$SOURCE_RUN_ID") || exit 1
# Match both immutable workflow identity and its API path. A forged workflow
# name or title cannot target another workflow's jobs.
printf '%s\n' "$source" | jq -e --argjson id "$SOURCE_RUN_ID" --argjson workflow "$fork_id" --argjson repo "$repo_id" '
	.id == $id and .workflow_id == $workflow and .repository.id == $repo and
	.path == ".github/workflows/ci-fork.yml" and .event == "pull_request" and
	(.display_title | type == "string" and test("^fork-ci #[1-9][0-9]*$")) and
	(.head_sha | type == "string" and test("^[0-9a-f]{40}$")) and
	(.run_attempt | type == "number" and . > 0 and floor == .) and
	(.pull_requests | type == "array")' >/dev/null || fail 'invalid source workflow or PR metadata'
pr_number=$(printf '%s\n' "$source" | jq -r '.display_title | ltrimstr("fork-ci #")')
head=$(printf '%s\n' "$source" | jq -r '.head_sha')
printf '%s\n' "$source" | jq -e --arg title "fork-ci #$pr_number" '(.name == "fork-ci" or .name == "pr-validation" or .name == $title)' >/dev/null || fail 'invalid source workflow name'
[ "$(printf '%s\n' "$source" | jq -r '.run_attempt')" = "$SOURCE_RUN_ATTEMPT" ] || noop 'source completion belongs to an older attempt'
[ "$(printf '%s\n' "$source" | jq -r '.status')" = completed ] || noop 'source run has not completed'

check_pr() {
	pr=$(read_api "repos/$GH_REPO/pulls/$pr_number") || exit 1
	printf '%s\n' "$pr" | jq -e --argjson number "$pr_number" --argjson repo "$repo_id" '
		.number == $number and .base.repo.id == $repo and (.state == "open" or .state == "closed") and
		(.head.sha | type == "string" and test("^[0-9a-f]{40}$")) and
		(.head.repo.id | type == "number" and . > 0 and floor == .)' >/dev/null || fail 'invalid current PR metadata'
	[ "$(printf '%s\n' "$pr" | jq -r '.state')" = open ] || noop 'PR is closed'
	[ "$(printf '%s\n' "$pr" | jq -r '.head.sha')" = "$head" ] || noop 'PR head superseded this validation'
	head_repo=$(printf '%s\n' "$pr" | jq -r '.head.repo.id')
	printf '%s\n' "$source" | jq -e --argjson number "$pr_number" --arg head "$head" --argjson repo "$repo_id" --argjson head_repo "$head_repo" '
		.head_repository.id == $head_repo and any(.pull_requests[];
			.number == $number and .head.sha == $head and .head.repo.id == $head_repo and .base.repo.id == $repo)' >/dev/null || fail 'source run lacks an exact PR association'
}
check_latest_source() {
	latest=$(read_api "repos/$GH_REPO/actions/workflows/ci-fork.yml/runs?event=pull_request&head_sha=$head&per_page=100" --paginate --slurp) || exit 1
	printf '%s\n' "$latest" | jq -e 'type == "array" and length > 0 and all(.[]; (.workflow_runs | type) == "array")' >/dev/null || fail 'invalid validation run listing'
	latest_source=$(printf '%s\n' "$latest" | jq -r --arg title "fork-ci #$pr_number" --arg head "$head" --argjson number "$pr_number" --argjson workflow "$fork_id" --argjson repo "$repo_id" --argjson head_repo "$head_repo" '
		[.[] | .workflow_runs[] | select(.workflow_id == $workflow and .path == ".github/workflows/ci-fork.yml" and .event == "pull_request" and
			.display_title == $title and .head_sha == $head and .repository.id == $repo and .head_repository.id == $head_repo and
			any(.pull_requests[]; .number == $number and .head.sha == $head and .base.repo.id == $repo and .head.repo.id == $head_repo))]
		| max_by(.id) | if . == null then "" else "\(.id) \(.run_attempt) \(.status)" end') || fail 'invalid source run association listing'
	[ "$latest_source" = "$SOURCE_RUN_ID $SOURCE_RUN_ATTEMPT completed" ] || noop 'a different validation run or attempt is latest'
}
latest_trusted() {
	# pull_request_target head_sha can describe the base on GitHub. Bind the
	# PR through the API-owned pull_requests association, never its title.
	runs=$(read_api "repos/$GH_REPO/actions/workflows/ci-control.yml/runs?event=pull_request_target&per_page=100" --paginate --slurp) || exit 1
	printf '%s\n' "$runs" | jq -e 'type == "array" and length > 0 and all(.[]; (.workflow_runs | type) == "array")' >/dev/null || fail 'invalid trusted run listing'
	printf '%s\n' "$runs" | jq -r --argjson number "$pr_number" --arg head "$head" --argjson workflow "$trusted_id" --argjson repo "$repo_id" --argjson head_repo "$head_repo" '
		[.[] | .workflow_runs[] | select(.workflow_id == $workflow and .path == ".github/workflows/ci-control.yml" and .name == "trusted-ci" and
			.event == "pull_request_target" and .repository.id == $repo and any(.pull_requests[];
				.number == $number and .head.sha == $head and .base.repo.id == $repo and .head.repo.id == $head_repo))]
		| max_by(.id) | if . == null then "" else "\(.id) \(.run_attempt) \(.status)" end'
}
check_pr
check_latest_source
trusted=$(latest_trusted) || fail 'cannot select latest trusted gate'
[ -n "$trusted" ] || noop 'no trusted gate exists for this exact PR head'
printf '%s\n' "$trusted" | grep -Eq '^[0-9]+ [0-9]+ (completed|in_progress|queued|waiting|pending|requested)$' || fail 'invalid latest trusted run metadata'
trusted_run=${trusted%% *}
fields=${trusted#* }
trusted_attempt=${fields%% *}
trusted_state=${fields#* }
read_gate_job() {
	jobs=$(read_api "repos/$GH_REPO/actions/runs/$trusted_run/attempts/$trusted_attempt/jobs?per_page=100" --paginate --slurp) || exit 1
	job=$(printf '%s\n' "$jobs" | jq -er --argjson attempt "$trusted_attempt" --argjson run "$trusted_run" '
		[.[] | .jobs[] | select(.name == "ci-required")] |
		if length == 0 then "" elif length != 1 then error("ambiguous required gate job") else .[0] |
			if (.id | type != "number") or (.id <= 0) or (.id | floor != .) or (.run_id != $run) or (.run_attempt != $attempt) then error("invalid required gate job")
			else "\(.id) \(.status) \(.conclusion)" end end') || fail 'invalid exact-attempt gate jobs'
	[ -n "$job" ] || noop 'required gate job is missing'
	job_id=${job%% *}
	job_result=${job#* }
	case "$job_result" in
		'completed failure' | 'completed timed_out' | 'in_progress null') ;;
		'queued null' | 'waiting null' | 'pending null' | 'requested null') noop 'required gate job has not started; it will check completed validation when assigned' ;;
		*) noop 'required gate is successful, skipped, cancelled or otherwise ineligible' ;;
	esac
}
read_gate_job
# A completed fork callback can race the trusted gate's post-job cleanup.
# Workflow status can still be queued while its job is running or failed.
# Allow up to six 15-second intervals within a 90-second settling budget,
# never follow a new attempt or mutate an active job. API requests remain
# subject to the five-minute workflow cap; event-filtered searches cap at 1000 runs.
settle_checks=0
settle_deadline=$(($(date +%s) + 90))
while [ "$trusted_state" != completed ] || [ "$job_result" = 'in_progress null' ]; do
	[ "$settle_checks" -lt 6 ] || noop 'trusted gate remains active after the bounded 90-second settling window'
	settle_remaining=$((settle_deadline - $(date +%s)))
	[ "$settle_remaining" -gt 0 ] || noop 'trusted gate remains active after the settling deadline'
	settle_sleep=15
	[ "$settle_remaining" -ge 15 ] || settle_sleep=$settle_remaining
	sleep "$settle_sleep"
	settle_checks=$((settle_checks + 1))
	check_pr
	check_latest_source
	settled=$(latest_trusted) || fail 'cannot inspect trusted gate during settling'
	case "$settled" in
		"$trusted_run $trusted_attempt "*)
			trusted=$settled
			trusted_state=${settled##* }
			case "$trusted_state" in completed | in_progress | queued | waiting | pending | requested) ;; *) fail 'invalid trusted status during settling' ;; esac
			;;
		*) noop 'trusted run or attempt changed during settling' ;;
	esac
	read_gate_job
done
case "$job_result" in 'completed failure' | 'completed timed_out') ;; *) noop 'required gate job is still active' ;; esac
# Recheck mutable state immediately before the sole mutation. A concurrent
# push after this check still cannot pass the original gate's exact-head check.
check_pr
check_latest_source
current_trusted=$(latest_trusted) || fail 'cannot recheck latest trusted gate'
[ "$current_trusted" = "$trusted" ] || noop 'trusted gate changed while reconciling'
# Never retry a POST: a lost response may have already queued the rerun.
gh api --method POST "repos/$GH_REPO/actions/jobs/$job_id/rerun" >/dev/null || fail 'gate rerun request failed; no mutation retry attempted'
report "Rerun requested for PR #$pr_number at $head: ${GITHUB_SERVER_URL:-https://github.com}/$GH_REPO/actions/runs/$trusted_run (job $job_id). Original approval and exact-head gates still apply."
