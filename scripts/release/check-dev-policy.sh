#!/bin/sh
set -eu

bootstrap=false
if [ "${1:-}" = --bootstrap ]; then
	bootstrap=true
	shift
fi
[ "$#" -eq 1 ] || { printf 'usage: %s [--bootstrap] POLICY_JSON\n' "$0" >&2; exit 2; }
jq -se --argjson bootstrap "$bootstrap" '
  def one($type): [.rules[] | select(.type == $type)] |
    if length == 1 then .[0] else error("missing or duplicate " + $type) end;
  def check:
    type == "object" and .context == "ci-dev-required" and
    (keys - ["context", "integration_id"] == []) and
    (.integration_id == null or (.integration_id | type == "number" and . >= 0 and floor == .));
  if length != 1 or (.[0] | type) != "object" then error("expected one policy object") else .[0] end |
  if (.rules | type) != "array" or any(.rules[]; type != "object" or (.type | type) != "string")
  then error("invalid dev rules") else . end |
  .name == "dev requires trusted fast feedback" and .target == "branch" and
  .enforcement == "active" and .bypass_actors == [] and
  .conditions.ref_name == {include:["refs/heads/dev"],exclude:[]} and
  all(.rules[]; .type != "merge_queue") and
  (one("required_signatures") | .type == "required_signatures") and
  (one("deletion") | .type == "deletion") and
  (one("non_fast_forward") | .type == "non_fast_forward") and
  (one("pull_request").parameters |
    .required_review_thread_resolution == true and .dismiss_stale_reviews_on_push == true and
    .allowed_merge_methods == ["merge"]) and
  (one("required_status_checks").parameters |
    .strict_required_status_checks_policy == false and
    .do_not_enforce_on_create == $bootstrap and
    (.required_status_checks | type == "array" and length == 1 and (.[0] | check)))
' "$1" >/dev/null || { printf 'dev policy: invalid protection contract\n' >&2; exit 1; }
