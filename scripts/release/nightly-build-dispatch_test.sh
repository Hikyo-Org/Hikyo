#!/usr/bin/env bash
# Drives nightly-build-dispatch.sh with a stub gh serving each API response.
set -euo pipefail
script="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)/nightly-build-dispatch.sh"
root="$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)"
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-nightly-build-fixture.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
source_sha=$(printf 'a%.0s' $(seq 40))
sha64=$(printf 'd%.0s' $(seq 64))

cat >"$work/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$CALLS"
case "$1 $2" in
"run watch") exit "${WATCH_STATUS:-0}" ;;
"run download")
	while [ "$#" -gt 0 ]; do [ "$1" = --dir ] && dir=$2; shift; done
	printf '%s' "$IDENTITY" >"$dir/nightly-build-identity.json"
	exit 0 ;;
esac
case "$*" in
*"--method POST"*) printf '%s\n' "$DISPATCH_RUN" ;;
*"/contents/"*) printf '%s\n' "$DEFINITION_SHA" ;;
*"/actions/runs/"*) printf '%s' "$RUN" ;;
*) exit 8 ;;
esac
STUB
chmod +x "$work/gh"

good_run() {
	jq -n --arg branch "$1" --arg conclusion "$2" --arg path "$3" \
		'{status:"completed",conclusion:$conclusion,path:$path,event:"workflow_dispatch",head_branch:$branch,head_sha:"f",repository:{full_name:"o/r"},head_repository:{full_name:"o/r"}}'
}
good_identity() {
	jq -n --arg commit "$source_sha" --arg sha "$sha64" \
		'{artifact_id:"77",commit:$commit,compatibility_sha256:$sha,non_darwin_sha256:$sha,skip:"false",tag:"v1.0.0-nightly.1",tag_exists:"false",version:"1.0.0-nightly.1"}'
}
workflow=.github/workflows/nightly-build.yml
definition=$(git -C "$root" hash-object "$workflow")

run_case() {
	: >"$work/calls"
	: >"$work/output"
	(cd "$root" && env GH_TOKEN=t GITHUB_REPOSITORY=o/r GITHUB_OUTPUT="$work/output" GITHUB_RUN_ID=5 GITHUB_RUN_ATTEMPT=1 GITHUB_RUN_NUMBER=9 \
		SOURCE="$source_sha" SOURCE_MODE="$1" RECEIPT_ARTIFACT_ID=44 ALLOW_MISSING_PREDECESSOR=false RUNNER_TEMP="$work/tmp$RANDOM" \
		GH_BIN="$work/gh" CALLS="$work/calls" "$script") >/dev/null 2>&1
}
fail() { printf 'nightly build dispatch fixture failed: %s\n' "$1" >&2; exit 1; }
refuses() {
	local name=$1
	shift
	if run_case "$@"; then fail "accepted $name"; fi
	if grep -q '^artifact_id=' "$work/output"; then fail "$name exported outputs"; fi
}

export DISPATCH_RUN=123 DEFINITION_SHA=$definition WATCH_STATUS=0
export RUN IDENTITY
RUN=$(good_run dev success "$workflow") IDENTITY=$(good_identity)
run_case dev || fail 'valid dev build refused'
grep -F 'ref=dev' "$work/calls" >/dev/null || fail 'dev source did not build on dev'
grep -Fx 'artifact_id=77' "$work/output" >/dev/null || fail 'artifact output missing'
grep -Fx 'build_run_id=123' "$work/output" >/dev/null || fail 'build run output missing'

RUN=$(good_run main success "$workflow")
run_case main || fail 'valid main build refused'
grep -F 'ref=main' "$work/calls" >/dev/null || fail 'main source did not build on main'

# A failed watch alone decides nothing: the authenticated run does.
RUN=$(good_run dev success "$workflow") WATCH_STATUS=1
run_case dev || fail 'transient watch failure refused a successful run'
WATCH_STATUS=0

RUN=$(good_run dev failure "$workflow"); refuses 'failed build' dev
RUN=$(good_run main success "$workflow"); refuses 'build on the wrong branch' dev
RUN=$(good_run dev success .github/workflows/other.yml); refuses 'another workflow' dev
RUN=$(good_run dev success "$workflow")
DEFINITION_SHA=0000; refuses 'changed build definition' dev
DEFINITION_SHA=$definition
DISPATCH_RUN=; refuses 'dispatch without a run' dev
DISPATCH_RUN=123
if run_case feature; then fail 'accepted an unknown source mode'; fi

for mutation in \
	'.commit="b"' \
	'.extra="x"' \
	'.tag="main\nskip=true"' \
	'.tag="v1\nskip=true" | .version="1\nskip=true"' \
	'.artifact_id="1\nskip=true"' \
	'.non_darwin_sha256 += "\nskip=true"' \
	'.non_darwin_sha256="short"' \
	'.skip="maybe"' \
	'.tag="v9"' \
	'.artifact_id=7'; do
	IDENTITY=$(good_identity | jq "$mutation")
	refuses "identity mutation $mutation" dev
done

# An already published run carries no build outputs, and that is the only such state.
IDENTITY=$(jq -n --arg commit "$source_sha" '{artifact_id:"",commit:$commit,compatibility_sha256:"",non_darwin_sha256:"",skip:"true",tag:"v1.0.0-nightly.1",tag_exists:"",version:"1.0.0-nightly.1"}')
run_case dev || fail 'already published run refused'
grep -Fx 'skip=true' "$work/output" >/dev/null || fail 'skip output missing'

printf 'nightly build dispatch fixture: success, wrong run, changed definition and forged identity behaved\n'
