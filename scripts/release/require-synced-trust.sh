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
# Exit 1 = unsynchronized or outstanding draft (a refusal); exit 4 = the listing failed.
scratch=$(mktemp "${TMPDIR:-/tmp}/hikyo-published-stable.XXXXXX")
trap 'rm -f "$scratch"' EXIT HUP INT TERM
"$GH_BIN" api --paginate --slurp "repos/$repository/releases?per_page=100" >"$scratch" ||
  { printf 'release: cannot list the public releases\n' >&2; exit 4; }
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
' "$scratch" >/dev/null || { printf 'release: an outstanding stable draft or unsynchronized public release exists; finish the draft and run ceremony sync-trust before another candidate\n' >&2; exit 1; }
printf 'release: published versions and authenticated trust metadata are synchronized\n'
