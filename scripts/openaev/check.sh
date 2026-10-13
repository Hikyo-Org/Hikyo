#!/usr/bin/env bash
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
# Install requirements-check.lock into the checking environment before running.
python3 -m ruff check --no-cache "$script_dir"
python3 -m mypy --cache-dir "${TMPDIR:-/tmp}/hikyo-openaev-mypy-cache" --exclude '/test_[^/]+\.py$' "$script_dir"
python3 -m unittest discover -s "$script_dir" -p 'test_*.py'
python3 -m unittest discover -s "$script_dir/publish" -p 'test_*.py'
python3 -m unittest discover -s "$script_dir/stack" -p 'test_*.py'
