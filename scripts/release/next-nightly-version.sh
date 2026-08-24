#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"

[ "$#" -eq 4 ] || {
	printf 'usage: %s STABLE_VERSION YYYYMMDD RUN_NUMBER COMMIT\n' "$0" >&2
	exit 2
}

stable=${1#v}
date=$2
run=$3
commit=$4

if ! is_semver "$stable" || [ "${stable%%[-+]*}" != "$stable" ]; then
	printf 'next nightly version: %s is not a stable SemVer version\n' "$1" >&2
	exit 2
fi
major=${stable%%.*}
remainder=${stable#*.}
minor=${remainder%%.*}
next_minor=$((minor + 1))
exec "$script_dir/nightly-version.sh" \
	"$major.$next_minor.0" "$date" "$run" "$commit"
