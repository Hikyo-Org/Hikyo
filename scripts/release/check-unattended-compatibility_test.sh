#!/bin/sh
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-unattended-compat.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
declaration() { # sqlite-mode postgres-mode sqlite-sequence
	jq -nc --arg a "$1" --arg b "$2" --argjson seq "$3" '{engines:[
		{sources:[{source:{genesis:"fresh/v1"},mode:"restart"},{source:{release:{sequence:$seq}},mode:$a}]},
		{sources:[{source:{release:{sequence:2}},mode:$b}]}]}'
}
declaration restart restart 2 >"$scratch/ok.json"
"$script_dir/check-unattended-compatibility.sh" "$scratch/ok.json" 2
for case in "maintenance restart 2" "restart maintenance 2" "restart restart 1"; do
	# shellcheck disable=SC2086
	declaration $case >"$scratch/bad.json"
	if "$script_dir/check-unattended-compatibility.sh" "$scratch/bad.json" 2 >/dev/null 2>&1; then
		printf 'unattended compatibility fixture: accepted %s\n' "$case" >&2; exit 1
	fi
done
if "$script_dir/check-unattended-compatibility.sh" "$scratch/ok.json" 3 >/dev/null 2>&1; then
	printf 'unattended compatibility fixture: accepted a missing previous edge\n' >&2; exit 1
fi
printf 'unattended compatibility fixture: restart edges accepted, breaking declarations refused\n'
