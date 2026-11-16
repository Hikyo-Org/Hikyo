#!/bin/sh
set -eu
[ "$#" -eq 2 ] || { printf 'usage: %s REPOSITORY AUTHENTICATED_METADATA\n' "$0" >&2; exit 2; }
repository=$1 metadata=$2
: "${GH_BIN:=gh}"
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
printf '%s\n' "$repository" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || exit 2
# Include published prereleases because stable trust sequences cover both.
# Authenticate metadata with stable preflight before invoking this helper.
scratch=$(mktemp "${TMPDIR:-/tmp}/hikyo-published-stable.XXXXXX") || { printf 'release: cannot create a scratch file\n' >&2; exit 2; }
trap 'rm -f "$scratch"' EXIT HUP INT TERM
# Exit 1 means "not synchronized" and nothing else: a read that fails, or a list that cannot
# be read, exits 2, so a caller never mistakes "could not look" for a refusal.
"$GH_BIN" api --paginate --slurp "repos/$repository/releases?per_page=100" >"$scratch" || { printf 'release: cannot list the public releases of %s\n' "$repository" >&2; exit 2; }
rc=0
# release_semver_pattern comes from the shared release library.
# shellcheck disable=SC2154
jq -e --slurpfile metadata "$metadata" --arg pattern "^v${release_semver_pattern#^}" '
  [ .[][] | select((.tag_name | test($pattern)) and
      (.tag_name | contains("-nightly.") | not)) ] as $stable |
  [ $stable[] | select(.draft == false) | .tag_name | ltrimstr("v") ] as $published |
  ($metadata[0].releases | map(.version)) as $known |
  all($stable[]; .draft == false) and
  all($published[]; . as $version | $known | index($version) != null) and
  ($metadata[0].highest_release == null or
    ($metadata[0].highest_release as $highest | $published | index($highest) != null))
' "$scratch" >/dev/null || rc=$?
case $rc in
0) ;;
1) printf 'release: an outstanding stable draft or unsynchronized public release exists; finish the draft and run ceremony sync-trust before another candidate\n' >&2; exit 1 ;;
*) printf 'release: cannot read the public release list or the trust metadata\n' >&2; exit 2 ;;
esac
printf 'release: published versions and authenticated trust metadata are synchronized\n'
