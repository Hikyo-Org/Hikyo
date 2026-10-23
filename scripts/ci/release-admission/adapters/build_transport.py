"""Materialize exact R2 source closure with reviewed successor script registry.

The 14 MB official package remains external. Explicit local output only;
no process, profile, network, native store or provider access occurs here.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import stat

HERE = Path(__file__).resolve().parent


def canonical(value):
    return json.dumps(value, sort_keys=True, indent=2).encode() + b'\n'


def exact_read(path, row):
    if any(p.is_symlink() for p in (path, *path.parents)):
        raise ValueError('adapter-transport:plain-original')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_size != row['bytes']:
            raise ValueError('adapter-transport:regular-whole-original')
        raw = stream.read(row['bytes'] + 1)
    if len(raw) != row['bytes'] or hashlib.sha256(raw).hexdigest() != row['rawSHA256']:
        raise ValueError('adapter-transport:exact-source-pin')
    return raw


def build(source_root, output):
    if (not source_root.is_absolute() or not output.is_absolute() or output.exists()
            or output.is_relative_to(HERE.parent)):
        raise ValueError('adapter-transport:explicit-fresh-external-output')
    index = json.loads((HERE / 'TRANSPORT-ORIGINS.json').read_bytes())
    original = source_root / 'chrome-capture-suppressed-persistent-owner-source'
    # Validate the entire closure before publishing even the first file.
    files = {relative: exact_read(original / relative, row) for relative, row in index['files'].items()}
    base = json.loads(files['review-manifest.json'])
    by_id = {row['id']: row for row in base['scripts']}
    for leaf in ('prepare-registry.json', 'stage-registry.json', 'cleanup-registry.json'):
        for row in json.loads((HERE / leaf).read_bytes())['scripts']:
            by_id[row['id']] = row
            files[row['file']] = (HERE / row['file']).read_bytes()
    guard = json.loads((HERE / 'U-GUARD-REGISTRY.json').read_bytes())
    by_id[guard['id']] = guard
    files[guard['file']] = exact_read(HERE / guard['file'], {'bytes': (HERE / guard['file']).stat().st_size, 'rawSHA256': guard['sha256']})
    if len(by_id) > 64:
        raise ValueError('adapter-transport:closed-registry-cap')
    files['review-manifest.json'] = canonical({'schema': 'hikyo-158/reviewed-owner-scripts/v1',
                                             'scripts': list(by_id.values())})
    rows = {relative: {'sha256': hashlib.sha256(raw).hexdigest(), 'bytes': len(raw)}
            for relative, raw in files.items()}
    files['source-manifest.json'] = canonical({'schema': 'hikyo-158/persistent-owner-source-manifest/v1', 'files': rows})
    output.mkdir(mode=0o700, parents=False, exist_ok=False)
    for relative, raw in files.items():
        path = output / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(raw)
    return {'files': len(files), 'bytes': sum(map(len, files.values())),
            'sourceManifestRawSHA256': hashlib.sha256(files['source-manifest.json']).hexdigest(),
            'reviewManifestRawSHA256': hashlib.sha256(files['review-manifest.json']).hexdigest()}


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--source-root', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    print(json.dumps(build(args.source_root, args.output), sort_keys=True))
