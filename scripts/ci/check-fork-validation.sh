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

fail() {
	printf 'fork validation gate: %s\n' "$1" >&2
	exit 1
}

check_current_pr() {
	pr=$(gh api "repos/$GH_REPO/pulls/$PR_NUMBER" --jq '[.head.sha, (.changed_files | tostring),
		(if .head.repo.id != null and .head.repo.id == .base.repo.id and .head.repo.full_name == .base.repo.full_name then .head.repo.full_name else "fork" end),
		.user.login, (.user.id | tostring), (.mergeable | tostring)] | join(" ")')
	current_head=${pr%% *}
	[ "$current_head" = "$HEAD_SHA" ] ||
		fail "PR head moved from $HEAD_SHA to $current_head; the newer run decides"
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
		fail 'PR has merge conflicts; resolve them before pull_request validation can start'
}

require_workflow_review() {
	check_current_pr
	[ "$head_repository" = "$GH_REPO" ] ||
		fail 'a fork PR that changes .github/ needs a maintainer to land it from a branch in this repository'
	if [ "$pr_author_id" = "$bdfl_author_id" ]; then
		case "$pr_author" in '' | *[!a-zA-Z0-9-]*) fail 'invalid BDFL author identity' ;; esac
		authority=$(gh api "repos/$GH_REPO/collaborators/$pr_author/permission" \
			--jq '[(.user.id | tostring), .permission] | join(" ")') ||
			fail 'cannot verify current BDFL maintainer permission'
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
	reviews=$(gh api --paginate --slurp "repos/$GH_REPO/pulls/$PR_NUMBER/reviews?per_page=100") ||
		fail 'cannot fetch workflow approval reviews'
	# gh rejects --slurp together with --jq. Fetch every page first, then
	# filter locally; keeping the fetch separate also preserves API failures.
	reviewers=$(printf '%s\n' "$reviews" | jq -r \
		--arg head "$HEAD_SHA" --arg author "$pr_author" --argjson author_id "$pr_author_id" '
		[.[][] | select(.user.id != null and .user.type != "Bot")] | group_by(.user.id) |
		map(max_by([(.submitted_at // ""), .id])) | .[] |
		select(.state == "APPROVED" and .commit_id == $head and
		.user.id != $author_id and .user.login != $author) | .user.login') ||
		fail 'cannot verify exact-head workflow approval'
	approved=false
	for reviewer in $reviewers; do
		case "$reviewer" in '' | *[!a-zA-Z0-9-]*) fail 'invalid reviewer identity' ;; esac
		permission=$(gh api "repos/$GH_REPO/collaborators/$reviewer/permission" --jq '.permission') ||
			fail 'cannot verify current maintainer permission'
		case "$permission" in write | maintain | admin) approved=true; break ;; esac
	done
	[ "$approved" = true ] ||
		fail 'workflow edits require an independent current maintainer approval on this exact PR head'
	check_current_pr
}

check_current_pr

# The files endpoint lists at most 3000 entries. A shorter list than
# changed_files could hide a workflow change, so it fails closed.
files=$(gh api --paginate "repos/$GH_REPO/pulls/$PR_NUMBER/files?per_page=100" \
	--jq '.[] | [.filename, (.previous_filename // "")] | @tsv')
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
	run=$(gh api "repos/$GH_REPO/actions/workflows/ci-fork.yml/runs?event=pull_request&head_sha=$HEAD_SHA&per_page=100" \
		--jq "[.workflow_runs[] | select(.display_title == \"fork-ci #$PR_NUMBER\")][0] // empty | \"\\(.id) \\(.status) \\(.conclusion)\"")
	run_id=${run%% *}
	state=${run#* }
	# A run awaiting maintainer approval reports completed/action_required
	# with no jobs yet; it is still pending, not a result.
	if [ -n "$run" ] && [ "${state% *}" = completed ] && [ "${state#* }" != action_required ]; then
		conclusion=$(gh api --paginate "repos/$GH_REPO/actions/runs/$run_id/jobs?filter=latest&per_page=100" \
			--jq ".jobs[] | select(.name == \"$gate_job\") | .conclusion")
		[ "$conclusion" = success ] ||
			fail "fork-ci run $run_id: '$gate_job' concluded '${conclusion:-missing}'"
		# Polling may outlive a push, a dismissed review, or collaborator removal.
		# Re-prove workflow-edit authority and the head immediately before passing.
		if [ "$workflow_changed" = true ]; then require_workflow_review; fi
		check_current_pr
		if [ "$mergeable" = true ]; then
			printf 'fork validation gate: fork-ci run %s passed on %s\n' "$run_id" "$HEAD_SHA"
			exit 0
		fi
		# A completed run does not resolve GitHub's pending mergeability.
		[ "$(date +%s)" -lt "$deadline" ] ||
			fail "PR mergeability is still pending after ${timeout_seconds}s; re-run once GitHub resolves it"
	fi
	[ "$(date +%s)" -lt "$deadline" ] ||
		fail "no completed fork-ci run for $HEAD_SHA within ${timeout_seconds}s (every fork run needs maintainer approval); re-run this job once it finishes"
	printf 'fork validation gate: waiting for fork-ci on %s (%s)\n' "$HEAD_SHA" "${run:-no run yet}"
	sleep "$poll_seconds"
done
