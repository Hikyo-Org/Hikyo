#!/bin/sh
# Offline bootstrap boundary tests. No downloads or installed service changes.
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
python3 - "$script_dir/upgrade-nightly.sh" <<'PY'
import hashlib
import base64
import io
import json
import os
import pathlib
import shlex
import subprocess
import sys
import tarfile
import tempfile
import unittest

script = pathlib.Path(sys.argv[1]).read_text()
helper = script.split('# BEGIN BOOTSTRAP VERIFIER\n', 1)[1].split('# END BOOTSTRAP VERIFIER', 1)[0]
tag = 'v0.0.1-nightly.20260907.27.gaaaaaaaa'

class BootstrapBoundaries(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.verifier = self.root / 'verify.py'
        self.verifier.write_text(helper)

    def run_verify(self, action, *args, success=True):
        result = subprocess.run([sys.executable, str(self.verifier), action, *map(str, args)], capture_output=True, text=True)
        self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
        return result.stdout.strip()

    def manifest(self, artifacts=None):
        if artifacts is None:
            artifacts = [{'name': 'hikyo_linux_arm64.tar.gz', 'kind': 'binary', 'platform': 'linux/arm64', 'sha256': 'b' * 64}]
        value = {'schema': 'hikyo.dev/nightly-manifest/v1', 'profile': 'nightly/v1', 'tag': tag, 'version': tag[1:], 'source_commit': 'a' * 40, 'release_sequence': 27, 'artifacts': artifacts}
        path = self.root / 'manifest.json'
        path.write_text(json.dumps(value))
        return path

    def archive(self, names=('hikyo',), member_type=tarfile.REGTYPE):
        path = self.root / 'archive.tar.gz'
        header = bytearray(20)
        header[:6] = b'\x7fELF\x02\x01'
        header[18:20] = (183).to_bytes(2, 'little')
        with tarfile.open(path, 'w:gz') as output:
            for name in names:
                entry = tarfile.TarInfo(name)
                entry.type = member_type
                entry.size = len(header) if member_type == tarfile.REGTYPE else 0
                entry.linkname = '/etc/shadow'
                output.addfile(entry, io.BytesIO(header) if entry.size else None)
        return path, hashlib.sha256(path.read_bytes()).hexdigest()

    def test_authenticated_inventory_selection(self):
        manifest = self.manifest()
        self.assertEqual(self.run_verify('commit', manifest, tag), 'a' * 40)
        self.assertEqual(self.run_verify('artifact', manifest, tag, 'arm64'), 'hikyo_linux_arm64.tar.gz\n' + 'b' * 64)

    def test_unsafe_duplicate_and_wrong_platform_inventory(self):
        for name in ('../hikyo', '/hikyo', 'hikyo\nrun', 'hikyo;exec', 'hikyo..tar.gz'):
            manifest = self.manifest([{'name': name, 'kind': 'binary', 'platform': 'linux/arm64', 'sha256': 'b' * 64}])
            self.run_verify('artifact', manifest, tag, 'arm64', success=False)
        item = {'name': 'hikyo.tar.gz', 'kind': 'binary', 'platform': 'linux/arm64', 'sha256': 'b' * 64}
        self.run_verify('artifact', self.manifest([item, item]), tag, 'arm64', success=False)
        self.run_verify('artifact', self.manifest(), tag, 'amd64', success=False)

    def test_duplicate_json_and_source_commit_fail(self):
        path = self.manifest()
        path.write_text(path.read_text().replace('"source_commit":', '"source_commit":"' + 'c' * 40 + '","source_commit":'))
        self.run_verify('commit', path, tag, success=False)
        path = self.manifest()
        value = json.loads(path.read_text())
        value['source_commit'] = 'c' * 40
        path.write_text(json.dumps(value))
        self.run_verify('commit', path, tag, success=False)

    def test_download_hash_mismatch_fails(self):
        path = self.root / 'cosign'
        path.write_bytes(b'unauthenticated executable')
        self.run_verify('hash', path, 'a' * 64, success=False)

    def test_safe_extraction_and_no_overwrite(self):
        archive, digest = self.archive()
        output = self.root / 'hikyo'
        self.run_verify('extract', archive, output, digest, 'arm64')
        self.assertEqual(output.stat().st_mode & 0o777, 0o700)
        original = output.read_bytes()
        self.run_verify('extract', archive, output, digest, 'arm64', success=False)
        self.assertEqual(output.read_bytes(), original)

    def test_archive_digest_paths_links_duplicates_and_platform(self):
        output = self.root / 'hikyo'
        archive, digest = self.archive()
        self.run_verify('extract', archive, output, 'a' * 64, 'arm64', success=False)
        self.assertFalse(output.exists())
        for names, kind in [(('../hikyo',), tarfile.REGTYPE), (('/hikyo',), tarfile.REGTYPE), (('hikyo',), tarfile.SYMTYPE), (('hikyo',), tarfile.LNKTYPE), (('hikyo',), tarfile.FIFOTYPE), (('hikyo', 'hikyo'), tarfile.REGTYPE)]:
            archive, digest = self.archive(names, kind)
            self.run_verify('extract', archive, output, digest, 'arm64', success=False)
            self.assertFalse(output.exists())

    def test_only_immutable_signed_nightlies_selected(self):
        release = {'tag_name': tag, 'draft': False, 'prerelease': True, 'immutable': True, 'assets': [{'name': 'release-manifest.json'}, {'name': 'release-manifest.sigstore.json'}]}
        path = self.root / 'releases.json'
        path.write_text(json.dumps([release]))
        self.assertEqual(self.run_verify('release', path), tag)
        for key in ('draft', 'prerelease', 'immutable'):
            changed = dict(release)
            changed[key] = not changed[key]
            path.write_text(json.dumps([changed]))
            self.run_verify('release', path, success=False)
        path.write_text(json.dumps([release, release]))
        self.run_verify('release', path, success=False)

    def test_future_version_nightly_release_selection(self):
        future = 'v1.2.0-nightly.20260908.28.gaaaaaaaa'
        release = {'tag_name': future, 'draft': False, 'prerelease': True, 'immutable': True, 'assets': [{'name': 'release-manifest.json'}, {'name': 'release-manifest.sigstore.json'}]}
        path = self.root / 'releases.json'
        path.write_text(json.dumps([release]))
        self.assertEqual(self.run_verify('release', path), future)

    def test_full_certificate_claim_policy_before_execution(self):
        workflow = 'https://github.com/Hikyo-Org/Hikyo/.github/workflows/nightly.yml@refs/heads/main'
        values = {8: 'https://token.actions.githubusercontent.com', 9: workflow, 10: 'a' * 40, 11: 'github-hosted', 12: 'https://github.com/Hikyo-Org/Hikyo', 13: 'a' * 40, 14: 'refs/heads/main', 15: '1316165429', 16: 'https://github.com/Hikyo-Org', 17: '316726515', 18: workflow, 19: 'a' * 40}
        for replacement in (None, (11, 'self-hosted'), (15, '999'), (17, '999')):
            claims = dict(values)
            if replacement:
                claims[replacement[0]] = replacement[1]
            args = ['openssl', 'req', '-new', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes', '-keyout', str(self.root / 'fixture.key'), '-outform', 'DER', '-out', str(self.root / 'fixture.der'), '-subj', '/CN=policy-parser-fixture', '-days', '1']
            for number, value in claims.items():
                args.extend(['-addext', '1.3.6.1.4.1.57264.1.' + str(number) + '=ASN1:UTF8String:' + value])
            subprocess.run(args, check=True, capture_output=True)
            # These synthetic bytes test policy decoding only. The production
            # shell must first pass Cosign; the prior test proves that order.
            bundle = {'mediaType': 'application/vnd.dev.sigstore.bundle.v0.3+json', 'verificationMaterial': {'certificate': {'rawBytes': base64.b64encode((self.root / 'fixture.der').read_bytes()).decode()}, 'tlogEntries': [{'logId': {'keyId': base64.b64encode(bytes.fromhex('c0d23d6ad406973f9559f3ba2d1ca01f84147d8ffc5b8445c224f98b9591801d')).decode()}, 'inclusionProof': {'treeSize': '1', 'checkpoint': {'envelope': 'rekor.sigstore.dev - 1193050959916656506\n1\nfixture\n'}}, 'inclusionPromise': {'signedEntryTimestamp': 'fixture'}, 'integratedTime': '1'}]}}
            path = self.root / 'certificate.sigstore.json'
            path.write_text(json.dumps(bundle))
            self.run_verify('certificate-policy', path, 'a' * 40, success=replacement is None)

    def test_signature_failure_stops_before_policy_or_download(self):
        # Execute the production shell's signature-verification section with a
        # failing maintained-verifier stand-in. No production bypass variable
        # or unauthenticated execution path is added to the distributed script.
        verifier = self.root / 'cosign'
        verifier.write_text('#!/bin/sh\nexit 23\n')
        verifier.chmod(0o700)
        section = script.split('"$scratch/cosign" verify-blob', 1)[1].split('# Only after Cosign', 1)[0]
        shell = 'set -eu\nscratch=' + shlex.quote(str(self.root)) + '\nrepository=Hikyo-Org/Hikyo\ncommit=' + 'a' * 40 + '\n"$scratch/cosign" verify-blob' + section
        result = subprocess.run(['sh', '-c', shell], capture_output=True, text=True)
        self.assertEqual(result.returncode, 23)
        self.assertNotIn('certificate-policy', result.stderr)
        for required in ('--certificate-identity', '--certificate-oidc-issuer', '--certificate-github-workflow-repository', '--certificate-github-workflow-ref', '--certificate-github-workflow-sha', '--trusted-root'):
            self.assertIn(required, section)
        self.assertNotIn('--insecure-ignore', section)

    def authorization_fixture(self):
        self.key = self.root / 'test-recovery.key'
        subprocess.run(['openssl', 'genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256', '-out', str(self.key)], check=True, capture_output=True)
        subprocess.run(['openssl', 'pkey', '-in', str(self.key), '-pubout', '-out', str(self.root / 'recovery-1.pub')], check=True, capture_output=True)
        key_digest = hashlib.sha256((self.root / 'recovery-1.pub').read_bytes()).hexdigest()
        # Replace the independently pinned fixture key in the extracted helper,
        # never add a production override or a key selected by signed evidence.
        self.verifier.write_text(helper.replace('1eb7ad2092668b73621c21a1eeb801ed6391bc794df1909abce8e1d45e03a229', key_digest))
        policy_path = pathlib.Path(sys.argv[1]).parent.parent / 'release/trust/nightly/policy.json'
        self.policy = json.loads(policy_path.read_text())
        self.metadata = {'schema': 'hikyo.dev/trust-metadata/v1', 'sequence': 2, 'recovery': {'id': 'recovery-1', 'sha256': key_digest}, 'event': {'signed_by': 'recovery-1'}}
        self.catalog = {'schema': 'hikyo.dev/upgrade-trust/v1', 'sequence': 3, 'bridges': []}
        self.state = self.root / 'state'
        self.state.mkdir(mode=0o700)
        return self.manifest()

    def sign_authorization(self, authorized=True):
        policy = self.root / 'policy.json'
        policy.write_text(json.dumps(self.policy))
        metadata = self.root / 'metadata.json'
        metadata.write_text(json.dumps(self.metadata))
        self.catalog['stable_metadata_sha256'] = hashlib.sha256(metadata.read_bytes()).hexdigest()
        self.catalog['nightly_policies'] = [hashlib.sha256(policy.read_bytes()).hexdigest()] if authorized else []
        (self.root / 'catalog.json').write_text(json.dumps(self.catalog))
        for name in ('metadata', 'catalog'):
            result = subprocess.run(['openssl', 'dgst', '-sha256', '-sign', str(self.key), str(self.root / (name + '.json'))], check=True, capture_output=True)
            (self.root / (name + '.sigstore.json')).write_text(json.dumps({'base64Signature': base64.b64encode(result.stdout).decode()}))

    def test_recovery_revocation_persists_and_rejects_older_authorization(self):
        manifest = self.authorization_fixture()
        self.sign_authorization()
        self.run_verify('authorize', self.root, manifest, self.state)
        self.policy['revoked_manifests'] = [hashlib.sha256(manifest.read_bytes()).hexdigest()]
        self.catalog['sequence'] = 4
        self.sign_authorization()
        self.assertIn('revoked', subprocess.run([sys.executable, str(self.verifier), 'authorize', str(self.root), str(manifest), str(self.state)], capture_output=True, text=True).stderr)
        self.assertEqual(json.loads((self.state / 'bootstrap-trust.json').read_text())['catalog_sequence'], 4)
        self.policy['revoked_manifests'] = []
        self.catalog['sequence'] = 3
        self.sign_authorization()
        self.run_verify('authorize', self.root, manifest, self.state, success=False)

    def test_unauthorized_policy_and_tampered_signature_refuse(self):
        manifest = self.authorization_fixture()
        self.sign_authorization(authorized=False)
        self.run_verify('authorize', self.root, manifest, self.state, success=False)
        self.sign_authorization()
        (self.root / 'catalog.json').write_text((self.root / 'catalog.json').read_text() + ' ')
        self.run_verify('authorize', self.root, manifest, self.state, success=False)
        self.assertFalse((self.state / 'bootstrap-trust.json').exists())

    def test_existing_installer_floor_and_symlink_refuse(self):
        manifest = self.authorization_fixture()
        self.sign_authorization()
        downloads = self.state / 'downloads'
        downloads.mkdir(mode=0o700)
        floor = {'metadata_sequence': 2, 'metadata_sha256': hashlib.sha256((self.root / 'metadata.json').read_bytes()).hexdigest(), 'catalog_sequence': 4, 'catalog_sha256': 'a' * 64}
        known = downloads / 'nightly-trust.json'
        known.write_text(json.dumps({'floor': floor}))
        known.chmod(0o600)
        self.run_verify('authorize', self.root, manifest, self.state, success=False)
        known.unlink()
        (self.state / 'bootstrap-trust.json').symlink_to(self.verifier)
        self.run_verify('authorize', self.root, manifest, self.state, success=False)

    def test_recovery_refusal_prevents_first_candidate_execution(self):
        manifest = self.authorization_fixture()
        self.policy['revoked_manifests'] = [hashlib.sha256(manifest.read_bytes()).hexdigest()]
        self.sign_authorization()
        candidate = self.root / 'hikyo'
        marker = self.root / 'executed'
        candidate.write_text('#!/bin/sh\ntouch ' + shlex.quote(str(marker)) + '\n')
        candidate.chmod(0o700)
        # Exercise the production order, substituting only fixture paths and
        # already-downloaded inputs; no executable is reached on refusal.
        authorization = 'python3 "$scratch/verify.py" authorize "$scratch" "$scratch/manifest.json" /var/lib/hikyo-upgrader'
        self.assertLess(script.index(authorization), script.index('"$scratch/hikyo" upgrade --help'))
        shell = 'set -eu\nscratch=' + shlex.quote(str(self.root)) + '\n' + authorization.replace('/var/lib/hikyo-upgrader', shlex.quote(str(self.state))) + '\n"$scratch/hikyo" upgrade --help\n'
        result = subprocess.run(['sh', '-c', shell], capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(marker.exists())

unittest.main(argv=['bootstrap-tests'])
PY
