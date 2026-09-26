#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
workflow="$script_dir/../../.github/workflows/ci.yml"
controller="$script_dir/../../.github/workflows/ci-control.yml"
# Out-of-band fuzz reporting moved to its own trusted base-context workflow
# (#189): ci.yml / ci-control.yml execute untrusted PR code and hold no
# issue/PR write, while fuzz-report.yml runs on workflow_run completion in the
# base context and is the sole holder of write authority.
reporter="$script_dir/../../.github/workflows/fuzz-report.yml"

require_line() {
	file=$1
	expected=$2
	if ! grep -F -- "$expected" "$file" >/dev/null; then
		printf 'trusted CI scripts fixture failed: missing %s in %s\n' "$expected" "$(basename "$file")" >&2
		exit 1
	fi
}

# The reusable validation graph runs untrusted PR code through base-controlled
# trusted scripts (fetched at BASE_SHA) and uploads the fuzz reproducers as an
# artifact the trusted reporter re-validates.
require_line "$workflow" "git show \"\$BASE_SHA:scripts/ci/classify-changed-paths.sh\" >\"\$trusted_classifier\""
require_line "$workflow" "git show \"\$BASE_SHA:scripts/ci/ci-job-registry.json\" >\"\$trusted_registry\""
require_line "$workflow" "CI_JOB_REGISTRY=\"\$trusted_registry\" \"\$trusted_classifier\" --files"
require_line "$workflow" "git show \"\$BASE_SHA:scripts/ci/check-required-jobs.sh\" >\"\$trusted_checker\""
require_line "$workflow" "CI_JOB_REGISTRY=\"\$trusted_registry\" \"\$trusted_checker\" --supports-plan-v2"
require_line "$workflow" "CI_JOB_REGISTRY=\"\$trusted_registry\" \"\$trusted_checker\" \"\$GITHUB_EVENT_NAME\" \"\$NEEDS_JSON\" \"\$PLAN_JSON\""
require_line "$workflow" "git show \"\$BASE_SHA:scripts/ci/analysis-shards-go/main.go\" >\"\$trusted_planner\""
# shellcheck disable=SC2016
require_line "$workflow" 'isolation_shard=$(go run "$trusted_planner" isolation --root .'
# Core package enumeration moved into the scheduler so app cleanup does not
# contend with other packages' PostgreSQL checkpoints. Keep workflow wiring
# pinned here, and execute its coverage/order/failure refusal proof directly.
require_line "$workflow" './scripts/ci/test-core-packages_test.sh'
require_line "$workflow" './scripts/ci/test-core-packages.sh'
"$script_dir/test-core-packages_test.sh"
# Race dispatch retains the base-controlled coverage boundary while isolating
# app cleanup from peer package migrations. Its behavior is checked directly.
require_line "$workflow" "git show \"\$BASE_SHA:scripts/ci/test-race-packages.sh\" >\"\$scheduler\""
# shellcheck disable=SC2016
require_line "$workflow" 'bash "$scheduler" "$packages"'
"$script_dir/test-race-packages_test.sh"
require_line "$workflow" 'name: Upload shard fuzz reproducers'
require_line "$workflow" 'name: Download shard fuzz reproducers'
require_line "$workflow" 'name: Upload minimized fuzz reproducers'
require_line "$workflow" "-fuzztime=100000x -timeout=2m"
# Workflow shell variables below are literal fixture text.
# shellcheck disable=SC2016
require_line "$workflow" 'echo "$shellcheck_dir" >>"$GITHUB_PATH"'

download_block=$(sed -n \
	'/name: Download shard fuzz reproducers/,/name: Find merged fuzz reproducers/p' \
	"$workflow")
if printf '%s\n' "$download_block" | grep -F 'continue-on-error: true' >/dev/null; then
	printf 'trusted CI scripts fixture failed: shard artifact download errors are suppressed\n' >&2
	exit 1
fi

if grep -Eq '^[[:space:]]+pull_request:' "$workflow"; then
	printf 'trusted CI scripts fixture failed: direct pull-request trigger is enabled\n' >&2
	exit 1
fi
if ! grep -F 'pull_request_target:' "$controller" >/dev/null ||
	! grep -F 'uses: ./.github/workflows/ci.yml' "$controller" >/dev/null; then
	printf 'trusted CI scripts fixture failed: base-controlled entrypoint is missing\n' >&2
	exit 1
fi

# Fork PRs (#813): validation runs untrusted under pull_request in ci-fork.yml,
# never under pull_request_target, and the base-controlled gate decides.
fork_workflow="$script_dir/../../.github/workflows/ci-fork.yml"
if ! grep -Eq '^  pull_request:' "$fork_workflow" ||
	grep -Eq '^[[:space:]]+pull_request_target:' "$fork_workflow" ||
	grep -F 'allow-unsafe-pr-checkout' "$workflow" "$controller" "$fork_workflow" >/dev/null; then
	printf 'trusted CI scripts fixture failed: fork validation left pull_request or checks out fork code in a trusted context\n' >&2
	exit 1
fi
# A skipped job named ci-required would satisfy the required check.
if grep -Eq '^  ci-required:' "$fork_workflow"; then
	printf 'trusted CI scripts fixture failed: fork workflow defines the required ci-required context\n' >&2
	exit 1
fi
require_line "$fork_workflow" "if: github.event.pull_request.head.repo.full_name != github.repository"
require_line "$controller" "if: github.event.pull_request.head.repo.full_name == github.repository"
controller_gate_steps=$(sed -n '/^  ci-required:/,$p' "$controller")
printf '%s\n' "$controller_gate_steps" |
	grep -F 'run: ./scripts/ci/check-fork-validation.sh' >/dev/null || {
	printf 'trusted CI scripts fixture failed: ci-required does not gate fork validation\n' >&2
	exit 1
}
# Only vouched fork authors pass: the trusted gate decides, and the fork run
# does not start validation without it. check-user fails unless allow-fail.
vouch_action='uses: mitchellh/vouch/action/check-user@'
printf '%s\n' "$controller_gate_steps" | grep -F "$vouch_action" >/dev/null || {
	printf 'trusted CI scripts fixture failed: ci-required does not require a vouched fork author\n' >&2
	exit 1
}
require_line "$fork_workflow" "$vouch_action"
require_line "$fork_workflow" 'needs: vouch'
if grep -F 'allow-fail' "$controller" "$fork_workflow" >/dev/null; then
	printf 'trusted CI scripts fixture failed: vouch check may pass an unvouched author\n' >&2
	exit 1
fi
"$script_dir/check-fork-validation_test.sh"

# Fork runs get no secrets and no OIDC identity (#813): nothing in the fork path
# may reference a secret, inherit secrets, or request an ID token. GitHub already
# withholds secrets from fork pull_request runs; this keeps the YAML from asking.
if grep -Eq 'secrets[.:]|id-token' "$fork_workflow" ||
	grep -Eq 'secrets[.:]|id-token: write' "$workflow"; then
	printf 'trusted CI scripts fixture failed: the fork validation path references secrets or an ID token\n' >&2
	exit 1
fi
if grep -Eq '(issues|pull-requests): write' "$fork_workflow"; then
	printf 'trusted CI scripts fixture failed: fork validation received issue/PR writes\n' >&2
	exit 1
fi

# Unvouched fork PRs are closed on open and reopen, from base YAML with no
# checkout, so a fork-added workflow's same-name check never gates a mergeable PR.
vouch_closer="$script_dir/../../.github/workflows/vouch-check-pr.yml"
require_line "$vouch_closer" 'uses: mitchellh/vouch/action/check-pr@'
require_line "$vouch_closer" 'auto-close: true'
require_line "$vouch_closer" 'types: [opened, reopened]'
require_line "$vouch_closer" "if: github.event.pull_request.head.repo.full_name != github.repository"
if ! grep -Eq '^  pull_request_target:' "$vouch_closer" ||
	grep -Eq 'actions/checkout|secrets[.:]|require-vouch' "$vouch_closer"; then
	printf 'trusted CI scripts fixture failed: vouch-check-pr may run PR code, reach secrets, or admit unvouched authors\n' >&2
	exit 1
fi

# dco-report holds PR write, so it must never run PR code: it checks out the
# base SHA only and runs base scripts over PR commits fetched as data.
dco_reporter="$script_dir/../../.github/workflows/dco-report.yml"
# shellcheck disable=SC2016
require_line "$dco_reporter" 'ref: ${{ github.event.pull_request.base.sha }}'
require_line "$dco_reporter" 'run: ./scripts/ci/report-dco.sh'
if [ "$(grep -c 'ref:' "$dco_reporter")" -ne 1 ] ||
	grep -Eq 'allow-unsafe-pr-checkout|secrets[.:]' "$dco_reporter"; then
	printf 'trusted CI scripts fixture failed: dco-report may check out PR code or reach secrets\n' >&2
	exit 1
fi
"$script_dir/report-dco_test.sh"

# Superseded PR runs must release workflow concurrency immediately. Aggregate
# gates still run after ordinary failures, but cancellation must skip them.
workflow_gate=$(sed -n '/^  ci-required:/,$p' "$workflow")
controller_gate=$(sed -n '/^  ci-required:/,$p' "$controller")
for gate in "$workflow_gate" "$controller_gate"; do
	if ! printf '%s\n' "$gate" |
		grep -Fx '    if: always() && !cancelled()' >/dev/null; then
		printf 'trusted CI scripts fixture failed: aggregate gate survives cancellation\n' >&2
		exit 1
	fi
done
# The aggregate gate requires every shard matrix directly (no fan-in jobs), and
# merges shard fuzz reproducers only when a fuzz shard failed.
for shard_job in test_core isolation_shard race_shard fuzz_shard; do
	printf '%s\n' "$workflow_gate" | grep -Fx "      - $shard_job" >/dev/null || {
		printf 'trusted CI scripts fixture failed: ci-required does not gate %s\n' "$shard_job" >&2
		exit 1
	}
done
printf '%s\n' "$workflow_gate" |
	grep -Fx "        if: \${{ needs.fuzz_shard.result == 'failure' }}" >/dev/null || {
	printf 'trusted CI scripts fixture failed: ci-required does not merge failed fuzz shard reproducers\n' >&2
	exit 1
}

# The untrusted validation graph (ci.yml, ci-control.yml) holds NO issue/PR write
# anywhere: executing attacker-influenced PR code must never reach a write token.
if grep -Eq '(issues|pull-requests): write' "$workflow" ||
	grep -Eq '(issues|pull-requests): write' "$controller"; then
	printf 'trusted CI scripts fixture failed: untrusted validation graph received issue/PR writes\n' >&2
	exit 1
fi

# The trusted reporter runs out of band on workflow_run completion, never as a
# direct pull_request(_target) job, and binds PR identity ONLY to GitHub-owned
# workflow_run metadata — never to an artifact an untrusted PR job could forge.
if ! grep -Eq '^  workflow_run:' "$reporter"; then
	printf 'trusted CI scripts fixture failed: reporter is not a workflow_run job\n' >&2
	exit 1
fi
if grep -Eq '^[[:space:]]+pull_request(_target)?:' "$reporter"; then
	printf 'trusted CI scripts fixture failed: reporter carries a direct pull-request trigger\n' >&2
	exit 1
fi
require_line "$reporter" 'github.event.workflow_run.pull_requests'

# The read-only classify job replays untrusted reproducers against the trusted
# base and holds no write; only the report jobs hold write. report-pr routes a
# PR finding (issues + PR write), report-main opens a repository issue for a main
# push (issues write). Both post through the base-controlled trusted script.
classify_block=$(sed -n '/^  classify:/,/^  report-pr:/p' "$reporter")
report_pr_block=$(sed -n '/^  report-pr:/,/^  report-main:/p' "$reporter")
report_main_block=$(sed -n '/^  report-main:/,$p' "$reporter")
if printf '%s\n' "$classify_block" | grep -Eq '(issues|pull-requests): write'; then
	printf 'trusted CI scripts fixture failed: read-only classify job received issue/PR writes\n' >&2
	exit 1
fi
if ! printf '%s\n' "$report_pr_block" | grep -F 'issues: write' >/dev/null ||
	! printf '%s\n' "$report_pr_block" | grep -F 'pull-requests: write' >/dev/null ||
	! printf '%s\n' "$report_pr_block" | grep -F './scripts/ci/report-fuzz-finding.sh "PR #' >/dev/null; then
	printf 'trusted CI scripts fixture failed: trusted PR fuzz reporter is missing\n' >&2
	exit 1
fi
if ! printf '%s\n' "$report_main_block" | grep -F 'issues: write' >/dev/null ||
	! printf '%s\n' "$report_main_block" | grep -F './scripts/ci/report-fuzz-finding.sh main' >/dev/null; then
	printf 'trusted CI scripts fixture failed: trusted main fuzz reporter is missing\n' >&2
	exit 1
fi

printf 'trusted CI scripts fixture: untrusted validation graph holds no writes; the out-of-band reporter owns PR binding and issue writes\n'
