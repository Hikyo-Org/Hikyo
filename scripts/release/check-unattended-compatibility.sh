#!/bin/sh
# An unattended patch release must be a plain restart upgrade from the previous
# stable release: in both engines the declaration needs an edge from that release
# (matched by sequence) whose mode is restart, which the declaration validator
# only permits when migrations and schema digest are unchanged. A maintenance
# edge, a changed schema or a missing edge is a breaking change and stops the
# unattended release.
set -eu
[ "$#" -eq 2 ] || { printf 'usage: %s DECLARATION PREVIOUS_RELEASE_SEQUENCE\n' "$0" >&2; exit 2; }
declaration=$1 previous=$2
printf '%s\n' "$previous" | grep -Eq '^[1-9][0-9]*$' || exit 2
jq -e --argjson previous "$previous" '
	(.engines | length) == 2 and
	all(.engines[];
		[.sources[] | select(.source.release.sequence == $previous)] as $edges |
		($edges | length) == 1 and $edges[0].mode == "restart")
' "$declaration" >/dev/null || {
	printf 'unattended release: the compatibility declaration is not a restart upgrade from the previous stable release; a maintainer must release this version\n' >&2
	exit 1
}
