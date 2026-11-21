#!/bin/sh
# The unattended release's repository policy:
#  - the App gets its tag and trust-branch bypass only after the highest stable tag's
#    own commit judges a tag by who pushed it (the stable line predates it otherwise);
#  - unsetting HIKYO_STABLE_RELEASE_APP_ID rolls everything back, including the
#    app trust branch policy a human ceremony could not push through;
#  - every ruleset listing is paginated, so a policy on page two is updated, not duplicated;
#  - the trust-sync branches admit exactly the App and the repository admin role;
#  - verify-repository-rulesets.sh fails on every deviation from that.
# The fake keeps a ruleset list and stops the configure script (exit 99) where it
# would create the main-branch policy, after the stable policies.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-stable-rollback.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
mkdir "$scratch/bin"
printf '[]\n' >"$scratch/rulesets.json"
printf '[]\n' >"$scratch/tags.json"
export FIXTURE="$scratch"
cat >"$scratch/bin/gh" <<'SH'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$FIXTURE/calls"
state=$FIXTURE/rulesets.json
method=GET jq_expr='' input='' path='' paginate=''
while [ "$#" -gt 0 ]; do
	case $1 in
		api) ;;
		--method) method=$2; shift ;;
		--jq) jq_expr=$2; shift ;;
		--input) input=$2; shift ;;
		--paginate | --slurp) paginate=1 ;;
		*) path=$1 ;;
	esac
	shift
done
tmp=$FIXTURE/next.json
show() { if [ -n "$jq_expr" ]; then jq -r "$jq_expr"; else cat; fi; }
# The commit a tag names, and the file served at that commit.
commit_of() { printf '%040d' "$(printf '%s' "$1" | cksum | cut -d' ' -f1)"; }
case "$method $path" in
	'GET repos/o/r/rulesets?per_page=100&page='*)
		page=${path##*page=}
		jq -c --argjson page "$page" '.[($page - 1) * 100 : $page * 100]' "$state" ;;
	'GET repos/o/r/rulesets/'*) id=${path##*/}; jq -c --argjson id "$id" '.[] | select(.id == $id)' "$state" | show ;;
	'POST repos/o/r/rulesets')
		[ "$(jq -r '.name' "$input")" != 'main requires PR and release CI' ] || exit 99
		jq --slurpfile in "$input" '. as $all | . + [($in[0] + {id: (($all | length) + 101)})]' "$state" >"$tmp" && mv "$tmp" "$state" ;;
	'PUT repos/o/r/rulesets/'*) id=${path##*/}; jq --slurpfile in "$input" --argjson id "$id" 'map(if .id == $id then $in[0] + {id: $id} else . end)' "$state" >"$tmp" && mv "$tmp" "$state" ;;
	'DELETE repos/o/r/rulesets/'*) id=${path##*/}; jq --argjson id "$id" 'map(select(.id != $id))' "$state" >"$tmp" && mv "$tmp" "$state" ;;
	'GET repos/o/r/git/matching-refs/tags/v')
		[ -n "$paginate" ] || { printf 'the tag listing must be paginated\n' >&2; exit 2; }
		[ "${FIXTURE_FAIL:-}" != tags ] || { printf 'gh: server error (HTTP 500)\n' >&2; exit 1; }
		jq -c '[.]' "$FIXTURE/tags.json" ;;
	'GET repos/o/r/commits/v'*) printf '{"sha":"%s"}\n' "$(commit_of "${path#repos/o/r/commits/}")" | show ;;
	'GET repos/o/r/contents/scripts/release/require-signed-tag.sh?ref='*)
		file=$FIXTURE/authority-${path##*ref=}
		[ -f "$file" ] || { printf 'gh: Not Found (HTTP 404)\n' >&2; exit 1; }
		printf '{"content":"%s"}\n' "$(base64 <"$file" | tr -d '\n')" | show ;;
	*) printf 'unexpected gh invocation: %s\n' "$*" >&2; exit 2 ;;
esac
SH
chmod +x "$scratch/bin/gh"
fail() { printf 'stable rollback fixture: %s\n' "$*" >&2; exit 1; }
configure() { # exits 99 where it reaches main configuration
	: >"$scratch/calls"
	set +e
	GH_BIN="$scratch/bin/gh" "$script_dir/configure-repository.sh" o/r >"$scratch/log" 2>&1
	code=$?
	set -e
	[ "$code" = 99 ] || { cat "$scratch/log" >&2; fail "the script stopped with $code, not at main configuration"; }
}
refused() { # fragment: configure must stop with that message before it changes any ruleset
	: >"$scratch/calls"
	set +e
	GH_BIN="$scratch/bin/gh" "$script_dir/configure-repository.sh" o/r >"$scratch/log" 2>&1
	code=$?
	set -e
	[ "$code" != 99 ] && [ "$code" != 0 ] || fail "configure was not refused for: $1"
	grep -qF -- "$1" "$scratch/log" || { cat "$scratch/log" >&2; fail "configure was refused for the wrong reason, want: $1"; }
	if grep -Eq '^(api )?--method (POST|PUT|DELETE)' "$scratch/calls"; then cat "$scratch/calls" >&2; fail "configure changed a ruleset although it refused: $1"; fi
}
trust='stable trust sync branches require stable-release app'
has() { jq -e --arg name "$1" '[.[] | select(.name == $name)] | length == 1' "$scratch/rulesets.json" >/dev/null; }
tag_apps() { jq -c '[.[] | select(.name == "release tags require admin role") | .bypass_actors[] | select(.actor_type == "Integration") | .actor_id]' "$scratch/rulesets.json"; }
# set_tags TAG...: the repository's tags, and the commit of each, whose tag authority is old or new.
tag_commit() { printf '%040d' "$(printf '%s' "$1" | cksum | cut -d' ' -f1)"; }
set_tags() { printf '%s\n' "$@" | jq -R '{ref: ("refs/tags/" + .)}' | jq -s . >"$scratch/tags.json"; }
authority() { # tag old|new
	if [ "$2" = half ]; then printf '.triggering_actor.type only\n' >"$scratch/authority-$(tag_commit "$1")"
	elif [ "$2" = half2 ]; then printf 'release_run_jq only\n' >"$scratch/authority-$(tag_commit "$1")"
	elif [ "$2" = new ]; then printf '# actor-based\nrelease_run_jq=1\n.triggering_actor.type\n' >"$scratch/authority-$(tag_commit "$1")"
	else printf '# the pre-actor script\nreleased_by_collaborator\n' >"$scratch/authority-$(tag_commit "$1")"; fi
}
export HIKYO_STABLE_RELEASE_APP_ID=4242

# --- the app is not granted anything while the stable line predates the actor-based tag authority.
refused 'no stable release tag exists'
set_tags v1.0.0 v1.1.0 v1.2.0-rc.1 v1.3.0-nightly.20261101.1.gabcdef12
authority v1.0.0 new
authority v1.1.0 old
authority v1.2.0-rc.1 new
refused 'predates the actor-based tag authority'
# Half of the marker is not the marker.
authority v1.1.0 half
refused 'predates the actor-based tag authority'
authority v1.1.0 half2
refused 'predates the actor-based tag authority'
authority v1.1.0 old
# Only a stable tag counts: a prerelease or a nightly that carries the marker does not stand in for it.
[ "$(jq -r '.[0].enforcement // "none"' "$scratch/rulesets.json")" = none ] || fail 'a refused configure left a ruleset behind'
# A tag listing that fails is a failure, not "no tag".
(FIXTURE_FAIL=tags; export FIXTURE_FAIL; : >"$scratch/calls"; if GH_BIN="$scratch/bin/gh" "$script_dir/configure-repository.sh" o/r >"$scratch/log" 2>&1; then fail 'configured despite a failed tag listing'; fi)
grep -Eq -- '--method (POST|PUT)' "$scratch/calls" && fail 'configured despite a failed tag listing'
# The highest stable tag by semantic version (1.10.0, not the text-order last) carries the marker.
set_tags v1.9.0 v1.10.0 v1.11.0-rc.1 v1.11.0-nightly.20261101.1.gabcdef12
authority v1.9.0 old
authority v1.10.0 new
authority v1.11.0-rc.1 old
authority v1.11.0-nightly.20261101.1.gabcdef12 old
configure
has "$trust" || fail 'the app policy was not installed'
[ "$(tag_apps)" = '[4242]' ] || fail "the tag policy did not admit the app: $(tag_apps)"
# The trust branches admit exactly the app and the admin role: a repository admin keeps a way in.
jq -e --arg name "$trust" '.[] | select(.name == $name) | (.bypass_actors | sort_by(.actor_type, .actor_id)) == [
	{actor_id: 4242, actor_type: "Integration", bypass_mode: "always"},
	{actor_id: 5, actor_type: "RepositoryRole", bypass_mode: "always"}]' "$scratch/rulesets.json" >/dev/null || fail 'the trust branch policy does not admit exactly the app and the admin role'

# --- listings are paginated: with the policies on page two, configure updates them and creates nothing.
cp "$scratch/rulesets.json" "$scratch/configured.json"
jq -c '[range(1; 101) | {id: ., name: ("unrelated " + tostring), enforcement: "active"}] + .' "$scratch/configured.json" >"$scratch/rulesets.json"
configure
if grep -- '--method POST' "$scratch/calls" | grep -vq 'hikyo-main-policy'; then cat "$scratch/calls" >&2; fail 'a policy on page two was not found and was created again'; fi
grep -q 'page=2' "$scratch/calls" || fail 'the second page of rulesets was never read'
[ "$(jq '[.[] | select(.name == "release tags are immutable")] | length' "$scratch/rulesets.json")" = 1 ] || fail 'a policy was duplicated'
cp "$scratch/configured.json" "$scratch/rulesets.json"
# Two policies with one name are ambiguous: stop before changing either.
jq -c '. + [.[] | select(.name == "release tags are immutable") | .id += 500]' "$scratch/configured.json" >"$scratch/rulesets.json"
refused 'ambiguous ruleset name'
cp "$scratch/configured.json" "$scratch/rulesets.json"

# --- the verifier asserts the whole policy; every deviation fails.
printf '{"id":200,"name":"main requires PR and release CI","enforcement":"active"}\n' | jq -s --slurpfile r "$scratch/rulesets.json" '$r[0] + .' >"$scratch/with-main.json"
mv "$scratch/with-main.json" "$scratch/rulesets.json"
verify() { GH_BIN="$scratch/bin/gh" "$script_dir/verify-repository-rulesets.sh" o/r "${1-4242}" >"$scratch/log" 2>&1; }
verify || { cat "$scratch/log" >&2; fail 'the verifier refused a correct policy'; }
cp "$scratch/rulesets.json" "$scratch/good.json"
tamper() { # description jq-expression
	jq "$2" "$scratch/good.json" >"$scratch/rulesets.json"
	if verify; then fail "the verifier accepted: $1"; fi
}
trust_edit() { printf 'map(if .name == "%s" then %s else . end)' "$trust" "$1"; }
tamper 'a trust policy without the admin role' "$(trust_edit '.bypass_actors |= map(select(.actor_type != "RepositoryRole"))')"
tamper 'a trust policy without the app' "$(trust_edit '.bypass_actors |= map(select(.actor_type != "Integration"))')"
tamper 'another actor on the trust policy' "$(trust_edit '.bypass_actors += [{actor_id: 7, actor_type: "Integration", bypass_mode: "always"}]')"
tamper 'a role other than admin on the trust policy' "$(trust_edit '.bypass_actors |= map(if .actor_type == "RepositoryRole" then .actor_id = 4 else . end)')"
tamper 'a disabled trust policy' "$(trust_edit '.enforcement = "disabled"')"
tamper 'a trust policy on other branches' "$(trust_edit '.conditions.ref_name.include = ["refs/heads/release/*"]')"
creation_edit() { printf 'map(if .name == "release tags require admin role" then %s else . end)' "$1"; }
tamper 'a tag creation policy open to another app' "$(creation_edit '.bypass_actors += [{actor_id: 7, actor_type: "Integration", bypass_mode: "always"}]')"
tamper 'a tag creation policy without the admin role' "$(creation_edit '.bypass_actors |= map(select(.actor_type != "RepositoryRole"))')"
tamper 'a tag creation policy for the maintain role' "$(creation_edit '.bypass_actors |= map(if .actor_type == "RepositoryRole" then .actor_id = 2 else . end)')"
tamper 'a tag creation policy aimed at branches' "$(creation_edit '.target = "branch"')"
tamper 'a disabled tag creation policy' "$(creation_edit '.enforcement = "disabled"')"
tamper 'tag creation that also covers nightlies' "$(creation_edit '.conditions.ref_name.exclude = []')"
cp "$scratch/good.json" "$scratch/rulesets.json"
# The rolled-back state is asserted as strictly: no app on the tags, no trust policy.
if verify ''; then fail 'the verifier accepted the app policy as a rolled-back state'; fi

# --- rolling back (the id unset) needs no marker and removes the app everywhere.
cp "$scratch/configured.json" "$scratch/rulesets.json"
unset HIKYO_STABLE_RELEASE_APP_ID
set_tags v1.0.0
authority v1.0.0 old
configure
has "$trust" && fail 'the app-only trust branch policy survived the rollback'
grep -q 'DELETE repos/o/r/rulesets/' "$scratch/calls" || fail 'the rollback did not delete the policy'
[ "$(tag_apps)" = '[]' ] || fail "the tag policy still admits an app after the rollback: $(tag_apps)"
jq -e '[.[] | select(.name == "main requires PR and release CI")] | length == 0' "$scratch/rulesets.json" >/dev/null
# Rolling back again changes nothing.
configure
grep -q 'DELETE' "$scratch/calls" && fail 'a second rollback deleted something'
printf 'stable rollback fixture: the app is granted only after the stable tag authority is actor-based; paginated listings; admin and app on the trust branches; rollback; verifier deviations\n'
