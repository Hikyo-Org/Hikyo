#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"

[ "$#" -eq 0 ] || {
	printf 'usage: GitHub release pages JSON | %s\n' "$0" >&2
	exit 2
}

stable=$(jq -r '
  if type != "array" or any(.[]; type != "array") then
    error("expected an array of GitHub release pages")
  else
    [.[][] | select(.draft == false and .prerelease == false)] |
    if length == 0 then
      ""
    elif (.[0].tag_name | type) == "string" then
      .[0].tag_name
    else
      error("stable release has no tag_name")
    end
  end
')

if [ -n "$stable" ]; then
	version=${stable#v}
	if ! is_semver "$version" || [ "${version%%[-+]*}" != "$version" ]; then
		printf 'latest stable version: invalid stable release tag %s\n' "$stable" >&2
		exit 2
	fi
fi
printf '%s\n' "$stable"
