#!/usr/bin/env bash
set -euo pipefail
if [[ "$#" != 4 ]]; then
  echo 'usage: run-hosted.sh TAG SOURCE_SHA IMAGE_REF RESULT_DIR' >&2
  exit 2
fi
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
python3 "$script_dir/run.py" --release-json "$4/release.json" --state "$4/exercise" \
  --tag "$1" --source-sha "$2" --image-ref "$3"
