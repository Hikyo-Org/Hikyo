import { describe, expect, it } from 'vitest';

import { ApiError } from './client.ts';
import {
  isHttpsIssuer,
  issuerCreateRefusalText,
  issuerDeleteRefusalText,
  issuerFieldRefusal,
  issuerUpdateRefusalText,
} from './federationIssuers.ts';

/**
 * The federation-issuer surface's pure gates. Each is where a wrong answer is a
 * SECURITY statement — an http issuer admitted, a delete promised that the
 * server refuses, a default-audience list left empty — so they are pinned here
 * rather than inferred from a screenshot.
 */

describe('isHttpsIssuer', () => {
  it('accepts an https URL with a host and nothing else', () => {
    expect(isHttpsIssuer('https://token.actions.githubusercontent.com')).toBe(true);
    expect(isHttpsIssuer('https://kubernetes.default.svc.cluster.local')).toBe(true);
  });

  it('refuses http, so federation trust never rests on the network path', () => {
    expect(isHttpsIssuer('http://token.actions.githubusercontent.com')).toBe(false);
  });

  it('refuses userinfo, query and fragment — each would be disclosed byte-exact', () => {
    expect(isHttpsIssuer('https://user:secret@host')).toBe(false);
    expect(isHttpsIssuer('https://host?x=1')).toBe(false);
    expect(isHttpsIssuer('https://host#frag')).toBe(false);
  });

  it('refuses a non-URL and an empty host', () => {
    expect(isHttpsIssuer('not a url')).toBe(false);
    expect(isHttpsIssuer('https://')).toBe(false);
  });
});

describe('issuerFieldRefusal', () => {
  const base = {
    jwksMode: 'discovery' as const,
    staticJwks: '',
    refusedAudiences: ['sts.amazonaws.com'],
  };

  it('passes a well-formed create', () => {
    expect(issuerFieldRefusal({ issuer: 'https://host', ...base })).toBeNull();
  });

  it('passes an update, which supplies no issuer to validate', () => {
    expect(issuerFieldRefusal(base)).toBeNull();
  });

  it('refuses a non-https issuer only when the issuer is supplied', () => {
    expect(issuerFieldRefusal({ issuer: 'http://host', ...base })).toMatch(/https/);
  });

  it('refuses an empty refused-audience list', () => {
    expect(issuerFieldRefusal({ ...base, refusedAudiences: [''] })).toMatch(/refused audience/);
  });

  it('refuses an audience carrying a line break — newline is the separator', () => {
    expect(issuerFieldRefusal({ ...base, refusedAudiences: ['a\nb'] })).toMatch(/line break/);
  });

  it('requires the JWKS document under static mode, and only then', () => {
    expect(issuerFieldRefusal({ ...base, jwksMode: 'static', staticJwks: '   ' })).toMatch(/Static/);
    expect(
      issuerFieldRefusal({ ...base, jwksMode: 'static', staticJwks: '{"keys":[]}' }),
    ).toBeNull();
  });
});

describe('issuer refusal mappers', () => {
  it('names a create 409 as a duplicate byte-exact issuer', () => {
    expect(issuerCreateRefusalText(new ApiError(409, 'x'))).toMatch(/already configured/);
  });

  it('names a create 403 as the instance-config second factor, never a disclosure', () => {
    const text = issuerCreateRefusalText(new ApiError(403, 'x'));
    expect(text).toMatch(/second factor/);
    expect(text).not.toMatch(/disclosure/);
  });

  it('reads a 404 as the ambiguous absent-or-masked shape, not a bare deletion', () => {
    expect(issuerUpdateRefusalText(new ApiError(404, 'x'))).toMatch(/not disclosed to this session/);
    expect(issuerDeleteRefusalText(new ApiError(404, 'x'))).toMatch(/not disclosed to this session/);
  });

  it('names a delete 409 as the still-naming bindings, live or revoked', () => {
    expect(issuerDeleteRefusalText(new ApiError(409, 'x'))).toMatch(/live or revoked/);
  });
});
