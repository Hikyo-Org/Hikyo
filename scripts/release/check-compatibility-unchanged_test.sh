#!/bin/sh
# The compatibility check compares the candidate's generated
# declaration with the previous stable release's published one. The declarations
# here have the real shape: the migrations come from the declaration the real
# generator produces for this source tree, and the source edges are copied from
# the hand-reviewed release/compatibility/sources.json (genesis edges only).
# Only release download and the signature verifier are faked; the comparison
# itself is the real script.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-compat-unchanged-test.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
mkdir "$scratch/bin" "$scratch/trust"
export FIXTURE_SCRATCH="$scratch"
c0=0000000000000000000000000000000000000001
real_declaration() { # version sequence commit
	jq -S --slurpfile sources "$repo_root/release/compatibility/sources.json" --arg v "$1" --argjson s "$2" --arg c "$3" '
		.version = $v | .sequence = $s | .commit = $c |
		.engines |= map(.sources = $sources[0].engines[.migrations.engine])' "$repo_root/internal/buildcompat/development.json"
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
		'{source_commit:$c,artifacts:[{name:"upgrade-compatibility.json",kind:"upgrade-compatibility",sha256:$sha}]}' >"$1/release-manifest.json"
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
		case "$*" in *' --published'*) ;; *) exit 3 ;; esac
		case "$*" in *' --version 1.1.0 '*) ;; *) exit 3 ;; esac ;;
	*) exit 2 ;;
esac
SH
chmod +x "$scratch/bin/gh" "$scratch/bin/go"
export GH_BIN="$scratch/bin/gh" GO_BIN="$scratch/bin/go"

real_declaration 1.1.0 2 "$c0" >"$scratch/baseline-decl.json"
write_baseline "$scratch/published" "$scratch/baseline-decl.json"
trust_metadata 1.1.0 1.0.0 1.1.0 >"$scratch/trust/metadata.json"
real_declaration 1.1.1 3 1111111111111111111111111111111111111111 >"$scratch/candidate.json"

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
# The baseline is verified as a published release and is the latest, so --latest is asked for.
grep -q 'stable verify .*--version 1.1.0 .*--published --latest' "$scratch/calls" || { cat "$scratch/calls" >&2; printf 'the baseline was not verified as the published latest\n' >&2; exit 1; }

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
done
# A shape that is not a two-engine declaration never compares equal to anything.
jq -S '.engines |= .[:1]' "$scratch/candidate.json" >"$scratch/changed.json"
refused same "$scratch/changed.json" 'not a complete two-engine declaration'
jq -S '.profile = "stable/v2"' "$scratch/candidate.json" >"$scratch/changed.json"
refused same "$scratch/changed.json" 'not a complete two-engine declaration'
printf '{}\n' >"$scratch/changed.json"
refused same "$scratch/changed.json" 'not a complete two-engine declaration'
: >"$scratch/changed.json"
refused same "$scratch/changed.json" 'not a complete two-engine declaration'

jq -S '.engines[0].schema_sha256 = "not-a-digest"' "$scratch/candidate.json" >"$scratch/changed.json"
refused same "$scratch/changed.json" 'not a complete two-engine declaration'

# Unauthenticated trust decides nothing, and a baseline release that is gone is a refusal, not a lookup failure.
refused untrusted "$scratch/candidate.json" 'stable trust does not authenticate'
refused missing-release "$scratch/candidate.json" 'v1.1.0 has no downloadable release'

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
refused same "$scratch/candidate.json" 'carries no declaration bound to its signed manifest'
cp "$scratch/baseline-decl.bak" "$scratch/published/upgrade-compatibility.json"
# A missing or empty baseline declaration never compares equal to a missing or empty candidate.
rm "$scratch/published/upgrade-compatibility.json"
refused same "$scratch/candidate.json" 'carries no declaration bound to its signed manifest'
: >"$scratch/published/upgrade-compatibility.json"
refused same "$scratch/candidate.json" 'carries no declaration bound to its signed manifest'
cp "$scratch/baseline-decl.bak" "$scratch/published/upgrade-compatibility.json"

# The signature verifier is the authority: an unverifiable baseline refuses.
refused unverifiable "$scratch/candidate.json" 'does not verify against the signed trust'
# A read failure is not a verdict; it exits 4 so the caller fails instead of deciding.
refused download-error "$scratch/candidate.json" 'cannot download v1.1.0' 4
# A trust with no earlier stable release has nothing to compare with.
trust_metadata 1.2.0 1.2.0 >"$scratch/trust/metadata.json"
refused same "$scratch/candidate.json" 'lists no stable release below 1.1.1'
# Prereleases are never the baseline, and the highest stable below the candidate wins.
trust_metadata 1.2.0-rc.1 1.0.0 1.1.0 1.1.1-rc.1 1.2.0-rc.1 >"$scratch/trust/metadata.json"
accepted same
if grep -q 'stable verify .*--latest' "$scratch/calls"; then printf 'a baseline below the trust highest was verified as latest\n' >&2; exit 1; fi
grep -q 'release download v1.1.0 ' "$scratch/calls" || { printf 'a prerelease became the baseline\n' >&2; exit 1; }
# Versions at or above the candidate are never the baseline.
trust_metadata 1.2.0 1.0.0 1.1.0 1.1.1 1.2.0 >"$scratch/trust/metadata.json"
accepted same
grep -q 'release download v1.1.0 ' "$scratch/calls" || { printf 'a version at or above the candidate became the baseline\n' >&2; exit 1; }
# The baseline never comes from the releases list or its latest flag.
if grep -q 'releases/latest\|api ' "$scratch/calls"; then printf 'the baseline was resolved through the API\n' >&2; exit 1; fi

printf 'compatibility check fixture: an unchanged declaration passes; every difference, new field, empty or unbound baseline refuses\n'
