# Exact template binder

This portable pure-data binder is preserved as source for reconstruction, not an execution grant. It requires exact known file fingerprints before allowing the documented binding leaves. The malformed operator/nested Resource/changed Action controls remain necessary. Its original raw SHA256 is `fc2b8beef44c6a017451f68f3e4940d5f193ca2e271b121b47fdbc1676d5e5a0`.

```python
"""Pure local policy-data binding. This module performs no API or filesystem write."""
from datetime import datetime, timedelta
import json
import hashlib
import re


# Canonical content hashes derived from the hash-verified frozen round-3 templates.
PINNED_TEMPLATE_SHA256 = {'hikyo-158-ambient.policy.json': 'be9b30bd9a6f3e4d9ee9dd4f7f480eaaec42f8da1f104025beb8fcaa2cad38e3',
 'hikyo-158-ambient.trust.json': 'b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188',
 'hikyo-158-assume.policy.json': '5e72eba03eb62d57fb0ac5d50d68e633b063b5f68dc821d7841ce77a5bc0d1ba',
 'hikyo-158-assume.trust.json': 'c1a5c0a03a3354bab2792cdc4b1633235bba1ce99309a38f190dcba624f82a4c',
 'hikyo-158-cleanup.policy.json': '7242df4500caa402bab0922e1eabae5441eb91127a677dbd07f8d889f45e88bc',
 'hikyo-158-cleanup.trust.json': 'b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188',
 'hikyo-158-kms-denied.policy.json': '743ccfe2d9754404944cddc4510ea57be5eca6bd0294da0abaaaf68ce4f47fbe',
 'hikyo-158-kms-denied.trust.json': 'b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188',
 'hikyo-158-oracle.policy.json': '2a78a1240f44df01fcc9824552745ff656e10bc90a8088649e069681980be537',
 'hikyo-158-oracle.trust.json': 'b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188',
 'hikyo-158-source.policy.json': '0b7bf375f8d88c1af3536a5800d67f2ccd7272ffbdbd139dbca82fe1f009589d',
 'hikyo-158-source.trust.json': 'b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188',
 'hikyo-158-static.policy.json': '3aaf3c7976d5610c2769f336c79ed69aff03aeaa82b5d56e7fe52891ef924280',
 'hikyo-158-static.trust.json': 'b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188',
 'hikyo-158-web.policy.json': 'b89444e780ec2ddc9c732929da61e9ebc08ba22aab036b47383025baf978419a',
 'hikyo-158-web.trust.json': 'b39b3ee57c1fb9f10b3aea389429cd12d64326e631c23a9e992de9caf04bf188',
 'setup-role-boundary.json': '51290dc8c0f03a10df8d799b77a7233a64932d7911f19568064ef86d07415d00',
 'setup-user.managed-policy.json': '0ed3d2d602205f96b54487a7bf8001cb1760797316f037ea60d14cfb511a94ca'}

def bind_templates(templates, bindings):
    if set(templates) != set(PINNED_TEMPLATE_SHA256):
        raise ValueError('template-file-set')
    for name, document in templates.items():
        canonical = json.dumps(document, sort_keys=True, separators=(',', ':')).encode()
        if hashlib.sha256(canonical).hexdigest() != PINNED_TEMPLATE_SHA256[name]:
            raise ValueError('unapproved-template')
    required = {'keyARN', 'runPrefix', 'externalId', 't0', 'authExpiresAt', 'cleanupExpiresAt'}
    if set(bindings) != required:
        raise ValueError('binding-fields')
    if not re.fullmatch(r'arn:aws:kms:eu-central-1:099256073061:key/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}', bindings['keyARN']):
        raise ValueError('key-arn')
    run_prefix = bindings['runPrefix']
    if not re.fullmatch(r'hikyo-e2e-[0-9]{8}c[0-9]{2}/', run_prefix):
        raise ValueError('run-prefix')
    t0, auth, cleanup = [datetime.fromisoformat(bindings[name]) for name in ['t0', 'authExpiresAt', 'cleanupExpiresAt']]
    if not all(bindings[name].endswith('Z') for name in ['t0', 'authExpiresAt', 'cleanupExpiresAt']):
        raise ValueError('utc-time-required')
    if auth != t0 + timedelta(hours=2) or cleanup != t0 + timedelta(hours=4):
        raise ValueError('window-shape')
    if not re.fullmatch(r'hikyo-158-[0-9]{8}c[0-9]{2}', bindings['externalId']):
        raise ValueError('external-id')
    # Every frozen ARN template already contains the separator after this token.
    prefix_component = run_prefix[:-1]
    replacements = {
        'UNBOUND_NEW_KEY_ARN': bindings['keyARN'],
        'UNBOUND_APPROVED_RUN_PREFIX': prefix_component,
        'UNBOUND_APPROVED_EXTERNAL_ID': bindings['externalId'],
        'UNBOUND_APPROVED_AUTH_UNTIL': bindings['authExpiresAt'],
        'UNBOUND_APPROVED_CLEANUP_UNTIL': bindings['cleanupExpiresAt'],
        'UNBOUND_APPROVED_SETUP_UNTIL': bindings['cleanupExpiresAt'],
    }
    audit = []

    def visit(value, path, name):
        if isinstance(value, dict):
            return {key: visit(child, path + [key], name) for key, child in value.items()}
        if isinstance(value, list):
            return [visit(child, path + [str(index)], name) for index, child in enumerate(value)]
        if not isinstance(value, str):
            return value
        result = value
        for token, replacement in replacements.items():
            if token not in value:
                continue
            if token.endswith('_UNTIL'):
                permitted = path[-1] == 'aws:CurrentTime' and 'Condition' in path
            elif token == 'UNBOUND_NEW_KEY_ARN':
                permitted = 'Resource' in path or path[-1] == 'secretsmanager:KmsKeyArn'
            elif token == 'UNBOUND_APPROVED_RUN_PREFIX':
                permitted = 'Resource' in path or path[-1] == 'kms:EncryptionContext:SecretARN'
            else:
                permitted = path[-1] == 'sts:ExternalId'
            if not permitted:
                raise ValueError('unapproved-leaf')
            result = result.replace(token, replacement)
        if 'UNBOUND_' in result:
            raise ValueError('unresolved-token')
        if result != value:
            audit.append({'file': name, 'path': '/' + '/'.join(path), 'before': value, 'after': result})
        return result

    documents = {name: visit(document, [], name) for name, document in templates.items()}
    for name in ['setup-user.managed-policy.json', 'setup-role-boundary.json']:
        if len(json.dumps(documents[name], sort_keys=True, separators=(',', ':'))) > 6144:
            raise ValueError('managed-policy-size')
    return documents, audit
```
