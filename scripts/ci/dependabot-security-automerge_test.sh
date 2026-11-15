#!/bin/sh
# Fixture for dependabot-security-automerge.sh with a fake gh. Each scenario
# states which pull requests may be armed; anything else must stay untouched.
set -eu

here=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
script=$here/dependabot-security-automerge.sh
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-dependabot-automerge-test.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM

fail() {
	printf 'dependabot-security-automerge fixture: %s\n' "$*" >&2
	exit 1
}

cat >"$scratch/gh" <<'EOF'
#!/bin/sh
# Scenario knobs arrive as environment variables. State lives in files under $FIX.
prev=
printf '%s\n' "$*" >>"$FIX/calls"
dep='"user":{"id":49699333,"login":"dependabot[bot]"}'
case "$*" in
	'api graphql '*)
		[ "${ALERTS:-ok}" != fail ] || { echo 'gh: Resource not accessible by integration (HTTP 403)' >&2; exit 1; }
		if [ "${ALERTS:-ok}" = null ]; then echo '{"data":{"repository":{"vulnerabilityAlerts":null}},"errors":[{"message":"forbidden"}]}'; exit 0; fi
		if [ "${ALERT_PAGES:-1}" = 2 ]; then
			echo '{"data":{"repository":{"vulnerabilityAlerts":{"nodes":[{"number":1,"dependabotUpdate":null}]}}}}'
			echo '{"data":{"repository":{"vulnerabilityAlerts":{"nodes":[{"number":2,"dependabotUpdate":{"pullRequest":{"number":'"${LINKED:-11}"'}}}]}}}}'
		else
			echo '{"data":{"repository":{"vulnerabilityAlerts":{"nodes":[{"number":2,"dependabotUpdate":{"pullRequest":{"number":'"${LINKED:-11}"'}}},{"number":3,"dependabotUpdate":null}]}}}}'
		fi ;;
	'api --paginate repos/o/r/pulls?state=open&base=main&per_page=100')
		[ "${PULLS:-ok}" != fail ] || { echo 'gh: boom' >&2; exit 1; }
		pr() { printf '{"number":%s,"head":{"sha":"%s","ref":"%s","repo":{"full_name":"%s"}},"draft":%s,"auto_merge":%s,%s}' "$1" "$2" "$3" "$4" "$5" "$6" "$7"; }
		a=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
		b=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
		c=cccccccccccccccccccccccccccccccccccccccc
		printf '['
		user=$dep
		[ "${LOGIN:-}" != impostor ] || user='"user":{"id":49699333,"login":"someone[bot]"}'
		pr 11 $a "${REF:-dependabot/go_modules/x}" "${HEAD_REPO:-o/r}" "${DRAFT:-false}" "${ARMED:-null}" "$user"
		printf ','
		pr 12 $b dependabot/go_modules/y o/r false null "$dep"
		printf ','
		pr 13 $c dependabot/go_modules/z o/r false null '"user":{"id":99999999,"login":"dependabot[bot]"}'
		printf ',{"number":14,"head":{"sha":"dddddddddddddddddddddddddddddddddddddddd","ref":"feature","repo":{"full_name":"o/r"}},"draft":false,"auto_merge":null,"user":{"id":1,"login":"person"}}'
		printf ']\n' ;;
	'api --paginate repos/o/r/pulls/'*'/commits?per_page=100')
		n=${3#repos/o/r/pulls/}; n=${n%%/*}
		sha=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
		author=49699333 committer=19864447 verified=true reason=valid
		case "${COMMIT:-ok}" in
			foreign-author) author=1 ;;
			foreign-committer) committer=1 ;;
			unverified) verified=false reason=unsigned ;;
			other-head) sha=eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee ;;
			empty) echo '[]'; exit 0 ;;
			many) jq -nc --arg sha "$sha" '[range(0;100) | {sha:(if . == 0 then $sha else "x" end),author:{id:49699333},committer:{id:19864447},commit:{verification:{verified:true,reason:"valid"}}}]'; exit 0 ;;
		esac
		printf '[{"sha":"%s","author":{"id":%s},"committer":{"id":%s},"commit":{"verification":{"verified":%s,"reason":"%s"}}}]\n' "$sha" "$author" "$committer" "$verified" "$reason" ;;
	'api --paginate repos/o/r/pulls/'*'/files?per_page=100')
		case "${FILES:-ok}" in
			ok) echo '[{"filename":"go.mod","status":"modified"},{"filename":"scripts/mcp/pnpm-lock.yaml","status":"modified"}]' ;;
			workflow) echo '[{"filename":"go.mod","status":"modified"},{"filename":".github/workflows/ci.yml","status":"modified"}]' ;;
			rename) echo '[{"filename":"go.sum","previous_filename":"cmd/main.go","status":"renamed"}]' ;;
			source) echo '[{"filename":"internal/go.mod.go","status":"modified"}]' ;;
			empty) echo '[]' ;;
		esac ;;
	'api repos/o/r/commits/'*'/check-runs?check_name=ci-queue-required&per_page=100 --jq '*)
		filter=
		for arg in "$@"; do [ "$prev" != --jq ] || filter=$arg; prev=$arg; done
		other='{"id":9,"app":{"id":7,"slug":"other-app"},"status":"completed","conclusion":"success"}'
		case "${QUEUE_CHECK:-none}" in
			none) runs=$other ;;
			success) runs='{"id":5,"app":{"id":15368,"slug":"github-actions"},"status":"completed","conclusion":"success"}' ;;
			failure) runs='{"id":5,"app":{"id":15368,"slug":"github-actions"},"status":"completed","conclusion":"failure"},'$other ;;
			cancelled) runs='{"id":5,"app":{"id":15368,"slug":"github-actions"},"status":"completed","conclusion":"cancelled"},'$other ;;
			fail) echo 'gh: boom' >&2; exit 1 ;;
		esac
		printf '{"check_runs":[%s]}' "$runs" | jq -c "$filter" ;;
	'pr merge '*)
		[ "${ARM_FAIL:-}" != "$3" ] || { echo 'gh: auto merge is not allowed' >&2; exit 1; } ;;
	*) echo "unexpected gh call: $*" >&2; exit 2 ;;
esac
EOF
chmod +x "$scratch/gh"

# run NAME EXPECT_STATUS EXPECT_ARMED -- VAR=value...
run() {
	name=$1 want_status=$2 want_armed=$3
	shift 3
	: >"$scratch/calls"
	status=0
	env GH_BIN="$scratch/gh" GH_REPO=o/r GH_TOKEN=token FIX="$scratch" "$@" "$script" >"$scratch/out" 2>"$scratch/err" || status=$?
	[ "$status" = "$want_status" ] || { cat "$scratch/out" "$scratch/err" >&2; fail "$name: exit $status, want $want_status"; }
	armed=$(grep '^pr merge ' "$scratch/calls" | awk '{print $3}' | sort | tr '\n' ' ' | sed 's/ $//')
	[ "$armed" = "$want_armed" ] || { cat "$scratch/out" >&2; fail "$name: armed '$armed', want '$want_armed'"; }
}

run 'security update is queued, the routine bump and the impostor are not' 0 11
grep -F 'pr merge 11 --repo o/r --auto --merge --match-head-commit aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' "$scratch/calls" >/dev/null ||
	fail 'the merge must be armed as a merge-queue auto-merge pinned to the checked head'
grep -F 'Left for a person: #12' "$scratch/out" >/dev/null || fail 'a routine Dependabot bump must be reported as left for a person'
run 'alert page two links the pull request' 0 11 ALERT_PAGES=2
run 'an alert linked to someone else is not a security update' 0 '' LINKED=14
run 'an alert linked to an impostor id is not a security update' 0 '' LINKED=13
run 'already armed is not armed again' 0 '' ARMED='{"merge_method":"merge"}'
run 'dry run arms nothing' 0 '' DRY_RUN=true
grep -F 'Would queue security update #11' "$scratch/out" >/dev/null || fail 'a dry run must say what it would queue'
grep -F 'states:[OPEN]' "$scratch/calls" >/dev/null || fail 'only open alerts may prove a security update'
grep -F 'pageInfo{hasNextPage endCursor}' "$scratch/calls" >/dev/null || fail 'the alert query must select pageInfo so that every page is read'
grep -F 'api graphql --paginate' "$scratch/calls" >/dev/null || fail 'the alert query must be paginated'
run 'an author id without the Dependabot login is refused' 0 '' LOGIN=impostor
run 'a head outside this repository is refused' 1 '' HEAD_REPO=other/r
run 'a branch that is not a Dependabot branch is refused' 1 '' REF=feature/x
run 'a draft is refused' 1 '' DRAFT=true
run 'a commit list at the limit is refused' 1 '' COMMIT=many
run 'an unreadable pull request list stops the run' 1 '' PULLS=fail
run 'a successful admission of another app does not count' 1 '' QUEUE_CHECK=failure
run 'an unreadable alert list arms nothing' 1 '' ALERTS=fail
run 'a null alert answer arms nothing' 1 '' ALERTS=null
run 'a commit not authored by Dependabot is refused' 1 '' COMMIT=foreign-author
run 'a commit not committed by GitHub is refused' 1 '' COMMIT=foreign-committer
run 'an unverified commit is refused' 1 '' COMMIT=unverified
run 'a head missing from the commits is refused' 1 '' COMMIT=other-head
run 'a pull request without commits is refused' 1 '' COMMIT=empty
run 'a workflow file is refused' 1 '' FILES=workflow
run 'a renamed source file is refused' 1 '' FILES=rename
run 'a source file that merely looks like a manifest is refused' 1 '' FILES=source
run 'a pull request without files is refused' 1 '' FILES=empty
run 'a failed queue admission is not re-armed' 1 '' QUEUE_CHECK=failure
run 'a cancelled queue admission is not re-armed' 1 '' QUEUE_CHECK=cancelled
run 'a passing admission is armed' 0 11 QUEUE_CHECK=success
run 'an unreadable admission check arms nothing' 1 '' QUEUE_CHECK=fail
run 'a refused arm fails the run' 1 11 ARM_FAIL=11
grep -F 'GitHub refused to arm auto-merge' "$scratch/out" >/dev/null || fail 'a refused arm must be reported'

status=0
env GH_BIN="$scratch/gh" GH_REPO=o/r FIX="$scratch" "$script" >/dev/null 2>&1 || status=$?
[ "$status" != 0 ] || fail 'a missing token must stop the run'
status=0
env GH_BIN="$scratch/gh" GH_REPO='o/r;x' GH_TOKEN=t FIX="$scratch" "$script" >/dev/null 2>"$scratch/err" || status=$?
[ "$status" != 0 ] || fail 'an invalid repository must stop the run'
grep -F 'invalid repository' "$scratch/err" >/dev/null || fail 'an invalid repository must be named'

printf 'dependabot-security-automerge fixture: only linked, verified, manifest-only security updates are queued\n'
