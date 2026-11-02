#!/bin/sh
set -eu

# Prints the most recently published signed-or-unsigned nightly prerelease tags,
# newest first, one per line. --limit N (default 1) bounds the count. The order is
# the tag's date and run number, as nightly-image.sh selects the moving tag, never
# the listing order: GitHub orders releases by the commit date of their source, so
# a nightly built from an older dev commit would otherwise look older than a
# nightly published before it.
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
      (.tag_name | type) == "string"
    ) | .tag_name | . as $tag | try (capture("^v[0-9]+\\.[0-9]+\\.[0-9]+-nightly\\.(?<date>[0-9]{8})\\.(?<sequence>[1-9][0-9]*)\\.g[0-9a-f]{8}$") | .tag = $tag)] |
    sort_by([(.date | tonumber), (.sequence | tonumber)]) | reverse |
    .[:$limit][].tag
  end
'
