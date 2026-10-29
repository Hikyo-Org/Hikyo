#!/usr/bin/env bash
# Developer feedback only. Full current-main candidate CI remains the merge gate.
# Node dependencies must already be installed; no source fixes or generation.
# Go checks may download modules/toolchains and populate normal Go caches.
set -euo pipefail

working_tree=false
plan_only=false
lane=all
go_scope=all
for argument in "$@"; do
	case "$argument" in
		--working-tree) working_tree=true ;;
		--plan) plan_only=true ;;
		--lane=*) lane=${argument#--lane=} ;;
		--go-scope=*) go_scope=${argument#--go-scope=} ;;
		*) printf 'usage: %s [--working-tree] [--plan] [--lane=all|go|frontend] [--go-scope=all|application|tooling]\n' "$0" >&2; exit 2 ;;
	esac
done

fail() { printf 'fast-feedback: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "missing $1; prepare the pinned contributor toolchain first"; }
case "$lane" in
	all | go | frontend) ;;
	*) fail 'invalid lane; expected all, go or frontend' ;;
esac
case "$go_scope" in
	all | application | tooling) ;;
	*) fail 'invalid Go scope; expected all, application or tooling' ;;
esac
[ "$lane" != frontend ] || [ "$go_scope" = all ] || fail 'frontend lane cannot select a Go scope'
# Planning code belongs to the immutable helper, even when the current working
# directory is a different, older or candidate-controlled source checkout.
feedback_tooling_dir=$(CDPATH='' cd -- "${FAST_FEEDBACK_TOOLS_DIR:-$(dirname "${BASH_SOURCE[0]}")}" && pwd)
[ -r "$feedback_tooling_dir/feedback-plan.sh" ] || fail 'missing feedback planning module in tooling checkout'
# shellcheck source=scripts/ci/feedback-plan.sh
source "$feedback_tooling_dir/feedback-plan.sh"

# CI supplies one absolute deadline before setup. Local feedback without that
# timestamp keeps its existing tool requirements and has no imposed deadline.
supervised=false
timeout_tool=''
if [ "${FAST_FEEDBACK_DEADLINE+x}" = x ]; then
	[[ "$FAST_FEEDBACK_DEADLINE" =~ ^[1-9][0-9]{0,10}$ ]] || fail 'invalid FAST_FEEDBACK_DEADLINE; expected a positive epoch timestamp'
	remaining=$((FAST_FEEDBACK_DEADLINE - $(date +%s)))
	[ "$remaining" -gt 0 ] || fail 'deadline exhausted before checks; selected checks did not complete'
	if [ "$plan_only" = false ]; then
		timeout_tool=$(command -v timeout || command -v gtimeout) || fail 'CI deadline needs GNU timeout (or gtimeout)'
		((BASH_VERSINFO[0] > 5 || (BASH_VERSINFO[0] == 5 && BASH_VERSINFO[1] >= 1))) || fail 'CI deadline needs Bash 5.1 or newer'
		group=$(ps -o pgid= -p "$BASHPID" | tr -d ' ')
		if [ "${FAST_FEEDBACK_SUPERVISOR_PID:-}" != "$PPID" ] || [ "$group" != "$PPID" ]; then
			# GNU timeout owns an isolated process group. Both parallel lanes and
			# their descendants stay in it, including on a failed lane or timeout.
			status=0
			# shellcheck disable=SC2016 # Expanded in the supervised child.
			"$timeout_tool" --kill-after=5s "${remaining}s" "$BASH" -c \
				'export FAST_FEEDBACK_SUPERVISOR_PID=$PPID; exec "$@"' _ \
				"$BASH" "$0" "$@" &
			supervisor_pid=$!
			wait "$supervisor_pid" || status=$?
			deadline_expired=false
			# Attribute the result before cleanup spends time escalating descendants.
			if { [ "$status" -eq 124 ] || [ "$status" -eq 137 ]; } && [ "$(date +%s)" -ge "$FAST_FEEDBACK_DEADLINE" ]; then
				deadline_expired=true
			fi
			if [ "$status" -ne 0 ]; then
				# The wrapper is outside the monitor's isolated group. Escalate
				# here even if a TERM handler already made the monitor exit.
				kill -TERM -- "-$supervisor_pid" 2>/dev/null || true
				sleep 1
				kill -KILL -- "-$supervisor_pid" 2>/dev/null || true
			fi
			if [ "$deadline_expired" = true ]; then
				fail 'deadline exhausted; incomplete selected checks cannot pass'
			fi
			exit "$status"
		fi
		supervised=true
	fi
fi
repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"
base=$(git rev-parse --verify "${FAST_FEEDBACK_BASE:-hikyo-upstream/main}^{commit}") || fail 'missing comparison base; fetch canonical main using CONTRIBUTING.md'
head=$(git rev-parse --verify "${FAST_FEEDBACK_HEAD:-HEAD}^{commit}") || fail 'invalid feedback revision'
comparison=$(git merge-base "$base" "$head") || fail 'base and revision have no merge base'
[ "$(git rev-parse HEAD)" = "$head" ] || fail 'checkout differs from requested revision; use a detached checkout of that revision'
if [ "$working_tree" = false ]; then
	[ -z "$(git status --porcelain --untracked-files=normal)" ] || fail 'checkout is dirty; use --working-tree for explicitly local feedback'
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-fast-feedback.XXXXXX")
lane_pids=()
cleanup() {
	local status=$?
	trap - EXIT TERM INT
	if [ "${#lane_pids[@]}" -gt 0 ]; then
		for lane in go frontend; do
			[ -f "$work/$lane.done" ] || printf 'fast-feedback: %s lane incomplete\n' "$lane" >&2
		done
	fi
	rm -rf "$work"
	exit "$status"
}
trap cleanup EXIT
trap 'exit 124' TERM
trap 'exit 130' INT
if [ "$working_tree" = true ]; then
	# Preserve both owners of a cross-package rename as deletion and addition.
	git diff --no-renames --name-only -z "$comparison" >"$work/changed"
	git ls-files --others --exclude-standard -z >"$work/untracked"
	cat "$work/untracked" >>"$work/changed"
else
	git diff --no-renames --name-only -z "$comparison" "$head" >"$work/changed"
fi

feedback_plan_build "$work/changed"

if [ "$plan_only" = true ]; then
	feedback_plan_json "$go_scope"
	exit 0
fi

started=$SECONDS
run() {
	local check_started=$SECONDS
	printf 'fast-feedback: running %s\n' "$*"
	if [ "$supervised" = true ]; then
		local remaining=$((FAST_FEEDBACK_DEADLINE - $(date +%s)))
		[ "$remaining" -gt 0 ] || fail "deadline exhausted before $*"
		# Keep descendants in the outer supervisor's group. The outer timeout
		# kills the entire group; this one also names the interrupted command.
		local status=0
		"$timeout_tool" --foreground --kill-after=5s "${remaining}s" "$@" || status=$?
		if { [ "$status" -eq 124 ] || [ "$status" -eq 137 ]; } && [ "$(date +%s)" -ge "$FAST_FEEDBACK_DEADLINE" ]; then
			fail "deadline exhausted during $*"
		fi
		if [ "$status" -ne 0 ]; then
			printf 'fast-feedback: check failed (exit %s): %s\n' "$status" "$*" >&2
			return "$status"
		fi
	else
		"$@"
	fi
	printf 'fast-feedback: completed in %ss\n' "$((SECONDS - check_started))"
}
printf 'fast-feedback: base=%s revision=%s working-tree=%s lane=%s\n' "$base" "$head" "$working_tree" "$lane"
if [ "$working_tree" = true ]; then
	run git diff --check "$comparison"
	while IFS= read -r -d '' path; do
		[ -f "$path" ] || continue
		status=0
		git diff --no-index --check -- /dev/null "$path" >"$work/whitespace" || status=$?
		[ ! -s "$work/whitespace" ] || { cat "$work/whitespace" >&2; fail 'untracked file whitespace differs'; }
		[ "$status" -le 1 ] || fail "cannot check untracked file $path"
	done <"$work/untracked"
else
	run git diff --check "$comparison" "$head"
fi

prepare_prerequisite() {
	case "$1" in
		go) need go ;;
		python) need python3 ;;
		node)
			need node
			node_version=$(node --version) || fail 'cannot determine Node version'
			[ "$node_version" = "v$(tr -d '\r\n' <.nvmrc)" ] || fail 'Node differs from .nvmrc; select the pinned contributor toolchain'
			;;
		pnpm) need pnpm ;;
		jq)
			need jq
			expected_pnpm=$(jq -er '.packageManager | select(startswith("pnpm@")) | ltrimstr("pnpm@")' package.json)
			pnpm_version=$(pnpm --version) || fail 'cannot determine pnpm version'
			[ "$pnpm_version" = "$expected_pnpm" ] || fail 'pnpm differs from packageManager; select the pinned contributor toolchain'
			;;
		shellcheck)
			need shellcheck
			shellcheck --version | grep -Fx 'version: 0.11.0' >/dev/null || fail 'ShellCheck 0.11.0 is required, matching CI'
			;;
		clientDependencies) [ -d clients/ts/node_modules ] || fail 'client dependencies missing; run pnpm --dir clients/ts install --frozen-lockfile before feedback' ;;
		webDependencies) [ -d web/node_modules ] || fail 'web dependencies missing; run pnpm --dir web install --frozen-lockfile before feedback' ;;
		# Hosted setup installs these; local Python imports fail if not prepared.
		releaseDependencies) : ;;
		*) fail "unknown feedback prerequisite: $1" ;;
	esac
}

execute_check() {
	case "$1" in
		audit-collector) run python3 -I -B -m unittest discover -s scripts/ops/audit-collector -p 'test_*.py' ;;
		release-admission) run python3 -B scripts/ci/release-admission/run_checks.py --unit ;;
		go-format)
			gofmt="$(go env GOROOT)/bin/gofmt"
			unformatted=$("$gofmt" -l "${go_files[@]}")
			[ -z "$unformatted" ] || { printf '%s\n' "$unformatted" >&2; fail 'changed Go files need gofmt'; }
			printf 'fast-feedback: Go formatting passed (%s files)\n' "${#go_files[@]}"
			;;
		# Full unchanged-consumer compatibility belongs to candidate CI.
		go-mod-verify) run go mod verify ;;
		go-compile)
			# Share a single build graph and link discarded binaries, including
			# duplicate package basenames. No test/init/TestMain executes. Omit
			# DWARF and dependency vet; full validation still runs go vet ./...
			run go test -c -vet=off -gcflags=all=-dwarf=false -ldflags=-w -o /dev/null "${go_packages[@]}"
			;;
		shellcheck) run shellcheck -x -- "${shell_files[@]}" ;;
		actionlint)
			# Run immutable tooling against source workflow paths as data.
			tools_dir=$(CDPATH='' cd -- "${FAST_FEEDBACK_TOOLS_DIR:-$repo_root/scripts/ci}" && pwd)
			workflow_files=()
			for path in "$repo_root"/.github/workflows/*.yml "$repo_root"/.github/workflows/*.yaml; do
				[ -f "$path" ] && workflow_files+=("$path")
			done
			[ "${#workflow_files[@]}" -gt 0 ] || fail 'no workflows remain to validate'
			(cd "$tools_dir/../.."; run "$tools_dir/run-go-tool.sh" actionlint "${workflow_files[@]}")
			;;
		docs)
			# An older source checkout may have a generation-only checker.
			docs_checker="${FAST_FEEDBACK_TOOLS_DIR:-$repo_root/scripts/ci}/../../docs/site/scripts/prepare-content.mjs"
			run node "$docs_checker" --check --source-root "$repo_root"
			;;
		client-typecheck) run pnpm --dir clients/ts run typecheck ;;
		client-test) run pnpm --dir clients/ts run test ;;
		web-typecheck) run pnpm --dir web run typecheck ;;
		web-lint) run pnpm --dir web run lint ;;
		web-test) run pnpm --dir web run test ;;
		*) fail "unknown feedback check: $1" ;;
	esac
}

execute_lane() {
	local check prerequisite
	feedback_plan_select "$1" "$go_scope"
	for prerequisite in ${feedback_prerequisites[@]+"${feedback_prerequisites[@]}"}; do
		prepare_prerequisite "$prerequisite"
	done
	for check in ${feedback_checks[@]+"${feedback_checks[@]}"}; do
		execute_check "$check"
	done
}

go_checks() { execute_lane go; }
frontend_checks() { execute_lane frontend; }

if [ "$lane" = go ]; then
	go_checks
elif [ "$lane" = frontend ]; then
	frontend_checks
elif [ "$supervised" = true ]; then
	# The lanes share no generated files: client commands precede web commands
	# inside their lane, while Go compilation and read-only tooling overlap it.
	(trap - EXIT TERM INT; go_checks; touch "$work/go.done") &
	lane_pids+=("$!")
	(trap - EXIT TERM INT; frontend_checks; touch "$work/frontend.done") &
	lane_pids+=("$!")
	for ((completed = 0; completed < 2; completed++)); do
		status=0
		finished=''
		wait -n -p finished "${lane_pids[@]}" || status=$?
		[ "$status" -eq 0 ] || fail "selected lane failed (exit $status); all lanes must pass"
		pending=()
		for pid in "${lane_pids[@]}"; do
			[ "$pid" = "$finished" ] || pending+=("$pid")
		done
		lane_pids=("${pending[@]}")
	done
	lane_pids=()
else
	go_checks
	frontend_checks
fi
printf 'fast-feedback: selected checks passed in %ss (lane=%s); full candidate CI remains required\n' "$((SECONDS - started))" "$lane"
