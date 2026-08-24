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
	[ ! -e /Volumes/HIKYO_SIGNING ] || fail 'signing RAM disk remains mounted; detach it before networking'
	if find /Volumes -mindepth 2 -type f -name '*.key.age' -print -quit 2>/dev/null | grep . >/dev/null; then
		fail 'age-wrapped private-key media is mounted; eject it before networking'
	fi
}

ram_device=
ram_dir=
cleanup_ram() {
	if [ -n "$ram_device" ]; then
		if hdiutil detach "$ram_device" >/dev/null 2>&1; then
			ram_device=
			ram_dir=
			return 0
		fi
		printf 'release ceremony: RAM disk detach failed: %s\n' "$ram_device" >&2
		return 1
	fi
}
trap 'cleanup_ram || exit 1' EXIT
trap 'cleanup_ram || true; exit 130' HUP INT TERM

mount_ram() {
	sectors=$1
	[ -z "${COSIGN_PASSWORD:-}" ] || fail 'unset COSIGN_PASSWORD; enter signing passphrases only at the prompt'
	command -v fdesetup >/dev/null 2>&1 || fail 'fdesetup is required to verify FileVault'
	fdesetup status | grep -Fx 'FileVault is On.' >/dev/null || \
		fail 'FileVault must be On so workstation swap is encrypted'
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
	cleanup_ram || fail 'RAM disk remains mounted; do not reconnect networking'
}

external_disk_id() {
	volume=$1
	[ -d "$volume" ] || fail "USB volume is absent: $volume"
	volume_info=$(diskutil info "$volume")
	printf '%s\n' "$volume_info" | grep -Eq \
		'Device Location:[[:space:]]*External|Removable Media:[[:space:]]*Removable' || \
		fail "USB volume is not external/removable: $volume"
	disk_id=$(printf '%s\n' "$volume_info" | awk -F: '
		/Part of Whole:/ {sub(/^[[:space:]]*/, "", $2); whole=$2}
		END {if (whole != "") print whole}
	')
	[ -n "$disk_id" ] || fail "cannot identify physical USB device: $volume"
	printf '%s\n' "$disk_id"
}

prompt_file() {
	label=$1
	printf '%s: ' "$label" >&2
	IFS= read -r prompted_file
	[ -f "$prompted_file" ] || fail "file is absent: $prompted_file"
	[ ! -L "$prompted_file" ] || fail 'private key must not be a symbolic link'
	case "$prompted_file" in
		/Volumes/*/*.key.age) ;;
		*) fail 'private key must be read from removable media under /Volumes' ;;
	esac
	volume_relative=${prompted_file#/Volumes/}
	volume_name=${volume_relative%%/*}
	media_info=$(diskutil info "/Volumes/$volume_name")
	printf '%s\n' "$media_info" | grep -Eq \
		'Device Location:[[:space:]]*External|Removable Media:[[:space:]]*Removable' || \
		fail 'private key is not on external/removable media'
	printf '%s\n' "$prompted_file"
}

decrypt_private_key() {
	wrapped_key=$1
	decrypted_key=$2
	printf 'age will prompt for the USB key-wrap passphrase.\n' >&2
	age --decrypt --output "$decrypted_key" "$wrapped_key"
	chmod 0600 "$decrypted_key"
}

wrap_private_key() {
	private_key=$1
	wrapped_key=$2
	key_label=$3
	printf 'age will prompt for the %s USB key-wrap passphrase.\n' "$key_label"
	age --passphrase --output "$wrapped_key" "$private_key"
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
trust_state_path=$state_home/hikyo/release-trust.json
saved_phase=

load_state() {
	state_file=$release_dir/state.json
	[ -e "$state_file" ] || return 0
	[ -f "$state_file" ] || fail "progress state is not a regular file: $state_file"
	jq -e --arg version "$version" --arg tag "$tag" --arg repository "$repository" '
		. as $state |
		.version == $version and .tag == $tag and .repository == $repository and
		(["bootstrap", "candidate-local", "candidate-merged", "tag-pushed",
		  "draft-verified", "bound-local", "bound-merged", "signed",
		  "signatures-staged", "published"] | index($state.phase) != null)
	' "$state_file" >/dev/null || fail 'saved progress identity or phase is invalid'
	saved_phase=$(jq -r '.phase' "$state_file")
	printf 'Recorded progress: %s\n' "$saved_phase"
}

require_saved_phase() {
	for allowed_phase in "$@"; do
		[ "$saved_phase" = "$allowed_phase" ] && return 0
	done
	[ -n "$saved_phase" ] || fail 'no saved progress exists for this release'
	fail "phase cannot continue from recorded progress: $saved_phase"
}

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
	saved_phase=$completed_phase
	printf 'Progress saved: %s\n' "$completed_phase"
}

verify_local_trust() {
	trust_state=$(mktemp "$release_dir/local-trust.XXXXXX")
	rm -f "$trust_state"
	"$script_dir/verify-bundle.sh" --root "$repo_root/release/trust/root.json" \
		--metadata "$repo_root/release/trust/metadata.json" \
		--metadata-signature "$repo_root/release/trust/metadata.sigstore.json" \
		--state "$trust_state" --trust-only
}

validate_manifest_assets() {
	bundle=$1
	bundle_stage=${2:-unsigned}
	manifest=$bundle/release-manifest.json
	[ -f "$manifest" ] || fail 'release-manifest.json is absent'
	if find "$bundle" -maxdepth 1 -type l -print | grep . >/dev/null; then
		fail 'release bundle contains a symbolic link'
	fi
	jq -e --arg version "$version" --arg tag "$tag" '
		.schema == "hikyo.dev/release-manifest/v1" and
		.version == $version and .tag == $tag and
		([.artifacts[] | select(.kind == "binary")] | length) == 6 and
		([.artifacts[] | select(.kind == "package")] | length) == 8 and
		. as $manifest |
		all(["apk", "archlinux", "deb", "rpm"][];
			. as $format |
			([$manifest.artifacts[] | select(.kind == "package" and .format == $format and .arch == "amd64")] | length) == 1 and
			([$manifest.artifacts[] | select(.kind == "package" and .format == $format and .arch == "arm64")] | length) == 1) and
		([.artifacts[] | select(.kind == "oci-payload")] | length) == 2
	' "$manifest" >/dev/null || fail 'release manifest identity or target matrix is invalid'
	asset_failure=false
	while IFS="$(printf '\t')" read -r name expected_sha kind package_format package_arch; do
		safe_release_name "$name" || { printf 'Unsafe asset name: %s\n' "$name" >&2; asset_failure=true; continue; }
		if [ "$kind" = package ]; then
			expected_package_name=$(package_file_name "$version" "$package_format" "$package_arch") || {
				printf 'Unsupported package version identity: %s\n' "$version" >&2
				asset_failure=true
				continue
			}
			[ "$name" = "$expected_package_name" ] || {
				printf 'Package name is not release-bound: %s\n' "$name" >&2
				asset_failure=true
				continue
			}
		fi
		case "$expected_sha" in *[!0-9a-f]* | '') asset_failure=true; continue ;; esac
		[ "${#expected_sha}" -eq 64 ] || { asset_failure=true; continue; }
		[ -f "$bundle/$name" ] || { printf 'Missing asset: %s\n' "$name" >&2; asset_failure=true; continue; }
		actual_sha=$(sha256_file "$bundle/$name")
		[ "$actual_sha" = "$expected_sha" ] || {
			printf 'Hash mismatch: %s\n' "$name" >&2
			asset_failure=true
		}
	done <<EOF
$(jq -r '.artifacts[] | [.name, .sha256, .kind, (.format // ""), (.arch // "")] | @tsv' "$manifest")
EOF
	[ "$asset_failure" = false ] || fail 'draft asset validation failed'
	while IFS= read -r actual_path; do
		actual_name=$(basename "$actual_path")
		[ "$actual_name" = release-manifest.json ] && continue
		if jq -e --arg name "$actual_name" 'any(.artifacts[]; .name == $name)' "$manifest" >/dev/null; then
			continue
		fi
		if [ "$bundle_stage" != unsigned ]; then
			case "$actual_name" in
				release-manifest.sigstore.json) continue ;;
				*.sigstore.json)
					signed_name=${actual_name%.sigstore.json}
					jq -e --arg name "$signed_name" 'any(.artifacts[]; .name == $name)' "$manifest" >/dev/null && continue
					;;
				*.signature)
					if [ "$bundle_stage" = local-signed ]; then
						signed_name=${actual_name%.signature}
						jq -e --arg name "$signed_name" 'any(.artifacts[]; .name == $name and .kind == "oci-payload")' \
							"$manifest" >/dev/null && continue
					fi
					;;
			esac
		fi
		fail "release bundle contains an unmanifested file: $actual_name"
	done <<EOF
$(find "$bundle" -maxdepth 1 -type f -print)
EOF
	printf 'Draft assets: six binaries and every manifest hash verified\n'
}

validate_checksum_manifest() {
	bundle=$1
	manifest=$2
	checksum_names_file=$(mktemp "${TMPDIR:-/tmp}/hikyo-checksum-names.XXXXXX")
	checksum_count=0
	while read -r checksum_hash checksum_name extra; do
		[ -z "${extra:-}" ] || fail 'checksums.txt contains an invalid line'
		safe_release_name "$checksum_name" || fail "checksums.txt contains an unsafe path: $checksum_name"
		case "$checksum_hash" in *[!0-9a-f]* | '') fail 'checksums.txt contains an invalid SHA-256' ;; esac
		[ "${#checksum_hash}" -eq 64 ] || fail 'checksums.txt contains an invalid SHA-256'
		jq -e --arg name "$checksum_name" \
			'([.artifacts[] | select(.kind == "binary" and .name == $name)] | length) == 1' \
			"$manifest" >/dev/null || fail "checksums.txt names a non-binary asset: $checksum_name"
		printf '%s\n' "$checksum_name" >>"$checksum_names_file"
		checksum_count=$((checksum_count + 1))
	done <"$bundle/checksums.txt"
	[ "$checksum_count" -eq 6 ] || fail 'checksums.txt must contain exactly six release archives'
	checksum_names=$(LC_ALL=C sort -u "$checksum_names_file")
	manifest_binary_names=$(jq -r '.artifacts[] | select(.kind == "binary") | .name' "$manifest" | LC_ALL=C sort)
	rm -f "$checksum_names_file"
	[ "$checksum_names" = "$manifest_binary_names" ] || \
		fail 'checksums.txt does not name the exact six manifest binaries once each'
	(
		cd "$bundle"
		shasum -a 256 -c checksums.txt >/dev/null
	) || fail 'checksums.txt does not match the release archives'
}

live_oci_digest() {
	oci_ref=$1
	docker buildx imagetools inspect "$oci_ref" --format '{{json .Manifest}}' |
		jq -er '.digest | select(test("^sha256:[0-9a-f]{64}$"))'
}

validate_unsigned_draft() {
	bundle=$1
	validate_manifest_assets "$bundle" unsigned
	manifest=$bundle/release-manifest.json
	candidate=$bundle/release-candidate.json
	verify_release_candidate_artifact "$manifest" "$bundle" || fail 'release candidate artifact is invalid'
	release_manifest_matches_candidate "$manifest" "$candidate" || \
		fail 'manifest identity differs from release candidate'
	authorize_release_candidate "$repo_root/release/trust/metadata.json" "$candidate" || \
		fail 'release candidate is not authorized by trust metadata'
	validate_checksum_manifest "$bundle" "$manifest"
	go run "$script_dir/verify-native-packages.go" "$bundle" "$version" || \
		fail 'native package payload or metadata verification failed'
	image_ref=$(jq -r '.artifacts[] | select(.kind == "image") | .image' "$manifest")
	image_digest=$(tr -d '\n' <"$bundle/image-index.digest")
	chart_ref=$(jq -r '.artifacts[] | select(.kind == "chart-digest") | .chart' "$manifest")
	chart_digest=$(tr -d '\n' <"$bundle/chart-index.digest")
	[ "$(live_oci_digest "$image_ref:$version")" = "$image_digest" ] || \
		fail 'live GHCR image tag does not resolve to image-index.digest'
	[ "$(live_oci_digest "$chart_ref:$version")" = "$chart_digest" ] || \
		fail 'live GHCR chart tag does not resolve to chart-index.digest'
	jq -e --arg image "$image_ref@$image_digest" --arg chart "$chart_ref@$chart_digest" '
		([.artifacts[] | select(.kind == "oci-payload" and .subject_kind == "image" and .subject == $image)] | length) == 1 and
		([.artifacts[] | select(.kind == "oci-payload" and .subject_kind == "chart" and .subject == $chart)] | length) == 1
	' "$manifest" >/dev/null || fail 'prepared OCI payload subjects do not match published digests'
	for payload_kind in image chart; do
		payload_name=$(jq -r --arg kind "$payload_kind" \
			'.artifacts[] | select(.kind == "oci-payload" and .subject_kind == $kind) | .name' "$manifest")
		payload_subject=$(jq -r '.critical.identity["docker-reference"] + "@" + .critical.image["docker-manifest-digest"]' \
			"$bundle/$payload_name")
		expected_subject=$(jq -r --arg name "$payload_name" '.artifacts[] | select(.name == $name) | .subject' "$manifest")
		[ "$payload_subject" = "$expected_subject" ] || fail "prepared OCI payload identity mismatch: $payload_name"
	done
	printf 'Unsigned draft: checksums, candidate, live GHCR digests, and OCI payloads verified\n'
}

upload_signature_assets() {
	set -- "$signed_dir"/*.sigstore.json
	[ -f "$1" ] || fail 'no Sigstore bundles found'
	for signature_asset in "$@"; do
		asset_name=$(basename "$signature_asset")
		if gh release view "$tag" --repo "$repository" --json assets \
			--jq '.assets[].name' | grep -Fx "$asset_name" >/dev/null; then
			asset_check=$(mktemp -d "$release_dir/asset.XXXXXX")
			gh release download "$tag" --repo "$repository" --pattern "$asset_name" --dir "$asset_check"
			cmp "$signature_asset" "$asset_check/$asset_name" || \
				fail "existing release asset differs: $asset_name"
			printf 'Existing signature asset verified: %s\n' "$asset_name"
		else
			gh release upload "$tag" "$signature_asset" --repo "$repository"
		fi
	done
}

require_draft_release() {
	[ "$(gh release view "$tag" --repo "$repository" --json isDraft --jq .isDraft)" = true ] || \
		fail "release is not an unsigned draft: $tag"
}

ensure_release_branch() {
	prefix=$1
	current_branch=$(git branch --show-current)
	branch_version=$(printf '%s' "$version" | tr '.+' '--')
	expected_branch=release/$prefix-$branch_version
	if [ -z "$current_branch" ] || [ "$current_branch" = main ]; then
		git switch -c "$expected_branch" origin/main
	elif [ "$current_branch" != "$expected_branch" ]; then
		fail "expected release branch $expected_branch, found $current_branch"
	fi
}

open_merge_pr() {
	title=$1
	body=$2
	branch=$(git branch --show-current)
	case "$branch" in release/*) ;; *) fail 'release PR must be opened from a release/* branch' ;; esac
	git diff --check
	for trust_file in primary-1.pub recovery-1.pub root.json metadata.json metadata.sigstore.json; do
		[ -f "$repo_root/release/trust/$trust_file" ] || fail "mandatory trust file is absent: $trust_file"
	done
	git add release/trust/primary-1.pub release/trust/recovery-1.pub \
		release/trust/root.json release/trust/metadata.json \
		release/trust/metadata.sigstore.json
	git diff --cached --check
	if ! git diff --cached --quiet; then
		git commit -s -m "$title"
	fi
	pr_number=$(gh pr list --repo "$repository" --head "$branch" --state all --limit 1 \
		--json number --jq '.[0].number // empty')
	if [ -n "$pr_number" ]; then
		pr_state=$(gh pr view "$pr_number" --repo "$repository" --json state --jq .state)
		[ "$pr_state" != CLOSED ] || fail "release PR $pr_number was closed without merge"
		if [ "$pr_state" = MERGED ]; then
			git fetch origin main
			return 0
		fi
	fi
	git fetch origin main
	git merge --no-edit --signoff origin/main
	git push -u origin HEAD
	if [ -z "$pr_number" ]; then
		gh pr create --repo "$repository" --base main --head "$branch" \
			--title "$title" --body "$body"
		pr_number=$(gh pr view --repo "$repository" --json number --jq .number)
	fi
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
				'Would generate separate primary/recovery keys and copy only age-wrapped .key.age files to USB.' \
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
				'Would attach OCI signatures, upload Sigstore bundles, and verify published subjects.'
			if [ "$("$script_dir/release-channel.sh" "$tag")" = prerelease ]; then
				printf '%s\n' 'Would skip the stable homebrew-tap for this prerelease.'
			else
				printf '%s\n' 'Would open or refresh a protected homebrew-tap PR only after public release verification.'
			fi
			printf '%s\n' "would require exact typed confirmation: publish $tag"
			;;
	esac
}

phase_bootstrap() {
	for command_name in age cosign diskutil hdiutil jq route shasum; do require_command "$command_name"; done
	[ -z "$saved_phase" ] || fail 'bootstrap cannot run when release progress already exists'
	[ ! -e "$repo_root/release/trust/root.json" ] || fail 'production trust root already exists; bootstrap is one-time only'
	printf 'Disconnect all networking, mount four distinct USB volumes, then continue.\n'
	confirm_exact "offline bootstrap $version"
	require_offline
	printf 'Primary USB copy A mount path: '; IFS= read -r primary_a
	printf 'Primary USB copy B mount path: '; IFS= read -r primary_b
	printf 'Recovery USB copy A mount path: '; IFS= read -r recovery_a
	printf 'Recovery USB copy B mount path: '; IFS= read -r recovery_b
	[ "$primary_a" != "$primary_b" ] && [ "$primary_a" != "$recovery_a" ] && \
		[ "$primary_a" != "$recovery_b" ] && [ "$primary_b" != "$recovery_a" ] && \
		[ "$primary_b" != "$recovery_b" ] && [ "$recovery_a" != "$recovery_b" ] || \
		fail 'all four USB mount paths must be distinct'
	primary_a_disk=$(external_disk_id "$primary_a")
	primary_b_disk=$(external_disk_id "$primary_b")
	recovery_a_disk=$(external_disk_id "$recovery_a")
	recovery_b_disk=$(external_disk_id "$recovery_b")
	[ "$primary_a_disk" != "$primary_b_disk" ] && [ "$primary_a_disk" != "$recovery_a_disk" ] && \
		[ "$primary_a_disk" != "$recovery_b_disk" ] && [ "$primary_b_disk" != "$recovery_a_disk" ] && \
		[ "$primary_b_disk" != "$recovery_b_disk" ] && [ "$recovery_a_disk" != "$recovery_b_disk" ] || \
		fail 'all four key copies must use distinct physical USB devices'
	for private_copy in "$primary_a/primary-1.key.age" "$primary_b/primary-1.key.age" \
		"$recovery_a/recovery-1.key.age" "$recovery_b/recovery-1.key.age"; do
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
	wrap_private_key "$ram_dir/primary-1.key" "$ram_dir/primary-1.key.age" PRIMARY
	wrap_private_key "$ram_dir/recovery-1.key" "$ram_dir/recovery-1.key.age" 'DIFFERENT RECOVERY'
	install -m 0600 "$ram_dir/primary-1.key.age" "$primary_a/primary-1.key.age"
	install -m 0600 "$ram_dir/primary-1.key.age" "$primary_b/primary-1.key.age"
	install -m 0600 "$ram_dir/recovery-1.key.age" "$recovery_a/recovery-1.key.age"
	install -m 0600 "$ram_dir/recovery-1.key.age" "$recovery_b/recovery-1.key.age"
	sync
	cmp "$ram_dir/primary-1.key.age" "$primary_a/primary-1.key.age"
	cmp "$ram_dir/primary-1.key.age" "$primary_b/primary-1.key.age"
	cmp "$ram_dir/recovery-1.key.age" "$recovery_a/recovery-1.key.age"
	cmp "$ram_dir/recovery-1.key.age" "$recovery_b/recovery-1.key.age"
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
	if find "$repo_root/release/trust" -name '*.key*' -print | grep . >/dev/null; then
		fail 'private key reached repository storage'
	fi
	record_state bootstrap
	printf 'Bootstrap complete. Reconnect networking, then choose phase 2.\n'
}

phase_candidate() {
	for command_name in age cosign diskutil gh git jq route; do require_command "$command_name"; done
	if [ "$saved_phase" = candidate-merged ]; then
		printf 'Candidate was already merged. Choose phase 3.\n'
		return 0
	fi
	case "$saved_phase" in '' | bootstrap | candidate-local) ;; *) fail "candidate cannot continue from $saved_phase" ;; esac
	metadata=$repo_root/release/trust/metadata.json
	[ -f "$metadata" ] || fail 'trust metadata is absent; run bootstrap first'
	if [ "$saved_phase" = candidate-local ]; then
		printf 'Resuming locally prepared candidate metadata.\n'
	elif jq -e --arg version "$version" '.pending_release.version == $version' "$metadata" >/dev/null; then
		printf 'Bootstrap metadata already authorizes candidate %s.\n' "$version"
	else
		require_online
		[ -z "$(git status --porcelain)" ] || fail 'worktree must be clean before preparing a later candidate'
		confirm_exact "prepare candidate branch $version"
		git fetch origin main
		git switch --detach origin/main
		ensure_release_branch candidate
		metadata=$repo_root/release/trust/metadata.json
		jq -e '.pending_release == null' "$metadata" >/dev/null || fail 'different pending release already exists'
		printf 'Disconnect networking and insert one recovery-key USB.\n'
		confirm_exact "offline candidate $version"
		require_offline
		recovery_key=$(prompt_file 'Recovery private-key path')
		mount_ram 131072
		cp -R "$repo_root/release/trust" "$ram_dir/trust"
		decrypt_private_key "$recovery_key" "$ram_dir/recovery-1.key"
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
		record_state candidate-local
		printf 'Reconnect networking, then type "online %s".\n' "$version"
		confirm_exact "online $version"
	fi
	require_online
	if [ -z "$(git branch --show-current)" ] || [ "$(git branch --show-current)" = main ]; then
		confirm_exact "prepare candidate branch $version"
	fi
	ensure_release_branch candidate
	record_state candidate-local
	verify_local_trust
	jq -e --arg version "$version" '.pending_release.version == $version' \
		"$repo_root/release/trust/metadata.json" >/dev/null || fail 'local trust metadata authorizes a different candidate'
	confirm_exact "push candidate $version"
	open_merge_pr "chore(release): authorize $version candidate" \
		"Recovery-signed release candidate metadata for $version. Public material only."
	record_state candidate-merged
	printf 'Candidate merged. Choose phase 3 after exact-main CI is green.\n'
}

phase_tag() {
	for command_name in cosign docker gh git go jq route shasum; do require_command "$command_name"; done
	require_saved_phase candidate-merged tag-pushed draft-verified
	require_online
	confirm_exact "prepare tag worktree $version"
	git fetch origin main
	remote_tag_sha=$(git ls-remote --tags origin "refs/tags/$tag" | awk 'NR == 1 {print $1}')
	if [ -n "$remote_tag_sha" ]; then
		git fetch origin "refs/tags/$tag:refs/tags/$tag"
		commit=$(git rev-parse "$tag^{commit}")
		[ "$commit" = "$remote_tag_sha" ] || fail "remote tag does not resolve to its advertised commit: $tag"
		git merge-base --is-ancestor "$commit" origin/main || fail "tagged commit is not reachable from origin/main: $tag"
	else
		commit=$(git rev-parse origin/main)
	fi
	[ -z "$(git status --porcelain)" ] || fail 'worktree must be clean before tagging'
	git switch --detach "$commit"
	mkdir -p "$release_dir"
	"$script_dir/verify-bundle.sh" --root "$repo_root/release/trust/root.json" \
		--metadata "$repo_root/release/trust/metadata.json" \
		--metadata-signature "$repo_root/release/trust/metadata.sigstore.json" \
		--state "$trust_state_path" --trust-only
	"$script_dir/test-fixtures.sh"
	pending=$(git show "$commit:release/trust/metadata.json" | jq -r '.pending_release.version // empty')
	[ "$pending" = "$version" ] || fail "tagged commit does not authorize pending release $version"
	ci_conclusion=$(gh run list --repo "$repository" --workflow ci.yml --commit "$commit" --limit 1 \
		--json conclusion --jq '.[0].conclusion // empty')
	[ "$ci_conclusion" = success ] || fail "exact-main CI is not green for $commit"
	local_tag_sha=
	if [ -n "$(git tag --list "$tag")" ]; then
		local_tag_sha=$(git rev-list -n 1 "$tag")
	fi
	[ -z "$remote_tag_sha" ] || [ "$remote_tag_sha" = "$commit" ] || fail "remote tag points at a different commit: $tag"
	[ -z "$local_tag_sha" ] || [ "$local_tag_sha" = "$commit" ] || fail "local tag points at a different commit: $tag"
	if [ -z "$remote_tag_sha" ]; then
		confirm_exact "tag $tag at $commit"
		[ -n "$local_tag_sha" ] || git tag "$tag" "$commit"
		git push origin "refs/tags/$tag"
		record_state tag-pushed
	else
		printf 'Existing immutable tag matches expected commit: %s\n' "$commit"
	fi
	if gh release view "$tag" --repo "$repository" --json isDraft --jq .isDraft >"$release_dir/release-is-draft" 2>/dev/null; then
		[ "$(cat "$release_dir/release-is-draft")" = true ] || fail "existing GitHub release is not a draft: $tag"
	fi
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
	require_draft_release
	mkdir -p "$release_dir"
	draft_dir=$release_dir/draft
	draft_download=$(mktemp -d "$release_dir/draft-download.XXXXXX")
	gh release download "$tag" --repo "$repository" --dir "$draft_download"
	validate_unsigned_draft "$draft_download"
	jq -e --arg commit "$commit" '.source_commit == $commit' \
		"$draft_download/release-manifest.json" >/dev/null || fail 'draft source commit differs from immutable tag'
	if [ -e "$draft_dir" ]; then
		old_draft=$(mktemp -d "$release_dir/replaced-draft.XXXXXX")
		mv "$draft_dir" "$old_draft/draft"
	fi
	mv "$draft_download" "$draft_dir"
	record_state draft-verified
	printf 'Unsigned draft verified at %s. Choose phase 4.\n' "$draft_dir"
}

phase_sign() {
	for command_name in age cosign diskutil docker gh git go hdiutil jq route shasum; do require_command "$command_name"; done
	require_saved_phase draft-verified bound-local bound-merged signed
	if [ "$saved_phase" = signed ]; then
		printf 'Release bundle was already signed. Choose phase 5.\n'
		return 0
	fi
	draft_dir=$release_dir/draft
	require_online
	validate_unsigned_draft "$draft_dir"
	signed_dir=$release_dir/signed
	if [ -d "$signed_dir" ]; then
		validate_manifest_assets "$signed_dir" local-signed
		if find "$signed_dir" -name '*.key*' -print | grep . >/dev/null; then
			fail 'private key reached signed bundle storage'
		fi
		recovered_state=$(mktemp "$release_dir/recovered-state.XXXXXX")
		rm -f "$recovered_state"
		"$script_dir/verify-bundle.sh" --root "$repo_root/release/trust/root.json" \
			--metadata "$repo_root/release/trust/metadata.json" \
			--metadata-signature "$repo_root/release/trust/metadata.sigstore.json" \
			--bundle "$signed_dir" --state "$recovered_state" --latest
		record_state signed
		printf 'Recovered an already-complete signed bundle. Choose phase 5.\n'
		return 0
	fi
	if [ "$saved_phase" = draft-verified ]; then
		[ -z "$(git status --porcelain)" ] || fail 'worktree must be clean before binding the manifest'
		confirm_exact "prepare binding branch $version"
		git fetch origin main
		git switch --detach origin/main
		ensure_release_branch bind
		printf 'Disconnect networking and insert one recovery-key USB.\n'
		confirm_exact "offline recovery $version"
		require_offline
		recovery_key=$(prompt_file 'Recovery private-key path')
		mount_ram 2097152
		cp -R "$draft_dir" "$ram_dir/bundle"
		cp -R "$repo_root/release/trust" "$ram_dir/trust"
		decrypt_private_key "$recovery_key" "$ram_dir/recovery-1.key"
		"$script_dir/bind-manifest.sh" "$ram_dir/bundle/release-manifest.json" \
			"$ram_dir/trust/metadata.json" "$ram_dir/trust/metadata.bound.json"
		printf 'Cosign will prompt for the RECOVERY Cosign passphrase.\n'
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
		record_state bound-local
		printf 'Reconnect networking, then continue.\n'
		confirm_exact "online bind $version"
	fi
	if [ "$saved_phase" = draft-verified ] || [ "$saved_phase" = bound-local ]; then
		require_online
		verify_local_trust
		manifest_sha=$(sha256_file "$draft_dir/release-manifest.json")
		jq -e --arg version "$version" --arg manifest_sha "$manifest_sha" '
			.pending_release == null and
			([.releases[] | select(.version == $version and .manifest_sha256 == $manifest_sha)] | length) == 1
		' "$repo_root/release/trust/metadata.json" >/dev/null || fail 'local trust metadata does not bind this draft'
		confirm_exact "push bound metadata $version"
		open_merge_pr "chore(release): bind $version manifest" \
			"Recovery-signed binding for the exact $version release manifest."
		record_state bound-merged
	fi
	confirm_exact "prepare primary signing $version"
	git fetch origin main
	git switch --detach origin/main
	printf 'Disconnect networking and insert one primary-key USB.\n'
	confirm_exact "offline primary $version"
	require_offline
	primary_key=$(prompt_file 'Primary private-key path')
	mount_ram 2097152
	cp -R "$draft_dir" "$ram_dir/bundle"
	cp -R "$repo_root/release/trust" "$ram_dir/trust"
	decrypt_private_key "$primary_key" "$ram_dir/primary-1.key"
	printf 'Cosign will prompt for the PRIMARY Cosign passphrase for each signing operation.\n'
	"$script_dir/sign-bundle.sh" "$ram_dir/bundle" "$ram_dir/primary-1.key" \
		"$ram_dir/trust/metadata.json"
	"$script_dir/verify-bundle.sh" --root "$ram_dir/trust/root.json" \
		--metadata "$ram_dir/trust/metadata.json" \
		--metadata-signature "$ram_dir/trust/metadata.sigstore.json" \
		--bundle "$ram_dir/bundle" --state "$ram_dir/verification-state.json" --latest
	[ ! -e "$signed_dir" ] || fail "signed directory already exists: $signed_dir"
	signed_tmp=$(mktemp -d "$release_dir/signed.XXXXXX")
	ditto "$ram_dir/bundle" "$signed_tmp"
	mv "$signed_tmp" "$signed_dir"
	finish_ram
	if find "$signed_dir" -name '*.key*' -print | grep . >/dev/null; then
		fail 'private key reached signed bundle storage'
	fi
	require_media_ejected "$primary_key" primary
	record_state signed
	printf 'Reconnect networking, then choose phase 5.\n'
}

verify_published_release_and_prepare_tap() {
	published_bundle=$1
	"$script_dir/verify-bundle.sh" --root "$repo_root/release/trust/root.json" \
		--metadata "$repo_root/release/trust/metadata.json" \
		--metadata-signature "$repo_root/release/trust/metadata.sigstore.json" \
		--bundle "$published_bundle" --state "$trust_state_path" --published --latest
	# Record core publication before tap work. If tap publication fails, phase 5
	# resumes at the already-published branch and retries without republishing.
	record_state published
	"$script_dir/publish-homebrew-cask.sh" "$repository" "$tag" \
		"$published_bundle" "${HIKYO_HOMEBREW_TAP_REPOSITORY:-Hikyo-Org/homebrew-tap}"
}

phase_publish() {
	for command_name in cosign gh jq route; do require_command "$command_name"; done
	require_saved_phase signed signatures-staged published
	require_online
	signed_dir=$release_dir/signed
	validate_manifest_assets "$signed_dir" local-signed
	[ -f "$signed_dir/release-manifest.sigstore.json" ] || fail 'signed manifest bundle is absent'
	if [ "$saved_phase" = published ]; then
		final_dir=$(mktemp -d "$release_dir/final.XXXXXX")
		gh release download "$tag" --repo "$repository" --dir "$final_dir"
		validate_manifest_assets "$final_dir" published
		verify_published_release_and_prepare_tap "$final_dir"
		printf 'Published release remains externally verified: %s\n' "$tag"
		return 0
	fi
	confirm_exact "stage signatures $tag"
	primary_name=$(jq -r '.public_key' "$signed_dir/release-candidate.json")
	primary_public=$repo_root/release/trust/$primary_name
	all_oci_signatures_present=true
	while IFS= read -r subject; do
		if ! cosign verify --insecure-ignore-tlog --key "$primary_public" "$subject" >/dev/null 2>&1; then
			all_oci_signatures_present=false
		fi
	done <<EOF
$(jq -r '.artifacts[] | select(.kind == "oci-payload") | .subject' "$signed_dir/release-manifest.json")
EOF
	if [ "$all_oci_signatures_present" = false ]; then
		"$script_dir/publish-oci-signatures.sh" "$signed_dir" \
			"$repo_root/release/trust/root.json" "$repo_root/release/trust/metadata.json" \
			"$repo_root/release/trust/metadata.sigstore.json"
	else
		printf 'Existing OCI signatures verified; attachment skipped.\n'
	fi
	upload_signature_assets
	record_state signatures-staged
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
				'- Debian, RPM, APK, and Arch Linux packages for amd64 and arm64.' \
				'- Signed multi-architecture OCI image and Helm chart.' \
				'- Verify the signed release manifest before installation.' >"$notes_file"
		else
			printf '%s\n' \
				"# Hikyo $version" '' \
				"Signed Hikyo $version release." '' \
				'- Linux, macOS, and Windows archives for amd64 and arm64.' \
				'- Debian, RPM, APK, and Arch Linux packages for amd64 and arm64.' \
				'- Homebrew cask follows through the protected homebrew-tap review.' \
				'- Signed multi-architecture OCI image and Helm chart.' \
				'- Verify the signed release manifest before installation.' >"$notes_file"
		fi
	fi
	[ -f "$notes_file" ] || fail "release-notes file is absent: $notes_file"
	gh release edit "$tag" --repo "$repository" --title "$tag" --notes-file "$notes_file"
	verify_dir=$(mktemp -d "$release_dir/verify.XXXXXX")
	gh release download "$tag" --repo "$repository" --dir "$verify_dir"
	validate_manifest_assets "$verify_dir" published
	"$script_dir/verify-bundle.sh" --root "$repo_root/release/trust/root.json" \
		--metadata "$repo_root/release/trust/metadata.json" \
		--metadata-signature "$repo_root/release/trust/metadata.sigstore.json" \
		--bundle "$verify_dir" --state "$trust_state_path" --published --latest
	confirm_exact "publish $tag"
	if [ "$channel" = prerelease ]; then
		gh release edit "$tag" --repo "$repository" --draft=false --prerelease --verify-tag
	else
		gh release edit "$tag" --repo "$repository" --draft=false --prerelease=false --verify-tag
	fi
	final_dir=$(mktemp -d "$release_dir/final.XXXXXX")
	gh release download "$tag" --repo "$repository" --dir "$final_dir"
	validate_manifest_assets "$final_dir" published
	verify_published_release_and_prepare_tap "$final_dir"
	printf 'Published and externally reverified: %s\n' "$tag"
}

if [ "${HIKYO_CEREMONY_SOURCE_ONLY:-}" != true ]; then
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
	load_state

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
fi
