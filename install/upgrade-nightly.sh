#!/bin/sh
# First-use bridge for an installed Hikyo binary without `hikyo upgrade`.
# HTTPS delivers this script. Pinned Cosign authenticates all executable bytes
# downloaded by it before the coordinator touches the installed deployment.
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

fail() { printf 'hikyo upgrade bootstrap: %s\n' "$*" >&2; exit 1; }
[ "$(id -u)" = 0 ] || fail 'run this bootstrap with sudo sh'
[ "$(uname -s)" = Linux ] || fail 'only Linux systemd is supported'
[ -d /run/systemd/system ] || fail 'systemd must be running on this host'
for command_name in curl python3 openssl; do
	command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

case "$(uname -m)" in
	x86_64 | amd64)
		arch=amd64
		cosign_sha256=4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71
		;;
	aarch64 | arm64)
		arch=arm64
		cosign_sha256=c5d324e091826b0d7a78eb16fef316450b4eb9aaec045611c08ba06f5e73220a
		;;
	*) fail 'only Linux amd64 and arm64 are supported' ;;
esac
trusted_root_sha256=6494e21ea73fa7ee769f85f57d5a3e6a08725eae1e38c755fc3517c9e6bc0b66
repository=Hikyo-Org/Hikyo

# Use a root-owned hierarchy, not a world-writable /tmp ancestor: the verified
# coordinator independently enforces ownership before staging an executable.
scratch=$(python3 - <<'PY'
import os, pathlib, stat, tempfile
root = pathlib.Path('/var/lib/hikyo-upgrader')
for parent in reversed(root.parents):
    info = parent.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
        raise SystemExit('unsafe bootstrap parent directory')
try:
    root.mkdir(mode=0o700)
except FileExistsError:
    pass
info = root.lstat()
if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o077:
    raise SystemExit('bootstrap state directory must be root-owned mode0700')
print(tempfile.mkdtemp(prefix='bootstrap-', dir=root))
PY
)
trap 'rm -rf "$scratch"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

cat >"$scratch/verify.py" <<'PY'
# BEGIN BOOTSTRAP VERIFIER
import hashlib
import base64
import fcntl
import json
import os
import pathlib
import re
import shutil
import subprocess
import stat
import sys
import tarfile
import tempfile

def fail(message):
    raise SystemExit('hikyo upgrade bootstrap: ' + message)

def unique_object(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            fail('duplicate JSON field')
        value[key] = item
    return value

def document(path):
    with open(path, 'rb') as source:
        raw = source.read(4 * 1024 * 1024 + 1)
    if len(raw) > 4 * 1024 * 1024:
        fail('JSON input exceeds 4 MiB')
    return json.loads(raw, object_pairs_hook=unique_object)

def digest(path):
    result = hashlib.sha256()
    with open(path, 'rb') as source:
        for block in iter(lambda: source.read(1024 * 1024), b''):
            result.update(block)
    return result.hexdigest()

tag_pattern = re.compile(r'v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)-nightly\.[0-9]{8}\.([1-9][0-9]*)\.g[0-9a-f]{8}')
sha_pattern = re.compile(r'[0-9a-f]{64}')

identity_policy = {
    'trusted_root_sha256': '6494e21ea73fa7ee769f85f57d5a3e6a08725eae1e38c755fc3517c9e6bc0b66',
    'issuer': 'https://token.actions.githubusercontent.com',
    'repository_uri': 'https://github.com/Hikyo-Org/Hikyo',
    'repository_id': '1316165429', 'repository_owner_uri': 'https://github.com/Hikyo-Org',
    'repository_owner_id': '316726515', 'runner_environment': 'github-hosted', 'require_sct': True,
    'rekor_log_id': 'c0d23d6ad406973f9559f3ba2d1ca01f84147d8ffc5b8445c224f98b9591801d',
    'checkpoint_origin': 'rekor.sigstore.dev - 1193050959916656506',
}
stable_tag_pattern = re.compile(r'v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?')
key_name_pattern = re.compile(r'[A-Za-z0-9._-]+')

def digest_inventory(value, limit):
    return isinstance(value, list) and len(value) <= limit and all(isinstance(item, str) and sha_pattern.fullmatch(item) for item in value) and len(set(value)) == len(value)

def recovery_signature(directory, name):
    # OpenSSL verifies the existing key-based Cosign envelope. The envelope
    # cannot supply a key; only the independently pinned recovery key is used.
    envelope = document(directory / (name + '.sigstore.json'))
    if not isinstance(envelope, dict) or not set(envelope) <= {'base64Signature', 'cert', 'rekorBundle'}:
        fail('invalid recovery signature envelope')
    signature = base64.b64decode(envelope['base64Signature'], validate=True)
    if not 1 <= len(signature) <= 16384:
        fail('invalid recovery signature size')
    signature_path = directory / (name + '.signature')
    signature_path.write_bytes(signature)
    subprocess.run(['openssl', 'dgst', '-sha256', '-verify', str(directory / 'recovery-1.pub'), '-signature', str(signature_path), str(directory / (name + '.json'))], check=True, capture_output=True, timeout=10)

def stable_policy(directory):
    # Only a recovery-signed policy delegates routine stable signing. This
    # mirrors the Go verifier's verifyStablePolicy and StablePolicy.Validate.
    recovery_signature(directory, 'stable-policy')
    policy = document(directory / 'stable-policy.json')
    expected = dict(identity_policy, schema='hikyo.dev/stable-policy/v1', workflow_path='.github/workflows/release.yml', protected_ref='refs/tags/v*')
    if not isinstance(policy, dict) or set(policy) != set(expected) | {'revoked_manifests', 'minimum_release_sequence', 'primary_keys_sha256', 'nightly_policies', 'bridges'} or any(type(policy[key]) is not type(value) or policy[key] != value for key, value in expected.items()):
        fail('stable policy differs from this bootstrap verifier')
    if type(policy['minimum_release_sequence']) is not int or policy['minimum_release_sequence'] < 1 or not isinstance(policy['primary_keys_sha256'], str) or not sha_pattern.fullmatch(policy['primary_keys_sha256']) or not digest_inventory(policy['revoked_manifests'], 256) or not digest_inventory(policy['nightly_policies'], 256) or not digest_inventory(policy['bridges'], 1024):
        fail('invalid stable delegation')
    root = directory / 'stable-trusted-root.json'
    if not 0 < root.stat().st_size <= 4 * 1024 * 1024 or digest(root) != policy['trusted_root_sha256']:
        fail('stable Sigstore root missing or substituted')
    return policy

def primary_keys_digest(keys):
    # Go's json.Marshal of []Primary: fixed field order, pending omitted when null.
    names = ('id', 'public_key', 'sha256', 'valid_from_release_sequence', 'valid_through_release_sequence', 'revoked')
    if not isinstance(keys, list) or not keys:
        fail('invalid primary key inventory')
    ordered = []
    for key in keys:
        if not isinstance(key, dict) or not set(names) <= set(key) <= set(names) | {'pending'} or not all(isinstance(key[name], str) and key_name_pattern.fullmatch(key[name]) for name in ('id', 'public_key', 'sha256')):
            fail('invalid primary key entry')
        entry = {name: key[name] for name in names}
        if key.get('pending') is not None:
            entry['pending'] = key['pending']
        ordered.append(entry)
    return hashlib.sha256(json.dumps(ordered, separators=(',', ':'), ensure_ascii=False).encode()).hexdigest()

def delegated_metadata(metadata, policy):
    # Mirrors validateDelegatedMetadata and StablePolicy.verify's tag check.
    highest, sequence = metadata.get('highest_release'), metadata.get('highest_release_sequence')
    tag, commit = metadata.get('release_tag'), metadata.get('source_commit')
    if metadata['event'].get('type') != 'release' or metadata.get('pending_release') is not None or not isinstance(highest, str) or type(sequence) is not int or sequence < policy['minimum_release_sequence'] or tag != 'v' + str(highest) or not isinstance(commit, str) or not re.fullmatch(r'[0-9a-f]{40}', commit) or primary_keys_digest(metadata.get('primary_keys')) != policy['primary_keys_sha256']:
        fail('stable workflow metadata exceeds delegated authority')
    releases = metadata.get('releases')
    if not isinstance(releases, list) or not all(isinstance(release, dict) and type(release.get('sequence')) is int for release in releases):
        fail('invalid stable release inventory')
    if any(release['sequence'] > sequence for release in releases) or not any(release['sequence'] == sequence and release.get('version') == highest for release in releases):
        fail('stable metadata highest release is absent or exceeded')
    prerelease = tag.partition('-')[2]
    if len(tag) > 256 or not stable_tag_pattern.fullmatch(tag) or prerelease == 'nightly' or prerelease.startswith('nightly.'):
        fail('stable signature requires exact stable tag and commit')
    return tag, commit

def stable_workflow_signature(cosign, directory, name, policy, tag, commit):
    # Cosign owns bundle, certificate chain, SCT and transparency-log checks
    # (all on by default); identity comes only from the authenticated policy and
    # metadata. Argv list, no shell: document fields never reach a shell.
    ref = 'refs/tags/' + tag
    bundle = directory / (name + '.sigstore.json')
    argv = [str(cosign), 'verify-blob', '--bundle', str(bundle), '--trusted-root', str(directory / 'stable-trusted-root.json'),
        '--certificate-identity', policy['repository_uri'] + '/' + policy['workflow_path'] + '@' + ref,
        '--certificate-oidc-issuer', policy['issuer'],
        '--certificate-github-workflow-repository', policy['repository_uri'][len('https://github.com/'):],
        '--certificate-github-workflow-ref', ref, '--certificate-github-workflow-sha', commit, str(directory / (name + '.json'))]
    result = subprocess.run(argv, stdin=subprocess.DEVNULL, capture_output=True, timeout=120)
    if result.returncode != 0:
        fail('stable workflow signature verification failed: ' + name)
    certificate_policy(bundle, commit, ref, policy['workflow_path'])

def recovery_authorization(directory, manifest_path, state_directory, cosign=None):
    directory, state_directory = pathlib.Path(directory), pathlib.Path(state_directory)
    recovery_hash = '1eb7ad2092668b73621c21a1eeb801ed6391bc794df1909abce8e1d45e03a229'
    if digest(directory / 'recovery-1.pub') != recovery_hash:
        fail('recovery public key differs from bootstrap pin')
    # The signer field is unauthenticated: it only selects which verification
    # must succeed. Unknown signers and any failed verification are refused.
    metadata, catalog, policy = (document(directory / name) for name in ('metadata.json', 'catalog.json', 'policy.json'))
    if not isinstance(metadata, dict) or not isinstance(catalog, dict):
        fail('invalid trust documents')
    signer = metadata.get('event', {}).get('signed_by')
    workflow = signer == 'github-actions-stable'
    if metadata.get('schema') != 'hikyo.dev/trust-metadata/v1' or metadata.get('recovery') != {'id': 'recovery-1', 'sha256': recovery_hash} or signer not in ('recovery-1', 'github-actions-stable'):
        fail('invalid trust metadata identity')
    delegation = workflow or 'stable_policy_sha256' in catalog
    stable = stable_policy(directory) if delegation else None
    if workflow:
        if cosign is None:
            fail('stable workflow metadata requires the pinned Cosign verifier')
        tag, commit = delegated_metadata(metadata, stable)
        for name in ('metadata', 'catalog'):
            stable_workflow_signature(cosign, directory, name, stable, tag, commit)
    else:
        if 'source_commit' in metadata or 'release_tag' in metadata:
            fail('legacy metadata carries delegated identity')
        for name in ('metadata', 'catalog'):
            recovery_signature(directory, name)
    if not {'schema', 'sequence', 'stable_metadata_sha256', 'nightly_policies', 'bridges'} <= set(catalog) <= {'schema', 'sequence', 'stable_metadata_sha256', 'nightly_policies', 'bridges', 'stable_policy_sha256'} or catalog['schema'] != 'hikyo.dev/upgrade-trust/v1' or catalog['stable_metadata_sha256'] != digest(directory / 'metadata.json'):
        fail('catalog does not bind the authenticated metadata')
    if delegation and catalog.get('stable_policy_sha256') != digest(directory / 'stable-policy.json'):
        fail('catalog does not bind exact stable delegation')
    for name, limit in (('nightly_policies', 256), ('bridges', 1024)):
        if not digest_inventory(catalog[name], limit):
            fail('invalid authorized digest inventory')
    if workflow and (catalog['nightly_policies'] != stable['nightly_policies'] or catalog['bridges'] != stable['bridges']):
        fail('stable workflow changed nightly or bridge authority')
    if digest(directory / 'policy.json') not in catalog['nightly_policies']:
        fail('nightly policy is not currently authorized by the trust catalog')
    expected_policy = dict(identity_policy, schema='hikyo.dev/nightly-policy/v1', workflow_path='.github/workflows/nightly.yml', protected_ref='refs/heads/main')
    if not isinstance(policy, dict) or set(policy) != set(expected_policy) | {'revoked_manifests'} or any(type(policy[key]) is not type(value) or policy[key] != value for key, value in expected_policy.items()):
        fail('authorized nightly policy differs from this bootstrap verifier')
    revoked = policy['revoked_manifests']
    if not isinstance(revoked, list) or len(revoked) > 256 or not all(isinstance(value, str) and sha_pattern.fullmatch(value) for value in revoked) or len(set(revoked)) != len(revoked):
        fail('invalid revoked manifest inventory')
    floor = {'metadata_sequence': metadata['sequence'], 'metadata_sha256': digest(directory / 'metadata.json'), 'catalog_sequence': catalog['sequence'], 'catalog_sha256': digest(directory / 'catalog.json')}
    minimum = {'metadata_sequence': 1, 'metadata_sha256': 'cc7470eb6d2aac727bdaff9f5bb30c5c1fbbbaa3de9d2e8a0b5a9bb77a4c8d09', 'catalog_sequence': 2, 'catalog_sha256': 'd688b21bbaee7f5dfc789e7f03b544853e90b40b5004e6203155dfce3df99262'}
    def check_floor(known):
        for name in ('metadata', 'catalog'):
            sequence, checksum = name + '_sequence', name + '_sha256'
            if type(floor[sequence]) is not int or type(known[sequence]) is not int or known[sequence] < 1 or not isinstance(known[checksum], str) or not sha_pattern.fullmatch(known[checksum]):
                fail('invalid trust floor')
            if floor[sequence] < known[sequence] or floor[sequence] == known[sequence] and floor[checksum] != known[checksum]:
                fail('trust rollback or equivocation refused')
    def private_open(path, flags):
        descriptor = os.open(path, flags | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o077 or info.st_nlink != 1:
            os.close(descriptor)
            fail('unsafe bootstrap trust state')
        return os.fdopen(descriptor, 'r+')
    # Serialize the floor with other bootstraps. The root-private parent was
    # checked before staging; a crash cannot erase an observed revocation.
    with private_open(state_directory / 'bootstrap-trust.lock', os.O_RDWR | os.O_CREAT) as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        check_floor(minimum)
        state_path = state_directory / 'bootstrap-trust.json'
        for path in (state_path, state_directory / 'downloads/nightly-trust.json'):
            try:
                with private_open(path, os.O_RDWR) as source:
                    raw = source.read(4 * 1024 * 1024 + 1)
                    if len(raw) > 4 * 1024 * 1024:
                        fail('trust state exceeds bound')
                    known = json.loads(raw, object_pairs_hook=unique_object)
                    check_floor(known if path == state_path else known['floor'])
            except FileNotFoundError:
                pass
        descriptor, temporary = tempfile.mkstemp(prefix='.bootstrap-trust-', dir=state_directory)
        try:
            with os.fdopen(descriptor, 'w') as output:
                json.dump(floor, output)
                output.flush()
                os.fsync(output.fileno())
            os.replace(temporary, state_path)
            parent = os.open(state_directory, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(parent)
            finally:
                os.close(parent)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
    if digest(manifest_path) in revoked:
        fail('nightly manifest revoked by current authorized nightly policy')

def certificate_policy(bundle_path, commit, ref='refs/heads/main', workflow_path='.github/workflows/nightly.yml'):
    # Cosign has already authenticated this bundle's exact leaf certificate.
    # OpenSSL owns X.509/ASN.1 decoding; this code only compares decoded policy
    # claims. Never use this operation in place of signature verification.
    bundle = document(bundle_path)
    if bundle.get('mediaType') != 'application/vnd.dev.sigstore.bundle.v0.3+json':
        fail('unsupported Sigstore bundle format')
    material = bundle['verificationMaterial']
    certificate = base64.b64decode(material['certificate']['rawBytes'], validate=True)
    if len(certificate) > 65536:
        fail('certificate exceeds 64 KiB')
    entries = material['tlogEntries']
    if not isinstance(entries, list) or len(entries) != 1:
        fail('exactly one authenticated transparency entry is required')
    entry = entries[0]
    if base64.b64decode(entry['logId']['keyId'], validate=True).hex() != 'c0d23d6ad406973f9559f3ba2d1ca01f84147d8ffc5b8445c224f98b9591801d':
        fail('transparency log differs from pinned nightly policy')
    proof = entry['inclusionProof']
    checkpoint = proof['checkpoint']['envelope'].splitlines()
    if len(checkpoint) < 3 or checkpoint[0] != 'rekor.sigstore.dev - 1193050959916656506' or not re.fullmatch(r'[1-9][0-9]*', str(proof['treeSize'])) or checkpoint[1] != str(proof['treeSize']):
        fail('transparency checkpoint differs from pinned nightly policy')
    if not entry['inclusionPromise']['signedEntryTimestamp'] or not re.fullmatch(r'[1-9][0-9]*', str(entry['integratedTime'])):
        fail('authenticated integrated timestamp is required')

    def openssl(*flags):
        result = subprocess.run(['openssl', *flags], input=certificate, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=10, check=True)
        if len(result.stdout) > 262144:
            fail('certificate parser output exceeds 256 KiB')
        return result.stdout.decode('ascii')

    openssl('x509', '-inform', 'DER', '-noout')
    lines = openssl('asn1parse', '-inform', 'DER').splitlines()
    workflow = 'https://github.com/Hikyo-Org/Hikyo/' + workflow_path + '@' + ref
    expected = {
        8: 'https://token.actions.githubusercontent.com',
        9: workflow,
        10: commit,
        11: 'github-hosted',
        12: 'https://github.com/Hikyo-Org/Hikyo',
        13: commit,
        14: ref,
        15: '1316165429',
        16: 'https://github.com/Hikyo-Org',
        17: '316726515',
        18: workflow,
        19: commit,
    }
    for number, value in expected.items():
        oid = '1.3.6.1.4.1.57264.1.' + str(number)
        matching = [i for i, line in enumerate(lines) if re.fullmatch(r'\s*[0-9]+:d=5\s+hl=[0-9]+\s+l=\s*[0-9]+\s+prim:\s+OBJECT\s+:' + re.escape(oid), line)]
        if len(matching) != 1 or matching[0] + 1 >= len(lines):
            fail('missing or duplicate authenticated certificate claim ' + oid)
        extension = re.fullmatch(r'\s*([0-9]+):d=5\s+hl=[0-9]+\s+l=\s*[0-9]+\s+prim:\s+OCTET STRING\s+\[HEX DUMP\]:[0-9A-Fa-f]+', lines[matching[0] + 1])
        if not extension:
            fail('unexpected certificate extension encoding')
        decoded = openssl('asn1parse', '-inform', 'DER', '-strparse', extension.group(1)).strip()
        claim = re.fullmatch(r'0:d=0\s+hl=[0-9]+\s+l=\s*[0-9]+\s+prim:\s+UTF8STRING\s+:(.*)', decoded)
        if not claim or claim.group(1) != value:
            fail('authenticated certificate claim differs from nightly policy: ' + oid)

def main():
    action, *args = sys.argv[1:]
    if action == 'hash':
        if not sha_pattern.fullmatch(args[1]) or digest(args[0]) != args[1]:
            fail('download SHA-256 mismatch')
    elif action == 'certificate-policy':
        certificate_policy(*args)
    elif action == 'authorize':
        recovery_authorization(*args)
    elif action == 'release':
        releases = document(args[0])
        if not isinstance(releases, list) or len(releases) > 100:
            fail('invalid GitHub release inventory')
        candidates = {}
        for release in releases:
            if not isinstance(release, dict):
                fail('invalid GitHub release')
            tag = release.get('tag_name', '')
            match = tag_pattern.fullmatch(tag) if isinstance(tag, str) else None
            if not match or release.get('draft') is not False or release.get('prerelease') is not True or release.get('immutable') is not True:
                continue
            assets = release.get('assets')
            if not isinstance(assets, list) or not all(isinstance(asset, dict) for asset in assets):
                fail('invalid GitHub release assets')
            names = [asset.get('name') for asset in assets]
            if names.count('release-manifest.json') != 1 or names.count('release-manifest.sigstore.json') != 1:
                continue
            sequence = int(match.group(1))
            if sequence in candidates:
                fail('ambiguous nightly release sequence')
            candidates[sequence] = tag
        if not candidates or max(candidates) <= 26:
            fail('no signed nightly containing the automatic upgrade coordinator has been published yet')
        print(candidates[max(candidates)])
    elif action in ('commit', 'artifact'):
        manifest = document(args[0])
        tag = args[1]
        if not isinstance(manifest, dict) or manifest.get('schema') != 'hikyo.dev/nightly-manifest/v1' or manifest.get('profile') != 'nightly/v1':
            fail('invalid nightly manifest profile')
        match = tag_pattern.fullmatch(tag)
        if not match or manifest.get('tag') != tag or manifest.get('version') != tag[1:] or type(manifest.get('release_sequence')) is not int or manifest['release_sequence'] != int(match.group(1)):
            fail('manifest release identity mismatch')
        commit = manifest.get('source_commit')
        if not isinstance(commit, str) or not re.fullmatch(r'[0-9a-f]{40}', commit) or not commit.startswith(tag.rsplit('.g', 1)[1]):
            fail('invalid source commit')
        if action == 'commit':
            print(commit)
            return
        inventory = manifest.get('artifacts')
        if not isinstance(inventory, list) or not 1 <= len(inventory) <= 100:
            fail('invalid signed artifact inventory')
        seen, selected = set(), []
        for artifact in inventory:
            if not isinstance(artifact, dict):
                fail('invalid signed artifact')
            name, checksum = artifact.get('name'), artifact.get('sha256')
            if not isinstance(name, str) or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]{0,199}', name) or '..' in name or name in seen:
                fail('unsafe or duplicate signed artifact name')
            if not isinstance(checksum, str) or not sha_pattern.fullmatch(checksum):
                fail('invalid signed artifact digest')
            seen.add(name)
            if artifact.get('kind') == 'binary' and artifact.get('platform') == 'linux/' + args[2]:
                if not name.endswith('.tar.gz'):
                    fail('unexpected Linux archive format')
                selected.append((name, checksum))
        if len(selected) != 1:
            fail('signed inventory must contain exactly one binary for this platform')
        print(*selected[0], sep='\n')
    elif action == 'extract':
        archive, output, checksum, arch = args
        if digest(archive) != checksum:
            fail('archive SHA-256 differs from the authenticated inventory')
        with tarfile.open(archive, mode='r:gz') as package:
            count, total, binary = 0, 0, None
            for member in package:
                count += 1
                parts = pathlib.PurePosixPath(member.name).parts
                if member.name.startswith('/') or not parts or '..' in parts or '\\' in member.name or member.name.startswith('./'):
                    fail('unsafe archive member path')
                if not member.isfile() and not member.isdir():
                    fail('archive contains a link or special file')
                total += member.size
                if count > 10000 or total > 512 * 1024 * 1024 or member.size < 0:
                    fail('archive exceeds bounded extraction limits')
                if member.name == 'hikyo':
                    if binary is not None or not member.isfile() or member.size == 0:
                        fail('archive must contain one regular hikyo executable')
                    binary = member
            if binary is None:
                fail('archive has no hikyo executable')
            with package.extractfile(binary) as source, open(output, 'xb') as target:
                shutil.copyfileobj(source, target, 1024 * 1024)
                target.flush()
                os.fsync(target.fileno())
        with open(output, 'rb') as candidate:
            header = candidate.read(20)
        machine = {'amd64': 62, 'arm64': 183}.get(arch)
        if len(header) != 20 or header[:6] != b'\x7fELF\x02\x01' or int.from_bytes(header[18:20], 'little') != machine:
            fail('candidate is not the expected Linux ELF executable')
        os.chmod(output, 0o700)
    else:
        fail('unknown verification operation')

if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, TypeError, KeyError, AttributeError, subprocess.SubprocessError, tarfile.TarError) as error:
        fail('invalid bootstrap input: ' + type(error).__name__)
# END BOOTSTRAP VERIFIER
PY

download() {
	curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
		--connect-timeout 15 --max-time 300 --max-filesize "$3" "$1" --output "$2"
}

printf 'Authenticating the latest signed nightly upgrade coordinator...\n' >&2
download "https://api.github.com/repos/$repository/releases?per_page=100" "$scratch/releases.json" 4194304
tag=$(python3 "$scratch/verify.py" release "$scratch/releases.json")
base_url="https://github.com/$repository/releases/download/$tag"
download "$base_url/release-manifest.json" "$scratch/manifest.json" 4194304
download "$base_url/release-manifest.sigstore.json" "$scratch/manifest.sigstore.json" 4194304
commit=$(python3 "$scratch/verify.py" commit "$scratch/manifest.json" "$tag")

download "https://github.com/sigstore/cosign/releases/download/v3.1.3/cosign-linux-$arch" "$scratch/cosign" 314572800
python3 "$scratch/verify.py" hash "$scratch/cosign" "$cosign_sha256"
chmod 700 "$scratch/cosign"
download "https://raw.githubusercontent.com/$repository/refs/heads/main/release/trust/nightly/trusted-root.json" "$scratch/trusted-root.json" 4194304
python3 "$scratch/verify.py" hash "$scratch/trusted-root.json" "$trusted_root_sha256"
"$scratch/cosign" verify-blob --bundle "$scratch/manifest.sigstore.json" \
	--trusted-root "$scratch/trusted-root.json" \
	--certificate-identity "https://github.com/$repository/.github/workflows/nightly.yml@refs/heads/main" \
	--certificate-oidc-issuer https://token.actions.githubusercontent.com \
	--certificate-github-workflow-repository "$repository" \
	--certificate-github-workflow-ref refs/heads/main \
	--certificate-github-workflow-sha "$commit" "$scratch/manifest.json"
python3 "$scratch/verify.py" certificate-policy "$scratch/manifest.sigstore.json" "$commit"

# The signing workflow alone is insufficient: authenticate the current trust
# catalog (recovery-signed, or workflow-signed under a recovery-signed stable
# policy) and revocations before even asking a candidate for --help.
trust_url="https://raw.githubusercontent.com/$repository/refs/heads/main/release/trust"
for trust_file in recovery-1.pub metadata.json metadata.sigstore.json catalog.json catalog.sigstore.json \
	stable-policy.json stable-policy.sigstore.json stable-trusted-root.json; do
	download "$trust_url/$trust_file" "$scratch/$trust_file" 4194304
done
download "$trust_url/nightly/policy.json" "$scratch/policy.json" 4194304
python3 "$scratch/verify.py" authorize "$scratch" "$scratch/manifest.json" /var/lib/hikyo-upgrader "$scratch/cosign"

# Only after Cosign authenticates the exact manifest may its inventory choose
# executable bytes. Neither a GitHub asset checksum nor an unsigned checksums
# file grants execution authority.
python3 "$scratch/verify.py" artifact "$scratch/manifest.json" "$tag" "$arch" >"$scratch/artifact"
artifact=$(sed -n '1p' "$scratch/artifact")
artifact_sha256=$(sed -n '2p' "$scratch/artifact")
download "$base_url/$artifact" "$scratch/archive.tar.gz" 536870912
python3 "$scratch/verify.py" extract "$scratch/archive.tar.gz" "$scratch/hikyo" "$artifact_sha256" "$arch"
"$scratch/hikyo" upgrade --help >/dev/null 2>&1 || fail 'the latest signed nightly does not include automatic upgrades yet; wait for the next published nightly'
printf 'Verified %s. Starting the upgrade coordinator; the installed binary is unchanged.\n' "$tag" >&2
"$scratch/hikyo" upgrade "$@" </dev/tty
