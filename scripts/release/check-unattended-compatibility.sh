#!/bin/sh
# An unattended patch release must not change upgrade compatibility. The
# candidate's generated upgrade-compatibility.json is compared with the one the
# previous stable release published: normalized with jq -S, minus only the
# version, sequence and source commit, the two documents must be byte-identical.
# Anything else (a migration, a schema digest, a source edge, a field that does
# not exist today) differs, so the release stays with a maintainer. The same
# script runs at plan time (before a tag is created) and at build time.
#
# usage: check-unattended-compatibility.sh REPOSITORY TRUST_DIR VERSION DECLARATION
#   REPOSITORY   OWNER/REPO that published the previous release
#   TRUST_DIR    release/trust at the commit that is (or will be) tagged
#   VERSION      the candidate's plain X.Y.Z
#   DECLARATION  the candidate's generated upgrade-compatibility.json
#
# The baseline is the highest plain stable version in the authenticated trust
# metadata below VERSION: no prerelease, and not whatever GitHub flags "latest".
# Both checks read the same trust, so they resolve the same tag. The baseline is
# fetched through the signature-verified release path (stable verify) and its
# declaration is bound to the signed manifest, never read as a loose asset.
#
# Exit 1: the release must be made by a maintainer (difference, missing or
# unverifiable baseline). Exit 4: a lookup failed; nothing is decided.
set -eu
: "${GH_BIN:=gh}" "${GO_BIN:=go}"
[ "$#" -eq 4 ] || { printf 'usage: %s REPOSITORY TRUST_DIR VERSION DECLARATION\n' "$0" >&2; exit 2; }
repository=$1 trust=$2 version=$3 declaration=$4
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
printf '%s\n' "$repository" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || exit 2
printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || exit 2
[ -d "$trust" ] && [ -f "$declaration" ] || exit 2
trust=$(CDPATH='' cd -- "$trust" && pwd)
declaration=$(CDPATH='' cd -- "$(dirname "$declaration")" && pwd)/$(basename "$declaration")
refuse() {
	printf 'unattended release: %s; a maintainer must release this version\n' "$*" >&2
	exit 1
}
lookup_failed() {
	printf 'unattended release: %s\n' "$*" >&2
	exit 4
}
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-unattended-compat.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

validate_compatibility_shape "$declaration" || refuse 'the candidate declaration is not a complete two-engine declaration'
# Authenticate the trust before reading which release is the baseline.
state="$work/release-trust-state.json"
"$GO_BIN" -C "$repo_root" run ./scripts/release/stable verify --trust "$trust" --trust-only --state "$state" >/dev/null ||
	refuse 'stable trust does not authenticate'
baseline=$(jq -er --arg version "$version" '
	def triple: split(".") | map(tonumber);
	[.releases[].version | select(test("^[0-9]+\\.[0-9]+\\.[0-9]+$")) | select(triple < ($version | triple))] |
	sort_by(triple) | last | select(. != null)
' "$trust/metadata.json") || refuse "authenticated trust lists no stable release below $version to compare with"
highest=$(jq -r '.highest_release // empty' "$trust/metadata.json")
latest_flag=
[ "$baseline" != "$highest" ] || latest_flag=--latest

download="$work/baseline"
mkdir "$download"
if ! "$GH_BIN" release download "v$baseline" --repo "$repository" --dir "$download" >/dev/null 2>"$work/err"; then
	grep -Eq 'release not found|HTTP 404' "$work/err" || { cat "$work/err" >&2; lookup_failed "cannot download v$baseline"; }
	refuse "v$baseline has no downloadable release to compare with"
fi
commit=$(jq -er '.source_commit' "$download/release-manifest.json") || refuse "v$baseline has no readable manifest"
# shellcheck disable=SC2086
"$GO_BIN" -C "$repo_root" run ./scripts/release/stable verify --trust "$trust" --directory "$download" \
	--version "$baseline" --commit "$commit" --state "$state" --published $latest_flag >/dev/null ||
	refuse "v$baseline does not verify against the signed trust"
verify_release_compatibility_artifact "$download/release-manifest.json" "$download" ||
	refuse "v$baseline carries no declaration bound to its signed manifest"

normalize() {
	jq -S 'del(.version, .sequence, .commit)' "$1" >"$2" || refuse "cannot normalize $1"
	[ -s "$2" ] || refuse "normalized $1 is empty"
}
normalize "$declaration" "$work/candidate.json"
normalize "$download/upgrade-compatibility.json" "$work/baseline.json"
cmp -s "$work/candidate.json" "$work/baseline.json" ||
	refuse "the compatibility declaration differs from the one v$baseline published (migrations, schema or source edges changed)"
printf 'unattended release: compatibility is identical to v%s\n' "$baseline"
