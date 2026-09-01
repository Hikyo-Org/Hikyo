import { describe, expect, it } from 'vitest';

import { ApiError } from './client.ts';
import { samlFailureText, samlProviderInputErrors, SAML_SLUG_PATTERN } from './samlProviders.ts';

describe('SAML_SLUG_PATTERN', () => {
  it('accepts a lowercase slug and rejects uppercase, leading hyphen and overflow', () => {
    expect(SAML_SLUG_PATTERN.test('acme-idp')).toBe(true);
    expect(SAML_SLUG_PATTERN.test('a')).toBe(true);
    expect(SAML_SLUG_PATTERN.test('Acme')).toBe(false);
    expect(SAML_SLUG_PATTERN.test('-acme')).toBe(false);
    expect(SAML_SLUG_PATTERN.test('a'.repeat(65))).toBe(false);
  });
});

describe('samlProviderInputErrors', () => {
  const base = {
    creating: true,
    slug: 'acme',
    displayName: 'Acme',
    entityId: 'https://idp.example/acme',
    metadataSource: 'file' as const,
    metadataDocument: '<md:EntityDescriptor/>',
    metadataUrl: '',
  };

  it('passes a complete file-backed create', () => {
    expect(samlProviderInputErrors(base)).toEqual([]);
  });

  it('rejects a bad slug only while creating', () => {
    expect(samlProviderInputErrors({ ...base, slug: 'Bad Slug' })).toContain(
      'Slug must be lowercase letters, digits or hyphens, and start with a letter or digit.',
    );
    // On reconfigure the slug is fixed route data, so it is not re-validated.
    expect(samlProviderInputErrors({ ...base, creating: false, slug: 'Bad Slug' })).toEqual([]);
  });

  it('requires a display name and an entity id', () => {
    expect(samlProviderInputErrors({ ...base, displayName: '   ' })).toContain(
      'A display name is required.',
    );
    expect(samlProviderInputErrors({ ...base, entityId: '' })).toContain(
      'An entity ID is required.',
    );
  });

  it('requires exactly the metadata field its source names', () => {
    expect(samlProviderInputErrors({ ...base, metadataSource: 'file', metadataDocument: '' })).toContain(
      'Paste the IdP metadata XML for a file-backed provider.',
    );
    expect(
      samlProviderInputErrors({ ...base, metadataSource: 'url', metadataDocument: '', metadataUrl: 'https://idp.example/meta' }),
    ).toEqual([]);
    expect(samlProviderInputErrors({ ...base, metadataSource: 'url', metadataDocument: '', metadataUrl: '' })).toContain(
      'A metadata URL is required for a URL-backed provider.',
    );
    expect(
      samlProviderInputErrors({ ...base, metadataSource: 'url', metadataDocument: '', metadataUrl: 'ftp://idp.example' }),
    ).toContain('The metadata URL must be an https:// address.');
  });
});

describe('samlFailureText', () => {
  it('maps a 403 to a second-factor prompt', () => {
    expect(samlFailureText(new ApiError(403, 'forbidden'), 'save-provider')).toContain(
      'second factor',
    );
  });

  it('prefers a 400 detail and falls back to a per-action sentence', () => {
    expect(samlFailureText(new ApiError(400, 'bad', 'entityID selects no descriptor'), 'save-provider')).toBe(
      'entityID selects no descriptor',
    );
    expect(samlFailureText(new ApiError(400, 'bad'), 'save-provider')).toContain('metadata');
  });

  it('explains the active-key conflict on retire', () => {
    expect(samlFailureText(new ApiError(409, 'conflict'), 'retire-key')).toContain('active');
  });

  it('reports a not-disclosed 404 and a generic server fault', () => {
    expect(samlFailureText(new ApiError(404, 'gone'), 'save-provider')).toContain('not');
    expect(samlFailureText(new ApiError(500, 'boom'), 'save-provider')).toContain('server');
  });
});
