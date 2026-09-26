import { describe, expect, it } from 'vitest';

import { ApiError } from './client.ts';
import { certificateRefusalText, issueFailureText, parsePolicy, policyText, type PkiPolicy } from './pki.ts';

const policy = {
  allowed_issuers: ['issuing'],
  dns_patterns: ['*.svc.example.com'],
  ip_ranges: [],
  uri_patterns: [],
  allow_wildcard_names: false,
  key_algorithms: ['ecdsa-p256'],
  key_usages: ['digital-signature'],
  ext_key_usages: ['server-auth'],
  max_ttl_seconds: 259200,
  default_ttl_seconds: 86400,
  renew_window_seconds: 28800,
  allow_csr: true,
  allow_generated_key: false,
  machine_issuance: false,
  organization: '',
};

describe('parsePolicy', () => {
  it('accepts the contract shape and returns wire numbers', () => {
    const parsed = parsePolicy(JSON.stringify(policy));
    expect(parsed).toEqual(policy);
  });

  it('refuses malformed JSON, unknown members and out-of-contract values before any request', () => {
    expect(parsePolicy('{')).toBe('The policy is not valid JSON.');
    expect(parsePolicy(JSON.stringify({ ...policy, regex: '.*' }))).toMatch(/does not match the profile shape/);
    expect(parsePolicy(JSON.stringify({ ...policy, key_algorithms: ['dsa'] }))).toMatch(/key_algorithms/);
    expect(parsePolicy(JSON.stringify({ ...policy, max_ttl_seconds: 9_000_000 }))).toMatch(/max_ttl_seconds/);
  });

  it('round-trips a returned policy through the editable text', () => {
    const returned: PkiPolicy = {
      ...policy,
      key_algorithms: ['ecdsa-p256'],
      key_usages: ['digital-signature'],
      ext_key_usages: ['server-auth'],
      max_ttl_seconds: 259200n,
      default_ttl_seconds: 86400n,
      renew_window_seconds: 28800n,
    };
    expect(parsePolicy(policyText(returned))).toEqual(policy);
  });
});

describe('certificate refusal text', () => {
  it('names the profile refusal with the server detail', () => {
    const error = new ApiError(400, 'invalid_argument', 'dns name "x.evil" is not allowed');
    expect(certificateRefusalText(error, 'issue the certificate')).toContain('x.evil');
  });

  it('warns that a failure after the request left may have issued a certificate', () => {
    expect(issueFailureText(new ApiError(500, 'internal'))).toMatch(/revoke any certificate/);
  });
});
