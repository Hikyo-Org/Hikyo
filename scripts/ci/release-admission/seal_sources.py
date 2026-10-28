"""Seal static successor bytes, excluding approval/configuration/runtime data."""
import hashlib
import json
from pathlib import Path


HERE = Path(__file__).resolve().parent
EXCLUDED = {'IMPLEMENTATION-MANIFEST.json', 'EXECUTION-MAP.json'}
SUFFIXES = {'.py', '.js', '.mjs', '.cjs', '.json'}
ROOT_JSON = {'BASELINE-SOURCE-PINS.json', 'HISTORICAL-ARCHIVE-PINS.json'}


def raw_pin(path):
    raw = path.read_bytes()
    return {'bytes': len(raw), 'rawSHA256': hashlib.sha256(raw).hexdigest()}


def static_files(root):
    result = {}
    for path in sorted(root.rglob('*')):
        relative = path.relative_to(root)
        if ('__pycache__' in relative.parts or path.name in EXCLUDED
                or path.suffix not in SUFFIXES or not path.is_file()):
            continue
        if path.suffix == '.json' and len(relative.parts) == 1 and path.name not in ROOT_JSON:
            continue
        if path.is_symlink():
            raise ValueError('release-seal:symlink-source')
        result[relative.as_posix()] = raw_pin(path)
    return result


def seal(root=HERE):
    files = static_files(root)
    manifest = {'schema': 'hikyo-158/release-implementation/v1',
                'files': {name: row['rawSHA256'] for name, row in files.items()}}
    raw = (json.dumps(manifest, sort_keys=True, indent=2) + '\n').encode()
    (root / 'IMPLEMENTATION-MANIFEST.json').write_bytes(raw)
    names = {'model': 'core/next_fixture_model.source-only.py',
             'executor': 'core/next_fixture_execution.source-only.py',
             'ledger': 'core/next_fixture_ledger.source-only.py',
             'runtime': 'core/next_fixture_runtime_bridge.source-only.py',
             'operator': 'core/next_operator_orchestration_stdout.source-only.py',
             'result': 'core/next_hosted_result.source-only.py',
             'finalizer': 'core/next_owner_finalization.source-only.py',
             'configuration': 'core/source_prepared_entrypoint.source-only.py',
             'collector': 'release_collector.py'}
    modules = {name: {'path': path, **files[path]} for name, path in names.items()}
    mapping = {'schema': 'hikyo-1046/unconfigured-execution-map/v1',
               'runtimeReady': False, 'sourceAdopted': False,
               'executionAuthorityGranted': False, 'actualConfiguration': None,
               'implementationManifestRawSHA256': hashlib.sha256(raw).hexdigest(),
               'implementationManifestSHA256': hashlib.sha256(json.dumps(
                   manifest, sort_keys=True, separators=(',', ':'), ensure_ascii=True).encode()).hexdigest(),
               'sourceFiles': files,
               'modules': modules}
    (root / 'EXECUTION-MAP.json').write_text(json.dumps(mapping, sort_keys=True, indent=2) + '\n')
    return mapping


if __name__ == '__main__':
    seal()
