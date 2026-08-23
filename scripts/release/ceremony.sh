#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"

dry_run=false
case "${1:-}" in
	'') ;;
	--dry-run) dry_run=true ;;
	*) printf 'usage: %s [--dry-run]\n' "$0" >&2; exit 2 ;;
esac

fail() {
	printf 'release ceremony: %s\n' "$*" >&2
	exit 1
}

require_command() {
	command -v "$1" >/dev/null 2>&1 || fail "required command is absent: $1"
}

confirm_exact() {
	expected=$1
	printf 'Type exactly "%s" to continue: ' "$expected"
	IFS= read -r answer
	[ "$answer" = "$expected" ] || fail 'confirmation did not match; nothing changed'
}

require_offline() {
	if route -n get default >/dev/null 2>&1; then
		fail 'network route exists; disconnect Wi-Fi, Ethernet, VPN, and tethering'
	fi
	printf 'Network check: offline\n'
}

require_online() {
	route -n get default >/dev/null 2>&1 || fail 'no network route exists; reconnect before continuing'
}

ram_device=
ram_dir=
cleanup_ram() {
	if [ -n "$ram_device" ]; then
		hdiutil detach "$ram_device" >/dev/null 2>&1 || true
		ram_device=
		ram_dir=
	fi
}
trap cleanup_ram EXIT HUP INT TERM

mount_ram() {
	sectors=$1
	[ ! -e /Volumes/HIKYO_SIGNING ] || fail '/Volumes/HIKYO_SIGNING already exists'
	# Every supported macOS signing shell provides the core-size limit.
	# shellcheck disable=SC3045
	ulimit -c 0
	# shellcheck disable=SC3045
	[ "$(ulimit -c)" = 0 ] || fail 'core dumps are not disabled'
	ram_device=$(hdiutil attach -nomount "ram://$sectors")
	diskutil eraseVolume HFS+ HIKYO_SIGNING "$ram_device" >/dev/null
	ram_dir=/Volumes/HIKYO_SIGNING
	touch "$ram_dir/.metadata_never_index"
	umask 077
}

finish_ram() {
	cleanup_ram
	trap cleanup_ram EXIT HUP INT TERM
}

prompt_file() {
	label=$1
	printf '%s: ' "$label" >&2
	IFS= read -r prompted_file
	[ -f "$prompted_file" ] || fail "file is absent: $prompted_file"
	case "$prompted_file" in
		/Volumes/*/*.key) ;;
		*) fail 'private key must be read from removable media under /Volumes' ;;
	esac
	media_info=$(diskutil info "$prompted_file")
	printf '%s\n' "$media_info" | grep -Eq \
		'Device Location:[[:space:]]*External|Removable Media:[[:space:]]*Removable' || \
		fail 'private key is not on external/removable media'
	printf '%s\n' "$prompted_file"
}

require_media_ejected() {
	private_key_path=$1
	label=$2
	printf 'Eject %s media, then continue.\n' "$label"
	confirm_exact "$label ejected $version"
	[ ! -e "$private_key_path" ] || fail "$label private-key media remains mounted"
}

state_home=${XDG_STATE_HOME:-"$HOME/Library/Application Support"}
state_root=$state_home/hikyo/release-ceremony

record_state() {
	completed_phase=$1
	[ "$dry_run" = false ] || return 0
	mkdir -p "$release_dir"
	umask 077
	state_tmp=$(mktemp "$release_dir/state.json.XXXXXX")
	jq -n \
		--arg version "$version" \
		--arg tag "$tag" \
		--arg phase "$completed_phase" \
		--arg repository "$repository" \
		'{version: $version, tag: $tag, phase: $phase, repository: $repository}' \
		>"$state_tmp"
	mv "$state_tmp" "$release_dir/state.json"
	printf 'Progress saved: %s\n' "$completed_phase"
}

validate_manifest_assets() {
	bundle=$1
	manifest=$bundle/release-manifest.json
	[ -f "$manifest" ] || fail 'release-manifest.json is absent'
	jq -e --arg version "$version" --arg tag "$tag" '
		.schema == "hikyo.dev/release-manifest/v1" and
		.version == $version and .tag == $tag and
		([.artifacts[] | select(.kind == "binary")] | length) == 6 and
		([.artifacts[] | select(.kind == "oci-payload")] | length) == 2
	' "$manifest" >/dev/null || fail 'release manifest identity or target matrix is invalid'
	asset_failure=false
	while IFS="$(printf '\t')" read -r name expected_sha; do
		[ -f "$bundle/$name" ] || { printf 'Missing asset: %s\n' "$name" >&2; asset_failure=true; continue; }
		actual_sha=$(sha256_file "$bundle/$name")
		[ "$actual_sha" = "$expected_sha" ] || {
			printf 'Hash mismatch: %s\n' "$name" >&2
			asset_failure=true
		}
	done <<EOF
$(jq -r '.artifacts[] | [.name, .sha256] | @tsv' "$manifest")
EOF
	[ "$asset_failure" = false ] || fail 'draft asset validation failed'
	printf 'Draft assets: six binaries and every manifest hash verified\n'
}

ensure_release_branch() {
	prefix=$1
	current_branch=$(git branch --show-current)
	if [ -z "$current_branch" ] || [ "$current_branch" = main ]; then
		branch_version=$(printf '%s' "$version" | tr '.+' '--')
		git switch -c "release/$prefix-$branch_version" origin/main
	fi
}

open_merge_pr() {
	title=$1
	body=$2
	git diff --check
	git add release/trust/primary-1.pub release/trust/recovery-1.pub \
		release/trust/root.json release/trust/metadata.json \
		release/trust/metadata.sigstore.json 2>/dev/null || \
		git add release/trust/metadata.json release/trust/metadata.sigstore.json
	git diff --cached --check
	git diff --cached --quiet && fail 'no release trust changes are staged'
	git commit -s -m "$title"
	git fetch origin main
	git rebase origin/main
	git push -u origin HEAD
	gh pr create --repo "$repository" --base main --head "$(git branch --show-current)" \
		--title "$title" --body "$body"
	pr_number=$(gh pr view --repo "$repository" --json number --jq .number)
	gh pr checks "$pr_number" --repo "$repository" --watch
	head_sha=$(git rev-parse HEAD)
	confirm_exact "merge PR $pr_number"
	gh pr merge "$pr_number" --repo "$repository" --squash --match-head-commit "$head_sha"
	while [ "$(gh pr view "$pr_number" --repo "$repository" --json state --jq .state)" != MERGED ]; do
		sleep 10
	done
	git fetch origin main
}

dry_run_phase() {
	case "$phase" in
		1)
			printf 'Dry run: bootstrap %s\n' "$version"
			printf '%s\n' \
				'Would require offline network check, four distinct USB volumes, and a RAM disk.' \
				'Would generate separate primary/recovery keys and copy only encrypted private keys to USB.' \
				'Would copy only public keys plus recovery-signed bootstrap metadata into Git.'
			;;
		2)
			printf 'Dry run: candidate %s\n' "$version"
			printf '%s\n' \
				'Would recovery-sign candidate metadata offline, then open a CI-gated PR.' \
				'would require exact typed confirmation: merge PR <number>'
			;;
		3)
			printf 'Dry run: tag %s\n' "$version"
			printf '%s\n' \
				'Would verify production trust and release fixtures before the immutable tag.' \
				'Would require exact-main CI success and prove tag/release non-use.' \
				"would require exact typed confirmation: tag $tag at <sha>" \
				'Would push one immutable tag, watch release CI, and verify every draft asset hash.'
			;;
		4)
			printf 'Dry run: sign %s\n' "$version"
			printf '%s\n' \
				'Would recovery-bind the exact draft manifest offline and merge signed metadata.' \
				'Would start a separate offline primary-key session and sign every artifact.'
			;;
		5)
			printf 'Dry run: publish %s\n' "$version"
			printf '%s\n' \
				'Would attach OCI signatures, upload Sigstore bundles, and verify published subjects.' \
				"would require exact typed confirmation: publish $tag"
			;;
	esac
}

phase_bootstrap() {
	for command_name in cosign diskutil hdiutil jq route shasum; do require_command "$command_name"; done
	[ ! -e "$repo_root/release/trust/root.json" ] || fail 'production trust root already exists; bootstrap is one-time only'
	printf 'Disconnect all networking, mount four distinct USB volumes, then continue.\n'
	require_offline
	printf 'Primary USB copy A mount path: '; IFS= read -r primary_a
	printf 'Primary USB copy B mount path: '; IFS= read -r primary_b
	printf 'Recovery USB copy A mount path: '; IFS= read -r recovery_a
	printf 'Recovery USB copy B mount path: '; IFS= read -r recovery_b
	[ "$primary_a" != "$primary_b" ] && [ "$primary_a" != "$recovery_a" ] && \
		[ "$primary_a" != "$recovery_b" ] && [ "$primary_b" != "$recovery_a" ] && \
		[ "$primary_b" != "$recovery_b" ] && [ "$recovery_a" != "$recovery_b" ] || \
		fail 'all four USB mount paths must be distinct'
	for volume in "$primary_a" "$primary_b" "$recovery_a" "$recovery_b"; do
		[ -d "$volume" ] || fail "USB volume is absent: $volume"
	done
	for private_copy in "$primary_a/primary-1.key" "$primary_b/primary-1.key" \
		"$recovery_a/recovery-1.key" "$recovery_b/recovery-1.key"; do
		[ ! -e "$private_copy" ] || fail "refusing to overwrite existing key copy: $private_copy"
	done
	confirm_exact "bootstrap $version"
	mount_ram 131072
	printf 'Cosign will prompt for the PRIMARY passphrase.\n'
	cosign generate-key-pair --output-key-prefix "$ram_dir/primary-1"
	printf 'Cosign will prompt for a DIFFERENT RECOVERY passphrase.\n'
	cosign generate-key-pair --output-key-prefix "$ram_dir/recovery-1"
	primary_sha=$(sha256_file "$ram_dir/primary-1.pub")
	recovery_sha=$(sha256_file "$ram_dir/recovery-1.pub")
	jq -n --arg recovery_sha "$recovery_sha" --arg primary_sha "$primary_sha" '{
		schema: "hikyo.dev/trust-root/v1",
		recovery: {id: "recovery-1", public_key: "recovery-1.pub", sha256: $recovery_sha},
		bootstrap_primary: {id: "primary-1", public_key: "primary-1.pub", sha256: $primary_sha}
	}' >"$ram_dir/root.json"
	jq -n --arg recovery_sha "$recovery_sha" --arg primary_sha "$primary_sha" --arg version "$version" '{
		schema: "hikyo.dev/trust-metadata/v1", sequence: 1,
		highest_release: null, highest_release_sequence: null,
		recovery: {id: "recovery-1", sha256: $recovery_sha},
		event: {type: "bootstrap", signed_by: "recovery-1"},
		primary_keys: [{id: "primary-1", public_key: "primary-1.pub", sha256: $primary_sha,
			valid_from_release_sequence: 1, valid_through_release_sequence: null, revoked: false}],
		releases: [], pending_release: {version: $version, sequence: 1, manifest_sha256: ("0" * 64)}
	}' >"$ram_dir/metadata.json"
	printf 'Cosign will prompt for the RECOVERY passphrase to sign bootstrap metadata.\n'
	cosign sign-blob --yes --new-bundle-format=false --tlog-upload=false \
		--use-signing-config=false --key "$ram_dir/recovery-1.key" \
		--bundle "$ram_dir/metadata.sigstore.json" "$ram_dir/metadata.json"
	cosign verify-blob --insecure-ignore-tlog --key "$ram_dir/recovery-1.pub" \
		--bundle "$ram_dir/metadata.sigstore.json" "$ram_dir/metadata.json" >/dev/null
	install -m 0600 "$ram_dir/primary-1.key" "$primary_a/primary-1.key"
	install -m 0600 "$ram_dir/primary-1.key" "$primary_b/primary-1.key"
	install -m 0600 "$ram_dir/recovery-1.key" "$recovery_a/recovery-1.key"
	install -m 0600 "$ram_dir/recovery-1.key" "$recovery_b/recovery-1.key"
	sync
	cmp "$ram_dir/primary-1.key" "$primary_a/primary-1.key"
	cmp "$ram_dir/primary-1.key" "$primary_b/primary-1.key"
	cmp "$ram_dir/recovery-1.key" "$recovery_a/recovery-1.key"
	cmp "$ram_dir/recovery-1.key" "$recovery_b/recovery-1.key"
	install -m 0644 "$ram_dir/primary-1.pub" "$repo_root/release/trust/primary-1.pub"
	install -m 0644 "$ram_dir/recovery-1.pub" "$repo_root/release/trust/recovery-1.pub"
	install -m 0644 "$ram_dir/root.json" "$repo_root/release/trust/root.json"
	install -m 0644 "$ram_dir/metadata.json" "$repo_root/release/trust/metadata.json"
	install -m 0644 "$ram_dir/metadata.sigstore.json" "$repo_root/release/trust/metadata.sigstore.json"
	finish_ram
	diskutil eject "$primary_a" >/dev/null
	diskutil eject "$primary_b" >/dev/null
	diskutil eject "$recovery_a" >/dev/null
	diskutil eject "$recovery_b" >/dev/null
	if find "$repo_root/release/trust" -name '*.key' -print | grep . >/dev/null; then
		fail 'private key reached repository storage'
	fi
	record_state bootstrap
	printf 'Bootstrap complete. Reconnect networking, then choose phase 2.\n'
}

phase_candidate() {
	for command_name in cosign diskutil gh git jq route; do require_command "$command_name"; done
	metadata=$repo_root/release/trust/metadata.json
	[ -f "$metadata" ] || fail 'trust metadata is absent; run bootstrap first'
	if jq -e --arg version "$version" '.pending_release.version == $version' "$metadata" >/dev/null; then
		printf 'Bootstrap metadata already authorizes candidate %s.\n' "$version"
	else
		require_online
		[ -z "$(git status --porcelain)" ] || fail 'worktree must be clean before preparing a later candidate'
		git fetch origin main
		git switch --detach origin/main
		ensure_release_branch candidate
		metadata=$repo_root/release/trust/metadata.json
		jq -e '.pending_release == null' "$metadata" >/dev/null || fail 'different pending release already exists'
		printf 'Disconnect networking and insert one recovery-key USB.\n'
		require_offline
		recovery_key=$(prompt_file 'Recovery private-key path')
		mount_ram 131072
		cp -R "$repo_root/release/trust" "$ram_dir/trust"
		install -m 0600 "$recovery_key" "$ram_dir/recovery-1.key"
		next_release_sequence=$(jq -r '(.highest_release_sequence // 0) + 1' "$ram_dir/trust/metadata.json")
		jq --arg version "$version" --argjson release_sequence "$next_release_sequence" '
			.sequence += 1 |
			.event = {type: "release-candidate", signed_by: .recovery.id} |
			.pending_release = {version: $version, sequence: $release_sequence, manifest_sha256: ("0" * 64)}
		' "$ram_dir/trust/metadata.json" >"$ram_dir/trust/metadata.candidate.json"
		printf 'Cosign will prompt for the RECOVERY passphrase.\n'
		cosign sign-blob --yes --new-bundle-format=false --tlog-upload=false \
			--use-signing-config=false --key "$ram_dir/recovery-1.key" \
			--bundle "$ram_dir/trust/metadata.candidate.sigstore.json" \
			"$ram_dir/trust/metadata.candidate.json"
		cosign verify-blob --insecure-ignore-tlog --key "$ram_dir/trust/recovery-1.pub" \
			--bundle "$ram_dir/trust/metadata.candidate.sigstore.json" \
			"$ram_dir/trust/metadata.candidate.json" >/dev/null
		install -m 0644 "$ram_dir/trust/metadata.candidate.json" "$repo_root/release/trust/metadata.json"
		install -m 0644 "$ram_dir/trust/metadata.candidate.sigstore.json" \
			"$repo_root/release/trust/metadata.sigstore.json"
		finish_ram
		require_media_ejected "$recovery_key" recovery
		printf 'Reconnect networking, then type "online %s".\n' "$version"
		confirm_exact "online $version"
	fi
	require_online
	ensure_release_branch candidate
	confirm_exact "push candidate $version"
	open_merge_pr "chore(release): authorize $version candidate" \
		"Recovery-signed release candidate metadata for $version. Public material only."
	record_state candidate-merged
	printf 'Candidate merged. Choose phase 3 after exact-main CI is green.\n'
}

phase_tag() {
	for command_name in cosign gh git jq route; do require_command "$command_name"; done
	require_online
	git fetch origin main
	commit=$(git rev-parse origin/main)
	[ -z "$(git status --porcelain)" ] || fail 'worktree must be clean before tagging'
	git switch --detach "$commit"
	mkdir -p "$release_dir"
	"$script_dir/verify-bundle.sh" --root "$repo_root/release/trust/root.json" \
		--metadata "$repo_root/release/trust/metadata.json" \
		--metadata-signature "$repo_root/release/trust/metadata.sigstore.json" \
		--state "$release_dir/release-trust.json" --trust-only
	"$script_dir/test-fixtures.sh"
	pending=$(git show origin/main:release/trust/metadata.json | jq -r '.pending_release.version // empty')
	[ "$pending" = "$version" ] || fail "origin/main does not authorize pending release $version"
	ci_conclusion=$(gh run list --repo "$repository" --workflow ci.yml --commit "$commit" --limit 1 \
		--json conclusion --jq '.[0].conclusion // empty')
	[ "$ci_conclusion" = success ] || fail "exact-main CI is not green for $commit"
	[ -z "$(git ls-remote --tags origin "refs/tags/$tag")" ] || fail "tag already exists: $tag"
	[ -z "$(git tag --list "$tag")" ] || fail "local tag already exists: $tag"
	if gh release view "$tag" --repo "$repository" >/dev/null 2>&1; then
		fail "GitHub release already exists: $tag"
	fi
	confirm_exact "tag $tag at $commit"
	git tag "$tag" "$commit"
	git push origin "refs/tags/$tag"
	run_id=
	attempt=0
	while [ "$attempt" -lt 30 ] && [ -z "$run_id" ]; do
		run_id=$(gh run list --repo "$repository" --workflow release.yml --commit "$commit" --limit 1 \
			--json databaseId --jq '.[0].databaseId // empty')
		[ -n "$run_id" ] || sleep 10
		attempt=$((attempt + 1))
	done
	[ -n "$run_id" ] || fail 'release workflow did not start'
	gh run watch "$run_id" --repo "$repository" --exit-status
	mkdir -p "$release_dir"
	draft_dir=$release_dir/draft
	if [ ! -f "$draft_dir/release-manifest.json" ]; then
		[ ! -e "$draft_dir" ] || fail "partial draft directory exists: $draft_dir"
		mkdir "$draft_dir"
		gh release download "$tag" --repo "$repository" --dir "$draft_dir"
	fi
	validate_manifest_assets "$draft_dir"
	jq -e --arg commit "$commit" '.source_commit == $commit' \
		"$draft_dir/release-manifest.json" >/dev/null || fail 'draft source commit differs from immutable tag'
	record_state draft-verified
	printf 'Unsigned draft verified at %s. Choose phase 4.\n' "$draft_dir"
}

phase_sign() {
	for command_name in cosign diskutil gh git hdiutil jq route; do require_command "$command_name"; done
	draft_dir=$release_dir/draft
	validate_manifest_assets "$draft_dir"
	require_online
	git fetch origin main
	ensure_release_branch bind
	printf 'Disconnect networking and insert one recovery-key USB.\n'
	confirm_exact "offline recovery $version"
	require_offline
	recovery_key=$(prompt_file 'Recovery private-key path')
	mount_ram 2097152
	cp -R "$draft_dir" "$ram_dir/bundle"
	cp -R "$repo_root/release/trust" "$ram_dir/trust"
	install -m 0600 "$recovery_key" "$ram_dir/recovery-1.key"
	"$script_dir/bind-manifest.sh" "$ram_dir/bundle/release-manifest.json" \
		"$ram_dir/trust/metadata.json" "$ram_dir/trust/metadata.bound.json"
	printf 'Cosign will prompt for the RECOVERY passphrase.\n'
	cosign sign-blob --yes --new-bundle-format=false --tlog-upload=false \
		--use-signing-config=false --key "$ram_dir/recovery-1.key" \
		--bundle "$ram_dir/trust/metadata.bound.sigstore.json" \
		"$ram_dir/trust/metadata.bound.json"
	cosign verify-blob --insecure-ignore-tlog --key "$ram_dir/trust/recovery-1.pub" \
		--bundle "$ram_dir/trust/metadata.bound.sigstore.json" \
		"$ram_dir/trust/metadata.bound.json" >/dev/null
	install -m 0644 "$ram_dir/trust/metadata.bound.json" "$repo_root/release/trust/metadata.json"
	install -m 0644 "$ram_dir/trust/metadata.bound.sigstore.json" \
		"$repo_root/release/trust/metadata.sigstore.json"
	finish_ram
	require_media_ejected "$recovery_key" recovery
	printf 'Reconnect networking, then continue.\n'
	confirm_exact "online bind $version"
	require_online
	confirm_exact "push bound metadata $version"
	open_merge_pr "chore(release): bind $version manifest" \
		"Recovery-signed binding for the exact $version release manifest."
	git switch --detach origin/main
	printf 'Disconnect networking and insert one primary-key USB.\n'
	confirm_exact "offline primary $version"
	require_offline
	primary_key=$(prompt_file 'Primary private-key path')
	mount_ram 2097152
	cp -R "$draft_dir" "$ram_dir/bundle"
	cp -R "$repo_root/release/trust" "$ram_dir/trust"
	install -m 0600 "$primary_key" "$ram_dir/primary-1.key"
	printf 'Cosign will prompt for the PRIMARY passphrase for each signing operation.\n'
	"$script_dir/sign-bundle.sh" "$ram_dir/bundle" "$ram_dir/primary-1.key" \
		"$ram_dir/trust/metadata.json"
	"$script_dir/verify-bundle.sh" --root "$ram_dir/trust/root.json" \
		--metadata "$ram_dir/trust/metadata.json" \
		--metadata-signature "$ram_dir/trust/metadata.sigstore.json" \
		--bundle "$ram_dir/bundle" --state "$ram_dir/verification-state.json" --latest
	signed_dir=$release_dir/signed
	[ ! -e "$signed_dir" ] || fail "signed directory already exists: $signed_dir"
	mkdir "$signed_dir"
	ditto "$ram_dir/bundle" "$signed_dir"
	finish_ram
	if find "$signed_dir" -name '*.key' -print | grep . >/dev/null; then
		fail 'private key reached signed bundle storage'
	fi
	require_media_ejected "$primary_key" primary
	record_state signed
	printf 'Reconnect networking, then choose phase 5.\n'
}

phase_publish() {
	for command_name in cosign gh jq route; do require_command "$command_name"; done
	require_online
	signed_dir=$release_dir/signed
	validate_manifest_assets "$signed_dir"
	[ -f "$signed_dir/release-manifest.sigstore.json" ] || fail 'signed manifest bundle is absent'
	confirm_exact "stage signatures $tag"
	"$script_dir/publish-oci-signatures.sh" "$signed_dir" \
		"$repo_root/release/trust/root.json" "$repo_root/release/trust/metadata.json" \
		"$repo_root/release/trust/metadata.sigstore.json"
	set -- "$signed_dir"/*.sigstore.json
	[ -f "$1" ] || fail 'no Sigstore bundles found'
	gh release upload "$tag" "$@" --repo "$repository"
	channel=$("$script_dir/release-channel.sh" "$tag")
	printf 'Release-notes file (leave blank for generated notes): '
	IFS= read -r notes_file
	if [ -z "$notes_file" ]; then
		notes_file=$release_dir/release-notes.md
		if [ "$channel" = prerelease ]; then
			printf '%s\n' \
				"# Hikyo $version" '' \
				'Prerelease and unsupported. This does not freeze the API or CLI.' '' \
				'- Linux, macOS, and Windows archives for amd64 and arm64.' \
				'- Signed multi-architecture OCI image and Helm chart.' \
				'- Verify the signed release manifest before installation.' >"$notes_file"
		else
			printf '%s\n' \
				"# Hikyo $version" '' \
				"Signed Hikyo $version release." '' \
				'- Linux, macOS, and Windows archives for amd64 and arm64.' \
				'- Signed multi-architecture OCI image and Helm chart.' \
				'- Verify the signed release manifest before installation.' >"$notes_file"
		fi
	fi
	[ -f "$notes_file" ] || fail "release-notes file is absent: $notes_file"
	gh release edit "$tag" --repo "$repository" --title "$tag" --notes-file "$notes_file"
	verify_dir=$(mktemp -d "$release_dir/verify.XXXXXX")
	gh release download "$tag" --repo "$repository" --dir "$verify_dir"
	"$script_dir/verify-bundle.sh" --root "$repo_root/release/trust/root.json" \
		--metadata "$repo_root/release/trust/metadata.json" \
		--metadata-signature "$repo_root/release/trust/metadata.sigstore.json" \
		--bundle "$verify_dir" --state "$release_dir/release-trust.json" --published --latest
	confirm_exact "publish $tag"
	if [ "$channel" = prerelease ]; then
		gh release edit "$tag" --repo "$repository" --draft=false --prerelease --verify-tag
	else
		gh release edit "$tag" --repo "$repository" --draft=false --prerelease=false --verify-tag
	fi
	final_dir=$(mktemp -d "$release_dir/final.XXXXXX")
	gh release download "$tag" --repo "$repository" --dir "$final_dir"
	"$script_dir/verify-bundle.sh" --root "$repo_root/release/trust/root.json" \
		--metadata "$repo_root/release/trust/metadata.json" \
		--metadata-signature "$repo_root/release/trust/metadata.sigstore.json" \
		--bundle "$final_dir" --state "$release_dir/release-trust.json" --published --latest
	record_state published
	printf 'Published and externally reverified: %s\n' "$tag"
}

[ "$(uname -s)" = Darwin ] || fail 'macOS is required'

printf '%s\n' \
	'1. One-time trust bootstrap' \
	'2. Prepare candidate and PR' \
	'3. Tag and download unsigned draft' \
	'4. Recovery-bind and primary-sign' \
	'5. Publish and verify'
printf 'Choose phase [1-5]: '
IFS= read -r phase
case "$phase" in 1 | 2 | 3 | 4 | 5) ;; *) printf 'release ceremony: invalid phase %s\n' "$phase" >&2; exit 2 ;; esac

printf 'Release version (for example 1.0.0-alpha.0): '
IFS= read -r version
is_semver "$version" || { printf 'release ceremony: invalid SemVer %s\n' "$version" >&2; exit 2; }
tag=v$version
release_dir=$state_root/$version
repository=${HIKYO_REPOSITORY:-Hikyo-Org/Hikyo}

if [ "$dry_run" = true ]; then
	dry_run_phase
	exit 0
fi

cd "$repo_root"
case "$phase" in
	1) phase_bootstrap ;;
	2) phase_candidate ;;
	3) phase_tag ;;
	4) phase_sign ;;
	5) phase_publish ;;
esac
