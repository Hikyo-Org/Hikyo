#!/usr/bin/env bash
# Routing only. The trusted publisher independently verifies every current PR.
set -euo pipefail
[[ $# == 0 ]]
: "${GH_REPO:?}" "${GITHUB_EVENT_NAME:?}" "${GITHUB_EVENT_PATH:?}"
[[ "$GH_REPO" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]
work=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/hikyo-dev-discovery.XXXXXX")
trap 'rm -rf "$work"' EXIT
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
jq -cse --arg name "$GITHUB_EVENT_NAME" --arg repo "$GH_REPO" --arg scope "$scope" \
  --slurpfile event "$GITHUB_EVENT_PATH" '
  def sha: type == "string" and test("^[0-9a-f]{40}$");
  def number: type == "number" and . > 0 and floor == .;
  # GitHub judges every PR of a stack by the stack base, so a layer of a stack
  # based on dev is a dev PR. A present but malformed stack is refused, not skipped.
  def stack_ok: .stack == null or
    (.stack | type == "object" and (.base | type == "object") and
      (.base.ref | type == "string" and . != "") and (.base.sha | sha));
  def dev_pr: stack_ok and (.base.ref == "dev" or .stack.base.ref == "dev");
  if length != 1 or (.[0] | type) != "array" then error("invalid dev PR pages") else .[0] end |
  if length == 0 or any(.[]; type != "array" or length > 100) or
     (length > 1 and any(.[0:-1][]; length != 100))
  then error("incomplete dev PR pagination") else . end |
  flatten as $all |
  if ($all | length) > 2000 or
     any($all[]; type != "object" or (.number | number | not) or .state != "open" or
       (.base.ref | type != "string") or .base.repo.full_name != $repo or (.head.sha | sha | not) or (stack_ok | not)) or
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
        $e.workflow_run.path == ".github/workflows/ci-feedback.yml" and ($e.workflow_run.head_sha | sha)
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
  if length > 256 then error("dev discovery exceeds matrix capacity") else . end
' "$work/pulls.json"
