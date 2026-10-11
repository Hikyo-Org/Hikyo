#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-green-main.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM
commit=176e6e67379d0675e6211f0491dc965cee4f1c5c
cat >"$fixture_dir/gh" <<EOF_GH
#!/bin/sh
case "\$2" in
  repos/Hikyo-Org/hikyo/actions/workflows/ci-main.yml/runs\?*per_page=1) cat '$fixture_dir/manual.json' ;;
  repos/Hikyo-Org/hikyo/actions/workflows/ci.yml/runs\?*per_page=1) cat '$fixture_dir/legacy.json' ;;
  *) echo 'unexpected workflow proof query' >&2; exit 2 ;;
esac
EOF_GH
chmod +x "$fixture_dir/gh"
jq -nc --arg commit "$commit" '{total_count:1,workflow_runs:[{id:20,head_sha:$commit,head_branch:"main",event:"workflow_dispatch",path:".github/workflows/ci-main.yml",status:"completed",conclusion:"success"}]}' >"$fixture_dir/valid-manual.json"
printf '{"total_count":0,"workflow_runs":[]}\n' >"$fixture_dir/empty.json"
reset_fixture() {
 cp "$fixture_dir/valid-manual.json" "$fixture_dir/manual.json"
 cp "$fixture_dir/empty.json" "$fixture_dir/legacy.json"
}
proof() {
 GH_BIN="$fixture_dir/gh" "$script_dir/require-green-main.sh" Hikyo-Org/hikyo "$commit"
}
reset_fixture
proof >/dev/null
for mutation in \
 '.workflow_runs[0].head_sha = ("a"*40)' \
 '.workflow_runs[0].head_branch = "feature"' \
 '.workflow_runs[0].event = "pull_request"' \
 '.workflow_runs[0].event = "push"' \
 '.workflow_runs[0].event = "schedule"' \
 '.workflow_runs[0].path = ".github/workflows/cache-warm.yml"' \
 '.workflow_runs[0].id = 0' \
 '.workflow_runs[0].status = "in_progress"' \
 '.workflow_runs[0].conclusion = "failure"' \
 '.workflow_runs[0].conclusion = "cancelled"' \
 '.workflow_runs = []' \
 '.workflow_runs = null' \
 '.total_count = -1' \
 '.total_count = 0' \
 '.total_count = 1.5' \
 '.workflow_runs += .workflow_runs'; do
 reset_fixture
 jq "$mutation" "$fixture_dir/valid-manual.json" >"$fixture_dir/manual.json"
 if proof >/dev/null 2>&1; then
  printf 'green main fixture failed: invalid provenance accepted: %s\n' "$mutation" >&2
  exit 1
 fi
done
# The latest-only queries are independent of large workflow histories or any
# number of unrelated observer/benchmark callbacks on the same main commit.
reset_fixture
jq '.total_count = 101' "$fixture_dir/valid-manual.json" >"$fixture_dir/manual.json"
proof >/dev/null
# Historical push/manual main CI remains a valid source before a wrapper run.
for event in push workflow_dispatch; do
 reset_fixture
 cp "$fixture_dir/empty.json" "$fixture_dir/manual.json"
 jq --arg event "$event" '.workflow_runs[0].path = ".github/workflows/ci.yml" | .workflow_runs[0].event = $event' "$fixture_dir/valid-manual.json" >"$fixture_dir/legacy.json"
 proof >/dev/null
done
# A newer failed/queued latest run in either definition blocks the older pass.
for newer in manual legacy; do
 for result in failure in_progress; do
  reset_fixture
  jq '.workflow_runs[0].id = 10 | .workflow_runs[0].path = ".github/workflows/ci.yml" | .workflow_runs[0].event = "push"' "$fixture_dir/valid-manual.json" >"$fixture_dir/legacy.json"
  jq --arg result "$result" '.workflow_runs[0].id = 30 | if $result == "in_progress" then .workflow_runs[0].status = $result | .workflow_runs[0].conclusion = null else .workflow_runs[0].conclusion = $result end' "$fixture_dir/$newer.json" >"$fixture_dir/newer.json"
  mv "$fixture_dir/newer.json" "$fixture_dir/$newer.json"
  if proof >/dev/null 2>&1; then
   printf 'green main fixture failed: newer %s %s hid behind older pass\n' "$newer" "$result" >&2
   exit 1
  fi
 done
done
reset_fixture
printf '{"total_count":1,"workflow_runs":[]}\n' >"$fixture_dir/legacy.json"
if proof >/dev/null 2>&1; then
 echo 'green main fixture failed: incomplete legacy response accepted' >&2
 exit 1
fi
printf 'green main fixture: exact immutable source, latest cross-workflow proof, large histories and provenance refusals passed\n'
