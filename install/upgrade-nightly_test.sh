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
import shutil
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
        self.metadata = {'schema': 'hikyo.dev/trust-metadata/v1', 'sequence': 3, 'recovery': {'id': 'recovery-1', 'sha256': key_digest}, 'event': {'signed_by': 'recovery-1'}}
        self.catalog = {'schema': 'hikyo.dev/upgrade-trust/v1', 'sequence': 5, 'bridges': []}
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
        self.catalog['sequence'] = 6
        self.sign_authorization()
        self.assertIn('revoked', subprocess.run([sys.executable, str(self.verifier), 'authorize', str(self.root), str(manifest), str(self.state)], capture_output=True, text=True).stderr)
        self.assertEqual(json.loads((self.state / 'bootstrap-trust.json').read_text())['catalog_sequence'], 6)
        self.policy['revoked_manifests'] = []
        self.catalog['sequence'] = 5
        self.sign_authorization()
        self.run_verify('authorize', self.root, manifest, self.state, success=False)

    def test_replayed_pre_import_snapshots_are_refused_on_empty_state(self):
        # The embedded minimum is the imported 2/4 trust: the earlier legacy
        # snapshot (1/2) and different bytes at those sequences are replays.
        for metadata_sequence, catalog_sequence in ((1, 2), (2, 4), (2, 5), (3, 4)):
            with self.subTest(metadata=metadata_sequence, catalog=catalog_sequence):
                self.setUp()
                manifest = self.authorization_fixture()
                self.metadata['sequence'], self.catalog['sequence'] = metadata_sequence, catalog_sequence
                self.sign_authorization()
                self.assertIn('rollback', subprocess.run([sys.executable, str(self.verifier), 'authorize', str(self.root), str(manifest), str(self.state)], capture_output=True, text=True).stderr)
                self.assertFalse((self.state / 'bootstrap-trust.json').exists())

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
        floor = {'metadata_sequence': 3, 'metadata_sha256': hashlib.sha256((self.root / 'metadata.json').read_bytes()).hexdigest(), 'catalog_sequence': 6, 'catalog_sha256': 'a' * 64}
        known = downloads / 'nightly-trust.json'
        known.write_text(json.dumps({'floor': floor}))
        known.chmod(0o600)
        self.run_verify('authorize', self.root, manifest, self.state, success=False)
        known.unlink()
        (self.state / 'bootstrap-trust.json').symlink_to(self.verifier)
        self.run_verify('authorize', self.root, manifest, self.state, success=False)

    def test_legacy_metadata_carrying_delegated_identity_refuses(self):
        manifest = self.authorization_fixture()
        self.metadata['source_commit'] = 'a' * 40
        self.sign_authorization()
        self.run_verify('authorize', self.root, manifest, self.state, success=False)

    # The committed trust is workflow-signed under the recovery-signed stable
    # policy. The recovery pin, policy signature (OpenSSL), policy validation,
    # delegation checks and certificate claims run for real; only Cosign is a
    # recording stand-in unless real Cosign is on PATH (see the last test).
    stable_commit = '2ac7bb36633d3ae0a99d7724968ca8c3cbf18334'

    def stable_fixture(self, cosign_status=0):
        trust = pathlib.Path(sys.argv[1]).parent.parent / 'release/trust'
        for name in ('recovery-1.pub', 'metadata.json', 'metadata.sigstore.json', 'catalog.json', 'catalog.sigstore.json', 'stable-policy.json', 'stable-policy.sigstore.json', 'stable-trusted-root.json'):
            shutil.copy(trust / name, self.root / name)
        shutil.copy(trust / 'nightly/policy.json', self.root / 'policy.json')
        self.state = self.root / 'state'
        self.state.mkdir(mode=0o700)
        self.cosign = self.root / 'cosign'
        self.cosign.write_text('#!/bin/sh\nprintf "%s\\n" "$@" >>"$0.argv"\nprintf -- "--\\n" >>"$0.argv"\nexit ' + str(cosign_status) + '\n')
        self.cosign.chmod(0o700)
        return self.manifest()

    def edit(self, name, change):
        path = self.root / name
        value = json.loads(path.read_text())
        change(value)
        path.write_text(json.dumps(value, indent=2) + '\n')

    def refuse(self, manifest, message):
        result = subprocess.run([sys.executable, str(self.verifier), 'authorize', str(self.root), str(manifest), str(self.state), str(self.cosign)], capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(message, result.stderr)

    def test_workflow_signed_committed_trust_authorizes_with_exact_cosign_argv(self):
        manifest = self.stable_fixture()
        self.run_verify('authorize', self.root, manifest, self.state, self.cosign)
        identity = 'https://github.com/Hikyo-Org/Hikyo/.github/workflows/release.yml@refs/tags/v1.0.0'
        expected = []
        for name in ('metadata', 'catalog'):
            expected += ['verify-blob', '--bundle', str(self.root / (name + '.sigstore.json')), '--trusted-root', str(self.root / 'stable-trusted-root.json'),
                '--certificate-identity', identity, '--certificate-oidc-issuer', 'https://token.actions.githubusercontent.com',
                '--certificate-github-workflow-repository', 'Hikyo-Org/Hikyo', '--certificate-github-workflow-ref', 'refs/tags/v1.0.0',
                '--certificate-github-workflow-sha', self.stable_commit, str(self.root / (name + '.json')), '--']
        self.assertEqual((self.root / 'cosign.argv').read_text().splitlines(), expected)
        floor = json.loads((self.state / 'bootstrap-trust.json').read_text())
        self.assertEqual((floor['metadata_sequence'], floor['catalog_sequence']), (2, 4))

    def test_workflow_path_refuses_failed_cosign_without_state(self):
        manifest = self.stable_fixture(cosign_status=1)
        self.refuse(manifest, 'stable workflow signature verification failed: metadata')
        self.assertFalse((self.state / 'bootstrap-trust.json').exists())

    def test_workflow_path_requires_cosign(self):
        manifest = self.stable_fixture()
        self.run_verify('authorize', self.root, manifest, self.state, success=False)

    def test_workflow_path_refuses_bad_policy_signature_before_cosign(self):
        manifest = self.stable_fixture()
        def flip(value):
            raw = bytearray(base64.b64decode(value['base64Signature']))
            raw[-1] ^= 1
            value['base64Signature'] = base64.b64encode(bytes(raw)).decode()
        self.edit('stable-policy.sigstore.json', flip)
        self.refuse(manifest, 'CalledProcessError')
        self.assertFalse((self.root / 'cosign.argv').exists())

    def test_workflow_path_refuses_altered_metadata_digest(self):
        manifest = self.stable_fixture()
        self.edit('metadata.json', lambda value: value.update(sequence=3))
        self.refuse(manifest, 'catalog does not bind the authenticated metadata')

    def test_workflow_path_refuses_policy_digest_and_inventory_mismatches(self):
        for change, message in (
            (lambda value: value.update(stable_policy_sha256='a' * 64), 'catalog does not bind exact stable delegation'),
            (lambda value: value.pop('stable_policy_sha256'), 'catalog does not bind exact stable delegation'),
            (lambda value: value.update(bridges=value['bridges'][:1]), 'stable workflow changed nightly or bridge authority'),
            (lambda value: value.update(nightly_policies=value['nightly_policies'] + ['b' * 64]), 'stable workflow changed nightly or bridge authority'),
        ):
            with self.subTest(message=message):
                self.setUp()
                manifest = self.stable_fixture()
                self.edit('catalog.json', change)
                self.refuse(manifest, message)

    def test_workflow_path_refuses_unknown_signer_and_substituted_root(self):
        manifest = self.stable_fixture()
        self.edit('metadata.json', lambda value: value['event'].update(signed_by='recovery-2'))
        self.refuse(manifest, 'invalid trust metadata identity')
        self.assertFalse((self.root / 'cosign.argv').exists())
        self.setUp()
        manifest = self.stable_fixture()
        (self.root / 'stable-trusted-root.json').write_text('{}\n')
        self.refuse(manifest, 'stable Sigstore root missing or substituted')

    # CI installs pinned Cosign in this job (supply-chain-checks); skip only on hosts without it.
    @unittest.skipUnless(shutil.which('cosign'), 'cosign is not on PATH; only the recording stand-in ran')
    def test_workflow_path_with_real_cosign(self):
        manifest = self.stable_fixture()
        self.cosign = pathlib.Path(shutil.which('cosign'))
        self.run_verify('authorize', self.root, manifest, self.state, self.cosign)
        self.edit('metadata.json', lambda value: value.update(sequence=3))
        self.state = self.root / 'state2'
        self.state.mkdir(mode=0o700)
        self.refuse(manifest, 'stable workflow signature verification failed: metadata')

    # Variants re-sign every document with a fixture recovery key (substituted
    # into the extracted verifier, as authorization_fixture does) so that one
    # mutated delegation field is the only reason to refuse. Metadata and
    # catalog sit above the committed 2/4 floor so the floor cannot mask a
    # dropped check. Cosign is the recording stand-in, which accepts anything.
    def delegated_variant(self, metadata=None, policy=None, recovery_signed=False):
        manifest = self.stable_fixture()
        key = self.root / 'variant.key'
        subprocess.run(['openssl', 'genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256', '-out', str(key)], check=True, capture_output=True)
        subprocess.run(['openssl', 'pkey', '-in', str(key), '-pubout', '-out', str(self.root / 'recovery-1.pub')], check=True, capture_output=True)
        key_digest = hashlib.sha256((self.root / 'recovery-1.pub').read_bytes()).hexdigest()
        self.verifier.write_text(helper.replace('1eb7ad2092668b73621c21a1eeb801ed6391bc794df1909abce8e1d45e03a229', key_digest))
        def sign(name):
            result = subprocess.run(['openssl', 'dgst', '-sha256', '-sign', str(key), str(self.root / (name + '.json'))], check=True, capture_output=True)
            (self.root / (name + '.sigstore.json')).write_text(json.dumps({'base64Signature': base64.b64encode(result.stdout).decode()}))
        def digest(name):
            return hashlib.sha256((self.root / (name + '.json')).read_bytes()).hexdigest()
        def metadata_change(value):
            value['recovery']['sha256'] = key_digest
            value['sequence'] = 3
            if metadata:
                metadata(value)
        self.edit('metadata.json', metadata_change)
        if policy:
            self.edit('stable-policy.json', policy)
        sign('stable-policy')
        def catalog_change(value):
            value['sequence'], value['stable_metadata_sha256'], value['stable_policy_sha256'] = 5, digest('metadata'), digest('stable-policy')
            current = json.loads((self.root / 'stable-policy.json').read_text())
            value['nightly_policies'], value['bridges'] = current['nightly_policies'], current['bridges']
        self.edit('catalog.json', catalog_change)
        if recovery_signed:
            sign('metadata')
            sign('catalog')
        self.sign_variant = sign
        return manifest

    def test_recovery_signed_catalog_must_bind_the_exact_stable_policy(self):
        def legacy(value):
            value['event']['signed_by'] = 'recovery-1'
            value.pop('source_commit')
            value.pop('release_tag')
        manifest = self.delegated_variant(metadata=legacy, recovery_signed=True)
        self.run_verify('authorize', self.root, manifest, self.state)
        def stale(value):
            value['sequence'], value['stable_policy_sha256'] = 6, 'a' * 64
        self.edit('catalog.json', stale)
        self.sign_variant('catalog')
        self.assertIn('catalog does not bind exact stable delegation', subprocess.run([sys.executable, str(self.verifier), 'authorize', str(self.root), str(manifest), str(self.state)], capture_output=True, text=True).stderr)

    def test_delegated_variant_control_is_accepted(self):
        manifest = self.delegated_variant()
        self.run_verify('authorize', self.root, manifest, self.state, self.cosign)

    def test_delegated_metadata_checks_each_refuse(self):
        def release(**change):
            return lambda value: value['releases'][0].update(change)
        def many(*changes):
            return lambda value: [change(value) for change in changes]
        authority = 'stable workflow metadata exceeds delegated authority'
        cases = {
            'event type': (lambda value: value['event'].update(type='release-candidate'), authority),
            'pending release': (lambda value: value.update(pending_release={'version': '1.0.1', 'sequence': 2, 'manifest_sha256': 'a' * 64}), authority),
            'tag differs from highest release': (lambda value: value.update(release_tag='v1.0.1'), authority),
            'highest release missing': (lambda value: value.update(highest_release=None), authority),
            'highest sequence not an integer': (lambda value: value.update(highest_release_sequence='1'), authority),
            'commit not hexadecimal': (lambda value: value.update(source_commit='x' * 40), authority),
            'commit too short': (lambda value: value.update(source_commit='a' * 39), authority),
            'primary key revoked after delegation': (lambda value: value['primary_keys'][0].update(revoked=True), authority),
            'release sequence above highest': (lambda value: value['releases'].append({'version': '1.0.1', 'sequence': 2, 'manifest_sha256': 'a' * 64}), 'stable metadata highest release is absent or exceeded'),
            'highest release absent': (release(version='0.9.0'), 'stable metadata highest release is absent or exceeded'),
            'releases not a list': (lambda value: value.update(releases={}), 'invalid stable release inventory'),
            'release sequence not an integer': (release(sequence='1'), 'invalid stable release inventory'),
            'primary key with extra field': (lambda value: value['primary_keys'][0].update(extra=1), 'invalid primary key entry'),
            'nightly tag': (many(lambda value: value.update(highest_release='1.0.0-nightly.1', release_tag='v1.0.0-nightly.1'), release(version='1.0.0-nightly.1')), 'stable signature requires exact stable tag and commit'),
            'overlong tag': (many(lambda value: value.update(highest_release='1.0.0-' + 'a' * 300, release_tag='v1.0.0-' + 'a' * 300), release(version='1.0.0-' + 'a' * 300)), 'stable signature requires exact stable tag and commit'),
            'non-canonical tag': (many(lambda value: value.update(highest_release='01.0.0', release_tag='v01.0.0'), release(version='01.0.0')), 'stable signature requires exact stable tag and commit'),
            # The stand-in accepts these, so only the certificate claim check
            # (and its ref and commit arguments) can refuse them.
            'certificate commit': (lambda value: value.update(source_commit='b' * 40), 'authenticated certificate claim differs'),
            'certificate ref': (many(lambda value: value.update(highest_release='1.0.1', release_tag='v1.0.1'), release(version='1.0.1')), 'authenticated certificate claim differs'),
        }
        for name, (change, message) in cases.items():
            with self.subTest(name):
                self.setUp()
                manifest = self.delegated_variant(metadata=change)
                self.refuse(manifest, message)

    def test_delegated_policy_checks_each_refuse(self):
        differs, invalid = 'stable policy differs from this bootstrap verifier', 'invalid stable delegation'
        cases = {
            'repository id': ({'repository_id': '999'}, differs),
            'workflow path': ({'workflow_path': '.github/workflows/other.yml'}, differs),
            'protected ref': ({'protected_ref': 'refs/heads/main'}, differs),
            'runner': ({'runner_environment': 'self-hosted'}, differs),
            'issuer': ({'issuer': 'https://issuer.example'}, differs),
            'log id': ({'rekor_log_id': 'a' * 64}, differs),
            'checkpoint origin': ({'checkpoint_origin': 'other'}, differs),
            'SCT disabled': ({'require_sct': False}, differs),
            'SCT as integer': ({'require_sct': 1}, differs),
            'trusted root digest': ({'trusted_root_sha256': 'a' * 64}, differs),
            'schema': ({'schema': 'hikyo.dev/stable-policy/v2'}, differs),
            'unknown field': ({'extra': 1}, differs),
            'revoked manifests not a list': ({'revoked_manifests': 'x'}, invalid),
            'revoked manifest malformed': ({'revoked_manifests': ['zz']}, invalid),
            'revoked manifests duplicated': ({'revoked_manifests': ['a' * 64, 'a' * 64]}, invalid),
            'nightly inventory duplicated': ({'nightly_policies': ['a' * 64, 'a' * 64]}, invalid),
            'bridge inventory malformed': ({'bridges': ['zz']}, invalid),
            'minimum sequence zero': ({'minimum_release_sequence': 0}, invalid),
            'minimum sequence boolean': ({'minimum_release_sequence': True}, invalid),
            'primary key digest malformed': ({'primary_keys_sha256': 'nothex'}, invalid),
            'primary key digest differs from metadata': ({'primary_keys_sha256': 'a' * 64}, 'stable workflow metadata exceeds delegated authority'),
            'minimum sequence above highest release': ({'minimum_release_sequence': 2}, 'stable workflow metadata exceeds delegated authority'),
        }
        for name, (change, message) in cases.items():
            with self.subTest(name):
                self.setUp()
                manifest = self.delegated_variant(policy=lambda value, change=change: value.update(change))
                self.refuse(manifest, message)

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
        authorization = 'python3 "$scratch/verify.py" authorize "$scratch" "$scratch/manifest.json" /var/lib/hikyo-upgrader "$scratch/cosign"'
        self.assertLess(script.index(authorization), script.index('"$scratch/hikyo" upgrade --help'))
        shell = 'set -eu\nscratch=' + shlex.quote(str(self.root)) + '\n' + authorization.replace('/var/lib/hikyo-upgrader', shlex.quote(str(self.state))) + '\n"$scratch/hikyo" upgrade --help\n'
        result = subprocess.run(['sh', '-c', shell], capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(marker.exists())

unittest.main(argv=['bootstrap-tests'])
PY
