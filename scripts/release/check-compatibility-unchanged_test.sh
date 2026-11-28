#!/bin/sh
# The compatibility check compares the candidate's generated
# declaration with the previous stable release's published one. The declarations
# here have the real shape: the migrations come from the declaration the real
# generator produces for this source tree, and the source edges are copied from
# the genesis edges of the hand-reviewed release/compatibility/sources.json (its release
# edges name the real v1.1.0, which these fixtures replace with their own baseline).
# Only release download and the signature verifier are faked; the comparison
# itself is the real script.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-compat-unchanged-test.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
mkdir "$scratch/bin" "$scratch/trust"
c0=0000000000000000000000000000000000000001
export FIXTURE_SCRATCH="$scratch" FIXTURE_COMMIT="$c0"
real_declaration() { # version sequence commit
	jq -S --slurpfile sources "$repo_root/release/compatibility/sources.json" --arg v "$1" --argjson s "$2" --arg c "$3" '
		.version = $v | .sequence = $s | .commit = $c |
		.engines |= map(.sources = ($sources[0].engines[.migrations.engine] | map(select(.source.genesis != null))))' "$repo_root/internal/buildcompat/development.json"
}
jq -e '.engines.sqlite | length > 1' "$repo_root/release/compatibility/sources.json" >/dev/null ||
	{ printf 'fixture: release/compatibility/sources.json lost its reviewed edges\n' >&2; exit 1; }
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"

trust_metadata() { # highest releases...
	highest=$1
	shift
	jq -nc --arg highest "$highest" --args '{highest_release:$highest, releases:($ARGS.positional | map({version:., sequence:1, manifest_sha256:("a"*64)}))}' "$@"
}
write_baseline() { # directory declaration-file
	mkdir -p "$1"
	cp "$2" "$1/upgrade-compatibility.json"
	jq -nc --arg c "$c0" '{version:"1.1.0",sequence:2,commit:$c,key_id:"k",public_key:"k.pub"}' >"$1/release-candidate.json"
	jq -nc --arg c "$c0" --arg sha "$(sha256_file "$2")" \
		'{version:"1.1.0",release_sequence:2,source_commit:$c,signing_key_id:"github-actions-stable",artifacts:[{name:"upgrade-compatibility.json",kind:"upgrade-compatibility",sha256:$sha}]}' >"$1/release-manifest.json"
}
cat >"$scratch/bin/gh" <<'SH'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$FIXTURE_SCRATCH/calls"
case "$FIXTURE_MODE" in missing-release) case "$*" in 'release download '*) printf 'release not found\n' >&2; exit 1 ;; esac ;; esac
case "$FIXTURE_MODE" in download-error) printf 'gh: server error (HTTP 502)\n' >&2; exit 1 ;; esac
case "$*" in
	'release download v1.1.0 --repo o/r --dir '*)
		for last; do :; done
		cp -R "$FIXTURE_SCRATCH/published/." "$last/" ;;
	'release download '*) printf 'release not found\n' >&2; exit 1 ;;
	*) printf 'unexpected gh invocation: %s\n' "$*" >&2; exit 2 ;;
esac
SH
cat >"$scratch/bin/go" <<'SH'
#!/bin/sh
set -eu
printf 'go %s\n' "$*" >>"$FIXTURE_SCRATCH/calls"
case "$*" in
	*'stable verify '*'--trust-only '*) [ "$FIXTURE_MODE" != untrusted ] || exit 1; exit 0 ;;
	*'stable verify '*)
		[ "$FIXTURE_MODE" != unverifiable ] || exit 1
		case "$*" in *' --historical'*) ;; *) printf 'trust rollback refused\n' >&2; exit 3 ;; esac
		case "$*" in *' --published'*) ;; *) exit 3 ;; esac
		case "$*" in *' --latest'*) exit 3 ;; esac
		case "$*" in *' --version 1.1.0 '*) ;; *) exit 3 ;; esac
		# What the real verifier prints on success (scripts/release/stable/verify.go): the verified release identity.
		case "$FIXTURE_MODE" in
		no-identity) printf '{"published_oci_verified":true}\n'; exit 0 ;;
		empty-identity) printf '{"identity":{},"published_oci_verified":true}\n'; exit 0 ;;
		esac
		dir=
		while [ $# -gt 0 ]; do [ "$1" != --directory ] || dir=$2; shift; done
		# The verifier alone decides the identity: the fixture's own files say sequence 2, the other-identity
		# verifier says 3, and the check must follow the verifier. A wrong version or commit is refused.
		profile=stable/v1 sequence=2 version=1.1.0 commit=$FIXTURE_COMMIT
		case "$FIXTURE_MODE" in other-identity) sequence=3 ;; other-version) version=1.0.0 ;; other-commit) commit=$(printf '9%.0s' $(seq 40)) ;; other-profile) profile=nightly/v1 ;; esac
		identity=$(jq -nc --arg p "$profile" --arg c "$commit" --arg v "$version" --argjson s "$sequence" --arg decl "$(shasum -a 256 "$dir/upgrade-compatibility.json" | cut -d' ' -f1)" --arg man "$(shasum -a 256 "$dir/release-manifest.json" | cut -d' ' -f1)" \
			'{identity: {profile: $p, version: $v, sequence: $s, commit: $c, compatibility_sha256: $decl, manifest_sha256: $man}, published_oci_verified: true}')
		printf '%s\n' "$identity"
		[ "$FIXTURE_MODE" != two-documents ] || printf '%s\n' "$identity" ;;
	*) exit 2 ;;
esac
SH
chmod +x "$scratch/bin/gh" "$scratch/bin/go"
export GH_BIN="$scratch/bin/gh" GO_BIN="$scratch/bin/go"

real_declaration 1.1.0 2 "$c0" >"$scratch/baseline-decl.json"
write_baseline "$scratch/published" "$scratch/baseline-decl.json"
trust_metadata 1.1.0 1.0.0 1.1.0 >"$scratch/trust/metadata.json"
# The identity v1.1.0 is installed under, as the fake verifier prints it (the real one is
# bound to the edge encoding in scripts/ci/release_identity_binding_test.go).
baseline_identity="$(jq -nc --arg c "$c0" --arg decl "$(sha256_file "$scratch/published/upgrade-compatibility.json")" --arg man "$(sha256_file "$scratch/published/release-manifest.json")" \
	'{profile: "stable/v1", version: "1.1.0", sequence: 2, commit: $c, compatibility_sha256: $decl, manifest_sha256: $man}')"
# The reviewed release-source edge from the baseline: per engine, the edge names the baseline's identity
# and carries the migrations and schema digest the baseline published (here the candidate's own, which
# an unchanged-schema patch shares). The optional jq filter bends one edge, to build the refused cases.
real_declaration 1.1.1 3 1111111111111111111111111111111111111111 >"$scratch/candidate-no-edge.json"
with_edge() { # identity-json [jq filter applied to each edge] [engine index, default both]
	jq -S --argjson id "$1" --argjson only "${3:-null}" '
		.engines |= (to_entries | map(.key as $i | .value | . as $e |
			if $only != null and $only != $i then . else
			.sources += [({source: {release: $id}, migrations: $e.migrations, schema_sha256: $e.schema_sha256, mode: "maintenance"} | '"${2:-.}"')] end))' "$scratch/candidate-no-edge.json"
}
with_edge "$baseline_identity" >"$scratch/candidate.json"

check() { # mode candidate-file
	FIXTURE_MODE=$1
	export FIXTURE_MODE
	: >"$scratch/calls"
	"$script_dir/check-compatibility-unchanged.sh" o/r "$scratch/trust" 1.1.1 "$2" >"$scratch/out" 2>&1
}
accepted() { check "$1" "${2:-$scratch/candidate.json}" || { cat "$scratch/out" >&2; printf 'compatibility fixture: refused %s\n' "$1" >&2; exit 1; }; }
refused() { # mode candidate-file want-fragment want-exit
	rc=0
	check "$1" "$2" || rc=$?
	[ "$rc" = "${4:-1}" ] || { cat "$scratch/out" >&2; printf 'compatibility fixture: %s exited %s, want %s\n' "$1" "$rc" "${4:-1}" >&2; exit 1; }
	grep -F "$3" "$scratch/out" >/dev/null || { cat "$scratch/out" >&2; printf 'compatibility fixture: %s was refused for the wrong reason, want: %s\n' "$1" "$3" >&2; exit 1; }
}

# An unchanged-schema patch, with a different version, sequence and commit, passes.
accepted same
grep -q 'release download v1.1.0 --repo o/r --dir ' "$scratch/calls" || { printf 'the baseline was not fetched\n' >&2; exit 1; }
# The baseline is verified as a historical, published release: its embedded trust is older.
grep -q 'stable verify .*--version 1.1.0 .*--historical --published' "$scratch/calls" || { cat "$scratch/calls" >&2; printf 'the baseline was not verified as a historical published release\n' >&2; exit 1; }

# The release-source edge from the baseline is required, and must match what the baseline published:
# an installed v1.1.0 is only routed to the next release by an edge whose source is that exact release.
edge_fault() { # file want-problem-line
	refused same "$1" "compatibility check: $2" 1
}
# A reviewed edge in the other valid mode passes too.
with_edge "$baseline_identity" '.mode = "restart"' >"$scratch/changed.json"
accepted same "$scratch/changed.json"
# No edge: both engines are named, the route needs no other difference, and no diff is printed.
edge_fault "$scratch/candidate-no-edge.json" 'sqlite: no release-source edge from v1.1.0'
grep -F 'compatibility check: postgres: no release-source edge from v1.1.0' "$scratch/out" >/dev/null || { printf 'compatibility fixture: the missing postgres edge was not named\n' >&2; exit 1; }
if grep -q '^@@ ' "$scratch/out"; then printf 'compatibility fixture: a missing edge alone printed a diff\n' >&2; exit 1; fi
[ "$(tail -n 1 "$scratch/out")" = 'compatibility check: the release-source edge from v1.1.0 is missing or does not match what v1.1.0 published; add the reviewed edge per engine' ] || { printf 'compatibility fixture: a missing edge alone did not end on its own refusal\n' >&2; exit 1; }
grep -F 'no command authors it' "$scratch/out" >/dev/null || { printf 'compatibility fixture: the edge guidance is missing\n' >&2; exit 1; }
grep -F "$baseline_identity" "$scratch/out" >/dev/null || { printf 'compatibility fixture: the required source identity was not printed\n' >&2; exit 1; }
# One engine without it is named, and the other is not.
with_edge "$baseline_identity" . 0 >"$scratch/changed.json"
edge_fault "$scratch/changed.json" 'postgres: no release-source edge from v1.1.0'
if grep -F 'sqlite: no release-source edge' "$scratch/out" >/dev/null; then printf 'compatibility fixture: an engine that has its edge was named\n' >&2; exit 1; fi
# A source that names the baseline and also a genesis is not a release-source edge (Source.Validate refuses it).
with_edge "$baseline_identity" '.source.genesis = "fresh/v1"' >"$scratch/changed.json"
edge_fault "$scratch/changed.json" 'sqlite: no release-source edge from v1.1.0 (an edge names v1.1.0 with a different release identity or another source field)'
# An edge with other migrations or another schema digest than the baseline published.
with_edge "$baseline_identity" '.migrations.entries |= .[:-1]' >"$scratch/changed.json"
edge_fault "$scratch/changed.json" 'sqlite: the edge from v1.1.0 lists migrations other than'
with_edge "$baseline_identity" '.migrations.entries[0].sha256 = ("e" * 64)' 1 >"$scratch/changed.json"
edge_fault "$scratch/changed.json" 'postgres: the edge from v1.1.0 lists migrations other than'
with_edge "$baseline_identity" '.schema_sha256 = ("b" * 64)' >"$scratch/changed.json"
edge_fault "$scratch/changed.json" 'sqlite: the edge from v1.1.0 names a schema digest other than'
# An edge from the wrong release: another commit, sequence, manifest digest, compatibility digest or version.
for field in '.commit = ("9" * 40)' '.sequence = 3' '.manifest_sha256 = ("f" * 64)' '.compatibility_sha256 = ("f" * 64)'; do
	with_edge "$(printf '%s' "$baseline_identity" | jq -c "$field")" >"$scratch/changed.json"
	edge_fault "$scratch/changed.json" 'sqlite: no release-source edge from v1.1.0 (an edge names v1.1.0 with a different release identity or another source field)'
done
with_edge "$(printf '%s' "$baseline_identity" | jq -c '.version = "1.0.0"')" >"$scratch/changed.json"
edge_fault "$scratch/changed.json" 'sqlite: no release-source edge from v1.1.0'
# A genesis edge is not a release edge, and a second copy of the edge is refused.
jq -S '.engines[0].sources += [.engines[0].sources[-1]]' "$scratch/candidate.json" >"$scratch/changed.json"
edge_fault "$scratch/changed.json" 'sqlite: more than one release-source edge from v1.1.0'
with_edge "$baseline_identity" '.mode = "bogus"' >"$scratch/changed.json"
edge_fault "$scratch/changed.json" 'sqlite: the edge from v1.1.0 has no valid mode'
# A missing edge and another difference are both reported, the difference with its diff.
jq -S '.new_top_level_field = true' "$scratch/candidate-no-edge.json" >"$scratch/changed.json"
edge_fault "$scratch/changed.json" 'sqlite: no release-source edge from v1.1.0'
grep -q '^@@ ' "$scratch/out" || { printf 'compatibility fixture: a difference beside a missing edge printed no diff\n' >&2; exit 1; }
# The missing edge is the last word even then, so a summary that quotes the last line never hides it.
[ "$(tail -n 1 "$scratch/out")" = 'compatibility check: the release-source edge from v1.1.0 is missing or does not match what v1.1.0 published, and the declaration also differs (see the diff above); add the reviewed edge per engine' ] || { cat "$scratch/out" >&2; printf 'compatibility fixture: a missing edge beside another difference did not end on the edge refusal\n' >&2; exit 1; }
grep -F 'the compatibility declaration differs from the one v1.1.0 published' "$scratch/out" >/dev/null || { printf 'compatibility fixture: the difference was not reported beside the missing edge\n' >&2; exit 1; }

# --edges-only judges release/compatibility/sources.json alone, before anything is generated.
sources_with() { # identity-json [jq filter applied to each edge]
	jq -S --argjson id "$1" --slurpfile base "$scratch/baseline-decl.json" '.engines |= with_entries(.key as $k | .value |= map(select(.source.genesis != null)) | .value += [($base[0].engines[] | select(.migrations.engine == $k) |
		({source: {release: $id}, migrations: .migrations, schema_sha256: .schema_sha256, mode: "maintenance"} | '"${2:-.}"'))])' "$repo_root/release/compatibility/sources.json"
}
edges_check() { # mode sources-file want-exit want-fragment
	rc=0
	FIXTURE_MODE=$1
	export FIXTURE_MODE
	: >"$scratch/calls"
	"$script_dir/check-compatibility-unchanged.sh" --edges-only o/r "$scratch/trust" 1.1.1 "$2" >"$scratch/out" 2>&1 || rc=$?
	[ "$rc" = "$3" ] || { cat "$scratch/out" >&2; printf 'compatibility fixture: --edges-only %s exited %s, want %s\n' "$1" "$rc" "$3" >&2; exit 1; }
	grep -F "$4" "$scratch/out" >/dev/null || { cat "$scratch/out" >&2; printf 'compatibility fixture: --edges-only: want: %s\n' "$4" >&2; exit 1; }
}
sources_with "$baseline_identity" >"$scratch/sources-ok.json"
edges_check same "$scratch/sources-ok.json" 0 'every engine carries its sound release-source edge from v1.1.0'
if grep -q 'run ./scripts/ci\|compatibility --candidate' "$scratch/calls"; then printf 'compatibility fixture: --edges-only generated a declaration\n' >&2; exit 1; fi
jq -S '.engines |= map_values(map(select(.source.genesis != null)))' "$repo_root/release/compatibility/sources.json" >"$scratch/sources-genesis.json"
edges_check same "$scratch/sources-genesis.json" 1 'sqlite: no release-source edge from v1.1.0'
grep -F 'postgres: no release-source edge from v1.1.0' "$scratch/out" >/dev/null || { printf 'compatibility fixture: --edges-only did not name postgres\n' >&2; exit 1; }
[ "$(tail -n 1 "$scratch/out")" = 'compatibility check: the release-source edge from v1.1.0 is missing or does not match what v1.1.0 published; add the reviewed edge per engine' ] || { printf 'compatibility fixture: --edges-only did not end on the edge refusal\n' >&2; exit 1; }
sources_with "$baseline_identity" '.migrations.entries |= .[:-1]' >"$scratch/changed.json"
edges_check same "$scratch/changed.json" 1 'lists migrations other than'
sources_with "$baseline_identity" '.schema_sha256 = ("b" * 64)' >"$scratch/changed.json"
edges_check same "$scratch/changed.json" 1 'names a schema digest other than'
sources_with "$(printf '%s' "$baseline_identity" | jq -c '.commit = ("9" * 40)')" >"$scratch/changed.json"
edges_check same "$scratch/changed.json" 1 'with a different release identity'
printf '{}\n' >"$scratch/changed.json"
edges_check same "$scratch/changed.json" 1 'not a hikyo.dev/upgrade-sources/v1 document'
edges_check unverifiable "$scratch/sources-ok.json" 5 'does not verify against the signed trust'

# Every difference refuses, including any field that does not exist today.
mutate() { jq -S "$1" "$scratch/candidate.json" >"$scratch/changed.json"; }
for expr in \
	'.engines[0].schema_sha256 = ("b" * 64)' \
	'.engines[1].migrations.entries += [{version: 9999, sha256: ("c" * 64)}]' \
	'.engines[0].migrations.entries |= .[:-1]' \
	'.engines[0].sources[0].mode = "restart"' \
	'.engines[1].sources += [{source: {release: {version: "1.1.0", sequence: 2, commit: ("0" * 40)}}, migrations: {engine: "postgres", entries: []}, schema_sha256: ("d" * 64), mode: "restart"}]' \
	'.engines[0].sources = []' \
	'.engines[0].new_field = 1' \
	'.new_top_level_field = true' \
	'.engines |= reverse'; do
	mutate "$expr"
	refused same "$scratch/changed.json" 'differs from the one v1.1.0 published' 1
	# The reviewer sees what changed: a unified diff precedes the refusal line, which stays last.
	grep -q '^@@ ' "$scratch/out" || { cat "$scratch/out" >&2; printf 'compatibility fixture: a difference prints no diff: %s\n' "$expr" >&2; exit 1; }
	[ "$(tail -n 1 "$scratch/out" | cut -c1-20)" = 'compatibility check:' ] || { printf 'compatibility fixture: the refusal line is not last\n' >&2; exit 1; }
done
# A short difference is shown whole and not reported as cut.
mutate '.new_top_level_field = true'
refused same "$scratch/changed.json" 'differs from the one v1.1.0 published' 1
if grep -F 'cut at 200 lines' "$scratch/out" >/dev/null; then printf 'compatibility fixture: a short diff was reported as cut\n' >&2; exit 1; fi
# A long difference says it was cut, so nobody mistakes the first 200 lines for all of it.
mutate '.engines[0].migrations.entries += [range(9001; 9101) | {version: ., sha256: ("c" * 64)}]'
refused same "$scratch/changed.json" 'cut at 200 lines' 1
[ "$(grep -c '^[-+ @]' "$scratch/out")" -le 205 ] || { printf 'compatibility fixture: the diff was not bounded\n' >&2; exit 1; }
# A shape that is not a two-engine declaration never compares equal to anything.
jq -S '.engines |= .[:1]' "$scratch/candidate.json" >"$scratch/changed.json"
refused same "$scratch/changed.json" 'not a complete two-engine declaration'
jq -S '.engines |= (map({key: .migrations.engine, value: .}) | from_entries)' "$scratch/candidate.json" >"$scratch/changed.json"
refused same "$scratch/changed.json" 'not a complete two-engine declaration'
jq -S '.profile = "stable/v2"' "$scratch/candidate.json" >"$scratch/changed.json"
refused same "$scratch/changed.json" 'not a complete two-engine declaration'
printf '{}\n' >"$scratch/changed.json"
refused same "$scratch/changed.json" 'not a complete two-engine declaration'
: >"$scratch/changed.json"
refused same "$scratch/changed.json" 'not a complete two-engine declaration'

jq -S '.engines[0].schema_sha256 = "not-a-digest"' "$scratch/candidate.json" >"$scratch/changed.json"
refused same "$scratch/changed.json" 'not a complete two-engine declaration'

# Two engines of the same kind are not the postgres and sqlite pair, whatever else is right.
jq -S '.engines[1].migrations.engine = "sqlite"' "$scratch/candidate.json" >"$scratch/changed.json"
refused same "$scratch/changed.json" 'not a complete two-engine declaration'
jq -S '.engines[0].migrations.engine = "postgres"' "$scratch/candidate.json" >"$scratch/changed.json"
refused same "$scratch/changed.json" 'not a complete two-engine declaration'

# A baseline manifest that does not name its source commit cannot be verified: exit 5, not a verdict.
cp "$scratch/published/release-manifest.json" "$scratch/manifest.bak"
jq 'del(.source_commit)' "$scratch/manifest.bak" >"$scratch/published/release-manifest.json"
refused same "$scratch/candidate.json" 'v1.1.0 has no readable manifest' 5
cp "$scratch/manifest.bak" "$scratch/published/release-manifest.json"

# A verifier that prints no release identity decides nothing: exit 5, and no identity is rebuilt from the files.
refused no-identity "$scratch/candidate.json" 'printed no release identity' 5
refused empty-identity "$scratch/candidate.json" 'printed no release identity' 5
refused two-documents "$scratch/candidate.json" 'printed no release identity' 5
refused other-version "$scratch/candidate.json" 'printed no release identity' 5
refused other-commit "$scratch/candidate.json" 'printed no release identity' 5
refused other-profile "$scratch/candidate.json" 'printed no release identity' 5
# The identity is the verifier's, not the files': when it says sequence 3, the edge that names sequence 2 (what the
# files say) is no longer the edge from the baseline, and the required identity printed is the verifier's.
refused other-identity "$scratch/candidate.json" 'compatibility check: sqlite: no release-source edge from v1.1.0 (an edge names v1.1.0 with a different release identity or another source field)' 1
grep -F '"sequence":3' "$scratch/out" >/dev/null || { cat "$scratch/out" >&2; printf 'compatibility fixture: the required identity is not the verifier'"'"'s\n' >&2; exit 1; }
# A baseline signed under another trust policy cannot be routed by any edge (plan.go skips edges across a policy change).
jq '.signing_key_id = "recovery-bridge"' "$scratch/manifest.bak" >"$scratch/published/release-manifest.json"
refused same "$scratch/candidate.json" 'needs a recovery bridge' 1
jq 'del(.signing_key_id)' "$scratch/manifest.bak" >"$scratch/published/release-manifest.json"
refused same "$scratch/candidate.json" 'needs a recovery bridge' 1
cp "$scratch/manifest.bak" "$scratch/published/release-manifest.json"

# Unauthenticated trust decides nothing, and a baseline release that is gone is a refusal, not a lookup failure.
refused untrusted "$scratch/candidate.json" 'stable trust does not authenticate' 5
refused missing-release "$scratch/candidate.json" 'v1.1.0 has no downloadable release' 5

# The shared binding check: shape first, then the candidate's version, sequence and commit.
jq -n --arg c "$c0" '{version:"1.1.0",sequence:2,commit:$c}' >"$scratch/bind.json"
validate_release_compatibility "$scratch/baseline-decl.json" "$scratch/bind.json" || { printf 'binding: a matching candidate was refused\n' >&2; exit 1; }
for field in '.version = "1.1.9"' '.sequence = 9' '.commit = ("9" * 40)'; do
	jq "$field" "$scratch/bind.json" >"$scratch/bind-other.json"
	if validate_release_compatibility "$scratch/baseline-decl.json" "$scratch/bind-other.json"; then printf 'binding: a declaration bound to another candidate was accepted (%s)\n' "$field" >&2; exit 1; fi
done
jq -S '.profile = "stable/v2"' "$scratch/baseline-decl.json" >"$scratch/changed.json"
if validate_release_compatibility "$scratch/changed.json" "$scratch/bind.json"; then printf 'binding: a wrong-shape declaration was accepted\n' >&2; exit 1; fi

# A baseline whose declaration is not the file the signed manifest names is refused.
cp "$scratch/baseline-decl.json" "$scratch/baseline-decl.bak"
jq -S '.engines[0].schema_sha256 = ("b" * 64)' "$scratch/baseline-decl.json" >"$scratch/published/upgrade-compatibility.json"
refused same "$scratch/candidate.json" 'carries no declaration bound to its signed manifest' 5
cp "$scratch/baseline-decl.bak" "$scratch/published/upgrade-compatibility.json"
# A missing or empty baseline declaration never compares equal to a missing or empty candidate.
rm "$scratch/published/upgrade-compatibility.json"
refused same "$scratch/candidate.json" 'carries no declaration bound to its signed manifest' 5
: >"$scratch/published/upgrade-compatibility.json"
refused same "$scratch/candidate.json" 'carries no declaration bound to its signed manifest' 5
cp "$scratch/baseline-decl.bak" "$scratch/published/upgrade-compatibility.json"

# The signature verifier is the authority: an unverifiable baseline refuses.
refused unverifiable "$scratch/candidate.json" 'does not verify against the signed trust' 5
# A read failure is not a verdict; it exits 4 so the caller fails instead of deciding.
refused download-error "$scratch/candidate.json" 'cannot download v1.1.0' 4
# A trust with no earlier stable release has nothing to compare with.
trust_metadata 1.2.0 1.2.0 >"$scratch/trust/metadata.json"
refused same "$scratch/candidate.json" 'lists no stable release below 1.1.1' 5
# Prereleases are never the baseline, and the highest stable below the candidate wins.
trust_metadata 1.2.0-rc.1 1.0.0 1.1.0 1.1.1-rc.1 1.2.0-rc.1 >"$scratch/trust/metadata.json"
accepted same
if grep -q 'stable verify .*--latest' "$scratch/calls"; then printf 'a baseline was verified as the latest release\n' >&2; exit 1; fi
grep -q 'release download v1.1.0 ' "$scratch/calls" || { printf 'a prerelease became the baseline\n' >&2; exit 1; }
# Versions at or above the candidate are never the baseline.
trust_metadata 1.2.0 1.0.0 1.1.0 1.1.1 1.2.0 >"$scratch/trust/metadata.json"
accepted same
grep -q 'release download v1.1.0 ' "$scratch/calls" || { printf 'a version at or above the candidate became the baseline\n' >&2; exit 1; }
# The baseline never comes from the releases list or its latest flag.
if grep -q 'releases/latest\|api ' "$scratch/calls"; then printf 'the baseline was resolved through the API\n' >&2; exit 1; fi

printf 'compatibility check fixture: an unchanged declaration passes; every difference, new field, empty or unbound baseline refuses\n'
