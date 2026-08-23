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

runs=$($GH_BIN api \
	"repos/$repository/actions/workflows/ci.yml/runs?branch=main&event=push&head_sha=$commit&per_page=1")
if ! printf '%s\n' "$runs" | jq -e --arg commit "$commit" '
	.workflow_runs | type == "array" and length == 1 and
	.[0].head_sha == $commit and .[0].conclusion == "success"
' >/dev/null; then
	printf 'green main: exact commit %s has no successful main CI run\n' "$commit" >&2
	exit 1
fi

printf 'green main: exact commit %s passed CI\n' "$commit"
