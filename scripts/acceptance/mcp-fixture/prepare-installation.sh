#!/bin/sh
# MANUAL STORAGE OWNER ONLY, after resource approval and exact PVC/PV mapping.
# Run on the Linux storage host against ONLY the new installation-authority
# claim's verified directory. Never run against an existing installation.
set -eu
if [ "$#" -ne 2 ]; then
  printf 'usage: prepare-installation.sh ABSOLUTE_EMPTY_PV_DIRECTORY VERIFIED_PVC_UID\n' >&2
  exit 2
fi
target=$1
pvc_uid=$2
case "$target" in /|/mnt|/var|/srv|/home|/tmp|*/../*|*/..|*/./*|*/.) exit 2 ;; /*) ;; *) exit 2 ;; esac
case "$pvc_uid" in *[!a-f0-9-]*|'') exit 2 ;; esac
test "${#pvc_uid}" -eq 36
test "$(id -u)" -eq 0
test -d "$target"
test ! -L "$target"
test "$(realpath "$target")" = "$target"
test -z "$(find "$target" -mindepth 1 -maxdepth 1 -print -quit)"
umask 077
chown 0:65532 "$target"
chmod 2770 "$target"
install -d -m 0700 -o 65532 -g 65532 "$target/operator-state"
printf '%s\n' "$pvc_uid" > "$target/fixture-pvc.uid"
chmod 600 "$target/fixture-pvc.uid"
test "$(stat -c '%a %g' "$target")" = '2770 65532'
test "$(stat -c '%a %u' "$target/operator-state")" = '700 65532'
printf 'Fresh installation directory prepared. Verify remount preservation and cross-node locks before admission.\n'
