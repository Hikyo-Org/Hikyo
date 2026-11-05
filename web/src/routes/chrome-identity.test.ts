import { describe, expect, it } from 'vitest';

import {
  chromeIdentityFromApi,
  chromeIdentityStyle,
  orgIdentityImageUrl,
  projectIdentityImageUrl,
} from './chrome-identity.ts';

describe('chromeIdentityFromApi', () => {
  it('maps hue, mark and a versioned same-origin image', () => {
    const identity = chromeIdentityFromApi(
      { hue: 280, mark: 'PO', image_version: 'abc123' },
      (version) => orgIdentityImageUrl('org 1', version),
    );
    expect(identity).toEqual({
      hue: 280,
      glyph: 'PO',
      image: '/api/v1/orgs/org%201/identity/image?v=abc123',
    });
    expect(chromeIdentityStyle(identity).backgroundImage).toBe(
      'url("/api/v1/orgs/org%201/identity/image?v=abc123")',
    );
  });

  it('treats null and empty marks as the monogram and drops the image when remote', () => {
    expect(chromeIdentityFromApi({ hue: 195, mark: null, image_version: 'v' }, null)).toEqual({
      hue: 195,
      glyph: null,
      image: null,
    });
    expect(chromeIdentityFromApi({ hue: 10, mark: '' }, (v) => v).glyph).toBeNull();
    expect(chromeIdentityFromApi(undefined, null).hue).toBe(195);
  });

  it('encodes every project path segment', () => {
    expect(projectIdentityImageUrl('a/b', 'c d', 'v1')).toBe(
      '/api/v1/orgs/a%2Fb/projects/c%20d/identity/image?v=v1',
    );
  });
});
