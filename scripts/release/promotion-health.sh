#!/bin/sh
# Fails the nightly run while nightly-promote.yml is failing or has stopped
# running, so publishing from a stale main cannot report a misleading success.
# Reads only, with the workflow token. Runs from main's own checkout.
set -eu

: "${GH_REPO:?}" "${GH_TOKEN:?}"
GH_BIN=${GH_BIN:-gh}
server=${GITHUB_SERVER_URL:-https://github.com}
# 36 hours, not 24: GitHub starts scheduled runs 4 to 10 hours late, and one
# promote-to-nightly gap measured 30h53m.
window=129600
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-promotion-health.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

# Workflow commands are read from stdout; only validated values reach them.
fail() {
	printf '::error::%s\n' "$*"
	exit 1
}

printf '%s\n' "$GH_REPO" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || fail 'invalid repository'
now=${PROMOTION_HEALTH_NOW:-$(date +%s)}
printf '%s\n' "$now" | grep -Eq '^[0-9]{1,12}$' || fail 'invalid clock'
since=$((now - window))
# jq formats the timestamp, since GNU and BSD date disagree on how.
since_iso=$(jq -nr "$since | todate")

# The window bounds the listing, which every merge to main also adds runs to. The
# landing trigger is the only other event, and its runs are not promotions.
"$GH_BIN" api --paginate "repos/$GH_REPO/actions/workflows/nightly-promote.yml/runs?branch=main&status=completed&created=%3E%3D$since_iso&per_page=100" --jq \
	".workflow_runs[] | select(.event != \"pull_request_target\" and ((.created_at | fromdateiso8601?) // 0) >= $since) | [.created_at, .id, (.conclusion // \"-\")] | @tsv" \
	>"$work/runs" 2>"$work/err" || {
	# gh's reason, kept from being read as a workflow command.
	head -n 5 "$work/err" | tr -cd '[:print:]\n' | sed 's/::/: :/g' >&2
	fail 'nightly-promote.yml runs cannot be read, so promotion health is unknown'
}
latest=$(sort "$work/runs" | tail -n 1)
[ -n "$latest" ] || fail 'nightly-promote.yml has no completed scheduled or manual run on main in 36 hours, so promotion has stopped running.'
tab=$(printf '\t')
IFS=$tab read -r _ run_id conclusion <<EOF
$latest
EOF
printf '%s\n' "$run_id" | grep -Eq '^[1-9][0-9]*$' || fail 'nightly-promote.yml returned an unexpected run record.'
printf '%s\n' "$conclusion" | grep -Eq '^[a-z_-]+$' || fail 'nightly-promote.yml returned an unexpected run record.'
url="$server/$GH_REPO/actions/runs/$run_id"
[ "$conclusion" = success ] || fail "The latest nightly-promote.yml run ended '$conclusion', so main has stopped taking dev: $url"
printf 'Promotion is healthy: %s\n' "$url"
