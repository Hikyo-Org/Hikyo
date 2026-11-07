#!/usr/bin/env bash
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-dev-discovery-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/gh" <<'GH'
#!/bin/sh
set -eu
if [ "$1" = api ] && [ "${2#repos/owner/repo/compare/}" != "$2" ]; then
  printf '%s\n' "$2" >>"$DEV_DISCOVERY_FIXTURE/compare.calls"
  [ ! -e "$DEV_DISCOVERY_FIXTURE/compare.fail" ] || exit 1
  cat "$DEV_DISCOVERY_FIXTURE/compare.json"
  exit 0
fi
[ "$1" = api ] && [ "$2" = --paginate ] && [ "$3" = --slurp ]
[ "$4" = 'repos/owner/repo/pulls?state=open&per_page=100' ]
cat "$DEV_DISCOVERY_FIXTURE/pulls.json"
GH
chmod +x "$work/bin/gh"
export DEV_DISCOVERY_FIXTURE="$work"
export GH_REPO=owner/repo GITHUB_EVENT_PATH="$work/event.json"
export GITHUB_EVENT_NAME=pull_request_target
head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
previous=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
reset() {
  jq -n --arg head "$head" '[ [{number:7,state:"open",base:{ref:"dev",repo:{full_name:"owner/repo"}},head:{sha:$head}}] ]' >"$work/pulls.json"
  jq -n --arg head "$head" '{repository:{full_name:"owner/repo"},number:7,action:"opened",pull_request:{number:7,base:{ref:"dev"},head:{sha:$head}}}' >"$work/event.json"
}
run() { PATH="$work/bin:$PATH" bash "$script_dir/discover-dev-prs.sh"; }
reset
run | jq -e --arg head "$head" '. == [{pr:7,head:$head}]' >/dev/null
jq --arg before "$previous" '.action="synchronize" | .before=$before' "$work/event.json" >"$work/new.json"
mv "$work/new.json" "$work/event.json"
run | jq -e --arg head "$head" --arg before "$previous" 'sort_by(.head) == ([{pr:7,head:$head},{pr:7,head:$before}] | sort_by(.head))' >/dev/null
reset
GITHUB_EVENT_NAME=workflow_run
jq -n --arg head "$head" '{repository:{full_name:"owner/repo"},workflow_run:{event:"pull_request",path:".github/workflows/ci-feedback.yml",head_sha:$head}}' >"$work/event.json"
run | jq -e --arg head "$head" '. == [{pr:7,head:$head}]' >/dev/null
GITHUB_EVENT_NAME=schedule
reset
run | jq -e --arg head "$head" '. == [{pr:7,head:$head}]' >/dev/null
# A dev push selects nothing unless it changed the dev publisher; every
# undeterminable case reassesses all PRs, and a main push always does.
after=cccccccccccccccccccccccccccccccccccccccc
gate=.github/workflows/ci-dev-gate.yml
push_event() { jq -n --arg ref "$1" --arg before "$2" --arg after "$after" '{repository:{full_name:"owner/repo"},ref:$ref,before:$before,after:$after,forced:false}' >"$work/event.json"; }
compare() { printf '%s\n' "$1" >"$work/compare.json"; }
selects() { # $1 label, $2 expected: none|all
  local out want
  rm -f "$work/compare.calls"
  out=$(run)
  if [ "$2" = none ]; then want='. == []'; else want=". == [{pr:7,head:\"$head\"}]"; fi
  jq -e "$want" <<<"$out" >/dev/null || { printf 'dev push discovery: %s expected %s, got %s\n' "$1" "$2" "$out" >&2; exit 1; }
}
GITHUB_EVENT_NAME=push
reset
push_event refs/heads/dev "$previous"
compare '{"status":"ahead","files":[{"filename":"api/x.go"},{"filename":"docs/a.md","previous_filename":"docs/b.md"}]}'
selects 'dev push without the publisher' none
grep -qx "repos/owner/repo/compare/$previous...$after" "$work/compare.calls" || { printf 'dev push discovery: compare range not read\n' >&2; exit 1; }
compare '{"status":"identical","files":[]}'
selects 'dev push with an empty diff' none
compare "{\"status\":\"ahead\",\"files\":[{\"filename\":\"$gate\"}]}"
selects 'dev push changing the publisher' all
compare "{\"status\":\"ahead\",\"files\":[{\"filename\":\"x\",\"previous_filename\":\"$gate\"}]}"
selects 'dev push renaming the publisher away' all
compare '{"status":"ahead","files":[{"filename":"a"}]}'
jq '.files = [range(0; 300) | {filename: "f\(.)"}]' "$work/compare.json" >"$work/new.json"; mv "$work/new.json" "$work/compare.json"
selects 'dev push at the compare file limit' all
jq '.files |= .[1:]' "$work/compare.json" >"$work/new.json"; mv "$work/new.json" "$work/compare.json"
selects 'dev push just under the compare file limit' none
compare '{"status":"diverged","files":[{"filename":"a"}]}'
selects 'dev push whose compare diverged' all
compare '{"status":"behind","files":[{"filename":"a"}]}'
selects 'dev push whose compare is behind' all
compare '{"status":"ahead","files":[]}'
selects 'dev push ahead with no listed files' all
compare '{"status":"ahead"}'
selects 'dev push with no file list' all
compare 'not json'
selects 'dev push with an unreadable compare' all
# A compare that would select none, so these prove the failure itself fails open.
compare '{"status":"ahead","files":[{"filename":"a"}]}'
: >"$work/compare.fail"
selects 'dev push when compare fails' all
rm "$work/compare.fail"
for mutation in '.before="0000000000000000000000000000000000000000"' 'del(.before)' '.before="bad"' '.before=null' '.after="bad"' 'del(.after)' '.forced=true' 'del(.forced)'; do
  push_event refs/heads/dev "$previous"
  jq "$mutation" "$work/event.json" >"$work/new.json"; mv "$work/new.json" "$work/event.json"
  selects "dev push with $mutation" all
done
push_event refs/heads/main "$previous"
compare '{"status":"ahead","files":[{"filename":"api/x.go"}]}'
selects 'main push' all
[ ! -e "$work/compare.calls" ] || { printf 'main push must not consult compare\n' >&2; exit 1; }
GITHUB_EVENT_NAME=schedule
reset
for mutation in '.[0][0].number=0' '.[0][0].base.ref=null' '.[0][0].base.repo.full_name="other/repo"' '.[0][0].head.sha="bad"' '.[0] += .[0]' '. + [.[0]]'; do
  reset
  jq "$mutation" "$work/pulls.json" >"$work/new.json"
  mv "$work/new.json" "$work/pulls.json"
  if run >/dev/null 2>&1; then printf 'dev discovery accepted malformed routing inventory: %s\n' "$mutation" >&2; exit 1; fi
done
reset
jq -n '[[],[]]' >"$work/pulls.json"
if run >/dev/null 2>&1; then printf 'dev discovery accepted nonterminal short page\n' >&2; exit 1; fi
reset
jq -n --arg head "$head" '[[range(1; 101) | {number:., state:"open", base:{ref:"dev",repo:{full_name:"owner/repo"}}, head:{sha:$head}}]]' >"$work/pulls.json"
run | jq -e --arg head "$head" '. == [{pr:1,head:$head}]' >/dev/null || { printf 'dev discovery mishandled a final page of exactly 100\n' >&2; exit 1; }
# Stacked PRs: GitHub judges every layer by the stack base, so a layer of a stack
# based on dev is discovered, a layer of any other stack is not, and a malformed
# stack is refused wherever it appears.
stk() { jq -n --arg ref "$1" --arg sha "$2" '{base:{ref:$ref,sha:$sha}}'; }
dev_tip=dddddddddddddddddddddddddddddddddddddddd
layer=eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
other=ffffffffffffffffffffffffffffffffffffffff
stack_pulls() { # $1 stack base ref for #8, $2 stack base ref for #9
  jq -n --arg head "$head" --arg layer "$layer" --arg other "$other" --argjson a "$(stk "$1" "$dev_tip")" --argjson b "$(stk "$2" "$dev_tip")" '[[
    {number:7,state:"open",base:{ref:"dev",repo:{full_name:"owner/repo"}},head:{sha:$head},stack:$a},
    {number:8,state:"open",base:{ref:"feat/lower",repo:{full_name:"owner/repo"}},head:{sha:$layer},stack:$a},
    {number:9,state:"open",base:{ref:"feat/other",repo:{full_name:"owner/repo"}},head:{sha:$other},stack:$b},
    {number:10,state:"open",base:{ref:"main",repo:{full_name:"owner/repo"}},head:{sha:"1111111111111111111111111111111111111111"}}
  ]]' >"$work/pulls.json"
}
GITHUB_EVENT_NAME=schedule
reset
stack_pulls dev main
run | jq -e --arg head "$head" --arg layer "$layer" 'sort_by(.pr) == [{pr:7,head:$head},{pr:8,head:$layer}]' >/dev/null || { printf 'dev discovery must include stacked-on-dev layers and exclude other stacks\n' >&2; exit 1; }
GITHUB_EVENT_NAME=workflow_run
jq -n --arg head "$layer" '{repository:{full_name:"owner/repo"},workflow_run:{event:"pull_request",path:".github/workflows/ci-feedback.yml",head_sha:$head}}' >"$work/event.json"
run | jq -e --arg layer "$layer" '. == [{pr:8,head:$layer}]' >/dev/null || { printf 'dev feedback callback must reach a stacked-on-dev layer\n' >&2; exit 1; }
jq -n --arg head "$other" '{repository:{full_name:"owner/repo"},workflow_run:{event:"pull_request",path:".github/workflows/ci-feedback.yml",head_sha:$head}}' >"$work/event.json"
run | jq -e '. == []' >/dev/null || { printf 'dev feedback callback must not reach a stack on another base\n' >&2; exit 1; }
GITHUB_EVENT_NAME=pull_request_target
jq -n --arg head "$layer" --argjson s "$(stk dev "$dev_tip")" '{repository:{full_name:"owner/repo"},number:8,action:"opened",pull_request:{number:8,base:{ref:"feat/lower"},head:{sha:$head},stack:$s}}' >"$work/event.json"
run | jq -e --arg layer "$layer" '. == [{pr:8,head:$layer}]' >/dev/null || { printf 'dev PR event must accept a stacked-on-dev layer\n' >&2; exit 1; }
jq '.pull_request.stack.base.ref="main"' "$work/event.json" >"$work/new.json"; mv "$work/new.json" "$work/event.json"
if run >/dev/null 2>&1; then printf 'dev PR event accepted a stack on another base\n' >&2; exit 1; fi
GITHUB_EVENT_NAME=schedule
# A malformed stack drops only its own PR from selection (the publisher refuses it
# if it is ever reached) and never stalls discovery for the others. A direct dev PR
# stays selected so the publisher can refuse it visibly.
malformed='.stack.base.sha="bad"|.stack.base.ref=""|.stack.base.ref=5|.stack.base=null|.stack="x"|.stack.base.sha=null|.stack={}'
IFS='|' read -r -a bad_stacks <<<"$malformed"
for mutation in "${bad_stacks[@]}"; do
  for index in 1 2 0; do
    reset
    stack_pulls dev main
    jq ".[0][$index] |= ($mutation)" "$work/pulls.json" >"$work/new.json"
    mv "$work/new.json" "$work/pulls.json"
    case $index in
      1) want="[{pr:7,head:\"$head\"}]" ;;
      2) want="[{pr:7,head:\"$head\"},{pr:8,head:\"$layer\"}]" ;;
      0) want="[{pr:7,head:\"$head\"},{pr:8,head:\"$layer\"}]" ;;
    esac
    run | jq -e "sort_by(.pr) == $want" >/dev/null || { printf 'dev discovery mishandled malformed stack %s on PR index %s\n' "$mutation" "$index" >&2; exit 1; }
  done
done
printf 'dev discovery fixtures: exact current heads, superseded-head cleanup, dev-push scoping with every fail-open case, stacked-on-dev admission, malformed stacks skipped without stalling, and malformed pagination refusals passed\n'
