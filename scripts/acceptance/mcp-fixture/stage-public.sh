#!/bin/sh
# Only public, already bound bundle bytes. Projected ConfigMap symlinks are
# followed on input; destination documents are regular files for O_NOFOLLOW.
set -eu
umask 077
test "$#" -ge 3
source_dir=$1
destination=$2
shift 2
test -d "$destination"
test ! -L "$destination"
for entry do
  relative=${entry%:*}
  expected=${entry##*:}
  case "$relative" in operator.pub|bundle/*) ;; *) exit 1 ;; esac
  case "$relative" in *[!A-Za-z0-9._/-]*|../*|*/../*|*/..|./*|*/./*|*/.) exit 1 ;; esac
  test "${#expected}" -eq 64
  case "$expected" in *[!a-f0-9]*) exit 1 ;; esac
  test "$(sha256sum "$source_dir/$relative" | cut -d ' ' -f 1)" = "$expected"
  mkdir -p "$destination/$(dirname "$relative")"
  target=$destination/$relative
  test ! -L "$target"
  if [ ! -e "$target" ]; then
    cp -L "$source_dir/$relative" "$target"
    chmod 600 "$target"
  fi
  test -f "$target"
  test "$(sha256sum "$target" | cut -d ' ' -f 1)" = "$expected"
done
