#!/usr/bin/env bash
# Exercise the real supervisor with controlled commands, no contributor state.
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
script="$script_dir/fast-feedback.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-feedback-deadline.XXXXXX")
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/repo" "$work/bin"
cd "$work/repo"
git init -q
export GIT_AUTHOR_NAME=Fixture GIT_AUTHOR_EMAIL=fixture@example.invalid
export GIT_COMMITTER_NAME=$GIT_AUTHOR_NAME GIT_COMMITTER_EMAIL=$GIT_AUTHOR_EMAIL
commit_fixture() {
	git add .
	local tree revision
	tree=$(git write-tree)
	if git rev-parse --verify HEAD >/dev/null 2>&1; then
		revision=$(printf 'fixture\n' | git commit-tree "$tree" -p HEAD)
	else
		revision=$(printf 'fixture\n' | git commit-tree "$tree")
	fi
	git update-ref HEAD "$revision"
}
printf '26.7.0\n' >.nvmrc
printf '{"packageManager":"pnpm@11.24.0"}\n' >package.json
printf 'module fixture.invalid/check\n\ngo 1.27.2\n' >go.mod
commit_fixture
FAST_FEEDBACK_BASE=$(git rev-parse HEAD)
export FAST_FEEDBACK_BASE
unset FAST_FEEDBACK_HEAD FAST_FEEDBACK_TOOLS_DIR FAST_FEEDBACK_DEADLINE
mkdir -p first/common second/common web/node_modules clients/ts/node_modules
printf 'package common\n\nfunc init() { panic("must not execute") }\n' >first/common/fixture.go
printf 'package common\n' >second/common/fixture.go
printf 'export const fixture = true;\n' >web/fixture.ts
printf 'export const fixture = true;\n' >clients/ts/fixture.ts
commit_fixture
FIXTURE_REAL_GO=$(command -v go)
export FIXTURE_REAL_GO FIXTURE_WORK=$work
cat >"$work/bin/go" <<'STUB'
#!/usr/bin/env bash
set -eu
if [ "$1" != test ]; then exec "$FIXTURE_REAL_GO" "$@"; fi
printf '%s\n' "$*" >"$FIXTURE_WORK/go.command"
touch "$FIXTURE_WORK/go.started"
case "$FIXTURE_MODE" in
	pass)
		while [ ! -e "$FIXTURE_WORK/frontend.started" ]; do sleep .02; done
		sleep .2; touch "$FIXTURE_WORK/go.finished" ;;
	early-124) exit 124 ;;
	early-137) kill -KILL "$BASHPID" ;;
	fail-go) while [ ! -e "$FIXTURE_WORK/frontend.child" ]; do sleep .02; done; exit 31 ;;
	*)
		bash -c 'trap "" TERM; echo "$BASHPID" >"$FIXTURE_WORK/go.child"; while :; do sleep 1; done' &
		wait ;;
esac
STUB
cat >"$work/bin/pnpm" <<'STUB'
#!/usr/bin/env bash
set -eu
if [ "$1" = --version ]; then printf '11.24.0\n'; exit; fi
printf '%s\n' "$*" >>"$FIXTURE_WORK/frontend.commands"
touch "$FIXTURE_WORK/frontend.started"
case "$FIXTURE_MODE" in
	pass)
		while [ ! -e "$FIXTURE_WORK/go.started" ]; do sleep .02; done
		touch "$FIXTURE_WORK/frontend.finished" ;;
	early-124) exit 124 ;;
	early-137) kill -KILL "$BASHPID" ;;
	fail-frontend) while [ ! -e "$FIXTURE_WORK/go.child" ]; do sleep .02; done; exit 32 ;;
	*)
		bash -c 'trap "" TERM; echo "$BASHPID" >"$FIXTURE_WORK/frontend.child"; while :; do sleep 1; done' &
		wait ;;
esac
STUB
cat >"$work/bin/node" <<'STUB'
#!/bin/sh
printf 'v26.7.0\n'
STUB
chmod +x "$work/bin/"*
export PATH="$work/bin:$PATH"
export FAST_FEEDBACK_SUPERVISOR_PID=$PPID # Inherited marker cannot bypass isolation.
# SIGKILL delivery is asynchronous, especially for Linux uninterruptible I/O.
# Give all captured children one bounded settling window, accepting only absent
# or zombie processes. Every other state remains a leak if it survives the bound.
assert_children_gone() {
	local attempt pid state started=$SECONDS
	local survivors=()
	for ((attempt = 0; attempt < 40; attempt++)); do
		survivors=()
		for pid in "$@"; do
			state=$(ps -o stat= -p "$pid" 2>/dev/null | tr -d '[:space:]' || true)
			if [ -n "$state" ] && [[ "$state" != Z* ]]; then survivors+=("$pid $state"); fi
		done
		[ "${#survivors[@]}" -gt 0 ] || return 0
		[ "$((SECONDS - started))" -lt 2 ] || break
		if [ "$attempt" -lt 39 ]; then sleep .05; fi
	done
	printf 'surviving descendant %s\n' "${survivors[@]}" >&2
	return 1
}
run_case() {
	rm -f "$work/"*.started "$work/"*.finished "$work/"*.child "$work/"*.commands
	export FIXTURE_MODE=$1 FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 3))
	local status=0
	bash "$script" >"$work/output" 2>&1 || status=$?
	if [ "$1" = pass ]; then
		[ "$status" -eq 0 ] || { cat "$work/output" >&2; exit 1; }
		[ -e "$work/go.finished" ] && [ -e "$work/frontend.finished" ]
		grep -Fx 'test -c -o /dev/null ./first/common ./second/common' "$work/go.command"
		diff -u <(printf '%s\n' '--dir clients/ts run typecheck' '--dir clients/ts run test' '--dir web run typecheck' '--dir web run lint' '--dir web run test') "$work/frontend.commands"
	else
		[ "$status" -ne 0 ]
		if grep -F 'selected checks passed' "$work/output"; then exit 1; fi
		if [ "$1" = timeout ]; then grep -F 'deadline exhausted' "$work/output"; fi
		grep -F 'lane incomplete' "$work/output"
		case "$1" in
			fail-go) [ -s "$work/frontend.child" ] ;;
			fail-frontend) [ -s "$work/go.child" ] ;;
			timeout) [ -s "$work/go.child" ] && [ -s "$work/frontend.child" ] ;;
		esac
		local children=()
		for path in "$work/"*.child; do
			[ -f "$path" ] || continue
			children+=("$(cat "$path")")
		done
		assert_children_gone "${children[@]}"
	fi
}
# Exercise the actual death oracle deterministically: transient D is not
# accepted until a later absent/Z observation; sustained live states stay red.
mkdir "$work/ps-oracle"
cat >"$work/ps-oracle/ps" <<'STUB'
#!/usr/bin/env bash
set -eu
pid=$4
count_file="$FIXTURE_WORK/ps-$pid.count"
count=0
[ ! -f "$count_file" ] || count=$(cat "$count_file")
printf '%s\n' "$((count + 1))" >"$count_file"
case "$pid" in
	101) [ "$count" -gt 0 ] || printf 'D\n' ;;
	102) if [ "$count" -eq 0 ]; then printf 'D\n'; else printf 'Z\n'; fi ;;
	103) printf 'D\n' ;;
	104) printf 'R\n' ;;
	105) printf 'S\n' ;;
	*) exit 1 ;;
esac
STUB
chmod +x "$work/ps-oracle/ps"
PATH="$work/ps-oracle:$PATH" assert_children_gone 101 102
[ "$(cat "$work/ps-101.count")" -eq 2 ] && [ "$(cat "$work/ps-102.count")" -eq 2 ]
if PATH="$work/ps-oracle:$PATH" assert_children_gone 103 104 105 >"$work/oracle-output" 2>&1; then
	printf 'death oracle accepted sustained live states\n' >&2
	exit 1
fi
for expected in '103 D' '104 R' '105 S'; do grep -F "surviving descendant $expected" "$work/oracle-output"; done
for pid in 103 104 105; do
	count=$(cat "$work/ps-$pid.count")
	[ "$count" -gt 1 ] && [ "$count" -le 40 ]
done
# A check's status is not timeout evidence. Execute the real helper and
# setup wrapper with early exits, retaining exact nonzero status and diagnostics.
for code in 124 137; do
	for selected_lane in go frontend; do
		export FIXTURE_MODE="early-$code" FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 30))
		status=0
		bash "$script" "--lane=$selected_lane" >"$work/output" 2>&1 || status=$?
		[ "$status" -eq "$code" ] || { cat "$work/output" >&2; exit 1; }
		if grep -F 'deadline exhausted' "$work/output"; then exit 1; fi
		grep -F "check failed (exit $code):" "$work/output"
		if grep -F 'selected checks passed' "$work/output"; then exit 1; fi
	done
	status=0
	FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 30)) bash "$script_dir/feedback-budget.sh" bash -c "exit $code" >"$work/output" 2>&1 || status=$?
	[ "$status" -eq "$code" ]
	if grep -F 'deadline exhausted' "$work/output"; then exit 1; fi
	grep -F "setup command failed (exit $code):" "$work/output"
done
# Cleanup may cross the deadline after an early failure. Advance a controlled
# clock only when the real supervisor begins its one-second cleanup escalation.
mkdir "$work/cleanup-clock"
FIXTURE_REAL_DATE=$(command -v date)
FIXTURE_REAL_SLEEP=$(command -v sleep)
export FIXTURE_REAL_DATE FIXTURE_REAL_SLEEP
cat >"$work/cleanup-clock/date" <<'STUB'
#!/usr/bin/env bash
set -eu
if [ "${1:-}" = +%s ]; then
	if [ -e "$FIXTURE_WORK/cleanup.started" ]; then
		printf '%s\n' "$((FAST_FEEDBACK_DEADLINE + 1))"
	else
		printf '%s\n' "$((FAST_FEEDBACK_DEADLINE - 30))"
	fi
else
	exec "$FIXTURE_REAL_DATE" "$@"
fi
STUB
cat >"$work/cleanup-clock/sleep" <<'STUB'
#!/usr/bin/env bash
set -eu
[ "${1:-}" != 1 ] || touch "$FIXTURE_WORK/cleanup.started"
exec "$FIXTURE_REAL_SLEEP" "$@"
STUB
chmod +x "$work/cleanup-clock/"*
for code in 124 137; do
	rm -f "$work/cleanup.started"
	export FIXTURE_MODE="early-$code" FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 30))
	status=0
	PATH="$work/cleanup-clock:$PATH" bash "$script" --lane=go >"$work/output" 2>&1 || status=$?
	[ "$status" -eq "$code" ] && [ -e "$work/cleanup.started" ] || { cat "$work/output" >&2; exit 1; }
	if grep -F 'deadline exhausted' "$work/output"; then exit 1; fi
	grep -F "check failed (exit $code):" "$work/output"
done
run_case pass
run_case fail-go
run_case fail-frontend
run_case timeout
for deadline in '' malformed 0 -1 1; do
	status=0
	FAST_FEEDBACK_DEADLINE="$deadline" bash "$script" >"$work/output" 2>&1 || status=$?
	[ "$status" -ne 0 ]
	grep -E 'invalid FAST_FEEDBACK_DEADLINE|deadline exhausted before checks' "$work/output"
done
# The generic setup wrapper uses the same epoch and refuses exhausted reserves.
FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 3)) bash "$script_dir/feedback-budget.sh" --reserve 60 >"$work/output" 2>&1 && exit 1
grep -F 'insufficient setup headroom' "$work/output"
status=0
FAST_FEEDBACK_DEADLINE=$(($(date +%s) + 1)) bash "$script_dir/feedback-budget.sh" bash -c 'trap "" TERM; while :; do sleep 1; done' >"$work/output" 2>&1 || status=$?
[ "$status" -ne 0 ]
grep -F 'deadline exhausted during setup; selected checks did not complete' "$work/output"
printf 'fast-feedback deadline fixtures passed\n'
