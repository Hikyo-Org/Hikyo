#!/bin/sh
set -eu

if [ "$#" -ne 1 ] || ! printf '%s\n' "$1" | grep -Eq '^[1-9][0-9]*$'; then
	printf 'usage: GitHub matching-refs pages JSON | %s RUN_NUMBER\n' "$0" >&2
	exit 2
fi

run_number=$1
jq -r --arg run "$run_number" '
  if type != "array" or any(.[]; type != "array") then
    error("expected an array of GitHub matching-refs pages")
  else
    [.[][] | select(
      (.ref | type) == "string" and
      (.ref | test(
        "^refs/tags/v[0-9]+\\.[0-9]+\\.[0-9]+-nightly\\.[0-9]{8}\\." +
        $run + "\\.g[0-9a-f]{8}$"
      ))
    ) | .ref | sub("^refs/tags/"; "")] |
    if length == 0 then
      ""
    elif length == 1 then
      .[0]
    else
      error("multiple nightly tags exist for workflow run " + $run)
    end
  end
'
