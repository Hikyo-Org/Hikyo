#!/usr/bin/env bash
# Routing only. The trusted publisher independently verifies every current PR.
set -euo pipefail
[[ $# == 0 ]]
: "${GH_REPO:?}" "${GITHUB_EVENT_NAME:?}" "${GITHUB_EVENT_PATH:?}"
[[ "$GH_REPO" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]
work=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/hikyo-dev-discovery.XXXXXX")
trap 'rm -rf "$work"' EXIT
# The reserve (250 requests, see docs/development/ci.md and groupReserve in
# scripts/ci/pr-gate/reserve.go) belongs to merge-queue group jobs. The sweep spends
# nothing it could do without, so it selects no PR unless a bucket can afford one
# publisher (its 42 reads, nonGroupCost) above the reserve, the threshold the publishers
# defer on; events, dispatch and pushes still reach the publishers, which defer on their
# own. The read is free, and a failed one does not stop the sweep.
reserve=250
cost=42
if [[ $GITHUB_EVENT_NAME == schedule ]] &&
  low=$(gh api rate_limit --jq ".resources | [.core, .graphql] | map(select(. != null and .remaining - $cost < $reserve)) | length" 2>/dev/null) &&
  [[ $low =~ ^[0-9]+$ && $low != 0 ]]; then
  printf 'sweep deferred: the rate limit leaves fewer than the %s requests kept for merge-group jobs after one publisher\n' "$reserve" >&2
  printf '[]\n'
  exit 0
fi
gh api --paginate --slurp "repos/$GH_REPO/pulls?state=open&per_page=100" >"$work/pulls.json"
# A dev push reassesses nothing unless it changed the dev publisher, because the
# publisher contract compares the base's copy against main's and a backmerge that
# repairs it must release PRs refused earlier. scope=none only on proof of that;
# anything undeterminable (no compare, empty or truncated list, unusable shas,
# force push) fails open to reassessing every PR.
scope=all
if [[ $GITHUB_EVENT_NAME == push ]] && jq -e '.ref == "refs/heads/dev"' "$GITHUB_EVENT_PATH" >/dev/null; then
  if range=$(jq -er '
       def sha: type == "string" and test("^[0-9a-f]{40}$") and test("[1-9a-f]");
       if (.before | sha) and (.after | sha) and .forced == false then "\(.before)...\(.after)" else empty end
     ' "$GITHUB_EVENT_PATH") &&
    gh api "repos/$GH_REPO/compare/$range" >"$work/compare.json" 2>/dev/null &&
    jq -e --arg path .github/workflows/ci-dev-gate.yml '
      (.status == "ahead" or .status == "identical") and (.files | type == "array") and
      (.files | length) < 300 and (.status == "identical" or (.files | length) > 0) and
      all(.files[]; .filename != $path and .previous_filename != $path)
    ' "$work/compare.json" >/dev/null 2>&1; then
    scope=none
  else
    printf 'dev push: reassessing every open PR (publisher changed or range undeterminable)\n' >&2
  fi
fi
# The scheduled sweep re-proves only DEV_SWEEP_CAP open PRs per run. The window rotates
# on a cursor that advances once per executed schedule run: the number of gate-dev
# schedule runs (one REST read of the run list's total_count), so real gaps of 6 to 12
# hours between runs cannot repeat a window the way a clock cursor does. PRs are sorted
# by number and the window starts at count x cap mod open. It reads no check record and
# writes no state, so nothing a run writes (or refreshes without changing state) can move
# it, and every open PR is reached within ceil(open/cap) consecutive runs. A PR opened or
# closed in between shifts the windows by at most one cycle; events and the retry path
# settle checks regardless. A run that fails or is deferred still advances the count, so the
# window it skipped is reached ceil(open/cap) runs later: the bound is in proving runs.
# GitHub reports at most 1,000 results for a filtered list, and retention can delete old
# runs, so a count of all schedule runs could stop moving. The count is therefore taken
# within the current 30-day epoch only (created >= the epoch start, at most 720 runs even
# at one an hour) and the epoch number is added as a stable offset. Windows are consecutive
# within an epoch; the sequence restarts at an arbitrary offset at each epoch boundary,
# which shifts the windows by at most one cycle once a month.
# shortcut: one extra cycle of delay once per 30-day epoch; upgrade to a persisted cursor
# (a repository variable) only if that delay ever matters, which needs a write this job
# does not have. If the count cannot be read, the hour of the clock stands in for it.
cap=${DEV_SWEEP_CAP:-5}
cursor=$(($(date +%s) / 3600))
if [[ $GITHUB_EVENT_NAME == schedule ]]; then
  # One idempotent REST read; a failure falls back to the clock hour, and no response is printed.
  epoch_seconds=2592000
  epoch=$(($(date +%s) / epoch_seconds))
  since=$(jq -nr --argjson at "$((epoch * epoch_seconds))" '$at | todate')
  if count=$(gh api "repos/$GH_REPO/actions/workflows/ci-dev-gate.yml/runs?event=schedule&per_page=1&created=%3E%3D$since" --jq .total_count 2>/dev/null) && [[ $count =~ ^[0-9]+$ && $count -lt 1000 ]]; then
    cursor=$((epoch * 1000 + count))
  fi
fi
jq -cse --arg name "$GITHUB_EVENT_NAME" --arg repo "$GH_REPO" --arg scope "$scope" \
  --argjson cap "$cap" --argjson cursor "$cursor" \
  --slurpfile event "$GITHUB_EVENT_PATH" '
  def sha: type == "string" and test("^[0-9a-f]{40}$");
  def number: type == "number" and . > 0 and floor == .;
  # GitHub judges every PR of a stack by the stack base, so a layer of a stack
  # based on dev is a dev PR. A PR with a malformed stack is not selected through it,
  # so one bad stack cannot stall discovery; the publisher refuses it if it is reached.
  def stack_ok: .stack == null or
    (.stack | type == "object" and (.base | type == "object") and
      (.base.ref | type == "string" and . != "") and (.base.sha | sha));
  def dev_pr: .base.ref == "dev" or (stack_ok and .stack.base.ref == "dev");
  if length != 1 or (.[0] | type) != "array" then error("invalid dev PR pages") else .[0] end |
  if length == 0 or any(.[]; type != "array" or length > 100) or
     (length > 1 and any(.[0:-1][]; length != 100))
  then error("incomplete dev PR pagination") else . end |
  flatten as $all |
  if ($all | length) > 2000 or
     any($all[]; type != "object" or (.number | number | not) or .state != "open" or
       (.base.ref | type != "string") or .base.repo.full_name != $repo or (.head.sha | sha | not)) or
     ([$all[].number] | unique | length) != ($all | length) or
     ($event | length) != 1 or $event[0].repository.full_name != $repo
  then error("invalid authenticated dev discovery identity") else . end |
  [$all[] | select(dev_pr)] as $pulls |
  $event[0] as $e |
  (if $name == "pull_request_target" then
     if ($e.number | number) and $e.pull_request.number == $e.number and
        ($e.pull_request | dev_pr) and ($e.pull_request.head.sha | sha)
     then [{pr:$e.number,head:$e.pull_request.head.sha}] +
       (if $e.action == "synchronize" and ($e.before | sha) and $e.before != $e.pull_request.head.sha
        then [{pr:$e.number,head:$e.before}] else [] end)
     else error("invalid dev PR event") end
   elif $name == "workflow_run" then
     if $e.workflow_run.event == "pull_request" and
        ($e.workflow_run.path == ".github/workflows/ci-feedback.yml" or $e.workflow_run.path == ".github/workflows/ci-feedback-retarget.yml") and
        ($e.workflow_run.head_sha | sha)
     then [$pulls[] | select(.head.sha == $e.workflow_run.head_sha) | {pr:.number,head:.head.sha}]
     else error("invalid feedback callback") end
   elif $name == "workflow_dispatch" then
     if ($e.inputs.pr_number // "") == "" then [$pulls[] | {pr:.number,head:.head.sha}]
     elif ($e.inputs.pr_number | type == "string" and test("^[1-9][0-9]*$"))
     then [$pulls[] | select(.number == ($e.inputs.pr_number | tonumber)) | {pr:.number,head:.head.sha}]
     else error("invalid dev PR dispatch") end
   elif $name == "push" and $e.ref == "refs/heads/dev" and $scope == "none" then []
   elif $name == "schedule" or ($name == "push" and ($e.ref == "refs/heads/main" or $e.ref == "refs/heads/dev"))
   then [$pulls[] | {pr:.number,head:.head.sha}]
   else error("unsupported dev discovery event") end) |
  group_by(.head) | map(min_by(.pr)) |
  if $name != "schedule" then .
  else
    sort_by(.pr) | length as $n | (if $n == 0 then 0 else ($cursor * $cap) % $n end) as $s | (.[$s:] + .[:$s]) | .[0:$cap]
  end |
  if length > 256 then error("dev discovery exceeds matrix capacity") else . end
' "$work/pulls.json"
