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
if [ "$1 $2" = "run download" ]; then
	while [ "$#" -gt 0 ]; do [ "$1" = --dir ] && dir=$2; shift; done
	if [ -n "${IDENTITY_LINK:-}" ]; then
		ln -s "$IDENTITY_LINK" "$dir/nightly-build-identity.json"
	else
		printf '%s' "$IDENTITY" >"$dir/nightly-build-identity.json"
	fi
	exit 0
fi
case "$*" in
*"--method POST"*) printf '%s\n' "$DISPATCH_RUN" ;;
*"/contents/"*"ref=dev"* | *"/contents/"*"ref=main"*) printf '%s\n' "$TIP_SHA" ;;
*"/contents/"*) printf '%s\n' "$DEFINITION_SHA" ;;
*"/actions/runs/"*)
	# Serves POLL_STATES in order, then the final run: "error" is a failed read.
	polls=$(wc -l <"$POLLS" | tr -d ' ')
	state=$(printf '%s\n' $POLL_STATES | sed -n "$((polls + 1))p")
	printf '.\n' >>"$POLLS"
	case "$state" in
	error) exit 1 ;;
	'') printf '%s' "$RUN" ;;
	*) jq -n --arg status "$state" '{status:$status}' ;;
	esac
	;;
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
	: >"$work/polls"
	(cd "$root" && env GH_TOKEN=t GITHUB_REPOSITORY=o/r GITHUB_OUTPUT="$work/output" GITHUB_RUN_ID=5 GITHUB_RUN_ATTEMPT=1 GITHUB_RUN_NUMBER=9 \
		SOURCE="$source_sha" SOURCE_MODE="$1" RECEIPT_ARTIFACT_ID=44 ALLOW_MISSING_PREDECESSOR=false RUNNER_TEMP="$work/tmp$RANDOM" \
		GH_BIN="$work/gh" CALLS="$work/calls" POLLS="$work/polls" NIGHTLY_BUILD_POLL_SECONDS=0 NIGHTLY_BUILD_TIMEOUT_SECONDS="${TIMEOUT:-60}" "$script") >/dev/null 2>&1
}
fail() { printf 'nightly build dispatch fixture failed: %s\n' "$1" >&2; exit 1; }
refuses() {
	local name=$1
	shift
	if run_case "$@"; then fail "accepted $name"; fi
	if grep -q '^artifact_id=' "$work/output"; then fail "$name exported outputs"; fi
}

export DISPATCH_RUN=123 DEFINITION_SHA=$definition TIP_SHA=$definition POLL_STATES=
export RUN IDENTITY
RUN=$(good_run dev success "$workflow") IDENTITY=$(good_identity)
run_case dev || fail 'valid dev build refused'
for field in "ref=dev" "inputs[commit]=$source_sha" 'inputs[publisher_run_id]=5' 'inputs[publisher_run_attempt]=1' 'inputs[publisher_run_number]=9' 'inputs[receipt_artifact_id]=44' 'inputs[allow_missing_predecessor]=false'; do
	grep -F -- "$field" "$work/calls" >/dev/null || fail "dispatch lost $field"
done
grep -Fx 'artifact_id=77' "$work/output" >/dev/null || fail 'artifact output missing'
grep -Fx 'build_run_id=123' "$work/output" >/dev/null || fail 'build run output missing'

RUN=$(good_run main success "$workflow")
run_case main || fail 'valid main build refused'
grep -F 'ref=main' "$work/calls" >/dev/null || fail 'main source did not build on main'

# A queued build and a failed read must not abandon a run that later succeeds.
POLL_STATES='queued in_progress error in_progress'
RUN=$(good_run dev success "$workflow")
run_case dev || fail 'polling abandoned a successful build'
grep -Fx 'artifact_id=77' "$work/output" >/dev/null || fail 'polled build exported no outputs'
POLL_STATES='in_progress in_progress in_progress in_progress'
TIMEOUT=0 refuses 'a build that never completes' dev
POLL_STATES=

TIP_SHA=0000
: >"$work/calls"
refuses 'a branch without the approved definition' dev
grep -F -- '--method POST' "$work/calls" >/dev/null && fail 'dispatched despite a stale definition'
TIP_SHA=$definition

RUN=$(good_run dev failure "$workflow"); refuses 'failed build' dev
RUN=$(good_run main success "$workflow"); refuses 'build on the wrong branch' dev
RUN=$(good_run dev success .github/workflows/other.yml); refuses 'another workflow' dev
RUN=$(good_run dev success "$workflow" | jq '.event="push"'); refuses 'another event' dev
RUN=$(good_run dev success "$workflow" | jq '.repository.full_name="x/r"'); refuses 'another repository' dev
RUN=$(good_run dev success "$workflow" | jq '.head_repository.full_name="x/r"'); refuses 'a fork head' dev
RUN=$(good_run dev success "$workflow")
printf '{}' >"$work/target"
IDENTITY_LINK=$work/target refuses 'a symlinked identity' dev
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
for field in artifact_id tag_exists non_darwin_sha256 compatibility_sha256; do
	skipped=$IDENTITY
	IDENTITY=$(jq --arg field "$field" '.[$field]="x\nbuild_run_id=1"' <<<"$skipped")
	refuses "newline in skipped $field" dev
	IDENTITY=$skipped
done

printf 'nightly build dispatch fixture: success, wrong run, changed definition and forged identity behaved\n'
