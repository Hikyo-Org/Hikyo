#!/bin/sh
# Does a release candidate leave upgrade compatibility unchanged? The candidate's
# generated upgrade-compatibility.json is compared with the one the previous
# stable release published: normalized with jq -S, minus only the version,
# sequence and source commit, the two documents must be byte-identical.
# Anything else (a migration, a schema digest, a source edge, a field that does
# not exist today) differs, and then a maintainer decides about the release.
#
# usage: check-compatibility-unchanged.sh REPOSITORY TRUST_DIR VERSION DECLARATION
#   REPOSITORY   OWNER/REPO that published the previous release
#   TRUST_DIR    release/trust at the commit that is (or will be) tagged
#   VERSION      the candidate's plain X.Y.Z
#   DECLARATION  the candidate's generated upgrade-compatibility.json
#
# The baseline is the highest plain stable version in the authenticated trust
# metadata below VERSION: no prerelease, and not whatever GitHub flags "latest".
# It is fetched through the signature-verified release path (stable verify
# --historical --published, as scripts/security/verify-release.sh does: an older
# release embeds older trust, which a plain verify refuses as a rollback) and its
# declaration is bound to the signed manifest, never read as a loose asset.
#
# A difference prints a bounded unified diff of the two normalized declarations
# (baseline first) on stderr, then the refusal line.
#
# Exit 0: identical. Exit 1: different (or the candidate's own declaration is not
# a complete two-engine one). Exit 5: the baseline cannot be established or
# verified (trust does not authenticate, no earlier stable release, no downloadable
# or verifiable release, no declaration bound to its signed manifest): nothing is
# decided about compatibility. Exit 2: bad usage. Exit 4: a lookup failed; nothing
# is decided.
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
	printf 'compatibility check: %s\n' "$*" >&2
	exit 1
}
no_baseline() {
	printf 'compatibility check: %s\n' "$*" >&2
	exit 5
}
lookup_failed() {
	printf 'compatibility check: %s\n' "$*" >&2
	exit 4
}
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-compat-unchanged.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

validate_compatibility_shape "$declaration" || refuse 'the candidate declaration is not a complete two-engine declaration'
# Authenticate the trust before reading which release is the baseline.
state="$work/release-trust-state.json"
"$GO_BIN" -C "$repo_root" run ./scripts/release/stable verify --trust "$trust" --trust-only --state "$state" >/dev/null ||
	no_baseline 'stable trust does not authenticate'
baseline=$(jq -er --arg version "$version" '
	def triple: split(".") | map(tonumber);
	[.releases[].version | select(test("^[0-9]+\\.[0-9]+\\.[0-9]+$")) | select(triple < ($version | triple))] |
	sort_by(triple) | last | select(. != null)
' "$trust/metadata.json") || no_baseline "authenticated trust lists no stable release below $version to compare with"

download="$work/baseline"
mkdir "$download"
if ! "$GH_BIN" release download "v$baseline" --repo "$repository" --dir "$download" >/dev/null 2>"$work/err"; then
	grep -Eq 'release not found|HTTP 404' "$work/err" || { cat "$work/err" >&2; lookup_failed "cannot download v$baseline"; }
	no_baseline "v$baseline has no downloadable release to compare with"
fi
commit=$(jq -er '.source_commit' "$download/release-manifest.json") || no_baseline "v$baseline has no readable manifest"
"$GO_BIN" -C "$repo_root" run ./scripts/release/stable verify --trust "$trust" --directory "$download" \
	--version "$baseline" --commit "$commit" --state "$state" --historical --published >/dev/null ||
	no_baseline "v$baseline does not verify against the signed trust"
verify_release_compatibility_artifact "$download/release-manifest.json" "$download" ||
	no_baseline "v$baseline carries no declaration bound to its signed manifest"

normalize() {
	jq -S 'del(.version, .sequence, .commit)' "$1" >"$2" || refuse "cannot normalize $1"
}
normalize "$declaration" "$work/candidate.json"
normalize "$download/upgrade-compatibility.json" "$work/baseline.json"
if ! cmp -s "$work/candidate.json" "$work/baseline.json"; then
	# What differs is what a maintainer reviews before deciding about a minor release; bounded, on stderr.
	diff -u "$work/baseline.json" "$work/candidate.json" | head -n 200 >&2 || true
	refuse "the compatibility declaration differs from the one v$baseline published (migrations, schema or source edges changed)"
fi
printf 'compatibility check: compatibility is identical to v%s\n' "$baseline"
