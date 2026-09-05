#!/bin/sh
set -eu

fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-bind-manifest.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM
printf '{"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","key_id":"primary-1","public_key":"primary-1.pub","sequence":7,"version":"0.1.0"}\n' \
	>"$fixture_dir/release-candidate.json"
candidate_sha=$(shasum -a 256 "$fixture_dir/release-candidate.json" | awk '{print $1}')
jq -n --slurpfile candidate "$fixture_dir/release-candidate.json" '{
 schema: "hikyo.dev/upgrade-compatibility/v1", profile: "stable/v1",
 version: $candidate[0].version, sequence: $candidate[0].sequence, commit: $candidate[0].commit,
 engines: ["sqlite", "postgres"] | map({migrations: {engine: ., entries: []}, schema_sha256: ("a" * 64), sources: []})
}' >"$fixture_dir/upgrade-compatibility.json"
compatibility_sha=$(shasum -a 256 "$fixture_dir/upgrade-compatibility.json" | awk '{print $1}')

jq -n --arg candidate_sha "$candidate_sha" --arg compatibility_sha "$compatibility_sha" '{
	version: "0.1.0", release_sequence: 7,
	source_commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", signing_key_id: "primary-1",
	artifacts: [{name: "release-candidate.json", kind: "release-candidate", sha256: $candidate_sha}, {name: "upgrade-compatibility.json", kind: "upgrade-compatibility", sha256: $compatibility_sha}]
}' >"$fixture_dir/manifest.json"
jq -n '{
	schema: "hikyo.dev/trust-metadata/v1", sequence: 4,
	highest_release: "0.0.9", highest_release_sequence: 6,
	recovery: {id: "recovery-1", sha256: ("3" * 64)},
	event: {type: "rotation", signed_by: "recovery-1"},
	primary_keys: [{
		id: "primary-1", public_key: "primary-1.pub", sha256: ("2" * 64),
		valid_from_release_sequence: 1, valid_through_release_sequence: null,
		revoked: false
	}],
	releases: [{version: "0.0.9", sequence: 6, manifest_sha256: ("1" * 64)}],
	pending_release: {version: "0.1.0", sequence: 7, manifest_sha256: ("0" * 64)}
}' >"$fixture_dir/metadata.json"

"$(dirname "$0")/bind-manifest.sh" "$fixture_dir/manifest.json" \
	"$fixture_dir/metadata.json" "$fixture_dir/bound.json" >/dev/null
want=$(shasum -a 256 "$fixture_dir/manifest.json" | awk '{print $1}')
jq -e --arg want "$want" '
	.sequence == 5 and .event == {type: "release", signed_by: "recovery-1"} and
	.highest_release == "0.1.0" and .highest_release_sequence == 7 and
	.pending_release == null and .releases[1].manifest_sha256 == $want
	' "$fixture_dir/bound.json" >/dev/null
printf 'manifest binding fixture: recovery metadata binds exact release bytes\n'
printf ' ' >>"$fixture_dir/upgrade-compatibility.json"
if "$(dirname "$0")/bind-manifest.sh" "$fixture_dir/manifest.json" "$fixture_dir/metadata.json" "$fixture_dir/tampered.json" > /dev/null 2>"$fixture_dir/tampered.log"; then
 printf 'manifest binding fixture: changed compatibility accepted\n' >&2; exit 1
fi
grep -F 'upgrade-compatibility.json hash mismatch' "$fixture_dir/tampered.log" >/dev/null
rm "$fixture_dir/upgrade-compatibility.json"
if "$(dirname "$0")/bind-manifest.sh" "$fixture_dir/manifest.json" "$fixture_dir/metadata.json" "$fixture_dir/missing.json" > /dev/null 2>"$fixture_dir/missing.log"; then
 printf 'manifest binding fixture: missing compatibility accepted\n' >&2; exit 1
fi
grep -F 'upgrade-compatibility.json is absent' "$fixture_dir/missing.log" >/dev/null
printf 'manifest binding fixture: compatibility mandatory and byte-bound\n'


jq '.commit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' \
	"$fixture_dir/release-candidate.json" >"$fixture_dir/candidate-mismatch.json"
mv "$fixture_dir/candidate-mismatch.json" "$fixture_dir/release-candidate.json"
if "$(dirname "$0")/bind-manifest.sh" "$fixture_dir/manifest.json" \
	"$fixture_dir/metadata.json" "$fixture_dir/rejected.json" \
	>/dev/null 2>"$fixture_dir/rejected.log"; then
	printf 'manifest binding fixture failed: mismatched candidate was accepted\n' >&2
	exit 1
fi
printf 'manifest binding fixture: mismatched candidate refused\n'
