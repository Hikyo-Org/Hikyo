#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"

[ "$#" -eq 4 ] || {
	printf 'usage: %s VERSION YYYYMMDD RUN_NUMBER COMMIT\n' "$0" >&2
	exit 2
}

version=${1#v}
date=$2
run=$3
commit=$4

if ! is_semver "$version" || [ "${version%%[-+]*}" != "$version" ]; then
	printf 'nightly version: %s is not a stable SemVer version\n' "$1" >&2
	exit 2
fi
if ! printf '%s\n' "$date" | grep -Eq '^[0-9]{8}$'; then
	printf 'nightly version: date must be YYYYMMDD\n' >&2
	exit 2
fi
if ! printf '%s\n' "$run" | grep -Eq '^[1-9][0-9]*$'; then
	printf 'nightly version: run number must be a positive integer\n' >&2
	exit 2
fi
if ! printf '%s\n' "$commit" | grep -Eq '^[0-9a-f]{40}$'; then
	printf 'nightly version: commit must be a full lowercase SHA-1\n' >&2
	exit 2
fi

short_commit=$(printf '%s' "$commit" | cut -c1-8)
printf '%s-nightly.%s.%s.g%s\n' "$version" "$date" "$run" "$short_commit"
