#!/usr/bin/env bash
# Called only from trusted default-branch tooling inside the absolute work cutoff.
set -euo pipefail
[[ $# == 0 ]]
: "${RUNNER_TEMP:?missing runner temporary directory}"
: "${RUNNER_TOOL_CACHE:?missing trusted runner tool cache}"
[[ "$RUNNER_TOOL_CACHE" == /* ]]
pin=$(awk '
  { sub(/\/\/.*$/, "") }
  $1 == "go" { if (NF != 2 || ++n != 1) exit 1; version=$2 }
  $1 == "toolchain" { if (NF != 2 || ++t != 1) exit 1; toolchain=$2 }
  END { if (n != 1 || (t && toolchain != "go" version)) exit 1; print version }
' go.mod)
[[ "$pin" =~ ^1\.([0-9]+)\.(0|[1-9][0-9]*)$ ]]
[[ ${BASH_REMATCH[1]} != 0* ]]
(( 10#${BASH_REMATCH[1]} >= 21 ))
# Ignore persisted settings and prevent workspace, flags or private-proxy overrides.
export GOENV=off GOWORK=off GOFLAGS='' GOPROXY=https://proxy.golang.org
export GOSUMDB=sum.golang.org GOPRIVATE='' GONOPROXY='' GONOSUMDB=''
case "${RUNNER_ARCH:?missing runner architecture}" in
  X64) arch=x64; host_arch=amd64 ;;
  ARM64) arch=arm64; host_arch=arm64 ;;
  *) exit 1 ;;
esac
bootstrap_go=
# Prefer an already cached exact target; otherwise use a complete supported SDK.
for sdk in "$RUNNER_TOOL_CACHE/go/$pin/$arch" "$RUNNER_TOOL_CACHE"/go/*/"$arch"; do
  version=${sdk%/*}; version=${version##*/}
  [[ "$version" =~ ^1\.([0-9]+)\.(0|[1-9][0-9]*)$ ]] || continue
  [[ ${BASH_REMATCH[1]} != 0* ]] || continue
  (( 10#${BASH_REMATCH[1]} >= 21 )) || continue
  [[ -f "$sdk.complete" && -x "$sdk/bin/go" ]] || continue
  bootstrap=$(GOTOOLCHAIN=local "$sdk/bin/go" env GOVERSION GOHOSTARCH)
  [[ "$bootstrap" == "go$version"$'\n'"$host_arch" ]]
  bootstrap_go="$sdk/bin/go"
  break
done
[[ -n "$bootstrap_go" ]]
export PATH="${bootstrap_go%/*}:$PATH" GOTOOLCHAIN="go$pin"
# Native Go downloads and verifies the exact toolchain without another SDK copy.
[[ "$("$bootstrap_go" env GOVERSION)" == "go$pin" ]]
"$bootstrap_go" build -o "$RUNNER_TEMP/queue-publisher" ./scripts/ci/pr-gate
