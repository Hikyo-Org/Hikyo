#!/bin/sh
# Decide whether a pull request must run the supply-chain-checks fixtures.
# Run in a checkout of the PR's merge commit, whose first parent is the base:
#   select-supply-chain-fixtures.sh [<base> [<head>]]   (defaults HEAD^1, HEAD)
# Prints `run=true` or `run=false`. It selects when the diff touches an exact
# fixture input listed in scripts/ci/supply-chain-fixture-inputs.txt (an entry
# ending in `/` is a directory prefix), or changes the steps of ci.yml's
# supply-chain-checks job. supply_chain_inputs_test.go derives the inputs from
# that job and fails when the list misses one.
set -eu

base=${1:-HEAD^1}
head=${2:-HEAD}
list=scripts/ci/supply-chain-fixture-inputs.txt
workflow=.github/workflows/ci.yml

# Everything from the job key to the next two-space-indented line. A change to a
# shared anchor defined outside the job is not seen; it still reaches ci-dev.
job_block() {
	git show "$1:$workflow" | awk '
		/^  supply-chain-checks:$/ { in_job = 1; next }
		in_job && /^  [^ ]/ { exit }
		in_job { print }
	'
}

# The list comes from the merge commit, so a PR that adds an input also selects.
entries=$(git show "$head:$list")
[ -n "$entries" ] || { echo 'supply-chain fixture input list is empty' >&2; exit 1; }

changed=$(git diff --name-only "$base" "$head")
selected=false
while IFS= read -r path; do
	[ -n "$path" ] || continue
	while IFS= read -r entry; do
		case "$entry" in
		'' | '#'*) continue ;;
		*/) case "$path" in "$entry"*) selected=true ;; esac ;;
		*) [ "$path" = "$entry" ] && selected=true ;;
		esac
	done <<EOF
$entries
EOF
	[ "$selected" = true ] && break
done <<EOF
$changed
EOF

if [ "$selected" = false ]; then
	before=$(job_block "$base")
	after=$(job_block "$head")
	[ -n "$after" ] || { echo "$workflow lost the supply-chain-checks job" >&2; exit 1; }
	[ "$before" = "$after" ] || selected=true
fi
printf 'run=%s\n' "$selected"
