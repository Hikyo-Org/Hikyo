#!/usr/bin/env bash
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-dev-discovery-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/gh" <<'GH'
#!/bin/sh
set -eu
[ "$1" = api ] && [ "$2" = --paginate ] && [ "$3" = --slurp ]
[ "$4" = 'repos/owner/repo/pulls?state=open&base=dev&per_page=100' ]
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
for mutation in '.[0][0].number=0' '.[0][0].base.ref="main"' '.[0][0].head.sha="bad"' '.[0] += .[0]' '. + [.[0]]'; do
  reset
  jq "$mutation" "$work/pulls.json" >"$work/new.json"
  mv "$work/new.json" "$work/pulls.json"
  if run >/dev/null 2>&1; then printf 'dev discovery accepted malformed routing inventory: %s\n' "$mutation" >&2; exit 1; fi
done
reset
jq -n '[[],[]]' >"$work/pulls.json"
if run >/dev/null 2>&1; then printf 'dev discovery accepted nonterminal short page\n' >&2; exit 1; fi
reset
jq --arg head "$head" '[[range(1; 101) | . as $n | $n]] | [.[0] | map({number:., state:"open", base:{ref:"dev",repo:{full_name:"owner/repo"}}, head:{sha:$head}})]' "$work/pulls.json" >"$work/new.json"
mv "$work/new.json" "$work/pulls.json"
run >/dev/null || { printf 'dev discovery refused a final page of exactly 100\n' >&2; exit 1; }
printf 'dev discovery fixtures: exact current heads, superseded-head cleanup and malformed pagination refusals passed\n'
