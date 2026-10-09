#!/bin/sh
# Trusted merge gate for untrusted pull-request validation (#813). trusted-ci runs this from
# the base branch under pull_request_target; it never checks out or executes PR
# code. The fork's validation ran untrusted under pull_request (ci-fork.yml),
# from YAML in the PR's merge ref. Accept that result only when:
#   1. the PR head is still HEAD_SHA (a newer push gets its own gate),
#   2. .github/ is untouched, or a same-repository PR has an independent,
#      currently authorized maintainer's latest approval on this exact head,
#      or is authored by the pinned BDFL who still has maintainer permission,
#   3. fork-ci's latest run for this PR (run title "fork-ci #N") and HEAD_SHA
#      completed, and its aggregate gate ("validation / ci-required")
#      succeeded. Another PR sharing the commit may target a different base,
#      so a run is never borrowed across PRs.
# Anything else fails closed.
set -eu

: "${GH_REPO:?}" "${PR_NUMBER:?}" "${HEAD_SHA:?}"
timeout_seconds=${FORK_GATE_TIMEOUT_SECONDS:-5400}
poll_seconds=${FORK_GATE_POLL_SECONDS:-30}
gate_job='validation / ci-required'
# Repository policy, loaded from the trusted base, never from PR input or env.
# Dunky13's immutable GitHub user ID survives account renames.
bdfl_author_id=991668

# Retries apply only to read-only API transport failures. Policy checks and
# malformed responses are never retried or turned into acceptance.
api_work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-fork-api.XXXXXX")
trap 'rm -rf "$api_work"' EXIT HUP INT TERM
run_url=

summary() {
	[ -n "${GITHUB_STEP_SUMMARY:-}" ] || return 0
	printf '%s\n' "$1" >>"$GITHUB_STEP_SUMMARY"
}

fail() {
	category=${2:-policy-refusal}
	printf 'fork validation gate [%s]: %s\n' "$category" "$1" >&2
	summary "### Fork validation gate: $category"
	summary "$1"
	[ -z "$run_url" ] || summary "Validation run: $run_url"
	exit 1
}

# gh supports --paginate and --slurp but not --slurp with --jq. Keep the
# transport and JSON filtering separate, and require actual JSON even when an
# HTTP proxy returns a successful HTML/text response.
gh_read() (
	filter=.
	url=$1
	shift
	paginate=false
	slurp=false
	while [ "$#" -gt 0 ]; do
		case "$1" in
			--paginate) paginate=true ;;
			--slurp) slurp=true ;;
			--jq) filter=$2; shift ;;
			*) fail 'unsupported read-only API option' api-failure ;;
		esac
		shift
	done
	set -- api "$url"
	[ "$paginate" = false ] || set -- "$@" --paginate
	[ "$slurp" = false ] || set -- "$@" --slurp
	attempt=1
	while :; do
		if gh "$@" >"$api_work/body" 2>"$api_work/error"; then
			# jq empty accepts an empty stream; require at least one JSON value.
			jq -e -s 'length > 0' "$api_work/body" >/dev/null 2>&1 ||
				fail "malformed API response from $url" api-failure
			jq -r "$filter" "$api_work/body" ||
				fail "invalid API data from $url" api-failure
			exit 0
		fi
		# HTTP codes from gh and specific network transport failures only.
		if [ "$attempt" -ge 3 ] || ! grep -Ei 'HTTP (429|5[0-9][0-9])|dial tcp|TLS handshake timeout|connection reset|i/o timeout|unexpected EOF|temporary failure in name resolution' "$api_work/error" >/dev/null; then
			fail "read-only API request failed for $url (attempt $attempt)" api-failure
		fi
		printf 'fork validation gate: retrying transient API failure (%s/3) for %s\n' "$attempt" "$url" >&2
		sleep "${FORK_GATE_API_RETRY_SECONDS:-2}"
		attempt=$((attempt + 1))
	done
)

check_current_pr() {
	pr=$(gh_read "repos/$GH_REPO/pulls/$PR_NUMBER" --jq '[.head.sha, (.changed_files | tostring),
		(if .head.repo.id != null and .head.repo.id == .base.repo.id and .head.repo.full_name == .base.repo.full_name then .head.repo.full_name else "fork" end),
		.user.login, (.user.id | tostring), (.mergeable | tostring)] | join(" ")') || fail 'cannot read current PR metadata' api-failure
	current_head=${pr%% *}
	printf '%s\n' "$pr" | grep -Eq '^[0-9a-f]{40} [0-9]+ [^ ]+ [a-zA-Z0-9-]+(\[bot\])? [0-9]+ (true|false|null)$' ||
		fail 'invalid PR metadata from API' api-failure
	[ "$current_head" = "$HEAD_SHA" ] ||
		fail "PR head moved from $HEAD_SHA to $current_head; the newer run decides" superseded
	fields=${pr#* }
	changed_files=${fields%% *}
	fields=${fields#* }
	head_repository=${fields%% *}
	fields=${fields#* }
	pr_author=${fields%% *}
	fields=${fields#* }
	pr_author_id=${fields%% *}
	mergeable=${fields#* }
	# pull_request validation cannot start while GitHub reports conflicts.
	# null means GitHub is still computing mergeability; keep polling then.
	[ "$mergeable" != false ] ||
		fail 'PR has merge conflicts; resolve them before pull_request validation can start' merge-conflict
}

require_workflow_review() {
	check_current_pr
	[ "$head_repository" = "$GH_REPO" ] ||
		fail 'a fork PR that changes .github/ needs a maintainer to land it from a branch in this repository'
	if [ "$pr_author_id" = "$bdfl_author_id" ]; then
		case "$pr_author" in '' | *[!a-zA-Z0-9-]*) fail 'invalid BDFL author identity' ;; esac
		authority=$(gh_read "repos/$GH_REPO/collaborators/$pr_author/permission" \
			--jq '[(.user.id | tostring), .permission] | join(" ")') ||
			fail 'cannot verify current BDFL maintainer permission' api-failure
		[ "${authority%% *}" = "$bdfl_author_id" ] ||
			fail 'BDFL permission lookup returned a different user identity'
		case "${authority#* }" in
			write | maintain | admin) ;;
			*) fail 'BDFL workflow exemption requires current maintainer permission' ;;
		esac
		check_current_pr
		return
	fi
	# API review author_association is historical, not current authority. Use
	# the latest review per immutable reviewer id and check current permissions.
	reviews=$(gh_read "repos/$GH_REPO/pulls/$PR_NUMBER/reviews?per_page=100" --paginate --slurp) ||
		fail 'cannot fetch workflow approval reviews' api-failure
	# gh rejects --slurp together with --jq. Fetch every page first, then
	# filter locally; keeping the fetch separate also preserves API failures.
	reviewers=$(printf '%s\n' "$reviews" | jq -r \
		--arg head "$HEAD_SHA" --arg author "$pr_author" --argjson author_id "$pr_author_id" '
		[.[][] | select(.user.id != null and .user.type != "Bot")] | group_by(.user.id) |
		map(max_by([(.submitted_at // ""), .id])) | .[] |
		select(.state == "APPROVED" and .commit_id == $head and
		.user.id != $author_id and .user.login != $author) | .user.login') ||
		fail 'cannot verify exact-head workflow approval' api-failure
	approved=false
	for reviewer in $reviewers; do
		case "$reviewer" in '' | *[!a-zA-Z0-9-]*) fail 'invalid reviewer identity' ;; esac
		permission=$(gh_read "repos/$GH_REPO/collaborators/$reviewer/permission" --jq '.permission') ||
			fail 'cannot verify current maintainer permission' api-failure
		case "$permission" in write | maintain | admin) approved=true; break ;; esac
	done
	[ "$approved" = true ] ||
		fail 'workflow edits require an independent current maintainer approval on this exact PR head'
	check_current_pr
}

check_current_pr

# The files endpoint lists at most 3000 entries. A shorter list than
# changed_files could hide a workflow change, so it fails closed.
files=$(gh_read "repos/$GH_REPO/pulls/$PR_NUMBER/files?per_page=100" --paginate \
	--jq 'if type != "array" or any(.[]; (.filename | type) != "string" or .filename == "" or (.previous_filename != null and (.previous_filename | type) != "string")) then error("invalid changed files") else .[] | [.filename, (.previous_filename // "")] | @tsv end') || fail 'cannot read changed files' api-failure
listed=$(printf '%s\n' "$files" | grep -c . || true)
[ "$listed" -ge "$changed_files" ] ||
	fail "listed $listed of $changed_files changed files; cannot prove .github/ is untouched"
workflow_changed=false
if printf '%s\n' "$files" | tr '\t' '\n' | grep '^\.github/' >&2; then
	workflow_changed=true
	require_workflow_review
fi

deadline=$(($(date +%s) + timeout_seconds))
while :; do
	check_current_pr
	run=$(gh_read "repos/$GH_REPO/actions/workflows/ci-fork.yml/runs?event=pull_request&head_sha=$HEAD_SHA&per_page=100" \
		--jq "[.workflow_runs[] | select(.display_title == \"fork-ci #$PR_NUMBER\")][0] // empty | \"\\(.id) \\(.status) \\(.conclusion)\"") || fail 'cannot read validation runs' api-failure
	if [ -n "$run" ]; then
		printf '%s\n' "$run" | grep -Eq '^[0-9]+ (queued|in_progress|completed|waiting|pending|requested) [a-z_]+$' ||
			fail 'invalid validation run metadata' api-failure
	fi
	run_id=${run%% *}
	state=${run#* }
	wait_reason='no run has started'
	[ -z "$run" ] || wait_reason="run $run_id is ${state% *}"
	if [ "${state#* }" = action_required ]; then
		wait_reason="run $run_id needs maintainer approval"
	fi
	# A run awaiting maintainer approval reports completed/action_required
	# with no jobs yet; it is still pending, not a result.
	if [ -n "$run" ] && [ "${state% *}" = completed ] && [ "${state#* }" != action_required ]; then
		run_url="${GITHUB_SERVER_URL:-https://github.com}/$GH_REPO/actions/runs/$run_id"
		jobs=$(gh_read "repos/$GH_REPO/actions/runs/$run_id/jobs?filter=latest&per_page=100" --paginate) ||
			fail 'cannot read validation jobs' api-failure
		printf '%s\n' "$jobs" | jq -e -s 'all(.[]; (.jobs | type) == "array")' >/dev/null ||
			fail 'invalid validation jobs response' api-failure
		conclusion=$(printf '%s\n' "$jobs" | jq -r --arg gate "$gate_job" '.jobs[] | select(.name == $gate) | .conclusion')
		if [ "$conclusion" != success ]; then
			category=validation-failure
			# Report every upstream failure; aggregate and cancelled sibling jobs
			# are consequences, not necessarily the root cause.
			roots=$(printf '%s\n' "$jobs" | jq -r --arg gate "$gate_job" --arg url "$run_url" '
				.jobs[] | select(.name != $gate and (.conclusion == "failure" or .conclusion == "timed_out" or .conclusion == "startup_failure")) |
				"- \(.name): \(.conclusion) (\($url)/job/\(.id))"')
			[ -z "$roots" ] || { printf '%s\n' "$roots" >&2; summary "Failing validation jobs:"; summary "$roots"; }
			if printf '%s\n' "$jobs" | jq -e '.jobs[] | select(.conclusion == "timed_out")' >/dev/null; then
				category=timeout
			fi
			case "${state#* }/$conclusion" in
				cancelled/* | */cancelled) category=cancelled ;;
				timed_out/* | */timed_out) category=timeout ;;
				*) ;;
			esac
			fail "fork-ci run $run_id: '$gate_job' concluded '${conclusion:-missing}'" "$category"
		fi
		# Polling may outlive a push, a dismissed review, or collaborator removal.
		# Re-prove workflow-edit authority and the head immediately before passing.
		if [ "$workflow_changed" = true ]; then require_workflow_review; fi
		check_current_pr
		if [ "$mergeable" = true ]; then
			summary "### Fork validation gate: success"
			summary "Exact head: $HEAD_SHA. Validation run: $run_url"
			printf 'fork validation gate: fork-ci run %s passed on %s\n' "$run_id" "$HEAD_SHA"
			exit 0
		fi
		# A completed run does not resolve GitHub's pending mergeability.
		[ "$(date +%s)" -lt "$deadline" ] ||
			fail "PR mergeability is still pending after ${timeout_seconds}s; re-run once GitHub resolves it" timeout
	fi
	[ "$(date +%s)" -lt "$deadline" ] ||
		fail "no completed fork-ci run for $HEAD_SHA within ${timeout_seconds}s ($wait_reason); re-run this job once it finishes" timeout
	printf 'fork validation gate: waiting for fork-ci on %s (%s)\n' "$HEAD_SHA" "${run:-no run yet}"
	sleep "$poll_seconds"
done
