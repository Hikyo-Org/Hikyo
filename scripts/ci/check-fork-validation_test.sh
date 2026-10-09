#!/bin/sh
# Drives check-fork-validation.sh against a stub gh that serves fixture JSON per
# endpoint and applies the --jq filter with jq, as gh does.
set -eu

script="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)/check-fork-validation.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-fork-gate-fixture.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

mkdir "$work/bin"
cat >"$work/bin/gh" <<'EOF'
#!/bin/sh
set -eu
url= filter=. slurp=false jq_filter=false
while [ $# -gt 0 ]; do
	case $1 in
	api | --paginate) ;;
	--slurp) slurp=true ;;
	--jq) filter=$2; jq_filter=true; shift ;;
	*) url=$1 ;;
	esac
	shift
done
if [ "$slurp" = true ] && [ "$jq_filter" = true ]; then
	printf 'the `--slurp` option is not supported with `--jq`\n' >&2
	exit 1
fi
bump() {
	count=0
	if [ -f "$FIXTURES/$1-calls" ]; then count=$(cat "$FIXTURES/$1-calls"); fi
	count=$((count + 1))
	printf '%s\n' "$count" >"$FIXTURES/$1-calls"
}
# Simulate transport and malformed-body failures before any jq processing.
if [ "$url" = repos/o/r/pulls/7 ] && [ -n "${API_ERROR:-}" ]; then
	bump api
	if [ "$count" -le "${API_ERRORS_BEFORE_SUCCESS:-99}" ]; then
		printf '%s\n' "$API_ERROR" >&2
		exit 1
	fi
fi
if [ "$url" = repos/o/r/pulls/7 ] && [ "${MALFORMED_API:-false}" = true ]; then
	bump api
	printf '<html>upstream unavailable</html>\n'
	exit 0
fi
case $url in
*/pulls/7/files*) fixture=files ;;
*/pulls/7/reviews*)
	[ "${REVIEWS_ERROR:-false}" = false ] || exit 1
	bump reviews
	fixture=reviews
	if [ "$count" -ge "${REVIEWS_CHANGE_AT:-99999}" ]; then fixture=reviews-later; fi
	;;
*/collaborators/*/permission)
	bump permission
	[ "${PERMISSION_ERROR:-false}" = false ] || exit 1
	fixture=permission
	if [ "$count" -ge "${PERMISSION_REMOVED_AT:-99999}" ]; then fixture=permission-later; fi
	;;
*/pulls/7)
	bump pr
	if [ "$count" -ge "${HEAD_MOVES_AT:-99999}" ]; then
		jq -r --arg sha fedcba9876543210fedcba9876543210fedcba98 ".head.sha = \$sha | $filter" "$FIXTURES/pr.json"
		exit 0
	fi
	if [ "$count" -ge "${MERGEABLE_RESOLVES_AT:-99999}" ]; then
		jq -r ".mergeable = true | $filter" "$FIXTURES/pr.json"
		exit 0
	fi
	if [ "$count" -ge "${MERGEABLE_PENDING_AT:-99999}" ]; then
		jq -r ".mergeable = null | $filter" "$FIXTURES/pr.json"
		exit 0
	fi
	fixture=pr
	;;
*/workflows/ci-fork.yml/runs*)
	# The API filters by commit server-side; the gate must ask for exactly
	# this head so an older head's passing run can never be selected.
	case $url in
	*"head_sha=$HEAD_SHA&"* | *"head_sha=$HEAD_SHA") ;;
	*) printf 'stub gh: runs query lacks head_sha=%s: %s\n' "$HEAD_SHA" "$url" >&2; exit 1 ;;
	esac
	fixture=runs
	;;
*/runs/42/jobs*) fixture=jobs ;;
*) printf 'stub gh: unexpected %s\n' "$url" >&2; exit 1 ;;
esac
jq -r "$filter" "$FIXTURES/$fixture.json"
EOF
chmod +x "$work/bin/gh"

head=0123456789abcdef0123456789abcdef01234567

# fixture <changed_files> <files-json> <runs-json> <gate-conclusion>
fixture() {
	printf '{"head":{"sha":"%s","repo":{"id":2,"full_name":"fork/r"}},"base":{"repo":{"id":1,"full_name":"o/r"}},"user":{"id":10,"login":"author"},"changed_files":%s,"mergeable":true}\n' "$head" "$1" >"$work/pr.json"
	printf '%s\n' "$2" >"$work/files.json"
	printf '%s\n' "$3" >"$work/runs.json"
	printf '{"jobs":[{"name":"validation / changes","conclusion":"success"},{"name":"validation / ci-required","conclusion":"%s"}]}\n' "$4" >"$work/jobs.json"
	printf '[[]]\n' >"$work/reviews.json"
	printf '[[]]\n' >"$work/reviews-later.json"
	printf '{"permission":"write"}\n' >"$work/permission.json"
	printf '{"permission":"none"}\n' >"$work/permission-later.json"
	rm -f "$work/pr-calls" "$work/reviews-calls" "$work/permission-calls" "$work/api-calls"
}

same_repo() {
	jq '.head.repo = .base.repo' "$work/pr.json" >"$work/pr-new.json"
	mv "$work/pr-new.json" "$work/pr.json"
}

bdfl_author() {
	jq '.user = {id:991668,login:"Dunky13"}' "$work/pr.json" >"$work/pr-new.json"
	mv "$work/pr-new.json" "$work/pr.json"
	printf '{"user":{"id":991668},"permission":"admin"}\n' >"$work/permission.json"
	printf '{"user":{"id":991668},"permission":"none"}\n' >"$work/permission-later.json"
}

approval() {
	printf '[[{"id":20,"user":{"id":11,"login":"maintainer"},"state":"APPROVED","commit_id":"%s","submitted_at":"2026-10-01T00:00:00Z"}]]\n' "$head" >"$work/reviews.json"
}

gate() {
	: >"$work/summary"
	PATH="$work/bin:$PATH" FIXTURES=$work GH_REPO=o/r PR_NUMBER=7 HEAD_SHA=${1:-$head} \
		GITHUB_STEP_SUMMARY="$work/summary" FORK_GATE_API_RETRY_SECONDS=0 FORK_GATE_TIMEOUT_SECONDS=${TEST_GATE_TIMEOUT_SECONDS:-0} FORK_GATE_POLL_SECONDS=0 "$script" >/dev/null 2>"$work/stderr"
}

# expect_pending <description>: rejected only by the deadline, never as a result.
expect_pending() {
	if gate; then
		printf 'fork gate fixture failed: accepted %s\n' "$1" >&2
		exit 1
	fi
	grep -F 'no completed fork-ci run' "$work/stderr" >/dev/null || {
		printf 'fork gate fixture failed: treated %s as a result: %s\n' "$1" "$(cat "$work/stderr")" >&2
		exit 1
	}
}

expect_accept() {
	gate || { printf 'fork gate fixture failed: rejected %s\n' "$1" >&2; exit 1; }
}

expect_reject() {
	if gate "${2:-}"; then
		printf 'fork gate fixture failed: accepted %s\n' "$1" >&2
		exit 1
	fi
}

docs='[{"filename":"docs/site/index.mdx"}]'
done_run='{"workflow_runs":[{"id":42,"status":"completed","display_title":"fork-ci #7"}]}'

fixture 1 "$docs" "$done_run" success
expect_accept 'a docs-only fork PR whose fork-ci gate passed'
expect_reject 'a PR head that moved since the event' fedcba9876543210fedcba9876543210fedcba98

# GitHub App authors have a literal [bot] suffix, but this does not grant
# workflow-edit authority or relax the reviewer/BDFL identity checks.
fixture 1 "$docs" "$done_run" success
jq '.user.login = "dependabot[bot]"' "$work/pr.json" >"$work/pr-new.json"
mv "$work/pr-new.json" "$work/pr.json"
expect_accept 'valid Dependabot author metadata on a docs-only passing PR'
fixture 1 '[{"filename":".github/workflows/ci.yml"}]' "$done_run" success
same_repo
jq '.user.login = "dependabot[bot]"' "$work/pr.json" >"$work/pr-new.json"
mv "$work/pr-new.json" "$work/pr.json"
expect_reject 'bot workflow edits still require independent maintainer approval'
grep -F 'workflow edits require an independent current maintainer approval' "$work/stderr" >/dev/null
approval
expect_accept 'bot workflow edits with independent exact-head maintainer approval'
for login in '[bot]' 'dependabot[other]' 'dependabot[bot][bot]' 'dependabot[bot]suffix' 'dependabot bot'; do
	fixture 1 "$docs" "$done_run" success
	jq --arg login "$login" '.user.login = $login' "$work/pr.json" >"$work/pr-new.json"
	mv "$work/pr-new.json" "$work/pr.json"
	expect_reject "malformed bot author login $login"
	grep -F 'invalid PR metadata from API' "$work/stderr" >/dev/null
done

fixture 1 "$docs" "$done_run" success
jq '.mergeable = null' "$work/pr.json" >"$work/pr-new.json"
mv "$work/pr-new.json" "$work/pr.json"
expect_reject 'completed validation while mergeability remains unknown'
grep -F 'PR mergeability is still pending' "$work/stderr" >/dev/null
rm -f "$work/pr-calls"
( MERGEABLE_RESOLVES_AT=4 TEST_GATE_TIMEOUT_SECONDS=10 expect_accept 'completed validation after a later poll resolves mergeability' )
[ "$(cat "$work/pr-calls")" -ge 5 ] || {
	printf 'fork gate fixture failed: mergeability was not checked on a later poll\n' >&2
	exit 1
}
fixture 1 "$docs" "$done_run" success
( MERGEABLE_PENDING_AT=3 expect_reject 'mergeability becomes unknown immediately before accepting validation' )
grep -F 'PR mergeability is still pending' "$work/stderr" >/dev/null

fixture 1 "$docs" "$done_run" failure
expect_reject 'a failed fork-ci gate'
fixture 1 "$docs" "$done_run" skipped
expect_reject 'a skipped fork-ci gate'

fixture 1 "$docs" '{"workflow_runs":[{"id":42,"status":"completed","display_title":"fork-ci #8"}]}' success
expect_reject 'a passing run that belongs to another PR on the same commit'
fixture 1 "$docs" '{"workflow_runs":[{"id":42,"status":"in_progress","display_title":"fork-ci #7"}]}' success
expect_pending 'an unfinished fork-ci run'
fixture 1 "$docs" '{"workflow_runs":[{"id":42,"status":"completed","conclusion":"action_required","display_title":"fork-ci #7"}]}' success
expect_pending 'a fork-ci run awaiting maintainer approval'
fixture 1 "$docs" '{"workflow_runs":[]}' success
expect_pending 'no fork-ci run at all'
jq '.mergeable = false' "$work/pr.json" >"$work/pr-new.json"
mv "$work/pr-new.json" "$work/pr.json"
expect_reject 'a conflicted PR whose validation cannot start'
grep -F 'PR has merge conflicts' "$work/stderr" >/dev/null || {
	printf 'fork gate fixture failed: conflicted PR did not fail immediately\n' >&2
	exit 1
}

fixture 2 '[{"filename":"docs/a.md"},{"filename":".github/workflows/ci-fork.yml"}]' "$done_run" success
expect_reject 'a fork PR that edits a workflow'
fixture 1 '[{"filename":"scripts/x.yml","previous_filename":".github/workflows/ci.yml"}]' "$done_run" success
expect_reject 'a fork PR that renames a workflow away'
fixture 3001 "$docs" "$done_run" success
expect_reject 'a truncated file list'

workflow='[{"filename":".github/workflows/ci-fork.yml"}]'
fixture 1 "$workflow" "$done_run" success
same_repo
bdfl_author
expect_accept 'the pinned BDFL with current maintainer permission and no reviews'
for permission in write maintain; do
	printf '{"user":{"id":991668},"permission":"%s"}\n' "$permission" >"$work/permission.json"
	expect_accept "BDFL with current $permission permission"
done
for permission in read triage none; do
	printf '{"user":{"id":991668},"permission":"%s"}\n' "$permission" >"$work/permission.json"
	expect_reject "BDFL without maintainer permission ($permission)"
done
printf '{"user":{"id":991668},"permission":"admin"}\n' >"$work/permission.json"
( PERMISSION_ERROR=true expect_reject 'BDFL permission API failure' )
rm -f "$work/permission-calls"
( PERMISSION_REMOVED_AT=2 expect_reject 'BDFL permission revoked before validation is accepted' )
[ "$(cat "$work/permission-calls")" -eq 2 ]
rm -f "$work/pr-calls"
( HEAD_MOVES_AT=3 expect_reject 'BDFL head moved while checking current permission' )
[ "$(cat "$work/pr-calls")" -eq 3 ]
printf '{"user":{"id":10},"permission":"admin"}\n' >"$work/permission.json"
expect_reject 'BDFL permission lookup returned another account'
bdfl_author
jq '.user.login = "renamed-owner"' "$work/pr.json" >"$work/pr-new.json"
mv "$work/pr-new.json" "$work/pr.json"
expect_accept 'BDFL identity survives an account rename'
fixture 1 "$workflow" "$done_run" success
same_repo
jq '.user.login = "Dunky13"' "$work/pr.json" >"$work/pr-new.json"
mv "$work/pr-new.json" "$work/pr.json"
expect_reject 'another user copying the BDFL login without the pinned ID'
fixture 1 "$workflow" "$done_run" success
bdfl_author
expect_reject 'BDFL exemption cannot authorize fork-controlled workflows'
fixture 1 "$workflow" "$done_run" failure
same_repo
bdfl_author
expect_reject 'BDFL still needs successful exact-head validation'

fixture 1 "$workflow" "$done_run" success
same_repo
expect_reject 'same-repo workflow edits without independent approval'
approval
expect_accept 'same-repo workflow edits with exact-head independent maintainer approval'
jq '.[0] = [.[0][0] | .user = {id:1,login:"reviewer[bot]",type:"Bot"}]' "$work/reviews.json" >"$work/reviews-new.json"
mv "$work/reviews-new.json" "$work/reviews.json"
expect_reject 'a bot approval without an independent maintainer'
grep -F 'workflow edits require an independent current maintainer approval' "$work/stderr" >/dev/null
bot_review=$(cat "$work/reviews.json")
approval
jq --argjson bot "$bot_review" '.[0] = $bot[0] + .[0]' "$work/reviews.json" >"$work/reviews-new.json"
mv "$work/reviews-new.json" "$work/reviews.json"
expect_accept 'bot review preceding a valid independent maintainer approval'
approval
( REVIEWS_ERROR=true expect_reject 'review API failure despite an approved fixture' )
for permission in admin maintain; do
	printf '{"permission":"%s"}\n' "$permission" >"$work/permission.json"
	expect_accept "current $permission maintainer approval"
done
printf '{"permission":"write"}\n' >"$work/permission.json"
( PERMISSION_ERROR=true expect_reject 'failed collaborator permission lookup' )
for permission in read triage none; do
	printf '{"permission":"%s"}\n' "$permission" >"$work/permission.json"
	expect_reject "reviewer with only $permission permission"
done
printf '{"permission":"write"}\n' >"$work/permission.json"
jq '.[0][0].user = {id:10,login:"author"}' "$work/reviews.json" >"$work/reviews-new.json"
mv "$work/reviews-new.json" "$work/reviews.json"
expect_reject 'PR author self-approval'
approval
jq '.[0][0].commit_id = "older-head"' "$work/reviews.json" >"$work/reviews-new.json"
mv "$work/reviews-new.json" "$work/reviews.json"
expect_reject 'approval of a stale head'
approval
jq '.[0] += [.[0][0] | .id = 21 | .state = "DISMISSED" | .submitted_at = "2026-10-01T00:01:00Z"]' "$work/reviews.json" >"$work/reviews-new.json"
mv "$work/reviews-new.json" "$work/reviews.json"
expect_reject 'latest reviewer state dismissed an earlier approval'
jq '[.[0][0:1], .[0][1:]]' "$work/reviews.json" >"$work/reviews-new.json"
mv "$work/reviews-new.json" "$work/reviews.json"
expect_reject 'latest review on a later API page dismissed an earlier approval'
approval
jq '.head.repo.id = 2' "$work/pr.json" >"$work/pr-new.json"
mv "$work/pr-new.json" "$work/pr.json"
expect_reject 'fork spoofing same-repository name despite a maintainer approval'
fixture 1 "$workflow" "$done_run" success
approval
expect_reject 'approved fork workflow changes'
fixture 1 "$workflow" "$done_run" success
same_repo
approval
( HEAD_MOVES_AT=3 expect_reject 'head moved while checking maintainer approval' )
fixture 1 "$workflow" "$done_run" success
same_repo
approval
( REVIEWS_CHANGE_AT=2 expect_reject 'approval dismissed before the completed gate is accepted' )
fixture 1 "$workflow" "$done_run" success
same_repo
approval
( PERMISSION_REMOVED_AT=2 expect_reject 'collaborator removed while validation ran' )
fixture 1 "$docs" "$done_run" success
( HEAD_MOVES_AT=3 expect_reject 'head moved after checking completed validation jobs' )

# Transport retry boundaries and actionable summaries must fail closed.
for error in 'HTTP 429: Too Many Requests' 'HTTP 503: Service Unavailable' 'dial tcp: i/o timeout'; do
	fixture 1 "$docs" "$done_run" success
	( API_ERROR="$error" API_ERRORS_BEFORE_SUCCESS=2 expect_accept "transient $error" )
	[ "$(cat "$work/api-calls")" -eq 5 ]
done
fixture 1 "$docs" "$done_run" success
( API_ERROR='HTTP 503: Service Unavailable' expect_reject 'exhausted transient retries' )
[ "$(cat "$work/api-calls")" -eq 3 ]
grep -F 'api-failure' "$work/summary" >/dev/null
for error in 'HTTP 403: Forbidden' 'HTTP 404: Not Found' 'unknown API error'; do
	fixture 1 "$docs" "$done_run" success
	( API_ERROR="$error" expect_reject "permanent $error" )
	[ "$(cat "$work/api-calls")" -eq 1 ]
done
fixture 1 "$docs" '{"workflow_runs":[{"id":null,"status":"completed","display_title":"fork-ci #7"}]}' success
expect_reject 'invalid validation run identity'
grep -F 'api-failure' "$work/summary" >/dev/null
fixture 1 "$docs" "$done_run" success
( MALFORMED_API=true expect_reject 'successful HTTP response with non-JSON body' )
[ "$(cat "$work/api-calls")" -eq 1 ]
grep -F 'malformed API response' "$work/stderr" >/dev/null
fixture 1 '[{"filename":null}]' "$done_run" success
expect_reject 'missing changed filename could conceal workflow edits'
grep -F 'api-failure' "$work/summary" >/dev/null
fixture 1 "$docs" "$done_run" success
jq 'del(.user.id)' "$work/pr.json" >"$work/pr-new.json"
mv "$work/pr-new.json" "$work/pr.json"
expect_reject 'syntactically valid but malformed PR metadata'
grep -F 'api-failure' "$work/summary" >/dev/null
fixture 1 "$docs" "$done_run" failure
jq '.jobs += [{id:123,name:"validation / web-go",conclusion:"failure"}]' "$work/jobs.json" >"$work/jobs-new.json"
mv "$work/jobs-new.json" "$work/jobs.json"
expect_reject 'underlying web-go failure'
grep -F 'validation / web-go: failure (https://github.com/o/r/actions/runs/42/job/123)' "$work/summary" >/dev/null
grep -F 'validation-failure' "$work/summary" >/dev/null
fixture 1 "$docs" '{"workflow_runs":[{"id":42,"status":"completed","conclusion":"cancelled","display_title":"fork-ci #7"}]}' cancelled
expect_reject 'cancelled validation'
grep -F 'cancelled' "$work/summary" >/dev/null
fixture 1 "$docs" "$done_run" failure
jq '.jobs += [{id:124,name:"validation / go-race",conclusion:"timed_out"}]' "$work/jobs.json" >"$work/jobs-new.json"
mv "$work/jobs-new.json" "$work/jobs.json"
expect_reject 'upstream timeout behind a failed aggregate'
grep -F 'Fork validation gate: timeout' "$work/summary" >/dev/null
fixture 1 "$docs" "$done_run" success
expect_reject 'superseded head summary' fedcba9876543210fedcba9876543210fedcba98
grep -F 'superseded' "$work/summary" >/dev/null
fixture 1 "$workflow" "$done_run" success
same_repo
expect_reject 'policy refusal summary'
grep -F 'policy-refusal' "$work/summary" >/dev/null

printf 'fork gate fixture: exact-head validation, untouched fork YAML, and same-repo workflow authority from independent approval or the pinned current-maintainer BDFL\n'
