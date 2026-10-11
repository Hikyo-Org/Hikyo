#!/bin/sh
# Execute the trusted callback against GitHub API fixtures. Every refusal must
# avoid the mutation; a qualifying completion may issue exactly one POST.
set -eu
script="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)/reconcile-fork-gate.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-reconcile-fixture.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir "$work/bin"
cat >"$work/bin/gh" <<'STUB'
#!/bin/sh
set -eu
url= method=GET slurp=false
while [ "$#" -gt 0 ]; do
	case "$1" in
		api | --paginate) ;;
		--slurp) slurp=true ;;
		--method) method=$2; shift ;;
		*) url=$1 ;;
	esac
	shift
done
if [ "$method" = POST ]; then
	printf '%s\n' "$url" >>"$FIXTURES/posts"
	[ "$url" = repos/o/r/actions/jobs/900/rerun ] || exit 1
	[ "${POST_ERROR:-false}" = false ] || exit 1
	exit 0
fi
case "$url" in
	repos/o/r) fixture=repo ;;
	*/workflows/ci-fork.yml) fixture=fork-workflow ;;
	*/workflows/ci-control.yml) fixture=trusted-workflow ;;
	*/runs/42) fixture=source ;;
	*/pulls/7)
		count=0
		[ ! -f "$FIXTURES/pr-calls" ] || count=$(cat "$FIXTURES/pr-calls")
		count=$((count + 1)); printf '%s\n' "$count" >"$FIXTURES/pr-calls"
		if [ "$count" -gt 1 ] && [ "${PR_MOVES:-false}" = true ]; then
			jq '.head.sha = "fedcba9876543210fedcba9876543210fedcba98"' "$FIXTURES/pr.json"
			exit 0
		fi
		fixture=pr ;;
	*/workflows/ci-fork.yml/runs*) fixture=fork-runs ;;
	*/workflows/ci-control.yml/runs*)
		count=0
		[ ! -f "$FIXTURES/trusted-calls" ] || count=$(cat "$FIXTURES/trusted-calls")
		count=$((count + 1)); printf '%s\n' "$count" >"$FIXTURES/trusted-calls"
		if [ "$count" -gt 1 ] && [ "${TRUSTED_MOVES:-false}" = true ]; then
			jq '[. | .workflow_runs[0].run_attempt = 2 | .workflow_runs[0].status = "queued"]' "$FIXTURES/trusted-runs.json"
			exit 0
		fi
		if [ "$count" -ge "${TRUSTED_SETTLES_AFTER:-99999}" ]; then
			jq '[. | .workflow_runs[0].status = "completed"]' "$FIXTURES/trusted-runs.json"
			exit 0
		fi
		fixture=trusted-runs ;;
	*/runs/80/attempts/1/jobs*)
		count=0
		[ ! -f "$FIXTURES/job-calls" ] || count=$(cat "$FIXTURES/job-calls")
		count=$((count + 1)); printf '%s\n' "$count" >"$FIXTURES/job-calls"
		if [ "$count" -ge "${JOB_SETTLES_AFTER:-99999}" ]; then
			jq '.[0].jobs[0].status="completed" | .[0].jobs[0].conclusion="failure"' "$FIXTURES/jobs.json"
			exit 0
		fi
		fixture=jobs ;;
	*) printf 'unexpected API URL %s\n' "$url" >&2; exit 1 ;;
esac
if [ "${MALFORMED_ENDPOINT:-}" = "$fixture" ]; then printf '<html>bad gateway</html>\n'; exit 0; fi
if [ "$slurp" = true ] && [ "$fixture" != jobs ]; then
	jq 'if type == "array" then . else [.] end' "$FIXTURES/$fixture.json"
else
	cat "$FIXTURES/$fixture.json"
fi
STUB
chmod +x "$work/bin/gh"
cat >"$work/bin/sleep" <<'SLEEP'
#!/bin/sh
[ "$1" = 15 ] || exit 1
printf 'sleep\n' >>"$FIXTURES/sleeps"
SLEEP
chmod +x "$work/bin/sleep"
head=0123456789abcdef0123456789abcdef01234567
fixture() {
	rm -f "$work/posts" "$work/pr-calls" "$work/trusted-calls" "$work/sleeps" "$work/job-calls"
	printf '{"id":1}\n' >"$work/repo.json"
	printf '{"id":10,"name":"fork-ci","path":".github/workflows/ci-fork.yml"}\n' >"$work/fork-workflow.json"
	printf '{"id":20,"name":"trusted-ci","path":".github/workflows/ci-control.yml"}\n' >"$work/trusted-workflow.json"
	printf '{"number":7,"state":"open","head":{"sha":"%s","repo":{"id":2}},"base":{"repo":{"id":1}}}\n' "$head" >"$work/pr.json"
	jq -n --arg head "$head" '{id:42,workflow_id:10,path:".github/workflows/ci-fork.yml",name:"fork-ci #7",display_title:"fork-ci #7",event:"pull_request",repository:{id:1},head_repository:{id:2},head_sha:$head,run_attempt:1,status:"completed",pull_requests:[{number:7,head:{sha:$head,repo:{id:2}},base:{repo:{id:1}}}]}' >"$work/source.json"
	jq '{workflow_runs:[.]}' "$work/source.json" >"$work/fork-runs.json"
	# Deliberately use a different top-level SHA: trusted PR association is the
	# authority even when pull_request_target describes its base commit here.
	jq '{workflow_runs:[. | .id=80 | .workflow_id=20 | .path=".github/workflows/ci-control.yml" | .name="trusted-ci" | .display_title="arbitrary PR title" | .event="pull_request_target" | .head_sha="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]}' "$work/source.json" >"$work/trusted-runs.json"
	printf '[{"jobs":[{"id":900,"run_id":80,"name":"ci-required","status":"completed","conclusion":"failure","run_attempt":1}]}]\n' >"$work/jobs.json"
}
change() {
	jq "$2" "$work/$1.json" >"$work/new.json"
	mv "$work/new.json" "$work/$1.json"
}
run() {
	PATH="$work/bin:$PATH" FIXTURES="$work" GH_REPO=o/r SOURCE_RUN_ID=42 SOURCE_RUN_ATTEMPT=1 "$script" >"$work/stdout" 2>"$work/stderr"
}
no_post() {
	[ ! -f "$work/posts" ] || { printf 'unexpected mutation: %s\n' "$(cat "$work/posts")" >&2; exit 1; }
}
reject() {
	if run; then printf 'accepted invalid fixture: %s\n' "$1" >&2; exit 1; fi
	no_post
}
noop() {
	run || { cat "$work/stderr" >&2; exit 1; }
	no_post
	grep -F 'No rerun:' "$work/stderr" >/dev/null
}
for source_change in '.workflow_id=11' '.path=".github/workflows/other.yml"' '.event="push"' '.repository.id=3' '.name="other"' '.display_title="fork-ci #8"' '.pull_requests[0].number=8' '.head_repository.id=3' '.pull_requests=[]'; do
	fixture; change source "$source_change"; reject "$source_change"
done
for pr_change in '.head.sha="fedcba9876543210fedcba9876543210fedcba98"' '.state="closed"'; do
	fixture; change pr "$pr_change"; noop
done
fixture; change pr '.number=8'; reject 'different PR'
for source_change in '.run_attempt=2' '.status="in_progress"'; do
	fixture; change source "$source_change"; noop
done
for latest_change in '.workflow_runs[0].id=43' '.workflow_runs[0].run_attempt=2' '.workflow_runs=[]'; do
	fixture; change fork-runs "$latest_change"; noop
done
for trusted_change in '.workflow_runs[0].status="in_progress"' '.workflow_runs=[]' '.workflow_runs[0].pull_requests[0].number=8' '.workflow_runs[0].pull_requests[0].head.sha="fedcba9876543210fedcba9876543210fedcba98"' '.workflow_runs[0].workflow_id=99'; do
	fixture; change trusted-runs "$trusted_change"; noop
done
for conclusion in success skipped cancelled; do
	fixture; change jobs ".[0].jobs[0].conclusion=\"$conclusion\""; noop
done
fixture; change jobs '.[0].jobs[0].status="queued" | .[0].jobs[0].conclusion=null'; noop
fixture; change jobs '.[0].jobs=[]'; noop
fixture; change jobs '.[0].jobs[0].run_id=81'; reject 'job from wrong run'
fixture; change jobs '.[0].jobs[0].id=900.5'; reject 'non-integer job identity'
fixture; change jobs '.[0].jobs[0].run_attempt=2'; reject 'job from wrong attempt'
fixture; change jobs '.[0].jobs += .[0].jobs'; reject 'ambiguous gate jobs'
for endpoint in repo fork-workflow trusted-workflow source pr fork-runs trusted-runs jobs; do
	fixture; ( MALFORMED_ENDPOINT="$endpoint" reject "malformed $endpoint response" )
done
for run_state in queued in_progress; do
fixture
change trusted-runs ".workflow_runs[0].status=\"$run_state\""
( TRUSTED_SETTLES_AFTER=2 run ) || { cat "$work/stderr" >&2; exit 1; }
[ "$(wc -l <"$work/posts" | tr -d ' ')" -eq 1 ]
[ "$(wc -l <"$work/sleeps" | tr -d ' ')" -eq 1 ]
done
fixture
change trusted-runs '.workflow_runs[0].status="in_progress"'
noop
[ "$(wc -l <"$work/sleeps" | tr -d ' ')" -eq 6 ]
fixture
change trusted-runs '.workflow_runs[0].status="queued"'
change jobs '.[0].jobs[0].status="queued" | .[0].jobs[0].conclusion=null'
noop
[ ! -f "$work/sleeps" ]
fixture
change trusted-runs '.workflow_runs[0].status="queued"'
change jobs '.[0].jobs[0].status="in_progress" | .[0].jobs[0].conclusion=null'
noop
[ "$(wc -l <"$work/sleeps" | tr -d ' ')" -eq 6 ]

fixture
# Run and job endpoints can settle in either order.
change jobs '.[0].jobs[0].status="in_progress" | .[0].jobs[0].conclusion=null'
( JOB_SETTLES_AFTER=2 run ) || { cat "$work/stderr" >&2; exit 1; }
[ "$(wc -l <"$work/posts" | tr -d ' ')" -eq 1 ]
[ "$(wc -l <"$work/sleeps" | tr -d ' ')" -eq 1 ]
fixture
change jobs '.[0].jobs[0].status="in_progress" | .[0].jobs[0].conclusion=null'
noop
[ "$(wc -l <"$work/sleeps" | tr -d ' ')" -eq 6 ]
fixture
# A relevant expired gate can appear beyond page one after a busy queue.
change trusted-runs '[{workflow_runs:[.workflow_runs[0] | .id=81 | .pull_requests[0].number=8]}, .]'
run || { cat "$work/stderr" >&2; exit 1; }
[ "$(wc -l <"$work/posts" | tr -d ' ')" -eq 1 ]
fixture; ( PR_MOVES=true noop )
fixture; ( TRUSTED_MOVES=true noop )
for conclusion in failure timed_out; do
	fixture; change jobs ".[0].jobs[0].conclusion=\"$conclusion\""
	run || { cat "$work/stderr" >&2; exit 1; }
	[ "$(wc -l <"$work/posts" | tr -d ' ')" -eq 1 ]
	grep -F 'Rerun requested for PR #7' "$work/stderr" >/dev/null
done
fixture
if ( POST_ERROR=true run ); then printf 'failed mutation was accepted\n' >&2; exit 1; fi
[ "$(wc -l <"$work/posts" | tr -d ' ')" -eq 1 ]
grep -F 'no mutation retry attempted' "$work/stderr" >/dev/null
# The display name can change while existing runs keep their old metadata.
# Keep immutable workflow/path/PR/head checks for both admitted names.
for name in fork-ci pr-validation; do
	fixture
	change fork-workflow ".name=\"$name\""
	change source ".name=\"$name\""
	run || { cat "$work/stderr" >&2; exit 1; }
	[ "$(wc -l <"$work/posts" | tr -d ' ')" -eq 1 ]
done
fixture; change fork-workflow '.name="other"'; reject 'unknown workflow display name'
printf 'fork gate reconciliation fixtures passed: exact PR/head/workflow/attempt binding, no-op states, malformed data, and one no-retry mutation\n'
