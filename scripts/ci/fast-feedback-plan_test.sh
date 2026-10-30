#!/usr/bin/env bash
# Exercise the planning interface without executing checks or bootstrapping tools.
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-feedback-plan.XXXXXX")
trap 'rm -rf "$work"' EXIT
fail() { printf 'feedback plan fixture: %s\n' "$*" >&2; exit 1; }
# shellcheck source=scripts/ci/feedback-plan.sh
source "$script_dir/feedback-plan.sh"
mkdir -p "$work/repo with spaces/app" "$work/repo with spaces/scripts/tool" "$work/bin"
cd "$work/repo with spaces"
printf 'package app\n' >app/owner.go
printf 'package tool\n' >scripts/tool/owner.go
printf 'package scripts\n' >scripts/owner.go
printf 'package tool\n' >"scripts/tool/line
break.go"
printf '#!/bin/sh\n' >'-space name.sh'
printf '#!/bin/sh\n' >"line
break.sh"
real_jq=$(command -v jq)
# A planning-only invocation must never ask runtime/dependency tools for data.
for tool in go node pnpm jq; do
	printf '#!/bin/sh\nprintf "planning invoked runtime tool\\n" >&2\nexit 91\n' >"$work/bin/$tool"
	chmod +x "$work/bin/$tool"
done
plan() {
	local scope=$1
	shift
	: >"$work/changed"
	if [ "$#" -gt 0 ]; then printf '%s\0' "$@" >"$work/changed"; fi
	PATH="$work/bin:$PATH" feedback_plan_build "$work/changed"
	PATH="$work/bin:$PATH" feedback_plan_json "$scope" >"$work/plan"
	# Every lane's prerequisites must be exactly the union required by its
	# ordered checks, with no omitted dependency or unrelated setup request.
	"$real_jq" -e '
		.lanes | keys == ["frontend", "go", "tooling"]' "$work/plan" >/dev/null
	"$real_jq" -e '
		all(.lanes[];
			([.checks[].prerequisites[]] | unique) ==
			([.prerequisites | to_entries[] | select(.value).key] | sort)) and
		(.lanes.go | .lane == "go" and .goScope == "application") and
		(.lanes.tooling | .lane == "go" and .goScope == "tooling") and
		(.lanes.frontend | .lane == "frontend" and .goScope == "all")' "$work/plan" >/dev/null
	# Independent contract: a producer cannot omit a dependency from both
	# check details and the lane union and still satisfy the consistency test.
	# shellcheck disable=SC2016 # jq resolves the required argument, not Bash.
	"$real_jq" -e --argjson required '{
		"audit-collector": ["python"],
		"release-admission": ["python", "releaseDependencies"],
		"go-format": ["go"], "go-mod-verify": ["go"], "go-compile": ["go"],
		"shellcheck": ["shellcheck"], "actionlint": ["go", "shellcheck"],
		"docs": ["node"],
		"client-typecheck": ["clientDependencies", "node", "pnpm", "jq"],
		"client-test": ["clientDependencies", "node", "pnpm", "jq"],
		"web-typecheck": ["clientDependencies", "webDependencies", "node", "pnpm", "jq"],
		"web-lint": ["clientDependencies", "webDependencies", "node", "pnpm", "jq"],
		"web-test": ["clientDependencies", "webDependencies", "node", "pnpm", "jq"]
	}' 'all(.lanes[].checks[]; .prerequisites == $required[.id])' "$work/plan" >/dev/null
}
expect() { "$real_jq" -e "$1" "$work/plan" >/dev/null || { cat "$work/plan" >&2; fail "$1"; }; }
plan all
expect '([del(.lanes)[]] | all(. == false)) and all(.lanes[]; .checks == [])'
plan all app/owner.go
expect '.go and (.lanes.go.checks | map(.id)) == ["go-format", "go-compile"] and .lanes.tooling.checks == [] and .lanes.go.prerequisites.go'
plan application scripts/tool/owner.go
expect '.go == false and .lanes.go.checks == [] and (.lanes.tooling.checks | map(.id)) == ["go-format", "go-compile"]'
plan tooling app/owner.go
expect '.go == false and (.lanes.go.checks | map(.id)) == ["go-format", "go-compile"] and .lanes.tooling.checks == []'
plan application scripts/owner.go
expect '.go == false and .lanes.go.checks == [] and (.lanes.tooling.checks | map(.id)) == ["go-format", "go-compile"]'
feedback_plan_select go tooling
[ "${#go_packages[@]}" -eq 1 ] && [ "${go_packages[0]}" = ./scripts ]
plan tooling go.mod
expect '.go and .lanes.tooling.checks == [] and (.lanes.go.checks | map(.id)) == ["go-mod-verify"]'
plan all .github/workflows/check.yml
expect '.workflows and (.lanes.tooling.checks | map(.id)) == ["actionlint"] and .lanes.tooling.prerequisites.go and .lanes.tooling.prerequisites.shellcheck and (.lanes.go.prerequisites.go | not)'
plan all CONTRIBUTING.md
expect '.docs and (.lanes.tooling.checks | map(.id)) == ["docs"] and .lanes.tooling.prerequisites.node and (.lanes.tooling.prerequisites.pnpm | not)'
plan all web/file.ts
expect '.web and (.client | not) and (.lanes.frontend.checks | map(.id)) == ["web-typecheck", "web-lint", "web-test"] and .lanes.frontend.prerequisites.clientDependencies and .lanes.frontend.prerequisites.webDependencies'
plan all api/openapi.yaml
expect '.client and .web and (.lanes.frontend.checks | map(.id)) == ["client-typecheck", "client-test", "web-typecheck", "web-lint", "web-test"]'
plan all docs/operations/audit-collector.md scripts/ci/aws-acceptance/test_runner.py
expect '.auditCollector and .releaseAdmission and (.lanes.tooling.checks | map(.id)) == ["audit-collector", "release-admission"] and .lanes.tooling.prerequisites.python and .lanes.tooling.prerequisites.releaseDependencies'
plan all app/owner.go scripts/tool/owner.go "scripts/tool/line
break.go" go.sum '-space name.sh' "line
break.sh" .github/workflows/aws-acceptance.yml docs/site/a.md clients/ts/a.ts docs/operations/audit-collector.md
expect 'all(del(.lanes)[]; . == true) and (.lanes.go.checks | map(.id)) == ["go-format", "go-mod-verify", "go-compile"] and (.lanes.tooling.checks | map(.id)) == ["audit-collector", "release-admission", "go-format", "go-compile", "shellcheck", "actionlint", "docs"]'
feedback_plan_select go tooling
[ "${#go_files[@]}" -eq 2 ] && [ "${#go_packages[@]}" -eq 1 ] && [ "${#shell_files[@]}" -eq 2 ]
[ "${go_files[1]}" = "./scripts/tool/line
break.go" ]
# A deleted file still selects its remaining owner; a fully removed package
# selects neither formatting nor compilation.
plan all app/deleted.go removed/final.go
expect '.go and (.lanes.go.checks | map(.id)) == ["go-compile"]'
feedback_plan_select go application
[ "${#go_files[@]}" -eq 0 ] && [ "${#go_packages[@]}" -eq 1 ] && [ "${go_packages[0]}" = ./app ]
# Unknown declarative identifiers must never become silent successful no-ops.
if (feedback_check_prerequisites unknown) >"$work/output" 2>&1; then fail 'accepted unknown check'; fi
if (feedback_plan_hosted_lane unknown) >"$work/output" 2>&1; then fail 'accepted unknown hosted lane'; fi
printf 'fast-feedback plan fixtures: domain compatibility, ordered checks, prerequisites, scopes and path conservation passed\n'
