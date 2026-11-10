#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
repository=${HIKYO_REPOSITORY:-Hikyo-Org/Hikyo}
state_root=${HIKYO_RELEASE_STATE_DIR:-"$HOME/Library/Application Support/hikyo/stable-releases"}
trust_state=${HIKYO_RELEASE_TRUST_STATE:-"${XDG_STATE_HOME:-$HOME/Library/Application Support}/hikyo/release-trust.json"}
fail() { printf 'stable ceremony: %s\n' "$*" >&2; exit 1; }
confirm() {
	printf 'Type exactly "%s" to continue: ' "$1"
	IFS= read -r response
	[ "$response" = "$1" ] || fail 'confirmation did not match'
}
clean_main() {
	[ -z "$(git status --porcelain)" ] || fail 'use a clean release checkout'
	git fetch origin main
	[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] || fail 'checkout must match current origin/main'
}
pr_branch_exists() {
	git show-ref --verify --quiet "refs/heads/$1" && return 0
	remote_branch=$(git ls-remote --heads origin "refs/heads/$1") || fail 'cannot inspect existing trust branch'
	[ -n "$remote_branch" ]
}
signed_pr() (
	branch=$1 title=$2 proposal_dir=$3
	shift 3
	[ "$(git config --get commit.gpgsign)" = true ] || fail 'commit.gpgsign=true is required'
	unchanged=true
	for name do
		cmp -s "$proposal_dir/$name" "release/trust/$name" || unchanged=false
	done
	if [ "$unchanged" = true ]; then printf 'Trust evidence already matches main.\n'; return 0; fi
	# Proposal bytes are already verified. Keep the caller checkout untouched,
	# including on signing, push, GitHub verification, or PR creation failures.
	mkdir -p "$state_root"
	pr_state=$(mktemp -d "$state_root/pr.XXXXXX")
	pr_worktree="$pr_state/worktree"
	# Invoked by the exit/signal trap within this subshell.
	# shellcheck disable=SC2329
	cleanup_pr() {
		if [ -d "$pr_worktree" ]; then
			if ! (cd "$repo_root" && git worktree remove "$pr_worktree"); then
				printf 'Preserved unfinished trust worktree for inspection: %s\n' "$pr_worktree" >&2
			fi
		fi
	}
	trap 'cleanup_pr' EXIT HUP INT TERM
	remote_branch=$(git ls-remote --heads origin "refs/heads/$branch")
	if [ -n "$remote_branch" ]; then
		git fetch origin "refs/heads/$branch:refs/remotes/origin/$branch"
	fi
	if ! git show-ref --verify --quiet "refs/heads/$branch" && [ -n "$remote_branch" ]; then
		git branch "$branch" "refs/remotes/origin/$branch"
	fi
	if git show-ref --verify --quiet "refs/heads/$branch"; then
		if [ -n "$remote_branch" ]; then
			[ "$(git rev-parse "$branch")" = "$(git rev-parse "refs/remotes/origin/$branch")" ] || fail 'local and remote trust branch differ; inspect them before retrying'
		fi
		base=$(git merge-base origin/main "$branch")
		git diff --name-only "$base" "$branch" >"$pr_state/changed"
		for name do printf 'release/trust/%s\n' "$name"; done >"$pr_state/allowed"
		while IFS= read -r changed; do
			grep -Fx "$changed" "$pr_state/allowed" >/dev/null || fail 'existing trust branch contains unrelated changes'
		done <"$pr_state/changed"
		git worktree add --detach "$pr_worktree" "$branch"
		for name do
			# setup retries can reuse the existing recovery signature only after
			# preflight authenticates it against the exact regenerated policy.
			[ -f "$proposal_dir/$name" ] || {
				[ "$name" = stable-policy.sigstore.json ] || fail 'proposal file missing'
				continue
			}
			cmp -s "$proposal_dir/$name" "$pr_worktree/release/trust/$name" || fail 'existing trust branch differs from verified proposal; no files were overwritten'
		done
	else
		git worktree add -b "$branch" "$pr_worktree" origin/main
		for name do cp "$proposal_dir/$name" "$pr_worktree/release/trust/$name"; done
	fi
	cd "$pr_worktree"
	go run ./scripts/release/stable preflight --trust release/trust
	if [ -n "$(git status --porcelain)" ]; then
		for name do git add -- "release/trust/$name"; done
		git commit -s -m "$title"
	fi
	"$script_dir/../git/install-hooks.sh"
	"$script_dir/../ci/check-commit-signatures.sh" origin/main HEAD
	git push origin "HEAD:refs/heads/$branch"
	commit=$(git rev-parse HEAD)
	verified=$(gh api "repos/$repository/commits/$commit" --jq '.commit.verification.verified')
	[ "$verified" = true ] || fail 'GitHub did not verify the pushed commit signature'
	prs=$(gh pr list --repo "$repository" --base main --head "$branch" --state open --json url)
	count=$(printf '%s\n' "$prs" | jq -er 'length')
	case "$count" in
		0) gh pr create --repo "$repository" --base main --head "$branch" --title "$title" \
			--body 'Public release trust evidence prepared and verified by the online ceremony. Review the exact policy or metadata changes and merge only after CI passes.' ;;
		1) printf '%s\n' "$prs" | jq -r '.[0].url' ;;
		*) fail 'multiple open PRs match the trust branch' ;;
	esac
)
download_verify() {
	version=$1
	is_semver "$version" || fail 'strict release version required'
	case "$version" in *-nightly.*) fail 'nightly versions use their own release process' ;; esac
	mkdir -p "$state_root"
	download=$(mktemp -d "$state_root/$version.XXXXXX")
	gh release download "v$version" --repo "$repository" --dir "$download"
	commit=$(jq -er '.source_commit' "$download/release-manifest.json")
	go run ./scripts/release/stable verify --trust release/trust --directory "$download" \
		--version "$version" --commit "$commit" --state "$trust_state" --latest --published
	manifest_sha=$(sha256_file "$download/release-manifest.json")
}

cd "$repo_root"
case "${1:-help}" in
	help|--help|-h)
		printf '%s\n' \
			'Online stable release ceremony (no USB media or network disconnection).' \
			'  status                 Verify one-time policy activation' \
			'  setup                  Authorize stable CI once using local recovery custody; open PR' \
			'  build VERSION          Push a signed tag from green main; CI stages a signed draft' \
			'  review VERSION         Download and verify the draft; print its review hash' \
			'  publish VERSION SHA256 Publish the reviewed draft using your local GitHub login' \
			'  sync-trust VERSION     Verify public release and open the trust-update PR' \
			'  homebrew VERSION       Verify public release and open the stable Homebrew tap PR' \
			'  --offline              Explicit legacy removable-media ceremony'
		;;
	status)
		[ "$#" -eq 1 ] || fail 'status takes no arguments'
		go run ./scripts/release/stable preflight --trust release/trust
		;;
	setup)
		[ "$#" -eq 1 ] || fail 'setup takes no arguments'
		clean_main
		[ ! -e release/trust/stable-policy.json ] || fail 'policy already exists; rotation needs a reviewed policy change'
		mkdir -p "$state_root"
		proposal=$(mktemp -d "$state_root/policy.XXXXXX")
		go run ./scripts/release/stable policy --trust release/trust --out "$proposal/stable-policy.json"
		cp release/trust/nightly/trusted-root.json "$proposal/stable-trusted-root.json"
		if pr_branch_exists release/authorize-stable-workflow; then
			signed_pr release/authorize-stable-workflow 'chore(release): authorize keyless stable workflow' "$proposal" \
				stable-policy.json stable-policy.sigstore.json stable-trusted-root.json
			exit 0
		fi
		printf 'Review the proposed delegation: %s\n' "$proposal/stable-policy.json"
		cat "$proposal/stable-policy.json"
		confirm 'authorize stable release workflow'
		signer=${HIKYO_RECOVERY_SIGNER:-"$HOME/Library/Application Support/hikyo/nightly-bootstrap/tools/key-custody"}
		[ -x "$signer" ] || fail 'local recovery Keychain signer is unavailable'
		# Disable core dumps before invoking the local helper. Private key bytes
		# and passphrases never enter this shell or the public proposal directory.
		# shellcheck disable=SC3045
		ulimit -c 0
		"$signer" sign recovery-1 "$proposal/stable-policy.json" "$proposal/stable-policy.sigstore.json"
		cp -R release/trust "$proposal/trust"
		for name in stable-policy.json stable-policy.sigstore.json stable-trusted-root.json; do cp "$proposal/$name" "$proposal/trust/$name"; done
		go run ./scripts/release/stable preflight --trust "$proposal/trust"
		signed_pr "release/authorize-stable-workflow" 'chore(release): authorize keyless stable workflow' "$proposal" \
			stable-policy.json stable-policy.sigstore.json stable-trusted-root.json
		;;
	build)
		[ "$#" -eq 2 ] || fail 'usage: ceremony.sh build VERSION'
		version=$2
		clean_main
		go run ./scripts/release/stable preflight --trust release/trust
		"$script_dir/require-synced-trust.sh" "$repository" release/trust/metadata.json
		commit=$(git rev-parse HEAD)
		"$script_dir/require-green-main.sh" "$repository" "$commit"
		mkdir -p "$state_root"
		candidate_dir=$(mktemp -d "$state_root/candidate.XXXXXX")
		go run ./scripts/release/stable candidate --trust release/trust --version "$version" --commit "$commit" --out "$candidate_dir/release-candidate.json"
		git show-ref --verify --quiet "refs/tags/v$version" && fail 'local tag already exists; inspect its existing draft'
		remote=$(git ls-remote --tags origin "refs/tags/v$version")
		[ -z "$remote" ] || fail 'remote tag already exists; inspect its existing draft'
		confirm "build v$version at $commit"
		git tag -s "v$version" "$commit" -m "Hikyo $version"
		"$script_dir/../git/install-hooks.sh"
		"$script_dir/../ci/check-commit-signatures.sh" origin/main HEAD
		git push origin "refs/tags/v$version"
		printf 'CI is building the signed draft. Once green, run: scripts/release/ceremony.sh review %s\n' "$version"
		;;
	review)
		[ "$#" -eq 2 ] || fail 'usage: ceremony.sh review VERSION'
		download_verify "$2"
		printf 'Verified draft directory: %s\nManifest SHA-256: %s\n' "$download" "$manifest_sha"
		printf 'Replace the draft placeholder with release notes on GitHub, review them and the acceptance evidence, then run:\n  scripts/release/ceremony.sh publish %s %s\n' "$2" "$manifest_sha"
		;;
	publish)
		[ "$#" -eq 3 ] || fail 'usage: ceremony.sh publish VERSION REVIEWED_MANIFEST_SHA256'
		version=$2 reviewed=$3
		printf '%s\n' "$reviewed" | grep -Eq '^[0-9a-f]{64}$' || fail 'reviewed SHA-256 required'
		clean_main
		download_verify "$version"
		[ "$manifest_sha" = "$reviewed" ] || fail 'draft changed since review'
		confirm "publish v$version $reviewed"
		HIKYO_RELEASE_TRUST_STATE="$trust_state" "$script_dir/publish-stable-draft.sh" \
			"$repository" "v$version" "$reviewed" release/trust
		printf 'Public release verified. Next, run:\n  scripts/release/ceremony.sh sync-trust %s\n' "$version"
		;;
	sync-trust)
		[ "$#" -eq 2 ] || fail 'usage: ceremony.sh sync-trust VERSION'
		version=$2
		clean_main
		is_draft=$(gh release view "v$version" --repo "$repository" --json isDraft --jq '.isDraft')
		[ "$is_draft" = false ] || fail 'release is not public'
		download_verify "$version"
		signed_pr "release/sync-trust-$version" "chore(release): record published $version trust" "$download" \
			metadata.json metadata.sigstore.json catalog.json catalog.sigstore.json
		printf 'Merge the trust PR after review and green CI before building another release.\n'
		;;
	homebrew)
		[ "$#" -eq 2 ] || fail 'usage: ceremony.sh homebrew VERSION'
		version=$2
		is_draft=$(gh release view "v$version" --repo "$repository" --json isDraft --jq '.isDraft')
		[ "$is_draft" = false ] || fail 'release is not public'
		download_verify "$version"
		tap_repository=${HIKYO_HOMEBREW_TAP_REPOSITORY:-Hikyo-Org/homebrew-tap}
		confirm "open Homebrew PR for v$version in $tap_repository"
		"$script_dir/publish-homebrew-cask.sh" "$repository" "v$version" "$download" \
			"$tap_repository"
		;;
	*) fail 'unknown command; run ceremony.sh --help' ;;
esac
