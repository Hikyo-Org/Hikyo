#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
	printf 'usage: %s POLICY_JSON\n' "$0" >&2
	exit 2
fi
command -v jq >/dev/null 2>&1 || {
	printf 'main policy: jq is required; install jq before local policy/docs validation (docs CI uses verify-docs.sh setup)\n' >&2
	exit 1
}
jq -se '
	def check($context):
		type == "object" and .context == $context and
		(keys - ["context", "integration_id"] == []) and
		(.integration_id == null or (.integration_id | type == "number" and . >= 0 and floor == .));
	def one($type): [.rules[] | select(.type == $type)] | if length == 1 then .[0] else error("missing or duplicate " + $type) end;
	if length != 1 then error("expected exactly one policy document") else .[0] end |
	if type != "object" or (.rules | type) != "array" then error("expected policy object and rules array") else . end |
	if any(.rules[]; .type == "required_linear_history")
	then error("MERGE queue is incompatible with required_linear_history") else . end |
	all(.rules[]; type == "object" and (.type | type) == "string") and
	.name == "main requires PR and release CI" and
	.target == "branch" and .enforcement == "active" and
	.bypass_actors == [] and
	.conditions.ref_name == {include:["refs/heads/main"], exclude:[]} and
	(one("deletion") | .type == "deletion") and
	(one("non_fast_forward") | .type == "non_fast_forward") and
	(one("required_signatures") | .type == "required_signatures") and
	(one("pull_request").parameters |
		.required_review_thread_resolution == true and .dismiss_stale_reviews_on_push == true) and
	(one("required_status_checks").parameters |
		.do_not_enforce_on_create == false and
		.strict_required_status_checks_policy == false and
		(.required_status_checks | type == "array" and length == 1 and
			(.[0] | check("ci-queue-required")))) and
	one("merge_queue").parameters == {
		check_response_timeout_minutes:60, grouping_strategy:"ALLGREEN",
		max_entries_to_build:1, max_entries_to_merge:1, merge_method:"MERGE",
		min_entries_to_merge:1, min_entries_to_merge_wait_minutes:0}
' "$1" >/dev/null || {
	printf 'main policy: invalid queue protection contract\n' >&2
	exit 1
}
