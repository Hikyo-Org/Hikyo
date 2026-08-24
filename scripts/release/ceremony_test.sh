#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-ceremony-test.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM

mkdir -p "$fixture_dir/bin" "$fixture_dir/state"
cat >"$fixture_dir/bin/uname" <<'EOF'
#!/bin/sh
printf 'Darwin\n'
EOF
chmod +x "$fixture_dir/bin/uname"

output=$(printf '1\n1.0.0-alpha.0\n' | env \
	PATH="$fixture_dir/bin:$PATH" \
	XDG_STATE_HOME="$fixture_dir/state" \
	"$script_dir/ceremony.sh" --dry-run)

printf '%s\n' "$output" | grep -F '1. One-time trust bootstrap' >/dev/null
printf '%s\n' "$output" | grep -F 'Dry run: bootstrap 1.0.0-alpha.0' >/dev/null
printf '%s\n' "$output" | grep -F 'copy only age-wrapped .key.age files to USB' >/dev/null
[ ! -e "$fixture_dir/state/hikyo/release-ceremony/1.0.0-alpha.0/state.json" ]

printf 'release ceremony fixture: dry-run bootstrap is mutation-free\n'

publish_output=$(printf '5\n1.0.0-alpha.0\n' | env \
	PATH="$fixture_dir/bin:$PATH" \
	XDG_STATE_HOME="$fixture_dir/state" \
	"$script_dir/ceremony.sh" --dry-run)

printf '%s\n' "$publish_output" | grep -F 'Dry run: publish 1.0.0-alpha.0' >/dev/null
printf '%s\n' "$publish_output" | grep -F 'would require exact typed confirmation: publish v1.0.0-alpha.0' >/dev/null

tag_output=$(printf '3\n1.0.0-alpha.0\n' | env \
	PATH="$fixture_dir/bin:$PATH" \
	XDG_STATE_HOME="$fixture_dir/state" \
	"$script_dir/ceremony.sh" --dry-run)
printf '%s\n' "$tag_output" | grep -F 'Would verify production trust and release fixtures before the immutable tag.' >/dev/null

if printf '1\n1.0.0_alpha.0\n' | env \
	PATH="$fixture_dir/bin:$PATH" \
	XDG_STATE_HOME="$fixture_dir/state" \
	"$script_dir/ceremony.sh" --dry-run >"$fixture_dir/invalid.out" 2>"$fixture_dir/invalid.err"
then
	printf 'release ceremony fixture: invalid SemVer was accepted\n' >&2
	exit 1
fi
grep -F 'invalid SemVer 1.0.0_alpha.0' "$fixture_dir/invalid.err" >/dev/null

printf 'release ceremony fixture: publish warning and SemVer refusal passed\n'

mkdir -p "$fixture_dir/state/hikyo/release-ceremony/1.0.0-alpha.1"
cat >"$fixture_dir/state/hikyo/release-ceremony/1.0.0-alpha.1/state.json" <<'EOF'
{"version":"1.0.0-alpha.1","tag":"v1.0.0-alpha.1","phase":"bound-merged","repository":"Hikyo-Org/Hikyo"}
EOF
resume_output=$(printf '4\n1.0.0-alpha.1\n' | env \
	PATH="$fixture_dir/bin:$PATH" \
	XDG_STATE_HOME="$fixture_dir/state" \
	"$script_dir/ceremony.sh" --dry-run)
printf '%s\n' "$resume_output" | grep -F 'Recorded progress: bound-merged' >/dev/null

printf 'release ceremony fixture: saved progress is loaded\n'

for fake_command in age cosign hdiutil; do
	cat >"$fixture_dir/bin/$fake_command" <<'EOF'
#!/bin/sh
exit 0
EOF
	chmod +x "$fixture_dir/bin/$fake_command"
done
cat >"$fixture_dir/bin/route" <<'EOF'
#!/bin/sh
exit 1
EOF
cat >"$fixture_dir/bin/diskutil" <<'EOF'
#!/bin/sh
if [ "${FAKE_DISK_LOCATION:-External}" = External ]; then
	printf 'Device Location: External\nPart of Whole: disk-%s\n' "$(basename "$2")"
else
	printf 'Device Location: Internal\nPart of Whole: disk-internal\n'
fi
EOF
chmod +x "$fixture_dir/bin/route" "$fixture_dir/bin/diskutil"
mkdir -p "$fixture_dir/primary-a" "$fixture_dir/primary-b" \
	"$fixture_dir/recovery-a" "$fixture_dir/recovery-b"

if printf '1\n1.0.0-alpha.0\noffline bootstrap 1.0.0-alpha.0\n%s\n%s\n%s\n%s\n' \
	"$fixture_dir/primary-a" "$fixture_dir/primary-b" \
	"$fixture_dir/recovery-a" "$fixture_dir/recovery-b" | env \
	PATH="$fixture_dir/bin:$PATH" \
	FAKE_DISK_LOCATION=Internal \
	XDG_STATE_HOME="$fixture_dir/state" \
	"$script_dir/ceremony.sh" >"$fixture_dir/internal.out" 2>"$fixture_dir/internal.err"
then
	printf 'release ceremony fixture: internal key storage was accepted\n' >&2
	exit 1
fi
grep -F 'USB volume is not external/removable' "$fixture_dir/internal.err" >/dev/null

printf 'release ceremony fixture: internal key storage is refused\n'

if printf '1\n1.0.0-alpha.0\noffline bootstrap 1.0.0-alpha.0\n%s\n%s\n%s\n%s\nwrong\n' \
	"$fixture_dir/primary-a" "$fixture_dir/primary-b" \
	"$fixture_dir/recovery-a" "$fixture_dir/recovery-b" | env \
	PATH="$fixture_dir/bin:$PATH" \
	XDG_STATE_HOME="$fixture_dir/state" \
	"$script_dir/ceremony.sh" >"$fixture_dir/refusal.out" 2>"$fixture_dir/refusal.err"
then
	printf 'release ceremony fixture: wrong confirmation was accepted\n' >&2
	exit 1
fi
grep -F 'confirmation did not match; nothing changed' "$fixture_dir/refusal.err" >/dev/null
[ ! -e "$fixture_dir/primary-a/primary-1.key.age" ]
[ ! -e "$fixture_dir/state/hikyo/release-ceremony/1.0.0-alpha.0/state.json" ]

printf 'release ceremony fixture: destructive bootstrap confirmation is fail-closed\n'

cat >"$fixture_dir/bin/fdesetup" <<'EOF'
#!/bin/sh
printf 'FileVault is %s.\n' "${FAKE_FILEVAULT_STATUS:-On}"
EOF
cat >"$fixture_dir/bin/hdiutil" <<'EOF'
#!/bin/sh
if [ "${1:-}" = detach ] && [ "${FAKE_DETACH_FAILURE:-false}" = true ]; then
	exit 1
fi
printf '/dev/disk-test\n'
EOF
cat >"$fixture_dir/bin/age" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$AGE_LOG"
cp "$4" "$3"
EOF
cat >"$fixture_dir/bin/gh" <<'EOF'
#!/bin/sh
printf '%s\n' "${FAKE_DRAFT_STATUS:-true}"
EOF
chmod +x "$fixture_dir/bin/fdesetup" "$fixture_dir/bin/hdiutil" \
	"$fixture_dir/bin/age" "$fixture_dir/bin/gh"

(
	PATH="$fixture_dir/bin:$PATH"
	XDG_STATE_HOME="$fixture_dir/library-state"
	HIKYO_CEREMONY_SOURCE_ONLY=true
	export PATH XDG_STATE_HOME HIKYO_CEREMONY_SOURCE_ONLY
	# shellcheck disable=SC1090,SC1091
	. "$script_dir/ceremony.sh"

	# shellcheck disable=SC2154
	[ "$trust_state_path" = "$fixture_dir/library-state/hikyo/release-trust.json" ]
	case "$trust_state_path" in *release-ceremony*) exit 1 ;; esac
	saved_phase=bound-local
	export saved_phase
	require_saved_phase bound-local
	if (require_saved_phase draft-verified) >"$fixture_dir/gate.out" 2>"$fixture_dir/gate.err"; then
		exit 1
	fi

	ram_device=/dev/disk-test
	FAKE_DETACH_FAILURE=true
	export FAKE_DETACH_FAILURE
	if cleanup_ram >"$fixture_dir/detach.out" 2>"$fixture_dir/detach.err"; then
		exit 1
	fi
	[ "$ram_device" = /dev/disk-test ]
	FAKE_DETACH_FAILURE=false
	export FAKE_DETACH_FAILURE
	cleanup_ram

	FAKE_FILEVAULT_STATUS=Off
	export FAKE_FILEVAULT_STATUS
	if (mount_ram 1) >"$fixture_dir/filevault.out" 2>"$fixture_dir/filevault.err"; then
		exit 1
	fi
	grep -F 'FileVault must be On' "$fixture_dir/filevault.err" >/dev/null

	printf 'cosign-encrypted-key\n' >"$fixture_dir/private.key"
	AGE_LOG="$fixture_dir/age.log"
	export AGE_LOG
	wrap_private_key "$fixture_dir/private.key" "$fixture_dir/private.key.age" PRIMARY >/dev/null
	cmp "$fixture_dir/private.key" "$fixture_dir/private.key.age"
	grep -F -- '--passphrase --output' "$fixture_dir/age.log" >/dev/null

	mkdir -p "$fixture_dir/fake-release" "$fixture_dir/local-release"
	cat >"$fixture_dir/fake-release/verify-bundle.sh" <<'EOF'
#!/bin/sh
exit "${FAKE_TRUST_VERIFY_STATUS:-0}"
EOF
	chmod +x "$fixture_dir/fake-release/verify-bundle.sh"
	script_dir=$fixture_dir/fake-release
	release_dir=$fixture_dir/local-release
	export script_dir release_dir
	FAKE_TRUST_VERIFY_STATUS=1
	export FAKE_TRUST_VERIFY_STATUS
	if verify_local_trust >"$fixture_dir/trust.out" 2>"$fixture_dir/trust.err"; then
		exit 1
	fi

	tag=v1.0.0-alpha.0
	repository=Hikyo-Org/Hikyo
	export tag repository
	FAKE_DRAFT_STATUS=false
	export FAKE_DRAFT_STATUS
	if (require_draft_release) >"$fixture_dir/draft.out" 2>"$fixture_dir/draft.err"; then
		exit 1
	fi
	grep -F 'release is not an unsigned draft' "$fixture_dir/draft.err" >/dev/null

	bundle=$fixture_dir/bundle
	mkdir "$bundle"
	artifacts=$fixture_dir/artifacts.jsonl
	: >"$artifacts"
	for binary_number in 1 2 3 4 5 6; do
		binary_name=binary-$binary_number.tar.gz
		printf 'binary-%s\n' "$binary_number" >"$bundle/$binary_name"
		binary_sha=$(shasum -a 256 "$bundle/$binary_name" | awk '{print $1}')
		jq -nc --arg name "$binary_name" --arg sha "$binary_sha" \
			'{name: $name, kind: "binary", sha256: $sha}' >>"$artifacts"
	done
	for payload_kind in image chart; do
		payload_name=$payload_kind.oci-payload.json
		printf '%s\n' "$payload_kind" >"$bundle/$payload_name"
		payload_sha=$(shasum -a 256 "$bundle/$payload_name" | awk '{print $1}')
		jq -nc --arg name "$payload_name" --arg sha "$payload_sha" \
			'{name: $name, kind: "oci-payload", sha256: $sha}' >>"$artifacts"
	done
	jq -s '{schema: "hikyo.dev/release-manifest/v1", version: "1.0.0-alpha.0",
		tag: "v1.0.0-alpha.0", artifacts: .}' "$artifacts" >"$bundle/release-manifest.json"
	version=1.0.0-alpha.0
	export version
	printf 'rogue\n' >"$bundle/unmanifested.txt"
	if (validate_manifest_assets "$bundle" unsigned) >"$fixture_dir/assets.out" 2>"$fixture_dir/assets.err"; then
		exit 1
	fi
	grep -F 'unmanifested file: unmanifested.txt' "$fixture_dir/assets.err" >/dev/null
	rm "$bundle/unmanifested.txt"
	: >"$bundle/checksums.txt"
	duplicate_sha=$(shasum -a 256 "$bundle/binary-1.tar.gz" | awk '{print $1}')
	for _ in 1 2 3 4 5 6; do
		printf '%s  binary-1.tar.gz\n' "$duplicate_sha" >>"$bundle/checksums.txt"
	done
	if (validate_checksum_manifest "$bundle" "$bundle/release-manifest.json") \
		>"$fixture_dir/checksums.out" 2>"$fixture_dir/checksums.err"; then
		exit 1
	fi
	grep -F 'exact six manifest binaries once each' "$fixture_dir/checksums.err" >/dev/null
	: >"$bundle/checksums.txt"
	for binary_number in 1 2 3 4 5 6; do
		binary_name=binary-$binary_number.tar.gz
		binary_sha=$(shasum -a 256 "$bundle/$binary_name" | awk '{print $1}')
		printf '%s  %s\n' "$binary_sha" "$binary_name" >>"$bundle/checksums.txt"
	done
	validate_checksum_manifest "$bundle" "$bundle/release-manifest.json"
)

printf 'release ceremony fixture: resume and security gates fail closed\n'
