"""Build native closed registries and seal actual stable adapter/model sources.

Run after parent core edits. Never reads release configuration or runtime data.
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent


def pin(path):
    raw = path.read_bytes()
    return {'bytes': len(raw), 'rawSHA256': hashlib.sha256(raw).hexdigest()}


def write(path, value):
    path.write_text(json.dumps(value, sort_keys=True, indent=2) + '\n')


def seal(*, node_path, root=ROOT):
    here = root / 'adapters'
    names = {'modelSHA256': root / 'core/next_fixture_model.source-only.py',
             'modelTestSHA256': root / 'core/test_next_fixture_model.py',
             'guardSHA256': here / 'native-guard.source-only.js',
             'observerSHA256': here / 'native-observer.source-only.js',
             'windowSHA256': here / 'shared-window.cjs',
             'documentSHA256': here / 'guarded-document.cjs',
             'kernelSHA256': here / 'stage-kernel.cjs'}
    stage = {name: pin(path)['rawSHA256'] for name, path in names.items()}
    write(here / 'STAGE-SOURCE-PINS.json', stage)
    subprocess.run([str(node_path), str(here / 'stage_build.cjs')], check=True,
                   cwd=here, stdin=subprocess.DEVNULL, timeout=15)
    subprocess.run([str(node_path), str(here / 'cleanup_build.cjs')], check=True,
                   cwd=here, stdin=subprocess.DEVNULL, timeout=15)
    # Refresh actual A/H/U script raw pins after derivation, including the
    # whitespace-normalized U successor. Original archive pins stay unchanged.
    for leaf in ('prepare-registry.json', 'create-registry.json', 'U-GUARD-REGISTRY.json'):
        record = json.loads((here / leaf).read_bytes())
        rows = record['scripts'] if 'scripts' in record else [record]
        for row in rows:
            row['sha256'] = pin(here / row['file'])['rawSHA256']
        write(here / leaf, record)
    files = {}
    for folder in (here, root / 'core'):
        for path in sorted(folder.rglob('*')):
            if (not path.is_file() or '__pycache__' in path.parts
                    or path == here / 'SOURCE-PINS.json'
                    or path.suffix not in ('.py', '.js', '.mjs', '.cjs', '.json')):
                continue
            if path.is_symlink():
                raise ValueError('adapter-seal:plain-source-only')
            files[path.relative_to(root).as_posix()] = pin(path)
    files['package_adapters.py'] = pin(root / 'package_adapters.py')
    files['test_adapter_context.py'] = pin(root / 'test_adapter_context.py')
    files['test_adapter_source_loader.py'] = pin(root / 'test_adapter_source_loader.py')
    result = {'schema': 'hikyo-1046/portable-adapter-sources/v1', 'files': files, 'stagePins': stage}
    write(here / 'SOURCE-PINS.json', result)
    return pin(here / 'SOURCE-PINS.json')['rawSHA256']


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--node', required=True, type=Path)
    args = parser.parse_args()
    if not args.node.is_absolute():
        raise ValueError('adapter-seal:explicit-node-path')
    print(seal(node_path=args.node))
