#!/bin/sh
set -eu

[ "$#" -eq 0 ] || {
	printf 'usage: GitHub release pages JSON | %s\n' "$0" >&2
	exit 2
}

jq -r '
  if type != "array" or any(.[]; type != "array") then
    error("expected an array of GitHub release pages")
  else
    [.[][] | select(
      .draft == false and
      .prerelease == true and
      (.tag_name | type) == "string" and
      (.tag_name | test("^v[0-9]+\\.[0-9]+\\.[0-9]+-nightly\\."))
    )] |
    if length == 0 then
      ""
    else
      .[0].tag_name
    end
  end
'
