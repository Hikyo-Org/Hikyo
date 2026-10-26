"""Finite source checks; full qualification requires an explicit original archive."""
import argparse
import ast
import hashlib
import importlib.metadata
import os
from pathlib import Path
import sys
import unittest


HERE = Path(__file__).resolve().parent
UNIT_MODULES = ('test_release_records', 'test_release_collector', 'test_decision_journal',
                'test_workflow_contract', 'test_successor_loader', 'test_historical_integrity',
                'test_repository_source_closure',
                'test_adapter_source_loader',
                'test_aws_acceptance_contract',
                'core.test_next_fixture_model')


def main():
    parser = argparse.ArgumentParser()
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--unit', action='store_true')
    mode.add_argument('--qualify', action='store_true')
    parser.add_argument('--archive-root', type=Path)
    args = parser.parse_args()
    if importlib.metadata.version('PyYAML') != '6.0.3':
        parser.error('install the pinned release-admission requirements')
    if args.qualify and (args.archive_root is None or not args.archive_root.is_absolute()):
        parser.error('--qualify requires an explicit absolute --archive-root; missing originals are not qualification')
    if args.unit and args.archive_root is not None:
        parser.error('--archive-root belongs to --qualify')
    for path in HERE.rglob('*.py'):
        if '__pycache__' not in path.parts:
            ast.parse(path.read_bytes(), filename=str(path))
    if args.qualify:
        os.environ['HIKYO_158_ARCHIVE_ROOT'] = str(args.archive_root)
    modules = UNIT_MODULES if args.unit else ('test_two_release_pipeline', 'test_adapter_context')
    for name in modules:
        if not (HERE / (name.replace('.', '/') + '.py')).is_file():
            parser.error('required check is missing: ' + name)
    sys.path.insert(0, str(HERE))
    from load_successor import bootstrap_helpers
    # This finite source runner computes integrity pins only. It neither
    # supplies independent source review nor grants operational adoption.
    bootstrap_helpers(
        reviewed_map_sha256=hashlib.sha256((HERE / 'EXECUTION-MAP.json').read_bytes()).hexdigest(),
        reviewed_manifest_raw_sha256=hashlib.sha256((HERE / 'IMPLEMENTATION-MANIFEST.json').read_bytes()).hexdigest())
    suite = unittest.defaultTestLoader.loadTestsFromNames(modules)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    if result.skipped:
        print('Required checks were skipped; source qualification is incomplete.', file=sys.stderr)
        return 1
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
