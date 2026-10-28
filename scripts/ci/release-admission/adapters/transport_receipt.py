"""Exact protocol.canonical for the CLOSED transport receipt value domain.

Numbers outside chunks are safe integers (sequence and readAtMS). Arbitrary
provider JSON, including fractional numbers, remains losslessly inside strings.
Do not use G's ASCII model canonicalizer for this distinct JS wire digest.
"""
import hashlib
import json


def _string(value):
    encoded = json.dumps(value, ensure_ascii=False, separators=(',', ':'))
    # JSON.stringify escapes unpaired UTF-16 surrogates; valid pairs decoded
    # from JSON become an astral Python codepoint and stay literal UTF-8.
    return ''.join('\\u%04x' % ord(c) if 0xD800 <= ord(c) <= 0xDFFF else c for c in encoded)


def canonical(value):
    if value is None:
        return 'null'
    if type(value) is bool:
        return 'true' if value else 'false'
    if type(value) is int and abs(value) <= 9007199254740991:
        return str(value)
    if type(value) is str:
        return _string(value)
    if type(value) is list:
        return '[' + ','.join(canonical(v) for v in value) + ']'
    if type(value) is dict and all(type(k) is str for k in value):
        keys = sorted(value, key=lambda k: k.encode('utf-16-be', errors='surrogatepass'))
        return '{' + ','.join(_string(k) + ':' + canonical(value[k]) for k in keys) + '}'
    raise ValueError('receipt:outside-closed-safe-integer-wire-domain')


def digest(value):
    return hashlib.sha256(canonical(value).encode('utf-8')).hexdigest()
