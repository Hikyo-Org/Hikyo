#!/bin/sh
set -eu

: "${GH_BIN:=gh}"

[ "$#" -eq 2 ] || {
	printf 'usage: %s OWNER/REPO COMMIT\n' "$0" >&2
	exit 2
}

repository=$1
commit=$2
if ! printf '%s\n' "$repository" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$'; then
	printf 'green main: invalid repository %s\n' "$repository" >&2
	exit 2
fi
if ! printf '%s\n' "$commit" | grep -Eq '^[0-9a-f]{40}$'; then
	printf 'green main: commit must be a full lowercase SHA-1\n' >&2
	exit 2
fi

manual_runs=$($GH_BIN api \
	"repos/$repository/actions/workflows/ci-main.yml/runs?branch=main&head_sha=$commit&per_page=1")
legacy_runs=$($GH_BIN api \
	"repos/$repository/actions/workflows/ci.yml/runs?branch=main&head_sha=$commit&per_page=1")
# Compare each known workflow's latest exact-SHA run. Unrelated callbacks cannot
# crowd out proof, and a newer failed/queued run cannot hide behind an old pass.
if ! jq -ne --arg commit "$commit" --argjson manual "$manual_runs" --argjson legacy "$legacy_runs" '
	def valid_latest($path):
		.total_count as $total | .workflow_runs as $runs |
		($total | type == "number" and . >= 0 and floor == .) and
		($runs | type == "array" and length == (if $total == 0 then 0 else 1 end)) and
		all($runs[];
			(.id | type == "number" and . > 0 and floor == .) and
			.path == $path and .head_sha == $commit and .head_branch == "main");
	($manual | valid_latest(".github/workflows/ci-main.yml")) and
	($legacy | valid_latest(".github/workflows/ci.yml")) and
	($manual.workflow_runs + $legacy.workflow_runs | sort_by(.id) | reverse | .[0] |
	 ((.path == ".github/workflows/ci-main.yml" and .event == "workflow_dispatch") or
	  (.path == ".github/workflows/ci.yml" and (.event == "workflow_dispatch" or .event == "push"))) and
	 .status == "completed" and .conclusion == "success")
' >/dev/null; then
	printf 'green main: exact commit %s needs successful full main CI; run gh workflow run ci-main.yml --ref main\n' "$commit" >&2
	exit 1
fi

printf 'green main: exact commit %s passed CI\n' "$commit"
