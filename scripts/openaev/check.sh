#!/usr/bin/env bash
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
python3 -m ruff check --no-cache "$script_dir"
python3 -m mypy --exclude '/test_[^/]+\.py$' --cache-dir "${TMPDIR:-/tmp}/openaev-mypy" "$script_dir"
python3 -m unittest discover -s "$script_dir" -p 'test_*.py'
