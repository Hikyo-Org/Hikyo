#!/bin/sh
set -eu

repo=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-signature-fixture.XXXXXX")
trap 'rm -rf "$repo"' EXIT HUP INT TERM
zero=0000000000000000000000000000000000000000

expect_refusal() {
	expected=$1
	shift
	if "$@" >"$repo/refusal-output" 2>&1; then
		printf 'signature fixture failed: command accepted: %s\n' "$*" >&2
		exit 1
	fi
	if ! grep -F "$expected" "$repo/refusal-output" >/dev/null; then
		cat "$repo/refusal-output" >&2
		printf 'signature fixture failed: expected refusal: %s\n' "$expected" >&2
		exit 1
	fi
}

git -C "$repo" init -q
git -C "$repo" config user.name 'Fixture Author'
git -C "$repo" config user.email 'fixture@example.com'
git -C "$repo" config commit.gpgsign false

printf 'base\n' >"$repo/file"
git -C "$repo" add file
git -C "$repo" commit -q -m base
base=$(git -C "$repo" rev-parse HEAD)

ssh-keygen -q -t ed25519 -N '' -f "$repo/signing-key"
printf 'fixture@example.com %s\n' "$(cat "$repo/signing-key.pub")" >"$repo/allowed-signers"
git -C "$repo" config gpg.format ssh
git -C "$repo" config user.signingkey "$repo/signing-key"
git -C "$repo" config gpg.ssh.allowedSignersFile "$repo/allowed-signers"
git -C "$repo" config commit.gpgsign true

mkdir -p "$repo/.githooks" "$repo/scripts/ci" "$repo/scripts/git"
cp "$(dirname "$0")/check-commit-signatures.sh" "$repo/scripts/ci/"
cp "$(dirname "$0")/../../.githooks/pre-push" "$repo/.githooks/"
cp "$(dirname "$0")/../git/install-hooks.sh" "$repo/scripts/git/"
chmod +x "$repo/.githooks/pre-push" "$repo/scripts/ci/check-commit-signatures.sh" "$repo/scripts/git/install-hooks.sh"
(cd "$repo" && scripts/git/install-hooks.sh >/dev/null)
[ "$(git -C "$repo" config core.hooksPath)" = .githooks ] || {
	printf 'signature fixture failed: pre-push hook was not installed\n' >&2
	exit 1
}

printf 'signed\n' >>"$repo/file"
git -C "$repo" commit -q -am signed
signed=$(git -C "$repo" rev-parse HEAD)
"$(dirname "$0")/check-commit-signatures.sh" "$base" "$signed" "$repo"
git -C "$repo" update-ref refs/remotes/hikyo-upstream/main "$base"
git -C "$repo" update-ref refs/remotes/origin/main "$base"
printf 'refs/heads/signed %s refs/heads/signed %s\n' "$signed" "$zero" >"$repo/push-input"
git -C "$repo" hook run --to-stdin="$repo/push-input" pre-push -- origin unused

primary=$(git -C "$repo" branch --show-current)
# Reproduce an ordinary branch fast-forward when canonical main already equals
# the signed head. Use the installed hook through an actual push to a local bare
# remote, seeded at the older base, rather than bypassing it with a raw SHA push.
git init -q --bare "$repo/remote.git"
git -C "$repo/remote.git" fetch -q "$repo" "$base:refs/heads/qualification"
git -C "$repo" update-ref refs/remotes/hikyo-upstream/main "$signed"
"$(dirname "$0")/check-commit-signatures.sh" "$signed" "$signed" "$repo"
git -C "$repo" push -q "$repo/remote.git" "refs/heads/$primary:refs/heads/qualification"
[ "$(git -C "$repo/remote.git" rev-parse refs/heads/qualification)" = "$signed" ] || {
	printf 'signature fixture failed: ordinary branch fast-forward did not land\n' >&2
	exit 1
}

# An ancestor also introduces no commits, but its requested tip must be trusted.
descendant=$(printf 'descendant\n' | git -C "$repo" commit-tree -S -p "$signed" "$signed^{tree}")
"$(dirname "$0")/check-commit-signatures.sh" "$descendant" "$signed" "$repo"
git -C "$repo" update-ref refs/remotes/hikyo-upstream/main "$descendant"
git -C "$repo" hook run --to-stdin="$repo/push-input" pre-push -- origin unused
expect_refusal 'not locally verified (status N)' "$(dirname "$0")/check-commit-signatures.sh" "$signed" "$base" "$repo"
git -C "$repo" update-ref refs/remotes/hikyo-upstream/main "$base"

expect_refusal 'invalid base' "$(dirname "$0")/check-commit-signatures.sh" missing-base "$signed" "$repo"
expect_refusal 'invalid head' "$(dirname "$0")/check-commit-signatures.sh" "$base" missing-head "$repo"
blob=$(git -C "$repo" rev-parse "$signed:file")
expect_refusal 'invalid base' "$(dirname "$0")/check-commit-signatures.sh" "$blob" "$signed" "$repo"
expect_refusal 'invalid head' "$(dirname "$0")/check-commit-signatures.sh" "$base" "$blob" "$repo"
git -C "$repo" update-ref refs/remotes/hikyo-upstream/main "$blob"
expect_refusal 'missing refs/remotes/hikyo-upstream/main' git -C "$repo" hook run --to-stdin="$repo/push-input" pre-push -- origin unused
git -C "$repo" update-ref refs/remotes/hikyo-upstream/main "$base"
# A missing nonzero object is a malformed push target, not a branch deletion.
printf 'refs/heads/signed %s refs/heads/signed %s\n' 1111111111111111111111111111111111111111 "$zero" >"$repo/push-input"
expect_refusal 'invalid head' git -C "$repo" hook run --to-stdin="$repo/push-input" pre-push -- origin unused

disjoint=$(printf 'disjoint\n' | git -C "$repo" commit-tree -S "$signed^{tree}")
expect_refusal 'no merge base' "$(dirname "$0")/check-commit-signatures.sh" "$base" "$disjoint" "$repo"
printf 'refs/heads/disjoint %s refs/heads/disjoint %s\n' "$disjoint" "$zero" >"$repo/push-input"
expect_refusal 'no merge base' git -C "$repo" hook run --to-stdin="$repo/push-input" pre-push -- origin unused

# Retain the signature while changing the signed message, producing an actual
# bad signature rather than merely removing local trust from a good signature.
git -C "$repo" cat-file commit "$signed" >"$repo/original-commit"
sed '$s/signed/tampered/' "$repo/original-commit" >"$repo/tampered-commit"
tampered=$(git -C "$repo" hash-object -t commit -w --stdin <"$repo/tampered-commit")
expect_refusal 'not locally verified (status B)' "$(dirname "$0")/check-commit-signatures.sh" "$base" "$tampered" "$repo"
expect_refusal 'not locally verified (status B)' "$(dirname "$0")/check-commit-signatures.sh" "$tampered" "$tampered" "$repo"
printf 'refs/heads/tampered %s refs/heads/tampered %s\n' "$tampered" "$zero" >"$repo/push-input"
expect_refusal 'not locally verified (status B)' git -C "$repo" hook run --to-stdin="$repo/push-input" pre-push -- origin unused

git -C "$repo" switch -q -c merge-side "$base"
printf 'side\n' >"$repo/side"
git -C "$repo" add side
git -C "$repo" commit -q -m side
git -C "$repo" switch -q "$primary"
git -C "$repo" -c commit.gpgsign=false merge -q --no-ff --no-edit merge-side
unsigned_merge=$(git -C "$repo" rev-parse HEAD)
if "$(dirname "$0")/check-commit-signatures.sh" "$base" "$unsigned_merge" "$repo" >/dev/null 2>&1; then
	printf 'signature fixture failed: unsigned merge commit accepted\n' >&2
	exit 1
fi
printf 'refs/heads/merge %s refs/heads/merge %s\n' "$unsigned_merge" "$zero" >"$repo/push-input"
expect_refusal 'not locally verified (status N)' git -C "$repo" hook run --to-stdin="$repo/push-input" pre-push -- origin unused

git -C "$repo" switch -q -c unsigned-case "$signed"
printf 'unsigned\n' >>"$repo/file"
git -C "$repo" -c commit.gpgsign=false commit -q -am unsigned
unsigned=$(git -C "$repo" rev-parse HEAD)
if "$(dirname "$0")/check-commit-signatures.sh" "$signed" "$unsigned" "$repo" >/dev/null 2>&1; then
	printf 'signature fixture failed: unsigned commit accepted\n' >&2
	exit 1
fi
expect_refusal 'not locally verified (status N)' "$(dirname "$0")/check-commit-signatures.sh" "$unsigned" "$unsigned" "$repo"
git -C "$repo" update-ref refs/remotes/hikyo-upstream/main "$unsigned"
printf 'refs/heads/unsigned-case %s refs/heads/unsigned-case %s\n' "$unsigned" "$zero" >"$repo/push-input"
if git -C "$repo" hook run --to-stdin="$repo/push-input" pre-push -- origin unused >/dev/null 2>&1; then
	printf 'signature fixture failed: pre-push hook accepted an unsigned commit\n' >&2
	exit 1
fi
git -C "$repo" update-ref refs/remotes/hikyo-upstream/main "$base"

# A normal fork fetch must not hide unsigned ancestors behind fork main.
git -C "$repo" update-ref refs/remotes/origin/main "$unsigned"
printf 'fork contribution\n' >>"$repo/file"
git -C "$repo" commit -q -am 'signed fork contribution'
fork_head=$(git -C "$repo" rev-parse HEAD)
"$(dirname "$0")/check-commit-signatures.sh" refs/remotes/origin/main "$fork_head" "$repo"
printf 'refs/heads/unsigned-case %s refs/heads/unsigned-case %s\n' "$fork_head" "$zero" >"$repo/push-input"
if git -C "$repo" hook run --to-stdin="$repo/push-input" pre-push -- origin unused >/dev/null 2>&1; then
	printf 'signature fixture failed: fork main hid an unsigned ancestor from the canonical check\n' >&2
	exit 1
fi

# The hook must not fall back to fork main when the canonical ref is absent.
git -C "$repo" update-ref -d refs/remotes/hikyo-upstream/main
if git -C "$repo" hook run --to-stdin="$repo/push-input" pre-push -- origin unused >/dev/null 2>&1; then
	printf 'signature fixture failed: missing canonical base fell back to fork main\n' >&2
	exit 1
fi

: >"$repo/allowed-signers"
if "$(dirname "$0")/check-commit-signatures.sh" "$base" "$signed" "$repo" >/dev/null 2>&1; then
	printf 'signature fixture failed: unverifiable signature accepted\n' >&2
	exit 1
fi
git -C "$repo" update-ref refs/remotes/hikyo-upstream/main "$base"
printf 'refs/heads/signed %s refs/heads/signed %s\n' "$signed" "$zero" >"$repo/push-input"
expect_refusal 'not locally verified' git -C "$repo" hook run --to-stdin="$repo/push-input" pre-push -- origin unused
expect_refusal 'not locally verified' "$(dirname "$0")/check-commit-signatures.sh" "$signed" "$signed" "$repo"
git -C "$repo" update-ref refs/remotes/hikyo-upstream/main "$signed"
printf 'refs/heads/signed %s refs/heads/signed %s\n' "$signed" "$zero" >"$repo/push-input"
expect_refusal 'not locally verified' git -C "$repo" hook run --to-stdin="$repo/push-input" pre-push -- origin unused

printf 'Signature fixture: checker and pre-push hook accept valid signatures and refuse invalid ones\n'
