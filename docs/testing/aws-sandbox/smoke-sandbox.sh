#!/usr/bin/env bash
# Provision, inspect and tear down the sandbox for the local real-AWS smoke
# (`TestAWSSecretsManagerRealSmoke`, hikyo #158). Meant for AWS CloudShell in
# eu-central-1, signed in as the sandbox account owner with MFA.
#
#   smoke-sandbox.sh check
#   smoke-sandbox.sh provision [RUN]     RUN is 10 lowercase hex digits; random if omitted
#   smoke-sandbox.sh teardown RUN
#
# provision creates one customer KMS key plus three IAM users (writer, oracle,
# cleanup), renders the policy templates next to this script, mints 3-hour
# GetSessionToken sessions and prints the manifest and descriptors to stdout.
# Long-term access keys live only in shell variables. Nothing secret is written
# to disk or logged; stdout carries the session credentials, so do not tee it.
#
# Access-key window: each user's long-term access key is created active and stays
# active, with its policies attached, from provision until teardown deactivates
# it. Run teardown promptly once the smoke has finished (or failed). Provision
# prints RUN and the exact teardown command on stderr before it creates anything,
# and again if it fails midway.
#
# The global flags --endpoint-url and --ca-bundle exist only to dry-run this
# script against a local emulator (Floci). --endpoint-url accepts only localhost,
# 127.0.0.0/8, ::1 and RFC1918 IPv4 literals; --ca-bundle needs --endpoint-url.
set -euo pipefail

REGION=eu-central-1
HERE=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
AWS_EXTRA=()
DRY_RUN=0
CA_BUNDLE=0
RUN_ID=

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

aws_() {
	command aws --region "$REGION" --output json ${AWS_EXTRA[@]+"${AWS_EXTRA[@]}"} "$@"
}

need() {
	local tool
	for tool in aws jq; do
		command -v "$tool" >/dev/null || die "$tool is required"
	done
}

# user_exists USER: 0 when present, 1 when IAM answers NoSuchEntity; any other
# failure aborts, so a failed lookup never reads as "already gone".
user_exists() {
	local out
	if out=$(aws_ iam get-user --user-name "$1" 2>&1); then
		return 0
	fi
	[[ $out == *NoSuchEntity* ]] || die "iam get-user $1: $out"
	return 1
}

# emulator_endpoint URL: allowlist. Only localhost, 127.0.0.0/8, ::1 and RFC1918
# IPv4 literals are emulators. The authority is cut at the first / ? or # before
# the userinfo is stripped, so neither https://a:b@host nor https://x/@127.0.0.1
# can smuggle a host past the check.
emulator_endpoint() {
	local u auth host a b
	u=$(tr '[:upper:]' '[:lower:]' <<<"$1")
	[[ $u =~ ^https?:// ]] || return 1
	[[ $u != *[[:space:]\\]* ]] || return 1
	auth=${u#*://}
	auth=${auth%%[/?#]*}
	auth=${auth##*@}
	[[ $auth =~ ^\[::1\](:[0-9]+)?$ ]] && return 0
	[[ $auth =~ ^([^:]+)(:[0-9]+)?$ ]] || return 1
	host=${BASH_REMATCH[1]}
	[[ $host == localhost ]] && return 0
	[[ $host =~ ^(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})$ ]] || return 1
	a=${BASH_REMATCH[1]} b=${BASH_REMATCH[2]}
	((a <= 255 && b <= 255 && BASH_REMATCH[3] <= 255 && BASH_REMATCH[4] <= 255)) || return 1
	((a == 127 || a == 10 || (a == 172 && b >= 16 && b <= 31) || (a == 192 && b == 168)))
}

# teardown_cmd RUN: the exact command that removes RUN (keeps the emulator flags).
teardown_cmd() {
	local c
	printf -v c '%q ' bash "$HERE/smoke-sandbox.sh" ${AWS_EXTRA[@]+"${AWS_EXTRA[@]}"} teardown "$1"
	printf '%s' "${c% }"
}

# Printed once from the main shell (not command-substitution subshells).
recovery_notice() {
	[[ $BASHPID == "$$" && -z ${RECOVERY_SHOWN:-} ]] || return 0
	RECOVERY_SHOWN=1
	printf 'provision failed or stopped midway. RUN=%s. Remove what it created with:\n  %s\n' "$RUN_ID" "$(teardown_cmd "$RUN_ID")" >&2
}

stamp() { date -u -d "+$1 seconds" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -v "+${1}S" +%Y-%m-%dT%H:%M:%SZ; }

# Fixed length, and no "r": no RUN is a prefix of another RUN or of a rerun
# prefix RUNrN, so the hikyo-e2e-RUN* patterns of two runs never overlap.
valid_run() { [[ $1 =~ ^[0-9a-f]{10}$ ]] || die "RUN must be 10 lowercase hex digits: $1"; }

# render TEMPLATE: substitute every ${TOKEN} from the environment, refuse leftovers.
render() {
	local out
	out=$(sed \
		-e "s|\${ACCOUNT}|$ACCOUNT|g" \
		-e "s|\${REGION}|$REGION|g" \
		-e "s|\${KMS_KEY_ARN}|$KMS_KEY_ARN|g" \
		-e "s|\${RUN_SECRET_ARN_PATTERN}|$RUN_SECRET_ARN_PATTERN|g" \
		-e "s|\${WRITER_SECRET_ARN_PATTERN}|$WRITER_SECRET_ARN_PATTERN|g" \
		-e "s|\${AUTH_EXPIRES_AT}|$AUTH_EXPIRES_AT|g" \
		-e "s|\${CLEANUP_EXPIRES_AT}|$CLEANUP_EXPIRES_AT|g" \
		"$HERE/$1")
	# shellcheck disable=SC2016 # the literal token marker is intended
	[[ $out != *'${'* ]] || die "unresolved token left in $1"
	jq -c . <<<"$out" || die "rendered $1 is not valid JSON"
}

# attach USER POLICY_NAME TEMPLATE: create a customer-managed policy and attach it.
attach() {
	local doc arn
	doc=$(render "$3")
	# Managed, not inline: IAM caps a user's inline policies at 2048 characters
	# in total, and the rendered writer policy alone exceeds that.
	arn=$(aws_ iam create-policy --policy-name "$1-$2" --policy-document "$doc" --query Policy.Arn --output text)
	aws_ iam attach-user-policy --user-name "$1" --policy-arn "$arn"
}

parse_global() {
	while (($#)); do
		case $1 in
		--endpoint-url)
			[[ $# -ge 2 ]] || die "--endpoint-url needs a value"
			emulator_endpoint "$2" || die "--endpoint-url must be an emulator on localhost, 127.0.0.0/8, ::1 or an RFC1918 address: $2"
			AWS_EXTRA+=(--endpoint-url "$2")
			DRY_RUN=1
			shift 2
			;;
		--ca-bundle)
			[[ $# -ge 2 ]] || die "--ca-bundle needs a value"
			AWS_EXTRA+=(--ca-bundle "$2")
			CA_BUNDLE=1
			shift 2
			;;
		*)
			REST+=("$1")
			shift
			;;
		esac
	done
}

banner() {
	if ((DRY_RUN)); then
		printf '*** DRY RUN against an emulator, not AWS ***\n' >&2
	fi
}

# provision and teardown run as the account root user (with MFA); resolves ACCOUNT.
require_root() {
	local arn
	arn=$(aws_ sts get-caller-identity --query Arn --output text)
	ACCOUNT=$(cut -d: -f5 <<<"$arn")
	[[ $ACCOUNT =~ ^[0-9]{12}$ ]] || die "could not resolve the account"
	((DRY_RUN)) || [[ $arn == "arn:aws:iam::$ACCOUNT:root" ]] || die "run as the account root user, not $arn"
}

# list_run_secrets PREFIX: ARN and deletion date of every secret whose name starts
# with PREFIX, including secrets already scheduled for deletion.
list_run_secrets() {
	aws_ secretsmanager list-secrets --include-planned-deletion --filters "Key=name,Values=$1" \
		--query 'SecretList[].[ARN,Name,DeletedDate]' --output text
}

# Resolve a key id from the alias; empty when the alias is gone.
# A failed list returns non-zero (command substitution does not inherit set -e).
alias_key() {
	local id
	id=$(aws_ kms list-aliases --query "Aliases[?AliasName=='alias/hikyo-smoke-$1'].TargetKeyId | [0]" --output text) || return 1
	[[ $id == None ]] || printf '%s' "$id"
}

cmd_check() {
	banner
	local keys id
	keys=$(aws_ kms list-keys --query 'Keys[].KeyId' --output text)
	printf 'Customer KMS keys in %s:\n' "$REGION"
	local found=0
	for id in $keys; do
		local meta
		meta=$(aws_ kms describe-key --key-id "$id" --query KeyMetadata)
		[[ $(jq -r .KeyManager <<<"$meta") == CUSTOMER ]] || continue
		found=1
		local state aliases mark=''
		state=$(jq -r .KeyState <<<"$meta")
		[[ $state == PendingDeletion ]] || mark='  REVIEW'
		aliases=$(aws_ kms list-aliases --key-id "$id" --query 'Aliases[].AliasName' --output text)
		printf '%s state=%s deletion=%s desc=%s aliases=%s%s\n' "$id" "$state" \
			"$(jq -r '.DeletionDate // "-"' <<<"$meta")" "$(jq -r '.Description // "-"' <<<"$meta")" "${aliases:--}" "$mark"
	done
	((found)) || printf '(none)\n'
	local secrets
	secrets=$(list_run_secrets hikyo-e2e-)
	printf 'Leftover hikyo-e2e-* secrets (REVIEW, teardown removes its own run):\n%s\n' "${secrets:-(none)}"
	printf 'Leftover hikyo-smoke-* IAM users:\n'
	local users
	users=$(aws_ iam list-users --query "Users[?starts_with(UserName,'hikyo-smoke-')].UserName" --output text)
	printf '%s\n' "${users:-(none)}"
	local pols
	pols=$(aws_ iam list-policies --scope Local --query "Policies[?starts_with(PolicyName,'hikyo-smoke-')].PolicyName" --output text)
	printf 'Leftover hikyo-smoke-* managed policies:\n%s\n' "${pols:-(none)}"
	if user_exists hikyo-158-setup; then
		printf 'Historical IAM user hikyo-158-setup: EXISTS  REVIEW\n'
	else
		printf 'Historical IAM user hikyo-158-setup: absent\n'
	fi
}

# mint_descriptor USER ACCESS_KEY SECRET SECONDS: GetSessionToken with the user's
# long-term key (retried for IAM propagation), verify the session's principal is
# the user ARN, print the one-line descriptor. Secrets reach aws and jq only
# through the environment of that one command, never argv.
mint_descriptor() {
	local out s_ak s_sk s_tok principal desc
	for _ in $(seq 12); do
		if out=$(AWS_ACCESS_KEY_ID=$2 AWS_SECRET_ACCESS_KEY=$3 AWS_SESSION_TOKEN='' \
			command aws --region "$REGION" --output json ${AWS_EXTRA[@]+"${AWS_EXTRA[@]}"} \
			sts get-session-token --duration-seconds "$4" 2>/dev/null); then
			break
		fi
		out=''
		sleep 5
	done
	[[ -n $out ]] || die "no session token for $1 after 12 attempts (IAM propagation?)"
	s_ak=$(jq -r .Credentials.AccessKeyId <<<"$out")
	s_sk=$(jq -r .Credentials.SecretAccessKey <<<"$out")
	s_tok=$(jq -r .Credentials.SessionToken <<<"$out")
	principal=$(AWS_ACCESS_KEY_ID=$s_ak AWS_SECRET_ACCESS_KEY=$s_sk AWS_SESSION_TOKEN=$s_tok \
		command aws --region "$REGION" --output text ${AWS_EXTRA[@]+"${AWS_EXTRA[@]}"} sts get-caller-identity --query Arn)
	[[ $principal == "arn:aws:iam::$ACCOUNT:user/$1" ]] || die "session principal for $1 is not its user ARN"
	desc=$(S_AK=$s_ak S_SK=$s_sk S_TOK=$s_tok REGION=$REGION jq -cn \
		'{mode:"static",region:$ENV.REGION,access_key_id:$ENV.S_AK,secret_access_key:$ENV.S_SK,session_token:$ENV.S_TOK}')
	((${#desc} <= 4096)) || die "descriptor for $1 exceeds 4096 bytes"
	printf '%s' "$desc"
}

cmd_provision() {
	local run=${1:-}
	# od reads exactly 5 bytes, so no pipe stage dies of SIGPIPE under pipefail.
	[[ -n $run ]] || run=$(od -An -N5 -tx1 /dev/urandom | tr -d ' \n')
	valid_run "$run"
	banner
	require_root
	RUN_ID=$run
	AUTH_EXPIRES_AT=$(stamp 7200)
	CLEANUP_EXPIRES_AT=$(stamp 10800)
	RUN_SECRET_ARN_PATTERN="arn:aws:secretsmanager:$REGION:$ACCOUNT:secret:hikyo-e2e-$run*"
	WRITER_SECRET_ARN_PATTERN="arn:aws:secretsmanager:$REGION:$ACCOUNT:secret:hikyo-e2e-$run*/static/*"

	local existing
	existing=$(alias_key "$run")
	[[ -z $existing ]] || die "alias/hikyo-smoke-$run already exists"
	# Recovery first: from here on a failure can leave resources behind. EXIT
	# also covers die and Ctrl-C; ERR needs errtrace to reach into functions.
	printf 'RUN=%s. If provision fails or you abandon it, remove everything with:\n  %s\n' "$run" "$(teardown_cmd "$run")" >&2
	set -E
	trap recovery_notice ERR
	trap '(($? == 0)) || recovery_notice' EXIT
	KMS_KEY_ARN=$(aws_ kms create-key \
		--description "hikyo #158 smoke $run" \
		--tags "TagKey=hikyo-smoke,TagValue=$run" \
		--query KeyMetadata.Arn --output text)
	aws_ kms create-alias --alias-name "alias/hikyo-smoke-$run" --target-key-id "$KMS_KEY_ARN" >/dev/null

	local actor user ak_json ak sk d
	local d_static d_expired d_oracle d_cleanup
	for actor in writer oracle cleanup; do
		user=hikyo-smoke-$run-$actor
		aws_ iam create-user --user-name "$user" --tags "Key=hikyo-smoke,Value=$run" >/dev/null
		case $actor in
		writer)
			attach "$user" writer writer.policy.json
			attach "$user" static-lifecycle static-lifecycle.policy.json
			;;
		oracle) attach "$user" oracle oracle.policy.json ;;
		cleanup) attach "$user" cleanup cleanup.policy.json ;;
		esac
		ak_json=$(aws_ iam create-access-key --user-name "$user")
		ak=$(jq -r .AccessKey.AccessKeyId <<<"$ak_json")
		sk=$(jq -r .AccessKey.SecretAccessKey <<<"$ak_json")
		ak_json=''
		d=$(mint_descriptor "$user" "$ak" "$sk" 10800)
		case $actor in
		writer) d_static=$d ;;
		oracle) d_oracle=$d ;;
		cleanup) d_cleanup=$d ;;
		esac
		d=''

		# The lifecycle fence needs an authentic expired static session: the
		# shortest STS allows is 900 s, so it is usable only after 15 minutes.
		if [[ $actor == writer ]]; then
			d_expired=$(mint_descriptor "$user" "$ak" "$sk" 900)
		fi
		ak=''
		sk=''
	done

	printf '===== manifest.json (nonsecret) =====\n'
	jq -cn --arg account "$ACCOUNT" --arg region "$REGION" --arg run "$run" --arg key "$KMS_KEY_ARN" \
		--arg exp "$AUTH_EXPIRES_AT" --arg p "arn:aws:iam::$ACCOUNT:user/hikyo-smoke-$run-" \
		'{account:$account,region:$region,authorization_id:("owner-smoke-"+$run),budget_usd:1,prefix:("hikyo-e2e-"+$run+"/"),
		  kms_key_arn:$key,expires_at:$exp,
		  writers:{static:{credential_file:"static.json",expired_credential_file:"static-expired.json",principal_arn:($p+"writer")}},
		  oracle:{credential_file:"oracle.json",principal_arn:($p+"oracle")},
		  cleanup:{credential_file:"cleanup.json",principal_arn:($p+"cleanup")}}'
	printf '===== end manifest.json =====\n'
	local file
	for file in static static-expired oracle cleanup; do
		printf '===== %s.json (SECRET, paste with cat > %s.json) =====\n' "$file" "$file"
		case $file in
		static) printf '%s\n' "$d_static" ;;
		static-expired) printf '%s\n' "$d_expired" ;;
		oracle) printf '%s\n' "$d_oracle" ;;
		cleanup) printf '%s\n' "$d_cleanup" ;;
		esac
		printf '===== end %s.json =====\n' "$file"
	done
	printf 'static-expired.json expires 15 minutes after provision; start the smoke only after that, and before %s.\n' "$AUTH_EXPIRES_AT"
	printf 'Sessions expire at %s (writer/oracle window) and %s (cleanup window).\n' "$AUTH_EXPIRES_AT" "$CLEANUP_EXPIRES_AT"
	printf 'Teardown as soon as the smoke has finished (the long-term access keys stay active until then):\n  %s\n' "$(teardown_cmd "$run")"
}

tolerate_missing() {
	local out
	if ! out=$("$@" 2>&1); then
		[[ $out == *NoSuchEntity* || $out == *NotFound* ]] || die "$out"
	fi
}

cmd_teardown() {
	local run=${1:-}
	[[ -n $run ]] || die "teardown needs RUN"
	valid_run "$run"
	banner
	require_root
	# Order: revoke (deactivate keys, detach policies), sweep secrets, delete keys,
	# users and run policies, then the alias and the KMS key. Each listing is
	# captured on its own line so a failed list aborts instead of reading as empty.
	local actor user id pol keys pols present=()
	for actor in writer oracle cleanup; do
		user=hikyo-smoke-$run-$actor
		user_exists "$user" || continue
		present+=("$user")
		keys=$(aws_ iam list-access-keys --user-name "$user" --query 'AccessKeyMetadata[].AccessKeyId' --output text)
		for id in $keys; do
			aws_ iam update-access-key --user-name "$user" --access-key-id "$id" --status Inactive
		done
		pols=$(aws_ iam list-attached-user-policies --user-name "$user" --query 'AttachedPolicies[].PolicyArn' --output text)
		# Detach everything; only hikyo-smoke-RUN-* policies are deleted, below.
		for pol in $pols; do
			aws_ iam detach-user-policy --user-name "$user" --policy-arn "$pol"
		done
		pols=$(aws_ iam list-user-policies --user-name "$user" --query 'PolicyNames[]' --output text)
		for pol in $pols; do
			aws_ iam delete-user-policy --user-name "$user" --policy-name "$pol"
		done
	done
	# Secrets left by an interrupted smoke (a test timeout skips cleanup). Delete
	# every name starting hikyo-e2e-RUN: that is the pattern the IAM policies
	# grant (hikyo-e2e-RUN*), so it covers the fixture prefix hikyo-e2e-RUN/ and
	# the rerun prefixes hikyo-e2e-RUNrN/, and the fixed-length RUN keeps it off
	# other runs. A narrower regex would leave behind what the policies allow.
	# Restore a pending deletion first so the force delete is immediate.
	local secrets arn name deleted
	secrets=$(list_run_secrets "hikyo-e2e-$run")
	while read -r arn name deleted; do
		[[ -n $arn ]] || continue
		[[ $name == "hikyo-e2e-$run"* ]] || continue
		[[ $deleted == None ]] || aws_ secretsmanager restore-secret --secret-id "$arn" >/dev/null
		aws_ secretsmanager delete-secret --secret-id "$arn" --force-delete-without-recovery >/dev/null
		printf 'secret %s force-deleted\n' "$name"
	done <<<"$secrets"
	for user in ${present[@]+"${present[@]}"}; do
		keys=$(aws_ iam list-access-keys --user-name "$user" --query 'AccessKeyMetadata[].AccessKeyId' --output text)
		for id in $keys; do
			tolerate_missing aws_ iam delete-access-key --user-name "$user" --access-key-id "$id"
		done
		tolerate_missing aws_ iam delete-user --user-name "$user"
		printf 'user %s removed\n' "$user"
	done
	# Run policies (detached above, or never attached by a partial provision).
	# Customer-managed and named hikyo-smoke-RUN-* only; nothing else is deleted.
	pols=$(aws_ iam list-policies --scope Local --query "Policies[?starts_with(PolicyName,'hikyo-smoke-$run-')].Arn" --output text)
	for pol in $pols; do
		tolerate_missing aws_ iam delete-policy --policy-arn "$pol"
	done
	local existing
	existing=$(alias_key "$run")
	if [[ -n $existing ]]; then
		aws_ kms delete-alias --alias-name "alias/hikyo-smoke-$run" >/dev/null
	fi
	# Find the key by its tag, so a key created before its alias is found too.
	local key meta tag found=0
	keys=$(aws_ kms list-keys --query 'Keys[].KeyId' --output text)
	for key in $keys; do
		meta=$(aws_ kms describe-key --key-id "$key" --query 'KeyMetadata.[KeyManager,KeyState]' --output text)
		[[ $meta =~ ^CUSTOMER[[:space:]]+(Enabled|Disabled)$ ]] || continue
		tag=$(aws_ kms list-resource-tags --key-id "$key" --query "Tags[?TagKey=='hikyo-smoke'].TagValue | [0]" --output text)
		[[ $tag == "$run" ]] || continue
		aws_ kms schedule-key-deletion --key-id "$key" --pending-window-in-days 7 >/dev/null
		aws_ kms describe-key --key-id "$key" --query 'KeyMetadata.[KeyId,KeyState,DeletionDate]' --output text
		found=1
	done
	((found)) || printf 'no live key tagged hikyo-smoke=%s; already handled\n' "$run"
}

REST=()
parse_global "$@"
set -- ${REST[@]+"${REST[@]}"}
((!CA_BUNDLE || DRY_RUN)) || die "--ca-bundle needs --endpoint-url"
need
[[ $# -ge 1 ]] || die "usage: smoke-sandbox.sh [--endpoint-url URL --ca-bundle FILE] check | provision [RUN] | teardown RUN"
sub=$1
shift
case $sub in
check) cmd_check ;;
provision) cmd_provision "$@" ;;
teardown) cmd_teardown "$@" ;;
*) die "unknown subcommand: $sub" ;;
esac
