#!/usr/bin/env bash
# The one selector of the Actions cache's dead entries. ci-cache-prune.yml carries this text verbatim as its
# step (a test pins the two equal): that workflow checks nothing out, so it cannot run this file. The seed
# writers' headroom jobs and ci-main-headroom.yml run it from their trusted checkout, before a save.
#   without HEADROOM_PREFIXES: delete what is superseded or idle (the hourly prune).
#   with it: also free room for the save that follows, unless every key of HEADROOM_SKIP_KEY (space separated)
#   exists on main (the save will be a hit). A key may hold one `*` for a part the caller cannot compute (the pnpm
#   store's Node ABI, which needs Node): it then matches any entry with that start and that end. The incoming size is the newest main entry of each lane that starts with
#   one of the space-separated prefixes. A prefix names the lane without the runner image or the ABI (the
#   lane key drops the image, so an image bump must not make the lane look absent). Deletion goes on until
#   the total plus the incoming save fits in 10 GB less a 0.25 GB margin, in this order: older feedback seeds,
#   pull-request entries, entries idle for three days, then the least recently used entries that are not seeds.
#   Never the newest main or dev entry of a seed lane (race, lint driver, pinned tools, feedback) and never
#   an unfamilied feedback seed. At most 100 deletes a run.
set -euo pipefail
now=$(date -u +%s)
prefixes=${HEADROOM_PREFIXES:-}
skip=${HEADROOM_SKIP_KEY:-}
gh api --paginate "repos/${GH_REPO}/actions/caches?per_page=100" |
  jq -s -r --argjson now "$now" --arg prefixes "$prefixes" --arg skip "$skip" --argjson budget 9750000000 '
    def stem: sub("-[0-9a-f]{64}(-.*)?$"; "") | sub("-[0-9a-f]{40}(-[0-9]+-[0-9]+)?$"; "");
    def lane: stem | gsub("-ubuntu[0-9]+"; "") | gsub("-[0-9]+(\\.[0-9]+)+"; "") | gsub("-[0-9a-f]{16}(?=-)"; "");
    def regenerable: test("^(go-mod-v2|go-test-v2|go-race-v2|go-lint(driver)?-v2|go-generated-v2|go-k8s-e2e-v2|go-fuzz-v2|go-feedback-v1|pinned-tool-v1|pnpm-store-v2|codeql-overlay-base-database|codeql-dependencies|codeql-trap)-");
    def matches($k): ($k | split("*")) as $p | if ($p | length) == 1 then . == $k else (startswith($p[0]) and endswith($p[-1]) and (length >= (($p[0] | length) + ($p[-1] | length)))) end;
    def epoch: sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601;
    def feedbackfam: test("^go-feedback-v2-.*-(main|dev)-(application|cli|tooling)-");
    def untouchable: test("^go-feedback-v2-") and (feedbackfam | not);
    def seed: test("^(go-race-v2|go-lint(driver)-v2|pinned-tool-v1)-") or feedbackfam;
    def oldest: sort_by([(.last_accessed_at | epoch), .id]);
    def dedupe: reduce .[] as $e ([]; if any(.[]; .id == $e.id) then . else . + [$e] end);
    def take($need): reduce .[] as $e ({sum: 0, out: []}; if .sum >= $need then . else {sum: (.sum + $e.size_in_bytes), out: (.out + [$e])} end) | .out;
    [.[].actions_caches[]] as $all
    | ([$all[] | select((.ref == "refs/heads/main" or .ref == "refs/heads/dev") and (.key | regenerable))]
        | group_by([.ref, (.key | lane)])
        | map(sort_by([.created_at, .id]) | reverse | .[1:][])) as $superseded
    | [$all[] | select((.ref | test("^refs/pull/[0-9]+/(merge|head)$")) and ((.last_accessed_at | epoch) < ($now - 86400)))] as $idle
    | ($superseded + $idle) as $regular
    | ([$prefixes | split(" ")[] | select(. != "")] as $wanted
        | [$all[] | select(.ref == "refs/heads/main" and (.key as $k | any($wanted[]; . as $p | $k | startswith($p))))]
        | group_by(.key | lane) | map(sort_by([.created_at, .id]) | last | .size_in_bytes) | add // 0) as $planned
    | [$skip | split(" ")[] | select(. != "")] as $keys
    | (if ($keys | length) > 0 and all($keys[]; . as $k | any($all[]; .ref == "refs/heads/main" and (.key | matches($k)))) then 0 else $planned end) as $incoming
    | (if $incoming == 0 then [] else
        ((($all | map(.size_in_bytes) | add // 0) - ($regular | map(.size_in_bytes) | add // 0)) + $incoming - $budget) as $over
        | if $over <= 0 then [] else
            ($regular | map(.id)) as $gone
            | [$all[] | select((.id | IN($gone[]) | not) and (.key | untouchable | not))] as $left
            | ([$left[] | select((.ref == "refs/heads/main" or .ref == "refs/heads/dev") and (.key | seed))]
                | group_by([.ref, (.key | lane)]) | map(sort_by([.created_at, .id]) | last.id)) as $newest
            | [$left[] | select(.id | IN($newest[]) | not)] as $open
            | ([$open[] | select((.ref == "refs/heads/main" or .ref == "refs/heads/dev") and (.key | feedbackfam))] | oldest) as $olderFeedback
            | ([$open[] | select(.ref | test("^refs/pull/[0-9]+/(merge|head)$"))] | oldest) as $pullRequests
            | ([$open[] | select((.last_accessed_at | epoch) < ($now - 259200))] | oldest) as $idleDays
            | ([$open[] | select(.key | seed | not)] | oldest) as $leastRecent
            | ($olderFeedback + $pullRequests + $idleDays + $leastRecent | dedupe | take($over))
          end
      end) as $headroom
    | ($regular + $headroom)[] | [.id, .ref, .key] | @tsv' >"${RUNNER_TEMP}/victims.tsv"
head -n 100 "${RUNNER_TEMP}/victims.tsv" | while IFS="$(printf '\t')" read -r id ref key; do
  if ! gh api -X DELETE "repos/${GH_REPO}/actions/caches/${id}" 2>"${RUNNER_TEMP}/delete.err"; then
    grep -q 'HTTP 404' "${RUNNER_TEMP}/delete.err" || { cat "${RUNNER_TEMP}/delete.err" >&2; exit 1; }
  fi
  printf 'deleted %s (%s)\n' "$key" "$ref"
done
