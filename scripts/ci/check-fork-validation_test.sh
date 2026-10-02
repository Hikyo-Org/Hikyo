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
url= filter=.
while [ $# -gt 0 ]; do
	case $1 in
	api | --paginate | --slurp) ;;
	--jq) filter=$2; shift ;;
	*) url=$1 ;;
	esac
	shift
done
bump() {
	count=0
	if [ -f "$FIXTURES/$1-calls" ]; then count=$(cat "$FIXTURES/$1-calls"); fi
	count=$((count + 1))
	printf '%s\n' "$count" >"$FIXTURES/$1-calls"
}
case $url in
*/pulls/7/files*) fixture=files ;;
*/pulls/7/reviews*)
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
	printf '{"head":{"sha":"%s","repo":{"id":2,"full_name":"fork/r"}},"base":{"repo":{"id":1,"full_name":"o/r"}},"user":{"id":10,"login":"author"},"changed_files":%s}\n' "$head" "$1" >"$work/pr.json"
	printf '%s\n' "$2" >"$work/files.json"
	printf '%s\n' "$3" >"$work/runs.json"
	printf '{"jobs":[{"name":"validation / changes","conclusion":"success"},{"name":"validation / ci-required","conclusion":"%s"}]}\n' "$4" >"$work/jobs.json"
	printf '[[]]\n' >"$work/reviews.json"
	printf '[[]]\n' >"$work/reviews-later.json"
	printf '{"permission":"write"}\n' >"$work/permission.json"
	printf '{"permission":"none"}\n' >"$work/permission-later.json"
	rm -f "$work/pr-calls" "$work/reviews-calls" "$work/permission-calls"
}

same_repo() {
	jq '.head.repo = .base.repo' "$work/pr.json" >"$work/pr-new.json"
	mv "$work/pr-new.json" "$work/pr.json"
}

approval() {
	printf '[[{"id":20,"user":{"id":11,"login":"maintainer"},"state":"APPROVED","commit_id":"%s","submitted_at":"2026-10-01T00:00:00Z"}]]\n' "$head" >"$work/reviews.json"
}

gate() {
	PATH="$work/bin:$PATH" FIXTURES=$work GH_REPO=o/r PR_NUMBER=7 HEAD_SHA=${1:-$head} \
		FORK_GATE_TIMEOUT_SECONDS=0 FORK_GATE_POLL_SECONDS=0 "$script" >/dev/null 2>"$work/stderr"
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

fixture 2 '[{"filename":"docs/a.md"},{"filename":".github/workflows/ci-fork.yml"}]' "$done_run" success
expect_reject 'a fork PR that edits a workflow'
fixture 1 '[{"filename":"scripts/x.yml","previous_filename":".github/workflows/ci.yml"}]' "$done_run" success
expect_reject 'a fork PR that renames a workflow away'
fixture 3001 "$docs" "$done_run" success
expect_reject 'a truncated file list'

workflow='[{"filename":".github/workflows/ci-fork.yml"}]'
fixture 1 "$workflow" "$done_run" success
same_repo
expect_reject 'same-repo workflow edits without independent approval'
approval
expect_accept 'same-repo workflow edits with exact-head independent maintainer approval'
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

printf 'fork gate fixture: exact-head validation, untouched fork YAML, and independently approved same-repo workflow edits only\n'
