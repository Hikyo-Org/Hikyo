#!/bin/sh
# Explicit installation ceremony. This helper is never invoked by the legacy
# binary installer and cannot silently enroll an existing manual deployment.
set -eu

if [ "$#" -eq 1 ] && [ "$1" = "--help" ]; then
	printf '%s\n' 'Usage: sudo sh install/security-maintenance/install-systemd.sh'
	printf '%s\n' 'Install and enroll the configured automatic/offline recipe, or keep manual mode disabled.'
	exit 0
fi
if [ "$#" -ne 0 ]; then
	printf '%s\n' 'Unexpected arguments; use --help.' >&2
	exit 2
fi
if [ "$(uname -s)" != Linux ] || [ "$(id -u)" -ne 0 ]; then
	printf '%s\n' 'This installation ceremony requires Linux root and systemd.' >&2
	exit 1
fi
for required in systemctl install jq; do
	command -v "$required" >/dev/null 2>&1 || {
		printf '%s\n' "$required is required" >&2
		exit 1
	}
done
binary=/usr/bin/hikyo
configuration=/etc/hikyo/security-maintenance.json
if [ ! -x "$binary" ]; then
	if [ -x /usr/local/bin/hikyo ]; then
		printf '%s\n' 'Found /usr/local/bin/hikyo from the binary installer, but the maintenance unit and native upgrades run the packaged /usr/bin/hikyo.' >&2
		printf '%s\n' 'Install the signed release package (deb, rpm, apk or Arch): https://hikyo.app/docs/installation/#native-packages' >&2
	else
		printf '%s\n' 'Install the signed Hikyo release package first; it provides /usr/bin/hikyo: https://hikyo.app/docs/installation/#native-packages' >&2
	fi
	exit 1
fi

# The binary owns strict schema, path, ownership and policy validation.
"$binary" security-maintenance status --config "$configuration" >/dev/null
mode=$(jq -er '.policy.mode | select(. == "automatic" or . == "manual" or . == "offline")' "$configuration")
printf '%s\n' "Security-maintenance mode: $mode."
printf '%s\n' 'Automatic maintenance grants the separate local coordinator restart and replacement authority.'
printf '%s\n' 'Compatible security fixes may include feature upgrades and database maintenance under local policy.'
printf '%s\n' 'Pins, configured windows, artifact trust and recovery safeguards remain binding.'

# Units first: they are inert until enabled, so a failure here records no consent
# and a failed enrollment can be fixed and rerun without an enrolled, unscheduled host.
script_directory=$(CDPATH='' cd "$(dirname "$0")" && pwd)
install -m 0644 "$script_directory/../systemd/hikyo-security-maintenance.service" /etc/systemd/system/hikyo-security-maintenance.service
install -m 0644 "$script_directory/../systemd/hikyo-security-maintenance.timer" /etc/systemd/system/hikyo-security-maintenance.timer
systemctl daemon-reload
if [ "$mode" = manual ]; then
	systemctl disable --now hikyo-security-maintenance.timer
	printf '%s\n' 'Manual mode retained; the security-maintenance timer is disabled.'
else
	"$binary" security-maintenance enroll --config "$configuration"
	# Started by enrollment; without this a reboot leaves the service down.
	if [ "$(jq -er .kind "$configuration")" = native ]; then
		systemctl enable "$(jq -r '.unit // "hikyo.service"' "$(jq -er .native_config "$configuration")")"
	fi
	systemctl enable --now hikyo-security-maintenance.timer
	printf '%s\n' 'Maintenance enrolled and scheduled. Inspect durable status with sudo hikyo security-maintenance status.'
fi
