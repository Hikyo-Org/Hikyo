# Input is go test -json, slurped as an array. Never certify skipped or partial runs.
def evidence:
  [.[] | .Output? // empty | select(contains("GITHUB_CONTRACT_EVIDENCE "))
   | split("GITHUB_CONTRACT_EVIDENCE ")[1] | fromjson];
def run_ids($kind): [.[] | select(.kind == $kind) | .run_id] | sort;
def artifact_ids($kind): [.[] | select(.kind == $kind) | .artifact_id] | sort;
def positive_id: type == "number" and . > 0 and floor == .;
def names($kind): [.[] | select(.kind == $kind) | [.destination,.surface,.name]] | sort;
. as $log | evidence as $events |
([ $log[] | select(.Action == "skip" or .Action == "fail") ] | length == 0) and
([ $log[] | select(.Action == "pass" and .Test != null) | .Test ] | sort) ==
(["TestGitHubDotComContract",
 "TestGitHubDotComContract/repository_missing_Variables_write_fails_first-sync_sentinel",
 "TestGitHubDotComContract/organization_missing_Variables_write_fails_first-sync_sentinel",
 "TestGitHubDotComContract/repository_minimum_token_and_workflow_consumption",
 "TestGitHubDotComContract/organization_minimum_token_and_workflow_consumption",
 "TestGitHubDotComContract/protected_environment_minimum_token_and_workflow_consumption",
 "TestGitHubDotComContract/administration_token_auto-create_identity_pin"] | sort) and
([ $events[] | select(.kind == "binding") ] | length == 1) and
([ $events[] | select(.kind == "binding") ] | all(
 .candidate_sha == $candidate and .workflow_sha == $workflow and
 .repository_id == $repository_id and .organization_id == $organization_id and
 .environment_id == $environment_id and .authorization_ref == $approval_ref and
 (.workflow_sha256 | test("^[0-9a-f]{64}$")))) and
([ $events[] | select(.kind == "consumption") ] | length == 18) and
([ $events[] | select(.kind == "consumption") ] | all(
 .workflow_sha == $workflow and (.run_id | positive_id) and (.artifact_id | positive_id) and
 (.secret_sha256 | test("^[0-9a-f]{64}$")) and (.variable_sha256 | test("^[0-9a-f]{64}$")))) and
([ $events[] | select(.kind == "consumption") | .artifact_id ] | unique | length == 18) and
([ $events[] | select(.kind == "consumption") | .destination ] | sort | group_by(.) | map({scope:.[0],count:length})) ==
[{scope:"environment",count:6},{scope:"organization",count:6},{scope:"repository",count:6}] and
([ $events[] | select(.kind == "dispatch_intent") | .nonce ] | unique | length == 18) and
($events | artifact_ids("owned_artifact")) == ($events | artifact_ids("cleaned_artifact")) and
($events | artifact_ids("owned_artifact")) == ($events | artifact_ids("consumption")) and
($events | run_ids("owned_run") | unique | length == 18) and
($events | run_ids("owned_run")) == ($events | run_ids("cleaned_run")) and
($events | run_ids("owned_run")) == ($events | run_ids("consumption")) and
($events | names("owned_name")) == ($events | names("cleaned_name")) and
([ $events[] | select(.kind == "owned_name") ] | length > 0) and
([ $events[] | select(.kind == "owned_environment") | .name ]) ==
([ $events[] | select(.kind == "cleaned_environment") | .name ]) and
([ $events[] | select(.kind == "owned_environment") ] | length == 1)
