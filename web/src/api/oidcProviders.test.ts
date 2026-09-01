import { describe, expect, it } from 'vitest';

import { ApiError } from './client.ts';
import {
  oidcProviderRefusalText,
  validatePolicyJson,
  validateProviderDraft,
  validateSlug,
  type OidcProvider,
  type OidcProviderDraft,
} from './oidcProviders.ts';

const provider: OidcProvider = {
  slug: 'acme',
  display_name: 'Acme',
  issuer: 'https://issuer.example',
  client_id: 'client',
  scopes: 'openid',
  redirect_uri: 'https://hikyo.example/api/v1/auth/oidc/acme/callback',
  jit_policy: null,
  assurance_policy: null,
  enabled: true,
};

const goodDraft: OidcProviderDraft = {
  slug: 'acme',
  displayName: 'Acme',
  issuer: 'https://issuer.example',
  clientId: 'client',
  clientSecret: 'shhh',
  scopes: 'openid',
  jitPolicy: '',
  assurancePolicy: '',
  enabled: true,
};

describe('validateSlug', () => {
  it('accepts the contract pattern and rejects the rest', () => {
    expect(validateSlug('acme-1').ok).toBe(true);
    expect(validateSlug('a').ok).toBe(true);
    for (const bad of ['', '-lead', 'UPPER', 'has space', 'x'.repeat(65)]) {
      expect(validateSlug(bad).ok, bad).toBe(false);
    }
    // The contract pattern permits a trailing hyphen; the WebUI must not be
    // stricter than the server it validates for.
    expect(validateSlug('trailing-').ok).toBe(true);
    // 64 characters is the boundary the pattern allows.
    expect(validateSlug(`a${'b'.repeat(63)}`).ok).toBe(true);
    expect(validateSlug(`a${'b'.repeat(64)}`).ok).toBe(false);
  });
});

describe('validatePolicyJson', () => {
  it('treats empty as the absent policy, not an empty string', () => {
    expect(validatePolicyJson('   ', 'P')).toEqual({ ok: true, value: null });
  });

  it('accepts a JSON object and preserves its exact text', () => {
    expect(validatePolicyJson('{"claim":"groups","values":["a"]}', 'P')).toEqual({
      ok: true,
      value: '{"claim":"groups","values":["a"]}',
    });
  });

  it('rejects malformed JSON, arrays and primitives — never an unattributed 400', () => {
    expect(validatePolicyJson('{not json', 'P').ok).toBe(false);
    expect(validatePolicyJson('[1,2]', 'P').ok).toBe(false);
    expect(validatePolicyJson('"string"', 'P').ok).toBe(false);
    expect(validatePolicyJson('5', 'P').ok).toBe(false);
    expect(validatePolicyJson('null', 'P').ok).toBe(false);
  });
});

describe('validateProviderDraft', () => {
  it('binds a valid create draft to a wire input', () => {
    const result = validateProviderDraft(goodDraft, null);
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.slug).toBe('acme');
      expect(result.input.enabled).toBe(true);
      expect(result.input.jitPolicy).toBeNull();
    }
  });

  it('requires the write-only secret on every save, including a disable', () => {
    const disabling = { ...goodDraft, clientSecret: '', enabled: false };
    const result = validateProviderDraft(disabling, provider);
    expect(result).toMatchObject({ ok: false, field: 'client_secret' });
  });

  it('refuses a changed issuer on reconfigure as a field error, before the wire', () => {
    const moved = { ...goodDraft, issuer: 'https://other.example' };
    const result = validateProviderDraft(moved, provider);
    expect(result).toMatchObject({ ok: false, field: 'issuer' });
  });

  it('keeps the immutable slug of the reconfigured provider, ignoring the draft slug', () => {
    const result = validateProviderDraft({ ...goodDraft, slug: 'ignored' }, provider);
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.slug).toBe('acme');
    }
  });

  it('validates the slug only on create', () => {
    expect(validateProviderDraft({ ...goodDraft, slug: 'Bad Slug' }, null)).toMatchObject({
      ok: false,
      field: 'slug',
    });
    // A reconfigure never sends the slug, so an odd draft slug is irrelevant.
    expect(validateProviderDraft({ ...goodDraft, slug: 'Bad Slug' }, provider).ok).toBe(true);
  });
});

describe('oidcProviderRefusalText', () => {
  it('names both discovery and slug collision on an unattributed 400', () => {
    const text = oidcProviderRefusalText(new ApiError(400, 'bad'), 'save-oidc-provider');
    expect(text).toMatch(/OpenID configuration/);
    expect(text).toMatch(/slug is already in use/);
  });

  it('maps a 403 to the MFA-mandatory instance-config refusal', () => {
    expect(oidcProviderRefusalText(new ApiError(403, 'no'), 'save-oidc-provider')).toMatch(
      /instance-config/,
    );
  });

  it('says the directory is undisclosed on a list 404 but names the resource on a save 404', () => {
    expect(oidcProviderRefusalText(new ApiError(404, 'x'), 'list-oidc-providers')).toMatch(
      /directory is not disclosed/,
    );
    expect(oidcProviderRefusalText(new ApiError(404, 'x'), 'save-oidc-provider')).toMatch(
      /unavailable or does not exist/,
    );
  });

  it('maps a 409 to a stale-state reload', () => {
    expect(oidcProviderRefusalText(new ApiError(409, 'race'), 'save-oidc-provider')).toMatch(
      /changed underneath you/,
    );
  });

  it('reports an uncertain outcome for a non-ApiError, worded for the operation', () => {
    expect(oidcProviderRefusalText(new Error('boom'), 'delete-oidc-provider')).toMatch(
      /whether the provider was deleted is unknown/,
    );
    expect(oidcProviderRefusalText(new Error('boom'), 'save-oidc-provider')).toMatch(
      /whether the change applied is unknown/,
    );
  });
});
