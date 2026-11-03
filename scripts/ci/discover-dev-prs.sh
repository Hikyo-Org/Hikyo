#!/usr/bin/env bash
# Routing only. The trusted publisher independently verifies every current PR.
set -euo pipefail
[[ $# == 0 ]]
: "${GH_REPO:?}" "${GITHUB_EVENT_NAME:?}" "${GITHUB_EVENT_PATH:?}"
[[ "$GH_REPO" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]
work=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/hikyo-dev-discovery.XXXXXX")
trap 'rm -rf "$work"' EXIT
gh api --paginate --slurp "repos/$GH_REPO/pulls?state=open&base=dev&per_page=100" >"$work/pulls.json"
jq -cse --arg name "$GITHUB_EVENT_NAME" --arg repo "$GH_REPO" \
  --slurpfile event "$GITHUB_EVENT_PATH" '
  def sha: type == "string" and test("^[0-9a-f]{40}$");
  def number: type == "number" and . > 0 and floor == .;
  if length != 1 or (.[0] | type) != "array" then error("invalid dev PR pages") else .[0] end |
  if length == 0 or any(.[]; type != "array" or length > 100) or
     (length > 1 and any(.[0:-1][]; length != 100))
  then error("incomplete dev PR pagination") else . end |
  flatten as $pulls |
  if ($pulls | length) > 2000 or
     any($pulls[]; type != "object" or (.number | number | not) or .state != "open" or
       .base.ref != "dev" or .base.repo.full_name != $repo or (.head.sha | sha | not)) or
     ([$pulls[].number] | unique | length) != ($pulls | length) or
     ($event | length) != 1 or $event[0].repository.full_name != $repo
  then error("invalid authenticated dev discovery identity") else . end |
  $event[0] as $e |
  (if $name == "pull_request_target" then
     if ($e.number | number) and $e.pull_request.number == $e.number and
        $e.pull_request.base.ref == "dev" and ($e.pull_request.head.sha | sha)
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
   elif $name == "schedule" or ($name == "push" and ($e.ref == "refs/heads/main" or $e.ref == "refs/heads/dev"))
   then [$pulls[] | {pr:.number,head:.head.sha}]
   else error("unsupported dev discovery event") end) |
  group_by(.head) | map(min_by(.pr)) |
  if length > 256 then error("dev discovery exceeds matrix capacity") else . end
' "$work/pulls.json"
