#!/bin/sh
# Does a release candidate leave upgrade compatibility unchanged? The candidate's
# generated upgrade-compatibility.json is compared with the one the previous
# stable release published: normalized with jq -S, minus only the version,
# sequence and source commit, and minus exactly one release-source edge per
# engine (the reviewed edge from that previous release), the two documents must
# be identical. Anything else (a migration, a schema digest, another source
# edge, a field that does not exist today) differs, and then a maintainer
# decides about the release.
#
# The edge is required, not tolerated: an installed release is recorded under its
# release identity and is only routed to the next release by an edge whose source
# is that exact release, while genesis edges never match it. Each edge must carry
# the migrations and schema digest the previous release published, and name its
# identity, exactly as `stable verify` prints it for that release. A baseline not
# signed under the github-actions-stable policy is refused (exit 1): the planner
# skips an edge across a trust-policy change, which needs a recovery bridge.
#
# usage: check-compatibility-unchanged.sh [--edges-only] REPOSITORY TRUST_DIR VERSION DECLARATION
#   REPOSITORY   OWNER/REPO that published the previous release
#   TRUST_DIR    release/trust at the commit that is (or will be) tagged
#   VERSION      the candidate's plain X.Y.Z
#   DECLARATION  the candidate's generated upgrade-compatibility.json
#   --edges-only judge only the release-source edge from the baseline, and read
#                DECLARATION as release/compatibility/sources.json (the reviewed
#                edges the generator copies into the declaration): exit 0 when each
#                engine has its sound edge, 1 when one is missing or wrong or when the
#                baseline was not signed under github-actions-stable (it needs a
#                recovery bridge, not an edge), with the same exit codes 2, 4 and 5
#                for the baseline as below. The release
#                planner uses it to say what is missing before anything is generated.
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
# Exit 0: identical apart from the required edge. Exit 1: different, the edge is
# missing or does not match what the previous release published, the baseline was
# not signed under github-actions-stable (a recovery bridge is needed), or the
# candidate's own declaration is not a complete two-engine one. Exit 5: the baseline cannot be established or
# verified (trust does not authenticate, no earlier stable release, no downloadable
# or verifiable release, no declaration bound to its signed manifest): nothing is
# decided about compatibility. Exit 2: bad usage. Exit 4: a lookup failed; nothing
# is decided.
set -eu
: "${GH_BIN:=gh}" "${GO_BIN:=go}"
edges_only=
if [ "${1:-}" = --edges-only ]; then
	edges_only=1
	shift
fi
[ "$#" -eq 4 ] || { printf 'usage: %s [--edges-only] REPOSITORY TRUST_DIR VERSION DECLARATION\n' "$0" >&2; exit 2; }
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

if [ -n "$edges_only" ]; then
	jq -e '.schema == "hikyo.dev/upgrade-sources/v1" and (.engines | type == "object")' "$declaration" >/dev/null ||
		refuse 'the source file is not a hikyo.dev/upgrade-sources/v1 document'
else
	validate_compatibility_shape "$declaration" || refuse 'the candidate declaration is not a complete two-engine declaration'
fi
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
# What the verifier prints is the verified release identity: take it from there, never rebuild it.
"$GO_BIN" -C "$repo_root" run ./scripts/release/stable verify --trust "$trust" --directory "$download" \
	--version "$baseline" --commit "$commit" --state "$state" --historical --published >"$work/verified.json" ||
	no_baseline "v$baseline does not verify against the signed trust"
# Exactly one document, naming the stable profile, the baseline's version and its source commit.
identity=$(jq -sce --arg v "$baseline" --arg c "$commit" 'select(length == 1) | .[0].identity |
	select(type == "object" and .profile == "stable/v1" and .version == $v and .commit == $c)' "$work/verified.json") ||
	no_baseline "the verification of v$baseline printed no release identity"
verify_release_compatibility_artifact "$download/release-manifest.json" "$download" ||
	no_baseline "v$baseline carries no declaration bound to its signed manifest"
# The planner skips an edge across a trust-policy change (internal/upgradecompat/plan.go), so
# no edge can route an installed release signed under another policy: that needs a recovery bridge.
[ "$(jq -r '.signing_key_id // empty' "$download/release-manifest.json")" = github-actions-stable ] ||
	refuse "v$baseline was not signed under the github-actions-stable policy, and the planner does not follow an edge across a trust-policy change: an upgrade from it needs a recovery bridge, not a release-source edge"

# The candidate is compared without the edge from the baseline release itself.
normalize() { # file out [release-identity-to-drop]
	jq -S --argjson id "${3:-null}" 'del(.version, .sequence, .commit) |
		if $id == null then . else .engines |= map(.sources |= map(select(.source != {release: $id}))) end' "$1" >"$2" || refuse "cannot normalize $1"
}
if [ -n "$edges_only" ]; then
	jq '.engines' "$declaration" >"$work/edges.json"
else
	jq '[.engines[] | {key: .migrations.engine, value: .sources}] | from_entries' "$declaration" >"$work/edges.json"
fi || refuse "cannot read the candidate's source edges"
problems=$(release_source_edge_problems "$identity" "$download/upgrade-compatibility.json" "$work/edges.json") || refuse "cannot check the candidate's source edges"
[ -z "$edges_only" ] || [ -n "$problems" ] || { printf 'compatibility check: every engine carries its sound release-source edge from v%s\n' "$baseline"; exit 0; }
if [ -n "$problems" ]; then
	printf '%s\n' "$problems" | sed 's/^/compatibility check: /' >&2
	printf 'compatibility check: an installed v%s has no upgrade route to this release without the reviewed release-source edge from v%s, per engine, in release/compatibility/sources.json.\n' "$baseline" "$baseline" >&2
	printf 'compatibility check: it names source.release %s and copies migrations and schema_sha256 from the upgrade-compatibility.json that v%s published; no command authors it.\n' "$identity" "$baseline" >&2
fi
[ -z "$edges_only" ] || refuse "the release-source edge from v$baseline is missing or does not match what v$baseline published; add the reviewed edge per engine"
normalize "$declaration" "$work/candidate.json" "$identity"
normalize "$download/upgrade-compatibility.json" "$work/baseline.json"
if ! cmp -s "$work/candidate.json" "$work/baseline.json"; then
	# What differs is what a maintainer reviews before deciding about a minor release; bounded, on stderr.
	diff -u "$work/baseline.json" "$work/candidate.json" >"$work/difference" || true
	head -n 200 "$work/difference" >&2
	[ "$(wc -l <"$work/difference" | tr -d ' ')" -le 200 ] || printf 'compatibility check: the diff above is cut at 200 lines; compare the two declarations in full before deciding\n' >&2
	printf 'compatibility check: the compatibility declaration differs from the one v%s published (migrations, schema or source edges changed)\n' "$baseline" >&2
	# A missing or wrong edge is the last word, so a summary that quotes the last line never hides it.
	[ -z "$problems" ] || refuse "the release-source edge from v$baseline is missing or does not match what v$baseline published, and the declaration also differs (see the diff above); add the reviewed edge per engine"
	exit 1
fi
[ -z "$problems" ] || refuse "the release-source edge from v$baseline is missing or does not match what v$baseline published; add the reviewed edge per engine"
printf 'compatibility check: compatibility is identical to v%s apart from the release-source edge from it\n' "$baseline"
