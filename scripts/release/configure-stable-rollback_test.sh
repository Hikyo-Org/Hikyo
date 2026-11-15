#!/bin/sh
# Rolling the unattended release back (HIKYO_STABLE_RELEASE_APP_ID unset) must
# remove the app-only trust branch policy, or the human ceremony could not push
# release/sync-trust-* branches. The fake keeps a ruleset list and stops the
# script (exit 99) where it starts configuring main, after the stable policies.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-stable-rollback.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
mkdir "$scratch/bin"
printf '[]\n' >"$scratch/rulesets.json"
export FIXTURE="$scratch"
cat >"$scratch/bin/gh" <<'SH'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$FIXTURE/calls"
state=$FIXTURE/rulesets.json
method=GET jq_expr='' input='' path=''
while [ "$#" -gt 0 ]; do
	case $1 in
		api) ;;
		--method) method=$2; shift ;;
		--jq) jq_expr=$2; shift ;;
		--input) input=$2; shift ;;
		*) path=$1 ;;
	esac
	shift
done
tmp=$FIXTURE/next.json
case "$method $path" in
	'GET repos/o/r/rulesets?per_page=100&page=1') exit 99 ;;
	'GET repos/o/r/rulesets') if [ -n "$jq_expr" ]; then jq -r "$jq_expr" "$state"; else cat "$state"; fi ;;
	'POST repos/o/r/rulesets') jq --slurpfile in "$input" '. as $all | . + [($in[0] + {id: (($all | length) + 101)})]' "$state" >"$tmp" && mv "$tmp" "$state" ;;
	'PUT repos/o/r/rulesets/'*) id=${path##*/}; jq --slurpfile in "$input" --argjson id "$id" 'map(if .id == $id then $in[0] + {id: $id} else . end)' "$state" >"$tmp" && mv "$tmp" "$state" ;;
	'DELETE repos/o/r/rulesets/'*) id=${path##*/}; jq --argjson id "$id" 'map(select(.id != $id))' "$state" >"$tmp" && mv "$tmp" "$state" ;;
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
trust='stable trust sync branches require stable-release app'
has() { jq -e --arg name "$1" '[.[] | select(.name == $name)] | length == 1' "$scratch/rulesets.json" >/dev/null; }
tag_apps() { jq -c '[.[] | select(.name == "release tags require admin role") | .bypass_actors[] | select(.actor_type == "Integration") | .actor_id]' "$scratch/rulesets.json"; }

(export HIKYO_STABLE_RELEASE_APP_ID=4242; configure)
has "$trust" || fail 'the app policy was not installed'
[ "$(tag_apps)" = '[4242]' ] || fail "the tag policy did not admit the app: $(tag_apps)"
jq -e --arg name "$trust" '.[] | select(.name == $name) | .bypass_actors == [{actor_id: 4242, actor_type: "Integration", bypass_mode: "always"}]' "$scratch/rulesets.json" >/dev/null || fail 'the trust branch policy does not admit only the app'

unset HIKYO_STABLE_RELEASE_APP_ID
configure
has "$trust" && fail 'the app-only trust branch policy survived the rollback'
grep -q 'DELETE repos/o/r/rulesets/' "$scratch/calls" || fail 'the rollback did not delete the policy'
[ "$(tag_apps)" = '[]' ] || fail "the tag policy still admits an app after the rollback: $(tag_apps)"
# Rolling back again changes nothing.
configure
grep -q 'DELETE' "$scratch/calls" && fail 'a second rollback deleted something'
printf 'stable rollback fixture: unset app id removes the trust branch policy and the app tag bypass\n'
