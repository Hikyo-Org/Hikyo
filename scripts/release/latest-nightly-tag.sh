#!/bin/sh
set -eu

# Prints the newest published signed-or-unsigned nightly prerelease tags,
# newest first, one per line. --limit N (default 1) bounds the count.
limit=1
if [ "$#" -eq 2 ] && [ "$1" = "--limit" ]; then
	limit=$2
	shift 2
fi
[ "$#" -eq 0 ] || {
	printf 'usage: GitHub release pages JSON | %s [--limit N]\n' "$0" >&2
	exit 2
}
case "$limit" in '' | *[!0-9]* | 0) printf 'latest nightly tag: positive --limit required\n' >&2; exit 2 ;; esac

jq -r --argjson limit "$limit" '
  if type != "array" or any(.[]; type != "array") then
    error("expected an array of GitHub release pages")
  else
    [.[][] | select(
      .draft == false and
      .prerelease == true and
      (.tag_name | type) == "string" and
      (.tag_name | test("^v[0-9]+\\.[0-9]+\\.[0-9]+-nightly\\."))
    )] |
    .[:$limit][].tag_name
  end
'
