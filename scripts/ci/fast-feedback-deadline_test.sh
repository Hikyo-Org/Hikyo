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
		for path in "$work/"*.child; do
			[ -f "$path" ] || continue
			pid=$(cat "$path")
			# A reaped or zombie process cannot continue executing descendants.
			state=$(ps -o stat= -p "$pid" || true)
			[ -z "$state" ] || [[ "$state" = Z* ]] || { printf 'surviving descendant %s %s\n' "$pid" "$state" >&2; exit 1; }
		done
	fi
}
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
