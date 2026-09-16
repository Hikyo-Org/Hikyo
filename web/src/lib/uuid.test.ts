import { afterEach, describe, expect, it, vi } from 'vitest';

import { uuid } from './uuid.ts';

const V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

/** Insecure contexts (plain http on a LAN address) expose `getRandomValues`
 * and nothing else — this is the shape the SPA actually runs in on a phone. */
function withoutRandomUUID(): void {
  vi.stubGlobal('crypto', { getRandomValues: crypto.getRandomValues.bind(crypto) });
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.resetModules();
});

describe('uuid', () => {
  it('is a version 4 identifier', () => {
    const value = uuid();
    expect(value).toMatch(V4);
    expect(value[14]).toBe('4');
    expect('89ab').toContain(value[19]);
  });

  it('does not repeat', () => {
    const seen = new Set(Array.from({ length: 1000 }, () => uuid()));
    expect(seen.size).toBe(1000);
  });

  it('still yields a valid v4 without crypto.randomUUID', () => {
    withoutRandomUUID();
    expect(crypto.randomUUID).toBeUndefined();
    const values = Array.from({ length: 1000 }, () => uuid());
    for (const value of values) {
      expect(value).toMatch(V4);
      expect(value[14]).toBe('4');
      expect('89ab').toContain(value[19]);
    }
    expect(new Set(values).size).toBe(1000);
  });

  it('fails loud rather than falling back to a weak identifier', () => {
    vi.stubGlobal('crypto', {});
    expect(() => uuid()).toThrow(/Web Crypto/);
  });
});

describe('module load in an insecure context', () => {
  it('importing sessionEpoch does not throw without crypto.randomUUID', async () => {
    withoutRandomUUID();
    vi.resetModules();
    const module = await import('../api/sessionEpoch.ts');
    expect(module.SESSION_CHANNEL).toBe('hikyo-root-auth');
  });
});
