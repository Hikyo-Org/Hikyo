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
	api | --paginate) ;;
	--jq) filter=$2; shift ;;
	*) url=$1 ;;
	esac
	shift
done
case $url in
*/pulls/7/files*) fixture=files ;;
*/pulls/7) fixture=pr ;;
*/workflows/ci-fork.yml/runs*) fixture=runs ;;
*/runs/42/jobs*) fixture=jobs ;;
*) printf 'stub gh: unexpected %s\n' "$url" >&2; exit 1 ;;
esac
jq -r "$filter" "$FIXTURES/$fixture.json"
EOF
chmod +x "$work/bin/gh"

head=0123456789abcdef0123456789abcdef01234567

# fixture <changed_files> <files-json> <runs-json> <gate-conclusion>
fixture() {
	printf '{"head":{"sha":"%s"},"changed_files":%s}\n' "$head" "$1" >"$work/pr.json"
	printf '%s\n' "$2" >"$work/files.json"
	printf '%s\n' "$3" >"$work/runs.json"
	printf '{"jobs":[{"name":"validation / changes","conclusion":"success"},{"name":"validation / ci-required","conclusion":"%s"}]}\n' "$4" >"$work/jobs.json"
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

printf 'fork gate fixture: only an untouched-.github fork PR with a passing fork-ci gate on its exact head is accepted\n'
