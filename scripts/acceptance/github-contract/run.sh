#!/usr/bin/env bash
# Run only with separately approved synthetic resources and protected credentials.
set -euo pipefail
# Tracing would print credentials when they are loaded below.
set +x
umask 077
fail() { printf 'github contract: %s\n' "$*" >&2; exit 1; }
[ "$#" -eq 4 ] || fail 'usage: run.sh CANDIDATE_SHA AUTHORIZATION_JSON CREDENTIALS_JSON NEW_EVIDENCE_DIRECTORY'
candidate=$1
authorization=$2
credentials=$3
evidence=$4
root=$(git rev-parse --show-toplevel)
cd "$root"
for tool in git go jq shasum; do command -v "$tool" >/dev/null || fail "missing $tool"; done
[[ "$candidate" =~ ^[0-9a-f]{40}$ ]] || fail 'candidate must be a full lowercase commit SHA'
[ "$(git rev-parse HEAD)" = "$candidate" ] || fail 'checkout differs from selected candidate'
[ -z "$(git status --porcelain --untracked-files=normal)" ] || fail 'candidate checkout must be clean'
go_version=$(go env GOVERSION)
[ "$go_version" = "go$(sed -n 's/^go //p' go.mod)" ] || fail 'use the pinned Go toolchain'
[ -f "$authorization" ] && [ -f "$credentials" ] || fail 'authorization and protected credential files are required'
# Reject group/world access and symlinks. Values are never printed or sourced.
[ ! -L "$credentials" ] || fail 'credential file must not be a symlink'
if stat --version >/dev/null 2>&1; then mode=$(stat -c '%a' "$credentials"); else mode=$(stat -f '%Lp' "$credentials"); fi
[[ "$mode" =~ ^[0-7]*00$ ]] || fail 'credential file must have no group/world permissions'
now=$(date -u +%s)
jq -e --arg candidate "$candidate" --argjson now "$now" '
  .candidate_sha == $candidate and
  (.approved_by | type == "string" and length > 0) and
  (.approval_ref | type == "string" and length > 0) and
  (.expires_at | fromdateiso8601) > $now and
  .synthetic_only == true and .no_new_spend == true and
  .maximum_workflow_dispatches == 18 and
  (.owner | type == "string" and length > 0) and
  (.repository | type == "string" and length > 0) and
  (.environment | type == "string" and length > 0) and
  (.ref | type == "string" and length > 0) and
  (.workflow_sha | test("^[0-9a-f]{40}$")) and
  ([.repository_id,.organization_id,.environment_id] | all(type == "number" and . > 0 and floor == .)) and
  (.credential_grants_attested == true)
' "$authorization" >/dev/null || fail 'authorization is incomplete, expired, or for another candidate'
# A recorded owner approval is a prerequisite, not authority created by this tool.
for field in OWNER REPOSITORY ENVIRONMENT REF WORKFLOW_SHA REPOSITORY_ID ORGANIZATION_ID ENVIRONMENT_ID; do
  key=$(printf '%s' "$field" | tr '[:upper:]' '[:lower:]')
  value=$(jq -er --arg key "$key" '.[$key] | tostring' "$authorization")
  export "HIKYO_GITHUB_CONTRACT_$field=$value"
done
export HIKYO_GITHUB_CONTRACT_CANDIDATE_SHA="$candidate"
export HIKYO_GITHUB_CONTRACT_AUTHORIZATION_REF
HIKYO_GITHUB_CONTRACT_AUTHORIZATION_REF=$(jq -er .approval_ref "$authorization")
export HIKYO_GITHUB_CONTRACT_WORKFLOW_PATH=.github/workflows/hikyo-contract-consume.yml
for field in REPOSITORY_TOKEN ORGANIZATION_TOKEN ENVIRONMENT_TOKEN REPOSITORY_DENIED_TOKEN ORGANIZATION_DENIED_TOKEN ENVIRONMENT_DENIED_TOKEN HARNESS_TOKEN; do
  value=$(jq -er --arg field "$field" '.[$field] | select(type == "string" and startswith("github_pat_"))' "$credentials") || fail "missing fine-grained $field"
  export "HIKYO_GITHUB_CONTRACT_$field=$value"
done
unset value
# Refuse to overwrite evidence. The directory must be ignored or outside Git.
[ ! -e "$evidence" ] || fail 'evidence directory already exists; preserve the earlier attempt'
case "$evidence" in /*) ;; *) evidence="$root/$evidence" ;; esac
case "$evidence" in "$root"/*) git check-ignore -q "$evidence/receipt.json" || fail 'evidence must be ignored or outside the checkout' ;; esac
mkdir -p "$evidence"
cp "$authorization" "$evidence/authorization.json"
tree=$(git rev-parse 'HEAD^{tree}')
started=$(date -u +%Y-%m-%dT%H:%M:%SZ)
export HIKYO_GITHUB_CONTRACT=1
status=0
# One noncached all-or-nothing test. Run this script through agent-heavy where required.
go test -p 2 -parallel 1 -count=1 -timeout=45m -json ./internal/adapter/githubactions -run '^TestGitHubDotComContract$' >"$evidence/go-test.jsonl" 2>"$evidence/go-test.stderr" || status=$?
finished=$(date -u +%Y-%m-%dT%H:%M:%SZ)
# A success-shaped go exit alone cannot turn a skip into provider proof.
proof=HOLD
observed_head=$(git rev-parse HEAD)
observed_tree=$(git rev-parse 'HEAD^{tree}')
if [ "$status" -eq 0 ] && [ "$observed_head" = "$candidate" ] &&
   [ "$observed_tree" = "$tree" ] &&
   [ -z "$(git status --porcelain --untracked-files=normal)" ] &&
   jq -e --argjson now "$(date -u +%s)" '(.expires_at | fromdateiso8601) > $now' "$evidence/authorization.json" >/dev/null &&
   jq -se --arg candidate "$candidate" --arg workflow "$HIKYO_GITHUB_CONTRACT_WORKFLOW_SHA" \
     --argjson repository_id "$HIKYO_GITHUB_CONTRACT_REPOSITORY_ID" \
     --argjson organization_id "$HIKYO_GITHUB_CONTRACT_ORGANIZATION_ID" \
     --argjson environment_id "$HIKYO_GITHUB_CONTRACT_ENVIRONMENT_ID" \
     --arg approval_ref "$HIKYO_GITHUB_CONTRACT_AUTHORIZATION_REF" \
     -f scripts/acceptance/github-contract/verify.jq "$evidence/go-test.jsonl" >/dev/null; then proof=PASS; fi
if ! jq -s '[.[] | .Output? // empty | select(contains("GITHUB_CONTRACT_EVIDENCE ")) | split("GITHUB_CONTRACT_EVIDENCE ")[1] | fromjson]' "$evidence/go-test.jsonl" >"$evidence/events.json" 2>"$evidence/events.stderr"; then
  proof=HOLD
  printf '[]\n' >"$evidence/events.json"
fi
log_sha=$(shasum -a 256 "$evidence/go-test.jsonl" | cut -d ' ' -f 1)
jq -n --arg status "$proof" --arg candidate "$candidate" --arg tree "$tree" --arg started "$started" --arg finished "$finished" --arg log_sha "$log_sha" --argjson exit_code "$status" --arg go_version "$go_version" --arg observed_head "$observed_head" --arg observed_tree "$observed_tree" --slurpfile authorization "$evidence/authorization.json" --slurpfile events "$evidence/events.json" '
 {schema:1,status:$status,contract:"TestGitHubDotComContract",candidate_sha:$candidate,source_tree:$tree,started_at:$started,finished_at:$finished,go_test_exit_code:$exit_code,go_version:$go_version,observed_head_after:$observed_head,observed_tree_after:$observed_tree,go_test_log_sha256:$log_sha,authorization:$authorization[0],events:$events[0],credential_revocation:"NOTRUN: owner must revoke all seven tokens after cleanup",scope:"Real github.com adapter API and hash-only workflow consumption; not whole-program, publication, deployment, or GHES proof"}
' >"$evidence/receipt.json"
printf 'github contract: %s; receipt %s/receipt.json\n' "$proof" "$evidence"
[ "$proof" = PASS ] || exit 1
