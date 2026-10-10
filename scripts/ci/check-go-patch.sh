#!/bin/sh
# Propose stable patch updates without modifying the checkout or authoring commits.
set -eu

proposal_dir=${GO_PATCH_PROPOSAL_DIR:-${RUNNER_TEMP:-${TMPDIR:-/tmp}}/go-patch-proposal}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM

fail() {
	printf 'Go patch check: %s\n' "$1" >&2
	exit 2
}

[ -f go.mod ] || fail 'run from the repository root containing go.mod'
current=$(awk '$1 == "go" { print $2; count++ } END { if (count != 1) exit 1 }' go.mod) ||
	fail 'go.mod must contain exactly one go directive'
printf '%s\n' "$current" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' ||
	fail 'go.mod must pin an exact stable Go patch version'

# Keep this source fixed. curl uses normal CA verification and fails on HTTP errors.
curl --fail --silent --show-error --retry 3 --proto '=https' --tlsv1.2 \
	'https://go.dev/dl/?mode=json' >"$work/releases.json" || fail 'official release metadata download failed'
latest=$(jq -ers --arg current "$current" '
	if length == 1 then .[0] else error("expected exactly one JSON document") end
	| if type != "array" or length == 0 then error("expected nonempty release array") else . end
	| if all(.[]; type == "object" and (.version | type == "string") and (.stable | type == "boolean"))
	  then . else error("invalid release metadata") end
	| if all(.[] | select(.stable); .version | test("^go[0-9]+\\.[0-9]+\\.[0-9]+$"))
	  then . else error("invalid stable release version") end
	| ($current | split(".") | map(tonumber)) as $pin
	| [ .[] | select(.stable) | .version | ltrimstr("go") | split(".") | map(tonumber)
	    | select(.[0] == $pin[0] and .[1] == $pin[1]) ]
	| if length == 0 then error("no stable release on the pinned major.minor line") else max_by(.[2]) end
	| map(tostring) | join(".")
' "$work/releases.json") || fail 'official release metadata is invalid or lacks the pinned stable line'

current_patch=${current##*.}
latest_patch=${latest##*.}
[ "$latest_patch" -ge "$current_patch" ] || fail "official latest $latest is older than pinned $current; refusing a downgrade"

if [ "$latest" = "$current" ]; then
	printf 'Go patch check: pinned Go %s is current on its stable release line\n' "$current"
	if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
		printf 'Go %s is the latest stable patch on its pinned major.minor line.\n' "$current" >>"$GITHUB_STEP_SUMMARY"
	fi
	exit 0
fi

mkdir -p "$proposal_dir"
awk -v latest="$latest" '$1 == "go" { print "go " latest; next } { print }' go.mod >"$work/go.mod"
diff_status=0
diff -u -L a/go.mod -L b/go.mod go.mod "$work/go.mod" >"$proposal_dir/go.mod.patch" || diff_status=$?
[ "$diff_status" -eq 1 ] || fail 'could not generate the proposed go.mod patch'
{
	printf 'Go patch update proposal\n\n'
	# Markdown code delimiters below are literal text.
	# shellcheck disable=SC2016
	printf 'Replace `go %s` with `go %s` in go.mod.\n\n' "$current" "$latest"
	printf 'Source: https://go.dev/dl/?mode=json\n\n'
	printf 'This is the newest stable patch on the pinned major.minor line. Review and apply go.mod.patch, then rerun validation and rebuild release binaries.\n\n'
	printf 'A Go patch can fix standard-library vulnerabilities, so govulncheck results may change with the compiler and standard library used to build each binary. Updating go.mod does not repair previously built artifacts.\n'
} >"$proposal_dir/README.md"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	cat "$proposal_dir/README.md" >>"$GITHUB_STEP_SUMMARY"
fi
printf 'Go patch check: stale pin %s; proposed %s in %s/go.mod.patch\n' "$current" "$latest" "$proposal_dir" >&2
exit 1
